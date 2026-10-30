# 三期内容创作 分享追踪与服务号通知 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking. 本环境可读取安装的同名无前缀技能；仅在后续实际进入实施时启用。

**Goal:** 在现有甲第内容和原生分享底座上，完成手机内容制作、完整素材导入、按实际分享人查看访问记录，以及服务号浏览提醒和消息中心。

**Architecture:** Go主服务维护小程序用户、内容、分享和通知；复用现有tracking记录/事务、Content/Draft/Asset及SDK。新增手机API、原排版文档格式、平台导入任务和Go消息/服务号模块；Java CRM继续负责正式客户，不承接小程序通知身份。有效访问事务内仅保存待通知事件，外部请求在事务外处理。

**Tech Stack:** 微信原生小程序TS/WXML/WXSS及对应JS，Go 1.25+、Gin、database/sql/MySQL、既有微信HTTP客户端，React后台。

**实施工作区：** /Users/honor.pei/Documents/订单项目/甲地灵光二期/开发版916/.worktrees/phase3-content-sharing；以下相对路径均在此工作区解析，原JDLG目录只作基线证据。

**Spec:** [三模块现状对比与详细实现方案](/Users/honor.pei/Documents/订单项目/甲地灵光二期/1024三期/需求整理/三模块现状对比与详细实现方案_最新确认版.md)。执行者须同时阅读其中的业务规则、API表、数据关系及验收矩阵。

**2026-10-27更新：** 增加自有域名接收页Task 4A及原生卡片能力边界，依据[域名服务器核对与分享页接入方案](/Users/honor.pei/Documents/订单项目/甲地灵光二期/1024三期/需求整理/域名服务器核对与分享页接入方案_20261027.md)。已通过右侧宝塔完成站点/代理/启动配置只读核对，线上基础跟踪开关已验证；候选配置已保存，未开发或加载新路由。最终进程环境及服务号后台尚需核验。

## Global Constraints

- 2026年11月15日交付全部确认范围的可验收版本；本计划仅覆盖三个关联模块，其他合同项仍共享截止日。
- 私人笔记仅受邀且登录的账号可查看和反馈，转发不增加权限。st不作为私有内容授权。
- 浏览提醒实际分享人；外部渠道为服务号，消息中心同步留存。
- 公众号正文图片、小红书图文/视频、视频号视频完整导入；公众号保留原排版。失败或漏图不能标记完整成功。
- no new dependencies without explicit request；优先使用已有golang.org/x/net/html、媒体/AI/微信HTTP能力。
- 不批量改旧分享、AI历史或客户归属；不全量补推历史通知；不将私有媒体改成永久公开URL。
- 主仓库基线main `73c36d8174fb5e446459468bb9cd692b902abd66`。嵌套CRM `master/7947152671868731553dd753215c07841dfbf184`有700项既有变化，禁止覆盖或顺手提交。
- 全部提交使用中文Lore协议。先验证后提交；不得强推共享分支。
- 下面新增函数、类型、文件及API均为拟实现契约，不是现有实现。测试代码表达最小可执行接口，完整安全/持久化断言按每任务所列场景补齐。
- 暂不固定生产浏览聚合窗口、匿名外推策略和业务项目数量关系；这些须作为配置或确认记录进入实施，不以测试值当生产默认值。
- 推荐PUBLIC_SHARE_ORIGIN为`https://www.jiadilingguang.com`，新增`/share/s/:code`及限定同源接口；不覆盖官网首页，不迁移api后台或static资源。
- 原生卡片和自有域名入口关联同一Share；直接转发旧卡片保留原入口来源，不能声称识别所有中转人、送达或未打开收件人。
- GET接收页/封面预取不生成已阅读提醒；原生目标需进入小程序实际阅读；私人笔记网页入口不暴露正文及媒体。
- 生产API代理localhost:8080已从宝塔主配置实读，不再依赖本地默认值。官网root和扩展include已核实，基础跟踪开关由线上config确认。Go项目页“未启动”不代表公网Go已停止，禁止因此启动第二进程；最终进程环境、trusted proxy、媒体目录及URL Link仍需部署前核实。

## 文件责任与边界

以下相对路径均位于主仓库`/Users/honor.pei/Documents/订单项目/甲地灵光二期/开发版916/JDLG/`。

