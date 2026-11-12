# 公开阅读与访问跟踪整改 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 在公开内容先完整浏览的前提下，保留可验证的访问归因与账号关联，并防止私密内容泄露及错误线索统计。

**Architecture:** 以服务端内容权限为事实来源，把页面访问、账号动作、媒体凭证和可选访问分析分别校验。同步修复四层门禁及业务 handler；定向项目资源保留独立权限。先交付公开模板闭环，再扩展明确公开的其他详情，最后完成跟踪、CRM 和报表验收。

**Tech Stack:** 微信原生 TypeScript／WXML、既有确定性 JS 构建、Node 内置测试、Go／Gin／MySQL、React 管理端；不新增依赖。

**Spec:** [业务规格与决策边界](2026-11-12-public-reading-tracking-spec.md)。执行者须同时阅读，尤其 D1–D5；本文的推荐设计不代表这些决定已经获批。

## Global Constraints

- 保留 11 月 11 日设计师工作台整改版的页面、配色、交互，以及现有视频 Range、私密项目和 AI 权限修复。
- 按内容与动作划分权限，不按“正常入口／分享入口”划分登录门槛。
- 本轮是文档交付；后续代码提交、后端部署、开发版上传、体验版验证、审核和正式发布分别留证。
- 不新增依赖。不整站关闭门禁，不自动加入项目，不开放草稿、私密资料或个人生成结果。
- 新隐私行为必须检查旧客户端，不能只修改新包的默认值。
- 旧的明确拒绝优先保留，不静默升级为同意，不把 `source=default` 当作用户主动授权。

## 0. 状态、依赖与交付单位

本文所有任务均未执行。已核对源码基线 `3ee3015`／代码 `e9b2733`；不是线上版本断言。旧报告“改 4 个文件十几行、跟踪零损失”不足以作为排期或交付承诺。

实施分为三个可独立审查的交付单位：A 公开浏览与媒体（任务 1–4）；B 跟踪选择与身份（任务 5–6）；C CRM 与指标（任务 7）。任务 8 集成发布。B/C 设计可以并行，权限契约确定前不得并行随意编辑共享门禁。

D1 未决内容不开放；D2/D4 未决时不发布新的个人轨迹处理规则；D3 决定联系资料用途；D5 决定新转化指标。A 可独立开发和验收。若 D2/D4 未定稿而先行发布 A，对新开放访问范围关闭尚未定稿的新增匿名／账号画像处理，客户端不建分析会话、服务端不接收对应新增处理，分析拒绝不得转化成登录门禁；旧客户端也受服务端约束。不能只扩大 tracking 白名单就连带扩大默认采集。既有已获准范围不借此擅自更改策略。

## 1. 文件与责任映射

完整路径相对仓库根目录 `JDLG`；文中的 `utils/`、`pages/` 短路径均相对 `miniprogram/miniprogram/`，同一单元的裸 Go 文件名沿用该单元完整路径的目录。不修改旧部署回执来伪装新版本。

| 责任 | 现有文件／拟新增文件 |
| --- | --- |
| 页面门禁及响应拦截 | `miniprogram/miniprogram/utils/profile-access.ts`、`utils/profile-request-guard.ts`；同步对应 `.js` |
| HTTP 精确路径与方法放行 | `service/route/profile_access_gate.go`；对应 `profile_access_gate*_test.go` |
| tracking 页面接收与会话关联 | `service/tracking/service.go`、`profile.go`、`privacy.go`、`config.go`；`miniprogram/miniprogram/utils/tracking/{index,core,routes}.ts`；`service/tracking/routes.json` |
| 公开正文 | `service/route/{template,inspiration,miniprogram,share_tracking,project_collections}.go`；对应页面 `templatesquaredetails`、`inspirationdetail`、`designerhome`、`contentreader`、`collectionreader` |
| 图像、视频、访问票据 | `miniprogram/miniprogram/utils/protected-media.ts`；`service/route/{share_tracking,shared_video_playback,project_notes}.go`；`service/tracking/media_access.go` |
| 需保留的定向笔记 | `miniprogram/miniprogram/pages/projectnoteview/projectnoteview.ts`、`utils/phase3-api.ts`；`service/tracking/project_notes.go` |
| 隐私入口与开关 | `miniprogram/miniprogram/pages/privacy/privacy.{ts,wxml,scss}`、`pages/settings/settings.{ts,wxml}`；SDK 同意状态 |
| 分享与分析解耦 | `miniprogram/miniprogram/pages/{contentreader,collectionreader}/*.ts`、SDK 分享函数及服务端分享校验 |
| 联系依据与自动线索 | `service/route/share_tracking_lead.go`、`share_tracking_lead_auto.go`；复用既有 CRM 模型与角色权限 |
| 渠道数据质量 | `service/route/share_tracking_channels.go`、`service/model/customer_service.go`、`background/src/pages/shareTracking/channels.tsx` |
| 实施时新增验证／说明 | `miniprogram/tests/public-reading-access.test.js`、`service/route/public_reading_access_test.go`、`service/route/share_tracking_channels_test.go`、`docs/api/public-reading-access.md`、`docs/phase3/public-reading-tracking-20261112/` |

