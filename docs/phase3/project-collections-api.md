# 公开项目合集服务边界与接口

更新日期：2026-10-31。本文记录 C01–C07 的后端契约。服务、处理器、小程序页面、共享路由及 C07 字段已接通。网页目录、子作品和精确目标 handoff 已集成；本文记录代码实现，不代表服务器部署或真机验收。

## 1. 数据和权限边界

- 合集只保存同一 `owner_id` 范围下的有序 `(content_id, content_version)` 引用，不复制原作品正文和媒体。
- 合集的管理者是创建它的当前账号。读取、修改、预览、发布、撤回和删除同时检查账号及 `owner_id` 范围。
- 管理权和公开展示权分开。管理端使用 `Scope.Manage` 及归属授权；已发布读取不依赖创建者当前的编辑权，但每次都重新检查合集状态及每个原作品的当前公开可用性。
- 不可加入私密草稿、未发布版本、其他归属的作品、撤回作品、只含旧元数据摘要的只读快照或同一作品的重复版本。
- 从合集移除项目、删除未发布合集草稿或撤回合集，都不删除原作品及原媒体。
- 已发布源失效时，公开清单保留对应顺序的“不可用”占位，但不返回旧标题、封面、内容标识或版本，也不自动替换成新版本。

## 2. 服务层方法

| 方法 | 用途 | 关键约束 |
| --- | --- | --- |
| `ProjectCollectionCandidates` | 分页获取可加入合集的公开作品 | 直接复用已有 `NativeReferenceCandidates`；服务端分页和授权筛选 |
| `SaveProjectCollection` | 新建或修改草稿 | 新建必须带幂等标识；修改必须带当前 `expected_revision` |
| `ProjectCollections` | 按当前账号分页列表 | 服务端按归属及创建账号精确限制 |
| `ProjectCollection` | 读取可管理合集 | 不向其他账号或其他归属暴露合集是否存在 |
| `PreviewProjectCollection` | 预览当前草稿 | 再次批量核验全部固定来源；预览凭证 15 分钟后失效 |
| `PublishProjectCollection` | 发布不可变清单版本 | 校验 revision、预览凭证及幂等标识；发布前再次核验全部来源 |
| `ReadPublishedProjectCollection` | 读取指定合集版本 | 清单版本不变；实时检查合集撤回和每个原作品的公开可用性 |
| `OpenPublishedProjectCollectionItem` | 从合集目录打开固定子作品 | 按清单位置重新校验合集和来源；返回合集上下文，不签发新分享凭证 |
| `ReadPublishedProjectCollectionItemContent` | 读取固定子作品正文 | 每次重验合集、清单位置及源作品；内部媒体 URL 改为合集上下文路径 |
| `ReadPublishedProjectCollectionItemMedia` | 读取固定子作品媒体 | 每次重验合集、清单位置、源作品及媒体归属 |
| `RevokeProjectCollection` | 撤回已发布合集 | 不删除原作品；撤回后所有历史合集版本均不可公开读取 |
| `DeleteProjectCollectionDraft` | 删除从未发布的草稿 | 仅 `version=0` 可执行；保留审计记录且不删除原作品 |

## 3. 版本与冲突规则

1. 创建请求用 `actor + idempotency_key` 定位；同标识、同参数返回同一合集，同标识、不同参数返回 409。
2. 草稿每次保存提升 `revision`，过期 revision 不能覆盖新数据。
3. 发布时生成新 `version`，把当时有序内容清单写入不可变版本记录，再提升草稿 `revision`。
4. 后续改顺序、添加或移除作品只改当前管理记录；重新发布形成 V2，V1 不会被静默替换。
5. 原作品发布新版本不改变已发布合集中的旧 `(content_id, content_version)` 引用。

## 4. HTTP 处理器映射（已注册）

