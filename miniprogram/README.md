# 微信小程序

## 首页分享封面

在首页通过微信右上角菜单分享，标题为“甲第灵光 · AI建房助手”，接收者点击后进入首页。

- 好友和群聊：`miniprogram/assets/share/home-chat.jpg`，1000 × 800，适配 5:4 分享卡片，使用铺满画布的横向构图。
- 朋友圈：`miniprogram/assets/share/home-timeline.jpg`，使用用户提供的 1280 × 1280 方形原图。

封面随代码包上传。`project.config.json` 的 `packOptions.include` 显式包含 `assets/share`，避免开启 `ignoreUploadUnusedFiles` 后分享图片被遗漏。

好友封面通过内置 `imagegen` 工具重新排版，再编码为 JPEG。提示要求：保留参考图的品牌、完整中文文案、别墅及三项功能，将别墅场景和宣传背景铺满 5:4 横向画布，不加两侧补白、方形内框或新文案。朋友圈直接使用原图。

微信控制卡片外框、小程序名称及版本标识；应用配置控制分享标题与封面。朋友圈按微信的方形缩略图展示。

验证：在 `miniprogram` 目录执行 `node --test tests/home-share.test.js tests/home-*.test.js`；上传体验版后分别检查好友、群聊、朋友圈显示与点击进入首页。
