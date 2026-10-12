# 首版跟进规则配置与重算合同

本实现覆盖正式 PRD IN-01—03、AT-15 的确定性规则配置、窗口、可解释证据和重算要求。标签仅用于跟进参考，不表示心理意愿、购买能力或成交概率。代码和自动化测试通过不等于生产启用、真机验收或容量达标。

## 已确定范围

- 规则按已授权 owner 隔离。查看需要 can_view；保存、重算需要 can_view + can_rules。只管理所选单一 owner；不能将某归属权限扩大到其他归属。
- 默认 followup-v1：滚动近 30 天；已识别账号至少两个独立会话且有一条服务端收藏成功证据为中级；至少一条服务端明确咨询、预约或报价成功事实为高级。仅有有效访问为低关注；缺证据、匿名或不同归属使用不一致规则时为待判断。
- 会话每日贡献上限默认 10；每种真实业务动作每日贡献上限默认 1。每日按 Asia/Shanghai 自然日分桶。刷新同一会话、心跳、按钮点击和反复上报不会无限加权。时长不单独升级标签。
- 评估窗口为 [as_of 减 window_days, as_of]，包含端点的已接受事实；与页面报表的筛选范围独立，页面筛选不悄悄改变标签含义。返回明确的 window_start/window_end/calculated_at。
- 固定排除匿名身份、预览、自访、测试、异常、重复、客户端点击和未经服务端验证的业务行为；排除项不可通过该配置关闭。业务证据必须能关联同窗口内的有效访问会话。
- 不支持任意表达式、脚本、模型评分、自动外联或动态扩展成功动作枚举。

## 管理接口

前缀 `/api/v1/management/tracking`。均由既有管理会话、tracking_access 与 owner 验证保护；写操作继续要求 Origin 和 `X-Tracking-Request: 1`。

### GET /intent-rules?owner=owner-a

无写权限但有查看权限的员工也能只读规则说明。返回 `{code:0,data:{current,history}}`，history 按 revision 倒序，始终含默认首版。各元素字段：

- rule_version、revision、owner_id、effective_at、actor、reason
- window_days、medium_sessions、medium_favorites、high_business_results、daily_session_cap、daily_action_cap
- timezone、participating_metrics、exclusions

默认首版尚无人工配置时 effective_at 为零时间、actor 为 0。自定义版本保存真实服务端生效时间和操作者。

### POST /intent-rules?owner=owner-a

请求是上述六个可配置整数加 `expected_rule_version`、`reason`。字段平铺；未知字段拒绝。字段范围：

- window_days：1–90，且不得超出已配置汇总保留天数
- medium_sessions：2–100
- medium_favorites、high_business_results：1–30
- daily_session_cap：1–10
- daily_action_cap：1–5
- reason：非空，最多 500 个字符

中级会话/收藏阈值和高级业务阈值不能超过窗口每日上限允许的最大贡献。每次保存创建新不可变版本，保留旧配置；版本指针、当前事实重算和审计同一事务提交，任何失败全部回滚。

返回 `{current,history,recomputed}`。recomputed 为 `{rule_version,evaluated_count,changed_count,as_of}`，changed_count 包含首次建立审计基线以及已有结果或贡献证据发生变化的账号。修改规则后列表按新版本现算；旧 as_of 查询选择当时有效版本，不将旧报表暗改为新规则。

并发或重复保存旧 expected_rule_version 返回 HTTP 409 `规则版本已变化，请刷新`。调用者应先重新读取，比较实际版本和参数，保留用户未保存输入；不能用旧基线盲重试。

### POST /intent-rules/recompute?owner=owner-a

请求：`{request_id,reason,rule_version}`。request_id 是 8–128 字符幂等标识，允许字母、数字、短横线、下划线和冒号；重试保留原 ID 和请求。reason 非空最多 500 字。rule_version 必须与所选归属当前版本一致。

同步重算所选归属窗口事实，以及旧评价记录中不再有证据的账号。返回 `{rule_version,evaluated_count,changed_count,as_of}`。同 actor/owner/request_id 与相同内容返回原结果，不重复重算或增加审计；不同内容复用 ID 返回 409 `重算标识已用于不同请求`。旧版本新请求返回 409。

这是有界同步重算。超过既有每请求 20,000 条/32 MiB 读取预算返回 422，整个事务回滚，不能声称部分结果是完整结果；大规模范围需后续分片任务方案与容量验证。

