# 本轮详细修改清单

原三个功能提交覆盖53个既有文件的代码/测试/接口说明整改，另补本目录交付文档。其后追加视觉收敛、租户锁修复及素材访客窗口修复；追加修复提交为7a8479c（5文件）、086535f（13文件）、c6561eb（3文件）；存在重叠，最终唯一文件数以整体diff为准，不把各提交文件数相加当作唯一总数。原生及发布状态见[VERIFICATION.md](VERIFICATION.md)。

## 内容创作

媒体优先、缩略图单组操作、折叠选填字段、独立保存及严格修订发布。

- [miniprogram/miniprogram/pages/contentedit/contentedit.js](../../../miniprogram/miniprogram/pages/contentedit/contentedit.js)
- [miniprogram/miniprogram/pages/contentedit/contentedit.ts](../../../miniprogram/miniprogram/pages/contentedit/contentedit.ts)
- [miniprogram/miniprogram/pages/contentedit/contentedit.wxml](../../../miniprogram/miniprogram/pages/contentedit/contentedit.wxml)
- [miniprogram/miniprogram/pages/contentedit/contentedit.wxss](../../../miniprogram/miniprogram/pages/contentedit/contentedit.wxss)
- [miniprogram/miniprogram/pages/contentworkbench/contentworkbench.js](../../../miniprogram/miniprogram/pages/contentworkbench/contentworkbench.js)
- [miniprogram/miniprogram/pages/contentworkbench/contentworkbench.ts](../../../miniprogram/miniprogram/pages/contentworkbench/contentworkbench.ts)
- [miniprogram/miniprogram/pages/contentworkbench/contentworkbench.wxml](../../../miniprogram/miniprogram/pages/contentworkbench/contentworkbench.wxml)
- [miniprogram/miniprogram/pages/contentworkbench/contentworkbench.wxss](../../../miniprogram/miniprogram/pages/contentworkbench/contentworkbench.wxss)
- [miniprogram/miniprogram/pages/contentwriting/contentwriting.js](../../../miniprogram/miniprogram/pages/contentwriting/contentwriting.js)
- [miniprogram/miniprogram/pages/contentwriting/contentwriting.json](../../../miniprogram/miniprogram/pages/contentwriting/contentwriting.json)
- [miniprogram/miniprogram/pages/contentwriting/contentwriting.ts](../../../miniprogram/miniprogram/pages/contentwriting/contentwriting.ts)
- [miniprogram/miniprogram/pages/contentwriting/contentwriting.wxml](../../../miniprogram/miniprogram/pages/contentwriting/contentwriting.wxml)

## 项目笔记

事后客户关联、保存基线、精确项目快照、发送与恢复检查点。

- [docs/phase3/private-project-notes.md](../../../docs/phase3/private-project-notes.md)
- [miniprogram/miniprogram/pages/projectnotes/projectnotes-core.js](../../../miniprogram/miniprogram/pages/projectnotes/projectnotes-core.js)
- [miniprogram/miniprogram/pages/projectnotes/projectnotes.wxml](../../../miniprogram/miniprogram/pages/projectnotes/projectnotes.wxml)
- [miniprogram/miniprogram/pages/projectnotes/projectnotes.wxss](../../../miniprogram/miniprogram/pages/projectnotes/projectnotes.wxss)
- [service/route/project_notes.go](../../../service/route/project_notes.go)
- [service/tracking/project_notes.go](../../../service/tracking/project_notes.go)

## 客户与阅读

双入口、受素材约束访客、受控管理预览及全局统计隔离。

