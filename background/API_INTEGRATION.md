# 前后端接口集成说明

更新：2026-09-16。本文描述统一后台（background/）与 Go 服务（service/）的实际集成方式；客户管理模块的接口细节以《实施记录》《客户快速报备与公海》与源码为准。

## 请求层

| 请求层 | 文件 | 使用方 | 特点 |
| --- | --- | --- | --- |
| 通用请求层 | `src/utils/request.ts`（`get/post/put/patch/del`） | 除客户管理外的全部页面 | 注入 `Authorization`（仅当存在真实 token；会话标记 `session_active` 不发送）、`credentials:'include'`、统一解析 `{code,msg,data}`（0/200 为成功）、401 时清会话并跳转 `/admin/login` |
| CRM 请求层 | `src/api/wecomCustomers.ts`（`crm()`） | 客户管理全部页面 | 仅凭 session cookie、自动携带防 CSRF 头 `X-CRM-Request: 1`、401 时派发 `management-session-expired` 事件由 `ManagementSessionProvider` 处理 |

两层的认证都以 Go 会话 cookie 为准；`utils/token.ts` 里的 `session_active` 只是"会话存在"的本地标记，不是凭据。

## 会话与路由

- 登录：`POST /api/v1/management/login`；企业微信登录：`GET /api/v1/management/wecom/config|start|callback`。
- 会话核验：`GET /api/v1/management/me`（`api/auth.ts` 的 `getMe`，带 10 秒超时）；CRM 上下文 `GET /api/v1/management/crm/context` 在 `/me` 成功后异步加载，不阻塞原后台页面。
- 路由守卫：`component/ProtectedRoute.tsx`；`crm_only` 账号只能停留在 `/customer-management`。

## 端点清单

全部端点的唯一权威定义在 `src/config/api.ts`（通用模块）与 `src/api/wecomCustomers.ts`、`src/api/customerLeads.ts`（客户模块）。概要：

| 前缀 | 用途 | 后端 |
| --- | --- | --- |
| `/api/v1/management/users|orders|stats|...` | 原平台模块（订单、会员、AI 等） | Go 直连数据库 |
| `/api/v1/management/customer-leads/*` | 小程序咨询线索工作台（CRM 启用后仅只读） | Go `route/customer_service.go` |
| `/api/v1/management/crm/*` | 客户管理（驾驶舱、客户档案、公海、待办、附件、审批、同步、导出、账号权限、来源接入） | Go 网关按白名单代理到 Java `/internal/management/v1/*`（HMAC 签名） |

CRM 白名单路由的完整定义见 `service/route/crm_bridge.go` 的 `crmRoute`；每用户导出限流 15 秒一次。

## 环境变量

- `VITE_API_BASE_URL`：生产为空（同源相对路径）。生产构建不要指向演示站点。
- `VITE_DEV_PROXY_TARGET`：开发代理目标，默认 `http://localhost:8080`；指向真实环境必须显式配置。

## 历史

早期版本的本文档描述的 gifts/warehouse/cashier 等页面与端点已不存在，2026-09-16 起按上述内容重写。
