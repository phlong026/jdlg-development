-- DESTRUCTIVE: only use for an empty, disabled Phase 3 import deployment after
-- a verified backup. Production rollback should disable routes/workers and keep
-- task evidence; down/up must never be used to replay paid provider requests.
DROP TABLE IF EXISTS content_import_jobs;
