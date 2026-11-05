# 导入启用与长按删除：修改清单

基线bc5405b，分支feat/content-import-delete-20261105。用户授权启用生产导入、为自建内容增加长按或侧滑删除。本轮选择统一长按入口，保留作品及生成记录已有侧滑。

## 业务变化

| 范围 | 最终行为 |
| --- | --- |
| 导入运行配置 | 接入现有Capture四项配置，启用CONTENT_IMPORT_ENABLED；保留其他配置、私密备份及失败回退；原启动环境和程序不变 |
| 实际抖音导入 | 用户提供链接7703942270734568713已完成queued→downloading→ready，保存私有媒体并生成未发布草稿；微信私有下载与getVideoInfo成功，MP4时长162秒、1080×1920、16934KB |
| 项目 | 长按→删除菜单→二次确认；创建者、Manage、owner、revision在后端事务内复核；删除墓碑使列表/详情/版本/编辑/媒体/旧私密分享/资料包/邀请失效 |
| 项目笔记 | 列表长按复用既有笔记删除确认；冻结历史沿用原业务规则，项目整体删除则所有访问入口失效 |
| 内容 | 草稿、已发布、已撤回均可长按删除；旧草稿DELETE接口契约保留；通用删除原子取消旧分享/固定版本读取/原生引用与待投递通知 |
| 合集 | 全状态长按逻辑删除，旧合集固定版本和分享失效，源作品保留；同一事务按collection_id取消目录及子作品通知，取消失败则回滚删除 |
| 资料库 | 长按或原按钮均先确认；仅移除自己的引用，不删除原作品 |
| 作品/生成记录 | 长按复用已有删除与二次确认；保留已有侧滑，异步回调复核当前账号，长按不误开详情 |
| 状态保护 | 确认取消不发送删除；账号/epoch/revision变化拒绝旧回调；409保留行并恢复读取；同账号前后台切换释放旧epoch操作锁，旧响应不释放新锁 |

## 验证与数据边界

- 小程序完整1086项测试、原生WXML编译零失败零跳过，TypeScript通过。
- Go完整测试及go vet通过；发布工具58项隔离测试通过；JS语法、生成物同步、签名/跟踪路由及差异检查通过。
- 生产导入任务表19列与全部必需索引已核验，启用前队列为空，无需迁移。
- 真实导入仅使用本次用户提供并授权验收的抖音链接。导入生成草稿，不自动发布；不删除该草稿。
- 实际删除验证只用本次新建验收对象，用户原有项目和作品不参与删除测试。历史与COS媒体保留，活动访问入口按删除语义失效。
- 后端部署、小程序上传、体验版、微信审核与正式发布分别记录，不把代码或上传通过表述为正式发布。

## 文件与修改部位

以下为相对仓库根的文件。TS为维护源，对应JS同步生成。

