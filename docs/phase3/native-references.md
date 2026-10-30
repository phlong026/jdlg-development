# 原生内容引用库

2026-10-27 新增。本增量复用已发布内容的不可变版本、原生阅读页与既有分享 SDK，没有复制公开正文、生成第二套分享凭据或新增数据库表。

## 当前可操作流程

内容工作台“已有资料”入口 → 选用原生内容 → 查看当前授权范围的已发布版本 → 保存引用 → 打开该固定版本 → 在内容阅读页使用微信原生分享。引用保存的是精确 `content_id + content_version`。A 发布、B 选用后，B 在原生阅读页创建自己的 Share，SDK 追加该 Share 的 `st`，作者不会代替实际分享人。

引用属于当前创建账号，授权范围内的其他账号不能通过引用 ID 读取或移除它。来源后续发布 V2 不改变已选 V1。来源撤回或当前授权撤销后重新校验失败，不能继续打开。列表中已撤回引用保留展示为不可用，可移除；删除保留幂等墓碑，不会使丢响应的旧创建请求重新生成引用。

## 接口

前缀：`/api/v1/miniprogram/workbench`。沿用签名认证、封禁检查、`can_manage` 与 owner 授权，并要求 `CONTENT_WORKBENCH_ENABLED=true`、内容发布能力启用。

| 方法 | 路径 | 契约 |
| --- | --- | --- |
| GET | `/native-reference-candidates?cursor=&limit=` | `items` 为已发布快照的 route/content/version/title/cover/published_at；默认20，最大50 |
| POST | `/native-references` | 只接受 `content_id`、`content_version`、`idempotency_key`；标题、封面、owner、route、创建人均由服务端推导 |
| GET | `/native-references?cursor=&limit=` | 当前账号的引用，含 `reference_id`、`target`、`available`、`created_at`；不包含正文、owner 或分享 token |
| DELETE | `/native-references/:id` | 当前创建人及当前授权范围内移除自己的引用，原内容保持不变 |
| GET | `/native-references/:id/target` | 再次校验精确发布版本后返回 `route_key` 和原生 `path`，不含 `st`；实际分享由原生阅读页创建 |

创建幂等绑定账号和请求键；同键不同来源/版本返回409。JSON上限128KiB，拒绝未知字段。客户端不能提交 `sharer_id`、`owner_id`、任意 route 或分享 token。

候选和引用按数据库扫描行使用稳定游标，每次扫描最多50条；其中可能含草稿或已移除引用，因此某页可能为空但仍有 `next_cursor`，前端仍允许继续加载。来源丰富信息使用三批读取（master/version/revoked），不逐条查询；数据库异常直接上抛，不当成来源撤回。页面最多显示200条，继续加载会换到后续窗口，仍可抵达后续来源。

## 验证与边界

定向测试覆盖服务端推导、幂等冲突、私有草稿/legacy/跨owner/跨账号拒绝、撤回、分页不丢行、三批读取与故障传播、V1不变及实际B创建分享。前端覆盖签名字段、响应丢失重试、账号变化、目标白名单、保存期间hide/show和第201条可达。类型检查、路由清单、原生WXML编译与独立审查通过。新增页从公开跟踪中排除，当前共有67个路由、22个公开、45个排除。

当前只接入可回放的已发布内容。旧模板、灵感作品及公开造价快照仍需来源适配：legacy元数据哈希不能冒充可回放的正文历史版本。生成记录可通过页面底部入口继续打开；私人笔记、未发布草稿、私人模板或私有造价结果不会出现在此公共引用候选中。此限制没有缩减原合同范围。

本地实现和审查不等于生产发布或微信真机卡片验收。旧卡片直接转发仍保留原Share来源，不能推断全部中间转发人、送达或已读。
