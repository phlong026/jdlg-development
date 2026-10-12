# Share-tracking daily projections

`daily_summary` stores `visible-v1` projections keyed by business day, owner,
original visitor, session, page view, content and published content version. The
business calendar is explicitly Asia/Shanghai (fixed UTC+08:00); all observed
visibility ranges are UTC milliseconds. Midnight splits duration, while the PV
is counted only on its original start day. UV consumers deduplicate the resolved
visitor key rather than summing daily UV counters. Sessions likewise deduplicate
session IDs across dates.

The projector deterministically sorts accepted events by occurred time and event
ID, establishes PVs first, and unions observed visibility across the session.
Overlapping pages cannot double-count the same session's foreground time. A
heartbeat arriving before its PV remains uncounted until the PV arrives and the
session is rebuilt. Rebuilding replaces the session's projections; retries never
increment counters. Adjacent ranges are compacted. Preview, self, test, abnormal
and duplicate facts are excluded using the same metric-version policy as raw
queries. No missing interval is filled in.

## Integrated transaction paths

- Acceptance and identity-link transactions call `RebuildSessionSummaries` after
  accepted facts or the identity link are durable within that transaction.
- Retention calls `PrepareSummaryRetention` before deleting reviewed raw event
  rows, inside that same transaction. It rebuilds and freezes affected sessions.
  The approved summary cutoff is passed into this hook, so legacy sessions
  reconstructed for the first time cannot leave newly created projections older
  than that cutoff. This includes sessions crossing the raw cutoff: readers use their projections
  exclusively, never add a partial raw session to its retained summary.
- Query reducers use `summaryFacts`, omit its returned expired-session IDs from
  raw reduction, and append its retained facts. Projections merge back to one PV
  across days. Date filters clip visibility ranges exactly.
- Privacy deletion includes `daily_summary` and `summary_state` in both
  discoverability and removal. Tombstones suppress reads and rebuilds even if
  a restored raw record remains. Restoration still requires tombstone replay.
- Whole daily buckets expire by their day-end timestamp, so a partial-day
  maintenance cutoff conservatively retains the bucket rather than deleting newer
  observations within it (up to one additional day). Both projection kinds belong
  to the summary-retention policy (default proposal
  180 days). Raw events retain the separate proposed 90-day policy. These periods
  still require the operator's approved maintenance policy and reviewed plan.

The derived projection contains no raw action/video payloads, source event IDs,
or access tokens. It necessarily retains a scoped visitor/session/page-view key,
start and last-observation time and compact disjoint visibility ranges to support
accurate filtered totals. These remain personal data and follow deletion and
access controls. Identity resolution is session-specific and applied again at
query time, including after raw expiry. The original anonymous key is not made a
global account alias.

Expired raw drilldowns return HTTP 410, not an empty list that appears to mean
no events existed. Retained views carry
`raw_events_expired`. A historical received-at snapshot earlier than the latest
frozen projection cannot be reconstructed after raw expiry; the reducer returns
409 rather than fabricate it. Rebuilding after raw expiry preserves the frozen
projection; a future metric-version migration must not pretend raw reconstruction
is still possible.

## Boundaries and validation

The SQL adapter implements `SessionRecordLister` to push the affected-session
predicate into the database. Adapters without it perform tenant-kind scans. Per-tenant transaction
serialization and query scans are still scaling limits. This implementation and
in-memory tests do not establish MySQL correctness under production load, the
100k-PV/day target, latency targets, device lifecycle correctness or permission
to deploy. Test those independently against the production-equivalent schema and
actual devices.

Tests cover duplicates, repeated rebuild, out-of-order PV, late segments,
Asia/Shanghai midnight, exact partial-range filtering, owner isolation,
post-expiry identity resolution, retained-summary survival, deletion tombstones
honest historical snapshot failure, a same-session anonymous-to-account identity
boundary, and first-time reconstruction of legacy data beyond summary retention.
