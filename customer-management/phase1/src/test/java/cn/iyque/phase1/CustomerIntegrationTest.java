package cn.iyque.phase1;

import static cn.iyque.phase1.Models.*;
import static org.assertj.core.api.Assertions.*;
import static org.mockito.Mockito.*;
import static org.springframework.test.web.servlet.request.MockMvcRequestBuilders.*;
import static org.springframework.test.web.servlet.result.MockMvcResultMatchers.*;
import java.time.Instant;
import java.util.*;
import javax.servlet.http.Cookie;
import javax.xml.parsers.DocumentBuilderFactory;
import org.xml.sax.InputSource;
import java.io.StringReader;
import cn.iyque.utils.IYqueCryptUtil;
import com.fasterxml.jackson.databind.ObjectMapper;
import org.junit.jupiter.api.*;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.boot.test.context.SpringBootTest;
import org.springframework.boot.test.autoconfigure.web.servlet.AutoConfigureMockMvc;
import org.springframework.boot.test.mock.mockito.MockBean;
import org.springframework.http.MediaType;
import org.springframework.test.web.servlet.*;

@SpringBootTest(properties={
 "spring.datasource.url=jdbc:h2:mem:crmtest;MODE=MySQL;DB_CLOSE_DELAY=-1",
 "spring.datasource.username=sa", "spring.datasource.password=", "spring.datasource.driver-class-name=org.h2.Driver",
 "crm.admin-password=TestAdmin!2026", "crm.secure-cookie=false", "crm.demo=false", "crm.jobs.enabled=false",
 "crm.standalone-enabled=true",
 "crm.wecom.corp-id=ww-test", "crm.wecom.token=test-token", "crm.wecom.aes-key=abcdefghijklmnopqrstuvwxyz0123456789ABCDEFG",
 "logging.level.root=WARN"
})
@AutoConfigureMockMvc
class CustomerIntegrationTest {
 @Autowired MockMvc mvc; @Autowired ObjectMapper json; @Autowired AuthService auth;
 @Autowired Repositories.Customers customers; @Autowired Repositories.Followups followups;
 @Autowired Repositories.Relations relations; @Autowired Repositories.Employees employees;
 @Autowired Repositories.Accounts accounts; @Autowired Repositories.Sessions sessions;
 @Autowired Repositories.Events events; @Autowired Repositories.Settings settings; @Autowired Repositories.SyncRuns runs;
 @Autowired ImportService imports; @Autowired SyncService sync;
 @Autowired CustomerWorkflow workflow; @Autowired CustomerReminders reminders;
 @Autowired CustomerService customerService; @Autowired CustomerProtection protection;
 @MockBean WecomGateway gateway;
 private static String staffHash;
 @BeforeEach void prepare() {
  workflow.notices().deleteAll();workflow.cases().deleteAll();workflow.attachments().deleteAll();workflow.sources().deleteAll();followups.deleteAll();relations.deleteAll();events.deleteAll();customers.deleteAll();sessions.deleteAll();runs.deleteAll();
  accounts.findAll().stream().filter(a->!a.getUsername().equals("admin")).forEach(accounts::delete);
  employees.deleteAll();settings.findAll().stream().filter(s->s.getId().startsWith("rel:")).forEach(settings::delete);
  if(staffHash==null) staffHash=auth.password("TestStaff!2026");
  for(int i=1;i<=2;i++) {
   Employee e=new Employee();e.setId("emp"+i);e.setName("顾问"+i);employees.save(e);
   Account a=new Account();a.setUsername("staff"+i);a.setEmployeeId(e.getId());a.setDisplayName(e.getName());a.setPasswordHash(staffHash);accounts.save(a);
   Customer c=new Customer();c.setId("client"+i);c.setName("客户"+i);c.setOwnerId(e.getId());customers.save(c);
  }
 }
 Cookie login(String username) throws Exception {
  var response=mvc.perform(post("/api/auth/login").header("X-CRM-Request","1").contentType(MediaType.APPLICATION_JSON)
   .content(json.writeValueAsString(Map.of("username",username,"password",username.equals("admin")?"TestAdmin!2026":"TestStaff!2026"))))
   .andExpect(status().isOk()).andExpect(jsonPath("$.passwordHash").doesNotExist()).andReturn().getResponse();
  String cookie=response.getHeader("Set-Cookie");assertThat(cookie).contains("HttpOnly","SameSite=Strict");
  return new Cookie("CRM_SESSION",cookie.substring("CRM_SESSION=".length()).split(";")[0]);
 }
 CustomerService.Edit edit(Customer c,String owner,String demand) {return new CustomerService.Edit(c.getVersion(),owner,c.getStage(),c.getCategory(),c.getIntent(),c.getPhone(),demand,c.getNextFollowupAt());}
 CustomerService.Report reportInput(String phone) {return new CustomerService.Report("报备测试客户","",phone,"湖南长沙某镇某村三组",new java.math.BigDecimal("200.5"),"新建","明年春天","预算待沟通");}
 Account staff(int n){return accounts.findById("staff"+n).orElseThrow();}
 @Test void reportingValidatesDeduplicatesAndLocksOwnership() throws Exception {
  Cookie cookie=login("staff1");
  String body=json.writeValueAsString(reportInput("+86 138-0000-1234"));
  var result=mvc.perform(post("/api/customers").cookie(cookie).header("X-CRM-Request","1").contentType(MediaType.APPLICATION_JSON).content(body))
   .andExpect(status().isOk()).andExpect(jsonPath("$.ownerUsername").value("staff1")).andExpect(jsonPath("$.phone").value("13800001234")).andExpect(jsonPath("$.phoneKey").doesNotExist()).andReturn();
  Customer c=json.readValue(result.getResponse().getContentAsString(),Customer.class);
  assertThat(c.getCustomerNumber()).startsWith("KH");assertThat(c.getProtectionExpiresAt()).isAfter(Instant.now().plusSeconds(89*86400));
  assertThatThrownBy(()->customerService.report(staff(2),reportInput("13800001234"))).isInstanceOf(ApiException.class).hasMessageContaining("重复");
  assertThatThrownBy(()->customerService.report(staff(1),reportInput("123"))).isInstanceOf(ApiException.class);
  assertThatThrownBy(()->customerService.report(staff(1),new CustomerService.Report("","","13800001235","",null,"其他","",""))).isInstanceOf(ApiException.class);
  assertThatThrownBy(()->customerService.visible(staff(2),c.getId())).isInstanceOf(ApiException.class);
  assertThatThrownBy(()->customerService.edit(accounts.findById("admin").orElseThrow(),c.getId(),edit(c,"emp2","转移"))).isInstanceOf(ApiException.class).hasMessageContaining("不能在此调整负责人");
  Customer adminLead=customerService.report(accounts.findById("admin").orElseThrow(),reportInput("13800001235"));
  assertThat(adminLead.getOwnerUsername()).isEqualTo("admin");
 }
 @Test void effectiveActivitiesRenewButProfileEditsDoNot() {
  Account a=staff(1);Customer c=customerService.report(a,reportInput("13800001234"));
  Instant expires=customers.findById(c.getId()).orElseThrow().getProtectionExpiresAt();c=customerService.editReport(a,c.getId(),c.getVersion(),reportInput("13800001234"));
  assertThat(c.getProtectionExpiresAt()).isEqualTo(expires);
  Attachment evidence=workflow.upload(a,c.getId(),new CustomerWorkflow.FileInput("报价.pdf",Base64.getEncoder().encodeToString("%PDF-1.7 test".getBytes())));c=customers.findById(c.getId()).orElseThrow();
  for(String kind:CustomerService.ACTIVITIES) {
   c.setProtectionExpiresAt(Instant.now().plusSeconds(60));c=customers.saveAndFlush(c);
   c=customerService.follow(a,c.getId(),new CustomerService.Follow(c.getVersion(),"已完成"+kind,"following",Instant.now().plusSeconds(86400),kind,"contacted",List.of(evidence.getId())));
   assertThat(c.getProtectionExpiresAt()).isEqualTo(c.getLastFollowupAt().plus(CustomerProtection.PERIOD));
  }
  Customer latest=c;
  assertThatThrownBy(()->customerService.follow(a,latest.getId(),new CustomerService.Follow(latest.getVersion()," ","paused",null,"quotation"))).isInstanceOf(ApiException.class);
  assertThatThrownBy(()->customerService.follow(a,latest.getId(),new CustomerService.Follow(latest.getVersion(),"修改资料","paused",null,"profile"))).isInstanceOf(ApiException.class);
 }
 @Test void expirationIsEnforcedBeforeJobAndClaimPreservesHistory() {
  Customer c=customerService.report(staff(1),reportInput("13800001234"));c.setProtectionExpiresAt(Instant.now().minusSeconds(1));c=customers.saveAndFlush(c);Customer expired=c;
  assertThat(customerService.list(staff(2),new CustomerService.Filter(null,null,null,null,null,"pool",null),1,20).getContent()).extracting(Customer::getId).contains(c.getId());
  assertThat(customerService.list(staff(1),null,1,20).getContent()).extracting(Customer::getId).doesNotContain(c.getId());
  assertThatThrownBy(()->customerService.follow(staff(1),expired.getId(),new CustomerService.Follow(expired.getVersion(),"过期后补跟进","paused",null))).isInstanceOf(ApiException.class).hasMessageContaining("公海");
  protection.releaseExpired();Customer pool=customers.findById(c.getId()).orElseThrow();assertThat(pool.getOwnerUsername()).isNull();assertThat(pool.getOwnerId()).isNull();
  Customer claimed=customerService.claim(staff(2),pool.getId(),pool.getVersion());assertThat(claimed.getCustomerNumber()).isEqualTo(c.getCustomerNumber());assertThat(claimed.getOwnerUsername()).isEqualTo("staff2");
  assertThat(followups.findByCustomerIdOrderByCreatedAtDescIdDesc(c.getId())).extracting(Followup::getKind).contains("report","release","claim");
  assertThatThrownBy(()->customerService.visible(staff(1),claimed.getId())).isInstanceOf(ApiException.class);
  assertThatThrownBy(()->customerService.claim(staff(1),pool.getId(),pool.getVersion())).isInstanceOf(ApiException.class);
 }
 @Test void concurrentReportsAndClaimsHaveOnlyOneWinner() throws Exception {
  var executor=java.util.concurrent.Executors.newFixedThreadPool(2);
  try {
   var start=new java.util.concurrent.CountDownLatch(1);Account a=staff(1),b=staff(2);
   java.util.concurrent.Callable<Boolean> first=()->{start.await();try{customerService.report(a,reportInput("13800001234"));return true;}catch(ApiException e){assertThat(e.status).isEqualTo(409);return false;}};
   java.util.concurrent.Callable<Boolean> second=()->{start.await();try{customerService.report(b,reportInput("13800001234"));return true;}catch(ApiException e){assertThat(e.status).isEqualTo(409);return false;}};
   var f1=executor.submit(first);var f2=executor.submit(second);start.countDown();assertThat(List.of(f1.get(),f2.get())).containsExactlyInAnyOrder(true,false);
   Customer c=customers.findByPhoneKeyOrPhone("13800001234","13800001234").get(0);c.setProtectionExpiresAt(Instant.now().minusSeconds(1));Customer pool=customers.saveAndFlush(c);
   var claimStart=new java.util.concurrent.CountDownLatch(1);
   var claims=new ArrayList<java.util.concurrent.Future<Boolean>>();
   for(Account actor:List.of(a,b)) claims.add(executor.submit(()->{claimStart.await();try{customerService.claim(actor,pool.getId(),pool.getVersion());return true;}catch(ApiException e){assertThat(e.status).isEqualTo(409);return false;}}));
   claimStart.countDown();assertThat(List.of(claims.get(0).get(),claims.get(1).get())).containsExactlyInAnyOrder(true,false);
  } finally {executor.shutdownNow();}
 }
 @Test void authenticationAndCsrfAreRequired() throws Exception {
  mvc.perform(get("/api/customers")).andExpect(status().isUnauthorized());
  mvc.perform(post("/api/auth/login").contentType(MediaType.APPLICATION_JSON).content("{}" )).andExpect(status().isForbidden());
  Cookie cookie=login("staff1");assertThat(sessions.existsById(AuthService.hash(cookie.getValue()))).isTrue();
  mvc.perform(post("/api/auth/logout").cookie(cookie).header("X-CRM-Request","1")).andExpect(status().isOk());
  mvc.perform(get("/api/auth/me").cookie(cookie)).andExpect(status().isUnauthorized());
 }
 @Test void managedAccountsRejectStandalonePasswordAndOldCookie() throws Exception {
  // standalone 开启（独立应用形态）下 managed 账号的拒绝语义：与 ManagementBridgeTest 的"默认关闭整体 403"互补
  Cookie cookie=login("staff1");
  Account a=staff(1);a.setAuthMode("managed");a.setManagementUserId(99L);accounts.save(a);
  // 统一账号（managed）不能使用独立密码登录
  mvc.perform(post("/api/auth/login").header("X-CRM-Request","1").contentType(MediaType.APPLICATION_JSON).content(json.writeValueAsString(Map.of("username","staff1","password","TestStaff!2026")))).andExpect(status().isUnauthorized());
  // 切换为 managed 前签发的独立会话同样立即失效
  mvc.perform(get("/api/auth/me").cookie(cookie)).andExpect(status().isUnauthorized());
 }
 @Test void staffScopeAppliesToListDetailMutationAndExport() throws Exception {
  Cookie cookie=login("staff1");
  mvc.perform(get("/api/customers").cookie(cookie)).andExpect(status().isOk()).andExpect(jsonPath("$.totalElements").value(1)).andExpect(jsonPath("$.content[0].id").value("client1"));
  mvc.perform(get("/api/customers/client2").cookie(cookie)).andExpect(status().isForbidden());
  mvc.perform(patch("/api/customers/client2").cookie(cookie).header("X-CRM-Request","1").contentType(MediaType.APPLICATION_JSON).content(json.writeValueAsString(edit(customers.findById("client2").get(),"emp2","越权")))).andExpect(status().isForbidden());
  mvc.perform(get("/api/customers").param("ownerId","emp2").cookie(cookie)).andExpect(jsonPath("$.totalElements").value(0));
  String csv=mvc.perform(get("/api/export/customers").cookie(cookie)).andExpect(status().isOk()).andReturn().getResponse().getContentAsString();
  assertThat(csv).contains("client1").doesNotContain("client2");
  mvc.perform(get("/api/accounts").cookie(cookie)).andExpect(status().isForbidden());
  mvc.perform(post("/api/sync").cookie(cookie).header("X-CRM-Request","1")).andExpect(status().isForbidden());
 }
 @Test void followupValidatesSchedulePersistsHistoryAndRejectsStaleEdits() throws Exception {
  Cookie cookie=login("staff1");Customer c=customers.findById("client1").get();
  String url="/api/customers/client1/followups";
  mvc.perform(post(url).cookie(cookie).header("X-CRM-Request","1").contentType(MediaType.APPLICATION_JSON).content(json.writeValueAsString(new CustomerService.Follow(c.getVersion(),"联系过客户","following",null)))).andExpect(status().isBadRequest());
  mvc.perform(post(url).cookie(cookie).header("X-CRM-Request","1").contentType(MediaType.APPLICATION_JSON).content(json.writeValueAsString(new CustomerService.Follow(c.getVersion(),"确认预算，下周沟通","following",Instant.now().plusSeconds(86400))))).andExpect(status().isOk());
  assertThat(followups.findByCustomerIdOrderByCreatedAtDescIdDesc(c.getId())).hasSize(1);
  mvc.perform(patch("/api/customers/client1").cookie(cookie).header("X-CRM-Request","1").contentType(MediaType.APPLICATION_JSON).content(json.writeValueAsString(edit(c,"emp1","旧表单")))).andExpect(status().isConflict());
  Customer updated=customers.findById(c.getId()).get();assertThat(updated.getLastFollowupAt()).isNotNull();
  mvc.perform(post(url).cookie(cookie).header("X-CRM-Request","1").contentType(MediaType.APPLICATION_JSON).content(json.writeValueAsString(new CustomerService.Follow(updated.getVersion(),"已成交","won",Instant.now().plusSeconds(86400))))).andExpect(status().isBadRequest());
  assertThat(followups.findByCustomerIdOrderByCreatedAtDescIdDesc(c.getId())).hasSize(1);
 }
 @Test void onlyAdminCanAssignAndPreviousOwnerLosesAccess() throws Exception {
  Customer c=customers.findById("client1").get();String body=json.writeValueAsString(edit(c,"emp2","新需求"));
  Cookie staff=login("staff1"),admin=login("admin");
  mvc.perform(patch("/api/customers/client1").cookie(staff).header("X-CRM-Request","1").contentType(MediaType.APPLICATION_JSON).content(body)).andExpect(status().isForbidden());
  mvc.perform(patch("/api/customers/client1").cookie(admin).header("X-CRM-Request","1").contentType(MediaType.APPLICATION_JSON).content(body)).andExpect(status().isOk());
  mvc.perform(get("/api/customers/client1").cookie(staff)).andExpect(status().isForbidden());
  assertThat(followups.findByCustomerIdOrderByCreatedAtDescIdDesc(c.getId()).get(0).getKind()).isEqualTo("profile");
  assertThat(customers.findById(c.getId()).get().getLastFollowupAt()).isNull();
 }
 @Test void overviewAndFiltersUseTheSameOwnerScope() throws Exception {
  for(Customer c:customers.findAll()) {c.setNextFollowupAt(Instant.now().minusSeconds(60));customers.save(c);}
  Cookie staff=login("staff1");
  mvc.perform(get("/api/overview").cookie(staff)).andExpect(jsonPath("$.all").value(1)).andExpect(jsonPath("$.overdue").value(1));
  mvc.perform(get("/api/customers").cookie(staff).param("queue","overdue").param("keyword","客户1")).andExpect(jsonPath("$.totalElements").value(1));
  mvc.perform(get("/api/customers").cookie(staff).param("keyword","%")).andExpect(jsonPath("$.totalElements").value(0));
 }
 @Test void accountDisableRevokesExistingSession() throws Exception {
  Cookie staff=login("staff1"),admin=login("admin");
  mvc.perform(post("/api/accounts").cookie(admin).header("X-CRM-Request","1").contentType(MediaType.APPLICATION_JSON)
   .content(json.writeValueAsString(new ApiController.AccountInput("staff1","顾问1","emp1","staff",false,""))))
   .andExpect(status().isOk()).andExpect(jsonPath("$.passwordHash").doesNotExist());
  mvc.perform(get("/api/auth/me").cookie(staff)).andExpect(status().isUnauthorized());
 }
 @Test void exportsEscapeMultilineSpreadsheetFormulas() throws Exception {
  Customer c=customers.findById("client1").get();c.setDemand("=1+1\nsecond line");customers.save(c);
  String csv=mvc.perform(get("/api/export/customers").cookie(login("staff1"))).andExpect(status().isOk()).andReturn().getResponse().getContentAsString();
  assertThat(csv).contains("\"'=1+1\nsecond line\"");
 }
 Event event(String id,String customer,String employee,String type,long time) {
  Event e=new Event();e.setId(AuthService.hash(id));e.setCustomerId(customer);e.setEmployeeId(employee);e.setChangeType(type);e.setEventTime(time);return e;
 }
 @Test void duplicateCallbacksAndSeveralEmployeesProduceOneCustomer() {
  long now=Instant.now().getEpochSecond();Event e=event("first","new-client","emp1","add_external_contact",now);
  assertThat(imports.accept(e)).isTrue();assertThat(imports.accept(e)).isFalse();
  imports.accept(event("second","new-client","emp2","add_external_contact",now));
  assertThat(customers.count()).isEqualTo(3);assertThat(relations.findByExternalUserid("new-client")).hasSize(2);assertThat(events.count()).isEqualTo(2);
 }
 @Test void deletionAndReaddDoNotEraseCustomerOrHistoryAndRejectOldEvents() {
  long now=Instant.now().getEpochSecond();
  imports.accept(event("add","client1","emp1","add_external_contact",now-100));
  imports.accept(event("delete","client1","emp1","del_follow_user",now-50));
  imports.accept(event("old-add","client1","emp1","add_external_contact",now-70));
  assertThat(relations.findById("client1&emp1").get().getStatus()).isEqualTo(1);assertThat(customers.existsById("client1")).isTrue();
  imports.accept(event("readd","client1","emp1","add_external_contact",now));
  assertThat(relations.findById("client1&emp1").get().getStatus()).isZero();assertThat(events.count()).isEqualTo(4);
 }
 @Test void snapshotPreservesManagedFieldsAndDoesNotUndoNewerDeletion() {
  long now=Instant.now().getEpochSecond();Customer c=customers.findById("client1").get();c.setStage("won");c.setDemand("人工资料");customers.save(c);
  imports.accept(event("delete","client1","emp1","del_follow_user",now));
  imports.snapshot(new WecomGateway.Snapshot("client1","更新昵称","",1,List.of(new WecomGateway.Relation("emp1",now-100,""))),now-10);
  Customer saved=customers.findById("client1").get();assertThat(saved.getName()).isEqualTo("更新昵称");assertThat(saved.getStage()).isEqualTo("won");assertThat(saved.getDemand()).isEqualTo("人工资料");assertThat(saved.getOwnerId()).isEqualTo("emp1");
  assertThat(relations.findById("client1&emp1").get().getStatus()).isEqualTo(1);
 }
 @Test void failedEventIsPersistedAndCanBeRetried() {
  Event e=event("retry","new-client","emp1","add_external_contact",Instant.now().getEpochSecond());imports.accept(e);
  when(gateway.contact("new-client")).thenThrow(new ApiException(502,"企微接口暂不可用"));sync.events();
  Event pending=events.findById(e.getId()).get();assertThat(pending.getStatus()).isEqualTo("pending");assertThat(pending.getAttempts()).isEqualTo(1);assertThat(pending.getNextAttemptAt()).isAfter(Instant.now());
  doReturn(new WecomGateway.Snapshot("new-client","补同步成功","",1,List.of(new WecomGateway.Relation("emp1",Instant.now().getEpochSecond(),"")))).when(gateway).contact("new-client");
  sync.retry(e.getId());sync.events();assertThat(events.findById(e.getId()).get().getStatus()).isEqualTo("done");assertThat(customers.findById("new-client").get().getName()).isEqualTo("补同步成功");
 }
 @Test void failedFullSyncDoesNotReconcileUnfinishedEmployee() {
  long now=Instant.now().getEpochSecond();imports.accept(event("existing","client1","emp1","add_external_contact",now-60));
  when(gateway.configured()).thenReturn(true);when(gateway.employees()).thenReturn(List.of("emp1"));when(gateway.employeeName("emp1")).thenReturn("顾问1");
  when(gateway.customerIds("emp1")).thenReturn(List.of("bad-client"));when(gateway.contact("bad-client")).thenThrow(new ApiException(502,"读取失败"));
  SyncRun run=sync.enqueue();sync.sync();assertThat(runs.findById(run.getId()).get().getStatus()).isEqualTo("failed");assertThat(relations.findById("client1&emp1").get().getStatus()).isZero();
 }
 @Test void reconciliationPreservesKnownDeletionReason() {
  long now=Instant.now().getEpochSecond();imports.accept(event("removed","client1","emp1","del_follow_user",now-60));
  imports.reconcileEmployee("emp1",Set.of(),now);
  assertThat(relations.findById("client1&emp1").get().getStatus()).isEqualTo(1);
 }
 String xml(String type,String customer) {return "<xml><ToUserName>ww-test</ToUserName><CreateTime>"+Instant.now().getEpochSecond()+"</CreateTime><Event>change_external_contact</Event><ChangeType>"+type+"</ChangeType><UserID>emp1</UserID><ExternalUserID>"+customer+"</ExternalUserID></xml>";}
 String part(String xml,String tag) throws Exception {return DocumentBuilderFactory.newInstance().newDocumentBuilder().parse(new InputSource(new StringReader(xml))).getElementsByTagName(tag).item(0).getTextContent();}
 ResultActions callback(String plain,String corp,boolean badSignature) throws Exception {
  String encrypted=new IYqueCryptUtil("test-token","abcdefghijklmnopqrstuvwxyz0123456789ABCDEFG",corp).encrypt(plain);
  return mvc.perform(post("/iycallback/handle").param("msg_signature",badSignature?"0".repeat(40):part(encrypted,"MsgSignature")).param("timestamp",part(encrypted,"TimeStamp")).param("nonce",part(encrypted,"Nonce")).contentType(MediaType.APPLICATION_XML).content(encrypted));
 }
 @Test void signedCallbackWithoutStateImportsAndDeduplicates() throws Exception {
  String plain=xml("add_external_contact","real-callback");callback(plain,"ww-test",false).andExpect(status().isOk()).andExpect(content().string("success"));
  callback(plain,"ww-test",false).andExpect(status().isOk());assertThat(events.count()).isEqualTo(1);assertThat(customers.existsById("real-callback")).isTrue();
 }
 @Test void callbackRejectsInvalidSignatureWrongCorpAndXmlEntities() throws Exception {
  callback(xml("add_external_contact","attack"),"ww-test",true).andExpect(status().isForbidden());
  callback(xml("add_external_contact","attack"),"ww-other",false).andExpect(status().isForbidden());
  callback("<!DOCTYPE xml [<!ENTITY x SYSTEM 'file:///not-a-real-file'>]>"+xml("add_external_contact","attack"),"ww-test",false).andExpect(status().isBadRequest());
  assertThat(events.count()).isZero();
 }
 @Test void callbackUrlChallengeWorksAndHalfAddDoesNotBecomeCustomer() throws Exception {
  String encrypted=new IYqueCryptUtil("test-token","abcdefghijklmnopqrstuvwxyz0123456789ABCDEFG","ww-test").encrypt("challenge-ok");
  mvc.perform(get("/iycallback/handle").param("msg_signature",part(encrypted,"MsgSignature")).param("timestamp",part(encrypted,"TimeStamp")).param("nonce",part(encrypted,"Nonce")).param("echostr",part(encrypted,"Encrypt"))).andExpect(status().isOk()).andExpect(content().string("challenge-ok"));
  callback(xml("add_half_external_contact","half-client"),"ww-test",false).andExpect(status().isOk());assertThat(customers.existsById("half-client")).isFalse();
 }

