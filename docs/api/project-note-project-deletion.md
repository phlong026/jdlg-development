# 删除私人项目

`DELETE /api/v1/miniprogram/project-notes/projects/:id`

请求体：

```json
{
  "expected_revision": 3
}
```

成功返回项目删除墓碑：

```json
{
  "project_id": "project-id",
  "revision": 3,
  "deleted_by": 40,
  "deleted_at": "2026-11-05T10:00:00Z"
}
```

只有项目创建者在仍具有该归属的管理权限时可删除。`expected_revision` 必须与服务器当前修订号一致，否则返回 `409`。

删除是逻辑删除：保留项目、笔记、冻结版本和审计记录，不删除 COS 文件。删除后项目从列表移除，且项目详情、编辑、旧私密分享、资料包和加入邀请均不再可访问。

常见错误：

- `400`：请求体或字段无效。
- `404`：项目不存在、已删除或当前账号无权访问。
- `409`：修订号已过期，客户端应保留当前列表项并提示刷新。
