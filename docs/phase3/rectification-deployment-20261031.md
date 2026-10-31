# 内容创作整改主后端部署记录

2026-10-31晚间，已通过用户登录的Chrome宝塔终端，将本轮主后端部署到`162.14.210.252`。部署命令成功输出已现场读取，公网检查在北京时间22:26完成。本记录不代表本轮小程序已经上传或全部41项需求已经完成。

## 发布来源与目标

| 项目 | 核验值 |
| --- | --- |
| 仓库 / 分支 | `phlong026/jdlg-development` / `feat/workspace-collections-20261031` |
| 程序源码提交 | `c36e9bb9f2cd3d52213e475dd47eb08befd5fdfb` |
| 发布前文档提交 | `91c0923f2820878a79676efb9b435decae853de6` |
| 目标服务器 | `162.14.210.252`，`VM-0-5-ubuntu`，`x86_64` |
| 主后端路径 / 端口 | `/www/wwwroot/jiadilingguang/service/bin/applinux` / `8080` |
| 发布前主进程 | 唯一`applinux`进程，PID `2100836` |
| 服务端发布包 | `/root/jdlg-content-rectification-c36e9bb/` |
| 私有备份目录 | `/root/jdlg-main-backup-20261031-c36e9bb` |
| 当前发布指针 | `/root/jdlg-release-current.json`（发布器激活成功后写入） |

上传的`jdlg-content-rectification-c36e9bb.tar.gz`为27,424,112字节，归档SHA-256为：

```text
f56d8bac5c357e5152f48e890709edec98c3f43e83ae1eb3fa26e37712fd8a44
```

解包前已核验归档哈希和所有归档成员路径，解包后调用发布器校验完整manifest，现场输出源码提交及候选程序SHA-256：

```text
ad6cb5ad43ed624031a671ba60f61bdf03437916a75477cea99f1247ba22b6ea
```

候选程序为Linux amd64静态构建，57,481,508字节。旧`dd7ed9c`包未用于本次切换。

## 现场执行证据

按顺序执行`runner.py prepare → migrate → activate`，每步看到成功输出后才继续：

```text
prepare completed: live process, runtime snapshot and private backups verified
migrate completed: additive migrations applied with captured runtime environment
activate completed: direct main-process health checks passed
```

`prepare`成功意味着旧程序、环境文件、运行环境快照及非空数据库备份已通过发布器检查；私有目录和运行环境快照分别按0700、0600保存。凭据、数据库备份和启动日志未导出或提交Git。

四项迁移是既有已审计增量迁移：通知、内容导入、公众号绑定、分享人索引。迁移后表、索引和历史分享人回填约束均由发布器校验通过。建立表不代表对应外部功能开关已打开。

`activate`成功意味着目标主进程、工作目录、候选哈希经过发布器检查，程序已替换，并以捕获的原运行环境加发布开关启动。它要求唯一主进程及本机8080接口返回预期结果：公开配置200、未登录工作台401。无需自动回退。

激活成功后终端输入通道再次出现焦点/页面刷新异常；额外的独立进程号、备份字节数和运行文件哈希读取未取得结果，因此不把这些追加读数列为已核验。部署判断依据是上述实际成功输出、此前服务器端包校验及下列公网新路由验证。

## 公网验证

证据文件：[六项公网检查原始结果](evidence/20261031-c36e9bb-public-verification.json)。检查使用正常HTTPS证书验证，所有请求的远端IP均为`162.14.210.252`，均返回JSON；没有写入测试内容或修改真实账号。

| 检查 | 结果 |
| --- | --- |
| `/api/v1/miniprogram/tracking/config` | 200，可解析JSON |
| `/api/v1/miniprogram/workbench/context`，不带登录凭证 | 401 |
| `/api/v1/miniprogram/workbench/project-collections`，不带登录凭证 | 401 |
| 私密资料包阅读路由，不带登录凭证 | 401 |
| 成员邀请阅读路由，不带登录凭证 | 401 |
| 不存在的公开合集版本 | 404，业务消息“合集不可访问” |

以上验证证明服务可达、新路由生效及未登录访问边界；不等于设计师、业主、受邀成员和撤权账号的真实业务验收。

另外核验`https://www.jiadilingguang.com/share/assets/reader.js`和`reader.css`：均返回200，远端IP正确，两个文件的SHA-256均与本次源码完全一致。详见[网页资源核验原始结果](evidence/20261031-c36e9bb-web-assets.json)。这两项资源来自后端内嵌文件，不需要另改Nginx或管理端目录。

## 上线范围与边界

本包服务端包含内容工作台、公开合集、项目私密资料包、成员邀请、媒体分页、合集网页阅读、分享归因及站内消息相关代码。复用原有通用记录表，不新建合集业务表。

- 启用创作工作台、项目笔记、站内消息、访问提醒及网页分享；媒体沿用私有COS配置。
- 外部导入、公众号绑定和服务号发送开关仍关闭；没有声称真实导入或服务号通知已交付。
- 本次只切换主后端；没有修改CRM18087、Nginx或管理端静态文件。
- 没有修改生产环境文件，也没有安装新的进程守护或自启动服务。机器重启后的自动恢复未在本次验证。
- 本轮小程序未上传、未选定体验版、未提审或正式发布。手机仍需取得相应新版后验收。
- 本轮本地自动化通过记录见[交付记录](rectification-delivery-20261031.md)。远端GitHub Actions因账户付款/额度限制未启动，不称为CI通过；没有更改付款设置。
- COS真实读写、多账号邀请/交付、成员撤权、实际分享人消息、新页视觉及手机手势验收仍待完成。

## 回退

若需回退本次主程序，先核对当前发布状态和业务影响，再使用已验证发布器：

```bash
python3 /root/jdlg-content-rectification-c36e9bb/runner.py rollback --backup /root/jdlg-main-backup-20261031-c36e9bb
```

回退恢复旧程序和原运行环境，保留增量数据库结构及新产生的业务记录；不执行删表迁移。本次没有为演练而切回生产旧版，实际回退演练仍未执行。
