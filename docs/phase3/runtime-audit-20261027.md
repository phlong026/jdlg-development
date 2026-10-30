# 甲第域名服务器核对与分享页接入方案

核对日期：2026年10月27日。第一轮约08:10—08:20完成公网检查；用户打开右侧宝塔后继续实读站点、启动配置和环境文件，并于08:28读取线上tracking/config。未改DNS、证书、Nginx、服务器程序或公众号配置，未发送通知。

**推荐接入地址为 `https://www.jiadilingguang.com/share/s/短码`，沿用现有官网域名、服务器和HTTPS。原生小程序卡片继续直接打开甲第原页面并携带st；两种入口使用同一套Share、内容版本及通知归属。** 地址结构为拟开发契约，当前没有可用分享页。

## 一 已验证与尚未验证的配置

| 项目 | 本次读到的事实 | 接入结论 |
|---|---|---|
| api.jiadilingguang.com | A记录162.14.210.252；HTTPS校验通过；证书2026-10-23至2027-01-21；`/admin/`为200、标题“后台管理” | 保留现有小程序API和管理后台 |
| www.jiadilingguang.com | A记录162.14.210.252；HTTPS校验通过；证书2026-10-24至2027-01-22；`/`为200、标题“甲第灵光智能体官网” | 作为本期自有域名分享入口，无需先新建域名或证书 |
| static.jiadilingguang.com | CNAME指向腾讯云COS；HTTPS校验通过，证书至2026-12-10；客户端已有该域名的公共资源地址 | 继续承接适合公开的静态资源，不承担私密正文和访问事件 |
| share.jiadilingguang.com | 本次DNS查询返回NXDOMAIN | 本期不以新子域名为前置依赖；现有证书也不能被假定覆盖它 |
| jiadilingguang.com | 本次本地和1.1.1.1递归DNS均返回NOERROR但A答案为空；www证书包含根域名不等于根域名已解析 | 分享地址明确使用www，不使用裸域名 |
| jiadilingguang.cn | 本次系统解析失败，旧部署包出现该域名 | 不以旧部署包中的地址确定现网域名 |
| API站点非API路径 | `/share/`和`/s/audit-not-a-real-share`均301至`https://api.jiadilingguang.com/api` | 当前API站点没有可用分享页；不能仅拼接路径交付 |
| 官网分享路径 | `/share/`和`/s/audit-not-a-real-share`均404 | 需新增路由和页面，官网首页已存在不代表分享页已实现 |
| 官网Nginx | 通过用户已登录宝塔实读`/www/server/panel/vhost/nginx/html_www.jiadilingguang.com.conf`；root为`/www/wwwroot/www.jiadilingguang.com`，已有extension目录include | 可新增独立分享配置，不替换官网根目录 |
| API Nginx | 实读`html_api.jiadilingguang.com.conf`；`location /api/`代理`http://localhost:8080`，`location /`返回301 `/api`；后台root实际为`/www/wwwroot/jiadilingguang` | API候选路径被重定向的原因已查明；新增www入口复用同一8080上游 |
| 官网扩展与伪静态 | extension目录仅有87B的site_total.conf，内容为网站统计access_log；rewrite/html_www.jiadilingguang.com.conf只有注释 | 本次已读文件中未见会截走/share/的业务重写，发布前仍需nginx -t/有效配置复核 |
| 运行跟踪开关 | 线上`/api/v1/miniprogram/tracking/config`返回200，enabled/content_enabled/video_enabled/intent_enabled均true，consent_version=v1 | 既有访问底座当前已启用；不代表新H5或服务号链路已实现 |
| Go项目登记 | 端口8080，执行文件`/www/wwwroot/jiadilingguang/service/bin/applinux`；执行命令`/usr/bin/python3 /www/server/jd-crm/bin/start-go.py`；项目列表状态“未启动” | 列表状态与公网API可用并存，不能据此启动第二个进程；实际PID/管理者仍需发布前确认 |
| 服务器管理入口 | SSH两把现有密钥认证失败；用户随后提供已登录的目标宝塔，主机显示162.14.210.252 | 本轮通过宝塔完成上述只读核对，原“缺管理入口”阻塞已解除 |

