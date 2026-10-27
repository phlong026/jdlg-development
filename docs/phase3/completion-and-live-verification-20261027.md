# 原排版、图文、COS补齐与真实联调记录

日期：2026-10-27。仓库：`phlong026/jdlg-development`；工作树：`开发版916/.worktrees/phase3-content-sharing`；分支：`feat/phase3-content-sharing`。本次基线为 `c439d65`。

本批已补齐安全原排版保存与阅读、小红书公开图文适配、私有COS集成；真实COS和隔离MySQL核心链路通过。**真实公众号/小红书链接导入到服务号提醒的完整验收尚未完成。** 用户已明确：指定链接及测试接收账号后期补充。本次没有部署生产应用、加载Nginx配置、执行生产迁移或发送真实微信消息。

## 1. 已实现及对既有能力的影响

| 环节 | 复用 | 本次新增/修复 | 实际影响与边界 |
| --- | --- | --- | --- |
| 公众号原排版 | article_v2、草稿和Asset、发布/撤回 | Body内持久保存安全layout_html；标签/属性/CSS完整值白名单；嵌套列表、引用、表格中的媒体映射 | 保留受支持排版；不留任意外部资源和危险脚本，不承诺任意模板逐像素复制 |
| 后台、小程序及H5阅读 | 原编辑器、rich-text、原生video和网页模板 | 三端原排版展示；视频拆分时闭合并恢复祖先节点；预览保持同样语义 | 导入原排版正文只读；编辑标题、封面时原正文保持，不静默变成普通段落；复杂表格视频仍需真机比较 |
| 小红书图文 | 异步任务、HTTPFetcher、媒体校验、草稿 | 公开SSR页图文适配；完整描述、作者、有序图集；保留xsec_token和xsec_source；支持WebP | 依赖公开页实际包含详情；空壳、登录要求或缺图明确失败，不伪造成功 |
| 小红书视频/抖音 | 已核对爆款视频复刻豆一豆协议 | 图文与视频分流；只有确认视频才请求配置的解析器 | 不把图集失败转为收费重试，不复制另一产品凭据 |
| 视频号 | 已核对元宝两跳协议 | 沿用本分支实验适配器 | 仍需甲第自己的有效Cookie文件和真实样例；加密或无完整视频时阻断 |
| 媒体转存 | 现有COS SDK、服务端COS凭据、原Asset契约和权限 | 独立私有前缀、canary预检、私有ACL、匿名HEAD+GET验证、SHA256与不可变写入 | 不返回COS直链，不修改原公共资源路径或桶策略；COS模式不读取旧本地同名文件，配置失效时上传/读取均返回不可用 |
| 写入恢复 | tracking_records及既有事务 | media_pending、media_object证据；同内容重试收敛，数据库提交结果不明时复查 | 不立即删除可能已提交引用的对象；旧待完成对象的自动清理尚无调度器 |
| 图片/视频代理 | 原素材/Share/版本授权 | 单段Range有界流、32KiB缓冲、8路并发、每来源120次/分钟、30秒写入期限 | 避免一次范围播放下载整段对象；GET/HEAD使用同一授权，不因登录拥有全部素材 |
| 分享跟踪 | st、Share.Sharer/Parent、Enter/Accept、原账本 | 导入后的内容直接进入原发布及分享链 | 提醒实际分享人B；公开GET/预取不直接生成阅读通知；撤回后重新拒绝读取 |
| 消息通知 | 同事务notice_event、站内收件箱、服务号队列和绑定代码 | 与新导入内容沿同一事件链联调 | 消息中心验证通过；微信accepted仅为接口受理，不能展示为用户已读 |

主要实现文件：`service/contentimport/article.go`、`xhs_gallery.go` 负责来源解析；`service/tracking/article_layout.go`、`media_objects.go` 负责安全正文与写入恢复；`service/contentstorage/cos.go`、`stream.go` 负责私有对象；`service/route/content_media_storage.go`、`content_import_media.go` 负责授权集成。阅读及保存同步涉及 `miniprogram/miniprogram/utils/article-document.ts`、`pages/contentreader`、`pages/contentedit` 和 `background/src/pages/shareTracking`。测试与运行配置随对应模块提交。

小红书图片缓存加入CDN对象路径和非鉴权查询信息：原笔记更新图片不会继续命中旧图；临时签名变化可复用同对象。封面已是图集首图时不重复保存或展示。标题不参与不可变对象身份，避免改标题后重试冲突。

## 2. 实际联调证据

