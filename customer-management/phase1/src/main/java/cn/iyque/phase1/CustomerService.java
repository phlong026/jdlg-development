package cn.iyque.phase1;
import static cn.iyque.phase1.Models.*;
import cn.iyque.domain.IYQueCustomerInfo;
import java.time.*;
import java.util.*;
import javax.persistence.criteria.Predicate;
import org.springframework.data.domain.*;
import org.springframework.data.jpa.domain.Specification;
import org.springframework.stereotype.Service;
import org.springframework.transaction.annotation.Transactional;

@Service
public class CustomerService {
 public static final Set<String> STAGES=Set.of("new","following","won","paused","invalid");
 public static final Set<String> CATEGORIES=Set.of("待确认","建房咨询","设计咨询","造价咨询","合作咨询");
 private final Repositories.Customers customers;
 private final Repositories.Followups followups;
 private final Repositories.Relations relations;
 private final Repositories.Employees employees;
 private final Repositories.Settings settings;
 private final CustomerWorkflow workflow;
 public CustomerService(Repositories.Customers c,Repositories.Followups f,Repositories.Relations r,Repositories.Employees e,Repositories.Settings s,CustomerWorkflow w) { customers=c;followups=f;relations=r;employees=e;settings=s;workflow=w; }
 public record Filter(String keyword,String stage,String ownerId,String intent,String category,String queue,String source) {}
 public record Edit(Long version,String ownerId,String stage,String category,String intent,String phone,String demand,Instant nextFollowupAt) {}
 public record Follow(Long version,String content,String stage,Instant nextFollowupAt,String kind,String outcome,List<String> attachmentIds) {
  public Follow(Long version,String content,String stage,Instant nextFollowupAt){this(version,content,stage,nextFollowupAt,"followup","contacted",List.of());}
  public Follow(Long version,String content,String stage,Instant nextFollowupAt,String kind){this(version,content,stage,nextFollowupAt,kind,"contacted",List.of());}
 }
 public record Report(String name,String wechat,String phone,String address,java.math.BigDecimal buildingArea,String buildingType,String expectedBuildTime,String remark) {}
 public record Claim(Long version) {}
 public static final Set<String> ACTIVITIES=Set.of("followup","communication","drawing","quotation");
 private boolean owns(Account a,Customer c) {return CustomerWorkflow.owns(a,c);}
 private static String required(String value,int max,String label) {String s=text(value,max);if(s.isBlank())throw new ApiException(400,"请填写"+label);return s;}
 static String phone(String value) {
  String s=required(value,80,"联系电话").replaceAll("[\\s()（）-]","");
  if(s.startsWith("+86"))s=s.substring(3);else if(s.startsWith("0086"))s=s.substring(4);
  if(!s.matches("(?:1[3-9][0-9]{9}|0[0-9]{9,11})"))throw new ApiException(400,"请填写有效的手机号或带区号的固定电话");return s;
 }
 private void reportingLock(){workflow.lock();}
 static void applyReport(Customer c,Report r) {
  c.setName(required(r.name(),200,"客户姓名"));c.setWechat(text(r.wechat(),80));c.setPhone(phone(r.phone()));c.setPhoneKey(c.getPhone());
  c.setAddress(required(r.address(),500,"详细镇村地址"));
  if(r.buildingArea()==null||r.buildingArea().signum()<=0||r.buildingArea().compareTo(new java.math.BigDecimal("1000000"))>0||r.buildingArea().stripTrailingZeros().scale()>2)throw new ApiException(400,"建房面积需大于 0、不超过 100 万平方米，最多两位小数");
  if(!Set.of("新建","翻建","改造","装修").contains(r.buildingType()==null?"":r.buildingType()))throw new ApiException(400,"请选择建房需求");
  c.setBuildingArea(r.buildingArea());c.setBuildingType(r.buildingType());c.setExpectedBuildTime(required(r.expectedBuildTime(),100,"预计建房时间"));c.setRemark(text(r.remark(),2000));
 }
 @Transactional public Customer report(Account a,Report r) {
  reportingLock();Customer c=new Customer();applyReport(c,r);
  List<Customer> duplicates=customers.findByPhoneKeyOrPhone(c.getPhoneKey(),c.getPhone()).stream().filter(x->x.getMergedInto()==null).toList();
  if(!duplicates.isEmpty())throw new ApiException(409,duplicates.stream().allMatch(CustomerProtection::inPool)?"该手机号已在客户公海，请搜索并重新报备认领":"该手机号已有客户档案，不能重复报备，请查看本人客户或联系主管核实");
  c.setId("manual-"+UUID.randomUUID());c.setCustomerNumber("KH"+UUID.randomUUID().toString().replace("-","").toUpperCase(Locale.ROOT));
  c.setAddedTimeSource("manual");c.setCategory("建房咨询");CustomerProtection.protect(c,a);customers.saveAndFlush(c);workflow.registerSource(c,"manual",c.getId(),c.getName(),c.getPhone(),c.getWechat(),c.getRemark());record(c,a,"report","手动报备客户，锁定归属，开启 90 天保护期");return c;
 }
 @Transactional public Customer claim(Account a,String id,Long version) {
  Customer c=workflow.locked(id);
  if(!CustomerProtection.inPool(c))throw new ApiException(409,"客户已被认领或仍在保护期，请刷新列表");
  if(version==null||!Objects.equals(version,c.getVersion()))throw new ApiException(409,"客户已被更新，请刷新后重试");
  if(c.getPoolAt()==null)record(c,a,"release","90 天保护已到期，进入公海；原归属账号："+c.getOwnerUsername());
  CustomerProtection.protect(c,a);c.setStage("new");c.setNextFollowupAt(null);workflow.notice(a.getUsername(),c,"claim:"+UUID.randomUUID(),"公海客户认领成功，开始 90 天保护");
  record(c,a,"claim","从公海重新报备认领，开启新的 90 天保护期");return customers.saveAndFlush(c);
 }
 @Transactional public Customer editReport(Account a,String id,Long version,Report r) {
  reportingLock();Customer c=editable(a,id,version);

  String key=phone(r.phone());
  boolean shared="protected".equals(c.getLifecycle())&&c.getPhoneKey()==null&&key.equals(c.getPhone());
  if(!shared&&customers.findByPhoneKeyOrPhone(key,key).stream().anyMatch(other->other.getMergedInto()==null&&!workflow.historyIds(c).contains(other.getId())))throw new ApiException(409,"该联系电话已存在客户档案");
  applyReport(c,r);if(shared)c.setPhoneKey(null);CustomerWorkflow.number(c);if("pending".equals(c.getLifecycle())){Account owner=c.getOwnerUsername()==null?null:workflow.accounts().findById(c.getOwnerUsername()).filter(Account::isEnabled).orElse(null);if(owner==null&&owns(a,c))owner=a;if(owner==null)throw new ApiException(400,"请先由主管分配归属账号");CustomerProtection.protect(c,owner);}c.setUpdatedAt(Instant.now());record(c,a,"profile","完善报备资料；待核实客户资料齐备后开始首次保护，已有保护不续期");return customers.saveAndFlush(c);
 }
 private boolean active(String stage) { return "new".equals(stage)||"following".equals(stage); }
 public Specification<Customer> specification(Account actor,Filter filter) {
  if(filter!=null) {
   if(present(filter.stage())&&!STAGES.contains(filter.stage())) throw new ApiException(400,"客户阶段无效");
   if(present(filter.category())&&!CATEGORIES.contains(filter.category())) throw new ApiException(400,"咨询类型无效");
   if(present(filter.intent())&&!Set.of("high","medium","low").contains(filter.intent())) throw new ApiException(400,"意向无效");
   if(filter.keyword()!=null&&filter.keyword().length()>200) throw new ApiException(400,"搜索内容过长");
  }
  return (r,q,b)->{
   List<Predicate> p=new ArrayList<>();p.add(b.isNull(r.get("mergedInto")));
   Predicate pool=b.or(b.isNotNull(r.get("poolAt")),b.lessThanOrEqualTo(r.get("protectionExpiresAt"),Instant.now()));
   if(filter!=null&&"pool".equals(filter.queue()))p.add(pool);
   else {
    p.add(b.isNull(r.get("poolAt")));p.add(b.or(b.isNull(r.get("protectionExpiresAt")),b.greaterThan(r.get("protectionExpiresAt"),Instant.now())));
    if(!actor.isAdmin()) p.add(b.or(b.and(b.isNull(r.get("ownerUsername")),b.equal(r.get("ownerId"),Objects.toString(actor.getEmployeeId(),"__unbound__"))),b.equal(r.get("ownerUsername"),actor.getUsername())));
   }
   if(filter!=null) {
    if(filter.keyword()!=null && !filter.keyword().isBlank()) {
     String like="%"+filter.keyword().trim().replace("\\","\\\\").replace("%","\\%").replace("_","\\_")+"%";
     p.add(b.or(b.like(r.get("name"),like,'\\'),b.like(r.get("phone"),like,'\\'),b.like(r.get("demand"),like,'\\'),b.like(r.get("customerNumber"),like,'\\'),b.like(r.get("wechat"),like,'\\'),b.like(r.get("address"),like,'\\')));
    }
    if(present(filter.source())){if(!Set.of("manual","wecom","miniprogram").contains(filter.source()))throw new ApiException(400,"来源无效");var sub=q.subquery(String.class);var sr=sub.from(Source.class);sub.select(sr.get("customerId")).where(b.equal(sr.get("kind"),filter.source()));p.add(r.get("id").in(sub));}
    if(present(filter.stage())) p.add(b.equal(r.get("stage"),filter.stage()));
    if(present(filter.ownerId())) p.add(filter.ownerId().startsWith("account:")?b.equal(r.get("ownerUsername"),filter.ownerId().substring(8)):b.equal(r.get("ownerId"),filter.ownerId()));
    if(present(filter.intent())) p.add(b.equal(r.get("intent"),filter.intent()));
    if(present(filter.category())) p.add(b.equal(r.get("category"),filter.category()));
    Instant now=Instant.now(),start=LocalDate.now(ZoneId.of("Asia/Shanghai")).atStartOfDay(ZoneId.of("Asia/Shanghai")).toInstant(),end=start.plusSeconds(86400);
    Predicate open=r.get("stage").in("new","following");
    String queue=filter.queue()==null?"all":filter.queue();
    switch(queue) {
     case "added":p.add(b.notEqual(r.get("addedTimeSource"),"unknown"));p.add(b.greaterThanOrEqualTo(r.get("createdAt"),start));p.add(b.lessThan(r.get("createdAt"),end));break;
     case "today": p.add(open);p.add(b.greaterThanOrEqualTo(r.get("nextFollowupAt"),start));p.add(b.lessThan(r.get("nextFollowupAt"),end));break;
     case "overdue": p.add(open);p.add(b.lessThan(r.get("nextFollowupAt"),now));break;
     case "unplanned":p.add(open);p.add(b.isNull(r.get("nextFollowupAt")));break;
     case "unassigned":p.add(b.isNull(r.get("ownerId")));p.add(b.isNull(r.get("ownerUsername")));break;
     case "pending":p.add(b.equal(r.get("lifecycle"),"pending"));break;
     case "pool":break;
     case "new":p.add(b.equal(r.get("stage"),"new"));break;
     case "all":break;
     default:throw new ApiException(400,"待办筛选无效");
    }
   }
   return b.and(p.toArray(new Predicate[0]));
  };
 }
 private static boolean present(String s) { return s!=null && !s.isBlank() && !"all".equals(s); }
 public Page<Customer> list(Account a,Filter f,int page,int size) {
  return customers.findAll(specification(a,f),PageRequest.of(Math.max(0,page-1),Math.min(100,Math.max(1,size)),Sort.by(Sort.Order.desc("updatedAt"),Sort.Order.asc("id"))));
 }
 public Customer visible(Account a,String id) {return workflow.visible(a,id);}
 public Map<String,Object> detail(Account a,String id) {
  Customer c=visible(a,id);List<IYQueCustomerInfo> rel=new ArrayList<>();List<Followup> history=new ArrayList<>();List<ReviewCase> reviewCases=new ArrayList<>();
  for(String cid:workflow.historyIds(c)){reviewCases.addAll(workflow.cases().findByCustomerIdOrderByCreatedAtDesc(cid));rel.addAll(relations.findByExternalUserid(cid));history.addAll(followups.findByCustomerIdOrderByCreatedAtDescIdDesc(cid));}
  history.sort(Comparator.comparing(Followup::getCreatedAt).thenComparing(Followup::getId).reversed());
  return Map.of("customer",c,"relations",rel,"followups",history,"sources",workflow.sources().findByCustomerId(c.getId()),"attachments",workflow.files(c),"cases",reviewCases);
 }
 private Customer editable(Account a,String id,Long version) {
  Customer c=workflow.locked(id);workflow.version(c,version);
  if(CustomerProtection.inPool(c))throw new ApiException(409,"客户已进入公海，请先重新报备认领");
  if(!a.isAdmin()&&!owns(a,c))throw new ApiException(403,"无权修改此客户");return c;
 }
 private void stageChange(Customer c,String stage){if(!Objects.equals(c.getStage(),stage)&&(Set.of("won","invalid").contains(Objects.toString(stage,""))||Set.of("won","archived").contains(c.getLifecycle())))throw new ApiException(400,"成交、归档及恢复需通过主管处理");}
 private void validate(String stage,Instant next) {
  if(!STAGES.contains(stage==null?"":stage)) throw new ApiException(400,"客户状态无效");
  if(active(stage) && next!=null && !next.isAfter(Instant.now())) throw new ApiException(400,"下次跟进时间应晚于现在");
 }
 private void record(Customer c,Account a,String kind,String content) {
  Followup f=new Followup();f.setCustomerId(c.getId());f.setOperatorId(a.getUsername());f.setKind(kind);f.setContent(content);
  f.setEffective(false);f.setStage(c.getStage());f.setNextFollowupAt(c.getNextFollowupAt());followups.save(f);
 }
 @Transactional public Customer edit(Account a,String id,Edit e) {
  Customer c=editable(a,id,e.version());stageChange(c,e.stage());
  if(!"legacy".equals(c.getLifecycle()) && !Objects.equals(c.getOwnerId(),e.ownerId()))throw new ApiException(409,"该客户已进入业务流转，不能在此调整负责人，请使用主管「分配 / 交接」");
  // Unchanged overdue schedules are valid when only profile fields are edited.
  if(!Objects.equals(c.getStage(),e.stage()) || !Objects.equals(c.getNextFollowupAt(),e.nextFollowupAt())) validate(e.stage(),e.nextFollowupAt());
  if(!CATEGORIES.contains(e.category()==null?"":e.category()) || !Set.of("high","medium","low").contains(e.intent()==null?"":e.intent())) throw new ApiException(400,"分类或意向无效");
  if(!a.isAdmin() && !Objects.equals(e.ownerId(),c.getOwnerId())) throw new ApiException(403,"只有管理员可以调整负责人");
  String owner=e.ownerId()==null||e.ownerId().isBlank()?null:e.ownerId();
  if(owner!=null && !employees.findById(owner).map(Employee::isActive).orElse(false)) throw new ApiException(400,"负责人不是有效员工");
  String before=c.getOwnerId();
  c.setOwnerId(owner);c.setStage(e.stage());c.setCategory(e.category());c.setIntent(e.intent());
  if(c.getCustomerNumber()!=null&&!Objects.equals(c.getPhone(),e.phone()))throw new ApiException(400,"请通过报备资料修改联系电话");
  c.setPhone(text(e.phone(),80));c.setDemand(text(e.demand(),2000));
  c.setNextFollowupAt(active(c.getStage())?e.nextFollowupAt():null);c.setUpdatedAt(Instant.now());
  record(c,a,"profile","更新客户资料和跟进安排"+(!Objects.equals(before,owner)?"；负责人由 "+(before==null?"未分配":before)+" 调整为 "+(owner==null?"未分配":owner):""));
  return customers.saveAndFlush(c);
 }
 @Transactional public Customer follow(Account a,String id,Follow f) {
  Customer c=editable(a,id,f.version());stageChange(c,f.stage());validate(f.stage(),f.nextFollowupAt());
  String content=text(f.content(),2000);if(content.isBlank())throw new ApiException(400,"请填写本次跟进记录");
  String kind=f.kind()==null?"followup":f.kind();if(!ACTIVITIES.contains(kind))throw new ApiException(400,"请选择有效的跟进类型");
  String outcome=f.outcome()==null?"contacted":f.outcome();if(!Set.of("contacted","no_answer","no_reply","internal").contains(outcome))throw new ApiException(400,"联系结果无效");
  boolean effective="contacted".equals(outcome);workflow.validateFiles(c,f.attachmentIds(),effective&&Set.of("drawing","quotation").contains(kind));
  if(active(f.stage())&&f.nextFollowupAt()==null)throw new ApiException(400,"继续跟进的客户请安排下次联系时间");
  c.setStage(f.stage());c.setNextFollowupAt(active(f.stage())?f.nextFollowupAt():null);Instant now=Instant.now();c.setUpdatedAt(now);
  if(effective){c.setLastFollowupAt(now);if("protected".equals(c.getLifecycle()))c.setProtectionExpiresAt(now.plus(CustomerProtection.PERIOD));}
  Followup row=new Followup();row.setCustomerId(id);row.setOperatorId(a.getUsername());row.setContent(content);row.setKind(kind);row.setOutcome(outcome);row.setEffective(effective);row.setAttachmentIds(String.join(",",f.attachmentIds()==null?List.of():f.attachmentIds()));row.setCreatedAt(now);row.setStage(c.getStage());row.setNextFollowupAt(c.getNextFollowupAt());followups.save(row);return customers.saveAndFlush(c);
 }
 public static String text(String value,int max) {
  String s=value==null?"":value.trim();if(s.length()>max) throw new ApiException(400,"输入内容过长");return s;
 }
 public Map<String,Long> overview(Account a) {
  Map<String,Long> m=new LinkedHashMap<>();
  for(String queue:List.of("all","new","today","overdue","unplanned","unassigned","pending","pool")) m.put(queue,customers.count(specification(a,new Filter(null,null,null,null,null,queue,null))));
  m.put("won",customers.count(specification(a,new Filter(null,"won",null,null,null,"all",null))));return m;
 }
 public record TeamRow(String ownerId,String name,long total,long today,long overdue,long unplanned) {}
 public Map<String,Object> dashboard(Account a) {
  Instant start=LocalDate.now(ZoneId.of("Asia/Shanghai")).atStartOfDay(ZoneId.of("Asia/Shanghai")).toInstant();
  Specification<Customer> known=(r,q,b)->b.notEqual(r.get("addedTimeSource"),"unknown");
  Specification<Customer> addedToday=(r,q,b)->b.and(b.greaterThanOrEqualTo(r.get("createdAt"),start),b.lessThan(r.get("createdAt"),start.plusSeconds(86400)));
  Specification<Customer> scope=specification(a,null);
  List<Customer> recent=customers.findAll(scope.and(known),PageRequest.of(0,5,Sort.by(Sort.Order.desc("createdAt"),Sort.Order.asc("id")))).getContent();
  List<TeamRow> team=new ArrayList<>();
  if(a.isAdmin()) {
   Instant now=Instant.now();
   for(Object[] row:customers.team(start,start.plusSeconds(86400),now)) {
    String owner=(String)row[0];String name=workflow.accounts().findById(owner).map(Account::getDisplayName).orElseGet(()->employees.findById(owner).map(Employee::getName).orElse(owner+"（已停用）"));
    team.add(new TeamRow(owner,name,((Number)row[1]).longValue(),((Number)row[2]).longValue(),((Number)row[3]).longValue(),((Number)row[4]).longValue()));
   }
  }
  return Map.of("overview",overview(a),"todayAdded",customers.count(scope.and(known).and(addedToday)),"unknownAddedTime",customers.count(scope.and(Specification.not(known))),"recent",recent,"team",team,"asOf",Instant.now());
 }
 public List<Customer> export(Account a,Filter f) {
  long count=customers.count(specification(a,f));if(count>20000) throw new ApiException(400,"请缩小筛选范围至 20000 条以内");
  return customers.findAll(specification(a,f),Sort.by("id"));
 }
}
