# 小程序类型检查修复（2026-11-17）

## 范围与基线

在 `8ef33d3` 上执行原有 `miniprogram/tsconfig.json` 的全量 `tsc --noEmit`，共188条诊断：126条微信系统接口声明缺失、43条隐式参数类型、19条阅读页异常/异步返回值/画布/变量作用域等诊断。这里的检查范围是小程序，不包含 Go 后端或管理后台。

本次保持严格检查配置、不增加类型忽略或依赖；沿用后台锁定的 TypeScript。原建房、装修计价、登录、支付实现和后端接口保持原有行为。

## 修复计划

1. 根据微信官方声明补齐三个系统接口及准确返回字段，在本地兼容声明集中处理。
2. 补全阅读页参数、异常、异步返回值和画布类型；避免同名脚本变量冲突。
3. 视频计时沿用现有单调计时能力检查；环境不提供该能力时不以系统时间替代，不影响业务播放。
4. 增加 `npm run typecheck` 与 CI 检查，并以正常/错误类型夹具验证其执行语义检查、保留严格配置且不输出 JS。
5. 运行全量类型检查、源码/生成产物一致性与相关业务回归，独立审查后本地提交。

## 类型依据

本地兼容接口按官方 `miniprogram-api-typings@5.3.1` 的 `WindowInfo`、`DeviceInfo`、`AppBaseInfo`、`AppBaseInfoHost` 字段定义补齐，移除旧 `AppBaseInfo` 的任意字段类型，保留其余声明与依赖锁定版本。三个系统接口最低基础库为2.20.1，现有运行时能力判断与旧接口回退保持原样：

- [wx.getWindowInfo](https://developers.weixin.qq.com/miniprogram/dev/api/base/system/wx.getWindowInfo.html)
- [wx.getDeviceInfo](https://developers.weixin.qq.com/miniprogram/dev/api/base/system/wx.getDeviceInfo.html)
- [wx.getAppBaseInfo](https://developers.weixin.qq.com/miniprogram/dev/api/base/system/wx.getAppBaseInfo.html)

小程序官方 [Performance](https://developers.weixin.qq.com/miniprogram/dev/api/base/performance/Performance.html) 没有保证 `now()`；部分宿主的能力通过运行时检查识别，不给官方类型强加该方法，也不替换成用户可调整的 `Date.now()`。Canvas异步查询沿用本地已有 `WechatMiniprogram.Canvas` 类型。

## 本地命令

```sh
cd miniprogram
npm run typecheck
npm run check:tracking
npm run check:tracking-routes
node --test tests/*.test.js
```

依赖复用仓库 `background/node_modules`。隔离工作树可通过 `REPORT_TEST_NODE_MODULES` 指向已有后台依赖目录，无需新增依赖；WXML测试可通过 `WXML_COMPILER` 指定已有微信编译器。

类型检查遵守现有项目配置；既有文件中的类型忽略不在本次扩大清理范围内。通过表示当前配置内无诊断，不表示已经完成真实设备或生产业务验收。

## 验证与交付

| 检查 | 结果 |
| --- | --- |
| 全量 TypeScript | 188 → 0，原严格配置保持不变 |
| 小程序全量回归 | 1835通过、0失败、0跳过，使用本机WXML编译器 |
| 聚焦阅读回归 | 83通过，包含在全量结果中 |
| 类型门禁夹具 | 5通过：拒绝错误赋值/隐式参数类型/错误微信接口调用，正确类型通过，禁止生成JS |
| 构建同步与策略 | `check:tracking`、`check:auth-signer`、`check:tracking-routes`通过；80路由、24公开、56排除 |
| 安全与静态 | 修改JS语法、JSON、YAML、diff检查通过；`npm audit --audit-level=high`全依赖0漏洞，无新增依赖或凭据 |
| 独立审查 | APPROVE，P0/P1/P2均为0 |

修复补充了真实异常回归：分享和海报请求拒绝null、字符串、普通对象及标准Error时均能给出提示并解锁；401或媒体身份改变仍清除旧内容。视频缺少单调计时能力时回调不中断、不生成虚假分析事件。原建房、装修计价、登录、支付、后端与后台实现文件相对基线没有变化。

当前仅本地实现和提交，未推送、合并、上传小程序或部署；远程CI、真实账号和真机行为未验收。无数据库迁移，本次提交可单独回退。