| 位置 | 单一责任 |
|---|---|
| service/route/content_workbench.go | 小程序内容/媒体写入口、能力和scope；复用认证与封禁检查 |
| service/tracking/article_document.go | article_v2结构、清理、顺序及媒体引用校验 |
| service/contentimport/ | 平台适配、持久化任务、租约和重启恢复；不管理发布权限 |
| service/route/share_tracking_my.go | 固定当前分享人的查询API，不提供后台权限代理 |
| service/shareweb/{handler,renderer}.go、templates/、assets/ | Go嵌入网页、公开内容渲染、浏览器采集适配及真实空/错/过期状态 |
| service/tracking/share_code.go、service/route/share_web.go | 短码映射到既有Share、受控网页entry/events/media和小程序交接，不复制内容或统计库 |
| service/tracking/my_shares.go | 有界分享人查询与游标，复用已有汇总 |
| service/tracking/notice_event.go | 已验证浏览到小型notice_event的映射；无HTTP外部调用 |
| service/notification/ | inbox、delivery、任务领取、幂等和重试；不依赖CRM username |
| service/officialaccount/ | 服务号账号绑定、OAuth/回调分发及模板发送；区分文章同步配置 |
| service/route/notification.go、official_account.go | 受保护消息API与独立服务号回调注册 |
| service/model/project_note.go及独立project-note业务文件 | 项目、笔记、方案版本、反馈与授权，不复用公开发布状态代表私人分享 |
| service/sql/phase3_*_up.sql及*_down.sql | 新增表/索引与明确回退，按模块独立提交 |
| miniprogram/miniprogram/pages/contentworkbench、contentimport、contentedit、sharevisits、visitdetail | 手机业务页面，真实空/错/加载/权限状态 |
| miniprogram/miniprogram/pages/messagecenter及notificationsettings | 收件箱、未读/已读、深链及服务号绑定入口 |
| miniprogram/miniprogram/pages/contentreader和utils/tracking | 原排版增量渲染、原生分享及事件兼容 |
| background/src/pages/shareTracking/editor.tsx及api/shareTracking.ts | 新格式预览、导入/通知失败管理；保留现有统计 |

## Task 0 平台样例与能力合同

**Files:** 新增`docs/phase3/platform-capabilities.md`、`service/contentimport/testdata/`下脱敏图文/视频响应样例。

**Interfaces:** 产生逐平台输入URL类型、完整输出字段、失败状态和来源标识；供Task 3实现，不提供“只返回封面也成功”的降级契约。

- [ ] 记录公众号、视频号、小红书各一组正常、失效、超限样例；验证来源路径、正文/图集/可播媒体和保存要求。
- [ ] 在服务号实际后台核验模板、接收身份和跳转条件，记录字段契约和账号能力，不把密钥写入仓库。
- [ ] 确认角色/空间、原排版样例、分享目标、通知合并与匿名策略；项目关系由Task 8单独收口。
- [ ] 每个平台分别留下输入→输出→甲第保存/接收的证据；不通过的平台阻断该适配器交付，不静默删除合同项。
- [x] 通过用户右侧宝塔只读核对www/API主配置、root/include/rewrite、代理localhost:8080、Go项目登记与启动/环境文件；线上tracking/config确认基础四项开关为true。生成本地候选phase3-share.conf；未修改服务器或加载配置。
- [ ] 部署前核对唯一Go进程及管理者、最终继承/共享覆盖环境、trusted proxy、媒体目录及生产URL Link；已读文件缺某键不作为运行时一定缺失的证据。候选配置需目标nginx -t及既有入口回归，不能因Go项目页“未启动”额外启动进程。
- [ ] 服务号后台核验网页授权域名、JS安全域名、校验文件、出口IP白名单、与小程序关联及浏览提醒适用模板；无可用模板须登记冲突项，不自动改成企微/群发/客服48小时消息。
- [ ] 提交中文能力核验记录；原始响应中的身份和访问凭证必须移除。

## Task 1 手机内容工作台权限和基础管理

**Files:** 新增`service/route/content_workbench.go`、`content_workbench_test.go`、`content_documents.go`及对应测试；新增`service/model/content_document.go`。修改`service/route/share_tracking_materials.go`、`service/route/share_tracking.go`、`service/tracking/types.go`、`service/tracking/content.go`。新增文档附件引用及迁移；`phase3_workbench_up.sql/_down.sql`仅在新增授权字段确有必要时增加。规范资料的`service/route/building_code.go`已有文件类型处理可作为代码模式参考，不能继承其公开权限。

