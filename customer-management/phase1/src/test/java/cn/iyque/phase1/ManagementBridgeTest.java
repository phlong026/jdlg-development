package cn.iyque.phase1;

import static cn.iyque.phase1.Models.*;
import static org.assertj.core.api.Assertions.*;
import static org.springframework.test.web.servlet.request.MockMvcRequestBuilders.*;
import static org.springframework.test.web.servlet.result.MockMvcResultMatchers.*;
import java.net.URI;
import java.nio.charset.StandardCharsets;
import java.security.MessageDigest;
import java.time.*;
import java.util.*;
import javax.servlet.http.Cookie;
import com.fasterxml.jackson.databind.ObjectMapper;
import org.junit.jupiter.api.*;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.boot.test.context.SpringBootTest;
import org.springframework.boot.test.autoconfigure.web.servlet.AutoConfigureMockMvc;
import org.springframework.test.web.servlet.MockMvc;
import org.springframework.test.web.servlet.request.MockHttpServletRequestBuilder;
import org.springframework.http.MediaType;
import org.springframework.jdbc.core.JdbcTemplate;

@SpringBootTest(properties={"spring.datasource.url=jdbc:h2:mem:bridgetest;MODE=MySQL;DB_CLOSE_DELAY=-1","spring.datasource.username=sa","spring.datasource.password=","spring.datasource.driver-class-name=org.h2.Driver","crm.admin-password=TestAdmin!2026","crm.secure-cookie=false","crm.demo=false","crm.jobs.enabled=false","crm.wecom.corp-id=ww-test","crm.bridge.key=0123456789abcdef0123456789abcdef","crm.bridge.key-id=v1","crm.bridge.audience=crm-test","logging.level.root=WARN"})
@AutoConfigureMockMvc
class ManagementBridgeTest {
 static final String KEY="0123456789abcdef0123456789abcdef",P="/internal/management/v1";
 @Autowired MockMvc mvc;@Autowired ObjectMapper json;@Autowired JdbcTemplate jdbc;@Autowired AuthService auth;
 @Autowired Repositories.Accounts accounts;@Autowired Repositories.Customers customers;@Autowired Repositories.Employees employees;@Autowired Repositories.Followups followups;@Autowired Repositories.Relations relations;@Autowired Repositories.Sessions sessions;
 @Autowired CustomerWorkflow workflow;
 @BeforeEach void setup(){
 workflow.notices().deleteAll();workflow.cases().deleteAll();workflow.attachments().deleteAll();workflow.sources().deleteAll();
  followups.deleteAll();relations.deleteAll();customers.deleteAll();sessions.deleteAll();accounts.findAll().stream().filter(a->!a.getUsername().equals("admin")).forEach(accounts::delete);employees.deleteAll();jdbc.update("DELETE FROM crm_bridge_nonce");jdbc.update("DELETE FROM crm_operation_audit");
  for(int i=1;i<=2;i++){Employee e=new Employee();e.setId("e"+i);e.setName("顾问"+i);employees.save(e);Account a=new Account();a.setUsername("managed-"+i);a.setAuthMode("managed");a.setManagementUserId((long)i);a.setDisplayName(e.getName());a.setEmployeeId(e.getId());a.setPasswordHash(auth.password("TestStaff!2026"));accounts.save(a);Customer c=new Customer();c.setId("c"+i);c.setName("客户"+i);c.setOwnerId(e.getId());c.setAddedTimeSource("event");customers.save(c);}
 }
 MockHttpServletRequestBuilder signed(String method,String suffix,String subject,long uid,String body,String nonce,long timestamp)throws Exception{
  URI uri=URI.create(P+suffix);String canonical=String.join("\n","CRM-BRIDGE-V1","v1","crm-test",subject,""+uid,""+timestamp,nonce,method,uri.getRawPath(),Objects.toString(uri.getRawQuery(),""),HexFormat.of().formatHex(MessageDigest.getInstance("SHA-256").digest(body.getBytes(StandardCharsets.UTF_8))));
  return request(org.springframework.http.HttpMethod.valueOf(method),uri).contentType(MediaType.APPLICATION_JSON).content(body).header("X-CRM-Key-Id","v1").header("X-CRM-Audience","crm-test").header("X-CRM-Subject",subject).header("X-CRM-User-Id",uid).header("X-CRM-Timestamp",timestamp).header("X-CRM-Nonce",nonce).header("X-CRM-Signature",ManagementBridgeFilter.signature(KEY,canonical));
 }
 MockHttpServletRequestBuilder signed(String method,String suffix,String subject,long uid,String body)throws Exception{return signed(method,suffix,subject,uid,body,UUID.randomUUID().toString(),Instant.now().getEpochSecond());}
 @Test void loginIdentityMetadataIsPlatformOnlyAndDoesNotExposePasswords()throws Exception {
  mvc.perform(signed("GET","/provision/identity","@platform",9,"")).andExpect(status().isOk()).andExpect(jsonPath("$.corpId").value("ww-test")).andExpect(jsonPath("$.demo").value(false)).andExpect(jsonPath("$.accounts[0].passwordHash").doesNotExist());
  mvc.perform(signed("GET","/provision/identity","managed-1",1,"")).andExpect(status().isForbidden());
 }
 @Test void manualReportingEndpointsAreAvailableThroughSignedGateway()throws Exception {
  String body=json.writeValueAsString(new CustomerService.Report("网关报备客户","wx-test","13800005555","长沙某镇某村",new java.math.BigDecimal("180"),"翻建","2027年春天",""));
  var response=mvc.perform(signed("POST","/customers","managed-1",1,body)).andExpect(status().isOk()).andExpect(jsonPath("$.ownerUsername").value("managed-1")).andReturn();
  Customer c=json.readValue(response.getResponse().getContentAsString(),Customer.class);
  mvc.perform(signed("PATCH","/customers/"+c.getId()+"/report","managed-1",1,"{\"version\":"+c.getVersion()+",\"report\":"+body+"}")).andExpect(status().isOk());
  c=customers.findById(c.getId()).orElseThrow();c.setProtectionExpiresAt(Instant.now().minusSeconds(1));c=customers.saveAndFlush(c);
  mvc.perform(signed("GET","/customers?queue=pool","managed-2",2,"")).andExpect(status().isOk()).andExpect(jsonPath("$.totalElements").value(1));
  mvc.perform(signed("POST","/customers/"+c.getId()+"/claim","managed-2",2,"{\"version\":"+c.getVersion()+"}")).andExpect(status().isOk()).andExpect(jsonPath("$.ownerUsername").value("managed-2"));
 }
 @Test void signatureReplayAndExpiryAreEnforced()throws Exception{
  mvc.perform(get(P+"/customers")).andExpect(status().isUnauthorized());
  String nonce=UUID.randomUUID().toString();long now=Instant.now().getEpochSecond();
  mvc.perform(signed("GET","/customers","managed-1",1,"",nonce,now)).andExpect(status().isOk()).andExpect(jsonPath("$.totalElements").value(1));
  mvc.perform(signed("GET","/customers","managed-1",1,"",nonce,now)).andExpect(status().isUnauthorized());
  mvc.perform(signed("GET","/customers","managed-1",1,"",UUID.randomUUID().toString(),now-120)).andExpect(status().isUnauthorized());
  mvc.perform(signed("GET","/customers","managed-1",1,"").with(r->{r.removeHeader("X-CRM-Audience");r.addHeader("X-CRM-Audience","other");return r;})).andExpect(status().isUnauthorized());
 }
 @Test void sharedSignatureVectorMatchesGo(){
  String canonical=String.join("\n","CRM-BRIDGE-V1","v1","crm-test","managed-1","1","1700000000","abcdefghijklmnopqrstuvwx","GET",P+"/customers","keyword=%E5%AE%A2%E6%88%B7&page=1",AuthService.hash(""));
  assertThat(ManagementBridgeFilter.signature(KEY,canonical)).isEqualTo("FYsUOul5xYtgnJ9Uhk46yrcD_YMSeKmmeGhjtjdlVzY");
 }
 @Test void scopeBindingAndPlatformCapabilityCannotBeForged()throws Exception{
  mvc.perform(signed("GET","/customers/c2","managed-1",1,"")).andExpect(status().isForbidden());
  mvc.perform(signed("GET","/customers","managed-1",2,"")).andExpect(status().isForbidden());
  mvc.perform(signed("GET","/provision/accounts","managed-1",1,"")).andExpect(status().isForbidden()).andExpect(jsonPath("$.code").doesNotExist());
  mvc.perform(signed("GET","/customers","@platform",9,"")).andExpect(status().isForbidden()).andExpect(jsonPath("$.code").doesNotExist());
  mvc.perform(signed("GET","/provision/employees","@platform",9,"")).andExpect(status().isOk());
  mvc.perform(signed("POST","/auth/login","managed-1",1,"{}")).andExpect(status().isForbidden());
 }
 @Test void managedAccountsRejectStandalonePasswordAndOldCookie()throws Exception{
  // 统一部署默认关闭独立登录（crm.standalone-enabled 默认 false）：/api 入口整体 403，独立密码与旧 Cookie 均不可用；
  // standalone 开启时 managed 账号的 401 拒绝语义由 CustomerIntegrationTest.managedAccountsRejectStandalonePasswordAndOldCookie 覆盖
  mvc.perform(post("/api/auth/login").header("X-CRM-Request","1").contentType(MediaType.APPLICATION_JSON).content("{\"username\":\"managed-1\",\"password\":\"TestStaff!2026\"}")).andExpect(status().isForbidden());
  Session s=new Session();s.setId(AuthService.hash("old-cookie"));s.setUsername("managed-1");s.setExpiresAt(Instant.now().plusSeconds(1000));sessions.save(s);
  mvc.perform(get("/api/customers").cookie(new Cookie("CRM_SESSION","old-cookie"))).andExpect(status().isForbidden());
  Account a=accounts.findById("managed-1").orElseThrow();a.setEnabled(false);accounts.save(a);
  mvc.perform(signed("GET","/customers","managed-1",1,"")).andExpect(status().isForbidden()).andExpect(jsonPath("$.code").value("account_inactive"));
 }
 @Test void profileEditPreservesOverdueScheduleAndUsesVersion()throws Exception{
  Customer c=customers.findById("c1").orElseThrow();c.setNextFollowupAt(Instant.now().minusSeconds(3600));customers.saveAndFlush(c);c=customers.findById("c1").orElseThrow();
  String body=json.writeValueAsString(new CustomerService.Edit(c.getVersion(),c.getOwnerId(),c.getStage(),c.getCategory(),c.getIntent(),"13800000000","补充需求",c.getNextFollowupAt()));
  mvc.perform(signed("PATCH","/customers/c1","managed-1",1,body)).andExpect(status().isOk()).andExpect(jsonPath("$.demand").value("补充需求"));
  assertThat(customers.findById("c1").orElseThrow().getNextFollowupAt()).isBefore(Instant.now());
  mvc.perform(signed("PATCH","/customers/c1","managed-1",1,body)).andExpect(status().isConflict());
  var tampered=signed("PATCH","/customers/c1","managed-1",1,body).content(body+" ");mvc.perform(tampered).andExpect(status().isUnauthorized());
 }
 @Test void dashboardUsesKnownAddedTimeAndStaffScope()throws Exception{
  Customer historical=new Customer();historical.setId("old");historical.setOwnerId("e1");historical.setCreatedAt(Instant.now().minusSeconds(86400*7));historical.setAddedTimeSource("relation");customers.save(historical);
  Customer unknown=new Customer();unknown.setId("unknown");unknown.setOwnerId("e1");customers.save(unknown);
  mvc.perform(signed("GET","/dashboard","managed-1",1,"")).andExpect(status().isOk()).andExpect(jsonPath("$.todayAdded").value(1)).andExpect(jsonPath("$.unknownAddedTime").value(1)).andExpect(jsonPath("$.overview.all").value(3)).andExpect(jsonPath("$.team.length()").value(0)).andExpect(jsonPath("$.recent.length()").value(2));
  mvc.perform(signed("GET","/customers?queue=added","managed-1",1,"")).andExpect(status().isOk()).andExpect(jsonPath("$.totalElements").value(1));
  Account a=accounts.findById("managed-1").orElseThrow();a.setRole("admin");accounts.save(a);
  mvc.perform(signed("GET","/dashboard","managed-1",1,"")).andExpect(status().isOk()).andExpect(jsonPath("$.team.length()").value(2)).andExpect(jsonPath("$.overview.all").value(4));
 }
 @Test void queryRawEncodingAndExportRetainPermissions()throws Exception{
  mvc.perform(signed("GET","/customers?keyword=%E5%AE%A2%E6%88%B71","managed-1",1,"")).andExpect(status().isOk()).andExpect(jsonPath("$.totalElements").value(1));
  String csv=mvc.perform(signed("GET","/export/customers","managed-1",1,"")).andExpect(status().isOk()).andReturn().getResponse().getContentAsString();assertThat(csv).contains("c1").doesNotContain("c2");
  assertThat(jdbc.queryForObject("SELECT COUNT(*) FROM crm_operation_audit",Long.class)).isEqualTo(2);
 }
 @Test void provisioningRejectsStaleRevisionsAndKeepsRolesSeparate()throws Exception{
  String body=json.writeValueAsString(new ManagementProvisionController.Input(3L,2,"新顾问","e1","staff",true));
  mvc.perform(signed("POST","/provision/accounts","@platform",9,body)).andExpect(status().isOk()).andExpect(jsonPath("$.authMode").value("managed")).andExpect(jsonPath("$.passwordHash").doesNotExist());
  String stale=json.writeValueAsString(new ManagementProvisionController.Input(3L,1,"旧请求","e1","admin",true));
  mvc.perform(signed("POST","/provision/accounts","@platform",9,stale)).andExpect(status().isConflict());
  assertThat(accounts.findById("managed-3").orElseThrow().getRole()).isEqualTo("staff");
 }

