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

## 第二次部署（2026-11-11 12:2x，邀请免审批 + 归因 + 签名流式播放）

- 包：`开发版916/交付/jdlg-backend-6895998.tar.gz`（包 SHA-256 `ccdc141a…c06307`）
- 二进制：SHA-256 `b222aaaeb5478fd0845a9dd57c36009d10c470f92d3c5e30ef2ef84f4030df90`，Linux amd64，`vcs.revision=6895998`
- 发布器 prepare：`{"status":"prepared","old_sha":"e1d7e6a8…","candidate_sha":"b222aaae…","pid":2066243}`
- activate：`{"status":"active", ..., "pid":2141526}`
- 备份：`/root/jdlg-backend-backup-20261111-6895998`（binary.previous、runtime-env.json、state.json）
- 公网验证：`/tracking/config` 200、`/project-notes/projects` 401、`POST /tracking/media/<id>/signed` 未登录 401、根路径 301

### 本包变更
1. 邀请链接登录后直接加入（服务端 apply 即 approved，免审批）
2. 成员归因：inviter_id / invitation_id / joined_at，详情返回 inviter_name
3. 媒体短时签名地址（10 分钟 TTL）+ Range 流式播放
4. 无数据库迁移、无环境变量变更

## 第三次后端部署（2026-11-11，提交 dab2b4a）

- 背景：纠正 2026-11-11 早些时候引入的「方案分享需申请/审批」策略；恢复「有效分享链接 + 已登录」直接查看
- 包：`开发版916/交付/后端-20261111-dab2b4a.tar.gz`（SHA-256 `42355af7…c18c1d3b`）
- 二进制 SHA-256：`8ac049b2…8bb91bed`（Linux amd64，`vcs.revision=dab2b4a`）
- 备份目录约定：`/root/jdlg-backend-backup-20261111-dab2b4a`
- 无数据库迁移、无环境变量变更

## 最新后端交付包（2026-11-11，提交 a394e9b）

- 包：`开发版916/交付/后端-20261111-a394e9b.tar.gz`（27,770,451 字节，SHA-256 `a141bc0c…4d9f2047`）
- 二进制 SHA-256：`53ef8838…48b2c8522`（Linux amd64，`vcs.revision=a394e9b`，vcs.modified=false）
- 变更：分享链接登录即看（无需成员/审批）、邀请免审批、成员归因、媒体签名流式播放
- 部署备份目录约定：`/root/jdlg-backend-backup-20261111-a394e9b`
- 旧包 `后端-20261111-dab2b4a` 已删除，避免误传

## 第四次后端部署（2026-11-11，提交 a394e9b）

- 包：`开发版916/交付/后端-20261111-a394e9b.tar.gz`（SHA-256 `a141bc0c…4d9f2047`）
- 二进制 SHA-256：`53ef8838…48b2c8522`（Linux amd64，`vcs.revision=a394e9b`）
- 发布前：PID 2141526，旧 SHA `b222aaae…4030df90`
- prepare：`{"status":"prepared", "pid":2141526}`
- activate：`{"status":"active", "pid":2238331}`
- 备份：`/root/jdlg-backend-backup-20261111-a394e9b`
- 公网验证：tracking/config 200、project-notes/projects 401、根路径 301
- 策略：方案分享链接「登录即可查看」，无成员/审批要求