## 标签与证据字段

原 level/reasons/rule_version/window_start/window_end/calculated_at/original_rule/override/manual_note 保持兼容，新增：

- metrics：sessions、favorites、business_results，均为窗口内经过每日上限后的贡献数
- thresholds：此次生效的六个配置参数
- participating_metrics、exclusions、timezone
- override_state：none、active 或 expired；生效人工覆盖含 actor、created_at、reason、expires_at，原规则结果仍单独可查看
- evidence：最多 100 条已授权证据，含 kind、record_id、session_id、occurred_at，页面证据附 page_view_id/content_id/content_version，业务证据附 action
- evidence_truncated：证据展示被截短时为 true；计算和指纹仍使用全部有效贡献，不以截短数据计算标签
- scope_required：多归属规则不一致时为 true；列表仍正常返回，标签为 unknown，提示选择单一归属查看规则

参与指标是支持的服务端事实类型，不表示所有业务成功源都已接通。证据不包含联系方式、需求正文或 CRM 私有内容。

## 人工修正历史与分页快照

`POST /intent-overrides` 保持等级、原因、≤30天有效期及独立规则权限的原接口，不更改意向阈值。每次决定在租户事务锁内分配 owner 级递增 revision，追加完整不可变 `intent_override_history`，保存 level、reason、expires_at、actor、created_at、revision 及 previous_decision_id。`intent_override` 只作兼容的最新指针，不能替代历史。历史按授权 owner + visitor 查询；匿名不会因人工修正变成真实身份。

- 在 as_of 之前的决定中按精确 created_at、revision 选择最新一条；相同时间由 revision 确定顺序。JSON 保留时间的完整精度，不将 SQL DATETIME(6) 截断时间当成决定的精确生效时间。替换后原理由、有效期和操作者仍可审计；最新决定到期时回归原规则，不能重新激活更早未到期决定。
- 访客列表的 next_cursor 绑定同一查询、as_of 和 owner 级 revision 上限。晚于快照或相同时间的后续修正不会改变这个游标的筛选成员及 filtered counts。所有成功访客页（包括空页和单页）另返 first_cursor；回到第一页必须保留原 as_of、筛选和 limit，把 first_cursor 原样作为 cursor。刷新或改变筛选才开始新快照。未带游标的查询按 as_of + 当前已知 revision 确定性选取；若要求区分同一时间戳内的先后，必须保留返回游标。旧格式游标没有 revision 上限，升级后返回 409，要求显式刷新。
- 现存单行覆盖在当前查询仍可用。首次替换时仅将实际存在的最后一条决定完整导入历史；不会从旧的仅等级审计伪造已丢失的理由、到期或操作者。早于最后已知旧决定的查询只对受影响 owner + visitor 返回 409。普通无覆盖账号及其他 owner 不因此受阻，旧快照在首次导入后仍可继续使用原游标。
- 如果服务器时钟倒退至该访客已有决定之前，写入返回 409，不创建倒序历史；正常相同时间并发由事务序列分配不同 revision。历史游标不得把当前评价指纹倒写回旧 revision。

完整决定保存在可按访客删除的不可变历史中。永久通用 audit 记录操作者、决定 ID、前一决定 ID、revision、等级及完整决定摘要；通过 ID 和摘要核验完整理由/到期/操作者，不在永久日志另存自由文本理由。新增历史、当前指针、revision 序列和 audit 同事务提交，任一失败全部回滚。

个人删除会清除对应 owner 的当前决定、历史和不可恢复边界；完整身份涉及未授权 owner 时整体拒绝，不能留下部分历史。历史和边界纳入既有人工审核的支持记录保留期。清理已批准的过期历史后，只为受影响身份保留不含理由的 `intent_override_boundary`，使早期不可重建快照返回 409；该边界也受个人删除和支持记录保留政策约束。`intent_override_clock` 仅含 owner 级序列，不含 visitor 或理由，保留以避免 revision 重用。没有新增自动清理计划，真实数据库/恢复演练仍待验收。

## 真实业务接入边界

