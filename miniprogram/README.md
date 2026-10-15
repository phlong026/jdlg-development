# 微信小程序

## 首页分享封面

在首页通过微信右上角菜单分享，标题为“甲第灵光 · AI建房助手”，接收者点击后进入首页。

- 好友和群聊：`miniprogram/assets/share/home-chat.jpg`，1000 × 800，适配 5:4 分享卡片，使用用户在 2026-10-15 提供的蓝天白色别墅横向图。
- 朋友圈：`miniprogram/assets/share/home-timeline.jpg`，使用用户提供的 1280 × 1280 方形原图。

封面随代码包上传。`project.config.json` 的 `packOptions.include` 显式包含 `assets/share`，避免开启 `ignoreUploadUnusedFiles` 后分享图片被遗漏。

好友封面直接使用用户指定的新图，保留“建别墅，从这里开始”和“测造价 · 选户型 · 看效果”等原图内容，仅导出为 1000 × 800 JPEG，完整展示品牌、别墅和文字。朋友圈使用原先的方形宣传图。

微信控制卡片外框、小程序名称及版本标识；应用配置控制分享标题与封面。朋友圈按微信的方形缩略图展示。

验证：在 `miniprogram` 目录执行 `node --test tests/home-share.test.js tests/home-*.test.js`；上传体验版后分别检查好友、群聊、朋友圈显示与点击进入首页。
