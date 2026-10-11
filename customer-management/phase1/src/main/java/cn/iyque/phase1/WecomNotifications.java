package cn.iyque.phase1;

import java.util.*;
import org.springframework.beans.factory.annotation.Value;
import org.springframework.scheduling.annotation.Scheduled;
import org.springframework.stereotype.Service;
import me.chanjar.weixin.cp.api.impl.WxCpServiceImpl;
import me.chanjar.weixin.cp.config.impl.WxCpDefaultConfigImpl;
import me.chanjar.weixin.cp.bean.message.WxCpMessage;
import static cn.iyque.phase1.Models.*;

/** Optional app notifications. Station notices remain the authoritative inbox. */
@Service
public class WecomNotifications {
 private final Repositories.Notices notices;private final Repositories.Accounts accounts;private final Repositories.Employees employees;
 @Value("${crm.notify.enabled:false}") private boolean enabled;
 @Value("${crm.demo:false}") private boolean demo;
 @Value("${crm.wecom.corp-id:}") private String corp;
 @Value("${crm.notify.secret:}") private String secret;
 @Value("${crm.notify.agent-id:0}") private int agentId;
 private WxCpServiceImpl client;
 public WecomNotifications(Repositories.Notices n,Repositories.Accounts a,Repositories.Employees e){notices=n;accounts=a;employees=e;}
 public boolean configured(){return enabled&&!demo&&!corp.isBlank()&&!secret.isBlank()&&agentId>0;}
 private synchronized WxCpServiceImpl client(){if(client==null){WxCpDefaultConfigImpl config=new WxCpDefaultConfigImpl();config.setCorpId(corp);config.setCorpSecret(secret);config.setAgentId(agentId);client=new WxCpServiceImpl();client.setWxCpConfigStorage(config);client.setMaxRetryTimes(0);}return client;}
 @Scheduled(fixedDelay=60000,initialDelay=120000) public void deliver(){if(!configured())return;
  for(Notice n:notices.findTop20ByPushStateOrderByCreatedAtAsc("pending")){
   if(notices.claimPush(n.getId())!=1)continue;
   Account a=accounts.findById(n.getUsername()).orElse(null);
   if(a==null||!a.isEnabled()||a.getEmployeeId()==null||!employees.findById(a.getEmployeeId()).map(Employee::isActive).orElse(false)){notices.finishPush(n.getId(),"skipped","未绑定有效企微成员，保留站内通知");continue;}
   try {WxCpMessage m=new WxCpMessage();m.setAgentId(agentId);m.setToUser(a.getEmployeeId());m.setMsgType("text");m.setContent(n.getContent()+"\n请在客户管理后台查看。\n通知编号："+n.getId());m.setEnableDuplicateCheck(true);m.setDuplicateCheckInterval(1800);
    var result=client().getMessageService().send(m);boolean ok=result!=null&&(result.getErrCode()==null||result.getErrCode()==0)&&(result.getInvalidUser()==null||result.getInvalidUser().isBlank())&&(result.getUnlicensedUser()==null||result.getUnlicensedUser().isBlank());
    notices.finishPush(n.getId(),ok?"sent":"failed",ok?"":"成员不在应用可见范围或消息被拒绝");
   }catch(Exception e){notices.finishPush(n.getId(),"failed","发送结果未确认，请检查应用配置与企微消息后重试");}
  }
 }
 public Map<String,Object> status(Account a){CustomerWorkflow.admin(a);return Map.of("configured",configured(),"failures",notices.findTop20ByPushStateOrderByCreatedAtAsc("failed"),"uncertain",notices.findTop20ByPushStateOrderByCreatedAtAsc("sending"));}
 public void retry(Account a,String id){CustomerWorkflow.admin(a);if(!configured())throw new ApiException(400,"企微通知尚未配置启用");if(notices.retryPush(id)!=1)throw new ApiException(409,"只能重试失败通知；发送中状态需由运维核实");}
}
