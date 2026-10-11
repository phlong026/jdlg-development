package cn.iyque.phase1;
import static cn.iyque.phase1.Models.*;
import cn.iyque.domain.IYQueCustomerInfo;
import java.time.Instant;
import java.util.Date;
import org.springframework.boot.ApplicationArguments;
import org.springframework.boot.ApplicationRunner;
import org.springframework.beans.factory.annotation.Value;
import org.springframework.stereotype.Component;
import org.springframework.transaction.annotation.Transactional;

@Component
public class Bootstrap implements ApplicationRunner {
 private final CustomerWorkflow workflow;private final Repositories.Accounts accounts;private final Repositories.Employees employees;
 private final Repositories.Customers customers;private final Repositories.Relations relations;private final Repositories.Settings settings;private final AuthService auth;
 @Value("${crm.admin-password}") private String adminPassword;
 @Value("${crm.demo:false}") private boolean demo;
 @Value("${crm.wecom.corp-id:}") private String corpId;
 public Bootstrap(Repositories.Accounts a,Repositories.Employees e,Repositories.Customers c,Repositories.Relations r,Repositories.Settings s,AuthService auth,CustomerWorkflow w) {workflow=w;accounts=a;employees=e;customers=c;relations=r;settings=s;this.auth=auth;}
 @Override @Transactional public void run(ApplicationArguments args) {
  if(!settings.existsById("import-lock")) { Setting s=new Setting();s.setId("import-lock");s.setSettingValue("1");settings.save(s); }
  String mode=demo?"demo":"live", deployment=demo?"DEMO":corpId;
  Setting existing=settings.findById("deployment").orElse(null);
  if(existing!=null && "live:".equals(existing.getSettingValue()) && !demo && customers.count()==0) {existing.setSettingValue("live:"+deployment);settings.save(existing);}
  if(existing!=null && !existing.getSettingValue().equals(mode+":"+deployment)) throw new IllegalStateException("数据库已绑定另一运行模式或企业，请使用独立数据库");
  if(existing==null) { Setting s=new Setting();s.setId("deployment");s.setSettingValue(mode+":"+deployment);settings.save(s); }
  if(!accounts.existsById("admin")) { Account a=new Account();a.setUsername("admin");a.setDisplayName("管理员");a.setRole("admin");a.setPasswordHash(auth.password(adminPassword));accounts.save(a); }
  if(demo && !settings.existsById("demo-seeded")) seed();
  workflow.lock();for(Customer c:customers.findAll()){if("miniprogram".equals(c.getAddedTimeSource()))continue;String kind="manual".equals(c.getAddedTimeSource())?"manual":"wecom";if(workflow.sourceCustomer(kind,c.getId())==null)workflow.registerSource(c,kind,c.getId(),c.getName(),c.getPhone(),c.getWechat(),"历史来源建档");}
 }
 private void seed() {
  String[] names={"张顾问","李顾问"};
  for(int i=0;i<2;i++) {
   Employee e=new Employee();e.setId("demo-advisor-"+(i+1));e.setName(names[i]);employees.save(e);
   Account a=new Account();a.setUsername("advisor"+(i+1));a.setDisplayName(names[i]);a.setEmployeeId(e.getId());a.setPasswordHash(auth.password("DemoAdvisor!2026"));accounts.save(a);
  }
  String[] names2={"陈先生","林女士","王先生","周女士","刘先生","赵女士","黄先生","吴女士"};
  String[] needs={"咨询两层自建房，地块约 12×10 米，希望先了解设计流程。","偏好现代风格，计划明年开工，约好周五继续沟通。","想了解三层住宅的大致造价区间。","关注适老化布局，需要与家人商量房间数量。","已确认设计服务，进入后续交付。","咨询合作渠道，待补充服务范围。","暂缓建房，三个月后再联系。","重复咨询，无进一步需求。"};
  for(int i=0;i<8;i++) {
   Customer c=new Customer();c.setId("demo-customer-"+(i+1));c.setName(names2[i]);c.setOwnerId("demo-advisor-"+(i%2+1));c.setDemand(needs[i]);
   c.setCategory(i==2?"造价咨询":i==5?"合作咨询":"建房咨询");c.setIntent(i<2?"high":"medium");c.setStage(i==4?"won":i==6?"paused":i==7?"invalid":i==0?"new":"following");
   c.setCreatedAt(Instant.now().minusSeconds(i*86400L));c.setAddedTimeSource("relation");c.setUpdatedAt(c.getCreatedAt());
   if(i<4) c.setNextFollowupAt(Instant.now().plusSeconds(i==0?-7200:i==1?3600:86400));
   customers.save(c);
   IYQueCustomerInfo r=new IYQueCustomerInfo();r.setEId(c.getId()+"&"+c.getOwnerId());r.setExternalUserid(c.getId());r.setCustomerName(c.getName());r.setUserId(c.getOwnerId());r.setStatus(0);r.setAddTime(Date.from(c.getCreatedAt()));relations.save(r);
  }
  Setting s=new Setting();s.setId("demo-seeded");s.setSettingValue("1");settings.save(s);
 }
}
