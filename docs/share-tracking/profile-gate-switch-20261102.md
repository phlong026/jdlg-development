# 小程序资料门禁临时放开开关（生产止血记录）

## 背景

2026-11-01 强制门禁上线后，正式版小程序（旧版客户端）整体不可用，体验版（7.2.18+ 配套客户端）正常：

- 服务端 `profile_access_gate.go` 对所有 `/api/v1/miniprogram/*` 业务接口默认强制登录与完整资料，未登录 401、资料不全 428，无开关。
- 旧版正式版客户端部分业务请求不携带签名（401），且没有 428 资料补全流程；存量用户几乎全部缺头像（245 用户头像填写率约 0%）。
- 配套客户端 7.2.21 已上传但未正式发版，形成"正式版不可用、体验版可用"的窗口。

本记录与 `profile-access-enforcement-20261101.md` 的门禁整改配套阅读；整改本身不回退，仅增加可临时放开的开关。

## 变更内容

提交 `d80b582`（基于生产运行版本 `b28e6a1`）：

- 新增 `MINIPROGRAM_PROFILE_GATE` 环境变量：设为 `off`（或 `0`/`false`，大小写不敏感）时 `miniprogramProfileGateMiddlewares` 返回空列表，不挂载认证与资料完整性中间件，小程序业务路由回到门禁启用前的既有行为；未设置或任何其他值均保持门禁开启。
- 挂载点从 `v1.Use(...)` 固定两个中间件改为按开关结果循环挂载（`service/route/index.go`），默认行为与变更前完全一致。

## 验证

- 门禁开关专项测试（默认开启拒绝匿名 / off 放开匿名业务请求）、门禁既有回归、后端全量 `go test ./... -count=1` 与 `go vet ./...` 全部通过。
- 受保护发布构建（`build-protected-release.sh`）通过：注入受保护 `crypto.go`，ELF HMAC/SM4 双项与源码一致（`auth_binary_hmac_match=true`、`auth_binary_sm4_match=true`），未重现 `0fd72b4` 的占位参数事故。

## 生产切换（北京时间 2026-11-02 09:20）

| 项目 | 值 |
| --- | --- |
| 发布包 | `交付/门禁开关止血-20261102/jdlg-gate-switch-d80b582.tar.gz`（SHA-256 `640d397f8b41054052019db52a49e3c303b49bdb88252a8635ac3798ed8ccc49`，服务器校验通过） |
| 提交 | `d80b5822825ce75ab25af392e3ae71ccd4af86e1` |
| 运行二进制 SHA-256 | `5adcdb74dd8dcaec144004502e1b77def132fed1ba23ef9785416a846bcd7d98`（`/proc/488973/exe` 实测一致） |
| 新进程 | PID `488973` |
| 备份目录 | `/root/jdlg-gate-switch-backup-20261102-d80b582` |
| 环境变量 | `/www/wwwroot/jiadilingguang/service/bin/.env.local` 追加 `MINIPROGRAM_PROFILE_GATE=off`（改前备份 `/root/env.local.bak-20261102`，共 4 行） |
| 迁移 | 沿用既有四项幂等迁移，无新增 |

公网探针对照（匿名请求）：

| 接口 | 切换前（门禁开启） | 切换后（开关 off） |
| --- | --- | --- |
| `GET /api/v1/miniprogram/tracking/config` | 200 | 200 |
| `GET /api/v1/miniprogram/designers` | 401 参数不合法 | 200 code:0 完整列表 |
| `GET /api/v1/miniprogram/templates` | 401 参数不合法 | 200 code:0 完整列表 |
| `GET /ping` | 200 | 200 |

旧版正式版客户端的匿名业务路径恢复到门禁启用前行为，止血目标达成。登录用户的强校验（各路由原有 token/签名检查）不受影响。

## 🔴 恢复门禁步骤（7.2.21 正式发版验收后执行，勿遗忘）

1. 确认 7.2.21 已正式发布且线上用户可正常登录、补全头像昵称并使用业务。
2. 编辑 `/www/wwwroot/jiadilingguang/service/bin/.env.local`，删除 `MINIPROGRAM_PROFILE_GATE=off` 一行（或改为 `on`）。
3. 重启后端进程（宝塔或 `runner.py` 重新激活均可；`.env.local` 在进程启动时由 `config.LoadLocalEnv` 读取，必须重启才生效）。
4. 公网复验：匿名请求 `designers`/`templates` 应回到 401；`tracking/config` 保持 200。

## 环境变量预留说明（按 env 预留统一配置策略）

- `MINIPROGRAM_PROFILE_GATE`：**临时止血开关**，预期随 7.2.21 正式发版后删除。后续统一配置清单应将其登记为"待移除"项；门禁长期态为开启（无变量即开启）。
- 本轮未新增其他环境变量、依赖或数据库结构。

## 回滚

- 后端：`python3 /root/jdlg-gate-switch-d80b582/runner.py rollback --backup /root/jdlg-gate-switch-backup-20261102-d80b582`（回到 `b28e6a1`）。
- 仅撤开关不回版本：删除 `.env.local` 中该行并重启进程即可，无需回滚二进制（d80b582 与 b28e6a1 业务行为在开关开启时完全一致）。
