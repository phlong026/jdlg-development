# 设计师工作台整改 · 增量部署方案（2026-11-10）

- 用途：本次「设计师工作台新建笔记」整改的**部署方案、服务器与连接方式记录**。提交完成后按本文件执行增量部署。
- 关联：`ops/phase3-release/README.md`（生产发布运维手册）、`ENV-部署环境变量配置清单.md`、`docs/phase3/server-admin-deployment-20261101.md`、`docs/phase3/rectification-deployment-20261031.md`
- 原则：**先提交、后部署**；后端 → 管理台 → 小程序，逐层验证；任一步失败停止并回滚；密钥、口令、DSN、COS 凭据一律不入库。

## 一、发布对象

| 对象 | 源码位置 | 产出 | 服务器落点 |
|---|---|---|---|
| 主后端 service | `service/`（Go） | Linux amd64 `applinux` | `162.14.210.252:/www/wwwroot/jiadilingguang/service/bin/applinux`，端口 `8080` |
| 管理前端（管理台） | `background/`（Vue + Vite） | `npm run build` 的 `dist/` → 包内 `admin/` | `162.14.210.252:/www/wwwroot/jiadilingguang/admin`，入口 `https://api.jiadilingguang.com/admin/` |
| 小程序前端 | `miniprogram/` | 微信小程序上传包（开发者工具） | 微信平台：体验版 → 提审 → 发布（非服务器文件部署） |
| CRM（不在本次范围） | `customer-management/`（Java，3306/18087） | — | **不得触碰**：不改 `start-go.py`、`crm.env`、`SERVER_PORT=18087` |

## 二、服务器与连接方式

| 项 | 值 |
|---|---|
| 主机 | `162.14.210.252`（`VM-0-5-ubuntu`，Linux amd64） |
| 连接方式 | SSH `root@162.14.210.252`，**密钥登录**（本机 `~/.ssh` 配置，凭据不入库、不写入本文档） |
| 主后端目录 | `/www/wwwroot/jiadilingguang/service/bin`（唯一 `applinux` 进程，监听 8080） |
| 管理台目录 | `/www/wwwroot/jiadilingguang/admin` |
| 发布工具（服务器） | `/root/jdlg-main-release-runner.py`、发布包 `/root/jdlg-content-release` |
| 发布工具（仓库） | `ops/phase3-release/{runner.py,binary_release.py,web_release.py,verify_release.py,inspect.py}` |
| 版本指针 | `/root/jdlg-release-current.json`（0600，仅指针，不含凭据） |
| 备份目录 | `/root/jdlg-main-release-backup-<日期>`（0700；`runtime-env.json` 0600） |
| Nginx | `/usr/bin/nginx`，配置与静态资源由 `web_release.py` 分阶段发布，必须先 `nginx -t` |

> 访问凭据、私钥口令、数据库 DSN、COS 密钥均只存在于运维侧，本仓库不记录。

## 三、后端增量部署步骤

> 本地已按提交 `16f89fd` 预构建可部署产物（供发布时复用）：
> - 产物：`/tmp/jdlg-build/applinux-vcs`（Linux amd64，静态）
> - SHA-256：`e1d7e6a84c582ef5348b275ceddc2abb2c37f55a4b75abf0f138283e6767566f`
> - `go version -m` 显示 `vcs.revision=16f89fd67a2b215f6aa59c03dc0e55c1b0939ade`、工作区干净
> - 注意：必须在干净的主检出目录构建（worktree 构建不会嵌入 VCS 信息）

