# 工作台修复后的接口契约

本页描述修复分支代码，不表示服务器已发布。响应保持`{code,msg,data}`，小程序身份和签名沿用既有请求层；所有本人范围由服务端核验。

## 项目与客户关联

`GET /api/v1/miniprogram/tracking/my-visitors/project-customers`：获取本人客户候选，支持`q/visitor/limit/cursor/as_of/share_cursor`。`visitor`为精确的`u:<账号ID>`，仅当前有权关系可返回。普通页返回items、next_cursor、as_of、share_window；items包括customer_ref、nickname、conversion_status、customer_handoff、project_activity_count。

`POST /api/v1/miniprogram/project-notes/projects`：原name/owner_id保留；可选`customer_ref`，服务端重新核验目标项目创建权和独立本人客户读取范围。客户端传CRMID仍拒绝；customer_ref不是管理端CRM ID或通用跨域授权。没有客户关联时旧请求保持兼容。

客户详情可返回`customer_handoff`（本人转化状态、是否已分配、下次跟进时间、联系授权是否就绪）与`conversion_reason`。不返回手机号、其他渠道管理线索ID或其他设计师详情。已有线索返回ID0时也通过converted表达完成，不依赖lead_id非零。

## 客户统计窗口与项目活动

客户/素材统计增加`share_window`：单次仍有400条分享预算；`truncated/next_cursor`说明是否存在更早窗口，后续请求用`share_cursor`进入下一段。普通记录分页cursor与分享窗口share_cursor独立；必须保持当前as_of，错账号/授权范围的窗口游标拒绝。界面显示分段口径，不将单段当全量。

客户列表及详情合并本人有权项目的阅读、反馈、确认和显式客户关联摘要。只返回项目ID/名称、事实类型和时间，不返回私密正文、反馈原文、媒体或分享凭据。`project_linked`表示关联，不增加浏览次数。项目活动上限与`project_activities_truncated`单独表示，完整项目历史仍在本人项目页面读取。

## 受邀成员固定方案阅读

路径前缀`/api/v1/miniprogram/project-notes/member-projects`：

- `GET /:id`：项目固定版本目录，支持limit/cursor。
- `GET /:id/versions/:version`：读取该项目的固定方案。
- `GET /:id/versions/:version/media/:media`：读取固定版本内素材。
- `POST /:id/versions/:version/media/:media/playback`：短时播放票据。

这些读取每次检查项目有效性、活跃成员资格和版本/素材归属，不授予草稿编辑、成员管理或创建人权限；播放票据发出后撤权也不能继续播放。反馈和确认沿用设计师实际分享的方案链接及指定确认人规则。

## AI视频采用与重试

路径前缀`/api/v1/miniprogram/ai-video-studio`：

- `GET /adoption-targets/projects/:project/notes/:note`：只读目标修订和规范化块位置；plain转换预览与实际采用使用同一算法，保留原正文、媒体及封面。
- `POST /media/:id/adopt/project-note`：expected_revision、project_id、note_id、insert_at及稳定draft_instance_id；账号/项目/笔记/位置失效时拒绝。归档项目拒绝新插入。
- `POST /media/:id/adopt/content`：owner_id、title及稳定draft_instance_id；客户端在发出前持久保存操作身份，丢响应后恢复原请求，同键不新建第二份草稿。

采用只修改草稿，不自动冻结、发布或更新已有固定分享版本。已有块格式正文在项目页保持排版预览及标题编辑，不把JSON作为纯正文再次保存。

旧AI图片`POST /api/v1/miniprogram/workbench/contents/from-generation`新增可选idempotency_key；新客户端失败重试复用，成功后优先继续编辑已存内容，明确另存才换键。旧客户端无键时按任务+归属提供兼容稳定键。已删除草稿不能靠重试复活，已编辑正文不被原生成输入覆盖。

## 发布与回退

新字段位于既有JSON记录和请求，保留旧字段、拒绝未知/越权输入。没有删除旧表/列或媒体。回退保留生成采用事实、客户触达和项目固定版本；本地代码回退验证与生产数据恢复分别记录。服务号外发另见official-account.md，缺真实配置时保持关闭。
