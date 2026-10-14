# 分享访问接口与数据合同

## 实现状态

本合同描述开发分支的已实现接口，不表示生产已启用或AT-01至AT-17已全部通过。采集、内容、视频和意向开关默认关闭；需要明确经营范围授权，平台管理员不会自动获得客户轨迹。离线测试及构建证据见 rollout-and-verification.md；真实MySQL/Redis、微信、CRM、媒体持久盘、设备、浏览器视觉与负载尚未验收。

## 身份和权限

管理接口前缀 `/api/v1/management/tracking`。沿用管理session并重新检查 `management_access` 启用、用户类型及企微会话；再从 `tracking_access` 读取服务器配置的tenant/owner能力。`can_view`、`can_manage`、`can_crm`、`can_delete`、`can_rules`独立。多归属能力暂采用交集，宁可拒绝不明的跨归属操作；不把某归属管理权限提升为全部范围权限。写操作要求Origin同源/既有白名单及 `X-Tracking-Request: 1`。

小程序前缀 `/api/v1/miniprogram/tracking`。登录请求沿用既有签名认证，服务器注入账号，客户端字段不能指定账号/租户/销售。匿名只使用随机客户端/匿名标识。采集凭据以 `X-Tracking-Token` 携带，服务器仅保存其SHA256；它不能访问后台查询。既有登录签名可能读取已登录时产生的认证设备值；追踪模块不新建指纹，也不把该值写入事件。

## 公开和采集接口

- `GET /config`：安全开关、告知文字/版本、采样和队列限制。不含客户数据。
- `GET /contents/:id/versions/:version`：公开已发布自有正文；不要求同意分析。版本固定，撤回拒读。正文按已持久化的body_format渲染：plain保持转义文本，rich_text仅渲染服务器允许列表清洗结果；媒体单独授权。此接口不承诺原业务页面的历史正文回放。
- `POST /share-entry`（JSON scene，不把令牌置于请求URL日志）：校验不透明分享后返回公开目标route/content/version/token，不创建PV、不暴露收件人。
- `GET /media/:id`：管理session按素材范围，或媒体确实属于仍可访问的已发布版本。无缓存并每次复核撤回，不接受任意外部URL代理。
- `POST /entry`：`route_key,content_id?,content_version?,client_instance,anonymous_id?,share_token?,previous_entry_token?,consent_version`。返回受限entry_token、session_id、visitor_key、share_id、服务器确认的content_version及可访问状态。原业务公开详情的版本以服务端resolver为准，客户端后续事件及分享必须使用返回版本。非法/过期/不匹配分享回退直接访问，内容不可访问则404。新的真实分享入口、换账号和超时产生新会话；同客户端当前匿名会话登录可审计关联。
- `POST /shares`：`route_key,content_id?,content_version?,parent_share_id?,channel,idempotency_key`。服务端确认实际分享目标，返回share_id/share_token/expiry。不推断是否发送成功。
- `POST /events/batch`：`batch_id,events`，最多50条和64KiB，逐条accepted/duplicate/rejected。未知字段/事件/版本拒绝，永久错误不会无限重试。服务端校验绑定、发生时间、分段、视频媒体归属，客户端不能发送业务成功事实。

事件类型：share_intent、page_view、page_end、visible_segment、content_click、image_open、outbound_click、scroll_depth、video_state、video_progress。scroll_depth为0至100百分数。`business_action_confirmed`仅允许可信服务端业务适配，不开放客户端入口。真实收藏成功通过独立服务端ConfirmFavorite写入business事实，不接受客户端“成功”事件。

旧57条路由中20条公开源页面接入统一适配器，37条按明确业务隐私边界排除源页面采集；另新增1条通用阅读路由，共58条策略。排除项仍保留原生分享，公开目标可单独建立分享发起归因；私有源页面不产生PV。完整路径、成功就绪条件和排除依据见 miniprogram/docs/route-inventory.md。列表曝光、源页content_click与目标详情PV分别记录；8个已审计公开源页面的真实回调只发送固定动作键和合法目标ID，不记录搜索词、输入、联系方式或任意dataset。

