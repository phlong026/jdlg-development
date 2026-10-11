package cn.iyque.phase1;

import static cn.iyque.phase1.Models.*;
import java.time.*;
import java.util.Set;
import org.springframework.stereotype.Service;
import org.springframework.transaction.annotation.Transactional;
import org.springframework.scheduling.annotation.Scheduled;
import org.springframework.data.domain.PageRequest;

@Service
public class CustomerProtection {
 public static final Duration PERIOD=Duration.ofDays(90);
 private final Repositories.Customers customers;
 private final Repositories.Followups followups;
 private final CustomerWorkflow workflow;
 public CustomerProtection(Repositories.Customers c,Repositories.Followups f,CustomerWorkflow w){customers=c;followups=f;workflow=w;}
 public static boolean inPool(Customer c) {
  if(c.getMergedInto()!=null||Set.of("won","archived","pending").contains(c.getLifecycle()))return false;
  return c.getPoolAt()!=null || (c.getProtectionExpiresAt()!=null && !c.getProtectionExpiresAt().isAfter(Instant.now()));
 }
 public static void protect(Customer c,Account actor) {
  c.setLifecycle("protected");c.setHistoricalReviewed(true);c.setProtectionStartedAt(Instant.now());
  c.setOwnerUsername(actor.getUsername());c.setOwnerId(actor.getEmployeeId());
  c.setPoolAt(null);c.setProtectionExpiresAt(Instant.now().plus(PERIOD));c.setUpdatedAt(Instant.now());
 }
 @Transactional
 public void releaseExpired() {
  workflow.lock();
  // Bounded batch; read permissions also check the deadline, including before the job runs.
  for(Customer c:customers.expired(Instant.now(),PageRequest.of(0,500))) {
   if(!inPool(c))continue;
   workflow.notice(c.getOwnerUsername(),c,"release:"+c.getId()+":"+c.getProtectionExpiresAt(),"客户保护到期，已进入公海");
   Followup f=new Followup();f.setEffective(false);f.setCustomerId(c.getId());f.setOperatorId("system");f.setKind("release");
   f.setContent("90 天无有效跟进，自动进入客户公海；原归属账号："+c.getOwnerUsername());f.setStage(c.getStage());followups.save(f);
   c.setLifecycle("pool");c.setPoolAt(c.getProtectionExpiresAt());c.setProtectionExpiresAt(null);c.setOwnerId(null);c.setOwnerUsername(null);
   c.setNextFollowupAt(null);c.setUpdatedAt(Instant.now());
  }
 }
}
