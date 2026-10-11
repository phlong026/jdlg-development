package cn.iyque.phase1;

import static cn.iyque.phase1.Models.*;
import java.util.*;
import javax.servlet.http.HttpServletRequest;
import org.springframework.web.bind.annotation.*;
import org.springframework.transaction.annotation.Transactional;
import org.springframework.beans.factory.annotation.Value;

/** Separate platform capability: a CRM supervisor cannot provision platform identities. */
@RestController @RequestMapping("/internal/management/v1/provision")
public class ManagementProvisionController {
 @Value("${crm.wecom.corp-id:}") private String corpId;
 @Value("${crm.demo:false}") private boolean demo;
 @GetMapping("/identity") public Map<String,Object> identity(HttpServletRequest r) {platform(r);return Map.of("corpId",corpId,"demo",demo,"accounts",accounts(r));}
 private final Repositories.Accounts accounts;private final Repositories.Employees employees;private final Repositories.Sessions sessions;private final AuthService auth;
 public ManagementProvisionController(Repositories.Accounts a,Repositories.Employees e,Repositories.Sessions s,AuthService auth) {accounts=a;employees=e;sessions=s;this.auth=auth;}
 private void platform(HttpServletRequest r) {if(!Boolean.TRUE.equals(r.getAttribute("crm.platform"))) throw new ApiException(403,"仅平台管理员可执行");}
 private static String blankToNull(String s) {return s==null||s.isBlank()?null:s;}
 public record Input(Long userId,long revision,String displayName,String employeeId,String role,boolean enabled) {}
 @GetMapping("/employees") public List<Employee> employees(HttpServletRequest r) {platform(r);return employees.findAll();}
 @GetMapping("/accounts") public List<Account> accounts(HttpServletRequest r) {platform(r);return accounts.findAll().stream().filter(a->"managed".equals(a.getAuthMode())).toList();}
 @PostMapping("/accounts") @Transactional public Account save(HttpServletRequest r,@RequestBody Input i) {
  platform(r);
  if(i.userId()==null||i.userId()<=0||i.displayName()==null||i.displayName().isBlank()||i.displayName().length()>100||!Set.of("admin","staff").contains(Objects.toString(i.role(),""))) throw new ApiException(400,"账号信息无效");
  if(i.enabled()&&i.employeeId()!=null&&!i.employeeId().isBlank()&&!employees.findById(Objects.toString(i.employeeId(),"")).map(Employee::isActive).orElse(false)) throw new ApiException(400,"请选择有效企微员工");
  String username="managed-"+i.userId();Account a=accounts.locked(username).orElse(null);
  if(a!=null&&(!"managed".equals(a.getAuthMode())||!Objects.equals(a.getManagementUserId(),i.userId()))) throw new ApiException(409,"目标账号已被占用");
  if(i.revision()<1) throw new ApiException(409,"授权版本无效");
  if(a!=null && i.revision()<a.getManagedRevision()) throw new ApiException(409,"授权版本已过期");
  if(a!=null && i.revision()==a.getManagedRevision()) {
   // 补偿重放到达时首次调用已成功：同 revision 同参数幂等返回，不做任何副作用（不删会话、不改动）。
   boolean same=Objects.equals(a.getManagementUserId(),i.userId())
       &&Objects.equals(a.getDisplayName(),i.displayName())
       &&Objects.equals(blankToNull(a.getEmployeeId()),blankToNull(i.employeeId()))
       &&Objects.equals(a.getRole(),i.role())
       &&a.isEnabled()==i.enabled();
   if(!same) throw new ApiException(409,"授权版本已过期");
   return a;
  }
  if(a==null) {a=new Account();a.setUsername(username);a.setPasswordHash(auth.password(UUID.randomUUID().toString()));}
  a.setManagementUserId(i.userId());a.setManagedRevision(i.revision());a.setAuthMode("managed");a.setDisplayName(i.displayName());a.setRole(i.role());a.setEmployeeId(blankToNull(i.employeeId()));a.setEnabled(i.enabled());
  sessions.deleteByUsername(username);return accounts.saveAndFlush(a);
 }
}