如果修改的页面不在 `build-tracking.js` 编译列表，不把普通 TypeScript 当 JS 文本复制；使用项目既有编译链并检查实际上传目录中的产物。

## Task 1: 冻结逐页、逐资源访问矩阵

**Files:** 阅读 `miniprogram/miniprogram/app.json`、四层门禁、业务 handler；新增 `docs/phase3/public-reading-tracking-20261112/access-matrix.json`、`docs/api/public-reading-access.md`。路由初始全集见文末。

**Interfaces:** 消费业务规格 D1；输出每条页面的 `route`、`read_class`、`collect`，以及 `requests[]` 中的 HTTP 方法、完整路由模式、公开投影、媒体来源、账号动作、撤回规则。`read_class` 仅用 `public`、`account`、`directed`、`private`、`infrastructure`；未知资源保守归为 `private` 并写明原因。

- [ ] 从 app.json 展开主包／分包，逐页追踪 `onLoad/onShow/onReady` 的请求、预加载和媒体；核对 72 条路由都有且只有一条矩阵记录。核对 settings、templatepreview、报告各步骤、utilitytools，不能只检查审核点名页。
- [ ] 明确允许公开的字段与发布状态；用真实公开样本和草稿／撤回／跨租户样本证明权限差异。客户端不通过任意 `from=share`、`preview=1` 声明公开。
- [ ] 将下列集合断言加入验收材料，再由权限测试消费矩阵，不用测试复述白名单实现：

```js
const registered = app.pages.concat((app.subPackages || []).flatMap(p => p.pages.map(x => p.root + '/' + x)));
assert.equal(new Set(matrix.map(x => x.route)).size, matrix.length);
assert.deepEqual([...matrix.map(x => x.route)].sort(), [...registered].sort());
```

- [ ] 公开／账号／定向／私密分类审查完成后提交“明确公开阅读与项目私密资源的访问边界”；D1 未决项目保持原权限。

## Task 2: 先打通公开模板详情的四层门禁

**Files:** 修改 `utils/profile-access.ts`、`utils/profile-request-guard.ts`、`pages/templatesquaredetails/templatesquaredetails.ts`（完整前缀见文件表）、`service/route/profile_access_gate.go`、`service/tracking/service.go`、`service/route/template.go`；扩展现有 profile/request/entry 测试及新增 public-reading 测试。

**Interfaces:** 公开正文消费 `GET /api/v1/miniprogram/templates/:id`，仅已发布且广场范围的安全投影；`GET /templates/:id/detail`、`/original-task` 等账号／权益能力不跟随放行。`st` 缺失、无效或分析服务失败不改变公开读权限。

