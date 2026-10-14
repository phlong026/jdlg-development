# 分享访问系统：联调环境准备清单

日期：2026-10-13（Asia/Shanghai）。基线：`28520dc`（PR #5 合并后）。

本清单只说明联调前**必须准备好什么、谁来准备、怎样算准备完成**。逐项验收步骤仍以 [17 项验收清单](acceptance-checklist.md) 为准，上线门槛以 [发布与验证记录](rollout-and-verification.md) 为准。联调环境必须与生产隔离：独立数据库、独立 Redis、独立媒体目录、测试小程序账号；不得连接生产库，也不得使用真实客户资料。

状态列在准备过程中填写：`待办` / `进行中` / `就绪` / `阻塞（原因）`。

## 0. 角色与责任

| 角色 | 负责内容 | 姓名 |
| --- | --- | --- |
| 环境负责人 | 服务器、MySQL、Redis、反向代理、HTTPS、媒体目录、备份 | |
| 业务负责人 | 组织/归属映射、测试账号授权矩阵、告知文案、37 个排除页面确认 | |
| CRM 负责人 | CRM 测试实例、桥接密钥、测试客户、CRM 账号绑定 | |
| 小程序负责人 | 体验版/开发版发布、测试微信号、基础库版本、真机 | |
| 测试执行人 | 按 AT 清单执行并记录证据 | |

## 1. 基础设施

| # | 项目 | 要求 | 就绪判据 | 状态 |
| --- | --- | --- | --- | --- |
| 1.1 | 隔离 MySQL 8 | 独立库，字符集 utf8mb4，支持 JSON 和 `DATETIME(6)`；连接用户不得拥有生产库权限 | `SELECT VERSION()` ≥ 8.0；联调库与生产库主机或实例不同 | |
| 1.2 | 业务基线数据 | 用现有迁移建好 users、management_access、management_crm_link、user_favorites、customer_leads 等原表；用 `cmd/localseed` 或脱敏夹具写入测试账号 | 后台可用测试管理账号登录；小程序可用测试账号登录 | |
| 1.3 | 隔离 Redis | 管理端会话（`SetUserSession`）和小程序 code session 使用 | 后台登录后 Redis 中有对应会话键；重启服务后会话仍在 | |
| 1.4 | HTTPS 与反向代理 | 测试域名带有效证书；代理转发 `Host`；记录代理出口网段 | 浏览器访问无证书告警；后端日志中 `RemoteAddr` 为代理地址 | |
| 1.5 | 媒体目录 | `TRACKING_MEDIA_DIR` 为绝对路径，不在任何静态目录下，权限 0700，能持久保存并纳入备份 | 上传一张图片后文件出现在目录中；无法通过静态 URL 直接下载 | |
| 1.6 | 数据库连接池 | 当前全局 `SetMaxOpenConns(20)`，与主业务共用 | 记录联调期间的连接数峰值（见第 8 节负载风险） | |
| 1.7 | 浏览器执行环境 | 能运行 Chromium/Playwright 且允许监听本地端口（当前云环境被 socket 权限拒绝） | `python background/e2e/share_tracking.py` 能产出截图 | |

## 2. 数据库迁移

按顺序在**隔离库**执行。执行前先备份，并记录执行人、时间和结果。

| # | 脚本 | 说明 | 就绪判据 | 状态 |
| --- | --- | --- | --- | --- |
| 2.1 | `service/sql/management_access_seed_template.sql` | 前置条件：管理端访问授权已存在；按模板逐个登记测试管理员 | 测试管理员登录后能打开总控台 | |
| 2.2 | `service/sql/share_tracking_up.sql` | 新建 `tracking_tenant_lock`、`tracking_access`、`tracking_records`，只增表，不授权 | `SHOW CREATE TABLE tracking_records` 含 7 个二级索引 | |
| 2.3 | `service/sql/share_tracking_bounded_queries_up.sql` | 对已存在的旧 `tracking_records` 补索引，可重复执行 | 重复执行无报错；索引存在 | |
| 2.4 | 回滚演练 | 关闭 `TRACKING_ENABLED`，确认原分享和原页面不受影响；**不删表** | 关闭开关后小程序原分享、原详情页正常 | |
| 2.5 | `EXPLAIN` 采样 | 对访客列表、素材列表、会话轨迹三类查询抓取实际 SQL 并 `EXPLAIN` | 记录是否命中 `tracking_scope_time` / `tracking_session_time` 等索引 | |

## 3. 服务端配置（环境变量）

所有采集开关默认关闭。联调时**逐项打开**，每打开一项先验证再开下一项。

