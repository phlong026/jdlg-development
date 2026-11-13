# 小程序客户查看逐次记录

`GET /api/v1/miniprogram/tracking/my-visits` 为设计师工作台“客户查看”提供逐次访问卡片。

## 权限和范围

- 要求真实小程序登录、活跃账号与本人分享查看权限。
- 服务端只聚合当前设计师本人创建或转发的分享链路；同归属空间内其他员工的分享不会进入结果。
- 路由与客户榜共同受 `TRACKING_MY_CUSTOMER_VIEW_ENABLED=true` 控制。
- 返回历史标题、封面和状态投影，不返回内容正文、客户手机号、管理端客户 ID 或分享凭据。

## 查询参数

支持 `start/end/as_of/limit/cursor/share_cursor/q/intent/channel`。时间使用 RFC3339Nano。

`channel` 可选：`app_message`、`timeline`、`code_link`、`direct`、`unknown`。`cursor` 绑定账号、租户、授权空间、时间、来源、搜索、意向和分享分段，不能跨筛选条件复用。

## 返回口径

每个 `page_view` 形成一张卡；`visible_segment` 和视频心跳只补充时长、观看进度，不增加访问次数。列表按 `occurred_at` 倒序，时间相同按 `view_id` 稳定排序。

顶层返回 `items/next_cursor/as_of/metric_version/share_window`。每个 `items[]` 包含：

- 访问标识：`view_id/session_id/occurred_at/share_id`。
- 素材投影：`route_key/content_id/content_version/title/cover/type/status`。素材已失效时只返回可授权的历史投影，不回读正文。
- 客户投影：`visitor`，字段与现有 `Visitor` 一致，包含 `visitor_key/nickname/avatar_url/identity_state/intent`。
- 本次指标：`visible_ms/video_watch_ms/progress_pct/progress_available/content_page_views`。
- 来源：`source.kind/source.channel`。

- `visible_ms`：该次访问在前台有效可见的去重时长。
- `video_watch_ms`：该次访问内合规播放样本累计的观看时长。
- `progress_pct`：由服务端验收后的 `played_ranges` 去重覆盖率计算，封顶 100。
- `progress_available`：是否存在可解释的视频覆盖数据；为 false 时客户端不显示视频进度。
- `content_page_views`：当前筛选时间及来源范围内，该访客查看同一素材版本的有效次数。
- `source.kind/channel`：入口来源和微信分享渠道；客户端应将缺失来源显示为“来源未记录”。
- `share_window`：当前最多 400 条本人分享的统计分段；使用 `share_cursor` 查看更早分段。
