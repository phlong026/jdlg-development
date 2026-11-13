# 客户与项目关联整改

## K04 / K05 / K06

- K04：素材热度钻取传递并恢复日期、范围标签及统计快照，显式切换范围/刷新解除旧快照。
- K05：消息中心重新显示时复位旧操作忙状态，旧epoch响应不能覆盖当前偏好。
- K06：首次客户/分享详情沿用上级快照，显式下拉刷新清理as_of并重新分页。
- 失败证据：新增3条行为测试首先失败（范围0≠1、prefsBusy=true、刷新仍旧as_of）。
- 验证：`node --test miniprogram/tests/customer-workflow-rectification.test.js miniprogram/tests/phase3-messagecenter.test.js` 25/25通过；TS/实际运行JS同步；`git diff --check`通过。
- 未验证：微信真机切后台/弱网和真实多账号回归。

## K01 / K02 / K03 / K07 / G01

| 编号 | 最终行为 | 失败与回归证据 |
| --- | --- | --- |
| K01 | 客户列表与详情加入本人项目浏览/反馈/确认摘要；关联未浏览单列project_linked，浏览数保持0。只返回项目名称/类型/时间/ID，不返回正文、反馈原文、媒体或分享凭据。 | my_project_customers_test.go：只有项目客户也进入列表；同owner另一设计师无权；150条同事活动不挤掉本人；删除客户不复活；无浏览关联不伪造访问，先失败后通过。 |
| K02 | converted独立回显；权威读取本人触达的状态/是否分配/下次跟进时间，既有他渠道线索ID保持隐藏。同actor重试去重、另一actor首次触达留证，复用全局客户。 | 前端ID0/重读先失败后通过；customer_handoff_test.go验证actor隔离、同actor去重及另一actor首次记录。 |
| K03 | 每段400分享预算，返回share_window和独立share_cursor；客户/素材支持更早段、上一段、最新段，明细保留来源段，展示真实分段口径。 | my_visitors_test.go第401条客户可切段、换actor游标拒绝；前端窗口操作先失败后通过。 |
| K07 | can_convert_lead沿用主线身份分流（业主sales、B端partner、未选identity_pending，未知身份拒绝），同时检查业务联系授权、账号手机号和已转化状态；缺失显示合法补齐说明，真实提交仍原事务复验。 | 后端联系未就绪/就绪/已转化三种行为；前端授权说明先失败后通过。 |
| G01 | 本人客户选择API和可信customer_ref验证；项目Manage权限与客户只读分享图分开，跨公司素材转发可关联本人项目。精确预选不退回普通列表，不受400统计窗口限制。 | project_customer_relationship_test.go：历史关系、跨owner转发、另一actor、非规范ID、Manage撤销、未知ref、SQL参数绑定；创建hook与项目前端由其他通道整合。 |

### API契约

- `GET /api/v1/miniprogram/tracking/my-visitors/project-customers`：token+本人scope鉴权；支持 `q/limit/cursor/as_of/share_cursor`，返回 `items[{customer_ref,nickname,conversion_status,customer_handoff,project_activity_count}]`、`next_cursor/cursor`、`as_of`、`share_window`。`visitor=u:<id>`精确预选先验证当前本人关系和活跃账号，再返回唯一安全姓名及承接，未知/越权拒绝，不回其他客户。
- `GET .../my-visitors/entries`：新增 `customer_handoff:{converted,status,assigned,next_followup_at,contact_ready}`、`conversion_reason`、`project_activities`、`project_activities_truncated`、`share_window`。不返回手机号、管理lead/customer ID或他人跟进内容。
- `POST .../my-visitors/lead`：新增 `converted:true`；既有线索 `lead_id=0` 策略保留，事务内按本人来源触达去重。
- `share_cursor`与页面内`cursor`独立。窗口游标绑定actor、tenant、完整scope、owner和as_of，不能挪到另一账号/快照；显式刷新回最新窗口。
- `customer_ref`只接受规范 `u:<positive id>`，不接受CRM ID。项目仍用原管理scope，CRMID继续拒绝。`makeProjectCustomerLinkVerifier(db,svc)`每次重读同actor客户scope，以参数化EXISTS验证分享、本人触达或本人项目关系，拒绝删除客户/删除项目来源，无界历史不进入应用内存。
- 项目摘要SQL/内存均先按服务端ProjectDesigner过滤再LIMIT，每类最近100条并明确截断提示，完整记录进入项目阅读。只读关联事实不算浏览或反馈。

### 验证与回滚

- 未新增依赖、数据库列或表；复用现有tracking事实、profile批量读取、线索/授权事务、消息epoch和API请求层。
- 参数化查询/事务核对Go官方资料：[SQL参数](https://go.dev/doc/database/sql-injection)、[database/sql](https://pkg.go.dev/database/sql#DB.BeginTx)。
- 完整相关Go包 `CGO_ENABLED=0 go test ./tracking ./route ./model`通过，`go vet`通过。客户工作流+消息中心29项通过；运行JS语法检查通过，TS/JS同步。
- 原生WXML编译sharevisits/visitdetail/messagecenter均通过。扩大phase3-workbench曾发现内容保存旧响应用例失败，已上报内容通道处理，最终全套以主代理集成为准。
- 安全自审：新增SQL数据均绑定；新API复用token、活跃账号与scope；无凭据/外发；摘要不回正文和分享token。
- 回滚：revert本逻辑提交恢复旧接口/展示，无迁移回退；新增触达和customer_ref均为兼容事实，旧版忽略额外字段，不删除线索或项目。
- 未验证：真实微信弱网/后台/跨角色真机、生产MySQL完整角色UAT、大规模负载；SQL driver/内存/VM属于隔离证据。