## 管理接口

统一 `{code:0,data:...}`；错误使用实际HTTP 400/401/403/404/409/410/413/429/503并给安全提示。内部数据库、密钥、token和正文不进入错误回包。

- `GET /context` 返回独立能力和实际归属。
- `GET /visitors`、`/contents`、`/contents/:id/visitors`：一致的时间、归属、排序、游标、as_of、metric_version。客户筛选另有identity/intent/q；切素材清除不适用客户筛选并提示。
- `GET /visitors/:id/sessions`：可加content_id；`include_related=true`只展开访问过此素材的那些会话中的其他页面。
- `GET /page-views/:id/events`：脱敏事件分页，含补报/不完整标记。原始事件已按批准策略过期则HTTP 410，并在保留访问记录上标raw_events_expired；不以空列表伪装从未发生。每层独立做范围检查。
- `POST /contents`、`PATCH /contents/:id`：人工plain或rich_text草稿；expected_revision乐观锁；不能借编辑变更历史归属。富文本为受控HTML源码输入，支持p/br/strong/em/h2/h3/ul/ol/li/blockquote，移除所有属性、脚本、外部嵌入和危险命名空间；清洗后无正文则拒绝。图片/视频只走授权媒体。未指定格式保持纯文本兼容，保存后的清洗内容用于预览、发布及读取。
- `POST /contents/:id/preview`：当前版本预览令牌，15分钟，绑定操作者。
- `POST /contents/:id/publish`：预览令牌、expected_revision和idempotency_key；创建不可变版本。
- `POST /contents/:id/revoke`：原因、版本、幂等键；永久撤回墓碑，旧入口和媒体不继续公开。
- `GET /media`、`POST /media` multipart：受控私有盘上传，真实PNG/JPEG/GIF≤10MiB、MP4≤50MiB，权属声明；MP4必须可读取合法尺寸；无任意URL导入或远程抓取。
- `GET /contents/:id/code?version=N`：仅真实微信码开关及配置齐备时调用官方接口，超时/权限失败明确报错；不是伪造二维码。URL Link见下一条。
- `GET /contents/:id/url-link?version=N`：需TRACKING_WECHAT_URLLINK_ENABLED=true、对该素材归属有管理权限且该版本已发布；调用官方 generate_urllink 返回 `{url_link, expires_at}`，链接带该账号当日新建的分享令牌，有效期29天（早于令牌到期）。发布接口在开启后也会在 `share_methods.url_link` 返回。生成失败只返回说明，不影响发布。
- `GET /mp-sync`、`POST /mp-sync`：需TRACKING_MP_SYNC_ENABLED=true及对配置归属的管理权限。GET返回配置摘要与最近一次同步结果（含 `publishes:false`）；POST立即同步一次（同一进程内同时只运行一次，手动触发间隔不少于1分钟，运行期间返回409，过于频繁返回429），返回各项计数与最多20条未同步原因。同步只生成或更新草稿，不发布、不撤回、不覆盖人工修改。
- `POST /identity-links`、`DELETE /identity-links/:visitor`：明确依据、业务ID/编号、专门权限，必须经过当前操作者可信CRM接口的目标权限验证。
- `POST /intent-overrides`：等级、原因和≤30天有效期；仅专门规则权限。完整人工决定追加不可变历史并可按 as_of 回放，访客游标另绑定覆盖 revision；标签是跟进参考，不是购买概率。
- `POST /deletions`：专门权限和原因；删除当前系统可控事实/映射，保留防重放墓碑；外部导出和备份治理另行核验，不虚称全副本即时清除。返回 `status`：`completed` 表示全部删除完成；`in_progress` 表示数据量超出单事务预算，已不可读取但未删完，重复同一请求继续。其他值或错误都不能当作完成。细节见 [权限、保留及删除维护](governance-maintenance.md)。
- `GET /diagnostics/rejections?days=7`：需要查看权限；返回已授权归属最近 1–90 个 UTC 自然日、因事件时间超窗被拒的事件计数，按天倒序。`counts` 的键：`clock_ahead`（设备时间超前服务器 2 分钟以上）、`clock_behind`（事件早于本会话服务端开始时间 2 分钟以上）、`stale_queue`（离线队列超过 72 小时）。SDK 会丢弃被拒事件，所以计数持续增长说明有设备时钟偏差或积压，该用户在列表中会表现为“没有访问记录”。只含归属、日期和计数，不含访客、会话或事件标识。

