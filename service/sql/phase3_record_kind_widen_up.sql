-- Widen tracking_records.kind for phase-3 project note / collection kinds.
-- Review online DDL, lock time and disk headroom before production execution.

-- tracking_records.kind was created as VARCHAR(24) for the original tracking
-- kinds. Phase-3 records introduced longer identifiers that exceed 24
-- characters, so every write below fails with MySQL 1406 (strict mode) and the
-- route layer surfaces it as the generic 503 "访问记录服务暂不可用":
--   project_note_invitation_request (31) — member invitation idempotency
--   project_note_join_request (25)       — owner join requests
--   project_collection_create (25) / _preview (26) / _version (26)
--   project_collection_publication (30) / _revocation (29)
-- Widen to VARCHAR(64) so these writes succeed and future kind names keep
-- headroom. Existing rows are untouched; widening cannot truncate.
ALTER TABLE tracking_records MODIFY COLUMN kind VARCHAR(64) NOT NULL;

-- Verify after rollout:
-- SELECT CHARACTER_MAXIMUM_LENGTH FROM information_schema.columns
--  WHERE table_schema=DATABASE() AND table_name='tracking_records' AND column_name='kind';
-- Then confirm end-to-end: create a member invitation from the mini program and
-- expect {code:0} instead of the 503 fallback.
