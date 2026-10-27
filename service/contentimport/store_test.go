package contentimport

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestSubmitRejectsCrossTenantAndSemanticKeyConflict(t *testing.T) {
	now := time.Date(2026, 10, 27, 10, 0, 0, 0, time.UTC)
	request := Request{Tenant: "tenant-a", Actor: 7, Owner: "owner-1", URL: "https://v.douyin.com/abc?b=2&a=1#fragment", Platform: Douyin, Rights: "authorized", Key: "request-001"}
	baseStore, _ := NewSQLStore(newScriptDB(t), "tenant-a")
	if _, _, err := baseStore.validateRequest(Request{Tenant: "tenant-b", Actor: 7, Owner: "owner-1", URL: request.URL, Platform: Douyin, Rights: "authorized", Key: "request-001"}); !errors.Is(err, errInvalidRequest) {
		t.Fatalf("cross tenant err=%v", err)
	}

	script := &sqlScript{t: t, steps: []sqlStep{{kind: "query", contains: "idempotency_key=? FOR UPDATE", rows: [][]driver.Value{jobValues(Job{ID: "existing", Tenant: "tenant-a", Actor: 7, Owner: "owner-1", URL: "https://v.douyin.com/abc?a=1&b=2", Platform: Douyin, Rights: "authorized", Key: "request-001", Status: Queued, CreatedAt: now, UpdatedAt: now}, "different-semantic-hash", nil)}}}}
	store, _ := NewSQLStore(scriptDB(script), "tenant-a")
	store.now = func() time.Time { return now }
	if _, err := store.Submit(context.Background(), request); !errors.Is(err, ErrConflict) {
		t.Fatalf("Submit err=%v", err)
	}
	script.assertDone()
}

func TestSubmitIsIdempotentAndCapsPendingPerActor(t *testing.T) {
	now := time.Date(2026, 10, 27, 10, 0, 0, 0, time.UTC)
	request := Request{Tenant: "tenant-a", Actor: 7, Owner: "owner-1", URL: "https://www.xiaohongshu.com/explore/0123456789abcdef01234567", Platform: Xiaohongshu, Rights: "own", Key: "request-002"}
	probe, _ := NewSQLStore(newScriptDB(t), "tenant-a")
	request, hash, err := probe.validateRequest(request)
	if err != nil {
		t.Fatal(err)
	}
	existing := Job{ID: "same-job", Tenant: "tenant-a", Actor: 7, Owner: request.Owner, URL: request.URL, Platform: request.Platform, Rights: request.Rights, Key: request.Key, Status: Queued, CreatedAt: now, UpdatedAt: now}
	script := &sqlScript{t: t, steps: []sqlStep{{kind: "query", contains: "idempotency_key=? FOR UPDATE", rows: [][]driver.Value{jobValues(existing, hash, nil)}}}}
	store, _ := NewSQLStore(scriptDB(script), "tenant-a")
	got, err := store.Submit(context.Background(), request)
	if err != nil || got.ID != "same-job" || got.Key != "" || got.Actor != 0 {
		t.Fatalf("got=%+v err=%v", got, err)
	}
	script.assertDone()

	capScript := &sqlScript{t: t, steps: []sqlStep{
		{kind: "query", contains: "idempotency_key=? FOR UPDATE"},
		{kind: "query", contains: "SELECT COUNT(*)", rows: [][]driver.Value{{int64(maxPendingPerActor)}}},
	}}
	store, _ = NewSQLStore(scriptDB(capScript), "tenant-a")
	if _, err := store.Submit(context.Background(), request); !errors.Is(err, ErrConflict) {
		t.Fatalf("pending cap err=%v", err)
	}
	capScript.assertDone()
}