## 数据字典与一致性

`tracking_access`复用users.id记录显式能力，不复制账号系统。

`tracking_records`为有类型的租户记录表。主键 `(tenant_id,kind,record_id)`；索引覆盖owner、visitor、content、share与时间，及session。kind区分content/version/media/share/share_request/entry/session/event/event_hash/sequence/view_binding/identity/crm/intent_override/business/audit/deletion/deletion_session/content_revoked/legacy_catalog/daily_summary/summary_state/rejection_stat等。rejection_stat 每个归属每个 UTC 日一行，只存计数，不纳入个人数据删除，也不在保留期清理范围内。JSON字段由Go结构定义且入口严格校验，不存任意用户payload。所有时间以UTC字符串写入DATETIME并显式按UTC读回，避免旧DSN loc=Local改变含义。

事件使用INSERT不可改写。event_hash防相同ID不同内容，sequence防相同分段序号冲突。事实接受、身份变更、删除、注释和意向评估在写事务内执行，租户行锁保证写入串行。纯查询（访客/素材/会话/事件列表、规则读取、公开阅读与媒体、分享解析、保留试算、诊断）使用 `READ ONLY` + `REPEATABLE READ` 快照事务，不取租户锁；代码层与数据库层都拒绝其中的写入。访客列表在快照上计算标签后，另开一个短写事务记录意向评估（按 as_of/人工修正水位防止旧请求覆盖新状态，并跳过快照后被删除的访客）。CRM 桥接校验在所有事务之外执行，映射写入前在锁内重新确认访客归属。跟踪最多同时占用 `TRACKING_MAX_WRITE_TX`（默认4）个写连接和 `TRACKING_MAX_READ_TX`（默认6）个读连接，超出时在进程内排队，请求取消即返回503，不占用共享连接池。查询先按权限过滤再统计；同一session重叠可见区间取并集，跨午夜按查询边界裁剪；心跳/视频事件不会制造PV。

### 原业务素材的只读版本快照

`makeTrackingLegacyResolver`读取原业务模型并复核发布/公开可见条件，覆盖AI工具详情/工作台、灵感、便民工具、公开模板和设计师主页6条路由。`legacy:<route hash>:<id>`不能跨路由借用，服务端拒绝不存在、未发布或非公开目标；模板遵守公开广场范围。只保存公开标题、类型、可用封面及尺寸；原正文只参与变更摘要，不复制正文、私人作品或资料。

原素材内容字段摘要决定source_snapshot版本；点赞、访问等互动计数不制造版本。version行不可变，legacy_catalog按owner提供只读素材卡，后台禁止用自有文章编辑器改写；来源变更后重新进入采用新版本，旧分享不再强归新内容。保留历史标题和封面快照不等于能回放历史正文，也不承诺外部原图永久可用。自有文章仍使用独立的预览、发布、撤回版本流程。

### 日汇总、保留与查询边界

当前metric_version为visible-v1。daily_summary按Asia/Shanghai自然日、owner、原visitor、session、page_view、content及version保存派生结果；跨午夜只分摊可见时长，PV记首次进入日，UV及会话须跨日去重。已接受事件和同会话匿名登录关联在同一事务重建受影响会话，乱序和重试替换投影，不累加重复计数。SQL适配器已有按session查询下推；其他适配器可回退扫描。