后续已通过宝塔终端实读：唯一 `applinux` PID为3763918、PPID为1，监听 `*:8080`，实际exe和cwd与上述登记一致，以UID 0运行。cgroup为 `/system.slice/bt.service`；这证明进程所在组，不能单凭它确定应用重启管理者，也没有点击宝塔的启动/重启按钮。

`/proc/3763918/environ`中的四项tracking开关均true，HTTP_ADDR=:8080、ENV=production、tenant=jdlg。URL Link/code开关、媒体目录、trusted proxy、公众号同步及本方案新增网页/服务号键均未设置；仅小程序AppID/Secret存在，文章同步及独立服务号凭据未见。`/proc/environ`是启动环境，结合已读 `.env`/`.env.local` 与公开config判断当前配置缺口，不将其冒充Go运行内存快照。启动脚本仍有共享环境白名单覆盖机制，部署时须复核加载来源。

实际assets目录0755、UID 0，可用空间约57.47GiB，不用于私人素材。拟定 `/www/server/jdlg-private/tracking-media` 及其父目录目前都不存在。部署时需在官网root之外建立私有目录并明确实际进程权限，不能因为公共assets存在就把受控媒体标为可复用。trusted proxy未配，启用前还需验证反代实际源地址并只信任相应本地地址。

服务号由用户确认为“华匠邦建科”。账号名称已确定，AppID、模板ID、行业场景、关联和接收条件尚未核验；名称不能代替凭据或发送能力。敏感值未输出或保存。

旧清单曾写API证书2026-10-26到期；本次TLS实读已续期至2027-01-21，旧日期不再作为现状。证书有效不证明自动续签任务正常运行。

机器可读证据见同目录《域名服务器只读核对证据_20261027.json》。记录DNS、证书和无登录HTTP结果，不含凭据、真实分享token或客户数据。

## 二 域名和路由分工

| 对外入口 | 用途 | 配置动作 |
|---|---|---|
| www域名`/`及现有官网路径 | 保留官网 | 不以新分享程序覆盖官网根目录 |
| www域名`/share/s/:code` | 客户接收页 | 新增Go分享网页路由；短码只定位服务端分享记录 |
| www域名`/share/assets/` | 分享网页CSS/JS等固定资源 | 随Go分享页一同发布，使用版本标识 |
| www域名`/api/v1/share-web/` | 网页入口解析、受控内容/媒体、事件及打开小程序 | 限定路由反向代理到已核实的甲第Go上游，保持同源，不直接开放管理接口 |
| www域名`/api/v1/wechat/official/callback` | 服务号消息、关注及适用发送回执 | 新增独立回调，不覆盖虚拟支付回调 |
| www域名`/api/v1/wechat/official/oauth/callback` | 服务号身份绑定的授权返回 | 一次性state对应原发起账号；不能让前端指定待绑定user_id |
| www根目录微信校验文件 | 实际账号域名校验 | 使用后台给出的真实文件名和内容；不覆盖现有文件与ACME路径 |
| api域名既有`/api/v1/miniprogram/*`、`/admin/` | 小程序和后台 | 保留现有入口及登录行为 |
| static域名既有资源 | 公共静态素材 | 保留；私有笔记、反馈及受控下载不直接迁到公共桶 |

实现选择：新增`service/shareweb/`，使用Go标准库`html/template`、`embed`和少量浏览器脚本；沿用现有Go服务、内容和媒体权限。无需为接收页另建独立React应用或新增服务器，也不直接搬用依赖wx API的小程序SDK。www主配置实际include为`/www/server/panel/vhost/nginx/extension/www.jiadilingguang.com/*.conf`，拟新增文件名为`phase3-share.conf`，代理目标沿用已实读的`http://localhost:8080`。