func TestGetAndListAreActorScopedAndReturnMetadataOnly(t *testing.T) {
	now := time.Date(2026, 10, 27, 10, 0, 0, 0, time.UTC)
	document := Document{Source: Source{Platform: Douyin, ID: "source", SourceURL: "https://v.douyin.com/secret"}}
	payload, _ := json.Marshal(document)
	job := Job{ID: "job-1", Tenant: "tenant-a", Actor: 7, Owner: "owner-1", URL: "https://v.douyin.com/public", Platform: Douyin, Rights: "own", Key: "private-key", Status: Downloading, LeaseToken: "private-lease", LeaseUntil: now.Add(time.Minute), RequestSent: true, CreatedAt: now, UpdatedAt: now}
	script := &sqlScript{t: t, steps: []sqlStep{
		{kind: "query", contains: "actor_id=? AND import_id=?", args: []any{"tenant-a", int64(7), "job-1"}, rows: [][]driver.Value{jobValues(job, "hash", payload)}},
		{kind: "query", contains: "actor_id=? AND owner_id IN (?)", args: []any{"tenant-a", int64(7), "owner-1", 10}, rows: [][]driver.Value{jobValues(job, "hash", payload)}},
	}}
	store, _ := NewSQLStore(scriptDB(script), "tenant-a")
	got, err := store.Get(context.Background(), 7, "job-1")
	if err != nil {
		t.Fatal(err)
	}
	assertPublicJob(t, got)
	jobs, err := store.List(context.Background(), 7, []string{"owner-1"}, 10)
	if err != nil || len(jobs) != 1 {
		t.Fatalf("jobs=%+v err=%v", jobs, err)
	}
	assertPublicJob(t, jobs[0])
	script.assertDone()
}

func TestClaimMarksExpiredPaidResolveUncertainAndLeasesQueuedJob(t *testing.T) {
	now := time.Date(2026, 10, 27, 10, 0, 0, 0, time.UTC)
	job := Job{ID: "job-queued", Tenant: "tenant-a", Actor: 7, Owner: "owner-1", URL: "https://v.douyin.com/public", Platform: Douyin, Rights: "own", Key: "request-003", Status: Queued, CreatedAt: now.Add(-time.Hour), UpdatedAt: now.Add(-time.Hour)}
	script := &sqlScript{t: t, steps: []sqlStep{
		{kind: "exec", contains: "status='uncertain'", affected: 1},
		{kind: "query", contains: "LIMIT 1 FOR UPDATE", rows: [][]driver.Value{jobValues(job, "hash", nil)}},
		{kind: "exec", contains: "SET status=?,lease_token=?", affected: 1},
	}}
	store, _ := NewSQLStore(scriptDB(script), "tenant-a")
	store.random = func() (string, error) { return "lease-token-001", nil }
	got, err := store.Claim(context.Background(), now, time.Minute)
	if err != nil || got.Status != Resolving || got.LeaseToken != "lease-token-001" || !got.LeaseUntil.Equal(now.Add(time.Minute)) {
		t.Fatalf("got=%+v err=%v", got, err)
	}
	script.assertDone()
}

func TestDownloadingSnapshotCanBeReclaimedWithoutResolving(t *testing.T) {
	now := time.Date(2026, 10, 27, 10, 0, 0, 0, time.UTC)
	document := Document{Source: Source{Platform: Channels, ID: "source", SourceURL: "https://channels.weixin.qq.com/s/abc"}}
	payload, _ := json.Marshal(document)
	job := Job{ID: "job-download", Tenant: "tenant-a", Actor: 7, Owner: "owner-1", URL: "https://channels.weixin.qq.com/s/abc", Platform: Channels, Rights: "own", Key: "request-004", Status: Downloading, RequestSent: true, CreatedAt: now.Add(-time.Hour), UpdatedAt: now.Add(-time.Hour)}
	script := &sqlScript{t: t, steps: []sqlStep{
		{kind: "exec", contains: "status='uncertain'", affected: 0},
		{kind: "query", contains: "resolution_json IS NOT NULL", rows: [][]driver.Value{jobValues(job, "hash", payload)}},
		{kind: "exec", contains: "SET status=?,lease_token=?", affected: 1},
	}}
	store, _ := NewSQLStore(scriptDB(script), "tenant-a")
	store.random = func() (string, error) { return "lease-token-002", nil }
	got, err := store.Claim(context.Background(), now, time.Minute)
	if err != nil || got.Status != Downloading || got.Document == nil || !got.RequestSent {
		t.Fatalf("got=%+v err=%v", got, err)
	}
	script.assertDone()
}

