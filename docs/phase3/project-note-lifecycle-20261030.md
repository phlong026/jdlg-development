# N03 项目笔记复制与软删除契约

## 范围

本次只增加项目笔记的「复制」和「删除」，不包含收藏，不新增表或身份模型，也不改变项目成员、业主确认、分享和反馈权限。

## API

### 复制笔记

```http
POST /api/v1/miniprogram/project-notes/notes/:note_id/copy
Content-Type: application/json

{
  "expected_revision": 3
}
```

成功返回一条新的可编辑 `ProjectNote`。新笔记：

- 使用新 `note_id`，修订号从 1 开始；
- 保持源笔记的标题、正文、正文格式、封面 ID 和媒体 ID；
- 只允许继续引用源笔记所在项目中、同 owner 的合法私有媒体；
- 不复制成员、确认人、冻结版本、分享记录、分享 token、打开记录、私密反馈或反馈处理记录。

### 删除笔记

```http
DELETE /api/v1/miniprogram/project-notes/notes/:note_id
Content-Type: application/json

{
  "expected_revision": 3
}
```

成功返回删除审计记录：

```json
{
  "note_id": "...",
  "project_id": "...",
  "revision": 3,
  "deleted_by": 7,
  "deleted_at": "2026-10-30T09:30:00Z"
}
```

删除新增 `note_deletion` tombstone，并仅清空原草稿记录的 `Session` 活动索引字段；草稿 payload、ID、Kind 及其他索引不变，不物理删除任何草稿、项目媒体、冻结版本、分享、反馈、反馈回复或确认记录。已冻结历史及其既有反馈仍可按原权限链读取。

## 权限与并发

两项操作都必须同时满足：

1. 小程序请求已通过现有登录、封禁和功能开关中间件；
2. `scope.Manage=true`；
3. scope 明确包含项目 `owner_id`；
4. actor 是项目原设计师；
5. `expected_revision` 与当前笔记修订号完全一致。

同 owner 的其他设计师、普通项目成员和仅持有分享 token 的访问者均不能复制或删除。修订号冲突返回 409；已软删除的笔记再次复制、删除、编辑或冻结均按不存在处理。

## 存储边界

- 复用现有 tracking record 存储，删除记录 kind 为 `note_deletion`，ID 与 `note_id` 相同，保证一条笔记只有一个 append-only tombstone。
- 不调用 `Repository.Remove`，不删除对象存储文件。活动草稿列表原本就按项目 ID 查询 `Session`，因此清空 `Session` 后自然排除软删草稿，不引入逐条查询 tombstone 的 N+1。
- 复制前重新执行正文校验和 `projectNoteSnapshotAssets` 项目归属校验，不能借复制引用其他项目或其他 owner 的私有媒体。
- 删除记录保存操作者、项目、删除时修订号和服务端时间，便于审计。

## 集成要求

生命周期实现位于独立文件，现有主项目笔记文件遵循以下最小接入约束：

1. 主路由注册 `registerProjectNoteLifecycleHandlers(manage, svc)`；
2. `ProjectNoteSaveNote`、`ProjectNoteFreezeVersion` 写入前调用 `projectNoteRequireActive`；
3. `ProjectNoteGetProjectDetail` 继续使用既有 `Session=project_id` 活动索引，无需逐条读取 tombstone；`Versions`、`Shares`、`Feedbacks`、`Resolutions` 和 `Confirmations` 不过滤。

项目列表不因删除单条笔记而删除项目；若产品以后需要「删除项目」，应另立项目级生命周期，不得把本契约扩展为级联物理删除。

## 验收边界

- 复制只新增一条草稿，不增加成员、版本、分享、反馈或确认数量。
- 伪造 `share_token` 等未知字段被严格 JSON 绑定拒绝。
- 跨项目媒体、越 owner、非原设计师、无 manage 权限和过期 revision 均失败。
- 删除后项目媒体仍可读取；既有冻结分享与反馈仍按原权限可读取。
- 删除后工作台不再返回该草稿，且不能继续保存或冻结该草稿。