- [miniprogram/miniprogram/pages/contentreader/contentreader.js](../../../miniprogram/miniprogram/pages/contentreader/contentreader.js)
- [miniprogram/miniprogram/pages/contentreader/contentreader.ts](../../../miniprogram/miniprogram/pages/contentreader/contentreader.ts)
- [miniprogram/miniprogram/pages/contentreader/contentreader.wxml](../../../miniprogram/miniprogram/pages/contentreader/contentreader.wxml)
- [miniprogram/miniprogram/pages/contentreader/contentreader.wxss](../../../miniprogram/miniprogram/pages/contentreader/contentreader.wxss)
- [miniprogram/miniprogram/pages/sharevisits/sharevisits.js](../../../miniprogram/miniprogram/pages/sharevisits/sharevisits.js)
- [miniprogram/miniprogram/pages/sharevisits/sharevisits.ts](../../../miniprogram/miniprogram/pages/sharevisits/sharevisits.ts)
- [miniprogram/miniprogram/pages/sharevisits/sharevisits.wxml](../../../miniprogram/miniprogram/pages/sharevisits/sharevisits.wxml)
- [miniprogram/miniprogram/pages/sharevisits/sharevisits.wxss](../../../miniprogram/miniprogram/pages/sharevisits/sharevisits.wxss)
- [miniprogram/miniprogram/pages/visitdetail/visitdetail.js](../../../miniprogram/miniprogram/pages/visitdetail/visitdetail.js)
- [miniprogram/miniprogram/pages/visitdetail/visitdetail.ts](../../../miniprogram/miniprogram/pages/visitdetail/visitdetail.ts)
- [miniprogram/miniprogram/pages/visitdetail/visitdetail.wxml](../../../miniprogram/miniprogram/pages/visitdetail/visitdetail.wxml)
- [miniprogram/miniprogram/pages/visitdetail/visitdetail.wxss](../../../miniprogram/miniprogram/pages/visitdetail/visitdetail.wxss)
- [miniprogram/miniprogram/utils/tracking/index.js](../../../miniprogram/miniprogram/utils/tracking/index.js)
- [miniprogram/miniprogram/utils/tracking/index.ts](../../../miniprogram/miniprogram/utils/tracking/index.ts)

## 其余后端及回归文件

包括聚合前素材过滤、合集归组、完整范围游标签名、父窗口重签、兼容空Route旧签名、真实profile/tracking生命周期、异步setData和错误恢复的行为验证。

- [miniprogram/tests/content-presentation.test.js](../../../miniprogram/tests/content-presentation.test.js)
- [miniprogram/tests/content-reader-refresh.test.js](../../../miniprogram/tests/content-reader-refresh.test.js)
- [miniprogram/tests/customer-visit-feed.test.js](../../../miniprogram/tests/customer-visit-feed.test.js)
- [miniprogram/tests/customer-workflow-rectification.test.js](../../../miniprogram/tests/customer-workflow-rectification.test.js)
- [miniprogram/tests/phase3-content-editor-ui.test.js](../../../miniprogram/tests/phase3-content-editor-ui.test.js)
- [miniprogram/tests/phase3-content-studio.test.js](../../../miniprogram/tests/phase3-content-studio.test.js)
- [miniprogram/tests/phase3-workbench.test.js](../../../miniprogram/tests/phase3-workbench.test.js)
- [miniprogram/tests/phase3-wxml-compile.test.js](../../../miniprogram/tests/phase3-wxml-compile.test.js)
- [miniprogram/tests/private-project-notes.test.js](../../../miniprogram/tests/private-project-notes.test.js)
- [miniprogram/tests/tracking-materials.test.js](../../../miniprogram/tests/tracking-materials.test.js)
- [miniprogram/tests/tracking-reader-entry.test.js](../../../miniprogram/tests/tracking-reader-entry.test.js)
- [service/route/content_workbench.go](../../../service/route/content_workbench.go)
- [service/route/content_workbench_test.go](../../../service/route/content_workbench_test.go)
- [service/route/project_notes_test.go](../../../service/route/project_notes_test.go)
- [service/route/share_tracking.go](../../../service/route/share_tracking.go)
- [service/tracking/content.go](../../../service/tracking/content.go)
- [service/tracking/my_visitors.go](../../../service/tracking/my_visitors.go)
- [service/tracking/my_visitors_test.go](../../../service/tracking/my_visitors_test.go)
- [service/tracking/project_notes_workflow_test.go](../../../service/tracking/project_notes_workflow_test.go)
- [service/tracking/query.go](../../../service/tracking/query.go)
- [service/tracking/types.go](../../../service/tracking/types.go)

