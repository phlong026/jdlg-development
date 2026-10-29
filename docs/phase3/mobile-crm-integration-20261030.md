# R01/W02 手机客户管理与 R02 项目绑定 CRM 最小契约

## 结论

当前已按第一阶段复用「响应式 CRM 页面 + 管理会话 + 服务端代理校验」，不是小程序 token 直连 CRM。小程序登录用户可以打开手机客户管理入口，但进入 H5 后仍须使用现有后台/企微登录；入口可见不代表获得 CRM 权限。

项目绑定 CRM 仍缺少一个关键条件：**小程序账号与已授权后台管理账号之间的显式、可审计映射**。在该映射存在前，继续拒绝客户端直传 `crm_id`，也不新增项目绑定能力。

## 已有能力盘点

### 1. 小程序身份

- 小程序使用签名 token 解析 `CodeSession.UserID`，业务路由再校验账号封禁状态。
- `user_identities` 已有已验证手机号与小程序 `user_id` 的映射，可用于联系方式回填或人工核对，不能用于授权。
- 小程序用户与后台管理用户在 `users.user_type` 上明确分离。

### 2. 设计师工作台授权

- 内容工作台通过 `tracking_access` 的 `tenant_id + owner_id + can_manage + enabled` 给小程序账号授权。
- 服务端额外限定 `users.user_type='miniprogram'` 且账号必须为 active。
- 这个 scope 仅表示某 owner 下的内容/项目管理能力，不会自动得到访客明细或 CRM 客户权限。
- 当前「我的→设计师工作台→客户管理」只承担打开既有后台登录入口，不表示小程序账号具备 CRM capability。

### 3. 后台 CRM 权限链

现有 CRM 权限为封闭链路：

1. 用户必须是 `user_type='management'`。
2. `management_access` 必须存在、`enabled=true`，且 scope 只能是 `platform_admin` 或 `crm_only`。
3. `management_crm_link` 必须是 `state='active' AND enabled=true`，用于获取受信 CRM subject。
4. 如果通过企微登录，`management_wecom_identity` 还会校验 corp、employee、version、在职状态和 8 小时会话。
5. CRM 代理只放行白名单路由；写操作还需满足 `Origin + X-CRM-Request + application/json`。
6. 代理请求使用服务端 HMAC 签名和超时，不向前端泄露桥接密钥。

### 4. 可直接复用的手机页面

- `background/src/pages/customerManagement.tsx` 已有客户列表、待办、提醒、主管工作台和客户详情抽屉。
- `customerResponsive.scss` 已覆盖 768px 和 380px 窄屏，包含横向标签、筛选、表单、分页和安全区适配。
- `ManagementSessionProvider` 每次先读 `/management/me`，再读 `/management/crm/context`；会话失效时统一清理并返回登录。
- `crm_only` 账号的前后端路由均是白名单，不会因手机入口得到平台管理权限。

### 5. 已有客户映射能力

- `customer_lead_crm_delivery` 已保存「小程序线索 ID → CRM customer_id」，用于后台线索直达客户档案。
- 分享访问治理已有「`u:<miniprogram_user_id>` 访客 → 经 CRM 桥接核验的 customer_id」关系，但该动作由已授权后台操作员执行，不是小程序 CRM 登录凭证。
- 现有 CRM 验证会重新读取目标客户，核对 customer ID、业务编号、未合并且未删除，可复用于项目关联的服务端校验。

## R01/W02：手机客户管理最小契约

### 第一阶段：复用响应式 H5，不改 CRM 授权模型

1. 小程序客户管理入口只打开已部署的 `/admin/customer-management`（小程序 `web-view` 或可信外部入口）。
2. 页面继续使用后台 cookie session、管理账号登录或既有企微 OAuth。
3. **不用小程序 token 换取管理会话，不在 URL 中携带 token，不传递 CRM subject。**
4. CRM 数据的读写仍全部通过 `ManagementAccessRequired + crmProxy`；小程序页面不新增 CRM 数据 API。
5. 小程序可以展示「手机客户管理」入口，但必须明示「需使用已授权的客户管理账号登录」；打开登录页本身不代表授权。

当前实现只允许小程序内部参数 `target=customer-management` 映射为固定地址 `https://api.jiadilingguang.com/admin/customer-management`。其他 target 均回退到既有后台首页；小程序 token、用户 ID 和任意外部 URL 都不会拼入 `web-view` 地址。

该阶段可以在不建立小程序—后台账号映射的情况下安全落地，但无法在小程序菜单层面准确判断某个小程序账号是否已授权。

### 第二阶段：需要显式账号映射后才可做能力显示

若要让小程序精确显示「当前小程序账号已具备 CRM 权限」或免除后台二次登录，需新增由平台管理员维护的显式映射，最小字段为：

```text
mini_user_id
management_user_id
enabled
version
approved_by
created_at / updated_at
```

能力查询只有在以下条件同时成立时才返回可用：