- [ ] 先增加行为复现：匿名、资料不完整账号、过期 token 分别从列表和分享进入公开模板；正文及预览图可见，自动导航次数与资料弹窗次数均为 0。草稿和非公开模板返回受控不可访问，不返回私有字段。
- [ ] 运行新增用例确认当前失败，失败应来自详情门禁或错误选择 `/detail`，不能接受夹具缺失／脚本无法执行的失败。
- [ ] 页面和请求拦截分别添加精确公开浏览规则；模板正文优先走公开接口，账号的收藏／权益状态分开请求，不能因 `token` 存在就整页走私有接口。401 清理失效身份、取消旧账号响应；公开正文最多重试一次匿名读，禁止登录循环。428 仅影响相关账号动作，不覆盖公开正文。
- [ ] 服务端按 **GET＋单个正整数 ID** 精确放行，沿用 handler 的发布状态和范围过滤；拒绝额外子路径、写方法和越权数据。跟踪 `/entry` 仅在 D2/D4 所选规则已生效且具备对应依据时接受该公开页面的匿名／资料不完整账号；先行发布浏览修复时按阶段边界关闭未获准的新增处理，但不阻止正文。不得放宽其它私密路由。
- [ ] 回归“未登录点下载→用户主动登录→恢复当前模板；取消登录仍能看全文；收费原图不能绕过权益”。登录并不自动要求与下载无关的联系信息。
- [ ] 运行客户端 profile/public-reading 和 Go route/tracking 相关测试，审查 `.ts/.js` 一致性，提交“允许完整浏览已公开模板并保留下载权益校验”。

## Task 3: 按资源范围扩展其它公开详情

**Files:** 公开正文文件表涉及的 handler／页面；对应 public-reading、reader-entry、collection、designer scope 测试。**不把 projectnoteview 加进普通公开白名单。**

**Interfaces:** 沿用现有 `GET /tracking/contents/:id/versions/:version`、`GET /tracking/project-collections/:id/versions/:version/items/:position/content`、`GET /designers/:id/homepage`、`GET /inspirations/:id`，前缀均为 `/api/v1/miniprogram`；具体合集目录接口从 `project_collections.go` 的现有注册逐项纳入矩阵，不能用 `/tracking/*` 泛放行。

- [ ] 先写正文返回与拒绝用例：当前公开版本／合法公开历史版本、撤回、非公开条目、跨租户、非法版本或位置。设计师主页不能附带私人订单、客户、联系方式权限字段。
- [ ] 先跑出失败，再同步页面、请求、HTTP、tracking 与 handler；匿名首次加载不请求收藏、个人主页管理、私有包或后台预览数据。可选账号增强失败只降级该区块。
- [ ] 内容就绪后呈现完整正文／图集／播放器；不通过只展示封面、剩余正文要求登记来替代公开阅读。继续沿用原 UI，不顺带改版。
- [ ] 定向笔记保留有效 pns＋有效账号＋现有头像昵称完整资料门槛，直至 D3 明确调整；读取不新增成员条件。确认仍校验指定人员与成员身份，邀请／审批流程不变。增加读者能读但不能确认、撤销 pns 后不能读、其它项目附件不可读的反例。
- [ ] 对模板预览、公共工具等剩余公开入口执行任务 1 同一矩阵；无明确公开语义的页面保留保护并记录理由。造价报告／定向设计方案只按 D1 定稿范围调整。
- [ ] 回归后按内容家族拆提交，每个提交同时包含精确权限规则与正反测试，不混入后续同意策略变更。

## Task 4: 公开媒体读取与视频播放闭环

**Files:** `utils/protected-media.ts`、公开阅读页媒体映射、`service/route/{share_tracking,shared_video_playback,project_notes}.go`、`service/tracking/media_access.go`；扩展 `miniprogram/tests/protected-media.test.js`、Go `share_tracking_signed_media_test.go`、`shared_video_playback_test.go`、`tracking/media_access_test.go`。

**Interfaces:** 先识别 URL／资源访问类别，再执行相应身份校验。已有公开素材读取与引用关系能满足时直接复用；公开视频若必须签票据，在现有签发器增加显式公开资源分支，保留原账号票据分支。票据访问不依赖分析 entry 或同意状态。