> 发布包已用 `ops/phase3-release/package_release.py` 在本地生成：
> - 目录：`/tmp/jdlg-release`（421 个文件：applinux + migrations×4 + admin/** + manifest.json）
> - manifest：`/tmp/jdlg-release/manifest.json`，421 项 SHA-256 已逐条校验通过
> - 管理台构建：`background npm run build` 成功，`npm test` 767/767 通过
> - 包内禁止 `.env.local`；迁移名必须与 `runner.py` 的 `MIGRATIONS` 一致


1. **构建**（工作区必须干净，记录提交号）：
   ```bash
   cd service
   GOCACHE=/tmp/jdlg-gocache GOMODCACHE=/tmp/jdlg-gomod GOOS=linux GOARCH=amd64 go build -o bin/applinux .   # 入口为 service/main.go
   ```
2. **打包 + 清单**：用 `ops/phase3-release/binary_release.py` 生成发布包与 `manifest.json`（含 SHA-256、提交号、`vcs.modified=false`）。
3. **上传**：`scp/rsync` 发布包到 `root@162.14.210.252:/root/jdlg-content-release/`，服务器侧核对 SHA-256。
4. **发布**（服务器，root，新备份目录 `0700`，顺序不可变）：
   ```bash
   RUNNER=/root/jdlg-main-release-runner.py
   PACKAGE=/root/jdlg-content-release
   BACKUP=/root/jdlg-main-release-backup-20261110
   python3 "$RUNNER" prepare  --package "$PACKAGE" --manifest "$PACKAGE/manifest.json" --backup "$BACKUP"
   python3 "$RUNNER" migrate  --backup "$BACKUP"
   python3 "$RUNNER" activate --backup "$BACKUP"
   ```
5. **验证**：`verify_release.py` + 公网接口抽查（未登录 401、公开 200、健康检查）；记录证据到 `docs/phase3/evidence/`。
6. **回滚**：`python3 "$RUNNER" rollback --backup "$BACKUP"`（只回程序/环境，不回数据库增量迁移）。

## 四、管理台增量部署步骤

1. 构建：`cd background && npm ci && npm run build`（产物 `dist/`）。
2. 复制为发布包内 `admin/`（`index.html` 必须在）。
3. 服务器侧发布（备份 → 校验完整清单 → 切换入口）：
   ```bash
   python3 ops/phase3-release/web_release.py admin --package "$PACKAGE" --backup "$BACKUP/web"
   ```
4. 如有 Nginx 变更：`web_release.py nginx --package "$PACKAGE" --backup "$BACKUP/web"`，先 `nginx -t` 再 reload。
5. 验证：入口与资源 HTTP 200 + 哈希匹配；Chrome 打开 `/admin/` 登录页。

## 五、小程序前端发布步骤

1. 构建：`cd miniprogram && npm run build:tracking`（`check:tracking` 必须通过）。
2. 微信开发者工具 CLI 上传（需 appid 与上传私钥，本机/CI 侧持有，不入库）：
   ```bash
   cli upload --project miniprogram --version <x.y.z> --desc "设计师工作台整改 20261110"
   ```
3. 平台设置体验版 → 真机回归（分享链路、笔记编辑、上传、隐私告知）→ 提审 → 发布。
4. 小程序无法通过服务器文件部署，**必须走微信平台**；本轮真机依赖项（T3.1/T3.2/T3.5、T7.4/T7.5）在此阶段验证。

## 六、本次部署涉及的环境与配置

| 配置 | 动作 |
|---|---|
| `TRACKING_PRIVACY_NOTICE` | 更新为默认开启口径文案（见 `tracking-privacy-notice-ops-20261110.md`） |
| `TRACKING_CONSENT_VERSION` | **不修改**（P7 决策） |
| 上传限额 | 本次后端包含图片 20 MiB / 视频 500 MiB 与 501 MiB body 上限；部署后生效 |
| 超时 | ReadHeader 10s / Read 900s / Write 900s / Idle 120s；客户端上传 900s |
| 其他 env | 沿用 `ENV-部署环境变量配置清单.md`，不新增必填项 |

## 七、部署前检查清单

- [ ] 所有 P0/P1 修复完成并通过评审（P0/P1 = 0）
- [ ] 所有 P2 修复完成（技术债台账清零或明确豁免）
- [ ] 提交完成，工作区干净，记录提交号
- [ ] `npm run check:tracking`、门禁测试、`go build ./...`、`go vet ./route` 通过
- [ ] 无 tsc/eslint/Go 单测环境限制已在部署记录中说明
- [ ] 部署顺序：后端 → 管理台 → 小程序
- [ ] 回滚路径与备份目录已确认

## 八、风险与边界

- **500 MiB 视频内存峰值**（T5.2 结论：≥500 MiB 堆 + 临时磁盘）：上线前建议完成流式改造（D4/D13）或限制并发；未改造时需运维限流。
- CRM（18087）不得重启/覆盖配置。
- 机器重启后无 systemd 自拉起；仅在明确确认激活状态时用 `runner.py start-current`。
- 数据库增量迁移不回滚；回滚只覆盖程序、环境与静态资源。
- 管理台发布保留旧哈希资源供已打开页面读取。
