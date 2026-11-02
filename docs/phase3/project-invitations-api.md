# 项目成员邀请 API

私人项目不再通过手工输入账号授权。设计师生成短期邀请，用户登录后提交加入申请，设计师明确批准后才创建项目成员权限。

## 设计师端

- `POST /api/v1/miniprogram/project-notes/projects/:id/member-invitations`
  - 请求：`expires_at`、`idempotency_key`
  - 返回：短期 `invitation_token`。有效期不得超过 7 天。
- `GET /api/v1/miniprogram/project-notes/projects/:id/member-invitations`
  - 只返回当前项目的有效邀请 ID、状态和到期时间，不再返回原始 token，供页面刷新后继续撤销。
- `DELETE /api/v1/miniprogram/project-notes/projects/:id/member-invitations/:invitation`
  - 撤销后，原邀请不能再提交或批准申请。
- `GET /api/v1/miniprogram/project-notes/projects/:id/member-requests`
  - 返回申请人昵称、头像与审核状态。账号 ID 仅作为服务端绑定值，页面不常驻展示。
- `POST /api/v1/miniprogram/project-notes/projects/:id/member-requests/:request/approve`
  - 请求：`idempotency_key`
  - 只有当前项目设计师可批准。批准会在同一事务内创建或恢复成员权限。

## 申请人端

- `GET /api/v1/miniprogram/project-notes/member-invitations/:token`
  - 必须登录。只返回项目名称、邀请有效期和本账号申请状态；不返回项目 ID、笔记、版本或素材。
- `POST /api/v1/miniprogram/project-notes/member-invitations/:token/apply`
  - 请求：`idempotency_key`
  - 只创建待审核申请，不创建成员权限。

## 安全边界

- 邀请 token 使用服务端安全随机值，按 SHA-256 索引，不携带项目资料权限。
- 持有或转发邀请链接不会自动加入项目。
- 批准前重新校验申请账号仍是有效小程序账号。
- 撤销、过期、跨项目或非项目设计师审批均失败。
- 新版前端已移除手填账号入口。原 `POST /projects/:id/members/:account` 在旧体验版过渡期内保留，仍要求当前设计师的项目管理作用域并校验目标账号有效；成员移除和指定确认人语义保持不变。
