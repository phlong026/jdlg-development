-- Rollback of phase3_record_kind_widen_up.sql.
-- Shrinking back to VARCHAR(24) fails if any row already uses a longer kind
-- (strict mode rejects data loss), which is the desired safety behavior: such
-- rows cannot exist unless phase-3 writes succeeded, in which case do not roll
-- back — the widened column is what keeps invitations and collections working.
ALTER TABLE tracking_records MODIFY COLUMN kind VARCHAR(24) NOT NULL;