**Interfaces:** 消费现有User ID和Scope，提供`/workbench/context`与`/workbench/contents`；读/创建/编辑/发布权限独立，不能将can_view当can_manage。新增权限判定函数`canUseWorkbench(actorType string, active, explicitGrant bool) bool`，作为路由校验的一部分。

- [ ] 写失败测试：后台账号有管理权限不等于小程序获授权；同owner下未授权者不能写；过期revision返回冲突。

```go
func TestWorkbenchNeedsExplicitMiniGrant(t *testing.T) {
    if canUseWorkbench("management", true, true) { t.Fatal("后台账号不能冒充手机授权") }
    if canUseWorkbench("miniprogram", true, false) { t.Fatal("缺少手机授权") }
    if !canUseWorkbench("miniprogram", true, true) { t.Fatal("明确授权应可进入") }
}
```

- [ ] 在service目录执行`CGO_ENABLED=0 go test ./route -run TestWorkbench -count=1`，确认新增断言先失败。
- [ ] 接入认证和明确scope，复用SaveContent/Preview/Publish，增加分类和创建者元数据；上传复用媒体服务，列表使用游标。保存时从服务端确定actor，不接受前端角色或recipient。
- [ ] 对确认的文档格式增加独立附件引用和受控下载，保持旧图片/MP4上传白名单。存储随机文件键并校验类型、体积和归属；阅读端显式显示文档条目，不能落入现有“非图片即video”的分支。测试本人下载、越权、撤回及超限；不把文档原文件下载扩成全格式在线编辑。

```text
认证账号 → 校验账号类型/状态及工作台授权 → 检查选定owner → 校验revision → 调用既有内容服务 → 返回草稿状态
```

- [ ] 重跑工作台与既有materials/content测试；验证旧后台及旧草稿不变。中文提交意图：让获授权人员从手机管理资料而不扩大后台权限。

## Task 2 原排版文档及兼容阅读

**Files:** 新增`service/tracking/article_document.go`、`article_document_test.go`；修改`richtext.go`、`content.go`、`types.go`；修改`miniprogram/.../pages/contentreader/contentreader.ts/.wxml/.wxss`和后台editor；新增`miniprogram/tests/article-document.test.js`及后台格式兼容测试。

**Interfaces:** `normalizeBody(body, format)`扩展支持`article_v2`，Body为`{schema:1,blocks:[...]}`；块的kind支持paragraph/heading/image/video/list/quote，媒体用media_id，排版仅白名单字段。旧plain/rich_text分支保持。完整引用必须存在于获授权MediaIDs中。

- [ ] 写失败测试，确保文字/图片位置不被拆成“正文＋附件”。

```go
func TestArticleDocumentKeepsMediaOrder(t *testing.T) {
    input := `{"schema":1,"blocks":[{"kind":"paragraph","text":"第一段"},{"kind":"image","media_id":"asset-0001"},{"kind":"paragraph","text":"第二段"}]}`
    got, format, err := normalizeBody(input, "article_v2")
    if err != nil || format != "article_v2" { t.Fatalf("format=%s error=%v", format, err) }
    var doc struct { Blocks []struct { Kind string `json:"kind"` } `json:"blocks"` }
    if err := json.Unmarshal([]byte(got), &doc); err != nil { t.Fatal(err) }
    if len(doc.Blocks) != 3 || doc.Blocks[1].Kind != "image" { t.Fatal("图片位置丢失") }
}
```

- [ ] 先运行`CGO_ENABLED=0 go test ./tracking -run TestArticleDocument -count=1`，确认旧实现拒绝新格式。
- [ ] 实现解析、白名单归一、引用校验和按块渲染；非法脚本/事件/外部嵌入拒绝或清理后明确告知，不能恢复任意HTML执行。保留媒体比例和确认样例排版。
- [ ] 测试旧两种格式、重复/缺失媒体、超长文档、空内容、移动屏幕；更新接口文档和后台类型。中文提交意图：保存图文顺序以支持完整文章阅读。

## Task 2A 三类AI文案及事实约束

**Files:** 新增`service/route/content_ai_copy.go`、`service/processor/content_copy.go`及对应测试；复用`service/route/ai_intent_stream.go`和现有AI任务/供应商调用；手机内容编辑页调用结果后保存回同一草稿。

