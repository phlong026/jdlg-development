# 装修报价 v2 · 动态算量交付记录

2026-11-17。依据用户确认的 V2 效果稿实现，并按用户补充要求将固定示例量升级为空间动态算量。参考资料目录位于工作区父目录 `需求文档/装修报价新增/20261117-V2右上角切换与文案精简`。

## 功能与边界

- 建房表单右上角切换建房／装修报价；往返保留表单。
- 装修表单连续滚动，包含地区、新旧房、面积、可选面宽进深、层数层高、户型、分层、风格及三档标准。
- 装修结果包含三费环图、六类空间、工程分组及项目量价；支持查看／收起、调整工程量和计入项、参数、档次与附加费。
- 新增共用空间尺寸抽屉，区分参数估算与逐间尺寸输入。实际面积由各空间长×宽派生，墙面由周长×有效层高扣除门窗；高度按空间→测量楼层→分层设置→全屋继承。
- 手动工程量优先，支持恢复自动计算；尺寸、总面积、层高变化后重新计算，取消或保存失败不会丢输入。
- 沿用建房账户规则：表单公开浏览，生成、保存和私有结果需登录；登录后金额完整可见，无额外明细权限。
- 装修缓存使用 `renovationBudgetResult:<userID>`，不会覆盖建房缓存。账户切换重新检查归属。
- 点击「分享报告」先确认公开，再发布独立 v5 固定快照，30 天有效。公开报告只读，可展开明细；原生私有页转发仅分享空表单入口。
- [动态算量依据](dynamic-quantities.md)说明公式与估算配置；[历史v1清单](pricing-reference.md)保留参考单价、倍率及辅材／人工分布口径。房型权重只分配面积比例，不固定各空间工程量；估算配置属于本项目规则，未称为土巴兔内部公式。

原建房 `aicost.ts/.js`、`aicostresult.ts` 的 SHA-256 与基线 `c4df672` 一致。建房 WXML/JSON 仅增加右上角切换组件；v4 分享和既有计价、九折、校验、缓存保持兼容。

## 验证证据

| 检查 | 结果 |
| --- | --- |
| 小程序相关回归 | 637 通过、0 失败、0 跳过 |
| 动态手算基准 | 单卧室4×3m/H3，门1.8㎡窗2.25㎡：地顶12㎡、墙37.95㎡；工程10039、默认总价11445 |
| 历史兼容 | v1的97814元报告原价读取；显式重新计算才升级v2，旧快照不重算 |
| 跨端合约 | 同组实测/估算JS引擎→适配器夹具由Go验证，金额、单项公式、来源枚举及64KiB限额一致 |
| 原生 WXML 编译和条件渲染 | 新页面/组件及原建房、公共分享模板通过 |
| 后端 | `CGO_ENABLED=0 go test ./... -count=1` 全包通过；route/model/tracking `go vet` 通过 |
| 静态 | 修改 JS 语法、TS 转译、Sass、JSON、构建同步、diff 空白检查通过 |
| 路由清单 | 80 路由，24 公开、56 排除，前后端一致 |
| 安全 | 新增字段与金额校验、签名路径、账户隔离、请求体限额等回归通过；生产依赖 `npm audit --omit=dev` 为0漏洞；未新增依赖或凭据 |
| 独立审查 | P0/P1 为0，审查通过；分层数量上限提示等问题已修复并复核 |

装修功能交付当时的全量 TypeScript 检查有188项既有错误，未将该次全量检查标为通过。后续[类型检查修复](../miniprogram-typecheck-20261117.md)已将小程序类型诊断清零，并加入持续集成检查。

可重复运行（使用已有依赖，不必安装新包）：

```sh
cd miniprogram
export REPORT_TEST_NODE_MODULES=/absolute/existing/background/node_modules
export WXML_COMPILER=/Applications/wechatwebdevtools.app/Contents/Resources/app.asar.unpacked/node_modules/wcc-exec/wcc
npm run check:tracking
npm run check:tracking-routes
npm run test:renovation
```

637项合集还包括原有 tracking、nickname、profile/login、分享和 report-template 相关回归；`test:renovation` 是日常聚焦子集。

## 原生视觉证据及验收缺口

本轮动态算量原生证据：[尺寸编辑](evidence/dynamic-measurement-editor.png)、[改长宽后](evidence/dynamic-after-length-change.png)、[改层高后](evidence/dynamic-after-height-change.png)。同一12㎡样本从4×3m/H3改为5×3m/H3，总价11445→13209；再改H4，总面积保持15㎡，总价14071。账户与HTTP均为离线fixture，没有生产写入。

以下原版主布局截图保留作历史对照：

[报价总览](evidence/native-overview.png)、[费用展开](evidence/native-details.png)、[公开只读报告](evidence/native-public.png)来自微信开发者工具2.02.2609250、基础库2.32.3、iPhone17模拟器。计价及页面源码来自本分支，账户、登录组件及 HTTP 使用离线 fixture；没有真实账号或生产发布写入。

实际自动化点击验证附加费勾选后，税费2917、总价100731，原先未计税为97814。自动点击还触发基础库触摸事件异常：`WAWebview.js:702040` 的 `Array.from(e.touches)` 收到空值；异常栈未包含业务文件。无自动点击的公开页正常渲染且无错误。没有修改基础库或降低生产安全设置；该自动化环境边界与真机触摸仍需验收。

本轮新增尺寸抽屉、算量来源标识及恢复自动工程量；手算、原生重算交互和WXML编译已核对。尚未完成逐设备像素对比。详见[视觉判定](visual-verdict.json)。

离线原生预览：在仓库根目录执行 `REPORT_TEST_NODE_MODULES=/absolute/existing/background/node_modules node docs/renovation-budget/native-preview.cjs .`，生成 `/tmp/jdlg-renovation-ui-preview-20261117`，再用开发工具打开该目录。fixture 仅供 UI 核对，不可上传为开发版或正式版。

## 发布与回滚

当前仅本地实现与提交，未推送、合并、上传小程序、部署服务器或进行真实账号/真机验收。后续发布须先部署支持v5的后端，再上传本分支小程序；[接口说明](../renovation-budget-share-api-20261117.md)记录独立创建路由和64KiB限额，原v4仍保持16KiB。

本次无数据库迁移。代码提交可 `git revert <本功能提交>` 回退，未提交的其他工作不在本分支中。已经发布的v5链接依赖支持v5的后端，未来回滚后它们将不可用；保留旧v4快照，勿清空共享Redis存储。当前未创建生产v5快照。
