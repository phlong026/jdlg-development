package cn.iyque.phase1;
import static cn.iyque.phase1.Models.*;
import java.time.Instant;
import java.util.*;
import java.util.concurrent.atomic.AtomicBoolean;
import org.springframework.scheduling.annotation.Scheduled;
import org.springframework.stereotype.Service;
import org.springframework.boot.context.event.ApplicationReadyEvent;
import org.springframework.context.event.EventListener;
import org.springframework.transaction.annotation.Transactional;
import org.springframework.beans.factory.annotation.Value;

@Service
public class SyncService {
 private final WecomGateway gateway;private final ImportService imports;private final Repositories.Events events;private final Repositories.SyncRuns runs;
 private final AtomicBoolean processing=new AtomicBoolean(false);
 @Value("${crm.jobs.enabled:true}") private boolean jobsEnabled;
 public SyncService(WecomGateway g,ImportService i,Repositories.Events e,Repositories.SyncRuns r) {gateway=g;imports=i;events=e;runs=r;}
 @EventListener(ApplicationReadyEvent.class) public void recover() {
  for(SyncRun r:runs.findAll()) if("running".equals(r.getStatus())) {r.setStatus("failed");r.setError("上次同步因服务重启中断，请重新同步");r.setFinishedAt(Instant.now());runs.save(r);}
 }
 public synchronized SyncRun enqueue() {
  if(!gateway.configured()) throw new ApiException(503,"请先配置企微客户联系凭据");
  if(runs.findFirstByStatusOrderByIdAsc("running").isPresent() || runs.findFirstByStatusOrderByIdAsc("pending").isPresent()) throw new ApiException(409,"已有同步任务正在处理");
  return runs.save(new SyncRun());
 }
 @Scheduled(fixedDelay=1500) public void scheduledEvents() {if(jobsEnabled) events();}
 public void events() {
  for(Event e:events.findTop30ByStatusAndNextAttemptAtBeforeOrderByEventTimeAsc("pending",Instant.now())) {
   try {
    if(!Set.of("del_follow_user","del_external_contact").contains(e.getChangeType())) {
     long at=Instant.now().getEpochSecond();imports.snapshot(gateway.contact(e.getCustomerId()),at);
    }
    e.setStatus("done");e.setError(null);
   }catch(Exception error) {
    e.setAttempts(e.getAttempts()+1);e.setError(error instanceof ApiException?error.getMessage():"客户补同步失败");
    e.setNextAttemptAt(Instant.now().plusSeconds(Math.min(900,15L*(1L<<Math.min(6,e.getAttempts())))));
    if(e.getAttempts()>=8) e.setStatus("failed");
   }
   events.save(e);
  }
 }
 public void retry(String id) {
  Event e=events.findById(id).orElseThrow(()->new ApiException(404,"事件不存在"));
  if("done".equals(e.getStatus())) return;
  e.setStatus("pending");e.setAttempts(0);e.setNextAttemptAt(Instant.now());events.save(e);
 }
 @Scheduled(fixedDelay=5000) public void scheduledSync() {if(jobsEnabled) sync();}
 public void sync() {
  if(!processing.compareAndSet(false,true)) return;
  try {
   Optional<SyncRun> next=runs.findFirstByStatusOrderByIdAsc("pending");if(next.isEmpty()) return;
   SyncRun run=next.get();run.setStatus("running");runs.save(run);
   try {
    Map<String,String> names=new LinkedHashMap<>();for(String id:gateway.employees()) names.put(id,gateway.employeeName(id));
    imports.roster(names);Set<String> fetched=new HashSet<>();
    for(String employee:names.keySet()) {
     long readStarted=Instant.now().getEpochSecond();Set<String> ids=new HashSet<>(gateway.customerIds(employee));
     for(String id:ids) if(fetched.add(id)) { long at=Instant.now().getEpochSecond();imports.snapshot(gateway.contact(id),at);run.setProcessed(run.getProcessed()+1);runs.save(run); }
     // Only reconcile this employee after its entire list and details were read successfully.
     imports.reconcileEmployee(employee,ids,readStarted);
    }
    run.setStatus("done");
   }catch(Exception e) {run.setStatus("failed");run.setError(e instanceof ApiException?e.getMessage():"同步失败，请查看服务日志并重试");}
   run.setFinishedAt(Instant.now());runs.save(run);
  }finally {processing.set(false);}
 }
 @Scheduled(fixedDelay=1800000,initialDelay=1800000) public void periodic() {if(jobsEnabled && gateway.configured()) try {enqueue();}catch(ApiException ignored) {}}
}