- [ ] 复现匿名读取普通 HTTPS 公共图片被 `identity()` 提前拦截；补充公共、账号、定向三类素材测试。公共外域只按既有可信源规则处理，不新增任意 URL 下载代理。
- [ ] 公共 URL 不先要求 token／头像昵称；本域公共媒体必须有服务端公开引用证明。私有下载继续验签，并检查账号变化后丢弃旧响应和缓存。
- [ ] 公开视频票据绑定资源所属公开内容、版本／必要位置、媒体 ID 和有效期；每次请求验证发布／撤回与引用关系，不能只看“这个媒体曾在任意内容公开过”。签名绑定这些范围，不复用账号票据的 actor=0 绕过校验。复用现有密钥、签发和限流模式，不另造一套密码学。
- [ ] 撤回、版本移除素材、票据过期、错误内容／媒体组合均拒绝；公开票据不得访问同源私有附件。缓存策略不得绕过撤回验证；下载到本机的既有文件无法远程收回，不能承诺抹除。
- [ ] 自动化验证同一视频 `Range: bytes=0-1023` 返回 206、准确 `Content-Range` 和 1024 字节；HEAD 不发正文；越界 416；完整请求、拖动、多段请求沿现有语义。匿名与账号路径均验证，不删除原票据 actor 及定向资源权限检查。
- [ ] 真机体验版在弱网验证首帧、拖动、后台恢复和撤回重试；同时核对合法域名、HTTPS、重定向与代理 Range 转发。通过后提交“让公开媒体随公开正文读取并保留私密视频校验”。

## Task 5: 访问分析选择、告知和分享能力解耦

**Files:** SDK `index.ts/core.ts`、`pages/privacy`／`pages/settings`、两类 reader 的复制方法；`service/tracking/{config,privacy,service}.go`；配置响应与必要的同意凭证存储；扩展 consent／handoff／reader 测试和 API 文档。

**Interfaces:** 公开读权限不消费 `consent`。推荐的账号画像状态为 `unresolved|accepted|declined`；记录当前告知版本、选择来源、选择时间与服务端接收依据，不用客户端 `explicit=true` 冒充可验证的用户选择。具体适用处理、默认状态及旧版行为按 D2/D4 定稿。

- [ ] 在实现前形成当前字段、用途、业务可见人员、保存／删除周期、撤回影响和旧客户端兼容清单；只读核查生产 config 与自动线索开关，输出脱敏布尔／版本，不输出凭据。
- [ ] 写失败用例：匿名可达隐私选择；旧拒绝跨 notice/version 保持关闭；失配旧同意不能静默扩大到新用途；关闭分析仍能复制普通链接、原生分享和播放。配置超时／分析服务故障仍可看正文。
- [ ] 将匿名可达的选择入口置于现有 privacy 页面，保留 settings 的同一状态入口，不为此公开整个账号设置页；公开页提供轻量非遮挡入口。拒绝不弹重复劝导；撤回清除待发事件、定时器、临时画像关联状态。
- [ ] 根据已选策略修订默认开启和来源记录；服务端校验适用版本与用途，旧客户端不得仅凭 `consent_version` 或旧默认开启继续新增处理。不得在前端隐藏提示、后台继续采集。采用加可空字段／兼容读取的小迁移，保留原始依据；禁止批量改写旧记录为已同意。
- [ ] 去掉 `copyWebLink` 对分析开启的依赖；普通公开分享无需分析即可创建或返回合法普通链接。分享能力凭证和追踪来源凭证分别校验；分析关闭时不为“保留来源”偷偷建立画像会话。
- [ ] 采用以下完整候选告知并逐项核对实际处理：“您可直接浏览公开内容。若您选择开启访问分析，我们会记录所读公开内容、阅读时长、播放和分享行为，并在符合关联条件且您登录后关联账号。具有相应权限的内容分享方及运营人员可查看关联后的访问资料，用于内容效果分析和意向判断。您可在隐私页面关闭访问分析；关闭不影响公开浏览、播放和普通分享。联系资料的收集及销售联系用途另行说明。”该候选仅对应 D2 推荐策略；需在完整隐私说明中同步字段清单、实际人员范围、保存期限、撤回和删除流程，且经 D4 核对后才可上线，不直接当作审核完成文案。
- [ ] 通过迁移、拒绝、旧客户端和分享回归；提交同意契约、前后端实现、文案与文档同一个逻辑增量。与 11 月 10 日决定的差异另记新决策，不覆写历史记录。

## Task 6: 身份关联、归因与阅读质量