| 验证项 | 数据/环境 | 结果 | 证明范围 |
| --- | --- | --- | --- |
| 私有COS真实调用 | 甲第原服务器162.14.210.252，既有COS桶与凭据；自行生成2×3 PNG，74字节 | 通过 | 私有写入、匿名HEAD及单字节GET均拒绝、Get字节与SHA256、Range 206、覆盖冲突、自有测试对象清理 |
| MySQL真实核心链 | 本机隔离MySQL 8.4、phase3_verify，真实项目SQL迁移；合成HTML/PNG | 通过 | 任务/租约/幂等/并发领取、草稿、发布、分享人B归因、同事务事件、消息中心、已读、撤回 |
| 真实公众号样例 | 飞书研究报告中的公开文章链接 | 阻断 | 匿名读取返回微信risk_control；未导入伪正文，未操作验证码；未证明文内视频采集成功 |
| 真实小红书样例 | 当次公开feed取得的完整分享链接，含公开分享参数 | 阻断 | 复验公开页不含完整note详情，返回invalid_xhs_response；未触发付费回退 |
| 服务号真实发送 | 华匠邦建科 | 未执行 | 仅知账号名称，缺实际配置与已关注绑定的测试账号；用户确认后期补充账号 |
| 原生卡片/URL Link | 微信真机及账号平台 | 未执行 | 服务端接收st后的归因已测，不能据此称微信端传参与URL Link已验收 |

COS并非缺凭据：现有凭据已在原服务器内成功复用，没有复制到本机、Git或客户端。原公共static域名继续保留旧资源；本批测试只写随机测试对象及非敏感canary，测试结束清理自身对象。没有修改应用二进制或存储桶策略。服务器内上传的三份临时验证程序均按摘要确认并移除。

最终验证程序SHA256：

```text
888519207d3a7ad31eb64c7fdd816527097f52d9bfc92ed1f897dd787b8f8a75
```

服务器结果：

```text
=== RUN   TestLivePrivateCOSRoundTrip
LIVE_COS_OK private_acl=true anonymous_denied=true sha256_verified=true overwrite_rejected=true range_verified=true bytes=74
--- PASS: TestLivePrivateCOSRoundTrip (0.34s)
PASS
```

截图：[最终COS实测](evidence/cos-live-final-pass-20261027.png)、[临时程序清理](evidence/cos-live-cleanup-20261027.png)。COS测试不覆盖版本控制状态下两个真实并发写请求的409/412竞态，部署前仍需核验该桶的版本控制及并发条件写行为。

MySQL细节及运行门禁见[实库验证记录](mysql-flow-verification-20261027.md)。其中“转存”特指合成图片的私有文件保存；本次COS是真实云测试的另一条证据，两者没有冒充一次真实外部链接全链路。

最终实库复验通过后，已停止本次创建并带验证标签的MySQL容器 `jdlg-phase3-verify-0706b0`，保留容器与私有配置用于复查，没有操作其他项目容器。恢复该显式联调前需要先启动此测试容器。

## 3. 检查与审查

| 检查 | 当次结果 |
| --- | --- |
| Go全量测试 | CGO_ENABLED=0 go test ./...通过 |
| Go静态检查与构建 | CGO_ENABLED=0 go vet ./...及go build ./...通过；路由最终改动另有定向复验 |
| 竞态检查 | contentimport、contentstorage、tracking、shareweb四包通过 |
| 小程序 | 类型检查、tracking脚本一致性、63个路由清单一致；528项中525通过、0失败、3项按原条件跳过 |
| 后台 | npm test：756/756通过，含类型与路由检查；生产构建通过 |
| 依赖审计 | 小程序及后台npm生产依赖审计均0项漏洞；未新增依赖 |
| 凭据与差异检查 | 变更文件凭据模式扫描无命中，git diff --check通过；已审读完整实现差异及逆向补丁检查 |
| 独立代码审查 | 隐私验证、原排版拆分、不可变标题、提交不明恢复、流式期限及GET/HEAD同授权问题修复后通过 |

本机Go 1.26.2原生cgo全量测试受到既有go-m1cpu初始化崩溃影响，因此全量测试使用CGO_ENABLED=0；不能将四包竞态通过扩大为全部路由的原生cgo竞态通过。本机无govulncheck/gosec安装，未声称运行了这两种工具。WXML编译和真实设备显示仍待微信开发者工具及真机验证。

## 4. 现有配置与尚缺配置

本节为本次宝塔核对结果，部署时应重新核对运行进程实际加载的配置，不能只检查磁盘文件。

