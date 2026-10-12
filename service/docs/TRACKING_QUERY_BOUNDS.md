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

## Public media authorization bounds

Anonymous media reads no longer list the tenant's complete `version` kind. SQLStore first reads the requested asset metadata, then executes a parameterized `SELECT EXISTS` that checks published-version references to that exact media ID (including cover references). Only one Boolean is returned; matching or unrelated article bodies are not transferred into Go. The scalar read still consumes the existing cumulative read budget. The 20,000-record and 32 MiB limits are unchanged.

The existence query restricts the server-selected tenant and asset owner, checks both record-owner and JSON-owner consistency, joins the current master in that same tenant/owner, requires a published version and non-revoked master, and excludes a current `content_revoked` tombstone. A historical published version can authorize its media while a newer draft is being edited, but withdrawal blocks every old version. Management media access retains its existing explicit scope checks.

This is a compatibility SQL lookup over existing version records; historical publications need no reverse-reference backfill to keep serving media. It bounds returned data, not database CPU or examined rows. JSON reference matching may scan many versions within the owner and still holds the current tenant transaction lock. Before production sign-off, profile actual MySQL plans and concurrent image/video-range traffic; an indexed reverse-reference migration may be needed for database latency. No such migration or live-load claim is included here.

The regression uses production Service.Media, SQLStore, and database/sql with a deterministic driver, including 560 same-owner published versions with ordinary 60,000-byte unrelated bodies and 20,001 unrelated versions. The previous implementation exceeded the byte/row budgets; the new path transfers one 178-byte asset record plus a Boolean in this fixture. Separate contract cases cover withdrawn masters, withdrawal tombstones, owner mismatches, historical published references, and covers. These are SQL-boundary and domain tests, not execution of MySQL's JSON functions or production performance measurements.

## Catalog membership and content pagination

New own-content rows and owner-scoped legacy-catalog rows persist server-assigned immutable `created_at`. Draft edits, publication, withdrawal, and legacy metadata refresh retain that timestamp. `RecordQuery.CatalogAsOf` applies the membership cutoff inside the SQL content/catalog selection, separately from mutable `occurred_at`. JSON timestamps retain nanoseconds; SQL uses a broad microsecond candidate bound, followed by an exact timestamp check before card membership and legacy-owner deduplication. Metric fact `as_of`, received-at semantics, and current withdrawal behavior are unchanged.

Content-card cursors now carry the selected metric value and content ID, bound to the original normalized filter, rather than an offset. Post-snapshot additions cannot enter the catalog for that snapshot, editing an older card does not remove it, and suppressing an earlier item does not repeat the next page. Changing `as_of`, sorting, direction, or another normalized filter invalidates the cursor. Cursors issued before this cursor-format change must be discarded and the first page reloaded; there is no silent reinterpretation of an old offset.

Compatibility is intentionally limited by the historical data that actually exists. A pre-upgrade row with no `created_at` uses its last persisted `occurred_at` as the membership boundary. Before the first subsequent mutation, that exact fallback is frozen into `created_at`, so the mutation does not itself move the boundary. This does not recover the true original creation time of an already-edited historical row. A request for an earlier historical snapshot can still omit an old material whose original creation predates its surviving mutable row. No complete pre-upgrade catalog reconstruction is promised. If strict historical reconstruction is required, review and perform an audit/source-backed backfill and invalidate active cursors during that change; never infer missing creation facts from present titles or engagement metrics. This patch does not execute a backfill.

Tests reproduce the new-earlier-ID pagination failure for both own and legacy catalogs, keep edited pre-existing cards in a fixed snapshot, cover draft/publish/withdraw lifecycle timestamp retention, exact submicrosecond cutoff, metric keyset order, and filter mismatch rejection. SQLStore boundary fixtures assert owner/creation predicates and bounded transfer with a modeled 20,001-row post-snapshot catalog. They do not execute MySQL date parsing or establish an index plan. JSON creation-time filtering and the media existence query remain explicit real-MySQL integration and load verification gates.

## Indexes and deployment

The initial schema `service/sql/share_tracking_up.sql` includes the existing tenant/kind/owner/time indexes plus:

- tracking_session_time: tenant_id, kind, session_id, occurred_at, record_id
- tracking_scope_content_time: tenant_id, kind, owner_id, content_id, occurred_at

Existing installations have an additive, re-runnable upgrade script at `service/sql/share_tracking_bounded_queries_up.sql`. It checks information_schema before adding either index. The application does not execute this script. A database operator must review disk use, online-DDL support, and possible index-build locking before applying it.

The existing per-tenant transactional lock is unchanged. It preserves acceptance, identity/deletion, and projection consistency, but remains a concurrency bottleneck requiring real profiling. Legacy JSON predicates, identity EXISTS clauses, ordering, and their index choices need EXPLAIN and representative MySQL measurement; the current tests do not establish their production plans.

## Maintenance limits and remaining verification

Tenant-wide retention planning/cleanup and rebuild callers also inherit SQLStore's cumulative read budget. A large all-kind maintenance plan or very large session rebuild can fail with 422 and roll back; no partial cleanup is reported as successful. Retention still needs an explicitly reviewed bounded/batched operational plan for large tenants. This patch does not introduce a background job framework, resumable cleanup protocol, or unreviewed bulk-delete bypass. Targeted privacy selectors do not authorize narrowing away the account's other owner scopes; deletion must continue checking its full relevant owner coverage before mutation.

Before production sign-off, measure MySQL plans and memory under representative million-row distributions, candidate/session fan-out, cross-owner identity histories, large retained summaries, late arrivals, concurrent ingestion/reads, deletion, and retention. Verify 422 handling preserves the user's filters and offers a useful narrower query. No production data migration, deletion, deployment, load generator, or index build was run as part of these local tests.
