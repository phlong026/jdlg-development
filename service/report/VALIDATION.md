# V5 本地交付验证 · 2026-09-20

代码分支：`feat/report-orchestrator-v5-20260920`。实现范围为报告服务端模块、接口合同、异步 worker 与 PDF 渲染；未部署、未发送客户消息、未发起真实支付。

| 检查 | 结果 |
|---|---|
| 全仓 Go 测试，启用新旧真实 Python PDF 集成 | 304 项通过（包含子测试），0 失败，3 项 MySQL 集成测试因无测试库跳过 |
| `CGO_ENABLED=0 go vet ./...` | 通过 |
| 独立 worker 构建 | `go build -o /tmp/report-worker-v5 ./cmd/report_worker` 通过（CGO 关闭） |
| Python 新旧渲染测试 | 33 项全部通过 |
| Go 格式检查、`git diff --check` | 通过 |
| 修改文件的密钥模式扫描 | 未发现实际密钥/私钥；不等同于完整安全审计 |
| 依赖文件 | go.mod/go.sum 未变；环境无 govulncheck，未执行依赖漏洞扫描 |
| 原报告草稿保护 | 复制基线中的 17 个源文件 SHA-256 均未改变 |

Go 测试实际执行环境：`REPORT_V5_REAL_PDF_TEST=1`、`REPORT_PDF_PYTHON=/usr/bin/python3`、`REPORT_PDF_FONT=/System/Library/Fonts/Supplemental/Arial Unicode.ttf`。现有 go-m1cpu 在本机 CGO 初始化异常，所以测试与构建使用 `CGO_ENABLED=0`。

完整集成测试通过创建任务、五角色主编排、用户确认、测试支付授权、实际 Go→Python 子进程渲染、就绪状态和下载归属校验。模型、数据库和存储使用测试替身；真实 PDF 经过正式渲染器验证。另有 HTTP 提供商响应格式、重定向拒绝、截断拒绝、失败恢复、缓存污染、过期租约、确认版本和 COS 回读内容损坏测试。

四种配方均验证四页预览及十页完整报告。长 UUID/64 位编号和页面溢出已有回归覆盖。空间样稿为十页单一 PDF，每页恰好一张完整图片；封面、事实页、确认优先级、需求任务书和行动清单已目视核对。

跳过的 MySQL 测试：`TestDispatchPendingPersistsAttemptAndPreservesSettledDownstream`、`TestCleanupRetainsPendingAndFailedEvents`、`TestWecomIdentityMySQLFlow`。新增 SQL 事务和租约逻辑通过数据库驱动替身验证，尚未在真实 MySQL 上执行迁移、并发压测或进程故障恢复演练。

上线验收仍需：真实模型内容质量与费用、MySQL 迁移与并发、私有 COS、微信商品/付款查单、独立小程序源码接入。企微已有欢迎领取入口，尚无报告完成后自动主动推送实现，需先核实可用接口和权限。
