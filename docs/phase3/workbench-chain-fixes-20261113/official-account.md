# E02 服务号配置与真实投递门禁

状态：**外部参数缺失，尚未关闭**。本轮只读核查发现独立服务号AppID/Secret、加密参数、模板及字段映射未配置，总开关和投递开关未开启；没有通过打开开关伪造就绪，也没有外发消息。

使用账号：既定“华匠邦建科”独立服务号；小程序AppID/Secret、文章同步配置不能替代其凭据。

需在服务器私有配置填入service/env.example所列OFFICIAL_ACCOUNT_*值。回调Token参数的实际键是OFFICIAL_ACCOUNT_CALLBACK_TOKEN，AES Key为43位；PUBLIC_SHARE_ORIGIN需为固定HTTPS来源。模板字段必须从实际可用模板获取，并映射至title/body/time，不能复制其他公众号字段名或使用猜测默认值。服务号关联当前小程序和固定消息中心跳转核实后，才可设置MINIPROGRAM_VERIFIED。

开通顺序：

1. 核对独立服务号认证、接口权限、OAuth/业务域名和模板；将密钥仅保存在服务器0600配置。
2. 核对绑定/通知表schema、服务号与小程序关联，先启用绑定能力、验证关注/解绑/重绑和回调签名。
3. 真实模板字段及接收人就绪后再启用外发；使用明确授权的测试接收人验证消息受理、微信回执和实际看到消息。
4. 站内通知留存、外发偏好、浏览事件资格分别验收，不能以一个总开关代替。

回退：关闭独立投递开关，保留站内通知和投递事实；不清空消息、绑定与回执数据。凭据不进Git、不写日志、不在聊天发送。

现有代码校验：service/officialaccount/binding.go Config.Status、service/route/phase3_notifications.go、phase3_notifications_test.go。已存在的配置/模板映射测试验证未配置及非法字段不放行；本轮没有真实模板，因此该项仍是依赖而非已修复。
