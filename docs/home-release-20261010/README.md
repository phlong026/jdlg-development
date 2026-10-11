# 首页开发版本与回退记录 · 2026-10-10

已通过微信开发者工具官方 CLI，将当前首页上传为小程序开发版本 **7.2.9**，AppID 为 `wx94fdc48861913ede`。命令返回 `✔ upload`、退出码 0，上传结果见 [日志](wechat-upload.log) 和 [包体统计](upload-info.json)。本次状态是后台开发版本已上传，尚未提交审核或发布到线上用户。

首页采用确认的别墅庭院品牌底图，调整 logo、品牌文字与 slogan 的位置，保留暖米色、香槟金与灰绿色主色。八个分类、原有“拍照说需求”AI 胶囊、智能造价表单、客服和导航的内容、交互与布局保持原有行为。装饰背景不接收点击，原生右上角胶囊区域保留；微信模拟器中已查看实际加载效果。

首页图片与自定义底部导航图标共 **29 个资源、4,121,760 字节**，沿用现有腾讯 COS 与 CDN：

- 桶：`jiadilingguangcos-1393500756`，地域：`ap-chongqing`。
- 域名：`https://static.jiadilingguang.com`。
- 本次独立目录：`assets/home-releases/20261010-b52afc7c12b5/`。
- 原始云资源未覆盖或删除；新目录使用固定版本 URL，防止以后改图影响本版。
- 每个资源已通过公共 CDN 下载，并核对 SHA-256 与原图一致，见 [资源清单](asset-manifest.json) 与 [校验记录](cdn-verification.json)。

代码用简单的 `homeAssets` 映射复用现有 `resolveAssetPath`，没有新增依赖。`project.config.json` 新增两条打包排除规则，排除已迁移的 `assets/home-reference` 与 `assets/smart-service-icon.png`；本地原图仍保留，`app.json` 使用的五张底部导航图片也保留。迁移后主包为 **1,462,218 字节，约 1.4 MB**，总包为 **2,556,590 字节，约 2.4 MB**，官方预览与最终上传的统计一致。

## 旧首页与回退

旧首页的代码和图片已保留在 [完整备份](旧首页完整备份-20261010.zip)，用于回退的原文件另存于 `before/`。旧首页基线提交为 `fb043f9eeae8e70e23b363601a97b1b1be32a085`；新版首页代码提交为 `a823ced62e7544a67dcf4bf8a06f91d042aa95ef`。这些提交保存在本地分支 `feat/home-header-cloud-release-20261010`，未推送 Git 远端。

回退脚本默认只检查，不写入。下面两条命令依次用于检查与实际恢复：

```sh
node "/Users/honor.pei/Documents/订单项目/甲地灵光二期/开发版916/JDLG/docs/home-release-20261010/rollback.cjs"
node "/Users/honor.pei/Documents/订单项目/甲地灵光二期/开发版916/JDLG/docs/home-release-20261010/rollback.cjs" --apply
```

脚本会先核对所有文件的本版 SHA-256；若发现后来又修改过的文件，会停止且不写入任何文件，避免覆盖后续工作。它恢复九个原文件、移除三个本次新增文件，保留云端资源。已在独立副本验证：默认检查不写入、实际恢复与旧文件哈希一致、后续编辑触发保护且零写入，见 [回退验证](rollback-verification.json)。也已验证新版提交的 Git 反向补丁可干净恢复，见 [Git 回退验证](git-rollback-check.json)。

**本地回退不会改变小程序后台。** 需要在恢复后重新编译预览，并用新的开发版本号上传旧首页；经审核发布的线上版本还需按微信后台发布流程处理。本次没有执行任何回退。

## 验证与已知缺口

首页相关七个测试文件全部通过，覆盖分类导航、原有表单校验、地区与面积传递、客服位置、背景及云资源映射、底部导航选择，见 [首页测试](home-tests.json)。新测试已先出现失败再随实现通过。修改的 JavaScript 语法、TypeScript 语法检查均通过，新云资源映射及其导入的资源 helper 的 TypeScript 类型检查通过；SCSS 与 WXSS 同步。官方 DevTools 预览编译与代码上传均成功。生产依赖漏洞扫描为零，修改文件未发现密钥，没有新增依赖。

全套测试执行 29 个文件，其中 27 个通过。`customer-service-action-placement.test.js` 与 `customer-service-knowledge.test.js` 两个客服测试失败，已经在恢复旧首页的独立副本复现，属于本次变更前已存在的测试/客服实现差异；本次没有扩展修改客服页面。详见 [全套结果](full-test-results.json) 和 [原版本失败复核](preexisting-test-failures.json)。因此本记录不声称全套测试通过。

本次已完成开发者工具中的预览、资源加载与视觉核对，尚未完成真实手机验收，尚未提交审核或发布。上传后的后台记录与精确资源、提交、备份哈希统一保存在 [发布记录](release-record.json)。