候选文件已保存为[phase3-share.conf](/Users/honor.pei/Documents/订单项目/甲地灵光二期/1024三期/需求整理/部署候选配置/phase3-share.conf)，未上传或加载。官网现有图片/JS/CSS正则会参与location匹配，因此候选前缀使用`^~`，并保留完整URI的proxy_pass；规则依据[NGINX location文档](https://nginx.org/en/docs/http/ngx_http_core_module.html#location)和[proxy_pass文档](https://nginx.org/en/docs/http/ngx_http_proxy_module.html#proxy_pass)。官网root、SSL/ACME、既有API/后台及虚拟支付回调均不修改。后续应用前还必须对目标Nginx执行配置校验并完成新Go路由验收，本轮没有将“候选文件存在”当作部署完成。

新增路径在候选中关闭继承的原始access_log，避免官网统计日志记录短码或OAuth code/state；原官网路径保持原日志。Go需提供脱敏后的请求状态、耗时、回调/投递错误与告警，错误日志和统一请求中间件也须验证不会输出完整凭证。未知/share/*返回404，不回退到管理页；回调请求方法、体积、验签和一次性state由Go校验，反向代理不代替业务授权。现有120秒读取超时不是导入或通知执行时限，外部任务仍异步处理。

## 三 统一分享契约

1. 已登录实际分享人B从内容制作或原生页发起分享，服务端验证内容、版本及权限，创建既有Share。作者A、owner和客户负责人不代替Sharer。
2. 直接以既有不可枚举share_token作为URL短码，复用其哈希索引、同一Share及有效期；无需重复短码映射表，返回可用的原生path/query、自有域名入口和能力状态。前端不传userId决定归属。
3. 原生卡片直接携带st；自有域名短码由服务器解析，验证Share仍有效、内容未撤回、版本可用。GET接收页、机器人抓封面和微信预览不产生“客户已阅读”提醒。
4. 公开图文/图片/视频可在H5阅读或播放；原生模板、作品或造价页的H5只承接入口，在小程序目标页实际阅读，不能把入口页打开算成已阅读原生方案。
5. 私人项目笔记的网页入口不暴露正文、媒体或私密标题，提示打开甲第并登录；仍由受邀账号在小程序读取、反馈和下载。分享码不授予邀请权限。
6. 网页新增浏览器采集适配器，按可见状态、用户选择和媒体绑定上报；服务端复用事件模型、去重和汇总，不新建另一套浏览账本。当前视频校验仅允许contentreader，H5必须明确加入允许的已发布内容绑定，不能简单放开任意route。
7. H5“打开小程序”时使用同一Share的目标path/query生成微信URL Link，不再调用“创建console分享”把B改成管理员。微信返回网址原样使用，不更换其hostname，也不在外层随意追加st。
8. H5到小程序的跳转增加短时有效的handoff标识；两端事实分别保留，提醒按确认规则关联去重。没有可信交接证据时，不按IP、昵称合并访客，不声称跨端身份已打通。
9. 旧卡片st、当前Share及URL Link都有有效期。自有域名可保持地址稳定，但不能暗示分享授权永久有效；过期页显示重新取得分享入口，不能静默复活已撤回或已过期的Share。

新页面与现有后台、小程序分别使用已有认证边界。公共H5不自动获得小程序登录态；服务号绑定授权只绑定发起者，不把普通访客的OAuth授权自动当作甲第实名账号。

### 新接口的最小契约

以下均为拟新增接口，当前不存在；现有管理URL Link会创建console Share，不能直接用作手机B的生成接口。每次正文、媒体或下载请求均重新校验同一Share、版本、撤回及访问授权，不按route/content_id单独放行。wx专用SDK、原生播放器观察器及小程序生命周期不能直接移入浏览器。

| 接口 | 行为 |
|---|---|
| POST 小程序`/workbench/share-entries` | 认证B；验证目标引用；创建Share及短码；返回share_id、native_path/query、web_url、expires_at和tracking_ready。现有contents/:id/share可委托这一业务逻辑 |
| GET www`/share/s/:code` | 返回通用网页外壳或可公开内容；过期/撤回/私有状态不泄露正文；不在GET中创建浏览通知 |
| POST www`/api/v1/share-web/entry` | 解析有效短码，建立有界网页访问上下文；不接受客户端伪造Sharer或recipient |
| POST www`/api/v1/share-web/events` | 浏览器事件适配现有Accept；限制体积、速率、事件类型和媒体绑定，遵守同意/关闭状态 |
| GET www`/api/v1/share-web/media/:id` | 对有效内容上下文再次校验媒体归属、撤回和范围读取；不能只凭media_id公开下载 |
| POST www`/api/v1/share-web/open-miniprogram` | 校验入口及目标后，按同一Share生成/复用微信URL Link与handoff；返回真实可用状态，不伪造普通网址作为成功 |

上述www API采用网页专用入口凭证和受控同源请求，不把匿名网页事件转成管理接口。服务端校验配置中的固定PUBLIC_SHARE_ORIGIN，不由任意Host头产生OAuth返回地址或分享链接。短码不可枚举、错误不回显内部user_id、share token或完整平台错误请求。

## 四 原生小程序卡片与黑谷的跟踪对比

结论：原生卡片可以支撑用户关心的“B分享—C打开—记录访问/停留—通知B”的业务闭环，已有大部分跟踪底座。实现介质不同，不要求先改成H5才能跟踪。黑谷客户端只能证明其记录字段和分享动作；没有证据证明它能识别所有直接转发者、获得送达回执或在客户未打开时识别收件人。

| 场景 | 当前甲第原生能力 | 三期动作或限制 |
|---|---|---|
| B分享，C实际打开 | st绑定Share、内容、路由和版本，建立访问会话 | 复用；新增前端“我的分享”和通知触发 |
| 页面可见停留 | 已有visible_segment、page_end及前后台校验 | 复用，仍是可见停留估算，不证明人一直阅读 |
| 视频播放及进度 | 自有contentreader已绑定媒体并采集进度/播放区间 | 复用；其他页面或H5播放器需逐个接入，不宣称全站视频自动覆盖 |
| C打开后在甲第内重新分享给D | 新Share保留Parent，当前身份成为新Sharer | 复用链条；C已登录并完成新分享凭证时才可向C发送其分享的提醒 |
| C在微信聊天里直接转发B的旧卡片 | 仍携带B原来的入口 | D打开可归到B入口；不能凭此确认中间转发人是C |
| C未登录 | 公开内容可使用匿名访客标识采集 | 不显示臆测的真实微信昵称/姓名，不自动建正式CRM客户 |
| 用户关闭/拒绝统计 | 现有SDK不创建统计entry或发送事件 | 保留业务可用；不承诺每次打开一定有记录和提醒 |
| 生成/预热分享入口 | 可有Share记录或share_intent | 文案是“生成入口/发起分享”，不等于微信已送达或已阅读 |
| 追踪准备失败 | 回退正常卡片分享 | 不显示“本次可追踪”；不能靠这次打开保证外推 |
| 浏览后离开小程序也收到 | 当前无完整服务号通知链路 | 新增绑定、消息中心、可靠投递及真实手机验收 |

真实身份、手机号、好友名单、未打开收件人、聊天内每个中转人不由share token自动产生。群渠道信息也不能被描述为完整群成员或每人送达明细。

## 五 三模块复用与新增统一清单

| 模块 | 直接复用 | 增量扩展/新增 |
|---|---|---|
| 内容创作 | Content/Draft/Asset、预览发布、版本撤回、受控上传、已有AI任务和公众号自身文章同步 | 手机工作台、article_v2排版、逐平台完整导入任务、项目笔记授权/反馈/导出、H5公开接收渲染 |
| 分享跟踪 | 原生SDK、st、Share.Sharer/Parent、Enter/Accept、事件去重/汇总、Go微信URL Link客户端 | 同一Share的网页短码、shareweb浏览器适配、原生目标引用、手机B创建URL Link、“我的分享”权限/分页、handoff及入口/阅读区分 |
| 服务号提醒 | users.id、微信HTTP/token基础、tracking事务及事件落库；参考既有任务重试思想 | 同事务待通知事件、服务号绑定/回调/模板、mini_notifications和delivery、持久化worker、消息中心/API/已读、失败处理和正确深链 |
| 域名服务器 | www/api域名现有DNS与TLS、现有官网/Go/MySQL、static公共资源 | 新路由反向代理、Go分享页面/脚本、配置与监控、微信账号侧域名和模板核验 |

服务号配置拟新增：`PUBLIC_SHARE_ORIGIN=https://www.jiadilingguang.com`、`SHARE_WEB_ENABLED`、`OFFICIAL_ACCOUNT_APP_ID`、`OFFICIAL_ACCOUNT_APP_SECRET`、`OFFICIAL_ACCOUNT_CALLBACK_TOKEN`、`OFFICIAL_ACCOUNT_ENCODING_AES_KEY`、`OFFICIAL_ACCOUNT_BROWSE_TEMPLATE_ID`及发送/绑定开关。敏感值只进入服务器私有配置，不写入文档或仓库。既有`TRACKING_MP_*`仅表示文章同步，不等同服务号投递已配置。

按实际启用的能力配置网页授权域名、JS接口安全域名、回调、校验文件和服务器出口IP白名单。只有使用小程序web-view时才涉及其业务域名配置；原生卡片本身不靠网页业务域名工作。服务号与小程序关联、可跳转页面及模板场景必须在目标账号后台验证，不能由DNS/TLS推导。

服务号模板是否允许本次浏览提醒、行业模板字段、当前接收条件尚未核验。官方文档本轮读取失败，微信后台页面受工具访问限制；不以第三方说明保证可发送。用户选择服务号作为外推渠道保持不变；如实际模板能力不满足，应提交明确冲突项确认，不能自行改为客服48小时消息、群发或企微。

## 六 部署前的剩余核对与验收

已完成：目标主机、www/api站点主配置/目录/include/重定向/证书引用、Go项目登记、启动脚本及基础环境文件、线上基础跟踪开关；首页显示8核、约31GB内存，根分区36.7/98.3GB占用，仅为核对时的快照。

剩余发布核对：独立应用重启管理方式、共享覆盖加载复核、私有媒体目录配置和读写验证、trusted proxy、生产URL Link微信接口实测、有效Nginx完整配置校验、持久worker并发及服务号账号模板/关联。配置值只报告非敏感项，密钥仅报告配置状态，不打印环境全文。Go管理页“未启动”不作为重启/额外启动授权或服务已停证据。

上线按：确认实际唯一Go进程及管理方式 → 备份配置和程序 → 部署关闭状态的新路由/功能 → 新增独立phase3-share.conf并执行`nginx -t` → 按发布流程切换 → 官网首页/旧API/后台/CRM/证书挑战路径回归 → 测试账号试点 → 真机验收后启用外推。回退仅移出本次新配置并恢复既有版本/开关，重新校验后按流程加载；不删除整个extension目录或覆盖site_total.conf。保留新记录，已发通知不能由数据库回退撤回。

必须通过的真实场景：

1. A制作、B分享、C打开：两种入口都对应正确版本并提醒B，后台作者及客户负责人不改变。
2. C打开后重新分享给D：有可验证新Share及Parent；直接转发旧卡片时不假报识别C。
3. H5图文/视频可读可播；原生页入口只有进入目标页后才记实际阅读。
4. 微信机器人抓封面、用户未同意统计、预览、自看、无权限和重复上报不制造客户已读提醒。
5. H5再打开小程序保留B与内容版本；关联去重有证据，不能按IP强行合并用户。
6. 私人笔记旧链接转发、撤销授权、媒体单独访问均不泄露正文或下载。
7. B退出甲第仍在服务号真实收到；中心保留同条消息，接口accepted不等于用户已读；未关注/取消关注/超时不影响内容访问。
8. HTTPS、官网首页、既有API/后台和CRM回调回归；新功能关闭时旧原生分享仍可用。

本文与《三模块现状对比与详细实现方案_最新确认版.md》及主仓库工程计划互相引用；是2026-10-27域名和分享入口的更新依据，不是服务器部署或服务号可用性验收结论。