受审保留维护会先重建并冻结受影响会话汇总，再删除原始事实；查询使用冻结汇总替代该会话原始部分，避免重复相加。拟定raw 90天、summary及支持记录180天尚待业务/隐私批准，破坏性维护默认关闭；不存在已启用的自动清理计划。汇总含可关联visitor/session及紧凑可见区间，仍是受权限和删除约束的个人数据。汇总按完整自然日桶过期，半日截止可保守多保留不足一天。删除个人数据同时删除daily_summary/summary_state，并保留防重放墓碑。

as_of固定收件快照用于分页；原始记录过期后，早于已冻结投影的历史as_of无法重建时返回409，不伪造历史结果。CRM动态注释快照仍有限制；人工修正采用不可变历史，访客 next_cursor/first_cursor 固定覆盖 revision，旧单行或已清理的不可恢复历史对受影响账号明确返回409，详见 intent-rules.md。详见 service/docs/TRACKING_DAILY_SUMMARIES.md 和 governance-maintenance.md。

现版本尚未以百万事实压测：事务仍按租户串行，部分查询仍加载租户事实后过滤。已移除逐访客全扫描N+1并接入日汇总，但不能据此宣称10万PV/日或PRD P95目标达标；生产查询计划、锁等待、索引和维护窗口需要独立验证。

### 可解释意向证据

首版确定性规则按授权owner提供可配置窗口、会话/收藏/业务阈值及每日贡献上限；默认followup-v1为近30天。`GET/POST /intent-rules`提供只读说明和专门规则权限配置，保存创建不可变版本并事务性重算；`POST /intent-rules/recompute`支持版本绑定的幂等重算。标签返回参与指标、阈值、固定排除项、可追溯证据、原规则与人工覆盖状态。不同归属配置不一致时标签为待判断并要求选择单一归属，列表仍可使用。普通复访、点击、估价和AI关键词不会单独升级；真实收藏和独立主动咨询成功已接线，预约/报价尚无可用的真实成功源，不伪造。接口、版本回放、审计和删除语义见 [首版跟进规则合同](intent-rules.md)。

## 查询资源预算与范围下推

访客列表、素材列表和意向证据优先读取每个页面访问、每个自然日一行的 daily_summary 投影；只有投影在所查 as_of 下不能精确复现的会话（尚未投影、旧版投影、有延后事件、或投影晚于 as_of 重建）才回退读取原始事件。前提是时间窗口按上海自然日对齐，或窗口结束晚于 as_of（后台的日期范围、“全部”和意向窗口都满足）。两种归约的结果由差分测试逐字段比对一致。一次一分钟访问约消耗 2 条预算（原来约 16 条）。会话与事件下钻、访客素材卡仍读原始事件。
数据库查询必须先限定授权 owner、业务时间和必要的内容/访客/页面或会话范围。为保留窗口前PV、跨午夜和跨页面时长去重，先选候选会话，再取得其所需完整事实，不对返回数据做静默截断。

每请求累计最多20,000条读取记录及32MiB原始payload，超预算返回422，提示缩小时间、归属、素材或访客范围；成功响应绝不冒充部分统计。已过期原始明细返回410，无法从保留数据重建的历史快照返回409。预算是防止无界读的安全限制，不是百万级宽窗口报表的SLO证明。实施、索引和验证详见 service/docs/TRACKING_QUERY_BOUNDS.md。

## 小程序已发布素材入口

已有“我的”页增加“可分享素材”，复用已注册的 `pages/contentreader/contentreader`，不新增页面。