- 小程序账号 active；
- 显式映射 enabled；
- 后台账号仍为 management 用户；
- `management_access.enabled=true`；
- scope 仍为 `crm_only` 或 `platform_admin`；
- `management_crm_link` 仍为 active/enabled；
- 若使用企微登录，企微身份仍有效。

任一条件失效都应即时回收入口或要求重新登录。

## R02：项目绑定 CRM 最小契约

### 当前状态

- 项目笔记 JSON 模型已有 `crm_id` 可选字段，不需为单一 ID 新增 SQL 列。
- 当前小程序创建项目时，只要传入 `crm_id` 就返回 403。这是正确的 fail-closed 边界，应保留。
- 原因是项目 scope 只证明小程序用户能管理某 owner 的项目，并不包含受信 CRM subject 和 CRM 客户权限。

### 推荐命令契约

不在「创建项目」中盲信 `crm_id`，而是使用独立的显式命令：

```http
POST /api/v1/miniprogram/project-notes/projects/:project_id/crm-link
```

```json
{
  "customer_id": "opaque-crm-customer-id",
  "business_number": "C20261030001",
  "expected_revision": 7
}
```

服务端必须按以下顺序验证：

1. 用小程序 token 取 actor，校验账号 active。
2. 重新读项目，确认 actor 是项目设计师/有效成员，且 scope 对项目 owner 有 manage 权限。
3. 通过显式小程序—后台账号映射取得 management actor，不使用手机号、昵称或认证类型推导。
4. 重新校验该 management actor 的 `management_access`、`management_crm_link` 和项目 owner 对应的 `tracking_access.can_crm`。
5. 调用现有 CRM 桥接读取目标客户，核对 `customer_id + business_number`，并拒绝已合并、已删除或超出负责范围的客户。
6. 仅当 `expected_revision` 匹配时写入项目 JSON，同时记录审计事件。
7. 返回的项目中只包含稳定客户 ID、业务编号和绑定时间，不拷贝 CRM 私有档案或联系人全量数据。

解绑应使用单独命令并同样要求项目权限、CRM 权限、乐观锁和审计。

### 权限合并规则

项目绑定取两套权限的交集，不取并集：

```text
可绑定 = 小程序项目可管理
       AND 显式账号映射有效
       AND 后台 CRM 账号有效
       AND owner 范围有 can_crm
       AND CRM 服务确认目标客户可访问
```

小程序项目权限不能提升 CRM 权限，CRM 权限也不能越过项目成员关系。

## 明确不可复用为授权依据的字段

- 手机号相同；
- 微信 OpenID / UnionID 存在；
- 昵称、头像或设计师认证已通过；
- `tracking_access.can_manage=true`；
- 客户端传入的 `management_user_id`、`crm_username`、`customer_id` 或 `project_id`；
- 线索表中已有 `customer_id`（它只证明该线索的同步交付结果，不证明当前操作人有权管理该客户）。

## 实施前缺失条件

1. 小程序账号 ↔ 后台管理账号的显式映射和管理界面。
2. 映射变更审计、版本号、停用及回收逻辑。
3. 面向小程序的只读 capability 接口，不返回 CRM subject 或后台账号详情。
4. H5 `web-view` 业务域名、cookie/SameSite、企微 OAuth 在微信内置浏览器的真机验证。
5. 将现有 CRM 客户核验抽成可复用的服务端函数，避免项目绑定重新实现一套宽松校验。
6. 项目 CRM 绑定/解绑审计事件及竞态、越权、停用账号回归测试。

## 发布门禁

- 仓库只能确认已有 `web-view` 页面和已部署的 `https://api.jiadilingguang.com/admin/` 管理入口，不能确认微信公众平台控制台中的「业务域名」配置。发布前必须确认 `api.jiadilingguang.com` 已加入该小程序的业务域名白名单。
- 真机验证微信内置 WebView 中的后台 cookie、SameSite 策略和企微 OAuth 回跳；开发者工具或浏览器成功不能替代该验收。
- 内容工作台入口仍只以小程序登录作为菜单门禁，最终准入继续由现有 `workbenchContext` 的 `enabled + can_manage` 服务端结果决定；401、403 与网络失败继续显示现有明确状态，不新增授权推导。

## 最小验收清单

- 未登录小程序用户不能进入项目或 CRM 功能。
- 只有内容工作台权限的账号不能获得 CRM 数据。
- 打开手机 H5 后仍必须通过后台会话校验。
- `crm_only` 账号不能进入平台管理路由。
- 不存在显式账号映射时，项目绑定返回 403。
- 任意客户端 `crm_id` 不会写入项目。
- 超出 owner 范围、已合并、已删除或业务编号不匹配的客户不可绑定。
- 映射、后台权限、CRM 绑定或企微身份停用后，既有手机会话不能继续读写。
- 项目修订号竞态返回 409，不覆盖新状态。
