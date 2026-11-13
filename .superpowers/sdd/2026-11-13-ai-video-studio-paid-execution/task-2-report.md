# Task 2 · 持久执行器与服务接口

本地实现与隔离 MySQL 8.4 行为验证完成。未调用真实付费供应商、未推送/上传/部署。

## 实现与接口

- 后台 worker 随 RegisterRoutes 启动，stop 取消并等待退出。只有业务表、执行表、审计表、素材表成功初始化且 worker 已注册，store.Ready 才开启；缺密钥/关闭运营/未开价仍禁止新生成，历史与已支付查询不受运营关闭影响。
- 配置行锁 + 全库活跃任务数负责跨进程 admission；claim 持久 queued→submitting 后 commit 才做外部网络调用。每次 claim 使用唯一 owner，180 秒租约覆盖最多 95 秒调用，绝对网络截止时间锚定数据库 lease_until-85s，Submit 前复核上下文与 owner/status/有效租约，写回/终态带租约检查。
- submitting 租约到期无 externalID 进入 uncertain；超时/HTTP错误/未知响应永不重发。已知 ID 仅 Query，生成总超时保留 uncertain 并继续查证；轮询按尝试次数 5–60 秒退避。未知无 ID 占并发容量，等待运营核对，避免无界供应商在途任务。
- Submit 收到重复 externalID 时进入 uncertain；worker 与管理附加标识通过同配置锁序列化查重。所有终态调用 Task1 FinalizeTask，先恢复已持久 DIRECT 结果再处理费用，不经过旧的直接终态 UPDATE；删除无人调用的旧恢复/写终态方法。
- 真实 builder 将持久草稿 768p/2k 转为供应商 768P/2K，并复用 provider 本地 schema 校验；输入素材按 owned ready 数据生成 HTTPS 签名 URL，有效期两小时；引用音视频使用实际媒资时长。DIRECT 播放/留存按已知外部 ID 刷新供应商 URL，已有留存走本地，不把临时 URL 当永久地址。
- POST `/batches` 接收 quote_id/idempotency_key；先查同 key 重放，再执行 readiness/当前配置校验；余额不足402、冲突409、配置/存储不可用503。POST `/batches/:id/cancel` 只退款仍 queued 子项，返回重读批次。
- GET `/submissions/:idempotencyKey` 只按 user/key 读取已存在任务，key<=128；不存在404，绝不创建任务。用于丢失回包后的无副作用恢复。
- GetBatch 支持 NULL result_url，charge 返回 quoted_stones/total_stones/reserved_stones/settled_stones/released_stones/fee_status、can_cancel；external_id 仅管理 userID=0 读取时下发，避免查询 N+1。
- 管理 POST `/results/:id/reconcile` 支持 attach_external_id/refund_confirmed_rejected/query。附加 ID 先 Query 供应商证据；退款只接受 uncertain 且无 externalID/成片，证据10–500字，审计来自认证 actor ID/name。唯一 task/action 审计在费用处理前持久化，失败可原价幂等重试；有外部 ID 禁止人工退款。没有人工填写成片 URL 的入口。

## 测试与证据

先写 TestStudioWorkerRequiresDurableStore，旧代码失败 undefined StudioWorker；实现后通过。独立复查发现分辨率大小写边界后新增真实 builder→provider 回归，修前失败“清晰度不支持”，规范化后通过。

- `CGO_ENABLED=0 go test ./model ./route ./aivideostudio -count=1` 通过。
- `CGO_ENABLED=0 go vet ./model ./route ./aivideostudio` 通过。
- 隔离 loopback MySQL schema `jdlg_studio_test_worker`：`AI_VIDEO_STUDIO_TEST_DSN=... CGO_ENABLED=0 go test ./model -run TestStudioWorker -count=1` 通过。覆盖双 worker 一次提交、过期租约首次派发0提交、DIRECT重启结算保留7.5秒时长、重启已知 ID 仅 Query、无 ID uncertain 不重发、过期 submitting 不重发、超时不退款继续 Query、运营关闭仍完成、cancel/claim 竞争、重复管理退款唯一费用事实、审计已写入后注入流水失败仍可恢复退款、外部 ID 不重复绑定、有 ID 禁人工退款、owned GET 恢复无建单、NULL 成片读取。
- 原生四模式/未知响应/4xx保守语义由 `aivideostudio` 现有协议测试验证；部分成功/原价退款和费用注入失败由 Task1 MySQL 用例覆盖。
- 最初使用 Task1 同 schema 时其它 active 测试记录合法占满全局并发导致用例未执行目标任务；改独立 worker schema 后通过。未修改真实业务库。
- 未新增依赖；govulncheck 未安装。diff 检查、凭据扫描、自审只覆盖本任务提交范围。

## 限制与回退

供应商原生幂等、取消、HTTP拒绝不受理语义尚无官方保证，保守待核对规则保留；未做真实秘塔账号联调/费用验收、真实媒体公网拉取与真机验收。管理 query 会触发下一轮 worker 查询，不直接写人工结果。

代码可 git revert；本任务新增审计表可保留。已有付费/退款是资金事实，不能通过删表或旧二进制回滚；使用 Task1 保留 MySQL 权威钱包的兼容回退方案。
