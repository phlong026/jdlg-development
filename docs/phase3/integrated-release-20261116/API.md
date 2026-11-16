# 作者与项目缩略图兼容契约

所有新增展示字段均可选，旧客户端忽略即可，未取得真实数据时不构造样例人物或图片。

## 内容作者

管理工作台的内容列表、搜索和管理读取，以及以下公开读取返回的 Content 可增加：

```json
{"author":{"display_name":"真实昵称","avatar":"https://公开头像地址"}}
```

- `/api/v1/miniprogram/tracking/contents/:id/versions/:version`
- `/api/v1/miniprogram/tracking/project-collections/:id/versions/:version/items/:position/content`

`display_name` 来自真实创建者当前资料；头像仅接受既有公开图片安全规则允许的 HTTPS 地址，头像失败回退不影响正文。创建者由服务端在创建内容时记录，修订与发布不转移作者；`creator_id` 不出现在客户端 JSON，创建/编辑请求不能指定 `creator_id` 或 `author`。

历史个人归属仅在规范 `designer-<正整数账号ID>` 可确认时回退到对应资料；历史团队归属没有已确认创建者时省略作者。资料查询异常记录警告并省略作者，内容读取继续按既有权限成功，不因为辅助资料故障阻断阅读。

## 项目列表图片

`GET /api/v1/miniprogram/project-notes/projects` 的每个项目可增加：

```json
{"preview_media":{"media_id":"授权图片ID","kind":"image","width":1600,"height":1200}}
```

图片来自当前固定版本的图片封面，缺少封面时取该版本第一张图片；没有固定版本或只有视频时省略。服务端一次批量读取版本，核对设计师、归属与项目，不逐项目查询；不返回原始 URL、存储路径、令牌、标题或 MIME。

客户端继续使用既有受保护项目媒体下载路由。401/403 进入既有权限失败处理并废弃迟到响应；单图片 404 或加载错误保留项目行并回退图标。首批最多下载六项，并发上限两项，账号、项目和页面代际切换后不回填旧图片。
