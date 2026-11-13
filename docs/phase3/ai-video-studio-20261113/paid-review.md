# AI 视频生成与灵石链路最终独立审查

## 最终裁决

| 门禁 | 裁决 |
| --- | --- |
| Spec compliance | **PASS**：本轮实现范围内的付费批次、持久执行、结果与退款、客户端恢复符合所审计划；真实供应商和 UAT 不在通过范围 |
| Code quality | **PASS**：未发现未解决的阻断或非阻断代码问题 |
| 阻断项 | P0 = 0，P1 = 0 |
| 非阻断项 | 本轮发现 P2 = 1，已修复并限定复核关闭；当前未解决 P2 = 0 |

原始审查范围为 `a4e0011..4b06db1`，共 4 个提交，依据 `review-a4e0011..4b06db1.diff`、实施计划、Task 1/2 报告及前两次独立门禁。随后按主代理授权，仅复核本报告 P2 对应的三个工作区文件差异。报告编写时 HEAD 仍为 `4b06db140eb2f73f95f2abdb43179f2294fd1432`，P2 修复尚未包含在该提交中；最终裁决要求交付时包含这些修复。

仓库为 `/Users/honor.pei/Documents/订单项目/甲地灵光二期/开发版916/JDLG`，分支 `feat/designer-ai-video-implementation-20261113`，remote 为 `https://github.com/phlong026/jdlg-development.git`。未改业务源码、未使用共享 MySQL、未访问生产或真实供应商、未递归委派。工作区其他未提交文件未自动纳入审查；`main.go` / `user.go` 注释日志及配置文档由主代理最终集成检查。

## P2：后台完成状态筛选与实际执行器不一致（已关闭）

- 原位置：`service/route/ai_video_studio.go:910-913`（4b06db1）；关联 `service/model/ai_video_studio_execution.go:317-320`、`background/src/pages/aiVideoStudio.tsx:248`。
- 确定触发路径：执行器全部完成写入 `completed`，部分完成写入 `partial_success`；后台已有“已完成”筛选发送 `status=completed`，但路由白名单仍是旧的 `succeeded/partial_failed`，所以在数据库查询前直接返回 400。全部列表可见的真实成功任务无法按成功状态筛选。UI 的 `reconciliation_required/retain_failed` 选项也没有对应批次状态。
- 修复：路由提取 `validStudioBatchStatus`，接受实际 worker/client 状态；管理页面使用真实排队、生成中、待确认、完成、部分完成、失败、取消选项，移除无真实来源的两个筛选。
- 限定复核文件：`service/route/ai_video_studio.go`、`service/route/ai_video_studio_validation_test.go`、`background/src/pages/aiVideoStudio.tsx`。
- 独立验证：`TestAIVideoStudioManagementAcceptsWorkerBatchStates` PASS；管理台定向 4 项 PASS；三个文件 `git diff --check` PASS。逐项比对页面选项与路由白名单，确认真实调用入口使用新校验器。主代理提供了该回归先失败、后通过的执行证据，本审查未再次制造失败版本。

## 跨层审查结果