**Interfaces:** 输入文案类型design_description/project_intro/promotion、已确认项目字段、来源资料和用户补充；输出可编辑草稿、使用的事实和缺失项。新增`validateCopyFacts(facts map[string]string,text string) error`检查面积/层数/造价等确定字段的一致性，定性说明与确定事实分开；自动检查不被表述为能证明所有建筑语义正确。

- [ ] 先写面积被改写、未知造价被编造及人工修改后保存的失败测试。

```go
func TestCopyFactsRejectChangedArea(t *testing.T) {
    facts := map[string]string{"area":"480㎡"}
    if err := validateCopyFacts(facts,"本方案建筑面积999㎡"); err == nil {
        t.Fatal("模型不得改写已确认面积")
    }
}
```

- [ ] 在service目录运行新processor/route测试，确认未校验的输出先失败。
- [ ] 从当前项目版本提取事实，复用现有AI调用、限额、超时和错误处理；固定事实片段由服务端提供，缺项提示补充。结果验证不通过时不覆盖已保存正文，不自动发布或重复扣费；精简/扩写/语气调整保存回同一草稿。
- [ ] 三类文案分别用已知/缺失事实样例验收，再验证并发编辑revision冲突和失败重试。中文提交意图：让文案生成围绕已确认项目资料且可人工修正。

## Task 3 三平台导入任务和适配器

**Files:** 新增`service/contentimport/{types,service,store,worker,official_article,channels,xiaohongshu}.go`及对应测试；新增`service/route/content_import.go`；新增`service/sql/phase3_import_jobs_up.sql`及`_down.sql`；复用`tracking/mp_source.go/mp_sync.go/mp_html.go/mp_fetch.go`及受控媒体服务。接入service/main.go的生命周期，避免无停止信号的重复worker。

**Interfaces:** `SourceAdapter.Resolve(ctx, sourceURL) (SourceDocument,error)`，SourceDocument含Title、SourceID、SourceURL、按顺序的Blocks和待取得媒体。`Registry.Resolve(ctx,platform,url)`选择已核验适配器；不支持返回明确错误。任务服务Submit/Get/RunOnce负责owner、幂等、租约及持久化，不让适配器自行发布。

- [ ] 用假适配器先测试“未支持平台不会标记成功”，随后测断点恢复和重复提交。

```go
func TestRegistryDoesNotPretendToImportUnknownPlatform(t *testing.T) {
    registry := Registry{Adapters: map[string]SourceAdapter{}}
    if _, err := registry.Resolve(context.Background(), "channels", "https://example.invalid/share"); err == nil {
        t.Fatal("没有适配器不能报告完整导入")
    }
}
```

- [ ] 运行`CGO_ENABLED=0 go test ./contentimport -count=1`，确认新包契约先失败。
- [ ] 按已核验的三平台契约分别实现适配，完成任务状态、受控媒体落盘、原排版文档转换、来源/权属和重复提示。漏图、不可播、超限进入明确失败或待核对状态。

```text
Submit(授权+幂等) → queued → 租约领取 → 解析与媒体校验 → 保存完整草稿 → needs_review/ready
外部失败 → failed(可恢复原因)；租约过期 → 可重新领取；取消 → cancelled
```

- [ ] 测试跨owner查询拒绝、重复来源、部分失败、过期直链、内容超限、重启恢复；禁止在tracking事务内执行网络。三平台各有独立真机导入验收，再中文提交；接口存在或单测假数据通过不能替代上游验证。

## Task 4 原生页面引用与统一分享

**Files:** 新增`service/tracking/native_reference.go`及测试、`service/route/content_native_reference.go`；修改`share_tracking_urllink.go`、SDK `utils/tracking/index.ts/routes.ts`和对应JS；新增原生引用选择页及`miniprogram/tests/native-reference-share.test.js`。

**Interfaces:** 工作台保存`native_ref{route_key,content_id,version,title,cover}`；Share创建继续由当前认证上下文确定Sharer。对公开模板/作品/快照先接入，私人目标依Task 8授权。URL Link新增手机受保护入口，原后台入口保留。

- [ ] 先写参数保留测试，并扩展为A制作、B分享归因到B。

```js
const test = require('node:test');
const assert = require('node:assert/strict');
const { appendToken } = require('../miniprogram/utils/tracking/index.js');
test('native parameters remain intact when tracking token is replaced', () => {
  assert.equal(appendToken('/pages/templatesquaredetails/templatesquaredetails?id=123&st=old', 'new'),
    '/pages/templatesquaredetails/templatesquaredetails?id=123&st=new');
});
```

