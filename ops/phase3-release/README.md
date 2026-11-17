# 三期生产发布运维手册

当前主后端实际运行目录已为 `/www/wwwroot/jiadilingguang/api/applinux`。下文 `runner.py` 的 `service/bin` 流程是历史通道，不能直接用于当前生产。2026-11-17部署准备请先读[当前发布入口与注意事项](../../docs/release-prepare-20261117/README.md)，使用 `binary_release.py` 并显式传入窗口实测PID/旧哈希；数据库备份和迁移单独处理。

## 生产边界

- 小程序主后端是独立 `applinux` 进程，目录为 `/www/wwwroot/jiadilingguang/service/bin`，监听 `8080`。
- CRM 是另一套服务，监听 `18087`。不得使用 CRM 的 `start-go.py`、`crm.env` 或 `SERVER_PORT=18087` 启动主后端，也不得覆盖 CRM 配置。
- 发布前必须存在且只能存在一个旧主后端进程；其可执行文件和工作目录必须与上述主后端目录一致。
- 旧进程的 `/proc/<pid>/environ` 是本次发布的运行环境真值。生产 `.env.local` 必须存在，但发布器只备份和校验 `.env`、`.env.local`，不会修改它们。
- 主后端运行环境中的 `SERVER_PORT` 必须未设置或等于 `8080`，否则 `prepare` 会在备份和数据库操作前拒绝执行。

## 发布命令

以下命令均以 root 执行。每次发布使用新的、权限为 `0700` 的 `BACKUP` 目录，不得复用旧目录。

```bash
RUNNER=/root/jdlg-main-release-runner.py
PACKAGE=/root/jdlg-content-release
BACKUP=/root/jdlg-main-release-backup-20261031

python3 "$RUNNER" prepare \
  --package "$PACKAGE" \
  --manifest "$PACKAGE/manifest.json" \
  --backup "$BACKUP"

python3 "$RUNNER" migrate --backup "$BACKUP"

python3 "$RUNNER" activate --backup "$BACKUP"
```

必须按 `prepare → migrate → activate` 顺序执行，任一步失败都应停止后续步骤。`prepare` 会备份旧二进制、`.env`、`.env.local`、数据库和旧进程运行环境；`migrate`、`activate` 会再次验证 PID、进程启动时间、文件哈希和运行环境快照。

## 回滚与显式重启

发布成功后需要回到旧版本时：

```bash
python3 "$RUNNER" rollback --backup "$BACKUP"
```

`rollback` 会恢复旧二进制，并使用发布前捕获的原始运行环境启动和探活。若旧版本探活失败，发布器会尝试恢复刚才的新版本。数据库迁移是增量保留的，回滚不会执行任何 `*_down.sql`，也不会删除已产生的数据。

机器重启或进程退出后，只有在运维人员明确确认当前激活状态时，才可显式执行：

```bash
python3 "$RUNNER" start-current --backup "$BACKUP"
```

`start-current` 不是 systemd 自动拉起服务，不会安装守护进程；若主后端已经运行，它只做探活，不会再启动第二个进程。不得用此命令启动或替代 CRM 服务。

## 私有备份与凭据

- `BACKUP/runtime-env.json` 含生产运行凭据，文件权限为 `0600`，其父目录必须为 `0700`。
- `BACKUP`、数据库备份、运行环境快照和启动日志不得提交 Git、上传发布包、发送到聊天或复制到公共目录。
- `/root/jdlg-release-current.json` 仅保存当前私有备份目录的指针，不包含凭据；仍应保持 `0600`。
- 发布器不会打印 MySQL DSN、COS 密钥、会话密钥或完整运行环境。

## Web 与 Nginx

- 已确认 Nginx 命令为 `/usr/bin/nginx`。
- 已确认管理端发布根目录为 `/www/wwwroot/jiadilingguang/admin`。
- Nginx 配置和管理端文件由 `web_release.py` 分阶段发布；Nginx 必须先通过 `nginx -t` 才能 reload。

```bash
python3 ops/phase3-release/web_release.py nginx --package "$PACKAGE" --backup "$BACKUP/web"
python3 ops/phase3-release/web_release.py admin --package "$PACKAGE" --backup "$BACKUP/web"
```

后端激活失败会自动恢复旧二进制和旧运行环境；Nginx 或管理端发布失败会自动恢复原配置或原文件。自动回退只覆盖程序与文件，不撤销已经成功执行的增量数据库迁移。
