# 在原登录卡片内确认头像与昵称

## 用户看到的流程

- 登录后发现资料缺失时，原底部卡片切换为头像、昵称确认表单，背景、Logo、字体与上方介绍保持不变。
- 已保存资料自动带入；资料完整的老账号沿用原来的直接进入行为。
- 新用户点击头像选择入口、昵称输入框，通过微信原生能力填写后点击“确认并继续”。登录入口不再另铺一层完整的资料页面。
- 独立登录页原有身份选择步骤保留，本轮只合并资料确认界面。

小程序不能通过一次登录静默获得新用户的真实微信头像和昵称。微信官方提供的是用户主动操作的 [头像昵称填写能力](https://developers.weixin.qq.com/miniprogram/dev/framework/open-ability/userProfile.html)：头像选择事件返回临时文件，昵称输入框提供微信昵称快捷填充。旧 `getUserProfile` 文档也提示头像昵称获取规则已调整。

## 修改位置与复用

- `miniprogram/miniprogram/components/login-guard/login-guard.{wxml,scss}`：在同一场景中切换登录卡和资料卡，持续挂载编辑器，保留上传与输入状态。
- `miniprogram/miniprogram/components/nickname-guard/nickname-guard.{ts,js,wxml,scss}`：新增嵌入展示模式，复用现有选择头像、昵称输入、保存、账号校验及服务端回读。
- `miniprogram/miniprogram/pages/login/login.{ts,js,wxml,scss}`：独立登录页同步使用嵌入表单，资料确认成功或登录失效时退出该状态。
- `miniprogram/tests/auth-welcome-assets.test.js`、`wechat-login-flow.test.js`：覆盖静默核验、场景保持、资料卡切换和成功退出。

删除登录入口的第二层完整资料场景及重复装饰内容，未增加依赖、接口或字体。原本独立使用资料组件的页面仍保留其展示方式。

## 验证记录

- 全量小程序测试 756 项通过，0 失败、0 跳过。
- TypeScript 检查通过；4 份相关 Sass 编译通过，仅有项目已有的 `@import` 弃用提示。
- 使用微信开发者工具原生 WXML 编译器，直接编译两个组件、登录页和共享模板，4 项均通过。全量测试内的其他页面 WXML 检查不替代此项。
- 微信开发者工具 2.02.2608080 / 基础库 3.16.3：390×844 中表单和确认按钮同屏；320×568 中可滚动到确认按钮，无遮挡。
- 生产依赖审计 0 漏洞；依赖文件未变化。变更文件凭据检查、差异空白检查和反向补丁检查通过。

截图为本地组件状态展示，未执行真实新账号授权或资料上传，未修改账号持久化数据：

- [同一页面展开资料确认，390×844](inline-profile.jpg)
- [小屏滚动后可确认，320×568](inline-profile-small.jpg)

原始截图为 `inline-profile-native.jpg` 与 `inline-profile-small-native.jpg`；视觉复核见 `inline-profile-verdict.json`。

## 验证边界与回退

未上传体验版或发布。真实微信昵称键盘、头像选择、安全检测及新老账号完整登录链路尚需真机验收。独立登录页通过逻辑与编译验证，尚无本轮独立视觉截图。

本轮只变更前端呈现和状态标记，无接口或数据结构迁移，可通过提交回退恢复。