- [ ] 先跑`node --test tests/native-reference-share.test.js`验证基线行为；新增“手机B不能复用console A链接归因”的服务端测试必须先失败。
- [ ] 实现允许目标/内容/版本校验、手机分享入口及可追踪状态，保留1.8秒预热/失败回退和title/cover。外层微信网址不任意追加st，生成时使用目标query。
- [ ] 执行`npm run build:tracking && npm run check:tracking && npm run check:tracking-routes && npm run test:tracking`。中文提交意图：让已有原生内容从工作台分享并保留实际来源。

## Task 4A 自有域名接收页与同一Share的跨端入口

**Files:** 新增`service/shareweb/handler.go`、`renderer.go`、`templates/reader.html`、`assets/reader.js`、`assets/reader.css`及Go/浏览器测试；新增`service/tracking/share_code.go`及测试、`service/route/share_web.go`及路由测试。增量修改`tracking/types.go/service.go`的网页访问上下文校验、`route/index.go`路由注册、`share_tracking_urllink.go`可复用的按现有Share生成URL Link逻辑；新增`docs/phase3/share-web-deployment.md`记录已实读的Nginx增量及回退方案。不得复制现有凭据或服务器配置全文入Git。

**Interfaces:**

- 认证手机入口`POST /api/v1/miniprogram/workbench/share-entries`消费目标引用和当前账号，产生同一share_id的native_path/query、web_url、expires_at、tracking_ready。内容专用分享接口委托这一业务逻辑。
- `Service.CreateShareCode(ctx, shareID string, actor int64) (ShareCode,error)`、`ResolveShareCode(ctx, code string) (Share,error)`；新增`ShareCode{Code,ShareID string; Expires time.Time}`，保存为既有Store中的独立记录种类，随机不可枚举、过期不超过源Share；Resolve重新校验Share和内容状态。它不创建第二份Share，也不保存私密正文。
- www路由与正文/媒体/事件契约遵循域名接入文档；网页GET不写阅读或通知。浏览器上下文由后端签发并区分客户端来源，route/content/version仍严格绑定；不通过任意SDK字符串放宽播放器权限。
- 小程序交接消费同一Share，生成实际微信URL Link及短时handoff；不得再创建console Share覆盖实际分享人。可信交接只用于保留来源和去重，不凭IP合并身份。

- [ ] 先新增失败测试：创建者B以外的账号不能创建B的短码；Share撤回/过期后短码不可读；只有短码的私密笔记请求不返回正文或媒体；GET和封面抓取不能产生page_view或notice_event。`CreateShareCode/ResolveShareCode`单元测试与路由请求测试共同覆盖，不只测字符串拼接。
- [ ] 在service执行`CGO_ENABLED=0 go test ./tracking ./route ./shareweb -run 'TestShareCode|TestShareWeb|TestShareHandoff' -count=1`，确认新增行为断言先失败，再实现Store映射、Go嵌入页面和同源路由。`article_v2`复用内容模型，网页按安全结构渲染；原生业务页只显示入口，不伪造网页全文。
- [ ] 新增浏览器脚本测试`service/shareweb/assets/reader.test.js`，用node:test模拟visibilitychange、同意/拒绝、媒体进度和网络重试；拒绝/不可见不产生有效阅读时长，重试保持事件ID，封面抓取不运行采集。执行`node --test service/shareweb/assets/reader.test.js`先失败再补实现。
- [ ] 将H5 events适配到既有Accept和汇总，区分入口打开与实际阅读；H5视频严格绑定已发布内容及媒体，私有页面仍进入受邀登录的小程序。媒体读取校验入口、范围和撤回，浏览器脚本不用wx API、不开管理接口。
- [ ] 对已有Share生成URL Link，保留B与版本；缓存按share_id/环境/有效期，失败显示不可用。测试H5→小程序不额外创建管理员分享，旧卡片直接转发不会生成臆测的C；交接乱序、重复事件和未知身份不重复或错投通知。
- [ ] 固定PUBLIC_SHARE_ORIGIN、默认关闭SHARE_WEB_ENABLED，允许的同源路径由服务端控制；Host伪造、短码猜测、跨源事件、超大上报、媒体越权均有拒绝测试。日志不含完整st、OAuth code或openid。
- [ ] 使用已核对的www扩展目录新增独立phase3-share.conf，代理沿用localhost:8080；前缀使用^~避免被官网JS/CSS/图片正则截走，proxy_pass保留完整URI。保留官网、ACME和旧API。实施前复读有效配置及唯一Go进程，备份后nginx -t通过再按发布流程启用；不在Task 4A开发期间直接操作生产。候选文件见域名接入文档，回退仅移出本次新增文件，不覆盖site_total.conf或整个extension目录。
- [ ] 重跑相关测试、Go vet、脚本语法及旧tracking回归；真机验证文章/视频H5、原生跳转、短码过期及同Share收件人。中文提交意图：让客户通过甲第官网域名接收内容并保留分享来源。

