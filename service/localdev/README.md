# 本地联调环境

只连本机 Docker 里的 MySQL / Redis，**不会连接任何线上资源**。用于调试后台、跑回归检查。

## 一键启动（首次）

```bash
cd service
./localdev/dev.sh bootstrap   # up → migrate → start → seed，顺序已排好
```

之后：后端 `http://127.0.0.1:28180`；本地管理账号 `admin_local`（平台管理员）、`crm_local`（仅客户管理），口令见 `cmd/localseed/main.go` 中的 `localPassword`（仅本地、虚构）。

前端开发服务器：

```bash
cd background
VITE_DEV_PROXY_TARGET=http://127.0.0.1:28180 npm run dev -- --port 5180
```

## 回归检查

```bash
cd background
npm run check                                                   # tsc + 路由一致性
/opt/homebrew/opt/python@3.11/bin/python3.11 e2e/smoke.py        # API 契约 + 26 个路由 + 修复项的界面行为
```

`e2e/smoke.py` 会写数据（改灵石、改精选），只允许对本机地址运行。

## 隔离措施（为什么不会误连线上）

| 措施 | 说明 |
|---|---|
| `env -i` 启动 | 后端不继承当前 shell 的任何环境变量 |
| `JDLG_LOCAL_ENV_FILE` | 只加载 `localdev/local.env`，**不读取** `service/.env.local`（里面可能有真实密钥） |
| 不配置外部集成 | 微信支付 / 虚拟支付 / COS / AI 供应商 / 企微全部走"未配置"分支，无线上副作用 |
| 种子工具护栏 | `cmd/localseed` 在 DSN 主机不是 `127.0.0.1` / `localhost` 或 `ENV=production` 时拒绝运行 |
| E2E 护栏 | `e2e/smoke.py` 对非本机地址直接拒绝，且没有放行开关 |

## 常见问题

- **端口被占用**：默认后端 `28180`、MySQL `13306`、Redis `16379`、前端 `5180`。后端的日志不在 stdout，在 `service/APPlog/` 按天文件里。
- **启动顺序**：必须先 `migrate` 再首次 `start`——白名单自动迁移 `management_crm_link_intent_up.sql` 依赖 `management_crm_link` 表，顺序反了首次启动会报错（仅日志，不影响运行）。`bootstrap` 已处理。
- **排序规则**：不要给 MySQL 加 `--collation-server`，否则建库与建表的排序规则混用，触发 `Illegal mix of collations`。
- **重置**：`./localdev/dev.sh reset` 删除本地数据卷。
