# 账号登录身份只读审计与隔离复现

2026-10-23；仅 `phlong026/jdlg-development`。工作树 `/tmp/jdlg-nickname-main`，分支 `fix/nickname-tracking-20261023`。执行时 HEAD `d39c8be142b127567521e765b8ad69824810592c`（此前 main 基线 `e67817e2dfe5f80d578cee8a7182ad46806f36b5`）；父任务其他跟踪修复同时存在于工作区。认证业务文件未由本审计改动。本审计没有 commit、push、部署、账号合并、生产配置更改、生产数据库访问或微信外呼。

## 可确认结论

所有登录方式最终已有共同的内部主键 `users.id`；问题在于不同凭据未绑定时分别查找/创建账号，并没有自动识别“现实中的同一个人”。已复现昵称保存在 A，而另一登录方式的合法 token 归属 B，因此 tracking 读 B 的空昵称，后台账号列表仍能看到 A 的昵称。这是明确失败条件，不是所示线上访客的已确认根因。

另复现两处独立缺陷：新手机号账号把缺失 openid 写成空串，第二个同类注册会撞 `uk_openid`；历史 32 位小写十六进制 openid 的设备迁移仍启用，新微信身份只要命中该设备索引就复用老账号，无需老账号 token。

保留设备指纹缓存的修复 `3993dc6` 符合现有会话签名协议。保留签名依赖没有新增或恢复上述迁移代码；旧设备迁移本来就仍存在，应该单独处理，不能靠清缓存或删除安全校验代替修复。

## 各入口实际行为

| 入口 | 凭据与查找 | 创建/绑定行为 | 最终主体 |
| --- | --- | --- | --- |
| 微信 `/miniprogram/login` | 服务端实际 Code2Session DTO 的 openid，随后非空 unionid | 未命中时先尝试历史设备迁移，再按 openid 创建 | `GetEffectiveUserByID` 后写 `code_session.UserID` |
| 短信 `/miniprogram/login/phone` | 路由实际 Redis 验证码验证；`user_identities(identity_type='phone',identity_key=phone)` | 未命中才 CreateByPhone、profile、BindPhone；不会按 profile.phone 或微信授权 contact 查找 | 同上 |
| 用户名/密码 `/miniprogram/login/password` | `users(username,user_type='miniprogram')` + VerifyPassword | 不创建；设置密码接口 UPDATE 当前 user_id 的 users 行 | 同上 |
| 小程序“企业登录”面板 | WXML `bindtap=handlePasswordLogin`，TS 调 `/login/password` | 就是同一小程序账号密码入口 | 同上 |
| 后台企业微信 OAuth | 真实企业成员身份 + `management_wecom_identity` 显式绑定 + enabled access | 仅管理账号；未预绑定拒绝，不注册小程序用户 | 管理后台 session `user_id`，不是小程序访客登录 |
| `/user/ai/wecom/verify-phone` | 已登录 token + 服务端微信手机号授权结果 | 只写 profile.enterprise_wechat_contact/verified，不建 phone 登录 identity | 当前账号 ID 仅用于该资料授权 |
| `/phone/bind` | 已登录 token + 实际短信验证码 | 无冲突才 phone->当前有效 user_id；其他有效账号占用则 HTTP409/40901 | 当前有效 ID；不自动合并 |

精确源码：

- 微信身份：`service/processor/wechat_auth.go:89`、`:99`、`:103`、`:117`、`:139`、`:155`、`:172`；Code2Session 字段 `service/function/wechat.go:188`。DTO 没有真实昵称/头像/手机号。
- 手机号：`service/route/miniprogram.go:295` 校验短信，`:309` 调策略；`service/processor/phone_auth.go:60` 查 identity，`:68` 创建，`:86` 更新 profile.phone，`:92` 建 identity，`:108` 解析有效 ID，`:120` 写 session。
- 真正绑定：`service/route/miniprogram.go:1393`、`:1418`、`:1427`、`:1435`、`:1442`、`:1451`。手机授权分离：同文件 `:3426`、`:3449`、`:3454`、`:3464`；`service/model/user_profile.go:268` 只 UPDATE 三个 enterprise_wechat 字段。
- 密码：`service/processor/password_auth.go:51`、`:57`、`:68`；`service/route/user_profile.go:601` 按当前 ID UPDATE username/password，`:613` 更新 has_password；没有新用户 INSERT。
- 小程序企业面板：`miniprogram/miniprogram/pages/login/login.wxml:183`、`:215`；`login.ts:616`、`:635`。后台企业微信：`service/route/wecom_identity.go:339` 强制 management，`:590` 查显式身份绑定和授权，`:602` 写管理 session。

## 约束与主体传递