| # | 变量 | 联调取值 | 说明 | 状态 |
| --- | --- | --- | --- | --- |
| 3.1 | `MYSQL_DSN` | 隔离库 DSN | 不得指向生产库 | |
| 3.2 | `TRACKING_TENANT` | 例如 `jdlg-staging` | 为空时采集强制关闭 | |
| 3.3 | `TRACKING_DEFAULT_OWNER` | 业务确认的默认归属 ID | 为空时采集强制关闭 | |
| 3.4 | `TRACKING_CONSENT_VERSION` | 例如 `staging-2026-10` | 必须与小程序告知版本一致 | |
| 3.5 | `TRACKING_PRIVACY_NOTICE` | 业务确认的联调告知文案 | 为空时采集强制关闭 | |
| 3.6 | `TRACKING_ENABLED` | 先 `false`，基础验证后 `true` | 总开关 | |
| 3.7 | `TRACKING_CONTENT_ENABLED` | AT-08 前打开 | 自有素材阅读 | |
| 3.8 | `TRACKING_VIDEO_ENABLED` | AT-11 前打开 | 视频计量 | |
| 3.9 | `TRACKING_INTENT_ENABLED` | AT-15 前打开 | 跟进参考 | |
| 3.10 | `TRACKING_MEDIA_DIR` | 见 1.5 | 未配置时上传返回不可用 | |
| 3.11 | `TRACKING_TRUSTED_PROXY_CIDRS` | 1.4 记录的代理网段 | 不配置时，代理后所有客户端共用同一个限流额度。采集开启但未配置或配置无效时，启动日志有 `[Tracking] TRACKING_TRUSTED_PROXY_CIDRS ...` 告警；运行中收到不可信地址发来的 `X-Forwarded-For` 时，会记录一次 `[Tracking] 收到来自 ... 的 X-Forwarded-For`。**就绪判据：联调期间的服务日志里两条告警都没有出现** | |
| 3.11a | `TRACKING_MAX_WRITE_TX` / `TRACKING_MAX_READ_TX` | 默认 4 / 6（取值 1–64） | 跟踪最多同时占用的数据库连接数，与主业务共用 20 连接池；超出时在进程内排队。无效值会打印告警并使用默认值 | |
| 3.12 | `TRACKING_WECHAT_CODE_ENABLED` | 默认 `false`；核实小程序码权限后再打开 | 依赖已有 Wechat AppID/AppSecret 配置 | |
| 3.13 | `TRACKING_WECHAT_ENV` | `trial` 或 `develop` | 联调不得用 `release` | |
| 3.14 | `CRM_BRIDGE_URL` / `CRM_BRIDGE_KEY`（≥32 字节）/ `CRM_BRIDGE_AUDIENCE` / `CRM_BRIDGE_KEY_ID` | CRM 测试实例 | 指向测试 CRM；密钥不进仓库、不进聊天 | |
| 3.15 | `ADMIN_ALLOWED_ORIGINS` / `CRM_ALLOWED_ORIGINS` | 联调后台域名 | 后台与 API 跨域部署时，跟踪写操作的 Origin 校验依赖它；同域部署可不填 | |
| 3.16 | `WECOM_LOGIN_*` | 如需验证企微登录则配置测试应用 | 未配置时企微会话的跟踪请求返回 401 | |
| 3.17 | Cookie 策略 | 确认 `Secure`/`HttpOnly`/`SameSite=Lax` 实际生效 | 当前显式设置只在 `CRM_BRIDGE_URL` 非空分支中生效，需在浏览器里核对 | |

启动后检查：`GET /api/v1/miniprogram/tracking/config` 返回的 `enabled`、`consent_version` 与配置一致。

## 4. 权限与测试账号矩阵

`tracking_access` 默认不给任何人授权，平台管理员也不例外。每行都要经业务负责人确认，执行后用该账号实际登录验证。

| # | 测试账号 | 类型 | management_access | tracking_access（owner：view/manage/crm/delete/rules） | 用于 | 状态 |
| --- | --- | --- | --- | --- | --- | --- |
| 4.1 | 管理员 A | 后台 | platform_admin | owner-1：全部为 1 | 全流程 | |
| 4.2 | 只读销售 B | 后台 | crm_only | owner-1：仅 view | AT-12 负向 | |
| 4.3 | 编辑 C | 后台 | platform_admin | owner-1：仅 manage | 素材发布、AT-12 | |
| 4.4 | 跨组织 D | 后台 | platform_admin | owner-2：view+manage | 跨归属隔离 | |
| 4.5 | 无跟踪权限 E | 后台 | platform_admin | 无记录 | 确认平台管理员不会自动获得权限 | |
| 4.6 | 小程序作者 M | 小程序 | — | owner-1：enabled+manage | “我的 → 可分享素材” | |
| 4.7 | 访客 A/B/C | 小程序 | — | — | A→B→C 传播链 | |
| 4.8 | 同账号双端 | 小程序 | — | — | AT-03 | |

注意：
- 后台账号和小程序账号是两套体系，4.6 必须给**小程序账号**单独配置 `tracking_access`。
- 一个账号有多个归属时，各项能力取交集。
- CRM 映射（4.1）还需要在 `management_crm_link` 中有 `state='active'` 的 CRM 用户名。

