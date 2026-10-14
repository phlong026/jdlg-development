# Governance and maintenance boundaries

## CRM mapping verification

`makeTrackingCRMVerifier(db)` is the service's `VerifyCRM` callback. Every association rechecks the actor's enabled management access, explicit owner-specific tracking view/CRM grant, and active `management_crm_link`. It calls the existing signed CRM bridge `GET /customers/:id` as that linked CRM username and management user ID. It never substitutes the platform subject or expands CRM permissions. A success envelope, exact canonical customer ID, exact nonempty upstream customer number, and absence of a merged/deleted target are required. Missing connection, database errors, denied remote access, stale IDs, mismatched numbers, or malformed response fail closed. Raw CRM responses are not exposed in errors or logs.

This verifies an accessible existing CRM record, not that the recorded account belongs to a particular natural person. The operator remains responsible for the explicit association basis; `LinkCRM` retains its permission checks and audit trail. No cross-account merging is introduced. The remote endpoint contract is taken from `background/src/api/wecomCustomers.ts` (`Detail.customer`); integration against a live CRM remains a release gate.

## Retention is opt-in reviewed maintenance

`DefaultRetentionPolicy()` returns raw 90 days and daily-summary/support-record 180 days with destructive maintenance **disabled**. These are proposed defaults requiring business/privacy review. `PlanRetention` is a dry run; it requires the authenticated tenant/owner scope and both deletion and rules permissions. The caller supplies explicit UTC cutoffs no newer than the configured minimum retention periods. A record exactly on the cutoff is retained.

`PurgeExpired` is a local maintenance method, not an HTTP endpoint, startup task, migration, or scheduler. The operator must explicitly enable the policy and provide a reviewed plan. The opaque review digest binds actor, tenant, owner set, cutoffs, exact candidate records and payload versions. Any candidate change requires another dry run and review. Execution and count-only audit insert are one repository transaction. Errors roll back. This implementation has not executed any production deletion or database migration.

Raw retention covers event, event fingerprint, sequence, view binding, expired-era entry, and rate-counter kinds. The longer policy covers session, identity, confirmed business, CRM mapping, manual override, share, share-request, daily_summary and summary_state kinds. It intentionally does not remove deletion/deletion-session tombstones, audit rows, or published/revoked content. Unknown kinds are preserved until separately classified. Older live mappings age from their last stored update; review this behavior before adopting a policy. The preview includes per-kind counts and no visitor identities.

Manual overrides now retain complete immutable decisions in `intent_override_history`, including reason, expiry and actor. Current `intent_override` pointers, history and `intent_override_boundary` are visitor-scoped and covered by full-identity deletion and the reviewed support-record retention policy. Deleting expired decisions creates or advances a reason-free boundary for only affected identities; older snapshots return 409 rather than silently losing a decision. Boundaries themselves expire under support retention and are deleted with the identity. The owner-wide `intent_override_clock` stores only a revision counter, no visitor or free text, and is retained to prevent revision reuse. Permanent generic audit rows reference and hash the complete decision rather than copying its reason, so personal-history erasure does not leave an undeletable second copy of that reason. None of this enables cleanup or changes the default 90/180-day policy. See `intent-rules.md` for legacy import and pagination contracts.

## Daily projections and retained-history boundaries

`daily_summary` and `summary_state` now persist derived `visible-v1` observations. Acceptance and current-session identity association rebuild affected sessions within their transactions. Projections are keyed by Asia/Shanghai day, owner, original visitor, session, page view, content and version. A cross-midnight visit has its PV on the start day only; duration is split, and UV/session counts must be deduplicated across days. Rebuild is replacement, so retries and out-of-order arrivals do not increment totals twice. Session-wide interval unions avoid overlapping-page foreground double counting.

`PurgeExpired` calls `PrepareSummaryRetention` inside the reviewed deletion transaction before raw records are removed. It rebuilds and freezes affected sessions, including those crossing the raw cutoff. Queries use the retained session projection exclusively, rather than adding a partial raw session to its full summary. The summary cutoff also prevents a first-time legacy reconstruction from extending approved history. Daily buckets expire by their day-end timestamp; a partial-day cutoff conservatively retains the bucket, potentially for up to one extra day.

