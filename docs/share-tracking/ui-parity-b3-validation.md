# 图稿补齐第三批 T08：手机会话与权限验证

## 验证边界

- 目标是同一 React Web 管理后台在电脑与手机浏览器中复用现有会话和授权；没有新增小程序原生管理页、WebView 或 SSO。
- 本文记录云隔离环境中的源码审计及合成 API 回归。没有提供获授权的测试部署、真实测试账号或手机设备会话，因此真实手机登录与当前数据库权限联调仍待验。
- 未连接或修改生产数据；未执行迁移；未创建账号或权限；未读取或提交真实密码。所有测试用户名、密码字符串、归属、访客及返回数据均为测试夹具。
- 不把模拟 fetch、可控 React hooks、页面宽度变量或静态源码断言称为真实浏览器/手机验收，也不生成假截图。

## 实际接线审计

1. 登录接口：`POST /api/v1/management/login`，后台既有密码策略校验管理账号；服务端 `SetUserSession` 保存 Redis 会话。前端以 `credentials: include` 携带 cookie。
2. 当前身份：`GET /api/v1/management/me`，前端 `cache: no-store`，10 秒超时。可信身份字段为响应 `data.id`；本地 `admin_token=session_active` 仅是兼容标记，不能认证，不以该标记构造 Bearer 凭据。
3. 当前管理授权：`ManagementAccessRequired` 每次 `/me` 读取 `management_access` 和真实 management 用户类型，停用/未授权为 403；服务不可用为 503。
4. 当前跟踪授权：每个 `/management/tracking/*` 请求通过 `AuthRequired`，再从当前 `management_access`、`tracking_access` 读取租户、归属及 view/manage/CRM/delete/rules。平台管理员身份不自动授予数据权限；客户端 owner 只是申请筛选，服务端仍须核验。多归属能力采用交集。
5. `tracking/context` 只给能力与 owner 列表，没有账号 ID；前端身份隔离必须使用经 `/me` 核验的管理账号 ID。
6. 401 通过共享 `expireSession` 清兼容标记并通知 `ManagementSessionProvider`；受保护路由软跳登录。Provider 的代次防护阻止失效后的旧 `/me` 成功响应恢复旧会话。
7. Cookie 显式 `Secure` / `HttpOnly` / `SameSite=Lax` / 8 小时设置目前位于 `CRM_BRIDGE_URL` 非空的初始化分支。目标部署的 Cookie、HTTPS、反向代理、跨站/CORS 实际行为需单独核验；本批不擅自修改已有部署策略。

关键源码：`background/src/api/auth.ts`、`utils/request.ts`、`utils/session.ts`、`component/ManagementSession.tsx`、`component/ProtectedRoute.tsx`；`service/route/management.go`、`middleware.go`、`crm_bridge.go`、`share_tracking.go`、`service/component/session.go`。

## 本批修复与隔离验证结论

- 跟踪 context 原先仅在挂载时读取。现增加 `useTrackingAccess`：当前代次的非 context 跟踪接口返回 403 时，立即清旧 context 并重验，列表/详情/治理子树以账号及权限代次为 key 重挂。即使仍能查看但 CRM 等单项能力撤回，也不会沿用旧详情/治理界面。权限更新以实际请求和重新核验为触发，不声明无请求时实时推送撤权。
- 通用 request 对 HTTP 失败保留 numeric `status`，既有错误文案、401 清会话行为及 `requestPage` 不变。跟踪 API 的普通请求与上传均捕获开始时的权限代次；已取消、旧账号或旧权限代次的晚到 403 不得清理当前页面。context 自身 403 直接成为可重试错误，不广播形成循环。每个账号最多自动核验一次；如重读 context 后业务端仍 403，清数据并停在“重新检查权限”的手动入口，防止自动挂载/403循环。手动核验或切换账号重置该预算。
- `tracking-view-v1` 原先在同一浏览器标签内被不同管理账号复用。现用 `tracking-view-v2:<actorId>`，只接受可信 `/me` 的正整数 ID；不迁移旧共享状态，不保存无身份筛选。同账号返回保留合法筛选，权限重验后丢弃不再授权的 owner，不把别的账号搜索条件带入新会话。

