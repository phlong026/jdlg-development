package cn.iyque.phase1;
import static cn.iyque.phase1.Models.*;
import java.util.*;
import java.nio.charset.StandardCharsets;
import java.time.Instant;
import javax.servlet.http.*;
import org.springframework.beans.factory.annotation.Value;
import org.springframework.data.domain.Page;
import org.springframework.http.*;
import org.springframework.web.bind.annotation.*;

@RestController @RequestMapping({"/api","/internal/management/v1"})
public class ApiController {
 private final AuthService auth;private final CustomerService customers;private final Repositories.Accounts accounts;private final Repositories.Employees employees;
 private final Repositories.Events events;private final Repositories.SyncRuns runs;private final WecomGateway gateway;private final SyncService sync;
 @Value("${crm.demo:false}") private boolean demo;
 @Value("${crm.wecom.corp-id:}") private String corpId;
 @Value("${crm.wecom.token:}") private String callbackToken;
 @Value("${crm.wecom.aes-key:}") private String callbackAes;
 public ApiController(AuthService a,CustomerService c,Repositories.Accounts ac,Repositories.Employees em,Repositories.Events ev,Repositories.SyncRuns r,WecomGateway g,SyncService s) {
  auth=a;customers=c;accounts=ac;employees=em;events=ev;runs=r;gateway=g;sync=s;
 }
 public record Login(String username,String password) {}
 public record AccountInput(String username,String displayName,String employeeId,String role,boolean enabled,String password) {}
 private Account actor(HttpServletRequest req) {return auth.current(req);}
 @PostMapping("/auth/login") public Account login(@RequestBody Login input,HttpServletRequest req,HttpServletResponse res) {
  return auth.login(input.username(),input.password(),req.getRemoteAddr(),res);
 }
 @GetMapping("/auth/me") public Map<String,Object> me(HttpServletRequest req) {return Map.of("account",actor(req),"demo",demo);}
 @PostMapping("/auth/logout") public void logout(HttpServletRequest req,HttpServletResponse res) {auth.logout(req,res);}
 @GetMapping("/customers") public Page<Customer> list(HttpServletRequest req,CustomerService.Filter filter,@RequestParam(defaultValue="1") int page,@RequestParam(defaultValue="20") int size) {return customers.list(actor(req),filter,page,size);}
 @PostMapping("/customers") public Customer report(HttpServletRequest req,@RequestBody CustomerService.Report input) {return customers.report(actor(req),input);}
 @PostMapping("/customers/{id}/claim") public Customer claim(HttpServletRequest req,@PathVariable String id,@RequestBody CustomerService.Claim input) {return customers.claim(actor(req),id,input.version());}
 public record ReportEdit(Long version,CustomerService.Report report) {}
 @PatchMapping("/customers/{id}/report") public Customer editReport(HttpServletRequest req,@PathVariable String id,@RequestBody ReportEdit input) {
  if(input.report()==null)throw new ApiException(400,"请填写报备资料");return customers.editReport(actor(req),id,input.version(),input.report());
 }
 @GetMapping("/customers/{id}") public Map<String,Object> detail(HttpServletRequest req,@PathVariable String id) {return customers.detail(actor(req),id);}
 @PatchMapping("/customers/{id}") public Customer edit(HttpServletRequest req,@PathVariable String id,@RequestBody CustomerService.Edit input) {return customers.edit(actor(req),id,input);}
 @PostMapping("/customers/{id}/followups") public Customer follow(HttpServletRequest req,@PathVariable String id,@RequestBody CustomerService.Follow input) {return customers.follow(actor(req),id,input);}
 @GetMapping("/overview") public Map<String,Long> overview(HttpServletRequest req) {return customers.overview(actor(req));}
 @GetMapping("/dashboard") public Map<String,Object> dashboard(HttpServletRequest req) {return customers.dashboard(actor(req));}
 @GetMapping("/employees") public List<Employee> employees(HttpServletRequest req) {
  Account a=actor(req);if(a.isAdmin()) return employees.findAll();
  return a.getEmployeeId()==null?List.of():employees.findById(a.getEmployeeId()).map(List::of).orElseGet(List::of);
 }
 @GetMapping("/accounts") public List<Account> accounts(HttpServletRequest req) {auth.admin(actor(req));return accounts.findAll();}
 @PostMapping("/accounts") public Account account(HttpServletRequest req,@RequestBody AccountInput input) {
  Account a=new Account();a.setUsername(input.username());a.setDisplayName(input.displayName());a.setEmployeeId(input.employeeId());a.setRole(input.role());a.setEnabled(input.enabled());
  return auth.saveAccount(actor(req),a,input.password());
 }
 @GetMapping("/sync") public Map<String,Object> syncState(HttpServletRequest req) {
  auth.admin(actor(req));return Map.of("configured",gateway.configured(),"callbackConfigured",!demo&&!corpId.isBlank()&&!callbackToken.isBlank()&&callbackAes.length()==43,"runs",runs.findTop10ByOrderByIdDesc(),"pending",events.countByStatus("pending"),"failed",events.countByStatus("failed"),"events",events.findTop50ByStatusNotOrderByCreatedAtDesc("done"));
 }
 @PostMapping("/sync") public SyncRun sync(HttpServletRequest req) {auth.admin(actor(req));return sync.enqueue();}
 @PostMapping("/sync/events/{id}/retry") public void retry(HttpServletRequest req,@PathVariable String id) {auth.admin(actor(req));sync.retry(id);}
 @GetMapping("/export/customers") public ResponseEntity<byte[]> export(HttpServletRequest req,CustomerService.Filter filter) {
  List<Customer> rows=customers.export(actor(req),filter);
  StringBuilder csv=new StringBuilder("\uFEFF客户,档案ID,负责人,状态,咨询类型,意向,电话,需求,添加时间,下次跟进,客户编号,归属账号,微信,镇村地址,建房面积,建房需求,预计建房时间,备注,保护到期,公海状态\r\n");
  for(Customer c:rows) line(csv,c.getName(),c.getId(),c.getOwnerId(),c.getStage(),c.getCategory(),c.getIntent(),c.getPhone(),c.getDemand(),"unknown".equals(c.getAddedTimeSource())?"待核实":c.getCreatedAt(),c.getNextFollowupAt(),c.getCustomerNumber(),c.getOwnerUsername(),c.getWechat(),c.getAddress(),c.getBuildingArea(),c.getBuildingType(),c.getExpectedBuildTime(),c.getRemark(),c.getProtectionExpiresAt(),CustomerProtection.inPool(c)?"客户公海":"非公海");
  return download(csv,"customers.csv");
 }
 @GetMapping("/export/followups") public ResponseEntity<byte[]> followupExport(HttpServletRequest req,CustomerService.Filter filter) {
  List<Customer> rows=customers.export(actor(req),filter);
  StringBuilder csv=new StringBuilder("\uFEFF客户,企微客户ID,类型,操作账号,时间,状态,下次跟进,内容\r\n");int count=0;
  for(Customer c:rows) {
   @SuppressWarnings("unchecked") List<Followup> items=(List<Followup>)customers.detail(actor(req),c.getId()).get("followups");
   for(Followup f:items) {if(++count>100000) throw new ApiException(400,"跟进记录过多，请缩小筛选范围");line(csv,c.getName(),c.getId(),f.getKind(),f.getOperatorId(),f.getCreatedAt(),f.getStage(),f.getNextFollowupAt(),f.getContent());}
  }
  return download(csv,"followups.csv");
 }
 private ResponseEntity<byte[]> download(StringBuilder s,String name) {
  return ResponseEntity.ok().contentType(MediaType.parseMediaType("text/csv;charset=UTF-8")).header("Content-Disposition","attachment; filename="+name).body(s.toString().getBytes(StandardCharsets.UTF_8));
 }
 private void line(StringBuilder out,Object... fields) {
  for(int i=0;i<fields.length;i++) {if(i>0)out.append(',');String s=fields[i]==null?"":fields[i].toString();String trimmed=s.stripLeading();if((!trimmed.isEmpty() && "=+@-".indexOf(trimmed.charAt(0))>=0)||s.startsWith("\t")||s.startsWith("\r"))s="'"+s;out.append('"').append(s.replace("\"","\"\"")).append('"');}out.append("\r\n");
 }
}
