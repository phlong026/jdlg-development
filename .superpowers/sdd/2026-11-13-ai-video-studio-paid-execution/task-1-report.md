# Task 1 · MySQL 权威钱包与工作台计费事务

已完成本地实现与隔离 MySQL 8.4 验证；未推送、未上传、未部署、未请求真实付费供应商。

## 接口与约束

- `InitExecutionTables()` 在 `InitTables()` 后调用：增加 quotes.billing_unit 与 quote_consumptions 唯一消费表。旧 quote 缺少 billing_unit 时拒绝扣费，须重新报价。
- `CreatePaidBatch(ctx,userID,quoteID,key) (batchID,replayed,error)`：先锁 users，检查同用户同 key 的 quote 一致性；重放优先于时效和配置。新创建验证 owned 未过期 quote、草稿 revision、当前开关/模式/唯一启用档位、ready owned 媒体。冻结 quote 的 billing_unit/unit_stones/billable_units/total，配置改价与改计费单位不改变旧 quote；停用档位阻止新执行。
- 同一事务扣余额、消费 quote、建 batch/tasks、逐项 reserve 费用快照及真实 consume stone_records。per_request 整数均摊，余数分给前几项，允许零额子项，总额必须为正。
- `FinalizeTask(ctx,taskID,StudioTaskOutcome) error`：completed（succeeded 别名）/failed/cancelled；LeaseOwner 非空时检查有效租约。取消只允许 queued。成功不再次扣费并创建唯一 settle 事实；明确失败/取消按原 reserve 退款及流水。uncertain 不可自动退款。批次聚合 completed/partial_success/failed/cancelled/uncertain。
- completed URL 先带租约条件独立持久化，再事务结算；费用失败 DIRECT 结果保留。已有结果禁止失败/取消覆盖。worker 重启必须优先按持久化 result_url 恢复 completed 结算，不能再提交供应商。
- `CancelQueued(ctx,userID,batchID)`：逐条原子取消仍为 queued 的 owned 子项，提交中/未知/已完成不退。

## 共享钱包最小修复

保留 UserRedisModel 与 Get/Deduct/Add/Set 签名，余额只读写 MySQL，无 nil DB Redis fallback、异步回写、启动覆盖 Redis。增减使用原子条件更新；拒绝负数/int64溢出；旧免费工具零额操作校验 MySQL 用户存在后 no-op。Set 可设零。

管理员 Set/Adjust 通过 SetStonesWithRecord / AdjustStonesWithRecord 同事务写余额与流水，删除旧失败补偿，避免撤销并发正常消费。禁用旧合并用户 CLI（启动即失败，在任何迁移之前），避免 Redis 合并破坏权威；未扩为新迁移工具。

## 验证证据

先新增 TestWalletNeverFallsBackToRedis，旧实现失败 `missing database must fail closed`；执行测试在实现前报缺失 API 编译失败。实现后：

- 隔离 loopback MySQL 8.4：`CGO_ENABLED=0 go test ./model -run 'TestStudioPaidAtomicLifecycle|TestWallet' -count=1` 通过。
- `CGO_ENABLED=0 go test ./model ./route ./cmd/merge_duplicate_users -count=1` 通过。
- `CGO_ENABLED=0 go vet ./model ./route ./cmd/merge_duplicate_users` 通过。
- `git diff --check` 通过；变更凭据检查未发现真实密钥/令牌；govulncheck 未安装，未新增依赖。

用例覆盖同key replay/错quote、同quote换key、冻结价格与计费单位、停用档位、per_request 1/3分摊、部分成功/排队取消、重复终态/重启结算、trigger注入预留流水失败全回滚、结算失败DIRECT URL保留、10同key创建与10旧钱包消费并发、10独立报价争1余额仅1成功、余额不足无quote消费、管理Set/Adjust流水失败回滚、后续合法消费不被补偿覆盖、零额旧工具、负数/溢出。

## 集成与边界

Task2 接启动迁移、worker租约、持久化结果恢复；本任务未改供应商提交与轮询。旧充值/邀请/签到业务和余额间已有事务缺口未扩展改造。

发布须停所有旧钱包写入进程/后台任务，保留最终Redis与MySQL对账快照，受控停写窗口核对余额再切权威，禁止新旧版本混跑。代码逆向补丁可回退，迁移/外部资金效果不能只靠git revert，上线回滚属于messy。优先回退到保留MySQL钱包的兼容版本，禁止直接启动旧Redis钱包二进制。