## 关键实现部位

- contentedit.persistDraft/publish：保持新输入未保存，内部save→preview_token→publish；过期校验重建，结果不明复用幂等检查点。
- projectnotes.persistNote/prepareProjectNoteSend/versionMatchesDraft：核对账号、项目、笔记revision、草稿实例、完整表单和项目快照；冻结不明先读回，凭证缺失明确恢复。
- myActorShares/MyVisitors/MyVisitorEntries：本人权限与素材筛选在聚合/分页之前，规则意向与展示窗口分开；游标绑定scope和快照，合法父窗口进入后重签。
- ReadManagedContent + contentreader + tracking.wrapPage：精确版本鉴权读取，管理预览不注册客户浏览、不预热分享、不开原生分享菜单。

## 已剔除的非必要用户步骤

- 创建图文/视频时再次确认呈现形式。
- 发布前强制先打开预览页面。
- 新建项目必须先关联客户。
- 内部保存后继续生成版本和创建分享的强提示。
- 普通发送手动串联保存、冻结版本、创建分享。
- 查看素材访客前强制进入分享批次中间页。

项目名称与合法归属、笔记标题与非空正文、服务端preview_token、固定版本/私有媒体权限、指定成员正式确认等必要规则继续保留。未新增自动保存、自动发布或自动扣费。

本目录另提供32项任务、验收表、接口契约、审查结果和发布回退步骤。

## 原生验收后的追加修改

- 内容工作台：已发布动作缩短为“分享”；新建图标使用主题色。图文/视频编辑器合并标题和介绍区，压缩空白，保留AI辅助；视频归属放入内容设置，不扩大权限。
- 项目详情：版本、资料、成员和记录收进可展开“更多管理”，进入其他项目时重置展开状态；编辑区继续保留保存笔记/发给客户。
- 上述视觉收敛涉及13个前端文件，独立审查P0/P1=0；具体修改位于既有contentworkbench、contentedit、projectnotes及对应测试，375/390宽各18态已完成原生截图及button/layout边界检查，无横向溢出，视觉P0/P1=0、P2=4非阻断。
- [service/tracking/store.go](../../../service/tracking/store.go)：租户锁从INSERT IGNORE后SELECT FOR UPDATE改为单条ON DUPLICATE KEY UPDATE，避免并发重复键共享锁升级死锁。
- [service/tracking/store_mysql_test.go](../../../service/tracking/store_mysql_test.go)：新增真实MySQL并发回归；旧24并发1213红、新20轮绿。同步[read_transaction_test.go](../../../service/tracking/read_transaction_test.go)、[scoped_sql_test.go](../../../service/tracking/scoped_sql_test.go)及[share_tracking_legacy_transaction_test.go](../../../service/route/share_tracking_legacy_transaction_test.go)的锁行为断言。
- 素材→访客首次onShow重置as_of已在customers/shares两分支修复，真实素材as_of保持一致、红绿回归通过；最终独立审查APPROVE、P0/P1/P2=0。修改涉及sharevisits TS/JS与对应回归测试，见c6561eb。

最新1701/1701 JS（最终日志/tmp/jdlg-mini-final-20261115.log）与Go全量/vet通过；真实角色、管理预览0请求、无权限拒绝、客户绑定/解除及隔离媒体适配边界见[VERIFICATION.md](VERIFICATION.md)。系统键盘、手机相册、真机及原生微信选收件人未执行；追加三提交已在隔离worktree反向revert，精确恢复febfe4c树053a70974cb422457200fa1566b7366a69d6ece0。DevTools与GitHub五项CI门禁均已通过；PR #51已合并，后端/后台已部署、小程序8.0.9已上传。实际回执见release-receipt.json；真机、体验版指向与正式发布单列。