**Files:** `utils/tracking/{index,core,routes}.ts`、`service/tracking/{service,profile}.go` 及已有 login-attribution／measurement-boundaries／reader-entry 测试；管理端访客展示仅改必要口径。

**Interfaces:** 保留现有 `previous entry`、客户端校验与账号切换规则；`st` 归因、`pns` 权限和账号关联各自独立。`collect:false` 的私密结果继续不创建 tracking entry。

- [ ] 用现有 harness 复现“匿名读→主动登录→同页恢复”，核对同一合法会话关联、公开正文不因资料缺失重锁。并列测试新 st、不同 client、超时旧会话、A→B、退出和迟到响应，禁止越界合并。
- [ ] 用可控时钟断言：入口点击不增加阅读 PV；`contentReady=false`、后台、不可见、拒绝分析时不增加有效阅读／播放时长；内容就绪后只开启一次观察。正常入口和分享入口采用相同阅读条件。
- [ ] 离线匿名待发事件、拒绝后重连、登录切换队列逐项验证；保留隔离策略，不以跨账号上传换取“零丢失”。可恢复与不可恢复数量在验收记录中分开。
- [ ] 覆盖分享签名超时、朋友圈预热失败、无 st 普通链接；内容仍可访问，后台不虚构来源。只把已成功确认来源的访问纳入相应渠道。无 st 的默认归属不能伪装成某个分享者带来；管理端继续按既有 owner／角色范围查询，不因用户点过分享就开放其其它项目、其它分享方或全部账号历史。
- [ ] 核对后台匿名、账号、资料展示、意向未知、有效线索分别有清楚口径；截图和事件样本均脱敏。提交“明确匿名到账号的归因边界并校正有效阅读统计”。

## Task 7: CRM 联系依据与渠道指标修复

**Files:** `service/route/share_tracking_lead.go`、`share_tracking_lead_auto.go`、`share_tracking_channels.go`、`service/model/customer_service.go`、`background/src/pages/shareTracking/channels.tsx`；扩展 lead 测试、新增 channels 测试、管理端脚本测试。

**Interfaces:** 线索转换消费可验证账号、当前角色范围及 D3 确认的联系方式使用依据；不从头像昵称完整度推导营销授权。保留旧 `converted_leads` 数据字段的兼容性，增加质量状态或使用既有统一错误结构，不让读取错误变成真实零。

- [ ] CRM 测试先覆盖：匿名不自动转实名、拒绝对应用途不自动复用旧手机号、没有联系依据时不能自动进入可联系线索池、越权业务人员不能查看或转入别人范围。既有普通访客 DTO 不新增手机号。
- [ ] 根据 D3 在手动／自动转换共用的服务校验点执行联系依据校验；自动开关状态与 CRM bridge 互斥规则需实测，不能只保护按钮。已经有线索不能重复创建，不擅自修改旧线索归属。
- [ ] 渠道测试用 201 个重复账号、401 个不同账号以及 DB 错误：先去重、每批最多 200、批量查询合并；每渠道按唯一账号计数，不产生 N+1。数据库错误记录操作上下文但不记录手机号／token，响应表示不可用或未知，UI 可重试，不能显示 0 假装无转化。
- [ ] D5 第一阶段保留历史含义并改准确标签／说明；第二阶段“本渠道新增线索”必须使用可追溯归因事件、确定时间窗、去重、有效状态、owner 范围。历史缺失不补造；成交是另一业务事件，不能替代。
- [ ] 运行 Go route/model 和管理端相关测试、类型检查；提交联系依据与报表修复为两个独立逻辑提交，分别保留回滚能力。

## Task 8: 集成验证、部署与审核材料

**Files:** 新增 `docs/phase3/public-reading-tracking-20261112/{README.md,CHANGELOG.md,acceptance.md,receipt.json}`；部署时复用既有 ops 流程，不复制另一版本前端覆盖当前 UI。

**Interfaces:** 输出源提交、服务二进制 SHA、数据库／配置迁移状态、真实后端健康与公开／私有探测、开发版上传回执、体验版录像、后台记录与审核包版本。某阶段缺证据就标未验证，不能标完成。

