# 本轮详细修改清单

已完成53个既有文件的代码/测试/接口说明整改，另补本目录交付文档。原生及发布状态见[VERIFICATION.md](VERIFICATION.md)。

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