- 收藏只在现有收藏业务提交成功后调用 ConfirmFavorite；客户端不能提交“成功”事件。
- 独立 `POST /customer-service/consultation-requests` 表示明确主动请求人工咨询。父路由在 CreateLead 和服务端 consultation_request_submitted 业务事件均成功后，调用 ConfirmConsultation(ctx, entryToken, authenticatedAccount, leadID)。普通 lead 自动保存、生成图和通用 /events 不调用这个 hook。
- ConfirmConsultation 验证 entry token、有效期、可信账号一致、删除墓碑以及非预览/自访；归属和会话来自服务端 entry，不相信客户端 session_no、owner 或 tenant。仅保存 lead ID 与会话，不传入电话号码、需求或备注正文。
- 每次查询及重算会复核真实源：收藏必须仍存在于同账号/目标的 user_favorites（取消是硬删除）；咨询 lead 必须仍属同账号且为 new/contacted/converted 状态。已取消、已删除或 invalid 的源即时排除。源库查询失败时整个查询返回 503，不能继续显示旧高/中标签；复核使用原请求取消信号、总计 2 秒预算及现有 tracking SQL 事务连接，不额外占用第二个连接。
- 同一 lead 只贡献首次成功记录；现有 CreateLead 会复用 lead，所以后续同 lead 请求保守少计，不声称逐次提交计数。按业务 ID 幂等防止重试刷新成功时间。采集失败不阻断原业务。
- 已核完整远程代码树：尚无可直接接入的真实预约成功源；员工报价跟进、客户端估价、支付价格与建规知识问答不能冒充客户报价/服务咨询请求。企微客服消息虽真实持久化，缺少可信小程序会话/归属桥及可靠业务 proof，因此本轮不接。

## 存储、审计、删除与回放

复用 tracking_records，不新增账号体系或数据库表。

- intent_override_history：owner + visitor + revision 的完整不可变决定
- intent_override：兼容的最新决定指针
- intent_override_clock：不含访客信息的 owner 级序列
- intent_override_boundary：仅受影响身份的不可恢复历史边界
- intent_rule：owner + rule_version 的不可变历史
- intent_rule_current：每 owner 当前版本指针
- intent_evaluation：owner + visitor 的当前评价指纹、level、版本和 as_of，仅用于比较变化，不作为查询标签缓存
- intent_recompute_request：重算幂等请求签名和聚合结果，不存访客明细
- audit：intent_rule_update、intent_recomputed、intent_recompute_requested；含操作者、时间、变化前后版本和等级，目标使用散列键，不复制业务正文

真实业务源的当前撤销状态优先于历史 as_of：规则版本可以按旧快照选择，但不会用历史快照恢复已撤销的收藏或已失效 lead。接口不承诺外部业务源的历史状态重放。

每次客户查询根据有权事实重新计算。贡献事实被删除、判异常或身份解除后不会返回旧缓存标签；显式重算也会将曾存在、现在无证据的旧账号记为待判断并留变化审计。历史快照不能倒写更新过的评价状态。身份删除同时删评价指纹，并以永久墓碑阻止重放恢复个人事实。评价和重算幂等记录纳入既有汇总保留维护；规则历史和审计不随日常汇总清理被改写。

## 自动化证据

`service/tracking/rules_test.go` 覆盖窗口与报表范围独立、版本历史/旧快照、改规则自动重算、权限、非法阈值、每日会话/行为上限、重复动作与无有效访问的伪业务不升级、多归属混配、重算幂等冲突、源删除/异常、身份解除、人工覆盖过期、删除后重放、真实咨询账号校验，以及审计失败原子回滚。

`service/route/share_tracking_rules_test.go` 覆盖 owner 越权、只读员工写入、重算写权限和未知请求字段；`share_tracking_business_test.go` 验证真实源 SQL 的账号/目标约束、取消/失效/删除排除、未知源拒绝、数据库错误脱敏和调用方取消。规则测试还覆盖纳秒级版本边界、相同标签推进时间水位以防旧快照倒写，以及运行时源撤销重算。既有查询权限/预算/删除测试继续覆盖新实现依赖的底层范围保证。生产 MySQL、真实设备及容量验收仍是独立门槛。

`service/tracking/intent_override_history_test.go` 保留原业务复现，并覆盖完整旧理由/到期/操作者、审计摘要、最新决定到期不复活前一条、同时间及晚于快照的多页筛选/计数/返回首页、旧单行首次迁移、精确409范围、并发revision、时钟倒退、SQL时间精度、审计失败原子回滚、跨owner删除和人工保留维护。kind名称均不超过现有SQL的VARCHAR(24)，不依赖未执行的新表迁移。