- [ ] 静态／测试按下节命令执行；有新迁移则在隔离库演练升级与回滚，不使用生产真实客户资料跑写入测试。扫描变更中的凭据，依项目现有工具完成依赖检查，不为扫描安装新依赖。
- [ ] 先发布兼容旧客户端的后端与必要的小迁移，再上传对应新小程序；涉及新个人处理规则时服务器同时限制旧客户端能力。旧包可继续读取公开投影，不能扩大到未获准处理；无法兼容则阻断这部分上线；独立浏览修复必须遵守 A 阶段不扩大未获准处理的边界。
- [ ] 逐资源灰度测试公开及拒绝样本，再真机体验版验收下表；管理端如改变指标同步部署其静态资源并检查实际 hash，不只重启后端。
- [ ] 准备回滚：备份当前二进制、代理配置、前端资源与相关配置；按逻辑提交逐项 revert。保留新增可空数据字段，代码回滚不删新同意／撤回记录；旧代码会重新开启分析或复用无依据手机号时，不回滚该安全约束，停用相关处理并前滚修复。新包旧后端组合必须演练，避免详情重新白屏。已发出的微信包不能依靠服务器回滚即时收回；回滚后端时保留新包所需公开读兼容接口，暂停灰度并记录小程序回退／重新提审所需时间。
- [ ] 审核说明仅陈述实测：“公开首页、模板列表及详情可完整浏览，无自动登录或资料补全弹窗。收藏／生成等账号操作由用户主动点击触发；取消后可继续浏览。访问分析可自主选择，关闭不影响公开阅读与普通分享。”若 D2/D4 未按此实现，不使用该句。另附私密内容样例及测试账号说明其权限目的；不能保证私密场景免于审核关注。
- [ ] 用户已授权部署时按当时授权继续，否则本任务不自行发布。代码提交、后端部署、上传、设体验版、提交审核、正式发布逐项核对终态，禁止把本次计划提交当代码部署。

## 验证命令与预期

以下在实施阶段执行；本次文档任务不冒称这些测试已运行。新增测试文件先由对应任务创建。使用本机现有原生 WXML 编译器，缺失则明确记验收缺口，不把 skip 当通过。

```bash
# 工作目录 JDLG/miniprogram
npm run build:tracking
npm run check:tracking
npm run check:tracking-routes
WXML_COMPILER=/Applications/wechatwebdevtools.app/Contents/Resources/app.asar.unpacked/node_modules/wcc-exec/wcc node --test tests/public-reading-access.test.js tests/profile-access.test.js tests/profile-request-guard.test.js tests/profile-gate-integration.test.js tests/protected-media.test.js tests/share-profile-journey.test.js tests/tracking-consent.test.js tests/tracking-login-attribution.test.js tests/tracking-reader-entry.test.js tests/tracking-measurement-boundaries.test.js tests/tracking-handoff.test.js
WXML_COMPILER=/Applications/wechatwebdevtools.app/Contents/Resources/app.asar.unpacked/node_modules/wcc-exec/wcc npm run test:tracking
# 工作目录 JDLG/service
CGO_ENABLED=0 go test ./route ./tracking ./model
CGO_ENABLED=0 go vet ./route ./tracking ./model
# 管理端有变更时，工作目录 JDLG/background
npm run check
npm run test:ui
npm run build
```

预期：新增复现先失败于真实行为，再修复通过；无新增静态错误。源码与构建产物一致，私密拒绝用例全部通过。已有 TS 基线问题单列，不擅改无关页面追求全绿；变更覆盖的静态问题必须修复。

## 最终验收矩阵

每行覆盖匿名、资料不完整账号和完整账号；有效 token／过期 token 分开。除注明账号专用外，列表、分享、扫码三类入口均测。

