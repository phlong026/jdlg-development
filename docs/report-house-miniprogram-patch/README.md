# 整屋报告小程序接入补丁

实际源码已应用到用户确认的 `开发版916/JDLG/miniprogram`。该目录是主仓忽略的复制源码，本补丁仅保存 9 个报告页面源文件和 9 个相关测试/测试运行辅助文件，不包含账号、项目配置或签名密钥。未新建 Git、未强制纳入整个小程序、未提交代码。

## 行为

- `GET /api/v1/miniprogram/reports/config` 的 `house_enabled=true` 时使用整屋表单，读取 `/house/questions` 核对协议版本，并向 `/house` 提交。未启用时保留旧报告输入与提交接口。
- 实际五步表单收集常住/返乡人数与频率、宅基地与边界、层数/卧室、长辈房、风格、庭院、停车、兼用、空间名称/数量/楼层/必须满足、按重要性排序的需求，以及预算总额和逐项包含范围。
- 新协议固定四页预览；完整图文报告按内容展开，不承诺固定十页或十四页。确认展示来自服务端冻结 facts 的原始需求，包含必须保留的房间和相邻建筑条件。
- 确认请求携带 `diagnosis_id`、`input_hash`、`choice` 和 `priorities: []`；空优先级数组由服务端保留冻结输入中的有序优先级，不从展示文案拆分重建。
- 重新读取服务端 `confirmed`、`can_pay` 与 `quote.can_pay` 后才允许新协议购买。仍调用原有虚拟支付工具；本轮未执行真实支付。
- 付费且制作完成的 House/V5 报告可直接领取；旧报告仍按原企业微信绑定规则处理。私有 PDF 请求签名包含原始 query，与后端 `URL.Path + '?' + RawQuery` 一致。
- 新协议在页面可见时持续轮询，前 12 次间隔 5 秒，之后 15 秒；隐藏/卸载或终态停止。暂时断网自动退避恢复，首次读取最多恢复 3 次，401/403/404 不循环请求。
- 生图提交结果不确定时显示核对说明并禁止重试生成。确认中离开页面、重新返回与旧响应迟到均不会卡住或覆盖新操作。

## 文件与回退

`manifest.json` 列出 18 个交付文件的 SHA-256 和原始文件 SHA-256；`changes.patch` 是相对小程序根目录的零上下文完整差异；先核对 manifest 中的原始 SHA，再用 `patch -p1` 应用，或用 `git apply --unidiff-zero`。每个交付文件都与实际源码字节一致。

原始 15 个文件已在修改前保存到相邻 `report-house-miniprogram-baseline/`，该目录保持不变。需回退时，将其 manifest 中列出的原文件逐一还原，并移除本次新增的三个测试文件 `report-runtime.js`、`report-api.test.js`、`report-template.test.js`。回退前先对照当前 SHA，避免覆盖后续编辑。

## 验证

在实际小程序根目录执行：

```sh
node ../background/node_modules/typescript/bin/tsc --noEmit --pretty false
node --test tests/report-*.test.js
```

结果：完整项目 TypeScript 类型检查 0 错误；44 项报告相关测试全部通过，0 跳过。测试包含三个由微信原生 WXML 编译器生成函数并在 VM 中实际渲染页面的用例；另对 5 个报告 Sass 文件编译成功。新增源文件使用现有 TypeScript 格式化工具整理，补丁扫描未发现硬编码密钥或尾部空白。未修改依赖，无新增依赖安装。

测试默认查找现有 `../background/node_modules`，也兼容原 `../AItestPlatform/node_modules`。其他环境可设置 `REPORT_TEST_NODE_MODULES` 指向已有 TypeScript/Sass。原生模板测试通过 `WXML_COMPILER` 指定已有 wcc；本机使用微信开发者工具自带路径，没有该编译器的环境会明确跳过这三个原生模板测试。

本轮验证不等同于微信真机或线上验收：未登录微信开发者工具、未调用线上报告/生图接口、未支付、未发送企业微信消息，也未上传发布小程序。后端上线与账号权限应在实际联调环境中验证。
