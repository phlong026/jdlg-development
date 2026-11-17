# 服务号首次URL验证修复

2026-11-17已合并[PR #58](https://github.com/phlong026/jdlg-development/pull/58)，部署并重启后端，基础接入已开启。用户已确认微信后台验证／保存成功；正确签名公网GET返回200并原样返回challenge，非法签名与两种AES防降级检查均403。模板外发、小程序关联和真实AI继续关闭，详见[部署回执](release-receipt.json)。

微信保存服务器配置时，首次GET请求使用`signature`、`timestamp`、`nonce`和`echostr`。原实现只支持`msg_signature`和加密challenge，会拒绝官方首次验证。本次补齐首次验证，验签通过后原样返回`echostr`，不写入绑定、订阅或通知数据。

## 接口约定

`GET https://api.jiadilingguang.com/api/v1/wechat/official/callback`

- 首次验证：对Token、timestamp、nonce字典序排序后计算SHA-1，与40位小写十六进制`signature`做常量时间比较；成功返回200和原样`echostr`。
- 时间戳仍限制在服务器时间前后5分钟；nonce为1至128字节，echostr为1至4096字节。
- 存在非空`msg_signature`或`encrypt_type=aes`时，仅执行原AES验签解密；失败返回403，不降级到明文。未知非空加密模式拒绝。
- 配置未启用、绑定能力未就绪、Store不存在或时间戳不新鲜时，保持503；其他非法challenge返回403。
- POST回调继续只接受已验签的AES XML推送，保留AppID校验和既有回执/订阅事件处理。

官方依据：[服务号消息推送接入](https://developers.weixin.qq.com/doc/service/guide/dev/push/index.md)、[消息加解密](https://developers.weixin.qq.com/doc/service/guide/dev/push/encryption.md)。首次GET验证与后续安全模式POST是两个阶段。

## 配置与验证

当前实际程序为`/www/wwwroot/jiadilingguang/api/applinux`，启动时读取同目录`.env.local`。`service/bin/.env.local`不由当前程序自动加载。私有配置文件权限保持0600，Token、AESKey和AppSecret不提交Git、不输出到日志或聊天。

先完成AppID、AppSecret、Token、43位EncodingAESKey和`PUBLIC_SHARE_ORIGIN=https://api.jiadilingguang.com`。基础接入开启使用`OFFICIAL_ACCOUNT_ENABLED=true`；`OFFICIAL_ACCOUNT_DELIVERY_ENABLED=false`和`OFFICIAL_ACCOUNT_MINIPROGRAM_VERIFIED=false`保持，直到模板与小程序关联完成。通知发送和真实AI调用不属于本次验签修复。

配置只在启动时读取。备份后更新程序并重启，验证正确签名GET返回200和原样challenge、错误签名返回403、模式混淆请求拒绝，再让用户重新提交微信配置。普通匿名GET返回403并不表示接入失败；微信签名GET才是有效验证。

## 验证与回滚

先以官方明文challenge复现基线403，再运行`CGO_ENABLED=0 go test ./officialaccount ./route -count=1`和对应vet。定向回归覆盖签名、缺失/超长参数、过期、关闭/缺少Store、AES成功及防降级、明文POST拒绝。macOS既有`go-m1cpu` CGO初始化崩溃使用无CGO路径验证，不记为通过。

程序、私有环境文件分别备份。仅回退配置时恢复关闭状态并受控重启；同时回退程序时须按下文顺序操作，并核对当前进程、二进制和环境未被后续任务修改。无数据库结构变更，不修改余额或任务，不清共享Redis。前端代码保持已上传8.0.13及已部署管理台内容。

本次运行PID为3820269，程序及配置备份位于`/root/jdlg-official-backup-20261117-aab310e`。若需要回退，先保持prepare时环境文件内容不变，通过本次`binary_release.py rollback`恢复旧程序；然后再恢复`api.env.local.before-enable`并受控重启。先修改环境文件会触发发布器哈希校验并拒绝程序回滚；不能把源码revert视为配置回退。
