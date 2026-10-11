package cn.iyque.phase1;

import static cn.iyque.phase1.Models.*;
import java.time.*;
import java.util.*;
import javax.persistence.EntityManager;
import javax.persistence.LockModeType;
import org.springframework.stereotype.Service;
import org.springframework.transaction.annotation.Transactional;

@Service
public class CustomerWorkflow {
 final Repositories.Customers customers; final Repositories.Sources sources; final Repositories.Attachments attachments;
 final Repositories.Cases cases; final Repositories.Followups followups; final Repositories.Accounts accounts;
 final Repositories.Employees employees; final Repositories.Settings settings; final Repositories.Notices notices;
 private final EntityManager em;
 private final com.fasterxml.jackson.databind.ObjectMapper json=new com.fasterxml.jackson.databind.ObjectMapper();
 public CustomerWorkflow(Repositories.Customers c,Repositories.Sources s,Repositories.Attachments at,Repositories.Cases ca,Repositories.Followups f,Repositories.Accounts ac,Repositories.Employees e,Repositories.Settings se,Repositories.Notices n,EntityManager em){customers=c;sources=s;attachments=at;cases=ca;followups=f;accounts=ac;employees=e;settings=se;notices=n;this.em=em;}
 public Repositories.Customers customers(){return customers;}
 public Repositories.Sources sources(){return sources;}
 public Repositories.Attachments attachments(){return attachments;}
 public Repositories.Cases cases(){return cases;}
 public Repositories.Followups followups(){return followups;}
 public Repositories.Accounts accounts(){return accounts;}
 public Repositories.Employees employees(){return employees;}
 public Repositories.Settings settings(){return settings;}
 public Repositories.Notices notices(){return notices;}
 public void lock(){settings.locked("report-lock").orElseThrow();}
 public Customer locked(String id){lock();Customer c=customers.locked(id).orElseThrow(()->new ApiException(404,"客户不存在"));em.refresh(c,LockModeType.PESSIMISTIC_WRITE);return c;}
 public static void number(Customer c){if(c.getCustomerNumber()==null)c.setCustomerNumber("KH"+UUID.randomUUID().toString().replace("-","").toUpperCase(Locale.ROOT));}
 public static boolean owns(Account a,Customer c){return c.getOwnerUsername()!=null?Objects.equals(c.getOwnerUsername(),a.getUsername()):a.getEmployeeId()!=null&&Objects.equals(c.getOwnerId(),a.getEmployeeId());}
 public static boolean canSee(Account a,Customer c){return a!=null&&(a.isAdmin()||CustomerProtection.inPool(c)||owns(a,c));}
 public static void admin(Account a){if(a==null||!a.isAdmin())throw new ApiException(403,"仅主管可执行");}
 public Customer visible(Account a,String id){Customer c=customers.findById(id).orElseThrow(()->new ApiException(404,"客户不存在"));if(c.getMergedInto()!=null)c=customers.findById(c.getMergedInto()).orElseThrow();if(!canSee(a,c))throw new ApiException(403,"无权查看此客户");return c;}
 public void version(Customer c,Long v){if(v==null||!Objects.equals(v,c.getVersion()))throw new ApiException(409,"客户已更新，请刷新后重试");if(c.getMergedInto()!=null)throw new ApiException(409,"客户已合并，请打开主档");}
 public List<String> historyIds(Customer c){Set<String> ids=new LinkedHashSet<>();ids.add(c.getId());sources.findByCustomerId(c.getId()).forEach(s->ids.add(s.getOriginCustomerId()));return new ArrayList<>(ids);}
 public void audit(Customer c,String actor,String kind,String content){Followup f=new Followup();f.setCustomerId(c.getId());f.setOperatorId(actor);f.setKind(kind);f.setEffective(false);f.setContent(CustomerService.text(content,2000));f.setStage(c.getStage());followups.save(f);c.setUpdatedAt(Instant.now());}
 public void notice(String username,Customer c,String key,String content){if(username==null)return;String id=AuthService.hash(username+":"+key);if(notices.existsById(id))return;Notice n=new Notice();n.setId(id);n.setUsername(username);n.setCustomerId(c.getId());n.setContent(content);notices.save(n);}
 public void notifyAdmins(Customer c,String key,String content){accounts.findAll().stream().filter(a->a.isAdmin()&&a.isEnabled()).forEach(a->notice(a.getUsername(),c,key,content));}
 public String sourceKey(String kind,String externalId){return kind+":"+AuthService.hash(externalId);}
 public Customer sourceCustomer(String kind,String externalId){return sources.findById(sourceKey(kind,externalId)).flatMap(s->customers.findById(s.getCustomerId())).orElse(null);}
 public void registerSource(Customer c,String kind,String externalId,String name,String phone,String wechat,String summary){
  String id=sourceKey(kind,externalId);Source s=sources.findById(id).orElse(null);if(s==null){s=new Source();s.setId(id);s.setCustomerId(c.getId());s.setOriginCustomerId(c.getId());s.setKind(kind);s.setExternalId(externalId);s.setCreatedAt(c.getCreatedAt());s.setSubmittedBy(Objects.toString(c.getOwnerUsername(),Objects.toString(c.getOwnerId(),"")));}
  s.setName(CustomerService.text(name,200));s.setPhone(CustomerService.text(phone,80));s.setWechat(CustomerService.text(wechat,80));s.setSummary(CustomerService.text(summary,2000));sources.save(s);
 }
 public record SourceHistory(String externalId,String operator,String content,Instant createdAt){}
 public record Ingest(String externalId,String name,String phone,String wechat,String summary,String submittedBy,String channel,Instant createdAt,List<SourceHistory> history){public Ingest(String externalId,String name,String phone,String wechat,String summary){this(externalId,name,phone,wechat,summary,"","",null,List.of());}}
 @Transactional public Customer ingest(Ingest i){lock();if(i.externalId()==null||!i.externalId().matches("[1-9][0-9]{0,18}"))throw new ApiException(400,"来源标识无效");Customer c=sourceCustomer("miniprogram",i.externalId());
  if(c==null){c=new Customer();c.setId("mini-"+UUID.randomUUID());number(c);c.setLifecycle("pending");c.setAddedTimeSource("miniprogram");c.setName(CustomerService.text(i.name(),200));c.setPhone(CustomerService.text(i.phone(),80));c.setWechat(CustomerService.text(i.wechat(),80));c.setDemand(CustomerService.text(i.summary(),2000));customers.saveAndFlush(c);audit(c,"system","source","小程序咨询接入，待分配及完善");notifyAdmins(c,"source:"+c.getId(),"新增小程序咨询待分配");}
  registerSource(c,"miniprogram",i.externalId(),i.name(),i.phone(),i.wechat(),i.summary());Source source=sources.findById(sourceKey("miniprogram",i.externalId())).orElseThrow();source.setSubmittedBy(CustomerService.text(i.submittedBy(),64));source.setChannel(CustomerService.text(i.channel(),64));if(i.createdAt()!=null)source.setCreatedAt(i.createdAt());sources.save(source);
  if(i.history()!=null){if(i.history().size()>5)throw new ApiException(400,"历史记录每批最多 5 条");for(SourceHistory row:i.history()){
   if(row.externalId()==null||!row.externalId().matches("[1-9][0-9]{0,18}"))throw new ApiException(400,"历史记录标识无效");String key="miniprogram:"+i.externalId()+":"+row.externalId();if(followups.existsByExternalKey(key))continue;
   Followup f=new Followup();f.setExternalKey(key);f.setCustomerId(source.getOriginCustomerId());f.setOperatorId(CustomerService.text(row.operator(),64));f.setContent(CustomerService.text(row.content(),2000));f.setCreatedAt(row.createdAt()==null?source.getCreatedAt():row.createdAt());f.setKind("source_history");f.setOutcome("imported");f.setEffective(false);f.setStage(c.getStage());followups.save(f);
  }}return c;
 }
 public List<Attachment> files(Customer c){List<Attachment> result=new ArrayList<>();for(String id:historyIds(c))result.addAll(attachments.findByCustomerIdOrderByCreatedAtDesc(id));return result;}
 public void validateFiles(Customer c,List<String> ids,boolean required){if(ids==null)ids=List.of();if(required&&ids.isEmpty())throw new ApiException(400,"请上传并选择相关凭证附件");if(ids.size()>10)throw new ApiException(400,"附件数量过多");Set<String> scope=new HashSet<>(historyIds(c));for(String id:ids){Attachment f=attachments.findById(id).orElseThrow(()->new ApiException(400,"附件不存在"));if(!scope.contains(f.getCustomerId()))throw new ApiException(403,"不能使用其他客户的附件");}}
 public record FileInput(String name,String base64){}
 @Transactional public Attachment upload(Account a,String id,FileInput i){Customer c=locked(id);if(!canSee(a,c)||CustomerProtection.inPool(c)||c.getMergedInto()!=null)throw new ApiException(403,"请先取得客户归属");
  String name=CustomerService.text(i.name(),200);if(name.isBlank()||name.contains("/")||name.contains("\\")||name.chars().anyMatch(ch->ch<32))throw new ApiException(400,"文件名无效");
  byte[] bytes;try{bytes=Base64.getDecoder().decode(i.base64());}catch(Exception e){throw new ApiException(400,"文件内容无效");}if(bytes.length==0||bytes.length>2*1024*1024)throw new ApiException(400,"附件大小需在 2MB 以内");
  String mime=null;String lower=name.toLowerCase(Locale.ROOT);
  if(lower.endsWith(".pdf")&&bytes.length>=5&&new String(bytes,0,5,java.nio.charset.StandardCharsets.US_ASCII).equals("%PDF-"))mime="application/pdf";
  if((lower.endsWith(".jpg")||lower.endsWith(".jpeg"))&&bytes.length>=3&&(bytes[0]&255)==255&&(bytes[1]&255)==216&&(bytes[2]&255)==255)mime="image/jpeg";
  if(lower.endsWith(".png")&&bytes.length>=8&&Arrays.equals(Arrays.copyOf(bytes,8),new byte[]{(byte)137,80,78,71,13,10,26,10}))mime="image/png";
  if(mime==null)throw new ApiException(400,"仅支持内容与扩展名一致的 PDF、JPG、PNG");if(files(c).size()>=20)throw new ApiException(400,"每位客户最多 20 个附件");
  Attachment f=new Attachment();f.setId(UUID.randomUUID().toString());f.setCustomerId(c.getId());f.setName(name);f.setMime(mime);f.setSize(bytes.length);f.setData(bytes);f.setOperatorId(a.getUsername());attachments.save(f);audit(c,a.getUsername(),"attachment","上传附件："+name);return f;
 }
 public record CaseInput(String kind,String content,List<String> attachmentIds){}
 @Transactional public ReviewCase request(Account a,String id,CaseInput i){Customer c=locked(id);if(!canSee(a,c)||c.getMergedInto()!=null)throw new ApiException(403,"无权申请");if(!Set.of("deal","archive","dispute","duplicate").contains(Objects.toString(i.kind(),"")))throw new ApiException(400,"申请类型无效");
  if(!a.isAdmin()&&!owns(a,c)&&!Set.of("dispute","duplicate").contains(i.kind()))throw new ApiException(403,"仅负责人可申请");String content=CustomerService.text(i.content(),2000);if(content.isBlank())throw new ApiException(400,"请填写申请说明");validateFiles(c,i.attachmentIds(),"deal".equals(i.kind()));
  ReviewCase r=new ReviewCase();r.setCustomerId(id);r.setKind(i.kind());r.setApplicant(a.getUsername());r.setContent(content);r.setAttachmentIds(String.join(",",i.attachmentIds()==null?List.of():i.attachmentIds()));cases.saveAndFlush(r);audit(c,a.getUsername(),"request","提交主管处理："+i.kind());notifyAdmins(c,"case:"+r.getId(),"客户有新的待处理申请");return r;
 }
 public record ReportReview(CustomerService.Report report,String reason){}
 @Transactional public void reportReview(Account a,ReportReview input){lock();if(input.report()==null)throw new ApiException(400,"请填写资料");Customer proposed=new Customer();CustomerService.applyReport(proposed,input.report());String reason=CustomerService.text(input.reason(),1000);if(reason.isBlank())throw new ApiException(400,"请说明共用电话或争议情况");Customer existing=customers.findByPhoneKeyOrPhone(proposed.getPhone(),proposed.getPhone()).stream().filter(c->c.getMergedInto()==null).findFirst().orElseThrow(()->new ApiException(400,"未找到重复电话，请直接报备"));
  if(cases.existsByStatusAndApplicantAndKindAndCustomerId("pending",a.getUsername(),"report",existing.getId()))throw new ApiException(409,"您已提交核实申请，请等待主管处理");
  ReviewCase r=new ReviewCase();r.setKind("report");r.setApplicant(a.getUsername());r.setCustomerId(existing.getId());r.setContent("新报备客户："+proposed.getName()+"；地址："+proposed.getAddress()+"；核实说明："+reason);try{r.setRequestData(json.writeValueAsString(input.report()));}catch(Exception e){throw new ApiException(400,"申请资料无效");}cases.saveAndFlush(r);notifyAdmins(existing,"case:"+r.getId(),"有重复电话报备申请待核实");
 }
 public record Decision(Long version,boolean approve,String reason,String ownerUsername){}
 @Transactional public Customer decide(Account a,long caseId,Decision i){admin(a);lock();ReviewCase r=cases.findById(caseId).orElseThrow(()->new ApiException(404,"申请不存在"));if(!"pending".equals(r.getStatus()))throw new ApiException(409,"申请已处理");Customer original=customers.findById(r.getCustomerId()).orElseThrow(()->new ApiException(404,"客户不存在"));Customer c=locked(original.getMergedInto()==null?original.getId():original.getMergedInto());version(c,i.version());String reason=CustomerService.text(i.reason(),2000);if(reason.isBlank())throw new ApiException(400,"请填写处理依据");
  if(i.approve()&&Set.of("deal","archive").contains(r.getKind())&&(CustomerProtection.inPool(c)||!Objects.equals(r.getApplicant(),c.getOwnerUsername())&&!accounts.findById(r.getApplicant()).map(Account::isAdmin).orElse(false)))throw new ApiException(409,"归属已变化，请由当前负责人重新申请");
  if(i.approve())switch(r.getKind()){
   case "deal":validateFiles(c,r.getAttachmentIds().isBlank()?List.of():List.of(r.getAttachmentIds().split(",")),true);c.setLifecycle("won");c.setStage("won");c.setPoolAt(null);c.setProtectionExpiresAt(null);c.setNextFollowupAt(null);break;
   case "archive":c.setLifecycle("archived");c.setStage("invalid");c.setPoolAt(null);c.setProtectionExpiresAt(null);c.setNextFollowupAt(null);break;
   case "dispute":if(CustomerProtection.inPool(c))throw new ApiException(409,"客户已在公海，请先认领后处理争议");if(i.ownerUsername()!=null&&!i.ownerUsername().isBlank())assign(c,i.ownerUsername());break;
   case "report":
    Account reporter=accounts.findById(r.getApplicant()).filter(Account::isEnabled).orElseThrow(()->new ApiException(400,"申请账号已停用"));
    if(reporter.getEmployeeId()!=null&&!employees.findById(reporter.getEmployeeId()).map(Employee::isActive).orElse(false))throw new ApiException(400,"申请员工已停用");
    Customer separate=new Customer();try{CustomerService.applyReport(separate,json.readValue(r.getRequestData(),CustomerService.Report.class));}catch(Exception e){throw new ApiException(400,"申请资料无效，请重新提交");}
    separate.setId("manual-"+UUID.randomUUID());number(separate);separate.setPhoneKey(null);separate.setAddedTimeSource("manual");CustomerProtection.protect(separate,reporter);customers.saveAndFlush(separate);registerSource(separate,"manual",separate.getId(),separate.getName(),separate.getPhone(),separate.getWechat(),"主管批准共用电话例外");audit(separate,a.getUsername(),"report","共用电话例外审批 #"+r.getId()+"；"+reason);notice(reporter.getUsername(),separate,"report-review:"+r.getId(),"主管已核实为独立客户并完成报备，开始 90 天保护");break;
   case "duplicate":break;
   default:throw new ApiException(400,"不支持的申请");
  }
  r.setStatus(i.approve()?"approved":"rejected");r.setReviewer(a.getUsername());r.setDecision(reason);r.setResolvedAt(Instant.now());cases.save(r);audit(c,a.getUsername(),"decision","主管处理"+r.getKind()+"："+reason);notice(r.getApplicant(),c,"decision:"+r.getId(),"您的客户申请已处理："+reason);return customers.saveAndFlush(c);
 }
 private void assign(Customer c,String username){Account target=accounts.findById(username).filter(Account::isEnabled).orElseThrow(()->new ApiException(400,"请选择有效账号"));if(target.getEmployeeId()!=null&&!employees.findById(target.getEmployeeId()).map(Employee::isActive).orElse(false))throw new ApiException(400,"员工已停用");c.setOwnerUsername(target.getUsername());c.setOwnerId(target.getEmployeeId());}
 public record Transfer(Long version,String ownerUsername,String reason,boolean release){}
 @Transactional public Customer transfer(Account a,String id,Transfer i){admin(a);Customer c=locked(id);version(c,i.version());String reason=CustomerService.text(i.reason(),2000);if(reason.isBlank())throw new ApiException(400,"请填写交接原因");String previous=c.getOwnerUsername();if(i.release()){
   if(Set.of("won","archived","pending","legacy").contains(c.getLifecycle()))throw new ApiException(400,"仅已完成有效报备的活跃客户可释放公海");c.setLifecycle("pool");c.setOwnerId(null);c.setOwnerUsername(null);c.setPoolAt(Instant.now());c.setProtectionExpiresAt(null);c.setNextFollowupAt(null);
  }else {if(CustomerProtection.inPool(c))throw new ApiException(409,"公海客户请先认领");assign(c,i.ownerUsername());}
  audit(c,a.getUsername(),"transfer","由 "+previous+" 交接至 "+c.getOwnerUsername()+"；"+reason);String key="transfer:"+UUID.randomUUID();notice(previous,c,key,"客户已由主管交接");notice(c.getOwnerUsername(),c,key,"主管交接了客户给您，原保护期限保持不变");return customers.saveAndFlush(c);
 }
 @Transactional public Customer activate(Account a,String id,Long version){admin(a);Customer c=locked(id);version(c,version);if(c.isHistoricalReviewed())throw new ApiException(409,"已完成历史纳管，不能重复续期");if(!"legacy".equals(c.getLifecycle()))throw new ApiException(400,"仅历史客户可执行纳管");if(!complete(c))throw new ApiException(400,"请先补齐 5 项报备资料");if(c.getOwnerUsername()==null)throw new ApiException(400,"请先确认归属账号");if(Set.of("won","invalid").contains(c.getStage()))throw new ApiException(400,"请通过主管成交确认或归档处理");
  number(c);c.setHistoricalReviewed(true);c.setLifecycle("protected");c.setProtectionStartedAt(Instant.now());c.setProtectionExpiresAt(c.getProtectionStartedAt().plus(CustomerProtection.PERIOD));audit(c,a.getUsername(),"activate","核实历史资料并首次纳入 90 天保护");return customers.saveAndFlush(c);
 }
 public static boolean complete(Customer c){return c.getName()!=null&&!c.getName().isBlank()&&c.getPhone()!=null&&c.getPhone().matches("(?:1[3-9][0-9]{9}|0[0-9]{9,11})")&&c.getAddress()!=null&&!c.getAddress().isBlank()&&c.getBuildingArea()!=null&&c.getBuildingArea().signum()>0&&Set.of("新建","翻建","改造","装修").contains(Objects.toString(c.getBuildingType(),""))&&c.getExpectedBuildTime()!=null&&!c.getExpectedBuildTime().isBlank();}
 @Transactional public Customer invalidate(Account a,String id,long followId,Long version,String reason){admin(a);Customer c=locked(id);version(c,version);Followup f=followups.findById(followId).orElseThrow(()->new ApiException(404,"跟进不存在"));if(!historyIds(c).contains(f.getCustomerId()))throw new ApiException(403,"跟进不属于此客户");if(reason==null||reason.isBlank())throw new ApiException(400,"请填写审核理由");f.setEffective(false);f.setReviewReason(CustomerService.text(reason,2000));followups.saveAndFlush(f);
  if("protected".equals(c.getLifecycle())){Instant base=c.getProtectionStartedAt();if(base==null)base=c.getCreatedAt();for(String cid:historyIds(c))for(Followup row:followups.findByCustomerIdOrderByCreatedAtDescIdDesc(cid))if(row.isEffective()&&CustomerService.ACTIVITIES.contains(row.getKind())&&row.getCreatedAt().isAfter(base))base=row.getCreatedAt();c.setProtectionExpiresAt(base.plus(CustomerProtection.PERIOD));}
  audit(c,a.getUsername(),"review","跟进 #"+followId+" 不计续期："+reason);return customers.saveAndFlush(c);
 }
 public record Merge(Long version,String targetId,Long targetVersion,String reason){}
 @Transactional public Customer merge(Account a,String id,Merge i){admin(a);lock();if(Objects.equals(id,i.targetId()))throw new ApiException(400,"不能关联自身");Customer from=locked(id),to=locked(i.targetId());version(from,i.version());version(to,i.targetVersion());if(i.reason()==null||i.reason().isBlank())throw new ApiException(400,"请填写核实依据");
  if(Set.of("won","archived").contains(from.getLifecycle())&&!from.getLifecycle().equals(to.getLifecycle()))throw new ApiException(409,"请保留已确认成交或归档客户为主档；恢复开发需单独审批操作");
  if("protected".equals(from.getLifecycle())&&!CustomerProtection.inPool(from)&&(!"protected".equals(to.getLifecycle())||CustomerProtection.inPool(to)||!Objects.equals(from.getOwnerUsername(),to.getOwnerUsername())))throw new ApiException(409,"原档案仍受保护，请保留该档案为主档，或先通过主管交接处理归属争议");
  if(sources.findByCustomerId(id).stream().anyMatch(s->!s.getOriginCustomerId().equals(id)))throw new ApiException(409,"请先解除已有下级关联，避免多层合并");
  registerSource(from,"archive",from.getId(),from.getName(),from.getPhone(),from.getWechat(),from.getDemand());registerSource(to,"archive",to.getId(),to.getName(),to.getPhone(),to.getWechat(),to.getDemand());
  for(Source s:sources.findByCustomerId(id)){s.setCustomerId(to.getId());sources.save(s);}from.setMergedInto(to.getId());from.setPhoneKey(null);audit(to,a.getUsername(),"merge","关联原客户 "+from.getId()+"，保留主档资料及归属；依据："+i.reason());customers.saveAndFlush(from);return customers.saveAndFlush(to);
 }
 @Transactional public Customer unlink(Account a,String id,String sourceId,Long version,String reason){admin(a);Customer target=locked(id);version(target,version);Source source=sources.findById(sourceId).orElseThrow(()->new ApiException(404,"来源不存在"));if(!source.getCustomerId().equals(id)||source.getOriginCustomerId().equals(id))throw new ApiException(400,"只能解除从其他档案关联的来源");if(reason==null||reason.isBlank())throw new ApiException(400,"请填写解除依据");Customer original=locked(source.getOriginCustomerId());for(Source s:sources.findByOriginCustomerId(original.getId())){s.setCustomerId(original.getId());sources.save(s);}original.setMergedInto(null);original.setPhoneKey(null);original.setLifecycle("pending");original.setStage("new");original.setProtectionExpiresAt(null);original.setPoolAt(null);audit(target,a.getUsername(),"unlink","解除来源关联并恢复待核实档案："+reason);customers.saveAndFlush(original);return customers.saveAndFlush(target);}
 @Transactional public Customer restore(Account a,String id,Long version,String reason){admin(a);Customer c=locked(id);version(c,version);if(!Set.of("won","archived").contains(c.getLifecycle()))throw new ApiException(400,"仅成交或归档客户可恢复");if(reason==null||reason.isBlank())throw new ApiException(400,"请填写恢复原因");c.setLifecycle("pending");c.setStage("new");audit(c,a.getUsername(),"restore","恢复为待核实客户："+reason);return customers.saveAndFlush(c);}
 public Map<String,Object> workbench(Account a){
  admin(a);List<Customer> all=customers.findAll().stream().filter(c->c.getMergedInto()==null).sorted(Comparator.comparing(Customer::getId)).toList();
  Map<String,Account> accountMap=new HashMap<>();accounts.findAll().forEach(x->accountMap.put(x.getUsername(),x));Set<String> activeEmployees=new HashSet<>();employees.findAll().stream().filter(Employee::isActive).forEach(e->activeEmployees.add(e.getId()));
  List<Customer> handover=all.stream().filter(c->{Account owner=accountMap.get(c.getOwnerUsername());return c.getOwnerUsername()!=null?(owner==null||!owner.isEnabled()||owner.getEmployeeId()!=null&&!activeEmployees.contains(owner.getEmployeeId())):c.getOwnerId()!=null&&!activeEmployees.contains(c.getOwnerId());}).toList();
  Map<String,List<Customer>> identities=new LinkedHashMap<>();for(Customer c:all){String phone=Objects.toString(c.getPhone(),"").replaceAll("[^0-9]","");if(phone.startsWith("86")&&phone.length()==13)phone=phone.substring(2);if(!phone.isBlank())identities.computeIfAbsent("phone:"+phone,k->new ArrayList<>()).add(c);String wx=Objects.toString(c.getWechat(),"").trim().toLowerCase(Locale.ROOT);if(!wx.isBlank())identities.computeIfAbsent("wechat:"+wx,k->new ArrayList<>()).add(c);}
  List<Customer> pending=all.stream().filter(c->"pending".equals(c.getLifecycle())).toList(),historical=all.stream().filter(c->"legacy".equals(c.getLifecycle())).toList();List<List<Customer>> duplicates=identities.values().stream().filter(v->v.size()>1).toList();
  Map<String,Object> result=new LinkedHashMap<>();result.put("pending",pending.stream().limit(200).toList());result.put("historical",historical.stream().limit(200).toList());result.put("handover",handover.stream().limit(200).toList());result.put("duplicates",duplicates.stream().limit(100).map(v->v.stream().limit(20).toList()).toList());result.put("totals",Map.of("pending",pending.size(),"historical",historical.size(),"handover",handover.size(),"duplicates",duplicates.size()));result.put("cases",cases.findTop200ByStatusOrderByCreatedAtAsc("pending"));result.put("sourceCount",sources.count());result.put("customerCount",all.size());return result;
 }
}
