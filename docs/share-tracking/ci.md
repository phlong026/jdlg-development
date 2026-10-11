# 分享访问分析的独立 CI 门禁

## 范围与安全边界

`.github/workflows/share-tracking-quality.yml` 补齐既有 `admin-quality.yml` 未执行的新后端与小程序跟踪检查。既有管理后台和模板合约工作流保持原样。

- 所有 PR 的 opened / synchronize / reopened / ready_for_review 运行；PR 不使用路径过滤，避免被设置为 required check 后因跳过而一直 pending。
- push 仅在本工作流、`service/**`、`miniprogram/**` 或 `docs/share-tracking/**` 变化时运行；覆盖后端依赖、路由、SDK、真实页面及检查脚本的变化。另支持手动运行。
- 两个独立 job：`Tracking backend tests and race checks`、`Tracking mini-program and route policy checks`。
- 权限仅为 `contents: read`；checkout 设置 `persist-credentials: false`。不使用业务凭据、secrets、部署步骤、PR 写回、生产数据库或微信账号。
- checkout、setup-node、setup-go 的完整 SHA 均复用仓库实际 `admin-quality.yml` 固定版本。Node.js 为 24.19.0；Go 为 1.27.2，兼容 `service/go.mod` 声明的 Go 1.25.0 及当前依赖。
- Node 聚焦测试只依赖内置模块与仓库源码，无需 `npm install`；Go 测试使用隔离的测试数据、内存实现、fake SQL 和 miniredis。
- 该文件只定义工作流；未修改仓库分支保护，也未将作业自动配置为 required checks。是否设为合并硬门禁由仓库维护者另行决定。

## CI 实际命令与本地复现

后端工作目录 `service`：

```sh
go test -count=1 ./tracking ./route
go test -race -count=1 ./tracking ./route
```

两次都禁用测试结果缓存，race 检查覆盖整个 tracking 和 route 包，不只匹配少量测试名。失败直接使对应 job 失败，没有 `continue-on-error` 或忽略退出码。

小程序工作目录 `miniprogram`：

```sh
npm run test:tracking
npm run check:tracking
npm run check:tracking-routes
```

- `test:tracking` 执行 SDK、真实公开回调、闲置与身份边界、告知同意、阅读入口竞态，以及路由检查器自身的正反例回归。
- `check:tracking` 只比对确定性 TS → JS 产物，不重写源码或生成文件。
- `check:tracking-routes` 从真实 app.json 的主包、分包提取全部 58 条路径；拒绝重复、不规范路径及未经更新的路由数量变化。前端 `ROUTES` 和 Go `routes.json` 必须与 app 路径逐项、大小写完全一致，不允许额外项或遗漏。
- 逐项校验前端 `collect` 与后端 `Public` 为显式布尔值且相同、`kind` 与 `Type` 相同、后端标题非空。公开页必须有受支持的就绪策略；私有页必须使用 excluded 类型、具体排除理由和对应就绪值。实际 runtime policy（含前导斜线形式）必须返回同一策略，未知路径必须关闭采集。
- 当前结果为 21 个公开源页面（含通用阅读页）和 37 个排除源页面。新增路由或调整隐私边界，需要审查后同步 app、两端策略、检查器基线及测试；不能靠忽略失败放行。

## 本地实际验证记录

2026-10-11，在 Node.js 24.19.0、Go 1.27.2、Linux amd64 下执行上述相同命令：

- 后端普通测试：`service/tracking`、`service/route` 通过。
- 后端 race：`service/tracking`、`service/route` 通过，未报告竞态。
- 聚焦小程序测试：145 项通过，0 失败，0 跳过；含检查器 1 项真实仓库正例和 17 项输入变异反例。
- TS/JS 同步检查通过；58 路由、21 公开 / 37 排除、前后端策略一致性通过。
- 工作流 YAML 可解析；静态核对触发器、只读权限、完整 action SHA 与既有工作流一致，无 secrets 引用或 `pull_request_target`。
- 受限本地环境的 Go 默认缓存目录不可写，因此仅将 `GOCACHE` / `GOMODCACHE` 指向现有可写缓存并将已安装 Go 加入 PATH；测试命令和包范围未改变。GitHub 托管 runner 使用正常默认缓存路径，无需这些本地覆盖。

以上是本地等价验证，不是 GitHub Actions 已运行或已通过的证据。应在推送后核对该提交对应的两个 job 结果。

## 旧全量失败独立披露

新增门禁没有运行或宣称旧小程序全量测试全绿，也没有修改、跳过或放宽其失败断言。单独运行以下命令仍会返回非零：

```sh
cd miniprogram
node --test tests/*.test.js
```

2026-10-11 当前结果：229 项，224 通过，2 失败，3 跳过。两项客服失败与新增本门禁前复测一致：

1. `customer-service-action-placement.test.js:22`：断言需求卡不应包含“对接设计老师”。
2. `customer-service-knowledge.test.js:49`：断言客服页应调用旧 `matchKnowledge`。

既有失败背景亦见 `miniprogram/docs/share-tracking-verification.md`。聚焦门禁成功不代表该债务已修复；不得将全量命令改为忽略失败来制造通过。微信真机、实际平台权限、部署环境与生产采集开启审批仍按验收文档独立完成，Node/Go 自动测试不能代替。