- `GET /api/v1/miniprogram/tracking/materials?limit=20&cursor=...`：必须通过现有完整签名 token 验证。服务端按当前账号、配置 tenant、`tracking_access.enabled=true AND can_manage=true`、`users.user_type=miniprogram`、账号 active 取 owner 范围。没有授权为 403，前端隐藏入口；权限/数据服务故障为错误和重试态，不能假装空数据。仅返回该范围的不可变已发布快照标题、封面、固定版本、发布状态及 reader route，不返回草稿正文/标题、owner、访问统计。默认20、最高50，受同一后端查询预算限制。
- `GET /api/v1/miniprogram/tracking/materials/:id/versions/:version`：每次点击重新验证当前权限、owner、固定发布版本和撤回墓碑，返回同一安全元数据。前端仅能导航到固定 reader；无权限、草稿、其他owner和撤回统一不可访问。此操作不生成PV或分享成功记录。
- 发布后继续编辑草稿时，入口保持该素材最近已发布的固定快照，绝不混入正在编辑的标题/封面。撤回后不再列出，旧reader也不能读取。内容开关独立于分析开关，关闭分析或拒绝告知仍可打开和原生分享。
- reader 成功加载前以及读取失败/撤回时隐藏原生分享菜单并清空旧内容；成功后分享目标仍是实际 content_id/content_version，标题/封面来自该发布版本。
- `POST /entry` 对 reader 使用真实发布素材 owner，不使用默认owner。已认证且当前具有该owner素材能力的mini账号由服务端判定 self；客户端不可提供self/preview标记。其准备分享的阅读事件不进入客户PV/UV/可见时长统计；读取权限故障时采集失败关闭，不影响公开阅读。普通接收者仍按合法告知同意后记录。
- 私有列表不落本地缓存，返回 `Cache-Control: private, no-store`。换号/登出/离开页立即清空列表，并丢弃旧请求；重复点击、旧打开请求不能把新账号导航到旧上下文。

### 授权配置前提（不自动执行）

现有小程序登录只接受 `users.user_type='miniprogram'`，管理后台账号只接受management身份。这两个账号不会根据名称、手机号或 management_access 自动映射。当前没有专门的mini素材授权UI；上线前须由有权限的数据库管理员确认实际mini用户ID、已存在的tenant和素材owner，经业务批准后显式配置 `tracking_access`。不能把管理端已授权当成小程序已授权。

下面仅是最小权限配置模板，不随迁移执行，也未在本轮对任何数据库执行。全部占位值必须由授权管理员核实，owner必须与要分享的素材一致；同一mini账号需要多个owner时逐项审批。mini能力仅供公开素材列表和打开，不允许访问管理端草稿或客户数据。

```sql
-- 先在受控环境核验账号类型和状态，以及目标素材owner；不得按昵称猜测账号。
SELECT id, user_type, status FROM users WHERE id = :verified_miniprogram_user_id;
-- 新增最小授权；若已存在记录，先人工审查其现有独立能力，勿直接覆盖。
INSERT INTO tracking_access
 (user_id, tenant_id, owner_id, can_view, can_manage, can_crm, can_delete, can_rules, enabled, updated_by, updated_at)
SELECT id, :verified_tenant_id, :verified_content_owner,
 FALSE, TRUE, FALSE, FALSE, FALSE, TRUE, :authorized_operator_id, UTC_TIMESTAMP(6)
FROM users
WHERE id = :verified_miniprogram_user_id
 AND user_type = 'miniprogram' AND COALESCE(status, 'active') = 'active';
-- 撤销此mini素材能力（仍需真实管理员授权）：
UPDATE tracking_access SET enabled=FALSE, updated_by=:authorized_operator_id, updated_at=UTC_TIMESTAMP(6)
WHERE user_id=:verified_miniprogram_user_id AND tenant_id=:verified_tenant_id AND owner_id=:verified_content_owner;
```

自动覆盖：`service/tracking/published_materials_test.go`、`service/route/share_tracking_materials_test.go`、`miniprogram/tests/tracking-materials.test.js`。数据库驱动和Node VM只验证代码合同；真实签名登录/Redis、授权配置、微信开发者工具/真机样式、真实A分享给B的全链路仍是上线验收项。
