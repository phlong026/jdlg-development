# 三期内容导入任务持久化实现

## 交付范围

- `service/contentimport/store.go`：租户隔离的 SQL 任务仓储。
- `service/contentimport/store_test.go`：基于 `database/sql/driver` 的无外部依赖契约测试。
- `service/sql/phase3_content_import_up.sql`：默认关闭、需人工执行的建表迁移。
- `service/sql/phase3_content_import_down.sql`：带数据销毁警告的回滚脚本。

本实现不创建生产表、不启动 worker、不调用解析供应商、不访问真实媒体文件，也不包含任何凭据。

## 状态与付费边界

1. `Submit` 以 `tenant + actor + idempotency_key` 幂等；规范化 URL、平台、owner、rights、目标小程序 AppID 共同参与语义哈希。同 key 不同语义返回冲突，避免重复扣取解析费用。
2. 每个 actor 最多保留 20 个 `queued/resolving/downloading` 任务；提交和人工重试都执行该限制。
3. `Claim` 只在短事务中选择并写租约：
   - `queued` 或“尚未发出请求”的过期 `resolving` 转为 `resolving`；
   - 有解析快照且租约为空/过期的 `downloading` 直接恢复下载，不再解析；
   - 已经发出请求但未保存解析结果的过期 `resolving` 转为 `uncertain`，禁止自动重发付费请求。
4. `BeginResolve` 必须在供应商 HTTP 请求之前落库 `request_sent=true`。
5. `SaveResolution` 只接受有效租约，最多保存 2 MiB 内部 JSON 快照并转为 `downloading`。
6. `Finish` 只接受 `ready/failed/blocked/uncertain`；成功时清理临时解析快照，失败/阻断时保留已有快照供下载恢复。
7. `Retry` 仅允许原 actor 操作：有快照时回到下载队列；无快照时仅未发出请求或明确的配置/登录类错误可回到解析队列；`uncertain` 永远拒绝自动重试。

## 隔离与数据暴露

- 构造器绑定单一 tenant，所有 SQL 都带 tenant 条件；提交时再次校验请求 tenant。
- `Get` 只能由创建任务的 actor 查询；`List` 同时限制 actor 和调用方已授权的 owner 集合，分页上限为 50。
- HTTP 可返回的 `Job` 元数据会清空 tenant、actor、rights、幂等 key、租约、`request_sent` 与 `Document`。
- `Document` 的 JSON 标签仍为 `json:"-"`；临时上游 URL 不会通过任务响应暴露。
- 日志只记录仓储操作名和错误类型，不记录源 URL、Cookie、token 或供应商原始错误文本。
- 终态错误只接受最多 64 字符的小写错误码，不能写入原始错误或凭据。

## 迁移与回滚

- 迁移只创建 `content_import_jobs`，不包含运行期开关或自动执行逻辑。
- 领取索引为 `(tenant_id, status, lease_until, created_at, import_id)`。
- down 脚本会删除全部导入任务和解析证据；生产回退应优先关闭入口与 worker 并保留表，只能在空表或确认备份后执行 down。

## 验证证据

- 定向测试覆盖：跨租户拒绝、actor 隔离、owner 列表边界、幂等语义冲突、待处理数量限制、过期解析转 uncertain、领取租约、下载快照恢复、失效租约拒写、uncertain 禁止重试、公开 JSON 脱敏、只读 schema 探测。
- 使用命令：`go test contentimport/types.go contentimport/links.go contentimport/store.go contentimport/store_test.go`。
- 静态检查：`go vet contentimport/types.go contentimport/links.go contentimport/store.go contentimport/store_test.go`。
- 包级竞态检查：`go test -race ./contentimport`。
- 服务全量测试：普通 `go test ./...` 被本机 `github.com/shoenig/go-m1cpu` 的 cgo 初始化段错误阻断；使用不依赖该本机探测库的 `CGO_ENABLED=0 go test ./...` 后全量通过。
- 依赖漏洞工具：本机未安装 `govulncheck`，因此未执行依赖漏洞扫描；改动未引入新依赖。
- 未验证：真实 MySQL 上两个 worker 的并发锁竞争、迁移实际执行、生产数据量下查询计划；这些必须在隔离测试库完成后才能启用功能开关。
