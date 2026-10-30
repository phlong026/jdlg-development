# 三期内容、分享与通知开发记录

本目录记录2026-10-27的服务器核对及开发进度。甲第主业务仍为既有版本，未执行生产新迁移或发送微信通知。公众号视频的独立采集环境及其仅供本机访问的 Nginx 路由已部署；www 分享网页候选配置仍未加载。

最新进度以[三模块独立能力补齐与验收](independent-capabilities-completion-20261027.md)为准。前一阶段见[采集部署与补漏](capture-and-module-audit-20261027.md)。已完成精确发布版本引用库、私人固定版本与Word导出、可信跨端交接和提醒去重；真实来源链接到服务号实发仍未完成。

## 复用与新增

| 模块 | 直接复用 | 本批新增 |
| --- | --- | --- |
| 内容创作 | 草稿、授权媒体、预览、版本发布和撤回 | 小程序内容工作台/编辑器，article_v2顺序排版，签名下载草稿媒体；后台/小程序同格式阅读 |
| 分享跟踪 | 原生SDK、st、Share.Sharer/Parent、Enter/Accept及统计 | 本人分享/访问详情的独立权限与分页，历史素材批量读取；同Share生成自有域名入口和原生目标 |
| 消息通知 | 接受访问事件的事务、users身份、已有微信HTTP客户端 | 同事务notice_event，持久收件箱/投递队列、服务号一次性身份绑定、关注/解绑、模板发送及回执、消息中心 |
| 分享网页 | 既有www域名、Go服务、公开内容版本和受控媒体 | Go内嵌接收页和浏览器适配，GET不产生阅读提醒；不另建服务或短码表 |

私人页面从公开跟踪路由清单中排除。管理素材的can_manage不代替查看访客的can_view；访问查询始终限定实际分享人，不能按作者或owner扩展到他人。

## 本地运行配置

默认新能力关闭；迁移不会自动启用功能。当前包含service/sql中的4组phase3_*_up.sql及对应down.sql（通知、服务号绑定、分享人索引、内容导入），应先在隔离环境分别验证。应用不自动迁移新表。

| 变量 | 启用条件/用途 |
| --- | --- |
| CONTENT_WORKBENCH_ENABLED=true | 手机编辑操作；还需当前小程序账号明确can_manage授权 |
| PROJECT_NOTES_ENABLED=true | 私人项目、受邀成员、固定版本及反馈确认；当前设计师仍需明确can_manage，受邀业主无需管理权限 |
| CONTENT_MEDIA_STORAGE=cos | 使用受控私有COS；复用服务器现有凭据，桶/地域默认取原配置，可显式覆盖；客户端只访问授权代理 |
| CONTENT_MEDIA_COS_PREFIX | 默认jd-content-private；与历史公共资源分离，写入前后验证匿名HEAD和GET均被拒绝 |
| TRACKING_MEDIA_DIR | 仅filesystem模式必需；官网root之外的绝对私有目录、权限0700。切换COS前须受检转存既有素材，COS模式不回退本地文件 |
| SHARE_WEB_ENABLED=true | 自有域名页面/兑换入口；PUBLIC_SHARE_ORIGIN设为已验证固定HTTPS来源 |
| PUBLIC_SHARE_ORIGIN=https://www.jiadilingguang.com | 生成分享网页及服务号授权返回，不从Host头推断 |
| TRACKING_WECHAT_URLLINK_ENABLED=true | 本小程序真实URL Link接口核验后启用，仍使用原有小程序凭据/共享token缓存 |
| NOTIFICATIONS_ENABLED=true | 收件箱schema校验通过才提供收件箱和消费worker |
| BROWSE_NOTIFICATIONS_ENABLED=true | 同事务生成浏览提醒；受上述收件箱开关约束 |
| OFFICIAL_ACCOUNT_ENABLED=true | 启用独立服务号绑定；需要服务号AppID/Secret、回调Token/AES Key和固定来源 |
| OFFICIAL_ACCOUNT_DELIVERY_ENABLED=true | 额外允许外推；需要真实模板ID和字段映射、已关注绑定、schema就绪 |
| OFFICIAL_ACCOUNT_MINIPROGRAM_VERIFIED=true | 后台确认服务号与当前小程序关联/跳转后设置；跳转固定消息中心，不接受客户端自定路径 |
| OFFICIAL_ACCOUNT_BROWSE_TEMPLATE_ID | 账号后台核验适用的浏览提醒模板，当前未确认 |
| OFFICIAL_ACCOUNT_BROWSE_FIELDS | JSON：真实模板字段名映射至title/body/time；不提供猜测模板字段默认值 |

