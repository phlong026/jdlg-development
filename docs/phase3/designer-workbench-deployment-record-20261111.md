# 设计师工作台整改 · 后端部署记录（2026-11-11）

- 分支/提交：`fix/designer-discovery-20261110`，部署提交 `f39ac81`（HEAD `f39ac81`）
- 服务器：`162.14.210.252`（VM-0-5-ubuntu），通过宝塔面板终端执行
- 范围：**仅主后端二进制**；管理台前端自 `8ef94ae` 无改动，未部署；小程序前端未上传微信平台

## 发布前核对

| 项 | 值 |
|---|---|
| 上传包 | `/root/jdlg-release-f39ac81.tar.gz`（29,794,932 字节） |
| 包 SHA-256 | `d9dac81e6f17bd2f0660170919e7803620ac445416b1dc8f73d645f06e9df40f`（与本地一致） |
| manifest 校验 | 421 文件，`bad=[]` |
| 新二进制 SHA-256 | `e1d7e6a84c582ef5348b275ceddc2abb2c37f55a4b75abf0f138283e6767566f` |
| 旧二进制 SHA-256 | `0f3f3f5a…ede63af2285f`（由发布器 prepare 时校验） |
| 部署前进程 | PID `421346`，`/www/wwwroot/jiadilingguang/api/applinux` |

## 执行记录

```
python3 /root/jdlg-backend-deploy-0be962c/binary_release.py prepare \
  --candidate /root/jdlg-release/applinux --candidate-sha e1d7e6a8… \
  --backup /root/jdlg-backend-backup-20261111-f39ac81 \
  --expected-pid 421346 --expected-old-sha 0f3f3f5a…
→ {"status": "prepared", ...}

python3 /root/jdlg-backend-deploy-0be962c/binary_release.py activate \
  --backup /root/jdlg-backend-backup-20261111-f39ac81
→ {"status": "active", "old_sha": "0f3f3f5a…", "candidate_sha": "e1d7e6a8…", "pid": 2066243}
```

## 部署后验证

| 检查 | 结果 |
|---|---|
| 新进程 | PID `2066243`（发布器返回） |
| 备份目录 | `/root/jdlg-backend-backup-20261111-f39ac81`（binary.previous、runtime-env.json、state.json，0700/0600） |
| 公网 `/api/v1/miniprogram/tracking/config` | HTTP 200 |
| 公网 `/api/v1/miniprogram/project-notes/projects`（未登录） | HTTP 401（符合鉴权） |
| 根路径 | HTTP 301（跳转正常） |

## 回滚

```bash
python3 /root/jdlg-backend-deploy-0be962c/binary_release.py rollback \
  --backup /root/jdlg-backend-backup-20261111-f39ac81
```
回滚恢复旧二进制与发布前运行环境；不涉及数据库迁移。

## 未包含 / 待人工处理

- 管理台静态资源：本次无改动，未部署。
- 小程序前端：需微信开发者工具上传（本机/CI 持 appid 与上传私钥），或由人工上传体验版。
- `TRACKING_PRIVACY_NOTICE` 文案更新：运维后台配置（T7.2 运营项），不随二进制。
- T5.2 上传压测、T7.4 线上包核查、T7.5 真机分享链路：按人工验收清单执行。