## Task 5 前端我的访问记录

**Files:** 新增`service/tracking/my_shares.go`、`my_shares_test.go`、`service/route/share_tracking_my.go`及测试；增量修改`store.go/scoped_store.go/visits.go`；必要索引独立`phase3_share_actor_index_up.sql/_down.sql`；新增sharevisits/visitdetail页面和API模块，后台保留原Scope。

**Interfaces:** `Service.MyShares(ctx,scope,actor,filter)`和`MyShareVisits(ctx,scope,actor,shareID,filter)`；actor来自认证。scope允许owner与share.Sharer必须同时符合。支持cursor/as_of/metric_version，不让客户端传任意分享人。

- [ ] 先写同owner不同分享人隔离测试。新增辅助`shareVisibleToActor(scope Scope,actor int64,share Share) bool`只作为后端安全查询的一部分。

```go
func TestMySharesDoNotTreatOwnerAsSharer(t *testing.T) {
    scope := Scope{Tenant:"tenant", Owners:[]string{"company"}, View:true}
    other := Share{Owner:"company", Sharer:"u:22"}
    if shareVisibleToActor(scope, 11, other) { t.Fatal("同企业不是本人分享授权") }
    own := Share{Owner:"company", Sharer:"u:11"}
    if !shareVisibleToActor(scope, 11, own) { t.Fatal("本人授权分享应可见") }
}
```

- [ ] 先运行`CGO_ENABLED=0 go test ./tracking ./route -run 'TestMyShares|TestMyShareVisits' -count=1`。
- [ ] 在SQL层固定分享人并使用索引/分页；旧投影空值只做受限兼容和批量回填。访客详情只聚合本账号有权查看的分享，不调用全企业访客历史再前端过滤。
- [ ] 覆盖游标篡改、重叠访客、权限撤销、累计口径、查询预算及N+1；后台原查询回归。中文提交意图：让分享人从手机查看反馈且不暴露同事记录。

## Task 6 有效访问到可靠中心消息

**Files:** 新增`service/tracking/notice_event.go`及测试，增量修改`service.go/store.go/governance.go`；新增`service/notification/{types,store,worker}.go`及测试；新增`service/sql/phase3_notifications_up.sql/_down.sql`。

**Interfaces:** `NoticeRecipient(Share)(int64,error)`只接受可核验小程序收件人，非小程序console操作者须有显式映射，不按同名或owner猜测。`BuildBrowseNotice`消费服务端已验证访问/Share及可配置规则，输出小型notice_event。通知处理器消费该事件，幂等创建mini_notifications及mini_notification_deliveries。

- [ ] 先锁定最重要的收件人语义，再补事务失败、重复事件和重启测试。

```go
func TestNoticeRecipientIsActualSharer(t *testing.T) {
    recipient, err := NoticeRecipient(Share{Owner:"company", Sharer:"u:42"})
    if err != nil || recipient != 42 { t.Fatalf("recipient=%d error=%v",recipient,err) }
    if _, err := NoticeRecipient(Share{Owner:"company", Sharer:"a:anonymous"}); err == nil {
        t.Fatal("匿名分享不能猜服务号接收人")
    }
}
```

- [ ] 先运行tracking及notification新增测试，确认未实现分支失败。
- [ ] 有效访问Accept同一Repository事务内写notice_event；保持原始event不可变。消费者用唯一业务键、短事务和持久化租约创建收件箱/投递任务；网络请求不在事务内执行。

```text
Accept事务：验证事件 → Insert event → Insert notice_event → 汇总 → commit
消费者事务：锁定待处理事件 → 幂等插入inbox+delivery → 标记消费 → commit
发送器：领取delivery → 事务外请求 → 写accepted/failed/unknown等状态
```

- [ ] 测试自看/预览/无授权/重复上报不误提醒、中心记录不依赖外推成功、停止后恢复、删除/撤回取消待发送。中文提交意图：避免浏览成功后漏提醒或重复创建消息。

