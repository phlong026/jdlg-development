# 公开笔记互动与固定版本 Word 接口

所有路径均保留既有小程序签名校验。基础路径 `/api/v1/miniprogram/tracking/contents/:id/versions/:version/interactions`，仅可操作当前仍可访问的已发布固定版本。数据按现有租户 records 隔离，不复用模板评论，不改变项目笔记反馈。

| 方法与后缀 | 请求 | 返回 data |
| --- | --- | --- |
| GET 空后缀 | `limit` 默认20、最大50，`cursor` 可选 | `like_count`、`favorite_count`、`comment_count`、`liked`、`favorited`、`comments`、`next_cursor` |
| POST `/comments` | `{text}`，去首尾空白后1–500字 | `{comment_id,text,created_at,mine,author:{nickname,avatar}}` |
| POST `/like` | `{active:boolean}` | `{active,count}`，显式状态幂等 |
| POST `/favorite` | `{active:boolean}` | `{active,count}`，显式状态幂等 |
| DELETE `/comments/:comment_id` | 空对象 | `{deleted:true}`，仅本人可软删除 |

GET匿名可用；提供token时正常校验身份并返回本人状态，错误token不会降级冒充匿名。写操作必须登录且未被封禁。响应使用 `Cache-Control: private, no-store`，8秒请求上下文超时；服务端错误记录路由与状态，不记录评论正文或令牌。评论作者仅投影昵称、头像。撤回、非法版本及不可访问内容拒绝读取或写入。

管理端固定版本导出：`GET|HEAD /api/v1/miniprogram/workbench/contents/:id/versions/:version/export.docx`。复用现有工作台身份、内容归属权限、DOCX导出限流、超时和安全响应头。导出所选发布版本的文字/图片；视频保留现有导出说明。图片缺失返回错误，不输出缺图文件。

没有新增依赖、配置或数据库迁移。复用现有records查询上限；单版本互动达到30,000行安全上限会返回不可用，不返回截断统计。暂不支持管理员删除其他人的评论。新服务部署前，旧服务的404由小程序显示简短不可用提示，正文继续可读。
