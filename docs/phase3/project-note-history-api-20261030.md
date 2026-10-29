# 项目笔记历史分页 API（2026-10-30）

本增量只分页读取用户主动产生的项目协作数据，不记录匿名或被动浏览身份。

## 项目管理者历史

```http
GET /api/v1/miniprogram/project-notes/projects/:project_id/history/:type
```

查询参数：

- `limit`：可选，默认 30，最大 50，超过 50 时按 50 返回。
- `cursor`：可选，使用上一页的 `next_cursor`，不得跨项目、跨类型或跨版本筛选复用。
- `version_id`：可选，仅 `feedbacks` / `resolutions` / `confirmations` 支持；版本必须属于当前项目。

`:type` 可用值：

- `notes`：笔记当前记录。
- `versions`：不可变版本摘要，只返回 `version_id` / `note_id` / `revision` / `title` / `frozen_at` / `media_count`，正文仍通过现有单版本精准读取接口获取。
- `feedbacks`：业主反馈。
- `resolutions`：设计师处理记录。
- `confirmations`：指定业主的方案确认事实。

返回结构：

```json
{
  "type": "feedbacks",
  "items": [],
  "next_cursor": "opaque-or-omitted"
}
```

列表按事实时间和记录 ID 稳定倒序。每页是一次有界查询，不扫描整个项目，也不按条目补查关联数据。

## 精准读取单条反馈

```http
GET /api/v1/miniprogram/project-notes/projects/:project_id/feedback/:feedback_id
```

仅当前项目的管理设计师可读；`feedback_id` 必须属于路径中的项目。该接口用于旧站内提醒按反馈 ID 直达。

## 精准读取当前活动草稿

```http
GET /api/v1/miniprogram/project-notes/projects/:project_id/notes/:note_id
```

仅当前项目的管理设计师可读，且 `note_id` 必须属于路径中的项目。接口返回最新草稿正文和 `revision`，供编辑保存时做乐观并发校验。已删除草稿虽仍保留审计记录，但该精准读取返回 404，不会将其重新打开为可编辑笔记。

## 受邀共享页历史

```http
GET /api/v1/miniprogram/project-notes/shared/:token/history/:type
```

只允许 `feedbacks` / `resolutions` / `confirmations`，查询自动固定到 token 邀请的唯一版本，不接受客户端 `version_id`。`limit` 和 `cursor` 规则与管理端相同。

每次翻页都重新校验登录账号、项目邀请、分享撤回和过期时间。权限被撤回后，已签发的游标不能继续读取；任何页均不返回其他项目或其他版本的数据。

## 路由注册

历史路由实现保持在 `registerProjectNoteHistoryHandlers(root, manage, svc, deps)`。合并本增量时，由主路由注册函数调用一次，不要在多处重复注册。