func TestResolutionAndTerminalWritesRequireLiveLease(t *testing.T) {
	now := time.Date(2026, 10, 27, 10, 0, 0, 0, time.UTC)
	script := &sqlScript{t: t, steps: []sqlStep{
		{kind: "exec", contains: "request_sent=TRUE", affected: 1},
		{kind: "exec", contains: "resolution_json=?", affected: 1},
		{kind: "exec", contains: "resolution_json=CASE", affected: 0},
	}}
	store, _ := NewSQLStore(scriptDB(script), "tenant-a")
	store.now = func() time.Time { return now }
	if err := store.BeginResolve(context.Background(), "job-1", "lease-token-001"); err != nil {
		t.Fatal(err)
	}
	document := Document{Source: Source{Platform: Douyin, ID: "source", SourceURL: "https://v.douyin.com/temporary"}}
	if err := store.SaveResolution(context.Background(), "job-1", "lease-token-001", document); err != nil {
		t.Fatal(err)
	}
	if err := store.Finish(context.Background(), "job-1", "stale-lease", Ready, "content-1", ""); !errors.Is(err, ErrConflict) {
		t.Fatalf("Finish err=%v", err)
	}
	script.assertDone()
}

func TestRetryNeverRequeuesUncertainAndUsesSnapshotForDownload(t *testing.T) {
	now := time.Date(2026, 10, 27, 10, 0, 0, 0, time.UTC)
	uncertain := Job{ID: "job-uncertain", Tenant: "tenant-a", Actor: 7, Owner: "owner-1", URL: "https://v.douyin.com/public", Platform: Douyin, Rights: "own", Key: "request-005", Status: Uncertain, RequestSent: true, CreatedAt: now, UpdatedAt: now}
	uncertainScript := &sqlScript{t: t, steps: []sqlStep{{kind: "query", contains: "actor_id=? AND import_id=? FOR UPDATE", rows: [][]driver.Value{jobValues(uncertain, "hash", nil)}}}}
	store, _ := NewSQLStore(scriptDB(uncertainScript), "tenant-a")
	if _, err := store.Retry(context.Background(), 7, uncertain.ID); !errors.Is(err, ErrConflict) {
		t.Fatalf("Retry uncertain err=%v", err)
	}
	uncertainScript.assertDone()

	document := Document{Source: Source{Platform: Douyin, ID: "source", SourceURL: "https://v.douyin.com/temp"}}
	payload, _ := json.Marshal(document)
	failed := uncertain
	failed.ID, failed.Status, failed.ErrorCode = "job-failed", Failed, "download_failed"
	retryScript := &sqlScript{t: t, steps: []sqlStep{
		{kind: "query", contains: "actor_id=? AND import_id=? FOR UPDATE", rows: [][]driver.Value{jobValues(failed, "hash", payload)}},
		{kind: "query", contains: "SELECT COUNT(*)", rows: [][]driver.Value{{int64(0)}}},
		{kind: "exec", contains: "status IN ('failed','blocked')", affected: 1},
	}}
	store, _ = NewSQLStore(scriptDB(retryScript), "tenant-a")
	store.now = func() time.Time { return now.Add(time.Minute) }
	got, err := store.Retry(context.Background(), 7, failed.ID)
	if err != nil || got.Status != Downloading || got.Document != nil {
		t.Fatalf("got=%+v err=%v", got, err)
	}
	retryScript.assertDone()
}

func TestJobJSONAndSchemaDoNotExposeInternalPayload(t *testing.T) {
	job := Job{ID: "job-1", Tenant: "tenant", Actor: 7, Rights: "own", Key: "secret-key", LeaseToken: "secret-lease", RequestSent: true, Document: &Document{ArchiveHTML: "secret-html"}}
	payload, err := json.Marshal(job)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"secret-key", "secret-lease", "secret-html", "request_sent"} {
		if strings.Contains(string(payload), secret) {
			t.Fatalf("public JSON leaked %q: %s", secret, payload)
		}
	}
	script := &sqlScript{t: t, steps: []sqlStep{{kind: "query", contains: "FROM content_import_jobs LIMIT 0"}}}
	store, _ := NewSQLStore(scriptDB(script), "tenant-a")
	if err := store.VerifySchema(context.Background()); err != nil {
		t.Fatal(err)
	}
	script.assertDone()
}