- 跟踪请求的旧账号/已取消 401 不再清除新会话：请求层可选内部 `shouldExpireSession` predicate，API 按发起代次决定是否处理失效；默认调用仍沿用原 401 行为。上传同样校验代次，仍抛原错误供请求方处理。
- context 响应缺少必要字段、权限类型无效、owner ID 空白/超长/带控制字符/重复、或有 view/manage 却无 owner，均显示可重试响应错误；不能解释成“已确认无权”，也不擅自补充写入能力。

以上是源码与隔离回归完成，真实手机、Cookie 签发、Redis 会话及当前数据库授权联调仍未执行。无后端权限结构或会话部署策略修改。跟踪管理组增加认证之前即可生效的 `Cache-Control: private, no-store`，避免受保护响应被缓存；其路由测试见本批汇总记录。

## 可执行隔离会话回归

运行：

```sh
cd background
node --test scripts/share-tracking-mobile-session.test.mjs
```

2026-10-12 终态专项结果：36 pass，0 fail，0 skip。执行实际 TypeScript/TSX 模块，mock 边界仅为 fetch、浏览器存储、React 调度及路由元素；不调用真实后端。

覆盖：

- 登录请求契约、失败不产生已登录标记、登录响应范围必须被新 `/me` 结果覆盖。
- 仅有本地标记不能认证；`/me` 401、403、503 关闭受保护页面；失败后的显式重试。
- 跟踪读取及上传 401 走相同失效入口；晚到 `/me` 不能恢复失效状态。
- 403 不误作整站会话过期，不静默重发；未授权 context 不挂载记录列表。
- 相同合成账号在 1440/390/320 宽度变量下请求相同身份、归属与能力；这只验证不按屏宽分叉的接线合同。
- crm_only 只允许现有客户管理和分享访问记录页面；退出失败也清本地身份；重复“返回目标路由”仍由 guard 拦截。
- owner 和标识正确 URL 编码，所有跟踪读取携带会话 cookie，不发送伪 Bearer 标记；context/list/detail/upload 请求显式 `cache: no-store`；卸载后的旧请求不能写回状态。
- 当前 403 清理旧能力/页面，CRM 单能力撤回也换代；重复并发 403 只触发一次重验；旧账号、旧权限代次、已取消请求及旧上传的晚到 403 不干扰新账号。重复拒绝会停下自动重验，手动操作可恢复。
- 主页面采用可信账号 ID 和新权限 revision 重挂记录子树；存储恢复与真正 TrackingRecords 请求验证同账号返回、owner 撤权、跨账号搜索隔离。
- 非法 actor、损坏存储、错误 context shape、StrictMode 重挂及 A→B→A 晚到响应均覆盖。

重复导航的 guard 合同不能替代真实浏览器 Back/Forward、BFCache 或手机后台唤醒测试。第一、二批详情/分页/CRM 目标测试可复用，但不替代这里的真实授权验收。

另已运行 `npm run typecheck` 通过；本批最终全量门禁的汇总结果以最终固定记录为准。后端没有本批源修改，另从既有测试选取 9 个顶层授权/范围用例运行 `go test -p 1 -count=1 ./route ./tracking -run <下列精确名称正则>`，两包通过：

- `TestTrackingCRMTargetRouteOwnerAndCurrentPermission`
- `TestTrackingGovernanceMutationOwnerScope`
- `TestTrackingPrivateScopePreservesOriginalPermissions`
- `TestDetailPaginationBindsEndpointTargetAndCurrentScope`
- `TestScopedEmptyOwnerNeverFallsBackToTenant`
- `TestVisitorCRMTargetRejectsAnonymousUnprivilegedAndRevoked`
- `TestVisitorContentsRevocationExpirationAndPermissions`
- `TestTrackingMutationCSRF`
- `TestCRMRouteAndManagementScopeAllowlist`

这些后端用例使用隔离路由/存储 fixture，仍不是目标部署的数据库权限联调。


## 真实验收所需条件

应先指定非生产测试站点和本次验收允许使用的现有账号、归属、数据集；测试密码通过安全登录方式提供，不进入本文或仓库。若需创建账号、调权限或造数据，须有相应环境和操作授权。

