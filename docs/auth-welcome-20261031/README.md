# 登录与资料补全欢迎场景 · 2026-10-31

最新效果见 [确认资料卡精简](inline-profile-simple.md)，流程见 [同页确认头像与昵称](inline-profile.md)，布局调整见 [第二轮底部布局与文字清晰度优化](refinement.md)。以下为首轮记录。

## 延续的修改

核对仓库：`phlong026/jdlg-development`，实际目录 `开发版916/JDLG`。
基线分支 `release/miniprogram-7.2.14`，基线提交 `02afdb8`（2026-10-30 22:10 +08:00）。该提交将登录门禁改为品牌底部卡，并保留资料完整账号的静默核验；昵称组件仍使用普通居中弹窗。

本轮工作分支：`feat/auth-welcome-scene-20261031`。

## 本轮结果

- 分享/公开内容登录门禁、独立登录页、老用户/新用户资料补全统一使用庭院欢迎场景。
- 顶部使用小程序原有屋檐图形、中文 Logo 与 ArchLight 英文标识。不重新绘制 Logo，不新增字体，页面继承小程序现有字体。
- 圆形头像为装饰示例，使用“一起发现理想的家”文案，不声称好友关系、点赞数或用户数量。
- 原生 `chooseAvatar`、`type="nickname"`、表单提交、隐私同意、账号资料核验及分享归属逻辑不变。
- 表单可纵向滚动，昵称输入保留自动顶起并增加输入框与键盘间距。
- 共享 WXML 模板与 Sass 样式，删除旧品牌文字排版和旧居中弹窗布局，未新增依赖。

## 修改位置

- `miniprogram/miniprogram/components/login-guard/login-guard.{wxml,scss}`
- `miniprogram/miniprogram/components/nickname-guard/nickname-guard.{wxml,scss}`
- `miniprogram/miniprogram/pages/login/login.{wxml,scss}`
- `miniprogram/miniprogram/templates/auth-welcome.wxml`
- `miniprogram/miniprogram/styles/_auth-welcome.scss`
- `miniprogram/miniprogram/assets/welcome/`：庭院、头像示例、原版 Logo
- `miniprogram/tests/auth-welcome-assets.test.js`

## 验证

- 全量小程序测试：755 项通过，0 失败，0 跳过，包含登录、资料补全、服务端回读与分享归属回归。
- TypeScript 全项目检查通过。
- 三个入口 Sass 编译通过；微信原生 WXML 编译通过（三个入口及共享模板）。
- 跟踪构建一致性与 61 条路由清单检查通过。
- 小程序生产依赖审计：0 漏洞；本轮变更未引入密钥或凭据。
- 微信开发者工具 2.02.2608080 / 基础库 3.16.3：iPhone 12/13 模拟器中显示登录与资料补全状态。通过本地组件 `setData` 展示测试状态，未提交资料、未执行新账号授权，不等于真实登录端到端验收。
- 原版 Logo 源文件与现有线上登录页引用资源 SHA256 相同：`17a4a850c4424b243e6f7221f3d72681e5163bbfacb2a811797d05b7fe7acf9d`。本地欢迎页使用其等比缩小版本。

截图：

- [新用户登录](login-preview.jpg)
- [补全昵称与头像](nickname-preview.jpg)

原始模拟器截图另存 `login-guard-native.jpg`、`nickname-native.jpg`。

## 验证边界与回退

未上传体验版、未发布生产。小屏真机、真实微信键盘顶起、原生头像授权及真实新/老账号完整链路尚需设备验收；独立登录页已通过编译与逻辑回归，未完成单独的原生视觉截图。

本轮仅涉及视觉、素材和新增验证，可整体 `git revert` 回退，无数据库或接口变更。期间并行提交 `2927ad8` 调整了 `pages/my/my.scss`，本轮提交不包含、不覆盖该文件。
