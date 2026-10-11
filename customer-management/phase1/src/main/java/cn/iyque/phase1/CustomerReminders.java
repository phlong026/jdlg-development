package cn.iyque.phase1;

import java.time.*;
import java.time.temporal.ChronoUnit;
import java.util.*;
import org.springframework.beans.factory.annotation.Value;
import org.springframework.scheduling.annotation.Scheduled;
import org.springframework.stereotype.Service;
import org.springframework.transaction.annotation.Transactional;
import static cn.iyque.phase1.Models.*;

@Service
public class CustomerReminders {
 private final CustomerWorkflow workflow; private final CustomerProtection protection;
 @Value("${crm.jobs.enabled:true}") private boolean enabled;
 public CustomerReminders(CustomerWorkflow w,CustomerProtection p){workflow=w;protection=p;}
 @Scheduled(fixedDelay=60000,initialDelay=60000) public void tick(){if(enabled){protection.releaseExpired();}}
 @Scheduled(fixedDelay=3600000,initialDelay=90000) @Transactional public void reminders(){if(enabled)createReminders();}
 @Transactional public void createReminders(){
  workflow.lock();Instant now=Instant.now();ZoneId zone=ZoneId.of("Asia/Shanghai");LocalDate today=now.atZone(zone).toLocalDate();
  Map<String,Account> owners=new HashMap<>();workflow.accounts().findAll().forEach(a->owners.put(a.getUsername(),a));
  for(Customer c:workflow.customers().findAll()){
   if(c.getMergedInto()!=null||CustomerProtection.inPool(c)||Set.of("won","archived").contains(c.getLifecycle()))continue;
   String user=c.getOwnerUsername();if(user==null&&c.getOwnerId()!=null)user=owners.values().stream().filter(a->Objects.equals(c.getOwnerId(),a.getEmployeeId())&&a.isEnabled()).map(Account::getUsername).findFirst().orElse(null);
   Account a=owners.get(user);if(a==null||!a.isEnabled()||(a.getEmployeeId()!=null&&!workflow.employees().findById(a.getEmployeeId()).map(Employee::isActive).orElse(false))){workflow.notifyAdmins(c,"handover:"+c.getId()+":"+today,"客户归属待确认或负责人已停用，请安排交接");continue;}
   if(c.getProtectionExpiresAt()!=null){long days=ChronoUnit.DAYS.between(today,c.getProtectionExpiresAt().atZone(zone).toLocalDate());if(Set.of(7L,3L,1L).contains(days))workflow.notice(user,c,"expiry:"+c.getId()+":"+today,"客户保护期剩余 "+days+" 天，请及时跟进");}
   if(c.getNextFollowupAt()!=null&&Set.of("new","following").contains(c.getStage())){LocalDate date=c.getNextFollowupAt().atZone(zone).toLocalDate();if(!date.isAfter(today))workflow.notice(user,c,"todo:"+c.getId()+":"+today,c.getNextFollowupAt().isBefore(now)?"客户跟进已逾期":"今天有客户需要跟进");}
  }
 }
}
