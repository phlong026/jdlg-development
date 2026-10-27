# 甲地灵光二期开发源码

本仓库保存 2026-10-11 当前开发源码的独立快照，包含：

- `service/`：Go 后端。
- `background/`：React 管理后台。
- `miniprogram/`：微信小程序完整源码及测试。
- `customer-management/`：Java 客户管理服务、阶段实现和 PC/H5 前端。
- `docs/`：开发说明、集成示例和验证材料。

来源目录：`开发版916/JDLG`；主仓库来源提交：`920526debca32f42b483f900a06133aa27e803c8`。客户管理模块和原先被 Git 忽略的小程序文件按当前工作目录完整纳入。旧仓库历史和本地 Git 元数据未导入。

依赖目录、编译产物、本地 `.env`、证书、私钥、日志、缓存及备份压缩包不入库。各模块的依赖版本由现有清单与锁文件保留，开发与部署请参考模块说明。

## 本地配置

上传副本已将 `service/function/crypto.go` 的默认 SM2/SM4/HMAC 密钥、`customer-management/src/main/java/cn/iyque/utils/JwtUtils.java` 的 JWT 签名密钥，小程序 `utils/parameter.js` 的默认协议密钥、客户管理移动端的固定访问令牌，以及客户管理 `application.yml` 中的密钥和密码替换为占位值或环境变量引用。原始本地开发目录未修改。

这些占位值不能用于生产。克隆后需在受保护的本地配置中提供项目所需凭据；代码快照上传不表示系统已部署或完成业务验收。

## 代码验证

先安装后台锁定依赖，再运行包含开发依赖的安全审计、后台回归与生产构建：

```sh
cd background
npm ci --ignore-scripts --no-audit --no-fund
npm audit --audit-level=high
npm test
npm run build
```

小程序全量测试会复用后台锁定的 TypeScript 和 Sass，无需微信凭据：

```sh
cd ../miniprogram
node --test tests/*.test.js
npm run check:tracking
npm run check:tracking-routes
```

全量测试保留三项需要本机微信 WXML 编译器的用例；缺少编译器时会明确跳过，可通过 `WXML_COMPILER` 指定编译器后执行。CI 的 `Admin quality` 同时执行全量小程序回归及 `npm audit --audit-level=high`，发现高危或严重依赖告警时失败；原有追踪专项与 Go 检查继续执行。

客服知识问答同时通过 Go 接口回归验证实际回复、分类、引导动作和命中优先级，随现有 Go 测试及竞态检查执行。

## 三期内容、分享和通知

首批实现的接口、默认关闭开关、迁移/回退及未完成项见[三期接入说明](docs/phase3/README.md)。本地构建不代表生产部署或服务号真实验收。