| 场景 | 内容及动作预期 | 跟踪／业务预期 |
| --- | --- | --- |
| 公开模板／营销笔记／公开合集／设计师作品 | 全文、预览图、视频直接可读；无自动弹窗 | 同意策略内匿名／账号记录，来源可为空 |
| 进入 AI 表单／点击生成／取消登录 | 先浏览输入；生成要求账号；取消保持表单 | 私人参数、结果与历史不进入分析 |
| 复制链接／转发，分析未开启／已拒绝 | 普通分享成功，接收者可读公开正文 | 不偷偷开启分析，不保证每次都有 st |
| 撤回、过期票据、私有素材 ID、跨租户 ID | 不泄露正文／媒体；清晰错误与合法重试 | 不把失败当有效阅读；不可借分析凭证越权 |
| 定向 pns 笔记／私密资料包／确认操作 | 原权限有效；合法读者不自动入项目／取得确认权 | 保留角色与项目范围，不因公开阅读重分类 |
| 匿名读后登录／30 分钟外登录／新 st／换设备 | 正文稳定，账号增强按新身份加载 | 只合并可证实的会话，不跨账号或跨设备追认 |
| 弱网、离线、重连、后台、切换账号 | 可控错误／重试，不死循环登录、不串媒体缓存 | 不多算时长、不越界发旧队列、不伪造来源 |
| 真机视频首帧／拖动／撤回 | 206／HEAD／416 与票据权限正确 | 分析关闭仍播放，观看统计符合可见性 |
| 隐私入口／拒绝／撤回／版本迁移 | 匿名可达，公开功能不受阻 | 明确拒绝不恢复，个人处理依据可审计 |
| 渠道 >200 账号／旧线索／DB 失败 | 报表正确或明确未知，能重试 | 不用历史线索当新增转化／成交，不显示假零 |

## 路由初始全集（72 条）

此清单只证明盘点范围，不代表已批准匿名开放。任务 1 要为每条补齐精确接口、媒体、动作与分类；新增路由必须同步矩阵。

```text
pages/index/index
pages/my/my
pages/aitools/aitools
pages/aitooldetail/aitooldetail
pages/login/login
pages/template/template
pages/distribution/distribution
pages/report/landing
pages/report/questionnaire
pages/report/preview
pages/report/delivery
pages/generatedetails/generatedetails
pages/Invitefriends/Invitefriends
pages/chat/chat
pages/designerhome/designerhome
pages/myInformationmodification/myInformationmodification
pages/templatesquaredetails/templatesquaredetails
pages/myplan/myplan
pages/myfavorites/myfavorites
pages/ordermanagement/ordermanagement
pages/wallet/wallet
pages/ruralvilladesign/ruralvilladesign
pages/Parentchildcreativity/Parentchildcreativity
pages/topupcenter/topupcenter
pages/allrounddesign/allrounddesign
pages/release/release
pages/release/designerworkpublish
pages/buildingreplacement/buildingreplacement
pages/checkin/checkin
pages/Identityauthen/Identityauthen
pages/generatehistory/generatehistory
pages/lingshidetails/lingshidetails
pages/orderdetail/orderdetail
pages/messagecenter/messagecenter
pages/adminlogin/adminlogin
pages/adminhome/adminhome
pages/withdrawcenter/withdrawcenter
pages/contentreader/contentreader
pages/contentworkbench/contentworkbench
pages/contentedit/contentedit
pages/contentwriting/contentwriting
pages/sharevisits/sharevisits
pages/visitdetail/visitdetail
pages/projectnotes/projectnotes
pages/projectnoteview/projectnoteview
pages/projectcollections/projectcollections
pages/collectionreader/collectionreader
pages/projectpackage/projectpackage
pages/projectpackageview/projectpackageview
pages/projectinvite/projectinvite
pages/nativereferences/nativereferences
pages/aigenerate/aigenerate
pages/customerservice/customerservice
pages/aivideo/aivideo
pages/aitoolworkbench/aitoolworkbench
pages/settings/settings
pages/newminnantool/newminnantool
pages/specadvisor/specadvisor
pages/aicost/aicost
pages/real/real
pages/aicostresult/aicostresult
pages/inspirationdetail/inspirationdetail
pages/privacy/privacy
pages/aicostshare/aicostshare
pages/passwordrecovery/passwordrecovery
pages/phonebind/phonebind
pages/templatepreview/templatepreview
pages/buildingcodedocs/buildingcodedocs
pages/voicecall/voicecall
pages/utilitytools/utilitytools
pages/utilitytooldetail/utilitytooldetail
pages/generateprogress/generateprogress
```