凭据变量为 OFFICIAL_ACCOUNT_APP_ID、OFFICIAL_ACCOUNT_APP_SECRET、OFFICIAL_ACCOUNT_CALLBACK_TOKEN、OFFICIAL_ACCOUNT_ENCODING_AES_KEY。只写服务器私有配置，不进Git。账号名称已确认为“华匠邦建科”；AppID、模板及真实可发送条件尚未核验。

首次给小程序账号授予tracking_access时，分别决定can_manage/can_view及owner；不默认提升所有现有账号。沿用原管理授权方式，不把management账号凭据写进小程序。

## 接口和状态

- /api/v1/miniprogram/workbench/context、contents、media：小程序签名认证、当前管理授权；草稿媒体需签名下载为本地临时文件。
- /api/v1/miniprogram/tracking/my-shares、/:id/visits：签名认证+明确can_view，实际分享人限定，cursor/as_of/metric_version分页。
- /api/v1/miniprogram/tracking/share-entries：签名认证，兑换当前账号真实Share；无权伪造Sharer。
- /share/s/:code：code直接复用既有不可枚举share_token；只公开发布内容，原生历史页承接入口。
- /api/v1/share-web/entry、events、media/:code/:id、open-miniprogram：重新验证Share、版本、撤回、媒体归属；网页写入限定同源和请求体积。
- /api/v1/miniprogram/notifications、unread-count、read-all、/:id、/:id/read：当前收件人隔离，不支持客户端指定recipient。
- /api/v1/miniprogram/official-account/binding：GET状态、POST一次性绑定、DELETE解绑；公开回调在/api/v1/wechat/official/*。

投递accepted只表示微信接口受理；回执单独保留，不能展示为用户已读。超时或租约过期的可能已提交请求保持unknown，不盲目重发；没有绑定或模板就绪时只保留消息中心，不在之后绑定时自动补发旧提醒。新路径日志用路由模式和状态/耗时，不记录短码、OAuth code/state或签名请求头。

## 发布与回退

先核实应用实际唯一进程及可重复的启停方式，备份程序/配置/数据库，在隔离环境验证迁移，再按既有发布流程部署默认关闭的新代码。官网extension新增候选phase3-share.conf后必须nginx -t，回归官网、旧API/后台、CRM及证书挑战路径。逐项开启试点账号与开关，不因本地测试通过就开启真实服务号外发。

回退优先关闭新开关并回退应用/新增Nginx文件，保留数据库和投递证据。down.sql用于空新表或经备份后的离线结构清理，会删除消息/绑定数据；有真实投递后不能将down/up当作生产回退，否则可能丢失去重证据。actor索引回退保留正确的visitor_key回填，避免改变既有归因语义。

## 下一增量及外部验收

仍未完成：逐平台真实导入验收、旧模板/作品/造价快照引用适配、业主检索、完整活动分页、私人项目CRM关联、媒体恢复核对清理、服务号真实发送与真机多角色验收。原生已发布内容的选择保存、Word导出和可信handoff已经补齐并独立验证，不再列为未开发。

原生卡片可以跟踪B分享、C打开后的访问；C直接转发旧卡片时只能保留B原归因。C在甲第重新发起分享才有新Share/Parent，不能声称识别所有直接转发者或获得微信送达/已读回执。

核对原始配置事实见runtime-audit-20261027.md/.json。详细开发规格见spec.md；本批验证结果另见verification-20261027.md。

## 链接导入增量（2026-10-27）

新增独立 `phase3_content_import_up/down.sql` 和默认关闭的导入模块，复用草稿、私有媒体、发布后的分享/通知。已接公众号安全原排版及文内视频适配、小红书公开图文、豆一豆视频适配器及元宝视频号实验适配器。媒体可选择已实测的私有COS。完整参考代码位置、配置、任务接口、错误恢复和真实验收边界见 [导入统一方案](content-import-plan-20261027.md)。

## 内容制作与我的页面前端（2026-10-27）

内容制作、独立链接导入视图、AI文案策划、编辑/预览/阅读和“我的”已按效果图落地。AI文案通过事件通道复用现有对话及计费，只有点击发送才调用模型，采用结果可带回同一草稿。私人项目入口已接入独立笔记页面与服务端契约；手机客户管理仍待接入。分类按实际返回的媒体推导，搜索使用既有content_q，沿用游标分页和权限。

代码、验证范围与原生截图见[前端实施记录](frontend-implementation-20261027.md)。模拟器使用隔离内存数据，不能替代真实接口、平台素材、微信分享或真机验收；生产及小程序上传未因本次前端修改而发生。

私人项目的全部接口、幂等规则、媒体隔离和当前限制见[私人项目笔记](private-project-notes.md)。采集部署文件及实际服务器证据见[采集部署](capture-deployment.md)。

原生引用见[引用库](native-references.md)，固定版本导出见[Word导出](private-note-export.md)，跨端协议和隐私边界见[可信交接](share-handoff.md)。
