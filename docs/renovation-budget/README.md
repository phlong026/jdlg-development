# 装修报价 v1 · 本地交付记录

2026-11-17。依据用户确认的 V2「右上角切换与文案精简」效果稿及土巴兔分项资料实现。参考资料目录位于工作区父目录 `需求文档/装修报价新增/20261117-V2右上角切换与文案精简`。

## 功能与边界

- 建房表单右上角切换建房／装修报价；往返保留表单。
- 装修表单连续滚动，包含地区、新旧房、面积、可选面宽进深、层数层高、户型、分层、风格及三档标准。
- 装修结果包含三费环图、六类空间、工程分组及项目量价；支持查看／收起、调整工程量和计入项、参数、档次与附加费。
- 沿用建房账户规则：表单公开浏览，生成、保存和私有结果需登录；登录后金额完整可见，无额外明细权限。
- 装修缓存使用 `renovationBudgetResult:<userID>`，不会覆盖建房缓存。账户切换重新检查归属。
- 点击「分享报告」先确认公开，再发布独立 v5 固定快照，30 天有效。公开报告只读，可展开明细；原生私有页转发仅分享空表单入口。
- [计价依据与局限](pricing-reference.md)包括固定参考量、倍率及辅材／人工分布口径；不是土巴兔实时接口或施工合同报价。

原建房 `aicost.ts/.js`、`aicostresult.ts` 的 SHA-256 与基线 `c4df672` 一致。建房 WXML/JSON 仅增加右上角切换组件；v4 分享和既有计价、九折、校验、缓存保持兼容。

## 验证证据

| 检查 | 结果 |
| --- | --- |
| 小程序相关回归 | 609 通过、0 失败、0 跳过 |
| 装修参考基准 | 100㎡标准新房：工程85802、主材44792、辅材12058、人工28952；默认总额97814 |
| 原生 WXML 编译和条件渲染 | 新页面/组件及原建房、公共分享模板通过 |
| 后端 | `CGO_ENABLED=0 go test ./... -count=1` 全包通过；route/model/tracking `go vet` 通过 |
| 静态 | 修改 JS 语法、TS 转译、Sass、JSON、构建同步、diff 空白检查通过 |
| 路由清单 | 80 路由，24 公开、56 排除，前后端一致 |
| 安全 | 新增字段与金额校验、签名路径、账户隔离、请求体限额等回归通过；生产依赖 `npm audit --omit=dev` 为0漏洞；未新增依赖或凭据 |
| 独立审查 | P0/P1/P2 均为0；最后分层数量上限提示已修复并复核 |

项目全量 TypeScript 检查仍有188项既有错误；本次相关文件没有新增语义错误。未将全量项目 typecheck 标为通过。

可重复运行（使用已有依赖，不必安装新包）：

```sh
cd miniprogram
export REPORT_TEST_NODE_MODULES=/absolute/existing/background/node_modules
export WXML_COMPILER=/Applications/wechatwebdevtools.app/Contents/Resources/app.asar.unpacked/node_modules/wcc-exec/wcc
npm run check:tracking
npm run check:tracking-routes
npm run test:renovation
```

609项合集还包括原有 tracking、nickname、profile/login、分享和 report-template 相关回归；`test:renovation` 是日常聚焦子集。

## 原生视觉证据及验收缺口

[报价总览](evidence/native-overview.png)、[费用展开](evidence/native-details.png)、[公开只读报告](evidence/native-public.png)来自微信开发者工具2.02.2609250、基础库2.32.3、iPhone17模拟器。计价及页面源码来自本分支，账户、登录组件及 HTTP 使用离线 fixture；没有真实账号或生产发布写入。

实际自动化点击验证附加费勾选后，税费2917、总价100731，原先未计税为97814。自动点击还触发基础库触摸事件异常：`WAWebview.js:702040` 的 `Array.from(e.touches)` 收到空值；异常栈未包含业务文件。无自动点击的公开页正常渲染且无错误。没有修改基础库或降低生产安全设置；该自动化环境边界与真机触摸仍需验收。

截图后增加结果栏档次选择器与分层数量提示，均经行为测试和 WXML 编译验证；尚未完成逐设备像素对比。详见[视觉判定](visual-verdict.json)。

离线原生预览：在仓库根目录执行 `REPORT_TEST_NODE_MODULES=/absolute/existing/background/node_modules node docs/renovation-budget/native-preview.cjs .`，生成 `/tmp/jdlg-renovation-ui-preview-20261117`，再用开发工具打开该目录。fixture 仅供 UI 核对，不可上传为开发版或正式版。

## 发布与回滚

当前仅本地实现与提交，未推送、合并、上传小程序、部署服务器或进行真实账号/真机验收。后续发布须先部署支持v5的后端，再上传本分支小程序；[接口说明](../renovation-budget-share-api-20261117.md)记录独立创建路由和64KiB限额，原v4仍保持16KiB。

本次无数据库迁移。代码提交可 `git revert <本功能提交>` 回退，未提交的其他工作不在本分支中。已经发布的v5链接依赖支持v5的后端，未来回滚后它们将不可用；保留旧v4快照，勿清空共享Redis存储。当前未创建生产v5快照。