| HTTP | 路径 | 服务方法 |
| --- | --- | --- |
| `GET` | `/api/v1/miniprogram/workbench/project-collections/candidates` | `ProjectCollectionCandidates` |
| `GET` | `/api/v1/miniprogram/workbench/project-collections` | `ProjectCollections` |
| `POST` | `/api/v1/miniprogram/workbench/project-collections` | `SaveProjectCollection(..., "", ...)` |
| `GET` | `/api/v1/miniprogram/workbench/project-collections/{id}` | `ProjectCollection` |
| `PATCH` | `/api/v1/miniprogram/workbench/project-collections/{id}` | `SaveProjectCollection(..., id, ...)` |
| `POST` | `/api/v1/miniprogram/workbench/project-collections/{id}/preview` | `PreviewProjectCollection` |
| `POST` | `/api/v1/miniprogram/workbench/project-collections/{id}/publish` | `PublishProjectCollection` |
| `POST` | `/api/v1/miniprogram/workbench/project-collections/{id}/revoke` | `RevokeProjectCollection` |
| `DELETE` | `/api/v1/miniprogram/workbench/project-collections/{id}` | `DeleteProjectCollectionDraft` |
| `GET` | `/api/v1/miniprogram/tracking/project-collections/{id}/versions/{version}` | `ReadPublishedProjectCollection` |
| `GET` | `/api/v1/miniprogram/tracking/project-collections/{id}/versions/{version}/items/{position}` | `OpenPublishedProjectCollectionItem` |
| `GET` | `/api/v1/miniprogram/tracking/project-collections/{id}/versions/{version}/items/{position}/content` | `ReadPublishedProjectCollectionItemContent` |
| `GET` | `/api/v1/miniprogram/tracking/project-collections/{id}/versions/{version}/items/{position}/media/{media_id}` | `ReadPublishedProjectCollectionItemMedia` |

管理路由实现在 `service/route/project_collections.go`，必须挂到现有已签名的 workbench group，由服务端登录上下文构造 `Scope`，不得接受客户端传入 `actor`、`owner_id` 列表或 `Manage`。公开读取与既有 contentreader 一致：发布内容 GET 不要求管理权，小程序仍复用原登录／昵称门禁。公开路由只接受合集 ID、版本和目录位置，不直接读取版本记录中的原标题来绕过当前来源检查。

## 5. C07 分享归因与统计

- 合集分享沿用现有分享接口：`route_key=pages/collectionreader/collectionreader`，`content_id` 和 `content_version` 分别放合集 ID 与版本。
- 目录 Enter 沿用合集 route/content/version。进入固定子作品时，EntryInput 在真实子作品 route/content/version 之外追加 `collection_id`、`collection_version`，并继续使用原合集 `share_token`。
- 服务端只有在分享目标为该合集、不可变清单确实包含该子版本、合集和源作品当前均可访问时，才把实际分享人 B 的 `share_id` 继承到子作品 Entry。客户端声明但无效的合集上下文直接拒绝，不降级为普通直访采集。
- Entry 持久化合集上下文；事件、通知投递前资格复核都会重新检查。合集撤回或源作品失效后，旧 Entry 不能继续产生有效事件或提醒。
- `MyShares.page_views` 保持历史兼容的总 page view，另返回 `directory_views` 和 `content_views`，避免把打开目录误写成全文阅读。
- 浏览提醒收件人仍是实际小程序分享人 B；`collection_directory_browse` 表示打开合集，`collection_content_browse` 表示查看合集中的作品。
- 公开 GET 只读取，不创建 Entry、事件或提醒。所有采集仍受全局 consent 版本约束。

## 6. 客户端与网页

- 原生工作台从“项目合集”进入管理；合集阅读页提供好友分享和复制网页链接。内部打开作品只传递已验证的原分享 token，不新建读者分享。
- 合集目录封面和子作品内部媒体均使用合集范围内的鉴权地址；原作品撤回后，目录中的标题和封面不再展示。
- `/share/s/:code` 展示目录，`/share/s/:code/items/:position` 展示固定子作品。网页 GET、封面预取不会生成阅读记录。
- 网页进入小程序只有在来源 Entry、已确认 page_view 和最终原生目标完全一致时才返回 `handoff_ready=true`。目录来源不能给子作品目标签发相同证明。
- SDK 在身份切换、撤回分析同意或访问过期时清除原分享归因；普通重试不会恢复已经清除的 token。

## 7. 范围限制
- 跨企业或跨归属授权平台。一个合集只有一个现有 `owner_id`。
- 私密项目资料包；它使用项目成员及接收人权限，不与公开合集共用“公开/私密”开关。