建议最少准备：

- 一个已启用管理账号，有明确 owner A 的 tracking view 权限。
- 一个已启用但无 tracking 权限的管理账号。
- owner A 和 owner B 的隔离合成访问数据，包含有/无 CRM 映射情况。
- 可由获授权管理员在测试库撤销/恢复 owner A 或 tracking view 的操作窗口。
- 测试站点、API、Redis/MySQL 与前端提交一致；HTTPS、cookie、CORS 和代理配置已确认。前端本地 fixture 不满足这一条件。

## 待验矩阵（全部尚未真实执行）

| 场景 | 操作与应见结果 | 必要证据 |
| --- | --- | --- |
| 手机首次登录 | 在手机浏览器打开 `/admin/share-tracking`；无会话先去登录；合法登录再取得当前 `/me` 和 tracking context | 设备/浏览器版本、部署 commit、脱敏请求状态和页面截图 |
| 登录中断与重试 | 错误密码、网络中断、重复提交；恢复网络后正常登录 | 无重复副作用、错误可恢复、未提前显示数据 |
| 同账号电脑/手机 | 分别登录同一测试账号，选择相同 owner/时间/身份/快照 | 两端权限和相同筛选指标一致；会话可独立存在 |
| 过期 401 | 测试会话确实失效后读取列表/详情；返回登录并重新登录 | 401、旧列表/详情清理、重登恢复，无过期凭据继续读取 |
| 管理账号停用 | 当前 `/me` 变为 403 | 不显示受保护页面，明确说明和重试入口 |
| 无跟踪授权 | 管理登录有效但 context 无 view | 明确无权状态，无列表/详情读取 |
| tracking 撤权 | 已打开详情时撤销 view/owner；触发下一次读取 | 后端 403，前端立即清理旧显示并重验范围，不继续旧治理操作 |
| 跨 owner | A 账号尝试 B 的列表、素材、访客、轨迹、CRM 目标 | 每个端点拒绝；不能靠修改 query/cursor 访问 B |
| 双视角与返回 | 两视角、展开轨迹、关闭详情、CRM 跳转后返回、Back/Forward | 筛选归属合法、分页快照一致、无旧身份/旧权限内容复现 |
| 换账号 | 同标签退出 A 再登录 B，含预存筛选 | 使用 B 的已验证 ID；不继承 A 搜索/owner，不取旧详情 |
| 后台恢复 | 手机切后台、锁屏再返回，含已过期/已撤权情况 | 下一次请求重新鉴权，观察 BFCache/已缓存页面，不以截图代替服务端证据 |

验收结束前逐项补充通过、失败或未执行及真实证据。不能因隔离回归通过而把此表改为整体通过。

## 最终整批回归与缓存约束

- 全后台626/626通过，包含本批36项会话/换号/撤权/上传回归；TypeScript、32条路由一致性与生产构建通过。
- 全Go719 pass、5 skip、0 fail；跳过项仍是待真实依赖环境验证，不能记为通过。
- 跟踪管理组在鉴权前设置 `Cache-Control: private, no-store`，包括401失败响应；客户端跟踪读取和上传显式 `cache: no-store`，不复用授权响应缓存。新路由合同使用隔离cookie session，不连接真实数据库。
- 既有小程序跟踪196/196、TS/JS生成一致性及58条路由策略通过；旧全量客服2失败仍保留。
- 独立复核复现的“持续403无限重验”和“旧账号迟到401清除新账号会话”已修复并由独立探针验证。一次自动重验仍失败后停止请求，显示手动重新检查权限；当前账号有效401仍正常失效。

此记录仍不是手机浏览器真实登录成功证明。真实站点、账号、Cookie属性、HTTPS/代理、CRM桥接及设备Back/Forward/BFCache行为都需要上述授权测试环境。

后端终态 `go test -race -p 1 -count=1 ./tracking ./model ./route` 与 `go vet -p 1 ./tracking ./model ./route` 亦全部通过。新增缓存头测试初次遗漏gin session测试中间件导致fixture panic，补上隔离cookie session后原命令与全量门禁通过；未改认证生产行为来迁就测试。