- `users.id` 为主键，username+user_type 唯一、openid 唯一，unionid 当前没有唯一索引：`service/model/user.go:418`、`:433`、`:434`。
- phone identity 唯一键为 `(identity_type,identity_key)`：`service/model/user_identity.go:28`、`:39`；phone->ID 实际查询 `:45`。该表当前认证调用只使用 phone；微信身份仍在 users.openid/unionid 中。
- `GetEffectiveUserByID` 仅跟随数据库已经存在的 merged_source/merged_to_user_id 链，最多 5 层，不根据设备/手机号/昵称猜同人：`service/model/user.go:559`、`:569`、`:577`。
- token 解密得到 session_id，再按 session key 查 code_session，校验通过后从该 session 写 context user_id：`service/route/middleware.go:135`、`:146`、`:268`。不根据请求中的昵称或设备反查当前账号。
- 昵称保存：`service/route/user_profile.go:269`、`:296`；GET 完整资料同一个 `codeSession.UserID`：`:184`、`:213`。
- tracking HTTP `/entry`：`service/route/share_tracking.go:683`、`:690`、`:695`；账号是 `GetTokenUserID(c)`。`service/tracking/service.go:169` 将其变成 `u:<ID>`。resolver `service/route/share_tracking_profiles.go:10`、`:16` 只查这些已确认 ID；实际 SQL `service/model/user_profile_tracking.go:46`。`service/tracking/profile.go:134`、`:147` 按各 ID 套用昵称。
- 因此 A 的昵称不会因登录 B 而自动迁移到 B；这是正确的身份隔离，但产品缺少完整的显式多凭据绑定/冲突处理流程。
- 匿名到已登录关联仅限相同 client_instance、前一个 entry token、30 分钟内匿名 session；写入 server identity_link。账号 A 到账号 B 不合并：`service/tracking/service.go:177`、`:184`、`:191`、`:198`、`:206`。本次此处为源码交叉核对，完整旅程由父任务另测。

## 8 个隔离复现结果

所有 8 个断言通过。这里“通过”包括成功复现现有失败条件，不表示问题已被本审计修复。ID 仅为隔离库合成 fixture 自动编号。

| 场景 | 预期产品目标/信任边界 | 当前实际与证据 |
| --- | --- | --- |
| 微信 A -> 微信手机号授权 -> SMS | 手机授权不能被误当作已建立短信登录绑定 | 实际 HTTP 手机授权成功、profile.contact 有值，但 identity 查无行；SMS 新建 B。A=3 昵称“授权微信昵称”，B=4 空；真实签名 middleware -> actual Enter -> resolver/admin SQL 都保持 A/B 分离 |
| 微信 A -> 明确 `/phone/bind` -> SMS | 明确验证绑定后同一内部 ID | 实际 POST 绑定200，换设备 SMS 返回同一 ID=5，昵称一致 |
| 已有 SMS A -> 未绑定微信 B | 无额外证据不能自动认为同人；冲突不能转移身份 | SMS A=4，微信 B=6，即使 device 相同仍分开；A 有昵称/B 空。B 绑定 A 手机实际40901，identity 仍属于 A，再次SMS仍 A |
| 微信 A -> 设置用户名密码 -> 密码登录 | 密码是当前账号另一凭据，不应新建 | 实际 signed POST /profile/password200，/login/password200，同 ID=7，token->entry 同 ID |
| 微信同 openid/unionid跨设备 | 真实微信标识可保持同主体 | 三次实际策略/HTTP，两个openid经同一个fixture unionid指向 ID=8；设备不同 |
| 历史32hexopenid + 相同device | 设备不应独立证明旧账号归属 | 旧用户 ID=9，给另一合成微信身份同 device、不提供旧 token，仍回绑至 ID=9、读取旧昵称“历史账号资料” |
| 第二个独立SMS注册 | 多个无微信身份账号应可并存 | 实际 HTTP400，信息包含 uk_openid。CreateByPhone 未设置 OpenID；Create 将 Go 零值空串明确插入 UNIQUE openid，第二个空串冲突 |
| 现代微信账号同设备 | 相同设备不能合并不同微信身份 | 实际两个不同真实格式 fixture openid，ID不同；历史回退仅限旧32hex条件，不应泛化所有设备都会串号 |

技术范围：调用实际小程序登录 HTTP 路由、Redis SMS VerifyCode、真实 User/Profile/Identity/CodeSession 模型、真实 Go middleware/签名、真实 PUT 昵称与 POST 密码/phone bind/授权路由、真实 MySQL profile/后台列表/tracking resolver。tracking Enter 使用实际 Service + 临时内存记录库；本地 `/audit/entry` wrapper 直接复用生产 TokenAuthRequired -> GetTokenUserID -> Service.Enter 链，不伪称执行了生产 `/tracking/entry` 的全部外围限流/来源规则。微信 provider 是 Scoped DefaultTransport 合成响应，拦截所有外呼、未知 host/path 直接错误；未登录真实微信或获取任何真人昵称。短信验证码仅在 miniredis 预置，未发短信。

前置 fixture 排错：最早一次因测试 SM4 key 字符串可被 Base64 解为12字节而失败，改用明确16字节 Base64；随后第一轮已执行到 A/B 和昵称读取，但后台列表依赖 user_memberships 未初始化。只补齐该实际辅助表后重跑最终 8/8。两次是临时 fixture 配置问题，不列作业务失败。最新 run.log 为最终完整执行结果。