 Account admin(){return accounts.findById("admin").orElseThrow();}
 Customer fresh(Customer c){return customers.findById(c.getId()).orElseThrow();}
 Attachment evidence(Account a,Customer c){return workflow.upload(a,c.getId(),new CustomerWorkflow.FileInput("凭证.pdf",Base64.getEncoder().encodeToString("%PDF-1.7 test".getBytes())));}
 @Test void automaticSourcesAreIdempotentAndCannotStealProtectedMaster(){
  Customer main=customerService.report(staff(1),reportInput("13800001234"));Instant deadline=fresh(main).getProtectionExpiresAt();
  Customer incoming=workflow.ingest(new CustomerWorkflow.Ingest("88","小程序姓名","13800001234","wx","咨询"));
  assertThat(incoming.getLifecycle()).isEqualTo("pending");assertThat(incoming.getProtectionExpiresAt()).isNull();
  assertThat(workflow.ingest(new CustomerWorkflow.Ingest("88","新昵称","13800001234","wx","再次咨询")).getId()).isEqualTo(incoming.getId());
  main=workflow.merge(admin(),incoming.getId(),new CustomerWorkflow.Merge(fresh(incoming).getVersion(),main.getId(),fresh(main).getVersion(),"地址及本人确认一致"));
  Customer linked=workflow.ingest(new CustomerWorkflow.Ingest("88","第三次昵称","13800001234","wx","再询价"));
  assertThat(linked.getId()).isEqualTo(main.getId());assertThat(linked.getOwnerUsername()).isEqualTo("staff1");assertThat(linked.getName()).isEqualTo("报备测试客户");assertThat(linked.getProtectionExpiresAt()).isEqualTo(deadline);
  assertThat(customerService.list(admin(),null,1,100).getContent()).extracting(Customer::getId).doesNotContain(incoming.getId());
  String sourceId=workflow.sourceKey("miniprogram","88");workflow.unlink(admin(),main.getId(),sourceId,fresh(main).getVersion(),"核实为同电话不同户");
  assertThat(fresh(incoming).getMergedInto()).isNull();assertThat(fresh(incoming).getLifecycle()).isEqualTo("pending");assertThat(fresh(main).getProtectionExpiresAt()).isEqualTo(deadline);
 }
 @Test void partnerIdentityIngestIsLabelledForSupervisorWithoutRelabellingExisting(){
  Customer partner=workflow.ingest(new CustomerWorkflow.Ingest("700","设计师账号","13800007000","","合作咨询","7","customer_service",null,List.of()," 设计师 "));
  assertThat(partner.getCategory()).isEqualTo("合作咨询");assertThat(partner.getLifecycle()).isEqualTo("pending");assertThat(partner.getOwnerUsername()).isNull();
  Customer owner=workflow.ingest(new CustomerWorkflow.Ingest("701","业主","13800007001","","建房咨询","8","customer_service",null,List.of(),"乡墅业主"));
  assertThat(owner.getCategory()).isEqualTo("待确认");
  Customer pending=workflow.ingest(new CustomerWorkflow.Ingest("702","未选身份","13800007002","","咨询"));
  assertThat(pending.getCategory()).isEqualTo("待确认");
  // 已建档案再次接入不改人工维护的分类
  assertThat(workflow.ingest(new CustomerWorkflow.Ingest("701","业主","13800007001","","再次咨询","8","customer_service",null,List.of(),"设计师")).getCategory()).isEqualTo("待确认");
 }
 @Test void supervisorCompletingPendingDoesNotAcquireItsOwnership(){
  Customer c=workflow.ingest(new CustomerWorkflow.Ingest("89","客户","13800001234","","咨询"));
  c=workflow.transfer(admin(),c.getId(),new CustomerWorkflow.Transfer(c.getVersion(),"staff1","分配接待",false));
  c=customerService.editReport(admin(),c.getId(),c.getVersion(),reportInput("13800001234"));assertThat(c.getOwnerUsername()).isEqualTo("staff1");assertThat(c.getLifecycle()).isEqualTo("protected");
 }
 @Test void noAnswerAndUploadsDoNotRenewAndQuotationRequiresEvidence(){
  Customer c=customerService.report(staff(1),reportInput("13800001234"));Instant deadline=fresh(c).getProtectionExpiresAt();
  c=customerService.follow(staff(1),c.getId(),new CustomerService.Follow(c.getVersion(),"未接通","following",Instant.now().plusSeconds(86400),"communication","no_answer",List.of()));assertThat(c.getProtectionExpiresAt()).isEqualTo(deadline);assertThat(c.getLastFollowupAt()).isNull();
  Customer latest=c;assertThatThrownBy(()->customerService.follow(staff(1),latest.getId(),new CustomerService.Follow(latest.getVersion(),"报价","paused",null,"quotation"))).hasMessageContaining("附件");
  Attachment file=evidence(staff(1),c);assertThat(fresh(c).getProtectionExpiresAt()).isEqualTo(deadline);
  c=customerService.follow(staff(1),c.getId(),new CustomerService.Follow(fresh(c).getVersion(),"已交付报价","paused",null,"quotation","contacted",List.of(file.getId())));assertThat(c.getProtectionExpiresAt()).isEqualTo(c.getLastFollowupAt().plus(CustomerProtection.PERIOD));
 }
 @Test void dealNeedsApprovalAndApprovedArchiveCanOnlyBeRestoredBySupervisor(){
  Customer c=customerService.report(staff(1),reportInput("13800001234"));Attachment file=evidence(staff(1),c);String id=c.getId();
  ReviewCase r=workflow.request(staff(1),id,new CustomerWorkflow.CaseInput("deal","合同已确认",List.of(file.getId())));assertThat(fresh(c).getLifecycle()).isEqualTo("protected");
  c=workflow.decide(admin(),r.getId(),new CustomerWorkflow.Decision(fresh(c).getVersion(),true,"核验合同",null));assertThat(c.getLifecycle()).isEqualTo("won");assertThat(c.getProtectionExpiresAt()).isNull();assertThat(CustomerProtection.inPool(c)).isFalse();
  Customer won=c;assertThatThrownBy(()->workflow.restore(staff(1),id,won.getVersion(),"恢复")).isInstanceOf(ApiException.class);
  c=workflow.restore(admin(),id,c.getVersion(),"客户新增另一项目");assertThat(c.getLifecycle()).isEqualTo("pending");
  r=workflow.request(staff(1),id,new CustomerWorkflow.CaseInput("archive","明确拒绝",List.of()));c=workflow.decide(admin(),r.getId(),new CustomerWorkflow.Decision(fresh(c).getVersion(),true,"核实无需联系",null));assertThat(c.getLifecycle()).isEqualTo("archived");assertThat(CustomerProtection.inPool(c)).isFalse();
 }
 @Test void staleDealApplicationCannotBeApprovedAfterOwnershipChanges(){
  Customer c=customerService.report(staff(1),reportInput("13800001234"));Attachment file=evidence(staff(1),c);ReviewCase r=workflow.request(staff(1),c.getId(),new CustomerWorkflow.CaseInput("deal","申请",List.of(file.getId())));
  c=workflow.transfer(admin(),c.getId(),new CustomerWorkflow.Transfer(fresh(c).getVersion(),"staff2","交接",false));Customer moved=c;
  assertThatThrownBy(()->workflow.decide(admin(),r.getId(),new CustomerWorkflow.Decision(moved.getVersion(),true,"核实",null))).hasMessageContaining("归属已变化");
 }
 @Test void channelAccountsAreIsolatedAndTransfersKeepDeadline() throws Exception{
  Account channel=new Account();channel.setUsername("channel");channel.setDisplayName("渠道");channel=auth.saveAccount(admin(),channel,"TestStaff!2026");Cookie cookie=login("channel");
  mvc.perform(get("/api/employees").cookie(cookie)).andExpect(status().isOk()).andExpect(content().json("[]"));
  mvc.perform(get("/api/customers").cookie(cookie)).andExpect(jsonPath("$.totalElements").value(0));
  Customer c=customerService.report(channel,reportInput("13800001234"));Instant deadline=fresh(c).getProtectionExpiresAt();c=workflow.transfer(admin(),c.getId(),new CustomerWorkflow.Transfer(c.getVersion(),"staff2","渠道转交顾问",false));assertThat(c.getProtectionExpiresAt()).isEqualTo(deadline);
  mvc.perform(get("/api/customers/"+c.getId()).cookie(cookie)).andExpect(status().isForbidden());
  Account stopped=staff(2);stopped.setEnabled(false);accounts.save(stopped);@SuppressWarnings("unchecked") List<Customer> handover=(List<Customer>)workflow.workbench(admin()).get("handover");assertThat(handover).extracting(Customer::getId).contains(c.getId());
 }
 @Test void privateAttachmentsCannotBeReadOrReusedAcrossCustomers() throws Exception{
  Customer c=customerService.report(staff(1),reportInput("13800001234"));Attachment file=evidence(staff(1),c);
  mvc.perform(get("/api/customers/"+c.getId()+"/attachments/"+file.getId()).cookie(login("staff2"))).andExpect(status().isForbidden());
  mvc.perform(get("/api/customers/"+c.getId()+"/attachments/"+file.getId()).cookie(login("staff1"))).andExpect(status().isOk()).andExpect(header().string("X-Content-Type-Options","nosniff"));
  Customer other=customerService.report(staff(2),reportInput("13800001235"));assertThatThrownBy(()->customerService.follow(staff(2),other.getId(),new CustomerService.Follow(other.getVersion(),"复用附件","paused",null,"quotation","contacted",List.of(file.getId())))).hasMessageContaining("其他客户");
  assertThatThrownBy(()->workflow.upload(staff(1),c.getId(),new CustomerWorkflow.FileInput("evil.html",Base64.getEncoder().encodeToString("<script>".getBytes())))).hasMessageContaining("仅支持");
 }
 @Test void historicalActivationRunsOnceAndKeepsOriginalHistory(){
  Customer c=customers.findById("client1").orElseThrow();c=workflow.transfer(admin(),c.getId(),new CustomerWorkflow.Transfer(c.getVersion(),"staff1","核实原员工",false));
  c=customerService.editReport(admin(),c.getId(),c.getVersion(),reportInput("13800001234"));assertThat(c.getLifecycle()).isEqualTo("legacy");assertThat(c.getProtectionExpiresAt()).isNull();c=workflow.activate(admin(),c.getId(),c.getVersion());Customer active=c;
  assertThatThrownBy(()->workflow.activate(admin(),active.getId(),active.getVersion())).hasMessageContaining("不能重复");assertThat(followups.findByCustomerIdOrderByCreatedAtDescIdDesc(c.getId())).extracting(Followup::getKind).contains("transfer","profile","activate");
 }
 @Test void invalidatingFollowupRecomputesWithinCurrentClaimAndRemindersDeduplicate(){
  Customer c=customerService.report(staff(1),reportInput("13800001234"));c.setProtectionStartedAt(Instant.now().minusSeconds(10*86400));c.setProtectionExpiresAt(Instant.now().plusSeconds(3*86400));c.setNextFollowupAt(Instant.now().minusSeconds(100));c=customers.saveAndFlush(c);
  reminders.createReminders();long count=workflow.notices().count();reminders.createReminders();assertThat(workflow.notices().count()).isEqualTo(count);
  c=customerService.follow(staff(1),c.getId(),new CustomerService.Follow(c.getVersion(),"联系","following",Instant.now().plusSeconds(86400)));Followup f=followups.findByCustomerIdOrderByCreatedAtDescIdDesc(c.getId()).get(0);
  c=workflow.invalidate(admin(),c.getId(),f.getId(),c.getVersion(),"经核实没有联系成功");assertThat(c.getProtectionExpiresAt()).isEqualTo(c.getProtectionStartedAt().plus(CustomerProtection.PERIOD));assertThat(followups.findById(f.getId()).orElseThrow().getContent()).isEqualTo("联系");
 }
 @Test void protectedCustomerCannotBeMergedIntoAnotherOwnersMaster(){
  Customer first=customerService.report(staff(1),reportInput("13800001234")),second=customerService.report(staff(2),reportInput("13800001235"));
  assertThatThrownBy(()->workflow.merge(admin(),first.getId(),new CustomerWorkflow.Merge(first.getVersion(),second.getId(),second.getVersion(),"尝试转移"))).hasMessageContaining("仍受保护");
 }