## Task 7 服务号绑定与实际投递

**Files:** 新增`service/officialaccount/{binding,callback,template}.go`及测试，`service/route/official_account.go`，`service/sql/phase3_official_bindings_up.sql/_down.sql`；复用`tracking/wechat_api.go`的超时/token/响应限制；注册独立回调，更新README配置说明。

**Interfaces:** `BindingIntent{state,user_id,expires_at,consumed_at}`由已登录者创建，随机一次性state保存在服务端；`consumeBindingIntent(intent,state,now)(int64,error)`只返回原发起账号。绑定凭证来自服务号已验证的OAuth/关注事件，不能来自前端任意openid。Sender.Send返回accepted/provider_msg_id或明确failed/unknown，不将消息已读作为返回值。

对外消息回调使用`https://www.jiadilingguang.com/api/v1/wechat/official/callback`，OAuth返回使用同域`/api/v1/wechat/official/oauth/callback`；PUBLIC_SHARE_ORIGIN固定配置，不由Host头决定。新增OFFICIAL_ACCOUNT_APP_ID/APP_SECRET/CALLBACK_TOKEN/ENCODING_AES_KEY/BROWSE_TEMPLATE_ID及启用开关，文章同步TRACKING_MP_*保持原用途。服务号后台可用模板和场景校验是投递验收前置，不承诺任意“浏览提醒”文案都可直接发送。

- [ ] 先写一次性state错误/过期测试，并写openid唯一绑定、回调重复、取消关注和错误收件人的路由测试。

```go
func TestBindingIntentRejectsWrongState(t *testing.T) {
    now := time.Now()
    intent := BindingIntent{State:"one-time-state",UserID:42,ExpiresAt:now.Add(time.Minute)}
    if _,err := consumeBindingIntent(intent,"other-state",now); err == nil {
        t.Fatal("不能替另一账号消费绑定意图")
    }
}
```

- [ ] 运行`CGO_ENABLED=0 go test ./officialaccount ./notification ./route -count=1`，再实现绑定、验签/解密分发、模板发送及绑定状态检查。复用已验证SDK/官方协议，不自创认证或加密协议。
- [ ] HTTP假服务模拟接口受理、明确拒绝、断连超时及回执乱序；unknown先核查，不无条件重发。迁移账号绑定时保留版本，旧delivery不会错投新绑定账号。
- [ ] 在实际服务号和至少两个收件账号完成绑定、离开小程序接收、点击到正确访问记录、未关注/取消关注场景，区分接口受理和真实手机接收。中文提交意图：让实际分享人通过服务号及时收到客户访问提醒。

## Task 8 私人项目笔记与反馈关联

**Files:** 新增`service/model/project_note.go`及项目/版本/授权/反馈迁移和测试；新增`service/route/project_note.go`、projectnotedetail等页面；新增`service/projectnote/export_docx.go`及测试/基础DOCX模板，使用已有能力或Go标准库模板填充，不擅加依赖；增量修改tracking目标校验和媒体访问，公开reader默认不变。

**Interfaces:** 项目ID、笔记ID、不可变方案版本ID、参与人授权分别稳定；新增`canReadPrivateNote(userID int64,invited,loggedIn bool) bool`只作为接口和媒体校验的一部分。读取、下载、反馈、分享创建及事件接受都校验授权；方案版本不等于Content发布版本。

- [ ] 先写未受邀账号即使持有分享入口也被拒绝的测试。

```go
func TestPrivateNoteForwardDoesNotGrantAccess(t *testing.T) {
    if canReadPrivateNote(99,false,true) { t.Fatal("转发入口不能授予项目权限") }
    if canReadPrivateNote(0,true,false) { t.Fatal("受邀身份仍需登录验证") }
    if !canReadPrivateNote(42,true,true) { t.Fatal("受邀登录账号应可读") }
}
```

- [ ] 完成项目关系及确认规则记录后，先运行新测试，随后实现私有读写/版本/反馈；授权撤销和版本更新均不可通过旧媒体URL绕过。
- [ ] 用至少两个方案版本验证意见指向、设计师回复、再分享；原图/Word对应所选版本，正式CRM customer_id回链有权限。中文提交意图：让具体项目的方案与业主意见连续保存。
- [ ] Word导出测试检查DOCX内部document.xml和媒体引用、文字可编辑、项目/版本一致；再用常用Word兼容软件实际打开检查图片完整、版式和文字编辑。未授权账号无法下载，公开资料不默认附带私密反馈；不能用PDF、Excel或整页截图替代DOCX。