 @Test void sourceCapabilityIsSeparateAndIdempotent() throws Exception{
  String body="{\"externalId\":\"77\",\"name\":\"来源客户\",\"phone\":\"13800007777\"}";
  mvc.perform(signed("POST","/ingest/miniprogram","managed-1",1,body)).andExpect(status().isForbidden());
  mvc.perform(signed("GET","/customers","@source",1,"")).andExpect(status().isForbidden());
  mvc.perform(signed("POST","/ingest/miniprogram","@source",1,body)).andExpect(status().isOk());
  mvc.perform(signed("POST","/ingest/miniprogram","@source",1,body)).andExpect(status().isOk());
  assertThat(workflow.sources().count()).isEqualTo(1);assertThat(customers.count()).isEqualTo(3);
  // Go 来源同步带上账号身份（identityType）：B 端身份新建档案标为合作咨询
  String partner="{\"externalId\":\"78\",\"name\":\"设计师来源\",\"phone\":\"13800007778\",\"identityType\":\"设计师\"}";
  mvc.perform(signed("POST","/ingest/miniprogram","@source",1,partner)).andExpect(status().isOk());
  assertThat(workflow.sourceCustomer("miniprogram","78").getCategory()).isEqualTo("合作咨询");
 }
 @Test void boundDisabledEmployeesStayBlockedButChannelAccountsMayLogin() throws Exception{
  Account channel=accounts.findById("managed-1").orElseThrow();channel.setEmployeeId(null);accounts.save(channel);
  mvc.perform(signed("GET","/employees","managed-1",1,"")).andExpect(status().isOk()).andExpect(content().json("[]"));
  mvc.perform(signed("GET","/customers","managed-1",1,"")).andExpect(jsonPath("$.totalElements").value(0));
  mvc.perform(signed("GET","/workbench","managed-1",1,"")).andExpect(status().isForbidden());
  Employee stopped=employees.findById("e2").orElseThrow();stopped.setActive(false);employees.save(stopped);
  mvc.perform(signed("GET","/customers","managed-2",2,"")).andExpect(status().isForbidden()).andExpect(jsonPath("$.code").value("employee_inactive"));
 }
 @Test void provisioningReplayOfSameRevisionIsIdempotentWithoutSideEffects() throws Exception{
  String body=json.writeValueAsString(new ManagementProvisionController.Input(3L,2,"新顾问","e1","staff",true));
  mvc.perform(signed("POST","/provision/accounts","@platform",9,body)).andExpect(status().isOk());
  Session s=new Session();s.setId(AuthService.hash("replay-cookie"));s.setUsername("managed-3");s.setExpiresAt(Instant.now().plusSeconds(1000));sessions.save(s);
  // B1 补偿重放：同 revision 同参数幂等成功，不删会话、不产生副作用
  mvc.perform(signed("POST","/provision/accounts","@platform",9,body)).andExpect(status().isOk()).andExpect(jsonPath("$.displayName").value("新顾问")).andExpect(jsonPath("$.passwordHash").doesNotExist());
  assertThat(sessions.findById(AuthService.hash("replay-cookie"))).isPresent();
  // 同 revision 参数不同：仍是版本冲突
  String changed=json.writeValueAsString(new ManagementProvisionController.Input(3L,2,"改名字","e1","staff",true));
  mvc.perform(signed("POST","/provision/accounts","@platform",9,changed)).andExpect(status().isConflict());
  // 更低 revision：拒绝
  String stale=json.writeValueAsString(new ManagementProvisionController.Input(3L,1,"更旧版本","e1","staff",true));
  mvc.perform(signed("POST","/provision/accounts","@platform",9,stale)).andExpect(status().isConflict());
 }
}