This is implemented persistence, but not anonymization. The projection omits raw action/video payloads, source event IDs and tokens; its scoped visitor/session/page-view keys and compact visibility ranges remain personal data. Owner filtering, identity resolution and individual deletion apply. `DeleteVisitor` includes both projection kinds; tombstones suppress queries and rebuilds from restored raw records. A surviving session record is never evidence of a complete retained visit.

Retained visits disclose `raw_events_expired`, and their raw drilldown returns HTTP 410. Once raw history expires, an `as_of` earlier than the frozen projection may be impossible to reproduce; that request returns 409 rather than inventing a snapshot. Frozen projections cannot be transparently reinterpreted under future metric versions without an approved migration. See `service/docs/TRACKING_DAILY_SUMMARIES.md` for the exact contract. The proposed 90/180-day policy still needs approval and live-database/restore validation; no cleanup has been enabled or run.

Backups, external authorized exports, caches outside this module and replicas require separately approved retention procedures. Restores must replay deletion tombstones before serving traffic. A fresh database without restored tombstones cannot safely enforce prior deletions. Audit and tombstone retention themselves require a separate approved governance policy; this helper never silently expires them.

The SQL adapter now pushes affected-session reads down through `SessionRecordLister`; other adapters may scan a tenant-kind set. The generic repository still serializes each tenant transaction and retention lists each eligible kind. This favors correctness for the initial implementation, not unbounded scale. Establish production row counts, query plans, lock time and maintenance windows on an authorized database before enablement; partitioned/batched retention needs its own transaction and race analysis.

## Operational diagnostics

`Diagnostics(ctx, scope)` returns only finite owner-scoped counts: accepted persisted events, malformed stored events, sampling gaps, inferred seeks, queue-loss flags, late arrivals, self visits and preview visits. Repeated flags in one event count once. Unknown labels, event payloads, IDs and free text never become counter names or output. Counters do not infer rejected or lost events that were never persisted, and they are not a substitute for ingress rate/error monitoring. No client PII logging is added.

## Intent evidence limits

The existing authenticated `POST /favorites` flow in `service/route/favorite.go` now calls `ConfirmFavorite` only after target validation, a nonnil `UserFavoriteModel.Add` success and any template-like synchronization complete without error. A nil model or failed business operation cannot manufacture this tracking success. The client supplies a restricted tracking token only from an enabled, consented active SDK context; the server requires that token's authenticated entry account and `u:` visitor match the signed-in favorite account. Anonymous, wrong-account, expired or unavailable analytics contexts fail closed for tracking.

The hook stores action, session and validated target type/ID as a server-side `business` fact. It deduplicates the account/type/target per UTC date, including repeated add/remove/add on that day. This is evidence that an add succeeded, not a guarantee the item is still favorited later, and not a measurement of purchase probability. It does not assert that the favored target is the page currently being read. The hook is asynchronous with a 500 ms context deadline; analytics errors do not change a completed favorite response. There is no durable outbox/retry guarantee, so timeout or process shutdown can lose this evidence. Do not silently retry the original favorite merely to improve analytics delivery.

The default `followup-v1` rule has a 30-day evaluation window; authorized owner-scoped rule versions may configure the implemented 1–90 day window as described in intent-rules.md. At least two observed sessions plus a confirmed favorite in that window can produce medium interest; repeat visits alone cannot. Anonymous or insufficient evidence stays unknown; valid signed-in observations otherwise start low. Existing high-interest logic accepts only trusted server-side consultation/appointment/quote success facts; that branch is not proof that every real business source is connected. Rule evidence, evaluation window and original-rule/manual-override metadata are returned separately. Manual overrides require dedicated rules permission, a reason and an expiry within 30 days. Arbitrary expressions and additional unsupported business-source adapters remain outside the implemented configuration contract.

