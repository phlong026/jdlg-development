# 逐页整改部署结果 · 2026-11-05

后端和管理后台已部署到生产并核验通过；小程序7.4.2已完成最终开发版上传。小程序尚未提审或正式发布，微信公众平台网页当前未登录，已请求用户扫码。开发者工具登录与公众平台网页登录是两套会话。

## 来源与目标

| 项目 | 实际结果 |
| --- | --- |
| 代码分支 | feat/designer-workbench-rectification-20261105，已推送GitHub |
| 后端功能提交 | bd95f63；固定构建源ebafbb7934faa13e4ed311639af96c34ab0a5919 |
| 小程序最终源码 | d3e41208075ee9f734245e481e1a7755ebc38d7a；版本7.4.2；AppID wx94fdc48861913ede |
| 小程序源码树 | 53b732bac6ff9423560c36a82196595b619e3cc9 |
| 管理后台测试修正 | 7d8c32d；业务页面沿用该分支现有实现并重新构建 |
| 服务器 | 162.14.210.252 |
| 后端路径 / 进程 | /www/wwwroot/jiadilingguang/api/applinux；PID679680 |
| 后端运行SHA256 | 56d9c3f378b83b5c0d18547b86911d9fa170505866b968611475d410f95f39f0 |
| 后台目录 | /www/wwwroot/jiadilingguang/admin |
| 后台地址 | https://api.jiadilingguang.com/admin/，Chrome实际显示/admin/login欢迎登录表单 |
| 后台入口SHA256 | 9e355d481ff8f8fb0ffe29b109489d6e6bcd4bf9f696b49aff5fba9baba8b1e8 |

小程序后续只修正提醒面板小按钮宽度，`service`与`background`树与已部署构建源一致；后端无需因小程序样式修正重复部署。后端通过受保护认证源码注入、客户端签名契约及ELF一致性检查后构建。受保护认证源码和实际凭据未提交。

## 实际执行与证据

发布包28,560,928字节，归档SHA256为621d194424edb02986143fce64340563dd174621b3d7a9d28b068852faada781。通过现有已登录宝塔会话上传，服务器先复查归档和候选程序哈希，再prepare私密备份、activate后端、安装管理后台。旧进程3993339切换为679680，发布器状态active；管理后台状态installed、323文件。

- [服务器复核](release-evidence/server-verification.json)：运行与磁盘程序均匹配候选哈希；原配置、启动环境、参数保持一致；323个后台文件现场哈希全部匹配；备份权限0700。
- [公网验证](release-evidence/public-verification.json)：323/323后台文件HTTPS 200且哈希匹配；公开配置200；未认证工作台、客户详情、转线索和服务号状态401，权限门禁正常。
- [真实登录账号复核](release-evidence/postdeploy-real-ui.json)：客户列表与详情、20条分享、3条内容和站内消息均无错误；客户详情显示4次查看31秒且保留快照；客户/分享卡均398px，视口430px。
- [小程序最终上传回执](release-evidence/miniprogram-receipt.json)和[官方CLI日志](release-evidence/miniprogram-upload.log)：最终7.4.2上传命令退出0、返回“✔ upload”。主包与总包见回执，不将此结果描述为正式发布。
- [构建清单](release-evidence/auth-build.json)及[包清单摘要](release-evidence/package.json)保留固定来源与校验值。

## 验证与边界

小程序完整1075项测试含原生WXML编译，0失败、0跳过；TypeScript、签名及跟踪产物检查通过。最后按钮样式修正另通过SCSS编译及15项消息中心回归，原生测得刷新64px、收起41px。管理后台762项测试及类型/路由检查、生产构建通过；Go完整测试及go vet通过；发布器43项隔离测试通过；两前端依赖审计零漏洞；新增代码凭据扫描无发现。

服务号状态目前明确为“未开启”，站内消息正常且首屏不依赖服务号。真实外发需要完整配置、接收身份、订阅及微信回执联验，本次保留原生产配置。未执行生产数据结构迁移。转线索仅在本人范围和身份门禁通过时显示；数据库尚无开放线索唯一约束，当前单一进程锁不能替代多实例原子去重。

手机键盘/大字体、多成员受邀接收、有数据的真实导入及有效已发布内容/合集完整阅读仍需相应真实场景。管理后台完成登录页与全量静态资源验证，未登录后执行业务写入。本地竞态检测因go-m1cpu的cgo初始化崩溃未进入测试；不将其写为通过。未声称远端CI已运行。

## 回退

后端私密备份：`/root/jdlg-workbench-backup-20261105`；管理后台原目录：`/root/jdlg-workbench-web-backup-20261105/admin/original`。原环境、原程序和配置仅留服务器私密目录，未导出或提交。

后端回退命令：

```sh
python3 /root/jdlg-workbench-release-20261105/package/binary_release.py rollback --backup /root/jdlg-workbench-backup-20261105
```

管理后台回退按原目录先恢复资源、最后恢复index.html，保留无引用新增资源。发布工具已做隔离回滚测试；生产未执行回退演练。本地代码均已提交，原有未跟踪.worktrees目录未纳入本次交付。
