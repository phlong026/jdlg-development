package cn.iyque.phase1;

import java.util.*;
import javax.servlet.http.HttpServletRequest;
import org.springframework.http.*;
import org.springframework.transaction.annotation.Transactional;
import org.springframework.web.bind.annotation.*;
import static cn.iyque.phase1.Models.*;

@RestController
@RequestMapping({"/api","/internal/management/v1"})
public class WorkflowController {
 private final WecomNotifications push;private final AuthService auth; private final CustomerWorkflow workflow;
 public WorkflowController(AuthService a,CustomerWorkflow w,WecomNotifications p){auth=a;workflow=w;push=p;}
 public record Action(Long version,String reason,String sourceId,Long followupId){}
 @PostMapping("/report-review") public void reportReview(HttpServletRequest r,@RequestBody CustomerWorkflow.ReportReview i){workflow.reportReview(auth.current(r),i);}
 @GetMapping("/push-status") public Object pushStatus(HttpServletRequest r){return push.status(auth.current(r));}
 @PostMapping("/notices/{id}/retry-push") public void retryPush(HttpServletRequest r,@PathVariable String id){push.retry(auth.current(r),id);}
 @GetMapping("/workbench") public Object workbench(HttpServletRequest r){return workflow.workbench(auth.current(r));}
 @GetMapping("/owners") public Object owners(HttpServletRequest r){CustomerWorkflow.admin(auth.current(r));return workflow.accounts().findAll();}
 @GetMapping("/notices") public Object notices(HttpServletRequest r){return workflow.notices().findTop100ByUsernameOrderByCreatedAtDesc(auth.current(r).getUsername());}
 @PostMapping("/notices/{id}/seen") @Transactional public void seen(HttpServletRequest r,@PathVariable String id){Account a=auth.current(r);Notice n=workflow.notices().findById(id).orElseThrow(()->new ApiException(404,"通知不存在"));if(!a.getUsername().equals(n.getUsername()))throw new ApiException(403,"无权操作");n.setSeen(true);}
 @PostMapping("/customers/{id}/attachments") public Object upload(HttpServletRequest r,@PathVariable String id,@RequestBody CustomerWorkflow.FileInput i){return workflow.upload(auth.current(r),id,i);}
 @GetMapping("/customers/{id}/attachments/{fileId}") public ResponseEntity<byte[]> file(HttpServletRequest r,@PathVariable String id,@PathVariable String fileId){
  Customer c=workflow.visible(auth.current(r),id);Attachment f=workflow.attachments().findById(fileId).orElseThrow(()->new ApiException(404,"附件不存在"));
  if(!workflow.historyIds(c).contains(f.getCustomerId()))throw new ApiException(403,"附件不属于此客户");
  return ResponseEntity.ok().header("Cache-Control","no-store").header("X-Content-Type-Options","nosniff").header("Content-Disposition",ContentDisposition.attachment().filename(f.getName(),java.nio.charset.StandardCharsets.UTF_8).build().toString()).contentType(MediaType.parseMediaType(f.getMime())).body(f.getData());
 }
 @PostMapping("/customers/{id}/requests") public Object request(HttpServletRequest r,@PathVariable String id,@RequestBody CustomerWorkflow.CaseInput i){return workflow.request(auth.current(r),id,i);}
 @PostMapping("/cases/{id}/decision") public Object decide(HttpServletRequest r,@PathVariable long id,@RequestBody CustomerWorkflow.Decision i){return workflow.decide(auth.current(r),id,i);}
 @PostMapping("/customers/{id}/transfer") public Object transfer(HttpServletRequest r,@PathVariable String id,@RequestBody CustomerWorkflow.Transfer i){return workflow.transfer(auth.current(r),id,i);}
 @PostMapping("/customers/{id}/activate") public Object activate(HttpServletRequest r,@PathVariable String id,@RequestBody Action i){return workflow.activate(auth.current(r),id,i.version());}
 @PostMapping("/customers/{id}/merge") public Object merge(HttpServletRequest r,@PathVariable String id,@RequestBody CustomerWorkflow.Merge i){return workflow.merge(auth.current(r),id,i);}
 @PostMapping("/customers/{id}/unlink") public Object unlink(HttpServletRequest r,@PathVariable String id,@RequestBody Action i){return workflow.unlink(auth.current(r),id,i.sourceId(),i.version(),i.reason());}
 @PostMapping("/customers/{id}/restore") public Object restore(HttpServletRequest r,@PathVariable String id,@RequestBody Action i){return workflow.restore(auth.current(r),id,i.version(),i.reason());}
 @PostMapping("/customers/{id}/review-followup") public Object review(HttpServletRequest r,@PathVariable String id,@RequestBody Action i){if(i.followupId()==null)throw new ApiException(400,"请选择跟进记录");return workflow.invalidate(auth.current(r),id,i.followupId(),i.version(),i.reason());}
 @PostMapping("/ingest/miniprogram") public Object ingest(HttpServletRequest r,@RequestBody CustomerWorkflow.Ingest i){if(!Boolean.TRUE.equals(r.getAttribute("crm.source")))throw new ApiException(403,"仅允许已验证的来源服务");Customer c=workflow.ingest(i);return Map.of("customerId",c.getId());}
}
