# 三期内容、分享与通知首批实现

本目录记录2026-10-27的服务器核对及首批本地实现。生产仍是既有版本；没有加载Nginx候选配置、建立线上目录、执行迁移或发送微信通知。

## 复用与新增

| 模块 | 直接复用 | 本批新增 |
| --- | --- | --- |
| 内容创作 | 草稿、授权媒体、预览、版本发布和撤回 | 小程序内容工作台/编辑器，article_v2顺序排版，签名下载草稿媒体；后台/小程序同格式阅读 |
| 分享跟踪 | 原生SDK、st、Share.Sharer/Parent、Enter/Accept及统计 | 本人分享/访问详情的独立权限与分页，历史素材批量读取；同Share生成自有域名入口和原生目标 |
| 消息通知 | 接受访问事件的事务、users身份、已有微信HTTP客户端 | 同事务notice_event，持久收件箱/投递队列、服务号一次性身份绑定、关注/解绑、模板发送及回执、消息中心 |
| 分享网页 | 既有www域名、Go服务、公开内容版本和受控媒体 | Go内嵌接收页和浏览器适配，GET不产生阅读提醒；不另建服务或短码表 |

私人页面从公开跟踪路由清单中排除。管理素材的can_manage不代替查看访客的can_view；访问查询始终限定实际分享人，不能按作者或owner扩展到他人。

## 本地运行配置

默认新能力关闭；迁移不会自动启用功能。先在隔离环境应用service/sql中原有3组phase3_*_up.sql及对应down.sql，并分别验证。应用不自动迁移新表。

| 变量 | 启用条件/用途 |
| --- | --- |
| CONTENT_WORKBENCH_ENABLED=true | 手机编辑操作；还需当前小程序账号明确can_manage授权 |
| TRACKING_MEDIA_DIR | 官网root之外已有绝对私有目录，建议权限0700，进程可读写。当前生产未配；不复用公共assets/COS承载草稿 |
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

本批未完成：公众号/小红书/视频号真实链接完整导入和逐平台样例验收、AI文案工作台接入、私人项目邀请协作/导出、H5到小程序可信handoff去重、H5视频观看指标、服务号真实发送与真机多角色验收。它们仍在统一需求范围，不以复制原文链接、页面样例、provider假HTTP测试替代。

原生卡片可以跟踪B分享、C打开后的访问；C直接转发旧卡片时只能保留B原归因。C在甲第重新发起分享才有新Share/Parent，不能声称识别所有直接转发者或获得微信送达/已读回执。

核对原始配置事实见runtime-audit-20261027.md/.json。详细开发规格见spec.md；本批验证结果另见verification-20261027.md。

## 链接导入增量（2026-10-27）

新增独立 `phase3_content_import_up/down.sql` 和默认关闭的导入模块，复用草稿、私有媒体、发布后的分享/通知。已接公众号顺序正文/文内视频、豆一豆抖音/小红书视频适配器及元宝视频号实验适配器。完整参考代码位置、配置、任务接口、错误恢复和未完成的原排版/图集/COS/真实验收见 [导入统一方案](content-import-plan-20261027.md)。
