package cn.iyque.phase1;
import cn.iyque.utils.IYqueCryptUtil;
import static cn.iyque.phase1.Models.*;
import javax.xml.parsers.DocumentBuilderFactory;
import org.xml.sax.InputSource;
import org.w3c.dom.Document;
import java.io.*;
import java.time.Instant;
import java.util.Set;
import org.springframework.beans.factory.annotation.Value;
import org.springframework.web.bind.annotation.*;
import javax.servlet.http.HttpServletRequest;

@RestController
public class WecomController {
 @Value("${crm.wecom.corp-id:}") private String corp;
 @Value("${crm.wecom.token:}") private String token;
 @Value("${crm.wecom.aes-key:}") private String aes;
 @Value("${crm.demo:false}") private boolean demo;
 private final ImportService imports;
 public WecomController(ImportService i) {imports=i;}
 private IYqueCryptUtil crypto() {
  if(demo || corp.isBlank() || token.isBlank() || aes.length()!=43) throw new ApiException(503,"企微回调尚未配置");
  return new IYqueCryptUtil(token,aes,corp);
 }
 private void query(String signature,String timestamp,String nonce) {
  try {
   if(signature==null || signature.length()!=40 || nonce==null || nonce.isBlank() || nonce.length()>256 || Math.abs(Instant.now().getEpochSecond()-Long.parseLong(timestamp))>300) throw new IllegalArgumentException();
  }catch(Exception e) {throw new ApiException(403,"回调签名或时间无效");}
 }
 @GetMapping(value="/iycallback/handle",produces="text/plain;charset=UTF-8")
 public String challenge(@RequestParam("msg_signature") String signature,@RequestParam String timestamp,@RequestParam String nonce,@RequestParam String echostr) {
  IYqueCryptUtil crypt=crypto();query(signature,timestamp,nonce);
  try {return crypt.verifyURL(signature,timestamp,nonce,echostr);}catch(Exception e){throw new ApiException(403,"回调验证失败");}
 }
 @PostMapping(value="/iycallback/handle",produces="text/plain;charset=UTF-8")
 public String event(@RequestParam("msg_signature") String signature,@RequestParam String timestamp,@RequestParam String nonce,HttpServletRequest req) throws IOException {
  IYqueCryptUtil crypt=crypto();query(signature,timestamp,nonce);
  byte[] body=req.getInputStream().readNBytes(131073);if(body.length>131072) throw new ApiException(413,"回调内容过大");
  final String plain;
  try {plain=crypt.decrypt(signature,timestamp,nonce,new String(body,java.nio.charset.StandardCharsets.UTF_8));}catch(Exception e){throw new ApiException(403,"回调验证失败");}
  Document d;
  try {
   DocumentBuilderFactory f=DocumentBuilderFactory.newInstance();f.setFeature("http://apache.org/xml/features/disallow-doctype-decl",true);
   f.setFeature("http://xml.org/sax/features/external-general-entities",false);f.setFeature("http://xml.org/sax/features/external-parameter-entities",false);
   f.setXIncludeAware(false);f.setExpandEntityReferences(false);d=f.newDocumentBuilder().parse(new InputSource(new StringReader(plain)));
  } catch(Exception e) {throw new ApiException(400,"回调内容无效");}
  if(!corp.equals(field(d,"ToUserName"))) throw new ApiException(403,"回调企业不匹配");
  if(!"change_external_contact".equals(field(d,"Event"))) return "success";
  String type=field(d,"ChangeType");
  if(!Set.of("add_external_contact","edit_external_contact","del_follow_user","del_external_contact").contains(type)) return "success";
  String external=field(d,"ExternalUserID"),employee=field(d,"UserID");
  if(external.isBlank() || external.length()>128 || employee.isBlank() || employee.length()>120) throw new ApiException(400,"回调缺少有效客户或员工");
  long created;
  try {created=Long.parseLong(field(d,"CreateTime"));if(created<=0 || created>Instant.now().getEpochSecond()+300) throw new IllegalArgumentException();}catch(Exception e){throw new ApiException(400,"事件时间无效");}
  Event e=new Event();e.setId(AuthService.hash(corp+"\n"+plain));e.setCustomerId(external);e.setEmployeeId(employee);e.setChangeType(type);
  e.setEventTime(created);e.setSourceState(CustomerService.text(field(d,"State"),255));
  imports.accept(e);return "success";
 }
 private String field(Document d,String tag) {return d.getElementsByTagName(tag).getLength()==0?"":d.getElementsByTagName(tag).item(0).getTextContent();}
}

