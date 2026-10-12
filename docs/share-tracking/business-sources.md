# 已验证业务成功源审计
固定基线：phlong026/jdlg-development @ c4e761d7a54cead8b1f43bec9fe391d7bcf3db89。核查时间：2026-10-11 UTC。

## 结论
- 可以最小接入的候选是 mini 主动咨询请求成功落库：/customer-service/leads → CustomerServiceModel.CreateLead。必须区分 handoff_human 与自动保存需求/生成图；现有任意 lead 行不能直接判高意向。
- 远程 customer-management 确实存在已持久化业务数据：phase1 员工报价/沟通跟进，legacy 企微客服收到的消息，以及会话存档。不能因为本地未下载 Java 而声称全仓没有咨询业务。
- 没有发现已实现的客户预约提交成功业务源。已有 nextFollowupAt 是员工跟进提醒；不是客户预约。
- 没有发现已实现的客户报价请求成功业务源。mini aicost 是客户端估算；phase1 quotation 是员工跟进；service_quote 是设计师资料价格；report.PaymentQuote 是报告支付价格。以上均不能冒充主动报价请求。
- 业务成功、客户主动性、可信身份归属是三个不同条件。任意一个缺失，均不能自动产生高意向事实。

## 核查覆盖及证据
GitHub 仓库读取接口 读取固定 SHA 的完整递归 tree：3711 条，truncated=false。完整读取 customer-management/src/main/java 下全部 378 个 Java 文件，以及 phase1/src/main/java 全部 19 个 Java 文件、5 个 SQL migration；关键词检查和具体调用链复核均使用固定 SHA。
远程 tree 含 background 183、customer-management 793、docs 330、miniprogram 1652、service 296 个 blob。
Go/mini 的主要证据文件另通过 GitHub 仓库读取接口 重新读取，并核对本地 git blob hash 与该固定 SHA 完全相等（核查时；并行实现随后可能改变工作区）：
- service/route/customer_service.go：2fbc603a5d70125ad13c12e983ee23b6ea9ffd83
- service/model/customer_service.go：230c5d11bdcca364dda1a6087e003e1fe904de34
- service/route/crm_sources.go：20f15982fd0d0e8a3ff418ed8b587769fcfae13c
- miniprogram/miniprogram/pages/customerservice/customerservice.ts：8f0bd1eab95e8552a786a9d3d7787693d38130f0
- miniprogram/miniprogram/pages/aicost/aicost.ts：7aabc4b918f73f390ae96b47d408a24d7351c984
- service/route/building_code.go：1cf77d484b0f6d400e2da7d4f9feb727479bd422
- service/model/building_code_chat.go：641e80b338f1df911370fcb39e29d84c5c944328
本报告是固定基线的静态源码审计，不能证明线上部署或真实数据库中的记录存在。

