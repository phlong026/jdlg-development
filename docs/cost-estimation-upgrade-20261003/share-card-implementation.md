# 造价测算分享卡实现记录

## 目标

从工程造价测算报告分享时，生成带当前测算数据的 5:4 图片卡片。卡片沿用小程序暖金色调，参考总价使用红色，并使用业务方提供的现代别墅图片作为主视觉。

## 源码改动

- `miniprogram/miniprogram/pages/aicostresult/aicostresult.ts`
  - `onShareAppMessage` 从报告基础信息读取城市、总建筑面积、结构形式和门窗标准。
  - 分享标题为“{城市}{面积}建房参考造价{金额}，查看测算报告”。
  - 异步返回动态图片路径，接收方仍跳转到造价测算表单。
- `miniprogram/miniprogram/utils/costEstimateShareImage.ts`
  - 使用 `wx.createOffscreenCanvas` 绘制 1000 × 800 分享封面。
  - 总价数字为 `#e64b3c`，根据文字宽度计算“万元”位置，避免不同金额位数重叠。
  - 加载本地别墅图片并按容器比例裁切；图片加载失败时回退到原有建筑线稿，因此不会阻断分享。
  - 图片写入 `wx.env.USER_DATA_PATH/cost-estimate-share` 后作为 `imageUrl` 返回。
- `miniprogram/miniprogram/assets/images/cost-estimate-share-house.jpg`
  - 本次提供的现代别墅图片，208 KiB，本地随小程序资源一起构建。
- `miniprogram/tests/aicost-report.test.js`
  - 覆盖动态分享标题、封面回调、提供图片和线稿回退逻辑。

## 验证

- TypeScript：`node ../background/node_modules/typescript/bin/tsc --noEmit` 通过。
- 造价相关自动测试：13/13 通过。
- 分享卡导出测试：模拟本地图片、画布和文件系统后，确认 JPEG 导出参数与 `wxfile://` 图片路径均会返回。
- 依赖审计：`npm audit --omit=dev` 显示 0 vulnerabilities。
- 微信开发者工具：热重载后“问题”面板显示 0 个错误；报告页的动态分享在开发者工具内无法代替真实微信好友会话发送。

## 2026-10-04 分享截图回退修复

开发者工具曾展示报告页面截图而非动态卡片。原因是 `Canvas.toDataURL` 被误按异步回调调用，导致没有写入 `imageUrl`。现已改为同步导出 JPEG 数据，并先通过 `wx.getImageInfo` 取得本地别墅图片路径后再绘制。分享标题已生效，修复后分享接口会收到动态图片路径。

## 回滚

删除 `costEstimateShareImage.ts/.js` 与图片资源，并将 `aicostresult.ts/.js` 的 `onShareAppMessage` 恢复为原有静态分享配置即可。
