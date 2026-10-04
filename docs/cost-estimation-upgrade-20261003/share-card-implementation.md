# 造价测算分享卡实现记录

## 目标

从工程造价测算报告分享时，生成带当前测算数据的 5:4 图片卡片。卡片沿用小程序暖金色调，参考总价使用红色，并使用业务方提供的现代别墅图片作为主视觉。

## 源码改动

- `miniprogram/miniprogram/pages/aicostresult/aicostresult.ts`
  - `onShareAppMessage` 从报告基础信息读取城市、总建筑面积、结构形式和门窗标准。
  - 分享标题为“{城市}{面积}建房参考造价{金额}，查看测算报告”。
  - 异步结果中再次返回标题、跳转路径和动态图片路径，避免异步完成后转发标题退回为小程序名称；接收方仍跳转到造价测算表单。
- `miniprogram/miniprogram/utils/costEstimateShareImage.ts`
  - 使用 `wx.createOffscreenCanvas` 绘制 1000 × 800 分享封面。
  - 按确认稿重绘为左文右图：标题“建房造价测算”、红色总价、两行测算参数、别墅主视觉及金色“点击查看测算报告”按钮。
  - 总价数字为 `#e64b3c`，在保留大号金额字体时测量文字宽度，再定位“万元”，避免金额和单位重叠。
  - 直接从小程序包内加载别墅图片，优先根路径并保留无前导斜杠路径兼容；不再绘制线框房屋回退视觉。
  - 图片写入 `wx.env.USER_DATA_PATH/cost-estimate-share` 后作为 `imageUrl` 返回。
- `miniprogram/miniprogram/assets/images/cost-estimate-share-house.jpg`
  - 本次提供的现代别墅图片，208 KiB，本地随小程序资源一起构建。
- `miniprogram/tests/aicost-report.test.js`、`miniprogram/tests/cost-estimate-share-image.test.js`
  - 覆盖动态分享标题、异步返回内容、本地别墅图片、金额与单位间距、按钮文案及 JPEG 导出。

## 验证

- TypeScript：`node ../background/node_modules/typescript/bin/tsc --noEmit` 通过。
- 造价相关自动测试：14/14 通过。
- 分享卡导出测试：模拟本地图片、画布和文件系统后，确认 JPEG 导出参数与 `wxfile://` 图片路径均会返回。
- 依赖审计：`npm audit --omit=dev` 显示 0 vulnerabilities。
- 微信开发者工具：热重载后“问题”面板显示 0 个错误；报告页的动态分享在开发者工具内无法代替真实微信好友会话发送。

## 2026-10-04 分享截图回退修复

开发者工具曾展示报告页面截图而非动态卡片。原因是 `Canvas.toDataURL` 被误按异步回调调用，导致没有写入 `imageUrl`。现已改为同步导出 JPEG 数据。

随后发现动态图片已生成但主视觉退化为线框房屋、金额与“万元”重叠、微信外层标题退回小程序名称。现已改为直接加载包内别墅图片，并在异步分享结果中完整返回标题、路径和图片；金额宽度也在大号金额字体下测量。

## 2026-10-04 分享卡排版调整

- 移除内层白色圆角面板和右侧竖版照片裁切，使用完整方形别墅原图配合左侧、顶部、地面渐变融入米金色背景。
- 标题由 40 px 放大为 50 px，金额最大 148 px，参数最大 42/40 px；文字根据实际宽度收缩，金额与万元保持 14 px 间隔。
- 金色按钮高度由 88 px 增加至 124 px，文字 48 px，按钮文字和箭头整体居中。
- 模拟器执行原绘图代码发现相对资源路径会解析至当前页面目录并返回 500；改为优先根路径，兼容路径仍保留。
- 删除不再使用的照片裁切辅助函数；未新增依赖，测算价格、动态标题和接收方路径保持原有行为。
- 改动文件为分享图工具的 TS/JS 和分享图测试；小程序目录原本被 Git 忽略，本次源码差异保存在同目录 `share-card-layout-20261004.patch`，避免改变整个仓库跟踪规则。

验证：相关 14 项测试、项目 TypeScript 检查、编译 JS 语法检查均通过；生产依赖审计为 0 vulnerabilities，本次改动未检出硬编码凭据。微信开发者工具重新编译后显示 0 errors；根路径优先版本已通过实际绘图代码生成本地 JPEG 并打开模拟器图片预览，复测未再出现资源加载错误。实际预览证据为本任务产物 `share-card-integrated/devtools-share-preview.png`；这验证了小程序 Canvas 绘制、图片加载与文件导出，不等同于真实微信好友会话验收。聊天外框预览为本地模拟展示。

排版评审：金额与单位不重叠，文字左对齐，主体图片与背景无白框分隔，底部按钮清晰。仍需在更新版本后重新分享，确认真机聊天显示。

本次布局回滚可执行 `git apply --reverse docs/cost-estimation-upgrade-20261003/share-card-layout-20261004.patch`，已通过反向应用检查。

## 2026-10-04 分享卡比例与首页配色统一

- 别墅由 650 px 缩小至 570 px，整体右移、下移，补回顶部细金色边框，保留屋顶黄色灯带；左右、顶部和地面渐变衔接米金背景。
- 标题由 50 px 调为 44 px，金额最大由 148 px 调为 132 px，参数由 42/40 px 调为 36/34 px，并降低参数字重。
- 按钮取色自首页 `/assets/home/核心输入区.png` 中央 AI 金色徽章，渐变为 `#eaceb5`、`#d5a073`、`#cd8b55`，搭配深棕文字。按钮高度 116 px，文字 44 px。
- 仅修改分享绘图工具 TS/JS 和对应测试；动态标题、跳转路径及报价计算保持原有行为，没有新增依赖或通用抽象。

验证：先以新增测试复现图片比例未缩小，再完成修改；相关 15 项测试、项目 TypeScript 检查、JS 语法和空白检查通过。生产依赖审计显示 0 vulnerabilities，改动文件未检出硬编码凭据。完整源码差异经过自审，未发现新增错误处理、性能或无用代码问题。

微信开发者工具使用本次实际绘图代码成功加载图片、导出本地 JPEG 并打开模拟器预览，调试器 0 errors。截图为本任务产物 `share-card-refined/devtools-share-preview.png`。卡片上的金额、单位、参数与建筑没有重叠，顶部细金边可见，图片边缘自然衔接。`chat-card-preview.png` 的聊天外框为本地模拟展示；真实微信接收方卡片仍需更新版本后重新分享验收。

小程序目录原本被 Git 忽略，本次源码差异保存为 `share-card-refinement-20261004.patch`。反向应用检查通过，可用 `git apply --reverse docs/cost-estimation-upgrade-20261003/share-card-refinement-20261004.patch` 单独回滚本轮视觉调整。

## 回滚

删除 `costEstimateShareImage.ts/.js` 与图片资源，并将 `aicostresult.ts/.js` 的 `onShareAppMessage` 恢复为原有静态分享配置即可。