| 文件 | 修改侧行号 | 类型 |
| --- | --- | --- |
| [docs/api/created-content-deletion.md](../../../docs/api/created-content-deletion.md) | 新增文件 | 接口与实施说明 |
| [docs/api/project-note-project-deletion.md](../../../docs/api/project-note-project-deletion.md) | 新增文件 | 接口与实施说明 |
| [docs/superpowers/plans/2026-11-05-content-import-and-deletion.md](../../../docs/superpowers/plans/2026-11-05-content-import-and-deletion.md) | 1 | 接口与实施说明 |
| [miniprogram/miniprogram/pages/contentworkbench/contentworkbench.js](../../../miniprogram/miniprogram/pages/contentworkbench/contentworkbench.js) | 357、368 | 界面交互及运行产物 |
| [miniprogram/miniprogram/pages/contentworkbench/contentworkbench.ts](../../../miniprogram/miniprogram/pages/contentworkbench/contentworkbench.ts) | 358、369 | 界面交互及运行产物 |
| [miniprogram/miniprogram/pages/contentworkbench/contentworkbench.wxml](../../../miniprogram/miniprogram/pages/contentworkbench/contentworkbench.wxml) | 23 | 界面交互及运行产物 |
| [miniprogram/miniprogram/pages/generatehistory/generatehistory.js](../../../miniprogram/miniprogram/pages/generatehistory/generatehistory.js) | 49、469、489、614、622、625 | 界面交互及运行产物 |
| [miniprogram/miniprogram/pages/generatehistory/generatehistory.ts](../../../miniprogram/miniprogram/pages/generatehistory/generatehistory.ts) | 106、554、571、705、714、716 | 界面交互及运行产物 |
| [miniprogram/miniprogram/pages/generatehistory/generatehistory.wxml](../../../miniprogram/miniprogram/pages/generatehistory/generatehistory.wxml) | 96 | 界面交互及运行产物 |
| [miniprogram/miniprogram/pages/nativereferences/nativereferences.js](../../../miniprogram/miniprogram/pages/nativereferences/nativereferences.js) | 122、124 | 界面交互及运行产物 |
| [miniprogram/miniprogram/pages/nativereferences/nativereferences.ts](../../../miniprogram/miniprogram/pages/nativereferences/nativereferences.ts) | 123、125 | 界面交互及运行产物 |
| [miniprogram/miniprogram/pages/nativereferences/nativereferences.wxml](../../../miniprogram/miniprogram/pages/nativereferences/nativereferences.wxml) | 26 | 界面交互及运行产物 |
| [miniprogram/miniprogram/pages/projectcollections/projectcollections.js](../../../miniprogram/miniprogram/pages/projectcollections/projectcollections.js) | 264、284 | 界面交互及运行产物 |
| [miniprogram/miniprogram/pages/projectcollections/projectcollections.ts](../../../miniprogram/miniprogram/pages/projectcollections/projectcollections.ts) | 265、285 | 界面交互及运行产物 |
| [miniprogram/miniprogram/pages/projectcollections/projectcollections.wxml](../../../miniprogram/miniprogram/pages/projectcollections/projectcollections.wxml) | 13 | 界面交互及运行产物 |
| [miniprogram/miniprogram/pages/projectnotes/projectnotes-core.js](../../../miniprogram/miniprogram/pages/projectnotes/projectnotes-core.js) | 152、162、165、272、476、650、654、659、670 | 界面交互及运行产物 |
| [miniprogram/miniprogram/pages/projectnotes/projectnotes.wxml](../../../miniprogram/miniprogram/pages/projectnotes/projectnotes.wxml) | 14、34 | 界面交互及运行产物 |
| [miniprogram/miniprogram/pages/release/release.js](../../../miniprogram/miniprogram/pages/release/release.js) | 485、493、497、518 | 界面交互及运行产物 |
| [miniprogram/miniprogram/pages/release/release.ts](../../../miniprogram/miniprogram/pages/release/release.ts) | 524、532、535、555 | 界面交互及运行产物 |
| [miniprogram/miniprogram/pages/release/release.wxml](../../../miniprogram/miniprogram/pages/release/release.wxml) | 124 | 界面交互及运行产物 |
| [miniprogram/miniprogram/utils/phase3-api.js](../../../miniprogram/miniprogram/utils/phase3-api.js) | 65、110、207、213 | 界面交互及运行产物 |
| [miniprogram/miniprogram/utils/phase3-api.ts](../../../miniprogram/miniprogram/utils/phase3-api.ts) | 66、111、208、214 | 界面交互及运行产物 |
| [miniprogram/miniprogram/utils/project-collections-api.js](../../../miniprogram/miniprogram/utils/project-collections-api.js) | 51、70 | 界面交互及运行产物 |
| [miniprogram/miniprogram/utils/project-collections-api.ts](../../../miniprogram/miniprogram/utils/project-collections-api.ts) | 52、71 | 界面交互及运行产物 |
| [miniprogram/tests/created-content-api.test.js](../../../miniprogram/tests/created-content-api.test.js) | 新增文件 | 行为/权限/回归测试 |
| [miniprogram/tests/created-content-deletion-ui.test.js](../../../miniprogram/tests/created-content-deletion-ui.test.js) | 新增文件 | 行为/权限/回归测试 |
| [miniprogram/tests/native-references.test.js](../../../miniprogram/tests/native-references.test.js) | 46、49、53、82 | 行为/权限/回归测试 |
| [miniprogram/tests/private-project-notes.test.js](../../../miniprogram/tests/private-project-notes.test.js) | 1206 | 行为/权限/回归测试 |
| [miniprogram/tests/project-collections-api.test.js](../../../miniprogram/tests/project-collections-api.test.js) | 42、46、49、53 | 行为/权限/回归测试 |
| [miniprogram/tests/project-collections-pages.test.js](../../../miniprogram/tests/project-collections-pages.test.js) | 399 | 行为/权限/回归测试 |
| [ops/phase3-release/import_activation.py](../../../ops/phase3-release/import_activation.py) | 1 | 接口/配置/生命周期 |
| [ops/phase3-release/test_import_activation.py](../../../ops/phase3-release/test_import_activation.py) | 1 | 行为/权限/回归测试 |
| [service/notification/collection_cancel_test.go](../../../service/notification/collection_cancel_test.go) | 新增文件 | 行为/权限/回归测试 |
| [service/notification/store.go](../../../service/notification/store.go) | 519 | 接口/配置/生命周期 |
| [service/route/content_deletion_test.go](../../../service/route/content_deletion_test.go) | 43 | 行为/权限/回归测试 |
| [service/route/content_workbench.go](../../../service/route/content_workbench.go) | 190 | 接口/配置/生命周期 |
| [service/route/phase3_notifications.go](../../../service/route/phase3_notifications.go) | 81 | 接口/配置/生命周期 |
| [service/route/project_collections.go](../../../service/route/project_collections.go) | 62 | 接口/配置/生命周期 |
| [service/route/project_collections_test.go](../../../service/route/project_collections_test.go) | 121 | 行为/权限/回归测试 |
| [service/route/project_note_lifecycle.go](../../../service/route/project_note_lifecycle.go) | 13 | 接口/配置/生命周期 |
| [service/route/project_note_lifecycle_test.go](../../../service/route/project_note_lifecycle_test.go) | 75 | 行为/权限/回归测试 |
| [service/tracking/content_deletion.go](../../../service/tracking/content_deletion.go) | 63 | 接口/配置/生命周期 |
| [service/tracking/content_deletion_test.go](../../../service/tracking/content_deletion_test.go) | 101 | 行为/权限/回归测试 |
| [service/tracking/project_collections.go](../../../service/tracking/project_collections.go) | 736、746 | 接口/配置/生命周期 |
| [service/tracking/project_collections_test.go](../../../service/tracking/project_collections_test.go) | 238 | 行为/权限/回归测试 |
| [service/tracking/project_note_lifecycle.go](../../../service/tracking/project_note_lifecycle.go) | 11、23、83 | 接口/配置/生命周期 |
| [service/tracking/project_note_project_deletion_test.go](../../../service/tracking/project_note_project_deletion_test.go) | 新增文件 | 行为/权限/回归测试 |
| [service/tracking/project_notes.go](../../../service/tracking/project_notes.go) | 305、316 | 接口/配置/生命周期 |
| [service/tracking/service.go](../../../service/tracking/service.go) | 20 | 接口/配置/生命周期 |