## 残留设备与清缓存

- `code_session:device:<value>` 索引仍写入：`service/model/code_session_redis.go:73`，历史回退读取 `:125`。前端 fingerprint 是系统参数/canvas组合的 hash，而非唯一人证据：`miniprogram/miniprogram/utils/deviceFingerprint.ts:31`、`:47`。
- 登录携带缓存值：`miniprogram/miniprogram/pages/login/login.ts:496`、`:525`、`:556`。现代匹配按真实 openid/unionid 优先；只有找不到时才走旧32hex回退，不能说“所有人按设备绑定”。
- pass 的验证用已定位 session 的 DeviceID：`service/route/middleware.go:248`；SDK `miniprogram/miniprogram/utils/tracking/api.ts:10`、`:15` 仅取缓存值签名，没有以它构造匿名ID或 analytics body。
- `3993dc6` 设置页只清 disposable cache：`miniprogram/miniprogram/pages/settings/settings.ts:607`、`:615`，保留合法 token 对应签名依赖，合理。
- 退出登录 `miniprogram/miniprogram/pages/my/my.ts:1058`、`:1075` 只删本地 token/userInfo，未请求服务端撤销；不会因此更改 users/profile 归属。未额外复现服务端会话撤销；不把此项当已确认昵称缺陷。

## 最小方案，需父任务确认再实施

1. **缺失微信身份持久化**：在 `UserModel.Create` 统一将空 openid/unionid 以 SQL NULL 保存（如参数处 `NULLIF(?, '')` 或传 nil），保留 uk_openid；无需给手机账号生成伪微信ID，更不能生成32hex伪openid再次触发旧迁移。不需要本次清洗生产数据。回归至少：两个不同手机可注册、各自再次登录ID不变、密码账号无openid可并存、微信真openid唯一性不变。
2. **多登录凭据统一**：沿用 users.id 和已有 phone identity，不另造总账号系统。当前已登录用户显式添加并验证尚未绑定的凭据，统一绑定到当前 ID。补齐“已登录短信账号添加微信身份”的服务端验证入口，验证新鲜 wx code 真实返回值与当前登录权；微信账号添加手机可复用 `/phone/bind`。任何凭据已归属另一个有效账号时保持冲突，进入单独双边认证/明确选择的账号整理流程，不自动合并或搬昵称、资产、tracking历史。下载用途的手机号授权不能未经明确用途确认被偷偷升级为登录绑定。
3. **退役旧设备迁移**：单独安全审阅/授权，将无需旧账号凭据的设备回绑下线，或只允许真实旧账号认证加新微信授权的显式迁移。保留 session signing 值；不要以移除 pass 验证代替迁移整改。历史账号如何找回及迁移需业务规则确认，本审计不改。
4. **绑定原子性（源码发现，未在本轮模拟并发）**：`UserIdentityModel.BindPhone:75` ON DUPLICATE KEY UPDATE 可更换 user_id；HTTP:1435 的先查后写不与其同一事务。当前顺序冲突保护实测正常，不能称为已证实并发串号；未来统一绑定时应让持久化层在冲突时拒绝改归属，同用户幂等，避免并发绕过外层检查。

## 复核与附件

- `/tmp/jdlg-account-audit/account_identity_audit_test.go`：只读 overlay 测试源。
- `/tmp/jdlg-account-audit/overlay.json`：映射到 route package 虚拟测试文件，不实际写入业务仓库。
- `/tmp/jdlg-account-audit/run.log` 与 `run.redacted.log`：最终 8/8；内容只有测试合成 ID/昵称标签，不含真实身份、token、手机号、密钥或生产地址。
- 本报告 `REPORT.md`。

复跑（会重置精确指定的隔离库 `jd_account_identity_test` 内五张测试表，不操作其他库）：

```sh
cd /tmp/jdlg-nickname-main/service
ACCOUNT_AUDIT_DSN='root@unix(/tmp/jdlg-fix-mysql/mysql.sock)/jd_account_identity_test?parseTime=true' \
GOPATH=/tmp/jdlg-nickname-diagnostics/gopath \
GOCACHE=/tmp/jdlg-nickname-diagnostics/gocache \
/tmp/jdlg-nickname-diagnostics/go/bin/go test \
  -overlay /tmp/jdlg-account-audit/overlay.json ./route \
  -run '^TestAccountIdentityAudit$' -count=1 -v
```

数据库仅 Docker `jdlg-fix-mysql`（父任务已创建、`--network none`）的本地 UNIX socket；测试只接受上述精确 DSN。其余存储为测试进程 miniredis/内存。测试不会回退环境数据库配置。若父任务希望把证据随修复保存在仓库，可在审计附件目录保存去敏报告/日志和 overlay 源，改用明确说明的独立测试命令；本审计目前未写仓库 docs 或提交测试。

无权限/自动审核拒绝、无依赖下载阻塞。剩余边界：真实微信原生昵称与手机号授权事件未验收，指定线上访客是否因登录方式分叉/旧设备回绑需获准后按 visitor_key 精确核验，不能凭截图推定；本审计未访问生产。
