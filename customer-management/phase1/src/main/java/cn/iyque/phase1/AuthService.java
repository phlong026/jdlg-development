package cn.iyque.phase1;
import static cn.iyque.phase1.Models.*;
import java.time.Instant;
import java.util.*;
import java.util.concurrent.ConcurrentHashMap;
import java.security.SecureRandom;
import java.nio.charset.StandardCharsets;
import java.security.MessageDigest;
import javax.servlet.http.*;
import org.springframework.beans.factory.annotation.Value;
import org.springframework.stereotype.Service;
import org.springframework.security.crypto.bcrypt.BCryptPasswordEncoder;
import org.springframework.http.ResponseCookie;
import org.springframework.transaction.annotation.Transactional;
import org.springframework.web.servlet.HandlerInterceptor;
import org.springframework.web.servlet.config.annotation.*;
import org.springframework.scheduling.annotation.Scheduled;

@Service
public class AuthService implements WebMvcConfigurer {
 private final Repositories.Accounts accounts;
 private final Repositories.Sessions sessions;
 private final Repositories.Employees employees;
 private final BCryptPasswordEncoder encoder=new BCryptPasswordEncoder(12);
 private final String dummyHash=encoder.encode("not-a-real-password");
 private final Map<String,long[]> attempts=new ConcurrentHashMap<>();
 @Value("${crm.secure-cookie:true}") private boolean secure;
 @Value("${crm.standalone-enabled:false}") private boolean standaloneEnabled;
 public AuthService(Repositories.Accounts a,Repositories.Sessions s,Repositories.Employees e) { accounts=a;sessions=s;employees=e; }
 public static String hash(String text) {
  try { return HexFormat.of().formatHex(MessageDigest.getInstance("SHA-256").digest(text.getBytes(StandardCharsets.UTF_8))); }
  catch(Exception e) { throw new IllegalStateException(e); }
 }
 public String password(String raw) {
  if(raw==null || raw.length()<12 || raw.getBytes(StandardCharsets.UTF_8).length>72) throw new ApiException(400,"密码须至少 12 个字符且不超过 72 字节");
  return encoder.encode(raw);
 }
 public Account login(String username,String password,String ip,HttpServletResponse response) {
  if(!standaloneEnabled) throw new ApiException(403,"请从统一管理后台登录");
  if(username==null || username.length()>64 || password==null || password.length()>128) throw new ApiException(400,"账号或密码格式错误");
  long now=System.currentTimeMillis();
  attempts.entrySet().removeIf(e->now-e.getValue()[0]>900_000);
  if(attempts.size()>10000) throw new ApiException(429,"登录繁忙，请稍后重试");
  long[] count=attempts.computeIfAbsent(ip,k->new long[]{now,0});
  synchronized(count) { if(++count[1]>15) throw new ApiException(429,"尝试次数过多，请 15 分钟后再试"); }
  Account a=accounts.findById(username).orElse(null);
  boolean matched=encoder.matches(password,a==null?dummyHash:a.getPasswordHash());
  if(!matched || a==null || !a.isEnabled() || !"standalone".equals(a.getAuthMode())) throw new ApiException(401,"账号或密码错误，统一账号请从管理后台登录");
  if(a.getEmployeeId()!=null && !employees.findById(a.getEmployeeId()).map(Employee::isActive).orElse(false))
   throw new ApiException(403,"员工已停用，请联系管理员");
  attempts.remove(ip);
  byte[] bytes=new byte[32];new SecureRandom().nextBytes(bytes);
  String token=Base64.getUrlEncoder().withoutPadding().encodeToString(bytes);
  Session session=new Session();session.setId(hash(token));session.setUsername(a.getUsername());session.setExpiresAt(Instant.now().plusSeconds(28800));
  sessions.save(session);cookie(response,token,28800);return a;
 }
 private void cookie(HttpServletResponse response,String value,int age) {
  response.addHeader("Set-Cookie",ResponseCookie.from("CRM_SESSION",value).httpOnly(true).secure(secure).sameSite("Strict").path("/").maxAge(age).build().toString());
 }
 private String token(HttpServletRequest req) {
  if(req.getCookies()!=null) for(Cookie cookie:req.getCookies()) if("CRM_SESSION".equals(cookie.getName())) return cookie.getValue();
  return "";
 }
 public void logout(HttpServletRequest req,HttpServletResponse response) { sessions.deleteById(hash(token(req)));cookie(response,"",0); }
 public Account current(HttpServletRequest request) { return (Account)request.getAttribute("crm.account"); }
 public void admin(Account a) { if(a==null || !a.isAdmin()) throw new ApiException(403,"仅管理员可执行此操作"); }
 @Transactional
 public Account saveAccount(Account actor,Account incoming,String password) {
  admin(actor);
  if(incoming.getUsername()==null || !incoming.getUsername().matches("[A-Za-z0-9._-]{3,64}")) throw new ApiException(400,"账号需为 3–64 位字母、数字或 ._-");
  if(incoming.getRole()==null || !Set.of("admin","staff").contains(incoming.getRole())) throw new ApiException(400,"角色无效");
  if(incoming.getEmployeeId()!=null && !incoming.getEmployeeId().isBlank() && !employees.findById(incoming.getEmployeeId()).map(Employee::isActive).orElse(false))
   throw new ApiException(400,"请选择有效企微员工");
  if(actor.getUsername().equals(incoming.getUsername()) && (!incoming.isAdmin() || !incoming.isEnabled())) throw new ApiException(400,"不能停用或降级当前管理员");
  Account old=accounts.findById(incoming.getUsername()).orElse(null);
  if(old!=null && "managed".equals(old.getAuthMode())) throw new ApiException(403,"统一账号只能在平台授权页维护");
  if(incoming.getDisplayName()==null || incoming.getDisplayName().isBlank() || incoming.getDisplayName().length()>100) throw new ApiException(400,"请填写显示名称");
  if(incoming.getEmployeeId()!=null&&incoming.getEmployeeId().isBlank())incoming.setEmployeeId(null);
  incoming.setPasswordHash(password!=null && !password.isEmpty()?password(password):old==null?password(password):old.getPasswordHash());
  sessions.deleteByUsername(incoming.getUsername());
  return accounts.save(incoming);
 }
 @Override public void addInterceptors(InterceptorRegistry registry) {
  registry.addInterceptor(new HandlerInterceptor() {
   @Override public boolean preHandle(HttpServletRequest req,HttpServletResponse res,Object handler) {
    if(!standaloneEnabled) throw new ApiException(403,"请从统一管理后台登录");
    res.setHeader("Cache-Control","no-store");
    if(!Set.of("GET","HEAD","OPTIONS").contains(req.getMethod()) && !"1".equals(req.getHeader("X-CRM-Request"))) throw new ApiException(403,"请求校验失败");
    if("/api/auth/login".equals(req.getRequestURI())) return true;
    Session s=sessions.findById(hash(token(req))).orElse(null);
    if(s==null || s.getExpiresAt().isBefore(Instant.now())) throw new ApiException(401,"请先登录");
    Account a=accounts.findById(s.getUsername()).filter(Account::isEnabled).orElseThrow(()->new ApiException(401,"请重新登录"));
    if(!"standalone".equals(a.getAuthMode())) throw new ApiException(401,"请从统一管理后台登录");
    if(a.getEmployeeId()!=null && !employees.findById(a.getEmployeeId()).map(Employee::isActive).orElse(false)) throw new ApiException(403,"员工已停用");
    req.setAttribute("crm.account",a);return true;
   }
  }).addPathPatterns("/api/**");
 }
 @Scheduled(fixedDelay=3600000) @Transactional public void cleanup() { sessions.deleteByExpiresAtBefore(Instant.now()); }
}
