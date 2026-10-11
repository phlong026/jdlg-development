# 首页安全区与高清视觉调整 · 2026-10-11

本轮只调整已确认的首页内容。修改位于本地分支 `fix/home-safe-area-layout-20261011`，基线为 `f7caa200750ccd313265bd72d5b2d6cacc15eec5`。官方预览编译成功；本轮没有再次上传开发版本、提交审核或发布。上一轮后台开发版本仍为 7.2.9。

## 已完成的修改

- Logo、中英文品牌文字和 slogan 同步放大 20%，同步调整 slogan 字间距与两侧短线；品牌区域整体下移，读取手机状态栏高度，在状态栏下至少留出 12px；旧基础库使用 68px 默认安全位置。品牌装饰不接收点击，右上角微信原生胶囊保持可见。
- 别墅底图叠加暖米色透明层，保留原图细节；品牌区域增加局部暖米色渐变，降低 Logo、品牌名和 slogan 后方杂乱线条的干扰；遮罩最高不透明度为76%，范围缩小，向右侧和下方连续淡出，保留别墅窗框、外墙和庭院细节；没有使用模糊滤镜，也没有降低 Logo、图标或文字的透明度。
- 八宫格从左至右、从上至下调整为：模板广场、AI工具、别墅设计、装修设计；庭院设计、旧居翻新、造价测算、建房顾问。原图标、链接与事件处理保持对应。
- 报价标题改为“10秒估算建房报价”，项目标签改为“别墅工程”“室内装修”。底部提示改为“点击按钮视为同意《隐私协议》”，协议链接打开已有隐私政策页。
- 原有“拍照说需求 · 1分钟出方案”胶囊、表单字段、估算按钮、案例、弹窗、客服与底部导航保持原有结构和行为。

修改文件为 `miniprogram/miniprogram/pages/index/index.wxml`、`index.scss`、`index.wxss`、`index.ts`、`index.js`；更新已有 `miniprogram/tests/home-cloud-header-release.test.js`，增加 `home-safe-area-layout.test.js`。沿用现有导航处理与云资源映射，没有增加依赖。

“室内装修”本轮是首页项目标签调整，仍沿用原有共用造价入口和查询参数，没有新增独立室内装修计价模型。协议提示与链接不新增后台同意记录，也没有改变现有微信隐私授权流程。

## 高清素材与云存储

首页与底部导航的 29 个图片资源继续使用原版本 COS/CDN 固定地址 `assets/home-releases/20261010-b52afc7c12b5/`。本轮未覆盖云端图片或压缩原图，所有原始文件 SHA-256 与上轮清单一致，见 [图片质量记录](asset-quality.json)。

八宫格每张图标为 384×384；品牌别墅图为 1837×856，AI 胶囊底图为 1598×494。高清图在实际小程序中加载，并以原有米色、金色与绿色风格显示。

## 对比与验证

查看 [修改前后对比](首页修改对比.html)、[大屏模拟器](微信模拟器-修改后-iPhone15ProMax.png)、[小屏模拟器](微信模拟器-修改后-iPhone8.png) 和 [真机预览二维码](预览二维码.png)。对比页只是展示原始截图的窗口，不改动截图像素。

- 首页相关 8 个测试文件全部通过，包含 30 组状态栏高度/屏宽组合、八宫格顺序与路由、隐私链接、原 AI 胶囊与受保护样式不变，见 [相关测试](target-test-results.json)。新增测试先失败再随实现通过。
- 全套 30 个测试文件中 28 个通过。两个客服测试 `customer-service-action-placement.test.js`、`customer-service-knowledge.test.js` 失败，已在原基线独立副本复现，见 [全套结果](full-test-results.json) 与 [旧版复核](preexisting-test-failures.json)。本轮未扩展修改客服页面。
- JavaScript 语法、首页及其导入模块 TypeScript 类型检查、SCSS/WXSS 对应规则检查通过；项目未配置独立 lint/formatter 命令，使用原有格式与 `git diff --check` 检查，见 [静态检查](static-security-check.json)。
- 生产依赖漏洞扫描为零，修改的源码与回退脚本未发现密钥，见 [依赖扫描](dependency-audit.json)。
- 官方微信 CLI 预览成功，主包 1,463,558 字节，总包 2,557,930 字节，见 [编译日志](wechat-preview.log) 与 [包体记录](preview-info.json)。

原生模拟器已检查 iPhone 15 Pro Max（430px）与 iPhone 8（375px）的首页布局，并点击查看已有隐私政策页。机型切换后需重新编译，避免开发者工具保留旧机型的比例。调试中出现过开发者工具页面栈冲突和未改动页面的 TDesign 字体缓存错误，具体最终状态见 [原生调试记录](native-debug-verification.json)。最终首页大屏控制台 Errors: 0、Warnings: 4；小屏 Errors: 0、Warnings: 3。尚未完成真实手机验收。

## 旧版保留与回退

修改前代码与全部原图保存在 [完整备份](首页7.2.9-修改前完整备份.zip)，原文件还保存在 `before/`。默认回退命令只核对哈希并显示计划：

```sh
node "/Users/honor.pei/Documents/订单项目/甲地灵光二期/开发版916/JDLG/docs/home-refinement-20261011/rollback.cjs"
```

确认需要恢复本轮修改前的首页时执行：

```sh
node "/Users/honor.pei/Documents/订单项目/甲地灵光二期/开发版916/JDLG/docs/home-refinement-20261011/rollback.cjs" --apply
```

脚本先验证所有当前文件与备份的 SHA-256，恢复六个原文件并移除本轮新增测试。后续文件若再次改动，脚本会停止且不写入，避免覆盖新工作。已在独立副本验证预演、精确恢复和后续编辑保护，见 [回退验证](rollback-verification.json)。本地当前版本未执行回退；云端资源保留。

本地回退不改变微信后台。需要后台使用旧首页时，恢复后重新编译并上传新的开发版本。