 @Test void sharedPhoneRequiresSupervisorApprovalAndPreservesExistingOwner(){
  Customer original=customerService.report(staff(1),reportInput("13800001234"));
  workflow.reportReview(staff(2),new CustomerWorkflow.ReportReview(reportInput("13800001234"),"核实不同镇村、同一家庭联系人"));ReviewCase request=workflow.cases().findTop200ByStatusOrderByCreatedAtAsc("pending").get(0);
  assertThat(customers.findByPhoneKeyOrPhone("13800001234","13800001234")).hasSize(1);
  workflow.decide(admin(),request.getId(),new CustomerWorkflow.Decision(fresh(original).getVersion(),true,"确认不同客户共用电话",null));
  List<Customer> matches=customers.findByPhoneKeyOrPhone("13800001234","13800001234");assertThat(matches).hasSize(2);assertThat(fresh(original).getOwnerUsername()).isEqualTo("staff1");
  Customer separate=matches.stream().filter(c->!c.getId().equals(original.getId())).findFirst().orElseThrow();assertThat(separate.getOwnerUsername()).isEqualTo("staff2");assertThat(separate.getPhoneKey()).isNull();
  separate=customerService.editReport(staff(2),separate.getId(),separate.getVersion(),reportInput("13800001234"));assertThat(separate.getPhoneKey()).isNull();
  assertThatThrownBy(()->workflow.decide(admin(),request.getId(),new CustomerWorkflow.Decision(fresh(original).getVersion(),true,"再次批准",null))).hasMessageContaining("已处理");
 }
 @Test void pushClaimIsExclusiveAndSuccessfulNoticeCannotBeRetried(){
  Customer c=customerService.report(staff(1),reportInput("13800001234"));
  workflow.transfer(admin(),c.getId(),new CustomerWorkflow.Transfer(c.getVersion(),"staff2","交接",false));Notice n=workflow.notices().findTop100ByUsernameOrderByCreatedAtDesc("staff2").get(0);
  assertThat(workflow.notices().claimPush(n.getId())).isEqualTo(1);assertThat(workflow.notices().claimPush(n.getId())).isEqualTo(0);
  assertThat(workflow.notices().finishPush(n.getId(),"sent","")).isEqualTo(1);assertThat(workflow.notices().retryPush(n.getId())).isEqualTo(0);
 }

