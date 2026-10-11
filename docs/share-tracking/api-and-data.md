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
- `GET /contents/:id/code?version=N`：仅真实微信码开关及配置齐备时调用官方接口，超时/权限失败明确报错；不是伪造二维码。未开放URL Link。
- `POST /identity-links`、`DELETE /identity-links/:visitor`：明确依据、业务ID/编号、专门权限，必须经过当前操作者可信CRM接口的目标权限验证。
- `POST /intent-overrides`：等级、原因和≤30天有效期；仅专门规则权限。标签是跟进参考，不是购买概率。
- `POST /deletions`：专门权限和原因；同步删除当前系统可控事实/映射，保留防重放墓碑；外部导出和备份治理另行核验，不虚称全副本即时清除。

## 数据字典与一致性

`tracking_access`复用users.id记录显式能力，不复制账号系统。

`tracking_records`为有类型的租户记录表。主键 `(tenant_id,kind,record_id)`；索引覆盖owner、visitor、content、share与时间，及session。kind区分content/version/media/share/share_request/entry/session/event/event_hash/sequence/view_binding/identity/crm/intent_override/business/audit/deletion/deletion_session/content_revoked/legacy_catalog/daily_summary/summary_state等。JSON字段由Go结构定义且入口严格校验，不存任意用户payload。所有时间以UTC字符串写入DATETIME并显式按UTC读回，避免旧DSN loc=Local改变含义。

事件使用INSERT不可改写。event_hash防相同ID不同内容，sequence防相同分段序号冲突。事实接受、身份变更及删除在事务内执行，租户行锁保证一致性。查询先按权限过滤再统计；同一session重叠可见区间取并集，跨午夜按查询边界裁剪；心跳/视频事件不会制造PV。

### 原业务素材的只读版本快照

`makeTrackingLegacyResolver`读取原业务模型并复核发布/公开可见条件，覆盖AI工具详情/工作台、灵感、便民工具、公开模板和设计师主页6条路由。`legacy:<route hash>:<id>`不能跨路由借用，服务端拒绝不存在、未发布或非公开目标；模板遵守公开广场范围。只保存公开标题、类型、可用封面及尺寸；原正文只参与变更摘要，不复制正文、私人作品或资料。

原素材内容字段摘要决定source_snapshot版本；点赞、访问等互动计数不制造版本。version行不可变，legacy_catalog按owner提供只读素材卡，后台禁止用自有文章编辑器改写；来源变更后重新进入采用新版本，旧分享不再强归新内容。保留历史标题和封面快照不等于能回放历史正文，也不承诺外部原图永久可用。自有文章仍使用独立的预览、发布、撤回版本流程。

### 日汇总、保留与查询边界

当前metric_version为visible-v1。daily_summary按Asia/Shanghai自然日、owner、原visitor、session、page_view、content及version保存派生结果；跨午夜只分摊可见时长，PV记首次进入日，UV及会话须跨日去重。已接受事件和同会话匿名登录关联在同一事务重建受影响会话，乱序和重试替换投影，不累加重复计数。SQL适配器已有按session查询下推；其他适配器可回退扫描。

受审保留维护会先重建并冻结受影响会话汇总，再删除原始事实；查询使用冻结汇总替代该会话原始部分，避免重复相加。拟定raw 90天、summary及支持记录180天尚待业务/隐私批准，破坏性维护默认关闭；不存在已启用的自动清理计划。汇总含可关联visitor/session及紧凑可见区间，仍是受权限和删除约束的个人数据。汇总按完整自然日桶过期，半日截止可保守多保留不足一天。删除个人数据同时删除daily_summary/summary_state，并保留防重放墓碑。

as_of固定收件快照用于分页；原始记录过期后，早于已冻结投影的历史as_of无法重建时返回409，不伪造历史结果。CRM等动态注释快照仍有限制。详见 service/docs/TRACKING_DAILY_SUMMARIES.md 和 governance-maintenance.md。

现版本尚未以百万事实压测：事务仍按租户串行，部分查询仍加载租户事实后过滤。已移除逐访客全扫描N+1并接入日汇总，但不能据此宣称10万PV/日或PRD P95目标达标；生产查询计划、锁等待、索引和维护窗口需要独立验证。

### 可解释意向证据

当前followup-v1规则固定近30天窗口。匿名或证据不足为待判断；有效登录访问默认低关注，单纯复访不会升为中等。至少两次会话访问加该窗口内的服务端真实收藏成功证据可判中等；咨询/预约/报价的高关注仅接受可信服务端成功事实，不能以已有规则分支冒充所有业务链路均已接线。支持带原因及最长30天有效期的人工修正，并分别显示原规则和人工覆盖。未提供任意规则配置后台，修改规则/窗口及更多成功源仍需单独实现和验收。

## 查询资源预算与范围下推

数据库查询必须先限定授权 owner、业务时间和必要的内容/访客/页面或会话范围。为保留窗口前PV、跨午夜和跨页面时长去重，先选候选会话，再取得其所需完整事实，不对返回数据做静默截断。

每请求累计最多20,000条读取记录及32MiB原始payload，超预算返回422，提示缩小时间、归属、素材或访客范围；成功响应绝不冒充部分统计。已过期原始明细返回410，无法从保留数据重建的历史快照返回409。预算是防止无界读的安全限制，不是百万级宽窗口报表的SLO证明。实施、索引和验证详见 service/docs/TRACKING_QUERY_BOUNDS.md。