| 能力 | 已确认可复用 | 还需补齐/核验 |
| --- | --- | --- |
| 域名与应用 | www.jiadilingguang.com、自有Go服务及8080入口、既有小程序凭据 | 加载/share/、share-web API与服务号回调的候选反代；nginx -t与旧站点回归；当前尚未加载 |
| COS | 原服务器.env/.env.local存在COS_SECRET_ID/KEY，原配置桶jiadilingguangcos-1393500756、地域ap-chongqing可用，已实测 | 配置CONTENT_MEDIA_STORAGE=cos及私有前缀，核对部署加载；切换前受检迁移既有本地素材；核验版本控制/生命周期 |
| 公众号图文 | 匿名读取和安全原排版代码 | 用户后期补充有效文章基准，当前样例受风控 |
| 公众号文内视频 | 独立Capture Worker代码和协议 | 服务器未发现Chromium；需要sandbox、独立Profile、网络命名空间/出口防火墙、CDP入口、HTTPS采集地址及共享密钥；不能只填开关冒充隔离 |
| 抖音/小红书视频 | 豆一豆协议适配 | CONTENT_IMPORT_DOUYIDOU_APP_ID/APP_SECRET在甲第未配置，需本项目授权账号；不复用小程序密钥 |
| 小红书图文 | 公开页适配及受控CDN下载 | 新的可读完整分享链接；若公开SSR持续只返回壳页，则需确认具备图集能力的稳定授权数据源，不能凭空猜API |
| 视频号 | 元宝协议实验适配 | CONTENT_IMPORT_YUANBAO_ENABLED及0600的CONTENT_IMPORT_YUANBAO_COOKIE_FILE；原服务器本次未配置；有效登录态与加密样例逐项验证 |
| 服务号账号 | 名称“华匠邦建科” | OFFICIAL_ACCOUNT_APP_ID、APP_SECRET、CALLBACK_TOKEN、ENCODING_AES_KEY；核验认证、接口权限、OAuth/业务域名及小程序关联 |
| 浏览提醒模板 | 队列、字段映射验证及回执代码 | OFFICIAL_ACCOUNT_BROWSE_TEMPLATE_ID、OFFICIAL_ACCOUNT_BROWSE_FIELDS真实映射；需账号后台实际可用模板，不能猜字段 |
| 实发测试对象 | 账号绑定及关注状态代码 | 用户后期补充小程序昵称/ID；需该账号关注服务号并完成绑定；不向随机用户发送 |
| 总开关和迁移 | 默认关闭、独立up/down迁移 | CONTENT_WORKBENCH_ENABLED、CONTENT_IMPORT_ENABLED、SHARE_WEB_ENABLED、PUBLIC_SHARE_ORIGIN、NOTIFICATIONS_ENABLED、BROWSE_NOTIFICATIONS_ENABLED；部署前备份并验证目标库迁移 |
| URL Link和归因 | 既有小程序凭据、原Share/st契约 | TRACKING_WECHAT_URLLINK_ENABLED、微信账号实际调用权限/额度与真机；TRACKING_TRUSTED_PROXY_CIDRS按真实反代链设置，不信任任意X-Forwarded-For |

服务号外发还需独立开启OFFICIAL_ACCOUNT_ENABLED、OFFICIAL_ACCOUNT_DELIVERY_ENABLED，并在关联与跳转核实后设置OFFICIAL_ACCOUNT_MINIPROGRAM_VERIFIED。账号名称、模板配置、绑定完成、微信接口受理和用户实际看到消息，是不同验收步骤。

## 5. 发布、回退和后续验收顺序

1. 完成独立审核与代码合并流程，在目标环境备份应用/配置/数据库；应用本批代码时保持新功能默认关闭。本次尚未推送、创建PR、合并或部署。
2. 核验目标库迁移、授权试点账号，配置私有COS并转存需要保留的旧素材；只在隔离验证前缀复核隐私与范围流。
3. 部署受隔离的采集运行环境，配置解析供应商及元宝实验账号；按照用户后期补充的真实链接逐平台验收完整正文、图集、视频、原排版和失败状态。
4. 在自有域名验证内容版本、撤回及媒体权限；用B账号分享、C账号/匿名访客打开，核对访问记录和只提醒B。原生卡片与复制URL分别测试；直接转发旧卡片仍保留原B归因，不能识别微信所有直接转发者或送达/已读回执。
5. 核验华匠邦建科真实模板、域名、小程序关联、关注与绑定，用指定账号完成“有效浏览→站内消息→服务号受理→回执→点击消息中心”，同时检查重复浏览/失败恢复。

回退优先关闭新开关、撤回新增路由/应用代码，保留任务和消息投递证据。不使用生产down/up删除并重建消息表来恢复，否则可能丢失去重证据。媒体pending不做模糊删除；对象引用与数据库提交结果核实后再处理遗留对象。

本批没有增加依赖或另一套分享/消息数据库。主要简化是复用原Asset、Share和通知账本，图文/视频按来源能力分流，删除重复解析器初始化，保持旧plain/rich_text兼容。完整三期合同范围、2026-11-15交付日期及其他待交付模块未因本次补齐而缩减。

## 6. 本批中文提交索引

| 提交 | 意图 |
| --- | --- |
| a645800、7b9ad34 | 安全原排版持久化与分享网页展示 |
| 46febae、92ce802 | 完整描述、封面去重与公开图集导入 |
| ee65657、a57927a | 原排版再次保存及视频拆分/预览结构保护 |
| 26d624a、4890b00、516d207、faecfb9 | 私有COS基础与匿名HEAD/GET隐私门禁 |
| 20212b7 | 范围播放使用有界流 |
| 3355d3f、fcf1b8e | 远端写入恢复与标题可变语义 |
| 3a4018b | 隔离MySQL真实核心链路 |
| 0415438 | 导入、上传和授权媒体读取接入私有COS |
| b65c086 | 手机导入失败提示 |
| d72e7e9 | 原服务器真实COS验证及证据 |
| ef23a12 | COS配置失效时上传/读取失败关闭 |

每步均使用中文提交；后续小范围配置门禁修复及文档同步以本分支最新提交为准。
