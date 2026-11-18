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

### 后端受保护发布构建

仓库中的 `service/function/crypto.go` 保留占位值，不能直接用普通 `go build` 作为发布包。本地受保护版本应保持仅当前用户可读，并在发布时显式传入：

```sh
chmod 600 /absolute/protected/crypto.go
service/scripts/build-protected-release.sh \
  --crypto-file /absolute/protected/crypto.go \
  --output-dir /absolute/output/jdlg-service-release
```

构建脚本只从 `git archive HEAD` 创建临时源码树，仅在临时目录注入受保护的 `crypto.go`，然后强制执行小程序 JavaScript 签名器到 Go 鉴权中间件的契约测试再构建。最终 ELF 保留 Go 符号，脚本会从加载段中读取实际 HMAC/SM4 slice 并与本次注入源文件比较，两项不一致均禁止输出。默认拒绝其他工作副本变更、未跟踪文件和其他 `skip-worktree` / `assume-unchanged` 文件。

输出目录权限为 `0700`，仅包含权限 `0700` 的后端可执行文件和权限 `0600` 的独立 `auth-build.json`；清单记录 Git commit、tree、Go 版本、受保护源文件摘要、二进制摘要和两项 ELF 匹配布尔值，不包含密钥源码，也不覆盖现有迁移发布器使用的 `manifest.json` / `files[]`。

## 代码验证

先安装后台锁定依赖，再运行包含开发依赖的安全审计、后台回归与生产构建：

```sh
cd background
npm ci --ignore-scripts --no-audit --no-fund
npm audit --audit-level=high
npm test
npm run build
```

管理台构建链暂时通过 npm override 固定使用 `@ryotsu/picomatch@4.0.7-a`，覆盖 Vite、Sass 等开发依赖中的 `picomatch`，处理 [GHSA-fc5q-39xc-vjpf](https://github.com/advisories/GHSA-fc5q-39xc-vjpf)。该版本由第三方维护，锁文件固定下载地址及完整性校验。上游发布修复版本后移除 override，并重新运行完整依赖审计、后台回归与构建。

小程序全量测试会复用后台锁定的 TypeScript 和 Sass，无需微信凭据：

```sh
cd ../miniprogram
npm run typecheck
node --test tests/*.test.js
npm run check:tracking
npm run check:tracking-routes
```

`npm run typecheck` 按小程序现有严格配置执行完整 TypeScript 语义检查，CI同步执行。`npm run build:tracking` 与 `npm run check:tracking` 负责生成和核对 JS；生成产物检查与类型检查分别执行。两者与鉴权签名构建复用后台锁定的编译运行时，无新增依赖。执行前沿用上面的后台锁定安装步骤；上传使用提交中的 JS 产物。修复范围及验证记录见[小程序类型检查修复](docs/miniprogram-typecheck-20261117.md)。

需要本机微信 WXML 编译器的用例在缺少编译器时会明确跳过，可通过 `WXML_COMPILER` 指定编译器后执行。CI 的 `Admin quality` 同时执行全量小程序回归及 `npm audit --audit-level=high`，发现高危或严重依赖告警时失败；原有追踪专项与 Go 检查继续执行。

客服知识问答同时通过 Go 接口回归验证实际回复、分类、引导动作和命中优先级，随现有 Go 测试及竞态检查执行。

`Admin quality` 也使用托管运行器已有的 JDK 17 和 Maven 执行 `customer-management/phase1` 的 `mvn -B test`，持续保护身份分类、CRM 同步及人工确认阶段。

## 三期内容、分享和通知

首批实现的接口、默认关闭开关、迁移/回退及未完成项见[三期接入说明](docs/phase3/README.md)。本地构建不代表生产部署或服务号真实验收。

设计师独立 AI 视频的页面、后台价格配置、素材与草稿接口、验证和未开放项见[2026-11-13 实现说明](docs/phase3/ai-video-studio-20261113/README.md)。新模块的付费生成仍受账务与执行就绪门禁保护。

工作台链路逐项修复及服务号开通依赖见[修复计划](docs/phase3/workbench-chain-fixes-20261113/PLAN.md)和[服务号配置门禁](docs/phase3/workbench-chain-fixes-20261113/official-account.md)。服务号独立配置项见`service/env.example`，缺少真实账号和模板时保持关闭。

客户跟踪逐次访问列表的页面、统计口径、本人权限、验证和回滚说明见[实施说明](docs/phase3/customer-visit-feed-20261114/README.md)。

剩余分支统一主线、媒体自适应和本地证据见[本轮实施记录](docs/phase3/unify-main-20261113/implementation-status.md)。

## 装修报价（2026-11-17，本地实现）

新增独立装修预算，复用建房预算的登录规则。入口在建房表单右上角，原建房计价、账户缓存和 v4 分享保持兼容。

- [功能、校验与回滚](docs/renovation-budget/README.md)
- [动态算量公式与估算配置](docs/renovation-budget/dynamic-quantities.md)
- [历史v1参考价格清单](docs/renovation-budget/pricing-reference.md)
- [v5 分享接口](docs/renovation-budget-share-api-20261117.md)

使用既有依赖运行 `cd miniprogram && npm run check:tracking && npm run check:tracking-routes && npm run test:renovation`。无本地依赖时，可将 `REPORT_TEST_NODE_MODULES` 指向已有后台依赖目录；不需要为此新增依赖。