| 关注点 | 结论与证据 |
| --- | --- |
| MySQL 共享余额与旧调用兼容 | PASS：`user_redis.go` 保留既有接口，条件 UPDATE 防余额不足与 int64 上溢，零额验证用户存在；不再使用 Redis 余额回填。旧调用仍经同一数据库更新；管理员 Set/Adjust 与流水同事务，删除失败后的反向补偿。旧合并 CLI 的 Redis 写入口已禁用。原充值/签到等完整业务幂等不被误称为本次重写完成。 |
| 扣费原子性与报价单次消费 | PASS：`CreatePaidBatch` 先锁钱包，再按 user/key 查重，校验 quote 一致性；quote_consumptions 主键防换 key 二次消费。余额、批次、任务、reserve、stone_records 在同一事务，任何失败回滚。 |
| 冻结旧价与运行时关闭 | PASS：按 quote 中 billing_unit、单价、计费单位和总价重算核验；当前价格变化不改变旧报价费用。当前运营开关、模式和唯一启用档位仍检查；同 key 已有批次可在关闭/报价过期后恢复读取。 |
| 素材与真实 builder | PASS：报价读取 owned ready 媒资并核对类型、真实时长；扣费事务再次锁定所有引用素材核验归属及 ready。builder 从冻结快照构造请求，768p/2k 转供应商大写档位，参考模式使用 reference_prompt，引用使用数据库时长与 HTTPS 授权签名地址，再调用同一原生 schema 校验。未知/失效素材不会送出付费请求。 |
| 能力与计价目录 | PASS：readiness 由表初始化及 worker 注册决定，实际启用再要求独立凭据、运营开关和有效价格。capabilities 只返回有启用正价的档位与时长；图像/混合素材分别检查存储、签名和探测依赖。供应商配置信息只表示配置存在，不表示真实账号已验证。 |
| 分摊、部分成功与重复终态 | PASS：每条 reserve 固定分摊，整数余数按顺序分配，合计等于报价；成功不二扣，失败按原 reserve 退回，零额不伪造正额流水。终态 no-op 在已清空租约检查之前，合法重复回执与相同成功结果重试不再受 MySQL changed rows 误判。 |
| 结果先持久与失败恢复 | PASS：成功结果先带租约条件持久化；费用事务失败仍保留 DIRECT URL/时长；worker 优先从持久结果恢复结算。已有成功证据拒绝失败/取消覆盖。取消只允许 queued，提交竞争由任务行状态收敛。 |
| 认领、并发与重启 | PASS：配置行锁序列化全进程 admission，活跃和未知任务计入容量；claim 持久 queued→submitting 后才允许外部请求。每次 owner 唯一，写回/终态校验有效租约；网络绝对 deadline 锚定 lease_until，Submit 前复核上下文与数据库 owner/status。 |
| 未知提交与超时 | PASS：过期 submitting 无 ID 进入 uncertain，不能再次 Submit；外部 ID 已知只 Query；HTTP/超时/未知响应不自动退款。总生成超时保留预留并继续查证，运营关闭不阻断已付费任务查询。未知无 ID 占用并发是明确保守策略，并非丢失队列。 |
| 外部 ID 唯一绑定 | PASS：worker 接收回执和管理员补 ID 都持有相同配置锁，并查全表是否已绑定其他任务。重复 ID 不能拿另一个任务的结果替本任务结算；冲突保持待确认。 |
| 管理核对权限与资金边界 | PASS：路由在 AuthRequired + ManagementAccessRequired 管理组内，CRM-only 被现有权限策略排除；actor 来自认证上下文。补 ID 先向供应商查询，退款只允许 uncertain、无外部 ID、无成片、无有效竞争租约，证据必填；不能人工提供结果 URL。 |
| 审计与退款重试 | PASS：审计与外部绑定/退款租约在同一事务，退款另走统一终态资金事务。若审计后退款失败，租约到期后可用相同动作重试，审计 task/action 唯一、退款费用事实唯一，不会重复退钱。这里是人工核对动作的幂等恢复，不宣称后台自动重放所有人工退款。 |
| 前端持久 key 与迟到回包 | PASS：发送前按账号持久化 quote/key；未知结果保留且阻止新报价付费。清理同时比较 quote_id/key，旧 A 成功/拒绝或只读查询回包不能删除新 B 的 pending。 |
| 自动恢复和显式恢复 | PASS：重进页面调用只读 GET `/submissions/:idempotencyKey`，404 不落单；仅用户明确“按原请求恢复”才 POST 原 quote/key。服务端按登录账号查询，不能跨账号恢复。 |
| 费用详情、播放与留存 | PASS：GetBatch 能读 NULL URL，返回实际 reserve 状态汇总、逐项结果、can_cancel；外部 ID 仅后台返回。详情离页停轮询、load epoch 防旧响应、终态停轮询且保留播放地址。未留存成片播放/留存按已知外部 ID 刷新临时 URL；已有稳定媒体走本地授权读取，不再依赖供应商查询。 |

## 独立运行证据

对 `4b06db1` 使用 `git archive` 建立隔离只读源码快照 `/tmp/jdlg-final-review.zP3eoY`，避免主代理在途改动混入最初审查：

- `env -u AI_VIDEO_STUDIO_TEST_DSN CGO_ENABLED=0 go test ./model ./route ./aivideostudio ./cmd/merge_duplicate_users -count=1`：PASS。MySQL 专用测试按约定跳过；没有连接或写入数据库。
- `CGO_ENABLED=0 go vet ./model ./route ./aivideostudio ./cmd/merge_duplicate_users`：PASS。
- 小程序 `ai-video-studio.test.js`、`ai-video-studio-contract.test.js`、`ai-video-studio-editor.test.js`：40 passed，0 failed。
- 管理台 `background/scripts/ai-video-studio-admin.test.mjs`：4 passed，0 failed。第一次与小程序合跑时，归档没有 node_modules 导致管理台模块加载失败；将临时快照链接到本机已有后台依赖后，单独完整复跑该测试文件通过，未安装/更改依赖。
- `git diff --check a4e0011..4b06db1`：PASS。
- P2 工作区限定重审：`env -u AI_VIDEO_STUDIO_TEST_DSN CGO_ENABLED=0 go test ./route -run TestAIVideoStudioManagementAcceptsWorkerBatchStates -count=1` PASS；管理台定向 4 项 PASS；限定 diff whitespace 检查 PASS。

Task 1/2 隔离 MySQL 故障注入与并发测试、全量小程序和后台构建结果属于作者/主代理证据，本审查已阅读测试及报告但未重跑数据库夹具。最终全量 Go、静态检查、依赖扫描及交付提交由主代理汇总，不应把本文定向验证替代那些门禁。

## 未执行的验收与交付边界

没有执行真实秘塔付费提交、生产钱包扣费/退款、供应商访问私有签名素材、真机播放/相册保存或角色 UAT；独立凭据仍由主代理确认就绪，不能把本地适配器测试当作真实 provider 通过。没有 remote push、上传、部署或发布。

共享钱包从 Redis 切换为 MySQL 的线上停写、差异对账、迁移和回退仍是发布前置工作。代码通过不允许新旧钱包写入进程混跑；已有真实扣费/退款/媒体事实不能靠删表或简单 revert 撤销。此限制已有交付文档描述，不构成本次本地代码审查的未修缺陷。
