package cn.iyque.phase1;

import java.io.*;
import java.nio.charset.StandardCharsets;
import java.security.MessageDigest;
import java.sql.Timestamp;
import java.time.Instant;
import java.util.*;
import javax.crypto.Mac;
import javax.crypto.spec.SecretKeySpec;
import javax.servlet.*;
import javax.servlet.http.*;
import org.springframework.beans.factory.annotation.Value;
import org.springframework.dao.DuplicateKeyException;
import org.springframework.jdbc.core.JdbcTemplate;
import org.springframework.scheduling.annotation.Scheduled;
import org.springframework.stereotype.Component;
import org.springframework.web.filter.OncePerRequestFilter;

/** Authenticates only the private service boundary; business permissions remain in CustomerService. */
@Component
public class ManagementBridgeFilter extends OncePerRequestFilter {
 public static final String PREFIX="/internal/management/v1";
 private final JdbcTemplate jdbc;
 private final Repositories.Accounts accounts;
 private final Repositories.Employees employees;
 @Value("${crm.bridge.key:}") private String key;
 @Value("${crm.bridge.key-id:v1}") private String keyId;
 @Value("${crm.bridge.audience:}") private String audience;
 public ManagementBridgeFilter(JdbcTemplate j,Repositories.Accounts a,Repositories.Employees e) {jdbc=j;accounts=a;employees=e;}
 @Override protected boolean shouldNotFilter(HttpServletRequest r) {return !r.getRequestURI().startsWith("/internal/");}
 public static String signature(String key,String canonical) {
  try {Mac mac=Mac.getInstance("HmacSHA256");mac.init(new SecretKeySpec(key.getBytes(StandardCharsets.UTF_8),"HmacSHA256"));return Base64.getUrlEncoder().withoutPadding().encodeToString(mac.doFinal(canonical.getBytes(StandardCharsets.UTF_8)));}
  catch(Exception e) {throw new IllegalStateException(e);}
 }
 private String header(HttpServletRequest r,String name) {
  String s=r.getHeader("X-CRM-"+name);
  if(s==null||s.isBlank()||s.length()>256||s.contains("\n")||s.contains("\r")) throw new ApiException(401,"内部身份校验失败");return s;
 }
 private static boolean allowed(String method,String suffix,boolean platform) {
  if(platform) return (method.equals("GET")&&Set.of("/provision/employees","/provision/accounts","/provision/identity").contains(suffix)) || (method.equals("POST")&&suffix.equals("/provision/accounts"));
  if(method.equals("GET")) return Set.of("/auth/me","/customers","/overview","/dashboard","/employees","/sync","/export/customers","/export/followups","/workbench","/owners","/notices","/push-status").contains(suffix)||suffix.matches("/customers/[^/]+(?:/attachments/[^/]+)?");
  if(method.equals("PATCH")) return suffix.matches("/customers/[^/]+(?:/report)?");
  return method.equals("POST")&&(suffix.matches("/(?:cases|notices)/[^/]+/(?:decision|seen|retry-push)")||suffix.equals("/sync")||suffix.equals("/customers")||suffix.equals("/report-review")||suffix.matches("/customers/[^/]+/(?:followups|claim|attachments|requests|transfer|activate|merge|unlink|restore|review-followup)")||suffix.matches("/sync/events/[a-fA-F0-9]+/retry"));
 }
 @Override protected void doFilterInternal(HttpServletRequest r,HttpServletResponse res,FilterChain chain) throws ServletException,IOException {
  res.setHeader("Cache-Control","no-store");
  String subject="",nonce="";long user=0;boolean authenticated=false;
  try {
   if(key.length()<32||audience.isBlank()) throw new ApiException(503,"内部接入未配置");
   String path=r.getRequestURI(),query=Objects.toString(r.getQueryString(),"");
   if(!path.startsWith(PREFIX+"/")||path.contains(";")||path.contains("..")||query.contains("\n")||query.contains("\r")) throw new ApiException(401,"内部路径无效");
   subject=header(r,"Subject");nonce=header(r,"Nonce");
   String uid=header(r,"User-Id"),ts=header(r,"Timestamp");user=Long.parseLong(uid);
   long requestTime=Long.parseLong(ts),now=Instant.now().getEpochSecond();
   if(user<=0||!nonce.matches("[A-Za-z0-9_-]{22,128}")||requestTime<now-60||requestTime>now+60) throw new ApiException(401,"内部请求已过期");
   if(!keyId.equals(header(r,"Key-Id"))||!audience.equals(header(r,"Audience"))) throw new ApiException(401,"内部身份校验失败");
   int limit=r.getMethod().equals("POST")&&path.matches(PREFIX+"/customers/[^/]+/attachments")?3*1024*1024:65536;byte[] body=r.getInputStream().readNBytes(limit+1);if(body.length>limit) throw new ApiException(413,"请求过大");
   String digest=HexFormat.of().formatHex(MessageDigest.getInstance("SHA-256").digest(body));
   String canonical=String.join("\n","CRM-BRIDGE-V1",keyId,audience,subject,uid,ts,nonce,r.getMethod(),path,query,digest);
   if(!MessageDigest.isEqual(signature(key,canonical).getBytes(StandardCharsets.US_ASCII),header(r,"Signature").getBytes(StandardCharsets.US_ASCII))) throw new ApiException(401,"内部身份校验失败");
   String suffix=path.substring(PREFIX.length());boolean platform=subject.equals("@platform");
   boolean source=subject.equals("@source");
   if(source?!(r.getMethod().equals("POST")&&suffix.equals("/ingest/miniprogram")):!allowed(r.getMethod(),suffix,platform)) throw new ApiException(403,"内部操作未授权");
   try {jdbc.update("INSERT INTO crm_bridge_nonce(id,expiresAt) VALUES (?,?)",AuthService.hash(keyId+":"+nonce),Timestamp.from(Instant.now().plusSeconds(180)));}
   catch(DuplicateKeyException e) {throw new ApiException(401,"重复内部请求");}
   r.setAttribute("crm.managementUserId",user);
   if(source) r.setAttribute("crm.source",true);
   else if(platform) r.setAttribute("crm.platform",true);
   else {
    final long actorId=user;
    var account=accounts.findById(subject).filter(a->a.isEnabled()&&"managed".equals(a.getAuthMode())&&Objects.equals(a.getManagementUserId(),actorId)).orElseThrow(()->new ApiException(403,"客户账号未关联或已停用","account_inactive"));
    if(account.getEmployeeId()!=null&&!employees.findById(Objects.toString(account.getEmployeeId(),"")).map(Models.Employee::isActive).orElse(false)) throw new ApiException(403,"员工已停用","employee_inactive");
    r.setAttribute("crm.account",account);
   }
   authenticated=true;
   chain.doFilter(new BodyRequest(r,body),res);
  } catch(ApiException e) {reject(res,e.status,e.getMessage(),e.code);}
    catch(NumberFormatException e) {reject(res,401,"内部身份格式无效");}
    catch(java.security.NoSuchAlgorithmException e) {throw new ServletException(e);}
  finally {
   if(authenticated) jdbc.update("INSERT INTO crm_operation_audit(managementUserId,crmUsername,action,resource,requestId,result,createdAt) VALUES (?,?,?,?,?,?,?)",user,subject,r.getMethod(),r.getRequestURI(),nonce,res.getStatus(),Timestamp.from(Instant.now()));
  }
 }
 private void reject(HttpServletResponse r,int status,String message) throws IOException {
  reject(r,status,message,null);
 }
 private void reject(HttpServletResponse r,int status,String message,String code) throws IOException {
  r.setStatus(status);r.setContentType("application/json;charset=UTF-8");
  r.getWriter().write(code==null||code.isEmpty()?"{\"message\":\""+message+"\"}":"{\"message\":\""+message+"\",\"code\":\""+code+"\"}");
 }
 @Scheduled(fixedDelay=60000) public void cleanup() {jdbc.update("DELETE FROM crm_bridge_nonce WHERE expiresAt < ?",Timestamp.from(Instant.now()));}
 static class BodyRequest extends HttpServletRequestWrapper {
  private final byte[] body;
  BodyRequest(HttpServletRequest r,byte[] b) {super(r);body=b;}
  @Override public ServletInputStream getInputStream() {
   ByteArrayInputStream in=new ByteArrayInputStream(body);
   return new ServletInputStream() {public int read(){return in.read();}public boolean isFinished(){return in.available()==0;}public boolean isReady(){return true;}public void setReadListener(ReadListener listener){throw new UnsupportedOperationException();}};
  }
  @Override public BufferedReader getReader() {return new BufferedReader(new InputStreamReader(getInputStream(),StandardCharsets.UTF_8));}
 }
}