func assertPublicJob(t *testing.T, job Job) {
	t.Helper()
	if job.Tenant != "" || job.Actor != 0 || job.Rights != "" || job.Key != "" || job.LeaseToken != "" || !job.LeaseUntil.IsZero() || job.RequestSent || job.Document != nil {
		t.Fatalf("internal fields exposed: %+v", job)
	}
}

func jobValues(job Job, semanticHash string, payload []byte) []driver.Value {
	var leaseToken any
	if job.LeaseToken != "" {
		leaseToken = job.LeaseToken
	}
	var leaseUntil any
	if !job.LeaseUntil.IsZero() {
		leaseUntil = job.LeaseUntil
	}
	return []driver.Value{job.Tenant, job.ID, job.Actor, job.Owner, job.Key, semanticHash, job.URL, string(job.Platform), job.Rights, job.TargetMPAppID, job.Status, job.ContentID, job.ErrorCode, job.RequestSent, payload, leaseToken, leaseUntil, job.CreatedAt, job.UpdatedAt}
}

type sqlStep struct {
	kind     string
	contains string
	args     []any
	rows     [][]driver.Value
	affected int64
}

type sqlScript struct {
	t     *testing.T
	mu    sync.Mutex
	steps []sqlStep
}

func (s *sqlScript) pop(kind, query string, args []driver.NamedValue) sqlStep {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.steps) == 0 {
		s.t.Fatalf("unexpected %s: %s", kind, query)
	}
	step := s.steps[0]
	s.steps = s.steps[1:]
	if step.kind != kind || !strings.Contains(query, step.contains) {
		s.t.Fatalf("got %s %q, want %s containing %q", kind, query, step.kind, step.contains)
	}
	if step.args != nil {
		if len(step.args) != len(args) {
			s.t.Fatalf("args=%v want=%v", args, step.args)
		}
		for index := range args {
			if args[index].Value != step.args[index] {
				s.t.Fatalf("arg[%d]=%v want=%v", index, args[index].Value, step.args[index])
			}
		}
	}
	return step
}

func (s *sqlScript) assertDone() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.steps) != 0 {
		s.t.Fatalf("%d SQL steps not consumed", len(s.steps))
	}
}

func scriptDB(script *sqlScript) *sql.DB {
	db := sql.OpenDB(scriptConnector{script: script})
	db.SetMaxOpenConns(1)
	return db
}

func newScriptDB(t *testing.T) *sql.DB { return scriptDB(&sqlScript{t: t}) }

type scriptConnector struct{ script *sqlScript }

func (c scriptConnector) Connect(context.Context) (driver.Conn, error) {
	return &scriptConn{script: c.script}, nil
}
func (c scriptConnector) Driver() driver.Driver { return scriptDriver{} }

type scriptDriver struct{}

func (scriptDriver) Open(string) (driver.Conn, error) { return nil, errors.New("use connector") }

type scriptConn struct{ script *sqlScript }

func (c *scriptConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("prepare unsupported")
}
func (c *scriptConn) Close() error              { return nil }
func (c *scriptConn) Begin() (driver.Tx, error) { return scriptTx{}, nil }
func (c *scriptConn) BeginTx(context.Context, driver.TxOptions) (driver.Tx, error) {
	return scriptTx{}, nil
}
func (c *scriptConn) QueryContext(_ context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	step := c.script.pop("query", query, args)
	columns := make([]string, 1)
	if len(step.rows) > 0 {
		columns = make([]string, len(step.rows[0]))
	}
	return &scriptRows{columns: columns, rows: step.rows}, nil
}
func (c *scriptConn) ExecContext(_ context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	step := c.script.pop("exec", query, args)
	return driver.RowsAffected(step.affected), nil
}
func (*scriptConn) CheckNamedValue(*driver.NamedValue) error { return nil }

