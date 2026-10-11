package cn.iyque.phase1;
import static cn.iyque.phase1.Models.*;
import cn.iyque.domain.IYQueCustomerInfo;
import java.time.Instant;
import java.util.*;
import org.springframework.stereotype.Service;
import org.springframework.transaction.annotation.Transactional;

@Service
public class ImportService {
 private final Repositories.Customers customers;private final Repositories.Relations relations;
 private final Repositories.Events events;private final Repositories.Settings settings;private final Repositories.Employees employees;
 private final CustomerWorkflow workflow;
 public ImportService(Repositories.Customers c,Repositories.Relations r,Repositories.Events e,Repositories.Settings s,Repositories.Employees p,CustomerWorkflow w) {workflow=w;customers=c;relations=r;events=e;settings=s;employees=p;}
 private void lock() { workflow.lock();settings.locked("import-lock").orElseThrow(()->new IllegalStateException("import lock missing")); }
 private String clockId(String relationId) {return "rel:"+AuthService.hash(relationId).substring(0,60);}
 private boolean newer(String relationId,long at) {
  return settings.findById(clockId(relationId)).map(s->at>=Long.parseLong(s.getSettingValue())).orElse(true);
 }
 private void clock(String relationId,long at) {
  Setting s=new Setting();s.setId(clockId(relationId));s.setSettingValue(Long.toString(at));settings.save(s);
 }
 private void employee(String id) {
  if(!employees.existsById(id)) {Employee e=new Employee();e.setId(id);e.setName(id);employees.save(e);}
 }
 @Transactional public boolean accept(Event event) {
  lock();
  if(events.existsById(event.getId())) return false;
  String key=event.getCustomerId()+"&"+event.getEmployeeId();
  boolean add="add_external_contact".equals(event.getChangeType());
  boolean delete=Set.of("del_external_contact","del_follow_user").contains(event.getChangeType());
  if((add||delete) && newer(key,event.getEventTime())) {
   Customer c=workflow.sourceCustomer("wecom",event.getCustomerId());if(c==null)c=customers.findById(event.getCustomerId()).orElse(null);
   if(c==null && add) { c=new Customer();c.setId(event.getCustomerId());c.setName("待同步客户");c.setLifecycle("pending");CustomerWorkflow.number(c);c.setOwnerId(event.getEmployeeId());c.setCreatedAt(Instant.ofEpochSecond(event.getEventTime()));c.setAddedTimeSource("event");customers.save(c); }
   if(c!=null)workflow.registerSource(c,"wecom",event.getCustomerId(),c.getName(),"","",event.getSourceState());
   employee(event.getEmployeeId());
   IYQueCustomerInfo r=relations.findById(key).orElseGet(IYQueCustomerInfo::new);
   r.setEId(key);r.setExternalUserid(event.getCustomerId());r.setUserId(event.getEmployeeId());
   if(add) {r.setAddTime(Date.from(Instant.ofEpochSecond(event.getEventTime())));r.setState(event.getSourceState());r.setStatus(0);}
   else r.setStatus("del_follow_user".equals(event.getChangeType())?1:2);
   relations.save(r);clock(key,event.getEventTime());
  }
  events.save(event);return true;
 }
 @Transactional public void snapshot(WecomGateway.Snapshot snapshot,long readStartedAt) {
  lock();
  Customer c=workflow.sourceCustomer("wecom",snapshot.id());if(c==null)c=customers.findById(snapshot.id()).orElse(null);
  List<WecomGateway.Relation> follows=snapshot.relations();
  if(c==null && follows.isEmpty()) return;
  if(c==null) {
   c=new Customer();c.setId(snapshot.id());c.setLifecycle("pending");CustomerWorkflow.number(c);
   WecomGateway.Relation first=follows.stream().min(Comparator.comparingLong(WecomGateway.Relation::addedAt).thenComparing(WecomGateway.Relation::employeeId)).get();
   c.setOwnerId(first.employeeId());c.setCreatedAt(Instant.ofEpochSecond(first.addedAt()>0?first.addedAt():readStartedAt));
   c.setAddedTimeSource(first.addedAt()>0?"relation":"unknown");
  }
  if("unknown".equals(c.getAddedTimeSource())) {
   var earliest=follows.stream().filter(f->f.addedAt()>0).min(Comparator.comparingLong(WecomGateway.Relation::addedAt));
   if(earliest.isPresent()) {c.setCreatedAt(Instant.ofEpochSecond(earliest.get().addedAt()));c.setAddedTimeSource("relation");}
  }
  if(c.getId().equals(snapshot.id())&&!c.isHistoricalReviewed()){c.setName(CustomerService.text(snapshot.name(),200));c.setAvatar(CustomerService.text(snapshot.avatar(),1024));}
  // Synchronization must not overwrite the manually managed stage, owner, notes or next follow-up.
  customers.save(c);workflow.registerSource(c,"wecom",snapshot.id(),snapshot.name(),"","","企业微信好友同步");
  for(WecomGateway.Relation follow:follows) {
   String key=snapshot.id()+"&"+follow.employeeId();
   if(!newer(key,readStartedAt)) continue;
   employee(follow.employeeId());
   IYQueCustomerInfo r=relations.findById(key).orElseGet(IYQueCustomerInfo::new);
   r.setEId(key);r.setExternalUserid(snapshot.id());r.setCustomerName(c.getName());r.setUserId(follow.employeeId());
   r.setAvatar(snapshot.avatar()==null?null:snapshot.avatar().substring(0,Math.min(255,snapshot.avatar().length())));r.setType(snapshot.type());
   r.setStatus(0);r.setState(CustomerService.text(follow.state(),255));r.setAddTime(Date.from(Instant.ofEpochSecond(follow.addedAt()>0?follow.addedAt():readStartedAt)));
   relations.save(r);clock(key,readStartedAt);
  }
 }
 @Transactional public void reconcileEmployee(String employeeId,Set<String> seen,long readStartedAt) {
  lock();
  for(IYQueCustomerInfo r:relations.findByUserId(employeeId)) {
   if(!seen.contains(r.getExternalUserid()) && newer(r.getEId(),readStartedAt)) {
    // A missing list entry cannot tell which side deleted; retain a known callback reason.
    if(!Integer.valueOf(1).equals(r.getStatus()) && !Integer.valueOf(2).equals(r.getStatus())) r.setStatus(3);
    relations.save(r);clock(r.getEId(),readStartedAt);
   }
  }
 }
 @Transactional public void roster(Map<String,String> names) {
  lock();
  for(Employee e:employees.findAll()) { e.setActive(names.containsKey(e.getId()));employees.save(e); }
  names.forEach((id,name)-> {Employee e=employees.findById(id).orElseGet(Employee::new);e.setId(id);e.setName(name);e.setActive(true);employees.save(e);});
 }
}