The signed-in favorite-to-tracking binding and real MySQL/Redis business path still need integration testing. `TestVerifiedFavoriteEvidenceAndDuplicateCap` verifies the domain-level wrong-account rejection, same-day deduplication and medium-intent rule; it does not execute a live favorite database transaction or prove exactly-once delivery.

## Checks performed

Unit tests in `service/tracking/governance_test.go` exercise default-disabled purge, nonmutating preview, exact cutoffs, owner isolation, tombstone/audit/content preservation, reviewed-plan drift, policy/date/permission rejection, actor/scope binding and finite diagnostics. `aggregates_test.go` covers duplicates, late/out-of-order facts, midnight and exact-range clipping, retained-metric survival, legacy cutoff handling, post-expiry identity, deletion tombstones and honest snapshot errors. `review_test.go`, `service_test.go` and `route/share_tracking_test.go` cover capability separation, CRM evidence, deletion isolation, intent and HTTP restrictions.

The latest completed full Go check recorded 437 passed test/subtest results, 5 explicit integration skips and no failures; see `rollout-and-verification.md` for commands, version, log path and exclusions. The admin governance suite checks permission-specific controls, exact targets, dirty/pending guards, late responses, deliberate deletion confirmation and truthful result handling. No live database, CRM call, deployed schedule, backup expiry, production cleanup, load target or restore drill has been tested or run.

## 大租户安全预算

目标访客的CRM、人工修正和删除按确切身份及关联会话下推，不以缩小owner范围来绕过完整删除授权检查。读取旧记录时同时识别JSON中的entry/session/share依赖；任何失败整笔回滚。删除能在一个读取预算内完成时仍是单事务原子执行，任何失败整笔回滚。预算放不下（累计超过20,000条或32MiB）时改为分阶段删除，见下节；不会把未完成的删除报告为completed。全租户保留期计划/清理与诊断仍可能触发同一防护，需有界维护方案与真实环境验证；当前没有自动分批清理或后台作业承诺。

### 分阶段删除（超出单事务读取预算）

1. **先核验范围，不改任何数据**：分页只读遍历该访客及其已核实会话的全部记录，任何一行的归属不在授权范围内即返回403，此时没有写入任何墓碑，和原子路径一致。
2. **再写墓碑**：访客墓碑（标记进行中）和每个会话墓碑，会话墓碑记录所属访客。从此该访客的数据不可读取，该账号和这些会话不能再新增数据。
3. **分页删除**：每页一个短写事务，每个查询最多500行。类别顺序固定，事件及其去重、序号、绑定、分享回执最先删除，**身份关联最后删除**：它是匿名会话记录与账号之间的唯一连接。
4. **完成标记**：全部删完后把访客墓碑标记为完成并写审计。

接口 `POST /deletions` 每次请求最多处理60页，未做完时返回 `status:"in_progress"`，重复同一请求从中断处继续；只有 `completed` 才表示删除完成。续做时会话清单从会话墓碑恢复，不依赖已被删除的记录。已完成的删除再次请求与未知访客一样返回404，不写任何数据，无权限的调用方也不能借此得知某账号曾被删除。

已知边界：墓碑生效后才发现的越权关联行（例如关联表被人工改过）会在删除中途返回403，这时数据已不可读但删除未完成，须人工处理后重试；分阶段路径只在核验之后遇到这种异常。事件、去重、序号等辅助记录与事件同页删除，不在第一步逐行核验归属。

## PR3 评审修复：关联匿名分享的删除边界

新分享保存服务端 entry 派生的 session 归属。删除已登录访客时，同时清除该账号已明确关联的匿名 session 的 share 与 share_request，停止解析这些旧令牌；同一匿名标识下未关联的其他会话不扩大删除。旧分享缺少 session 时，只使用已接收且不冲突的 share_intent 事实恢复确切关联，不按整个设备匿名 ID 猜测身份。无法证明关联的旧记录不能冒称已属于目标账号；保留最小化与到期治理仍须按批准政策执行。
