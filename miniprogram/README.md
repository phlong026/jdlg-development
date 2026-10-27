# 微信小程序

## 工程入口与云端图片

在微信开发者工具导入 `JDLG-home-restore/miniprogram`（本 README 所在目录），AppID 为 `wx94fdc48861913ede`，项目显示名为“甲第灵光-新版首页-云端图片”。同级旧工程 `JDLG/miniprogram` 仍是旧首页，不作为本次上传来源。

图片发布清单为 `assets-cloud-manifest.json`，记录 45 张原图的字节数、SHA-256 和不可变 CDN 地址。当前发布前缀：

`https://static.jiadilingguang.com/assets/miniprogram-releases/20261022-292ba168e0ef/`

`utils/asset.ts` 对清单中的图片返回对应版本 URL。更新图片时需先上传新版本、校验 CDN 原件，再同步清单与解析表，避免覆盖旧 URL 导致封面缓存混用。恢复首页已有的 `homeAssets.ts` 继续使用经过验证的原 CDN 版本。

两个 `project.config.json` 同步排除云图，保留原生 TabBar 配置要求的 5 张本地图标（123,513 字节）；这些图标也已备份到云端。不要忽略整个 `assets` 目录，也不要恢复强制包含整个 `assets/share` 的配置。

## 首页与预算分享封面

首页标题为“甲第灵光 · AI建房助手\n建别墅从这里开始”，点击后进入 `/pages/index/index?from=share`。

- 首页好友/群聊封面：`assets/share/home-chat.jpg`，1000 × 800。
- 首页朋友圈封面：`assets/share/home-timeline-v2.jpg`，1000 × 1000。
- 预算入口朋友圈封面：`assets/share/budget-timeline-v2.jpg`，1000 × 1000。
- 预算好友/结果卡片使用 `assets/images/cost-estimate-share-house.jpg` 下载后的原图绘制；结果金额仍来自当前测算。

以上图片通过 `resolveAssetPath` 获取版本化 CDN 地址，再由 `utils/cloudImage.ts` 下载到 `USER_DATA_PATH/cloud-images`。分享回调返回微信可读取的本地文件，避免原生封面破图。缓存按完整 URL 区分版本，并发请求合并，超时 10 秒；失败删除半成品、返回空路径并允许重试。朋友圈封面准备完成后才开放对应入口。

微信控制卡片外框、小程序名称与原生文字布局。标题包含换行符，但模拟器会合并为自然折行，不能据此声称已实现固定两行标题。

专项验证：`node --test tests/cloud-image.test.js tests/cloud-asset-package.test.js tests/home-share.test.js tests/aicost-*.test.js tests/cost-estimate-share-image.test.js tests/timeline-image.test.js tests/timeline-sharing.test.js tests/template-detail-moments-share.test.js`。

云端文件校验、实际包体、开发版本上传及验证边界见 `../docs/cloud-assets-20261022/README.md`。开发版本上传与微信审核、正式发布是不同阶段。

## 朋友圈入口与参数

分享菜单由统一页面生命周期管理：公开页面和已有显式朋友圈回调的页面开启好友、朋友圈两个入口；场景1154单页模式不调用分享菜单。私密订单、钱包、身份、报告和未知页面默认隐藏朋友圈。

默认分享只保留路由清单的公开内容 ID，不复制当前页面的全部参数。估算地区、建筑面积及订单号不进入默认分享；邀请码、生成任务编号、方案 ID 由各业务页面显式提供。

本轮恢复首页和分享修复的截图、原素材与验证边界见 `../docs/home-restore-20261021/README.md`。后续首页预览已实际渲染原封面，证据见 `模拟器-朋友圈封面已渲染.png`；系统性审计与待修问题见同目录的 `分享一致性排查.md`，真实微信的场景1154及跨账号验收仍需单独确认。

## 三期内容、分享和通知

首批实现的接口、默认关闭开关、迁移/回退及未完成项见[三期接入说明](../docs/phase3/README.md)。本地构建不代表生产部署或服务号真实验收。
