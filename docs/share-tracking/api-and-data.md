# 分享访问接口与数据合同

## 实现状态

本合同描述开发分支的代码，不表示生产功能已经启用。采集、内容、视频和意向开关默认关闭；需要明确经营范围授权，平台管理员不会自动获得客户轨迹。服务端Go标准依赖已下载并完成完整源码包编译及测试；真实MySQL/Redis、微信、CRM、媒体持久盘、设备与负载尚未联调。

## 身份和权限

管理接口前缀 `/api/v1/management/tracking`。沿用管理session并重新检查 `management_access` 启用、用户类型及企微会话；再从 `tracking_access` 读取服务器配置的tenant/owner能力。`can_view`、`can_manage`、`can_crm`、`can_delete`、`can_rules`独立。多归属能力暂采用交集，宁可拒绝不明的跨归属操作；不把某归属管理权限提升为全部范围权限。写操作要求Origin同源/既有白名单及 `X-Tracking-Request: 1`。

小程序前缀 `/api/v1/miniprogram/tracking`。登录请求沿用既有签名认证，服务器注入账号，客户端字段不能指定账号/租户/销售。匿名只使用随机客户端/匿名标识。采集凭据以 `X-Tracking-Token` 携带，服务器仅保存其SHA256；它不能访问后台查询。既有登录签名可能读取已登录时产生的认证设备值；追踪模块不新建指纹，也不把该值写入事件。

## 公开和采集接口

- `GET /config`：安全开关、告知文字/版本、采样和队列限制。不含客户数据。
- `GET /contents/:id/versions/:version`：公开已发布自有正文；不要求同意分析。版本固定，撤回拒读。正文以纯文本渲染，媒体单独授权。
- `GET /share-entry?scene=TOKEN`：校验不透明分享后返回公开目标route/content/version/token，不创建PV、不暴露收件人。
- `GET /media/:id`：管理session按素材范围，或媒体确实属于仍可访问的已发布版本。无缓存并每次复核撤回，不接受任意外部URL代理。
- `POST /entry`：`route_key,content_id?,content_version?,client_instance,anonymous_id?,share_token?,previous_entry_token?,consent_version`。返回受限entry_token、session_id、visitor_key、share_id及可访问状态。非法/过期/不匹配分享回退直接访问，内容不可访问则404。新的真实分享入口、换账号和超时产生新会话；同客户端当前匿名会话登录可审计关联。
- `POST /shares`：`route_key,content_id?,content_version?,parent_share_id?,channel,idempotency_key`。服务端确认实际分享目标，返回share_id/share_token/expiry。不推断是否发送成功。
- `POST /events/batch`：`batch_id,events`，最多50条和64KiB，逐条accepted/duplicate/rejected。未知字段/事件/版本拒绝，永久错误不会无限重试。服务端校验绑定、发生时间、分段、视频媒体归属，客户端不能发送业务成功事实。

事件类型：share_intent、page_view、page_end、visible_segment、content_click、image_open、outbound_click、scroll_depth、video_state、video_progress。`business_action_confirmed`仅允许可信服务端业务适配，不开放客户端入口。

## 管理接口

统一 `{code:0,data:...}`；错误使用实际HTTP 400/401/403/404/409/413/429/503并给安全提示。内部数据库、密钥、token和正文不进入错误回包。

- `GET /context` 返回独立能力和实际归属。
- `GET /visitors`、`/contents`、`/contents/:id/visitors`：一致的时间、归属、排序、游标、as_of、metric_version。客户筛选另有identity/intent/q；切素材清除不适用客户筛选并提示。
- `GET /visitors/:id/sessions`：可加content_id；`include_related=true`只展开访问过此素材的那些会话中的其他页面。
- `GET /page-views/:id/events`：脱敏事件分页，含补报/不完整标记。每层独立做范围检查。
- `POST /contents`、`PATCH /contents/:id`：人工纯文本草稿；expected_revision乐观锁；不能借编辑变更历史归属。
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

`tracking_records`为有类型的租户记录表。主键 `(tenant_id,kind,record_id)`；索引覆盖owner、visitor、content、share与时间，及session。kind区分content/version/media/share/share_request/entry/session/event/event_hash/sequence/view_binding/identity/crm/intent_override/business/audit/deletion/deletion_session/content_revoked等。JSON字段由Go结构定义且入口严格校验，不存任意用户payload。所有时间以UTC字符串写入DATETIME并显式按UTC读回，避免旧DSN loc=Local改变含义。

事件使用INSERT不可改写。event_hash防相同ID不同内容，sequence防相同分段序号冲突。事实接受、身份变更及删除在事务内执行，租户行锁保证一致性。查询先按权限过滤再统计；同一session重叠可见区间取并集，跨午夜按查询边界裁剪；心跳/视频事件不会制造PV。

现版本尚未以百万事件压测：事务按租户串行、查询仍加载租户事实后过滤，不宣称达到PRD P95目标。已移除逐访客全扫描N+1，但索引下推、读事务优化、增量汇总和持久快照仍需压测后决定。当前metric_version为visible-v1；as_of固定收件快照用于分页，身份/CRM动态注释的历史快照另有局限。
