# Governance and maintenance boundaries

## CRM mapping verification

`makeTrackingCRMVerifier(db)` is the service's `VerifyCRM` callback. Every association rechecks the actor's enabled management access, explicit owner-specific tracking view/CRM grant, and active `management_crm_link`. It calls the existing signed CRM bridge `GET /customers/:id` as that linked CRM username and management user ID. It never substitutes the platform subject or expands CRM permissions. A success envelope, exact canonical customer ID, exact nonempty upstream customer number, and absence of a merged/deleted target are required. Missing connection, database errors, denied remote access, stale IDs, mismatched numbers, or malformed response fail closed. Raw CRM responses are not exposed in errors or logs.

This verifies an accessible existing CRM record, not that the recorded account belongs to a particular natural person. The operator remains responsible for the explicit association basis; `LinkCRM` retains its permission checks and audit trail. No cross-account merging is introduced. The remote endpoint contract is taken from `background/src/api/wecomCustomers.ts` (`Detail.customer`); integration against a live CRM remains a release gate.

## Retention is opt-in reviewed maintenance

`DefaultRetentionPolicy()` returns raw 90 days and summary/support-record 180 days with destructive maintenance **disabled**. These are proposed defaults requiring business/privacy review. `PlanRetention` is a dry run; it requires the authenticated tenant/owner scope and both deletion and rules permissions. The caller supplies explicit UTC cutoffs no newer than the configured minimum retention periods. A record exactly on the cutoff is retained.

`PurgeExpired` is a local maintenance method, not an HTTP endpoint, startup task, migration, or scheduler. The operator must explicitly enable the policy and provide a reviewed plan. The opaque review digest binds actor, tenant, owner set, cutoffs, exact candidate records and payload versions. Any candidate change requires another dry run and review. Execution and count-only audit insert are one repository transaction. Errors roll back. This implementation has not executed any production deletion or database migration.

Raw retention covers event, event fingerprint, sequence, view binding, expired-era entry, and rate-counter kinds. The longer policy covers session, identity, confirmed business, CRM mapping, manual override, share and share-request kinds. It intentionally does not remove deletion/deletion-session tombstones, audit rows, or published/revoked content. Unknown kinds are preserved until separately classified. Older live mappings age from their last stored update; review this behavior before adopting a policy. The preview includes per-kind counts and no visitor identities.

**Important aggregate limitation:** current reports rebuild from raw facts; there is no independent persistent, privacy-safe aggregate store. Purging raw facts at 90 days therefore removes their historical report contribution. The 180-day setting bounds supporting records and does not promise 180 days of full aggregate history. Before enabling destructive maintenance where 180-day reporting is required, implement and validate approved aggregate persistence or approve the shorter reporting horizon. Never present surviving session rows as complete historical visits.

Backups, external authorized exports, caches outside this module and replicas require separately approved retention procedures. Restores must replay deletion tombstones before serving traffic. A fresh database without restored tombstones cannot safely enforce prior deletions. Audit and tombstone retention themselves require a separate approved governance policy; this helper never silently expires them.

The generic SQL repository currently serializes each tenant transaction and lists each eligible kind. This favors correctness for the initial implementation, not unbounded scale. Establish production row counts, query plans, lock time and maintenance windows on an authorized database before enablement; partitioned/batched retention needs its own transaction and race analysis.

## Operational diagnostics

`Diagnostics(ctx, scope)` returns only finite owner-scoped counts: accepted persisted events, malformed stored events, sampling gaps, inferred seeks, queue-loss flags, late arrivals, self visits and preview visits. Repeated flags in one event count once. Unknown labels, event payloads, IDs and free text never become counter names or output. Counters do not infer rejected or lost events that were never persisted, and they are not a substitute for ingress rate/error monitoring. No client PII logging is added.

## Intent evidence limits

The existing authenticated `POST /favorites` flow in `service/route/favorite.go` validates its target and commits `UserFavoriteModel.Add`. It is a potential medium-intent success source, but currently has no verified tracking entry/session binding, and can report success when the favorite model is nil. Therefore this change does not manufacture medium intent from a frontend click, repeated visit, or success-looking response.

A future hook must require a nonnil successful persistence result, verify collection token/session/account equality after user consent, bind the relevant target, deduplicate by committed favorite identity (including repeated add/remove policy), emit only after the actual transaction commits, and ensure analytics failure cannot turn the successful business operation into a failed or duplicated favorite. No such hook is claimed as delivered here. Current business actions remain restricted to existing verified server-side success paths; configurable rule versions/windows and full medium criteria are separate release gates where not implemented by the main service.

## Checks performed

Unit tests exercise default-disabled purge, nonmutating preview, exact cutoff retention, owner isolation, tombstone/audit/content preservation, reviewed-plan drift detection, policy/date/permission rejection, actor/scope review binding, and finite diagnostics. `go test ./tracking ./route` is the targeted offline verification. No live database, CRM call, deployed schedule, backup expiry, or production cleanup has been tested or run.