 @Test void mergeCannotReopenConfirmedCustomersOrLoseTheirReviewHistory(){
  Customer won=customerService.report(staff(1),reportInput("13800001234"));Attachment file=evidence(staff(1),won);ReviewCase r=workflow.request(staff(1),won.getId(),new CustomerWorkflow.CaseInput("deal","已签约",List.of(file.getId())));won=workflow.decide(admin(),r.getId(),new CustomerWorkflow.Decision(fresh(won).getVersion(),true,"核实凭证",null));
  Customer pending=workflow.ingest(new CustomerWorkflow.Ingest("101","咨询","13800001234","","咨询"));Customer confirmed=won;
  assertThatThrownBy(()->workflow.merge(admin(),confirmed.getId(),new CustomerWorkflow.Merge(confirmed.getVersion(),pending.getId(),pending.getVersion(),"归并"))).hasMessageContaining("已确认");
  workflow.merge(admin(),pending.getId(),new CustomerWorkflow.Merge(pending.getVersion(),won.getId(),won.getVersion(),"同一项目咨询"));
  @SuppressWarnings("unchecked") List<ReviewCase> history=(List<ReviewCase>)customerService.detail(admin(),pending.getId()).get("cases");assertThat(history).extracting(ReviewCase::getId).contains(r.getId());assertThat(fresh(won).getLifecycle()).isEqualTo("won");
 }