## 5. 外部系统

| # | 项目 | 要求 | 就绪判据 | 状态 |
| --- | --- | --- | --- | --- |
| 5.1 | CRM 测试实例 | 至少 2 个测试客户（含客户编号），其中 1 个已合并、1 个已删除 | 后台用 4.1 账号能打开 CRM 客户详情 | |
| 5.2 | CRM 签名桥 | 测试 CRM 能验证 `X-CRM-*` 签名 | 用 `/identity-links` 成功映射一次，审计中有 `crm_link` | |
| 5.3 | 微信小程序 | 体验版已上传当前代码，合法域名已配置联调 API 域名 | 真机打开体验版，`/miniprogram/tracking/config` 请求成功 | |
| 5.4 | 小程序码（可选） | 已核实 `getwxacodeunlimit` 权限 | 打开 3.12 后能生成码，扫码进入阅读页 | |
| 5.5 | 企微（可选） | 测试企业和应用 | 企微登录后能访问跟踪菜单 | |

## 6. 设备与测试素材

| # | 项目 | 要求 | 状态 |
| --- | --- | --- | --- |
| 6.1 | iOS 真机 | 记录系统版本、微信版本、基础库版本 | |
| 6.2 | Android 真机 | 同上 | |
| 6.3 | 手机时间 | 自动对时；另准备 1 台故意偏移 3 分钟的设备，验证时钟偏差（服务端只容忍 ±2 分钟）。偏移设备访问后，`GET /api/v1/management/tracking/diagnostics/rejections` 中的 `clock_ahead` 或 `clock_behind` 应该增加 | |
| 6.4 | 桌面浏览器 | Chrome，覆盖 320/375/390/768/1024/1440 六个宽度 | |
| 6.4a | 大访客删除演练 | 在测试环境给一个测试访客造出超过 1 万条事件，发起删除：先确认单次请求可能返回 `in_progress`，此时该访客在列表中已消失、该账号无法新增访问；重复同一请求直到返回 `completed`；再核对该访客的事件、汇总、会话、身份关联均为 0 | |
| 6.5 | 授权测试素材 | 9:16 和 16:9 图片各 1 张、超长标题、MP4 视频 1 个（≤50MB），全部为自有或已授权 | |
| 6.6 | 录屏与对时 | 录屏能显示系统时间，用于和服务端计时对账 | |

## 7. 证据归档

| # | 项目 | 要求 | 状态 |
| --- | --- | --- | --- |
| 7.1 | 证据目录 | 按 `AT-xx/日期/执行人` 存放录屏、截图、SQL 对账结果 | |
| 7.2 | 提交号 | 每份证据都标注服务端、后台、小程序对应的提交号 | |
| 7.3 | 脱敏 | 证据中不出现真实手机号、真实昵称或密钥 | |
| 7.4 | 集成测试 | 配置 `CRM_IDENTITY_TEST_DSN`、`WECOM_EVENT_INBOX_TEST_DSN`、`USER_BAN_TEST_DSN` 指向隔离库，补跑当前跳过的 5 条 Go 集成测试 | |

## 8. 联调前必须知晓的已知风险

以下问题来自 2026-10-13 的代码评审（见 [评审记录](code-review-20261013.md)），会直接影响联调结果的解读：

1. **写入仍串行**：查询、公开阅读、媒体和分享解析已经改为无锁快照读，CRM 校验也移出了事务；但事件上报、进入、分享、收藏/咨询回执、注释和删除仍然串行通过同一把租户锁。高并发上报时的排队属于已知限制，请记录出现时的并发量。
2. **查询预算**：列表已改读日汇总，一次访问约占 2 条预算，单个时间窗口约可容纳 1 万次访问；超过时仍返回 422，请记录出现时的数据量。会话和事件下钻仍读原始事件，单个访客的超长历史也可能触发 422。
3. **保留期清理没有可执行入口**：`PurgeExpired` 目前只有测试调用，联调中不需要也无法验证自动清理。
4. **时钟偏差**：设备时间与服务器相差超过 2 分钟时，事件会被全部拒绝，见 6.3。现在被拒数量可通过 `diagnostics/rejections` 查看，但事件本身仍然丢失；时间校正属于后续改动。

## 9. 开始联调的放行条件

以下全部满足后，才开始执行 AT 清单：

- [ ] 第 1–3 节全部为“就绪”
- [ ] 第 4 节账号矩阵已经业务负责人书面确认，每个账号都实际登录验证过
- [ ] CRM 至少完成一次成功映射和一次拒绝映射（5.2）
- [ ] 小程序体验版能完成一次“进入 → 可见 30 秒 → 退出”，后台能看到这条访问
- [ ] 回滚演练（2.4）完成
- [ ] 证据目录和记录模板已建好