## Task 9 页面、消息中心及最终集成

**Files:** 修改my入口、app.json、messagecenter.ts/.js/.wxml/.wxss；新增内容制作、导入预览、资料详情、分享设置、访问列表、通知设置页面；修改`miniprogram/scripts/build-tracking.js`的明确页面源清单以覆盖采用JS兼容TS子集的新页面，确保编译产物一致；更新后台导入/投递状态视图和使用说明。

**Interfaces:** 按Spec接口表读取，消息以当前user_id收件人分页；notification_id只定位数据，后端仍校验账号。进入详情后才将中心消息置为已读；服务号accepted不改变中心已读状态。

- [ ] 用现有node:test页面沙箱加入加载/空/错/无权限/分页/返回、跨账号缓存清理、消息已读等回归。先定义纯展示函数`messageBadge({read_at})`以锁定已读含义。

```js
const test = require('node:test');
const assert = require('node:assert/strict');
const { messageBadge } = require('../miniprogram/utils/notification-view.js');
test('provider acceptance does not mark the inbox message as read', () => {
  assert.equal(messageBadge({read_at:null,delivery_state:'accepted'}), '未读');
  assert.equal(messageBadge({read_at:'2026-10-26T10:00:00Z',delivery_state:'failed'}), '已读');
});
```

- [ ] 先运行对应node测试；实现真实API及状态，不以效果图示例数字作为接口兜底。消息中心不开放原生分享，不从通知深链放大客户权限。
- [ ] 运行SDK构建一致性、路由检查、相关node测试、后台typecheck和UI测试；再做至少3个真实项目、A作者/B分享人/C访客以及同企业D账号的端到端验证。中文提交意图：把创作 分享 访问和提醒连成可处理的手机工作流。

## 全局验证及发布回退

所有命令在注明的目录运行，不把主仓库根当Go模块根。

```bash
# service目录：基础CGO路径须单独验证；本机go-m1cpu初始化异常时纯Go用于业务基线
CGO_ENABLED=0 go test ./tracking ./route ./contentimport ./notification ./officialaccount
CGO_ENABLED=0 go test ./shareweb
CGO_ENABLED=0 go vet ./...
# miniprogram目录
npm run build:tracking
npm run check:tracking
npm run check:tracking-routes
npm run test:tracking
node --test tests/article-document.test.js tests/native-reference-share.test.js
# background目录
npm run check
npm run test:ui
npm run build
# 主仓库根：新H5脚本测试
node --test service/shareweb/assets/reader.test.js
```

- [ ] Go格式化新增/修改文件；新页面TS/JS一致。若增加了node测试文件，更新test:tracking入口或在CI中明确运行，避免“文件存在但CI不执行”。
- [ ] 对改动文件检查秘密信息、依赖变更和日志；新依赖须先有明确请求。现有支付、订单和CRM不因本计划顺带改造。
- [ ] 自审完整差异，检查同owner不同分享人的数据泄漏、查询索引/N+1、无界列表、外部超时及未知状态；不得把Go纯函数单测当作服务号规则验证。
- [ ] 新迁移先加可空/新表，元数据分批回填，必要约束后加；独立提交up/down和回退说明。正式回退保留业务数据，先关新入口及worker；已发服务号消息不可通过数据库回滚撤回。
- [ ] 分别记录本地检查、CI、目标环境部署、小程序体验版本、真实分享接收、媒体播放、服务号到达及角色验收，最终范围以三期合同和本聊天确认记录核对。
- [ ] 生产配置应用前保存原片段与程序，nginx配置校验后再切换；回归www官网首页、api旧接口/后台、CRM和证书挑战路径。分享页GET/封面预取不外推，H5进入小程序后不重复提醒；不能用curl 200替代真机阅读和服务号收件。

## 本计划的实施前确认点

需要业务或账号事实的事项已经明确列出，未用推测填充：原排版标准样例、三平台适配的实际来源/输出、服务号可用模板与绑定路径、通知合并/匿名策略、角色及项目数量关系、AI文案额度、文档格式及限额。它们不阻止本轮完成影响分析，但对应任务不得在缺少事实时擅自缩减交付。

本轮仅产出方案和基线证据；不启动上述实施任务，不把未完成的任务勾选为完成。