 @Test void allThreeSourcesShareMasterAndWecomResyncCannotOverwriteManualFields(){
  Customer main=customerService.report(staff(1),reportInput("13800001234"));Instant deadline=fresh(main).getProtectionExpiresAt();
  long now=Instant.now().getEpochSecond();var relation=new WecomGateway.Relation("emp1",now,"trusted-event");imports.snapshot(new WecomGateway.Snapshot("wx-three","企微昵称","",1,List.of(relation)),now);Customer wecom=customers.findById("wx-three").orElseThrow();
  main=workflow.merge(admin(),wecom.getId(),new CustomerWorkflow.Merge(wecom.getVersion(),main.getId(),fresh(main).getVersion(),"核实同一客户"));
  Customer mini=workflow.ingest(new CustomerWorkflow.Ingest("300","小程序昵称","13800001234","","咨询"));main=workflow.merge(admin(),mini.getId(),new CustomerWorkflow.Merge(mini.getVersion(),main.getId(),fresh(main).getVersion(),"核实同一手机号和地址"));
  imports.snapshot(new WecomGateway.Snapshot("wx-three","同步新昵称","",1,List.of(relation)),now+1);
  assertThat(fresh(main).getName()).isEqualTo("报备测试客户");assertThat(fresh(main).getOwnerUsername()).isEqualTo("staff1");assertThat(fresh(main).getProtectionExpiresAt()).isEqualTo(deadline);
  assertThat(workflow.sources().findByCustomerId(main.getId())).extracting(Source::getKind).contains("manual","wecom","miniprogram");
  @SuppressWarnings("unchecked") List<cn.iyque.domain.IYQueCustomerInfo> rels=(List<cn.iyque.domain.IYQueCustomerInfo>)customerService.detail(staff(1),main.getId()).get("relations");assertThat(rels).extracting(cn.iyque.domain.IYQueCustomerInfo::getExternalUserid).contains("wx-three");
 }

 @Test void importedFollowupsAreIdempotentAcrossMergesAndNeverRenewProtection(){
  Customer main=customerService.report(staff(1),reportInput("13800001234"));Instant deadline=fresh(main).getProtectionExpiresAt();Instant old=Instant.now().minusSeconds(86400*5);
  var input=new CustomerWorkflow.Ingest("900","旧线索","13800001234","","咨询","88","mini",old,List.of(new CustomerWorkflow.SourceHistory("99","legacy-1","旧跟进记录",old)));
  Customer mini=workflow.ingest(input);main=workflow.merge(admin(),mini.getId(),new CustomerWorkflow.Merge(mini.getVersion(),main.getId(),fresh(main).getVersion(),"同一客户"));workflow.ingest(input);
  @SuppressWarnings("unchecked") List<Followup> rows=(List<Followup>)customerService.detail(staff(1),main.getId()).get("followups");assertThat(rows.stream().filter(f->"source_history".equals(f.getKind())).toList()).hasSize(1);assertThat(rows.stream().filter(f->"source_history".equals(f.getKind())).findFirst().orElseThrow().isEffective()).isFalse();assertThat(fresh(main).getProtectionExpiresAt()).isEqualTo(deadline);
 }
}
