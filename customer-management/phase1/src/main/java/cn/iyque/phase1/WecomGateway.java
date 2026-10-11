package cn.iyque.phase1;
import java.util.*;
import org.springframework.stereotype.Component;
import org.springframework.beans.factory.annotation.Value;
import me.chanjar.weixin.cp.api.impl.WxCpServiceImpl;
import me.chanjar.weixin.cp.config.impl.WxCpDefaultConfigImpl;
import me.chanjar.weixin.cp.bean.external.contact.*;
import me.chanjar.weixin.common.error.WxErrorException;

@Component
public class WecomGateway {
 @Value("${crm.wecom.corp-id:}") private String corp;
 @Value("${crm.wecom.secret:}") private String secret;
 @Value("${crm.demo:false}") private boolean demo;
 private WxCpServiceImpl service;
 public record Relation(String employeeId,long addedAt,String state) {}
 public record Snapshot(String id,String name,String avatar,Integer type,List<Relation> relations) {}
 public boolean configured() { return !demo && !corp.isBlank() && !secret.isBlank(); }
 private synchronized WxCpServiceImpl client() {
  if(!configured()) throw new ApiException(503,"请先配置企微客户联系凭据");
  if(service==null) {
   WxCpDefaultConfigImpl config=new WxCpDefaultConfigImpl();config.setCorpId(corp);config.setCorpSecret(secret);
   service=new WxCpServiceImpl();service.setWxCpConfigStorage(config);service.setMaxRetryTimes(1);service.setRetrySleepMillis(300);
  }
  return service;
 }
 private ApiException error(Exception e) {
  String suffix=e instanceof WxErrorException?"（错误码 "+((WxErrorException)e).getError().getErrorCode()+"）":"";
  return new ApiException(502,"企微读取失败，请检查权限或稍后重试"+suffix);
 }
 public List<String> employees() {
  try { return client().getExternalContactService().listFollowers(); } catch(ApiException e) {throw e;} catch(Exception e) {throw error(e);}
 }
 public String employeeName(String id) {
  try { String name=client().getUserService().getById(id).getName();return name==null?id:name; } catch(Exception e) {return id;}
 }
 public List<String> customerIds(String employeeId) {
  try { return client().getExternalContactService().listExternalContacts(employeeId); } catch(ApiException e) {throw e;} catch(Exception e) {throw error(e);}
 }
 public Snapshot contact(String id) {
  try {
   List<Relation> relations=new ArrayList<>();Set<String> cursors=new HashSet<>();String cursor=null;ExternalContact external=null;
   do {
    WxCpExternalContactInfo page=client().getExternalContactService().getContactDetail(id,cursor);
    if(page==null || page.getExternalContact()==null || !id.equals(page.getExternalContact().getExternalUserId())) throw new IllegalStateException("incomplete response");
    external=page.getExternalContact();
    if(page.getFollowedUsers()!=null) for(FollowedUser f:page.getFollowedUsers()) relations.add(new Relation(f.getUserId(),f.getCreateTime()==null?0:f.getCreateTime(),f.getState()));
    cursor=page.getNextCursor();
    if(cursor!=null && !cursor.isBlank() && !cursors.add(cursor)) throw new IllegalStateException("repeated cursor");
   } while(cursor!=null && !cursor.isBlank());
   return new Snapshot(id,external.getName(),external.getAvatar(),external.getType(),relations);
  } catch(ApiException e) {throw e;} catch(Exception e) {throw error(e);}
 }
}