type scriptTx struct{}

func (scriptTx) Commit() error   { return nil }
func (scriptTx) Rollback() error { return nil }

type scriptRows struct {
	columns []string
	rows    [][]driver.Value
	index   int
}

func (r *scriptRows) Columns() []string { return r.columns }
func (r *scriptRows) Close() error      { return nil }
func (r *scriptRows) Next(dest []driver.Value) error {
	if r.index >= len(r.rows) {
		return io.EOF
	}
	copy(dest, r.rows[r.index])
	r.index++
	return nil
}

func TestPublicJobRetryProjectionUsesDurableEvidence(t *testing.T) {
	for _, item := range []struct {
		job   Job
		retry bool
	}{
		{Job{Status: Failed, RequestSent: true, ErrorCode: "import_failed", Document: &Document{}}, true},
		{Job{Status: Failed, RequestSent: true}, false},
		{Job{Status: Uncertain, RequestSent: true, Document: &Document{}}, false},
		{Job{Status: Blocked, RequestSent: true, ErrorCode: "browser_required"}, true},
		{Job{Status: Blocked, RequestSent: true, ErrorCode: "unsupported_encryption"}, false},
	} {
		if out := publicJob(item.job); out.CanRetry != item.retry || out.Document != nil || out.RequestSent {
			t.Fatalf("projection=%+v want retry=%v", out, item.retry)
		}
	}
}
func TestExpiredSnapshotCannotBeRetriedIndefinitely(t *testing.T) {
	job := Job{ID: "job-expired", Tenant: "tenant-a", Actor: 7, Owner: "owner-a", Status: Blocked, RequestSent: true, ErrorCode: "media_source_expired", Document: &Document{}}
	if publicJob(job).CanRetry {
		t.Fatal("expired signed media falsely retryable")
	}
	payload, _ := json.Marshal(job.Document)
	script := &sqlScript{t: t, steps: []sqlStep{{kind: "query", contains: "actor_id=? AND import_id=? FOR UPDATE", rows: [][]driver.Value{jobValues(job, "hash", payload)}}}}
	store, _ := NewSQLStore(scriptDB(script), "tenant-a")
	if _, err := store.Retry(context.Background(), 7, job.ID); !errors.Is(err, ErrConflict) {
		t.Fatal(err)
	}
	script.assertDone()
}
func TestFreeCaptureAndRefreshedLoginCanRetryWithoutPaidReplay(t *testing.T) {
	for _, code := range []string{"capture_timeout", "capture_failed", "requires_login"} {
		job := Job{ID: "job-configuration", Tenant: "tenant-a", Actor: 7, Owner: "owner-a", Status: Blocked, RequestSent: true, ErrorCode: code}
		if !publicJob(job).CanRetry {
			t.Fatal(code, "not retryable")
		}
		script := &sqlScript{t: t, steps: []sqlStep{{kind: "query", contains: "actor_id=? AND import_id=? FOR UPDATE", rows: [][]driver.Value{jobValues(job, "hash", nil)}}, {kind: "query", contains: "SELECT COUNT(*)", rows: [][]driver.Value{{int64(0)}}}, {kind: "exec", contains: "status IN ('failed','blocked')", affected: 1}}}
		store, _ := NewSQLStore(scriptDB(script), "tenant-a")
		out, err := store.Retry(context.Background(), 7, job.ID)
		if err != nil || out.Status != Queued {
			t.Fatal(out, err)
		}
		script.assertDone()
	}
}
func TestSnapshotRetryOnlyAllowsRecoverableFailures(t *testing.T) {
	for _, code := range []string{"media_too_large", "invalid_media", "missing_media", "invalid_stored_media", "invalid_draft"} {
		job := Job{Status: Failed, RequestSent: true, ErrorCode: code, Document: &Document{}}
		if publicJob(job).CanRetry {
			t.Fatal("deterministic failure advertised as retryable", code)
		}
	}
	if !publicJob(Job{Status: Failed, RequestSent: true, ErrorCode: "article_fetch_failed"}).CanRetry {
		t.Fatal("free article GET not retryable")
	}
}
