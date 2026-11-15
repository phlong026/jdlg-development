# 兼容发布与回退

本轮没有数据库结构迁移。`customer_ref` 在既有项目PATCH中增加可选三态字段；素材筛选只扩展现有查询；旧调用不提供新参数时维持原行为。

## 发布次序

1. 完成自动化与原生验收，提交本轮文件，创建PR并等待必需检查通过。
2. 使用预期head SHA合并到`main`，从合并后的精确源码生成发布物。
3. 在隔离干净工作树使用`service/scripts/build-protected-release.sh`，注入现有受保护的`crypto.go`，不把认证密钥写入Git。构建器验证前端签名契约及Linux ELF中实际密钥。
4. SSH通道`jdlg-prod`上传二进制及发布器至新的私有目录。使用当前PID和旧二进制SHA256显式执行`binary_release.py prepare`，备份原程序、原运行环境、原参数和应用配置后再`activate`。
5. 管理后台使用同一合并提交构建，旧`admin/`保留完整备份，原子切换目录；检查入口及引用静态资源。
6. 核对公网健康、认证拒绝、旧版兼容及已授权真实账号路径。
7. 在正确AppID及开发权限下上传小程序，记录官方终态回执。开发上传、体验版、审核、正式发布单独核验；不会把上传当成正式上线。

## 生产目录

- 主后端：`/www/wwwroot/jiadilingguang/api/applinux`，当前工作目录为该`api/`目录，端口8080。
- 管理后台：`/www/wwwroot/jiadilingguang/admin/`。
- 原进程`/proc/<pid>/environ`作为运行环境真值；发布器私有保留环境快照，不输出凭据、不修改原配置。
- 宝塔/TAT备用部署通道继续保留。

## 回退

后端执行同一私有发布器`rollback --backup <本轮私有目录>`，恢复旧程序和原运行环境并探活。管理后台切回本轮备份目录，核对HTML引用资源。小程序保留上一版本上传与正式发布状态，必要时按微信平台能力回退；尚未正式发布的版本不影响线上正式版。

代码通过中文Lore逻辑提交回退；临时工作树验证反向补丁或revert可用，不改变原工作区。运行环境和备份仅存私有目录，不进入仓库或发布公共文件。

## 本轮执行回执

2026-11-15（Asia/Shanghai）。[PR #51](https://github.com/phlong026/jdlg-development/pull/51)在5项CI全部成功后于19:07:40合并，应用发布源码为`55f836d7a913ccaada4e522f0c5f8db45c5c5a13`。完整机器回执见[release-receipt.json](release-receipt.json)。

| 项目 | 已核验结果 |
| --- | --- |
| CI | Admin quality run 43705627806、Share tracking quality run 43705627760，5项全部成功，含Linux race |
| 后端 | PID460749；SHA256 `135764731e11ce90b91f160e92a7cad18d160d32182e6b92ea03a168e3f970b3`；运行环境及配置保留，健康检查通过 |
| 后端备份 | `/root/jdlg-pr51-backup-20261115-55f836d`；目录0700、环境快照0600 |
| 后台前端 | 同一合并源码构建，发布94文件；公网index与构建哈希一致，5个入口引用资源均200，旧哈希资源保留 |
| 后台备份 | `/root/jdlg-pr51-web-backup-20261115-55f836d/admin/original` |
| 生产包 | `/root/jdlg-pr51-release-20261115-55f836d/package-clean`；102个文件校验通过，无AppleDouble元数据 |
| 公网接口 | tracking/config=200；未登录workbench/context与imports/capabilities均401 |
| 已登录只读 | 工作台4项内容、my-visits和素材空态读取成功；本人固定版本管理预览成功，跟踪写请求0、媒体错误0 |
| 小程序 | AppID `wx94fdc48861913ede`；8.0.9；从干净合并源码上传，官方CLI exit0与`✔ upload`终态 |

原生产二进制SHA256为`e10d26552b4b75a7cd4d22ae98fbdb3e1508e2530471f50b8a3c4068decb1b66`、PID1051555。本次未执行数据库迁移，未改环境变量或功能开关。

首轮管理台发布因压缩包含macOS自动生成的`._*`文件，被完整清单校验在任何覆盖前拦截。重新使用`COPYFILE_DISABLE=1 tar --no-xattrs --no-acls --no-fflags`打包，核对档案无`._*`且服务器文件集与清单一致后成功发布。没有修改应用源码、打包器或发布器。

## 实际回退入口

后端：使用本轮私有目录中的`binary_release.py rollback --backup /root/jdlg-pr51-backup-20261115-55f836d`，恢复原二进制和原运行环境，再核验公网与认证路径。

后台：将`/root/jdlg-pr51-web-backup-20261115-55f836d/admin/original/`的原文件恢复到`/www/wwwroot/jiadilingguang/admin/`（入口最后恢复），检查旧HTML引用资源。新增哈希文件可保留为无引用资源，不需删除生产文件。

小程序此次仅完成代码上传；体验版指向未独立核验，未在本任务提审或正式发布。系统键盘、手机相册、微信原生选收件人及完整真机客户旅程仍待验；不能将生产只读冒烟或CLI上传称为正式小程序发布。
