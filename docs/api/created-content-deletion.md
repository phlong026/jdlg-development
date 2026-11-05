# 删除自建内容与项目合集

## 自建内容

`POST /api/v1/miniprogram/workbench/contents/:id/delete`

## 项目合集

`POST /api/v1/miniprogram/workbench/project-collections/:id/delete`

两个接口均使用当前修订号：

```json
{
  "expected_revision": 3
}
```

仅能删除当前账号有权管理的内容。自建内容支持草稿、已发布和已撤回状态；合集支持草稿、已发布和已撤回状态。修订号不一致返回 `409`，客户端应重新读取后再操作。

删除为逻辑删除：保留已上传媒体、固定版本和审计历史，但当前目录、旧公开链接、旧合集入口会立即失效。删除合集不会删除合集中的原作品。

旧客户端的 `DELETE /api/v1/miniprogram/workbench/contents/:id` 和 `DELETE /api/v1/miniprogram/workbench/project-collections/:id` 仍保留，且仍只允许删除从未发布的草稿。
