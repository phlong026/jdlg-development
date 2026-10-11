# Tracking query safety bounds and predicate pushdown

Status: implemented and covered by in-process endpoint, repository, and SQL-driver tests. No live MySQL execution plan, million-row production-like load, concurrent throughput, latency SLO, or deployed index migration has been verified. These limits are a fail-closed safety envelope, not evidence that production performance targets are met.

## Hard budgets

- Each query transaction shares a cumulative budget of 20,000 records read and 32 MiB of raw JSON payload. Primary facts, candidate reads, repeated/enrichment reads, and successful point lookups all count; these are not UI page-size limits or counts of distinct visitors.
- SQLStore enforces the same cumulative transaction budget, including callers outside the query endpoints. Each list uses a parameterized `LIMIT remaining_records + 1`. Reading the extra row fails the operation; it never produces partial totals or silently cuts off a page.
- A database-side `CASE` avoids transferring a single JSON payload larger than 32 MiB. Cumulative payload size is checked during row iteration. Returned driver buffers are copied before subsequent rows can overwrite them.
- A budget failure is sticky. An optional metadata lookup cannot swallow it and allow a successful result or committed transaction. The caller receives HTTP 422 with “查询范围过大，请缩小时间或归属、素材、访客范围”. Narrow the authorized owner, time, content, or visitor selection as supported by that endpoint. Increasing UI page size does not change this budget.
- JSON decoding, maps, intervals, and response objects have additional allocation overhead. The 32 MiB figure is an input-payload budget, not a measured or guaranteed 32 MiB process-RSS limit. A statement may still examine more database rows than it returns; the budget does not prove a database CPU, sorting, or elapsed-time bound.

## Read interface and pushed predicates

`ScopedRecordLister.ListScoped(kind, RecordQuery)` is optional for small in-memory repositories; SQLStore implements it. Only the server constructs RecordQuery. Every runtime value, including tenant, owner, identity, dates, object IDs, and limit, is a SQL parameter. Nil selector slices impose no restriction; a non-nil empty slice selects nothing.

RecordQuery supports owners, record IDs, session IDs, visitor candidates, share IDs, content, page-view ID, inclusive start/through timestamps, exclusive end timestamps, and an identity snapshot time. SQL always restricts the server-selected tenant and record kind. Query endpoints always add their authorized owner intersection before fetching JSON.

- Visitor/content queries first find candidate events in the requested owner/time/object range, then fetch complete events for those authorized sessions up to as_of. The 15-second tail beyond End matches the existing accepted visible-segment maximum. Earlier page_view events and other content in the same session remain available for interval-overlap attribution.
- Session queries first restrict the requested visitor and optional material. Related-content expansion is limited to the resulting session IDs, rather than all sessions of that visitor.
- Event queries push the page-view ID but still fetch its complete session before deriving visibility. The final response contains only the requested page view and in-window events.
- Content cards and legacy material catalogs are owner scoped; a single-content request also narrows material facts. Visitor-only filters do not affect the content view.
- Intent calculation still uses its defined 30-day evidence window, but narrows reads to authorized owners and the current candidate visitors. Business, CRM, and override reads are bounded and scoped.
- Identity, deletion-session, visitor-deletion, and summary-state metadata are fetched for relevant sessions/identities instead of listing their entire tenant-wide kinds.

Legacy entry/session, identity, share, view-binding, and share-request relationship fields are selected from their authoritative JSON paths when their denormalized columns are blank. Identity joins use that effective session identifier. Daily-summary visitor candidates include original identity, snapshot-linked identity, and stored projected identity; query endpoints still perform exact snapshot identity resolution before returning facts. These selectors also support targeted privacy operations without assuming that JSON-only legacy dependencies have indexed relationship columns.

## Semantics retained

- A page view beginning before Start can contribute visible time inside the window without counting a new PV. Segments ending just after End are clipped rather than lost.
- Full-session interval deduplication preserves attribution across content and page views; content filters are not applied prematurely to the deduplication inputs.
- Daily-summary reads push authorized owner and overlapping Asia/Shanghai business-day buckets. Cross-midnight ranges remain clipped exactly in UTC milliseconds.
- Received-at and occurred-at are checked against as_of after loading the selected facts. Late receipt cannot change an earlier snapshot. Identity links established after that snapshot do not retroactively identify anonymous facts.
- For every raw candidate session, summary_state is checked even when no retained bucket intersects the window. A raw-expired session never falls back to a misleading partial set of residual raw rows.
- Frozen expired summaries return HTTP 409 if their built_at is later than the requested historical receipt snapshot. Raw event detail for a retained expired view returns HTTP 410. Neither case substitutes invented raw events or a supposedly reproducible historical snapshot.
- SQL DATETIME(6) bounds are conservatively rounded for candidate selection; exact event timestamps and clipping are checked by the existing fact calculations.

## Indexes and deployment

The initial schema `service/sql/share_tracking_up.sql` includes the existing tenant/kind/owner/time indexes plus:

- tracking_session_time: tenant_id, kind, session_id, occurred_at, record_id
- tracking_scope_content_time: tenant_id, kind, owner_id, content_id, occurred_at

Existing installations have an additive, re-runnable upgrade script at `service/sql/share_tracking_bounded_queries_up.sql`. It checks information_schema before adding either index. The application does not execute this script. A database operator must review disk use, online-DDL support, and possible index-build locking before applying it.

The existing per-tenant transactional lock is unchanged. It preserves acceptance, identity/deletion, and projection consistency, but remains a concurrency bottleneck requiring real profiling. Legacy JSON predicates, identity EXISTS clauses, ordering, and their index choices need EXPLAIN and representative MySQL measurement; the current tests do not establish their production plans.

## Maintenance limits and remaining verification

Tenant-wide retention planning/cleanup and rebuild callers also inherit SQLStore's cumulative read budget. A large all-kind maintenance plan or very large session rebuild can fail with 422 and roll back; no partial cleanup is reported as successful. Retention still needs an explicitly reviewed bounded/batched operational plan for large tenants. This patch does not introduce a background job framework, resumable cleanup protocol, or unreviewed bulk-delete bypass. Targeted privacy selectors do not authorize narrowing away the account's other owner scopes; deletion must continue checking its full relevant owner coverage before mutation.

Before production sign-off, measure MySQL plans and memory under representative million-row distributions, candidate/session fan-out, cross-owner identity histories, large retained summaries, late arrivals, concurrent ingestion/reads, deletion, and retention. Verify 422 handling preserves the user's filters and offers a useful narrower query. No production data migration, deletion, deployment, load generator, or index build was run as part of these local tests.