## 1. mini 客服线索提交：最小可接候选
源代码：
- [route/customer_service.go L89-L160](https://github.com/phlong026/jdlg-development/blob/c4e761d7a54cead8b1f43bec9fe391d7bcf3db89/service/route/customer_service.go#L89-L160)
- [model/customer_service.go L339-L382](https://github.com/phlong026/jdlg-development/blob/c4e761d7a54cead8b1f43bec9fe391d7bcf3db89/service/model/customer_service.go#L339-L382)
- [mini customerservice L472-L499](https://github.com/phlong026/jdlg-development/blob/c4e761d7a54cead8b1f43bec9fe391d7bcf3db89/miniprogram/miniprogram/pages/customerservice/customerservice.ts#L472-L499)
- [mini customerservice L741-L841](https://github.com/phlong026/jdlg-development/blob/c4e761d7a54cead8b1f43bec9fe391d7bcf3db89/miniprogram/miniprogram/pages/customerservice/customerservice.ts#L741-L841)

持久化：customer_leads INSERT 或已有行 UPDATE；返回 lead ID。CreateLead 在写入后还 backfillLeadIDForEvents，全部 err==nil 才路由返回 code=0。
可信 mini user：codeSession.UserID 来自 GetTokenCodeSession，不是 body.user_id。
不可信/缺失：req.session_no、source_task_no、remark、payload.reason 均客户端输入；lead 表无 tracking tenant/owner/session，assigned_to 初始空，不能等同分享归属。source_task_no 没有在此验证属于用户。
提交语义：同一个 createLead 被 structured_requirement、generate_click、handoff_human 调用。前两者不能仅因服务器存了 lead 就解释为主动找人工咨询。
失败语义：handoffHuman 在 createLead 失败后仍记录 human_handoff_triggered 并打开企微；所以事件或拉起成功不证明线索提交成功。
重复语义：findExistingLeadID 按 user+source_task_no 或 user+session_no 查；找不到还 fallback findLatestOpenLeadID。故 lead ID 不是独立提交 ID；旧 lead.created_at 不是后续新咨询的成功时间。UPDATE 会覆盖 session/remark/source 信息。
事务风险：现有 CreateLead 的 UPDATE/INSERT 与 backfill 是独立 SQL；backfill 失败时可能已写入 lead，但路由返回失败。若新增证明要可靠重试，建议事务内 outbox 或独立持久化提交回执，避免“成功业务无证据”和重试刷时间。

最小 hook：
1. 在同一既有业务提交中显式标注“请求人工咨询”，保留需求内容和来源，不把按钮遥测本身当完成。
2. 在 CreateLead 返回成功后，将服务端读取的 lead ID/user 交给服务器内 helper。未成功时不确认。
3. 使用 tracking entry token 解析真实 Entry，要求当前认证 mini user 与 Entry.Account 相等，且 consent、删除/异常/预览/自访等条件通过；session/owner/tenant 只能从服务端 Entry/仓库作用域派生。
4. 记录独立不可变 success proof（或者明确保守按 lead ID 去重），包含 source kind、business record ID、action、occurred_at、user 和可信 tracking binding。
5. 后续规则必须回查源记录仍存在有效；删除、撤回、解除身份关联需失效并重算。统计窗口看实际提交成功时间，不看读取或重放时间。

明确禁止：把 /customer-service/events 中客户端可填写的 event_type=lead_submit、human_handoff_triggered、intent_level=high 或 lead_id 当可信服务器确认。该路由 L47-L86 接收这些值，没有验证 lead_id 属于当前 user。

## 2. Go → phase1 CRM ingestion：可靠来源桥，但不是新的意向行为
- [service/route/crm_sources.go L15-L71](https://github.com/phlong026/jdlg-development/blob/c4e761d7a54cead8b1f43bec9fe391d7bcf3db89/service/route/crm_sources.go#L15-L71)
- [CustomerWorkflow.java L40-L54](https://github.com/phlong026/jdlg-development/blob/c4e761d7a54cead8b1f43bec9fe391d7bcf3db89/customer-management/phase1/src/main/java/cn/iyque/phase1/CustomerWorkflow.java#L40-L54)
- [ManagementBridgeFilter.java L50-L75](https://github.com/phlong026/jdlg-development/blob/c4e761d7a54cead8b1f43bec9fe391d7bcf3db89/customer-management/phase1/src/main/java/cn/iyque/phase1/ManagementBridgeFilter.java#L50-L75)

Go 将真实 lead.id 作为 externalId、lead.user_id 作为 submittedBy，经签名 @source 内部请求 POST /ingest/miniprogram。Java @Transactional ingest 写 crm_customer、crm_source，使用 miniprogram + hash(externalId) 去重。返回 200+customerId 后 Go 写 customer_lead_crm_delivery checkpoint。
这证明“来源接入成功”，不是第二次客户咨询；不能计两次。客户最初 pending、未分配 owner；不存在分享 session/owner/tenant 输入。可用作 CRM 记录定位桥，不可直接填高意向。

## 3. phase1 CRM 员工沟通/报价跟进：真实持久化，非客户报价请求
- [CustomerService.java L157-L166](https://github.com/phlong026/jdlg-development/blob/c4e761d7a54cead8b1f43bec9fe391d7bcf3db89/customer-management/phase1/src/main/java/cn/iyque/phase1/CustomerService.java#L157-L166)
- [ApiController.java L38-L40](https://github.com/phlong026/jdlg-development/blob/c4e761d7a54cead8b1f43bec9fe391d7bcf3db89/customer-management/phase1/src/main/java/cn/iyque/phase1/ApiController.java#L38-L40)
- [Models.java L43-L59](https://github.com/phlong026/jdlg-development/blob/c4e761d7a54cead8b1f43bec9fe391d7bcf3db89/customer-management/phase1/src/main/java/cn/iyque/phase1/Models.java#L43-L59)

POST /customers/{id}/followups → @Transactional CustomerService.follow。
kind 允许 followup/communication/drawing/quotation；outcome 为员工选择 contacted/no_answer/no_reply/internal。contacted 才 effective；drawing/quotation+contacted 要求客户范围内附件。followups.save(row) 与 customers.saveAndFlush(c) 同事务。真正成功时点为事务提交，不能在 saveAndFlush 但尚未 commit 时跨服务发确认。
有真实 CRM customer、当前认证 operator、owner 权限校验；无直接 mini tracking session/tenant。经 crm_source 可追溯 mini lead，但不代表此报价是客户主动请求。
适合展示“员工已记录报价跟进，待人工核实”，不适合自动 server_quote。若未来新业务提供客户主动请求凭证，可在事务提交后 outbox 接入；现有字段不足。
nextFollowupAt 只是下次联系时间，不是客户预约。

## 4. legacy 企业微信客服消息：真实客户主动消息候选，身份桥缺失
- [IYcallbackController.java L84-L100](https://github.com/phlong026/jdlg-development/blob/c4e761d7a54cead8b1f43bec9fe391d7bcf3db89/customer-management/src/main/java/cn/iyque/controller/IYcallbackController.java#L84-L100)
- [IYqueKfServiceImpl.java L114-L186](https://github.com/phlong026/jdlg-development/blob/c4e761d7a54cead8b1f43bec9fe391d7bcf3db89/customer-management/src/main/java/cn/iyque/service/impl/IYqueKfServiceImpl.java#L114-L186)
- [IYqueKfServiceImpl.java L251-L284](https://github.com/phlong026/jdlg-development/blob/c4e761d7a54cead8b1f43bec9fe391d7bcf3db89/customer-management/src/main/java/cn/iyque/service/impl/IYqueKfServiceImpl.java#L251-L284)
- [IYqueKfMsgServiceImpl.java L78-L149](https://github.com/phlong026/jdlg-development/blob/c4e761d7a54cead8b1f43bec9fe391d7bcf3db89/customer-management/src/main/java/cn/iyque/service/impl/IYqueKfMsgServiceImpl.java#L78-L149)
- [IYqueKfMsgSub.java](https://github.com/phlong026/jdlg-development/blob/c4e761d7a54cead8b1f43bec9fe391d7bcf3db89/customer-management/src/main/java/cn/iyque/entity/IYqueKfMsgSub.java)

签名解密回调后，服务器通过 WeCom syncMsg 拉消息，saveIYqueKfMsg 异步调用 customerBatchGet 补充 external contact，再 iYqueKfMsgSubDao.save。
数据含 id/msgId/externalUserId/unionid/openKfid/origin/sendTime/switchUserId。实体注释明确 origin=3 客户消息；4 系统事件、5 员工消息。
必须仅用 origin=3 且真实内容落库的记录。不能以回调 HTTP 成功、拉取 cursor 保存或异步方法被调用当成功。
现有不足：异步异常仅日志；只取每客户最后一条并在 hasMore=0 分支处理；msgId 未声明唯一约束；不保证完整可靠事件流。corp 来自服务配置，记录本身无 tenant；unionid/externalUserId 未映射 mini tracking user，switchUserId 接待员工也不等于分享 owner。
可接点为 save 的真实事务提交后、带唯一 corp+msgId 的 outbox；仍需明示身份绑定和来源范围桥。未接通前应保持 unavailable/unverified，不从姓名/手机号/unionid 字符串猜关联。

## 5. legacy 企业微信会话存档：真实消息持久化，需额外限定
- [IYqueMsgAuditServiceImpl.java L163-L244](https://github.com/phlong026/jdlg-development/blob/c4e761d7a54cead8b1f43bec9fe391d7bcf3db89/customer-management/src/main/java/cn/iyque/service/impl/IYqueMsgAuditServiceImpl.java#L163-L244)
- [IYqueMsgAuditServiceImpl.java L251-L309](https://github.com/phlong026/jdlg-development/blob/c4e761d7a54cead8b1f43bec9fe391d7bcf3db89/customer-management/src/main/java/cn/iyque/service/impl/IYqueMsgAuditServiceImpl.java#L251-L309)

服务器 getChatDatas → getDecryptData → handleMsg 在线程中 yqueMsgAuditDao.saveAndFlush，存 msgId/fromId/acceptId/acceptType/content/dataSeq/msgTime。
这是已收到的聊天记录，不是每条都为询价/预约；群消息、员工主动消息、系统/异常都应排除。现有前缀 wm/wo 判断用于客户分类，不能替代真实身份绑定。缺少 mini/session/分享 owner/tenant；不建议为本次最小功能接通整个敏感会话存档。若后续纳入，需明确用途/范围并只传最小事件证明，不导入正文为埋点。

## 6. 建规 AI 问答：有真实持久化，但业务语义不同
- [building_code.go L200-L254](https://github.com/phlong026/jdlg-development/blob/c4e761d7a54cead8b1f43bec9fe391d7bcf3db89/service/route/building_code.go#L200-L254)
- [building_code_chat.go L68-L106](https://github.com/phlong026/jdlg-development/blob/c4e761d7a54cead8b1f43bec9fe391d7bcf3db89/service/model/building_code_chat.go#L68-L106)

认证 UserID，校验建规会话属于用户，CreateSession/AppendMessage 写 building_code_chat_sessions/messages。用户提问保存后才调用 AI，助手回答也持久化并标识 ok/error/fallback。但保存异常只是日志且流程继续；AppendMessage 未返回 message ID。此类问知识问题不等于请求人工服务/预约/报价，应作为普通使用行为，不能直接提升为销售高意向。

## 7. 需要明确排除的假阳性
- aicost.onEstimateCost：estimateTotalCost 后 wx.setStorageSync，再 navigateTo；没有服务器报价请求或成功 ID。
- solutiongeneration 的 quotation tab 是 UI 标签；onConfirm 仅 showToast。
- service_quote：设计师资料报价数字，不是访客请求。
- report.PaymentQuote：付费报告的支付价格查询，不是报价需求。
- phase1 expectedBuildTime/nextFollowupAt：计划字段，不是客户预约成功。
- AI 根据关键词产生的 intent/high、员工手工 intent/high：推断/人工判断，不能转换为自动已确认业务事实。
- 收藏是另一条真实业务，应按既有收藏落库/取消和规则配置计中关注；不能重命名成咨询成功。

## 建议验收补充
1. 手动咨询真实提交成功→产生一次带业务ID与时点的证据；写业务失败→零证据。
2. generate_click、结构化需求保存、仅拉起企微、公共 /events 伪造 lead_submit→零高意向证据。
3. 用户 A 的 lead + 用户 B 的 entry token/session、跨 owner/tenant→拒绝关联。
4. 相同业务 ID 重试和重复消息→不新增、不刷新发生时间；窗口外旧记录不能用重算时间续期。
5. 业务已成功但 tracking 临时失败→不谎报业务失败；通过可靠 outbox/明确补偿恢复，且不重复确认。
6. 关联源删除、invalid、身份解绑、匿名、预览/自访/测试/异常→排除或撤销，重算可追溯。
7. 预约/报价没有接通真实业务源时保持未接入；不要为了 AT-15 样例伪造生产提交功能。


## 本分支最小接入结果

现有“对接人工”操作改走专用 `POST /customer-service/consultation-requests`，其他自动需求保存与图片生成仍走旧 `/leads`。专用指令保留原 `lead_submit` 漏斗事件，并额外写入最小 `consultation_request_submitted` 证明。只有在已认证账号的 CreateLead、原漏斗事件和固定证明均成功后，才调用域内 ConfirmConsultation。通用 `/events` 的同名事件不调用确认钩子。

分析证明只包含业务线索 ID、真实账号与服务端解析的访问上下文，不复制客户姓名、联系方式或需求正文。咨询页保持隐私排除；仅明确同意且已有可用访问上下文时提供可选请求头。无同意、旧账号、失效上下文、自访、预览、已删除身份不能生成该证据。

保守去重按线索 ID：同一线索重复提交不刷高，也不声称精确记录每次新的咨询请求。异步确认限时 500ms，失败可能少计，但不阻断已经提交的业务。未来需要强投递或逐请求计数时，应另评估业务事务 outbox/独立成功回执。预约和报价仍无符合上述条件的成功源；没有为凑埋点新建假业务。

## PR3 评审修复：收藏成功回执

通用收藏在业务事务提交后返回持久行 ID、账号、类型、目标及原始 created_at；INSERT IGNORE 重试读取同一已提交行，不以遥测收到的 UTC 日期生成新成功事实。删除后重新添加产生新行 ID。规则重算回查精确 ID、账号、类型、目标、创建时间；取消或被替换的旧源行不能被同一目标的新收藏重新证明。旧版没有 durable receipt 的业务事实保留原记录，但不再当作已验证收藏意向，不猜测补造源关联。

第一次迟到确认只接受发生在绑定 entry 开始时间附近的源回执，已有已验证同回执重试保留原时间；长时间后在新 session 重试不会刷入近期窗口。异步确认仍可能因进程终止或超时少计，没有新增可靠 outbox 保证。真实 MySQL 事务、取消/重加竞争和端到端登录仍待验，database/sql 驱动夹具及并发测试不能替代这些验收。
