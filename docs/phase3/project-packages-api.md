# 项目内“分享资料”服务边界

本文件记录 D01–D05 的服务核心与后续接入点。当前实现位于 `service/tracking/project_packages.go`，不改变原有单篇项目笔记分享接口。

## 已实现的服务能力

### 1. 草稿与预览

- `ProjectPackageSaveDraft`
  - 新建或按 `expected_revision` 更新草稿。
  - `idempotency_key` 相同且请求相同会返回同一草稿；相同键对应不同请求返回 409。
  - 可组合当前项目已保存笔记和既有固定版本；笔记选择显式携带 `note_id + revision`，不按标题猜关联。
  - 所有内容必须属于同一项目，合计最多20项。
  - 草稿允许暂不选接收人，便于分步编辑；不会自动选择项目全体成员。
- `ProjectPackagePreview`
  - 发布前重新读取每个固定版本，并核对每篇已保存笔记的 revision。
  - 接收人必须是当前有效项目成员，且至少明确选择一人。
  - 返回 `preview_signature`。草稿修订、版本清单或接收人变化后，旧签名不能发布。

### 2. 固定交付与权限

- `ProjectPackagePublish`
  - 按预览结果在同一事务中统一冻结所选笔记，再生成固定版本清单和接收人白名单。
  - 任一笔记在预览后变化或冻结失败，全部新版本与交付一起回滚；既有固定版本不会重复冻结。
  - 发布与幂等请求在同一事务中完成，不会留下半份交付。
  - 网络重试不会重复生成交付；重放响应不再次返回原始令牌。
- `ProjectPackageRead`
  - 目录读取必须同时满足：令牌有效、交付未撤回且未过期、设计师项目授权仍有效、账号在本次白名单、账号仍是当前有效项目成员。
  - 有链接但未入选的其他项目成员不能读取。
- `ProjectPackageRevoke`
  - 撤回后原链接立即失效。

### 3. 子版本上下文

- `ProjectPackageReadVersion`
  - 先验证组合令牌和接收人，再验证 `version_id` 确实存在于固定清单。
  - 同项目但未列入本次交付的版本仍返回 404。
  - 反馈、处理结果和确认记录继续按精确版本聚合。
- `ProjectPackageReadVersionMedia`
  - 只允许读取该固定版本快照中已有的封面、图片或视频。
- `ProjectPackageAddFeedback`
  - 反馈绑定具体固定版本，复用现有设计师反馈消息和处理链路。
- `ProjectPackageConfirmVersion`
  - 仅当前指定确认人可确认，且确认仍唯一绑定具体版本。
  - 与单篇分享产生的同版本确认共用原有唯一性约束。

组合令牌不得传给现有 `ProjectNoteReadShare`、单篇反馈或单篇确认接口。组合服务使用独立令牌记录，并在服务端建立受验证的子版本上下文。

### 4. 回执与历史

- `ProjectPackageAcknowledge`
  - 只有显式调用才写入阅读回执。
  - 打开目录或读取正文不会被自动描述成“已读”。
  - 可绑定目录或清单内的具体版本，事件 ID 幂等且不能改绑。
- `ProjectPackageGetHistory`
  - 设计师可读取有界的交付与显式回执记录。
  - 历史响应不返回组合令牌。

## 当前支持的资料类型

当前接受已保存的 `ProjectNote` 和已经冻结的 `ProjectNoteVersion`。已保存笔记在最终发布时统一冻结，固定版本内已经快照化的封面、图片和视频可随版本授权读取。

项目目前没有独立、可靠、可回溯的文件版本关联模型，因此本实现没有增加任意 `file_id` 字段，也没有把资料库文件假装成项目交付文件。要支持 PDF、Word 或其他文件，必须先建立“文件版本—项目—授权归属”的服务端关系，再将该固定文件版本加入组合清单。

## HTTP 接口映射

`service/route/project_packages.go` 的以下处理器已注册。管理端复用项目设计师权限；接收端仅要求登录，再由每个接口核验项目与本次接收资格。

| 方法 | 路径 | 服务方法 |
| --- | --- | --- |
| PUT | `/api/v1/miniprogram/project-notes/projects/:project_id/packages/draft` | `ProjectPackageSaveDraft` |
| GET | `/api/v1/miniprogram/project-notes/packages/:id/preview` | `ProjectPackagePreview` |
| POST | `/api/v1/miniprogram/project-notes/packages/:id/publish` | `ProjectPackagePublish` |
| POST | `/api/v1/miniprogram/project-notes/package-deliveries/:id/revoke` | `ProjectPackageRevoke` |
| GET | `/api/v1/miniprogram/project-notes/projects/:project_id/package-history` | `ProjectPackageGetHistory` |
| GET | `/api/v1/miniprogram/project-packages/shared/:token` | `ProjectPackageRead` |
| GET | `/api/v1/miniprogram/project-packages/shared/:token/versions/:version_id` | `ProjectPackageReadVersion` |
| GET | `/api/v1/miniprogram/project-packages/shared/:token/versions/:version_id/media/:media_id` | `ProjectPackageReadVersionMedia` |
| POST | `/api/v1/miniprogram/project-packages/shared/:token/versions/:version_id/media/:media_id/playback` | 返回 `{url, expires_at}` 短时视频播放地址，逐请求复核固定版本权限 |
| POST | `/api/v1/miniprogram/project-packages/shared/:token/versions/:version_id/feedback` | `ProjectPackageAddFeedback` |
| POST | `/api/v1/miniprogram/project-packages/shared/:token/versions/:version_id/confirm` | `ProjectPackageConfirmVersion` |
| POST | `/api/v1/miniprogram/project-packages/shared/:token/receipts` | `ProjectPackageAcknowledge` |

## 集成与提醒

- 管理和接收页面均已注册到 `app.json`，并排除公开访问采集。项目详情“分享资料”可进入选择、预览和交付流程。
- 组合 API 独立于单篇 API，不能把组合 token 传给单篇接口。受邀组不加载设计师管理 Scope，业主无需管理端二次登录。
- 反馈消息沿用精确版本和反馈定位；主动阅读回执新增 `project_package_receipt`，按交付、接收人和版本去重，站内消息返回具体方案。
- 阅读回执是已经发生的事实。交付后来撤回仍保留历史回执，但当前设计师权限被移除后不得查询；它不恢复资料读取资格，也不触发服务号外发。
- `tracking_records` 为通用记录表，新增记录类型无需新增表。回退程序保留记录，不删除业务数据。
- 独立 PDF/Word 文件组合尚未实现。页面只展示方案及其固定媒体，不把资料库文件假装成已授权项目附件。
- 服务器启用、小程序上传和真机验收仍需分别记录。

分享资料接收页与单篇项目笔记均要求昵称与头像登记完整。视频地址契约、10分钟票据生命周期和 Range 行为与 [单篇项目笔记](private-project-notes.md) 一致。只允许当前交付清单内的精确版本和 MP4 素材，不把组合令牌或账号长期凭据放入播放器 URL。
