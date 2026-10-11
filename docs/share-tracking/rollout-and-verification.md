# 分享访问安全启用与验证记录

## 证据版本与结论

记录时间：2026-10-11 UTC（对应Asia/Shanghai 2026-10-12）。执行者：开发分支自动化验证。分支feat/share-visit-tracking-20261012；本轮固定阶段命名 first-release-closure，父提交 811247a859c3d876431ea8dafbe896213e8ace87，对应远程父阶段9ca6335c71028f341427555c192922cd8bcd7c54。发布manifest关联本阶段本地commit与远程完整tree。

已实现并有离线自动证据，不等于17项AT全部验收通过。生产采集、迁移、清理、部署及合并均未执行。

## 本轮已完成检查

- 后台：在background执行npm test以及npm run build；519项通过，0失败，另TypeScript、路由一致性和生产构建通过。包括原469项和新增50项；本轮构建含治理界面及富文本。原始日志：/tmp/share-closure-admin.log。
- 小程序聚焦：在miniprogram执行npm run test:tracking以及npm run check:tracking；171/171通过，TS/JS源产物一致。覆盖SDK、真实页面就绪/动作回调、阅读页及视频计量单元。日志：/tmp/share-closure-mini.log。这里的“真实页面回调”指执行仓库原页面JS配合模拟wx及数据，不是真机或生产接口。
- 小程序全量：node --test tests/*.test.js，共255项，250通过、2失败、3跳过。失败仍为固定基线已有tests/customer-service-action-placement.test.js、tests/customer-service-knowledge.test.js，未修改这两项既有失败断言；新增咨询成功指令不改变其已知失败背景。全量不是全绿；日志：/tmp/share-closure-mini-all.log。
- 服务端：在service执行go test -json ./...，已完成全源码包编译及测试；578条通过、5条跳过、0失败。此计数按含Test的JSON终态记录统计，包含子测试，不是578个顶层测试函数；无测试文件的包不计作这5条跳过。日志：/tmp/share-closure-go.jsonl。
- Go跳过项：TestActualGeneratorAndPythonRenderer缺REPORT_PDF_PYTHON/REPORT_PDF_FONT；TestUserBanFlowAndListFilters缺隔离USER_BAN_TEST_DSN；TestDispatchPendingPersistsAttemptAndPreservesSettledDownstream及TestCleanupRetainsPendingAndFailedEvents缺WECOM_EVENT_INBOX_TEST_DSN；TestWecomIdentityMySQLFlow缺CRM_IDENTITY_TEST_DSN。没有把跳过写成成功或真实数据库证据。
- Chromium在当前执行环境被系统socket权限拒绝，受审重试仍失败。background/e2e/share_tracking.py已准备320/375/390/768/1024/1440宽度及交互回归，使用明确合成API fixture；未取得截图，未通过浏览器视觉验收，未将脚本存在或Node断言当作截图证据。

上述/tmp日志是本次执行环境的临时原始证据，不是部署产物或永久共享链接；交付时保留本摘要并将终态日志随最终提交关联归档。后续新增测试或修复需独立追加实际结果。

## 实现范围与可复查入口

- 全部57个旧路由逐一有策略：20个公开源页面加新增通用阅读页接入，37个源页面按账户、会话、财务、身份、私人作品等明确业务边界排除。详见miniprogram/docs/route-inventory.md及service/tracking/routes.json；公开源码策略覆盖不等于微信57条原分享入口全部真机通过。排除源仍保留原分享参数和业务逻辑。
- 原素材resolver通过既有模型核验公开发布状态，6条详情/工作台/主页路由创建真实标题封面只读卡和source_snapshot版本；正文摘要用于判变更，旧正文不存档或回放。不存在/不公开/错误路由namespace拒绝；旧版本入口不误归新版本。证据：service/tracking/legacy_test.go、service/route/share_tracking_legacy_test.go。
- 8个已审计公开源页面观察真实列表/素材点击及图片打开回调；列表曝光不制造每项素材PV，目标必须成功加载可见才独立PV。证据：miniprogram/tests/observed-content-actions.test.js。
- 日汇总已事务接入事件、身份关联、保留和删除；可在原始事件过期后保留受控历史指标，明细返回410，不能重建的历史快照返回409。证据：service/tracking/aggregates_test.go、governance_test.go及service/docs/TRACKING_DAILY_SUMMARIES.md。
- 真实收藏成功已接入原签名POST /favorites，非空持久层Add成功且模板同步无错后才异步确认；必须匹配采集token与同一已登录账号，按账号/类型/目标/UTC日封顶去重。分析失败不改变收藏成功结果；异步超时或进程中断可能少计，不承诺可靠投递。近30天至少两个会话加收藏证据可判中等。持久层、微信登录和前后端完整链路尚未真实联调。
- 正文支持plain及显式rich_text允许列表，保存前服务端清洗，前后端只对显式格式渲染富文本；媒体仍走授权资源。证据：service/tracking/richtext_test.go、background/scripts/share-tracking.test.mjs及阅读页回归。

AT对应测试及还差哪些证据见acceptance-checklist.md。内存repository、模拟HTTP、DOM测试和合成浏览器fixture均须与真实数据库/平台验收分开。

## 迁移和安全配置

先在获授权的隔离MySQL环境备份并执行service/sql/share_tracking_up.sql，验证迁移、回滚及旧业务兼容。此迁移只新增表，不授予任何用户权限，不删改旧表；不把自动创建表当授权。本轮未执行真实迁移。

由业务负责人确认组织、owner和能力映射后，再配置tracking_access。默认不授权任何人，包括平台运维。can_view、can_manage、can_crm、can_delete、can_rules独立；多归属能力交集可能有意限制操作，需以真实角色矩阵验证。

配置TRACKING_TENANT、TRACKING_DEFAULT_OWNER、TRACKING_CONSENT_VERSION、TRACKING_PRIVACY_NOTICE。采集只有TRACKING_ENABLED=true且告知配置齐全才启用。TRACKING_CONTENT_ENABLED、TRACKING_VIDEO_ENABLED、TRACKING_INTENT_ENABLED分别控制对应能力。TRACKING_MEDIA_DIR必须是受保护、可持久备份的绝对路径，不能放公开静态目录。

微信码仅在负责人核实真实小程序权限/基础库/发布路径后设置TRACKING_WECHAT_CODE_ENABLED=true，沿用已有微信App配置；TRACKING_WECHAT_ENV=release|trial|develop。程序不向聊天输出密钥；URL Link和公众号自动导入未开放。

raw 90天、summary及支持记录180天是待批准建议，DefaultRetentionPolicy的破坏性开关默认关闭。启用采集不会启用清理；先审批告知、期限、备份及删除渠道，使用PlanRetention只读试算，审核绑定具体作用域/候选记录的计划后才执行维护。维护不是已部署定时任务，也没有开放清理HTTP端点。

## 回滚与数据恢复

先关闭采集总开关，原分享/阅读核心流程继续；内容撤回属于单独操作，不能误认为删除客户记录。保留数据库表和事实用于复核，禁止直接删表作为普通回滚。恢复旧应用时不启用新端点；重新启用前重跑权限、重复/乱序、身份切换、原素材公开判定、撤回和队列重放。

个人删除同时覆盖当前系统可控原始事实、映射、日汇总和状态；保留墓碑防旧队列及恢复原始数据复活。生产前须定义备份到期、副本及外部导出的批准治理流程，恢复时先重放删除墓碑再开放流量。当前库返回completed不等于所有备份、缓存或外部副本已销毁。恢复演练未进行。

## 仍阻止宣布上线就绪的门槛

1. 真实MySQL/Redis迁移、并发事务、索引/锁等待、真实组织权限矩阵及原公开素材模型联调。
2. 匿名登录关联政策、37项采集排除范围、告知版本、raw/summary期限、删除渠道和备份治理获得业务/隐私批准。
3. 真实CRM签名桥接按当前操作者重验客户权限，映射前后审计；规则版本/窗口配置重算、收藏和主动咨询成功链逐项补验，不把高关注代码分支当成咨询/预约/报价全链路已完成。
4. 微信双端A→B→C、取消分享、原生入口、扫码、冷/热启动及朋友圈单页；已知完全同参数onShow恢复不凭空计新会话的边界须真机确认。
5. 授权媒体上传/尺寸/权限/撤回、视频倍速/全屏/锁屏、iOS/Android后台/强退/离线对账；真实阅读拒绝分析后的可用性。
6. 六宽度截图、短屏/大字号/键盘/安全区及原比例视觉验收。当前无截图，不能以构建通过替代。
7. 获授权隔离负载下10万PV/日、百万事实查询P95、积压与故障/开关回滚、恢复及维护演练。日汇总实现不等于SLO已达标。

任何未验项继续标为待验；代码提交、备份或推送不等于生产发布批准。

本阶段另通过 go test -race ./tracking ./route；日志 /tmp/share-closure-race.log。Go统计包含新增实际路由权限子测试。原CRLF源文件维持行尾，差异检查使用 git -c core.whitespace=cr-at-eol diff --check。

## 安全下推与第二轮阅读页复核

本轮包含 scoped SQL、目标身份隐私依赖下推、两个索引及可重复升级脚本。接口测试在禁止全量List的仓库中与原内存口径对照；累计读数/字节超限422回滚，不返回部分统计。20,001条无关数据不阻碍小目标CRM/删除；超大目标及全租户保留清理/诊断仍可能超预算，需另行批准的有界维护流程。参见 service/docs/TRACKING_QUERY_BOUNDS.md。

阅读页补13项真实模块集成回归；旧阶段的st复活及迟到scene覆盖已由红转绿验证，普通同页重试保留尚未消费的有效首次入口。171项跟踪测试包括原SDK、真实动作、空闲/异步、同意和阅读页入口套件。

本轮另执行 go vet ./tracking ./route，通过无诊断；日志 /tmp/share-closure-vet.log。修改后全量前端构建及小程序回归已重跑。真实浏览器、MySQL、企微、CRM及微信设备仍未接通，不声称端到端或SLO通过。
