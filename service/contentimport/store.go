package contentimport

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"
)

const (
	maxResolutionBytes = 2 << 20
	maxPendingPerActor = 20
)

var (
	storeIDPattern    = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_:-]{0,127}$`)
	errorCodePattern  = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)
	targetAppPattern  = regexp.MustCompile(`^[A-Za-z0-9_-]{0,64}$`)
	errInvalidRequest = errors.New("content import: invalid request")
)

type SQLStore struct {
	db     *sql.DB
	tenant string
	now    func() time.Time
	random func() (string, error)
}

var _ JobStore = (*SQLStore)(nil)

func NewSQLStore(db *sql.DB, tenant string) (*SQLStore, error) {
	if db == nil || !storeIDPattern.MatchString(tenant) || len(tenant) > 64 {
		return nil, errInvalidRequest
	}
	return &SQLStore{db: db, tenant: tenant, now: func() time.Time { return time.Now().UTC() }, random: secureStoreToken}, nil
}

func (s *SQLStore) VerifySchema(ctx context.Context) error {
	rows, err := s.db.QueryContext(ctx, `SELECT tenant_id,import_id,actor_id,owner_id,idempotency_key,semantics_hash,source_url,platform,rights,target_mp_appid,status,content_id,error_code,request_sent,resolution_json,lease_token,lease_until,created_at,updated_at FROM content_import_jobs LIMIT 0`)
	if err != nil {
		return importStoreError("verify_schema", err)
	}
	if err := rows.Close(); err != nil {
		return importStoreError("verify_schema_close", err)
	}
	return nil
}

func (s *SQLStore) Submit(ctx context.Context, request Request) (Job, error) {
	request, semanticHash, err := s.validateRequest(request)
	if err != nil {
		return Job{}, err
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return Job{}, importStoreError("submit_begin", err)
	}
	defer tx.Rollback()

	existing, err := queryJob(ctx, tx, jobSelect+` WHERE tenant_id=? AND actor_id=? AND idempotency_key=? FOR UPDATE`, s.tenant, request.Actor, request.Key)
	if err == nil {
		if existing.semanticsHash != semanticHash {
			return Job{}, ErrConflict
		}
		if err := tx.Commit(); err != nil {
			return Job{}, importStoreError("submit_existing_commit", err)
		}
		return publicJob(existing.Job), nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return Job{}, importStoreError("submit_existing", err)
	}

	var pending int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM content_import_jobs WHERE tenant_id=? AND actor_id=? AND status IN ('queued','resolving','downloading') FOR UPDATE`, s.tenant, request.Actor).Scan(&pending); err != nil {
		return Job{}, importStoreError("submit_pending_count", err)
	}
	if pending >= maxPendingPerActor {
		return Job{}, ErrConflict
	}
	id, err := s.random()
	if err != nil {
		return Job{}, importStoreError("submit_id", err)
	}
	now := s.now().UTC()
	job := Job{ID: id, Tenant: s.tenant, Actor: request.Actor, Owner: request.Owner, URL: request.URL, Platform: request.Platform, Rights: request.Rights, Key: request.Key, TargetMPAppID: request.TargetMPAppID, Status: Queued, CreatedAt: now, UpdatedAt: now}
	_, err = tx.ExecContext(ctx, `INSERT INTO content_import_jobs (tenant_id,import_id,actor_id,owner_id,idempotency_key,semantics_hash,source_url,platform,rights,target_mp_appid,status,content_id,error_code,request_sent,created_at,updated_at) VALUES (?,?,?,?,?,?,?,?,?,?,'queued','','',FALSE,?,?)`, s.tenant, job.ID, job.Actor, job.Owner, job.Key, semanticHash, job.URL, job.Platform, job.Rights, job.TargetMPAppID, now, now)
	if err != nil {
		// A concurrent insert may have won the unique idempotency key. Resolve it
		// inside this transaction rather than interpreting driver-specific errors.
		existing, lookupErr := queryJob(ctx, tx, jobSelect+` WHERE tenant_id=? AND actor_id=? AND idempotency_key=?`, s.tenant, request.Actor, request.Key)
		if lookupErr == nil {
			if existing.semanticsHash != semanticHash {
				return Job{}, ErrConflict
			}
			if commitErr := tx.Commit(); commitErr != nil {
				return Job{}, importStoreError("submit_race_commit", commitErr)
			}
			return publicJob(existing.Job), nil
		}
		return Job{}, importStoreError("submit_insert", err)
	}
	if err := tx.Commit(); err != nil {
		return Job{}, importStoreError("submit_commit", err)
	}
	return publicJob(job), nil
}

func (s *SQLStore) Get(ctx context.Context, actor int64, id string) (Job, error) {
	if actor <= 0 || !storeIDPattern.MatchString(id) {
		return Job{}, errInvalidRequest
	}
	row, err := queryJob(ctx, s.db, metadataJobSelect+` WHERE tenant_id=? AND actor_id=? AND import_id=?`, s.tenant, actor, id)
	if errors.Is(err, sql.ErrNoRows) {
		return Job{}, ErrNotFound
	}
	if err != nil {
		return Job{}, importStoreError("get", err)
	}
	return publicJob(row.Job), nil
}

func (s *SQLStore) List(ctx context.Context, actor int64, owners []string, limit int) ([]Job, error) {
	if actor <= 0 || limit < 1 || limit > 50 || len(owners) == 0 || len(owners) > 100 {
		return nil, errInvalidRequest
	}
	ownerSet := make(map[string]struct{}, len(owners))
	for _, owner := range owners {
		if !storeIDPattern.MatchString(owner) {
			return nil, errInvalidRequest
		}
		ownerSet[owner] = struct{}{}
	}
	owners = owners[:0]
	for owner := range ownerSet {
		owners = append(owners, owner)
	}
	sort.Strings(owners)
	args := make([]any, 0, len(owners)+3)
	args = append(args, s.tenant, actor)
	marks := make([]string, len(owners))
	for index, owner := range owners {
		marks[index] = "?"
		args = append(args, owner)
	}
	args = append(args, limit)
	rows, err := s.db.QueryContext(ctx, metadataJobSelect+` WHERE tenant_id=? AND actor_id=? AND owner_id IN (`+strings.Join(marks, ",")+`) ORDER BY created_at DESC,import_id DESC LIMIT ?`, args...)
	if err != nil {
		return nil, importStoreError("list", err)
	}
	defer rows.Close()
	jobs := make([]Job, 0, limit)
	for rows.Next() {
		row, scanErr := scanJob(rows)
		if scanErr != nil {
			return nil, importStoreError("list_scan", scanErr)
		}
		jobs = append(jobs, publicJob(row.Job))
	}
	if err := rows.Err(); err != nil {
		return nil, importStoreError("list_rows", err)
	}
	return jobs, nil
}

func (s *SQLStore) Claim(ctx context.Context, now time.Time, lease time.Duration) (Job, error) {
	if now.IsZero() || lease < time.Second || lease > 10*time.Minute {
		return Job{}, errInvalidRequest
	}
	now = now.UTC()
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return Job{}, importStoreError("claim_begin", err)
	}
	defer tx.Rollback()
	// The provider may have accepted a paid request before the lease expired.
	// Preserve that uncertainty instead of blindly charging and resolving again.
	if _, err := tx.ExecContext(ctx, `UPDATE content_import_jobs SET status='uncertain',error_code='provider_result_unknown',lease_token=NULL,lease_until=NULL,updated_at=? WHERE tenant_id=? AND status='resolving' AND request_sent=TRUE AND resolution_json IS NULL AND lease_until<=?`, now, s.tenant, now); err != nil {
		return Job{}, importStoreError("claim_expire_resolve", err)
	}
	row, err := queryJob(ctx, tx, jobSelect+` WHERE tenant_id=? AND (status='queued' OR (status='resolving' AND request_sent=FALSE AND lease_until<=?) OR (status='downloading' AND resolution_json IS NOT NULL AND (lease_until IS NULL OR lease_until<=?))) ORDER BY created_at,import_id LIMIT 1 FOR UPDATE`, s.tenant, now, now)
	if errors.Is(err, sql.ErrNoRows) {
		if commitErr := tx.Commit(); commitErr != nil {
			return Job{}, importStoreError("claim_empty_commit", commitErr)
		}
		return Job{}, ErrNoJob
	}
	if err != nil {
		return Job{}, importStoreError("claim_select", err)
	}
	token, err := s.random()
	if err != nil {
		return Job{}, importStoreError("claim_token", err)
	}
	status := row.Status
	if status != Downloading {
		status = Resolving
	}
	until := now.Add(lease)
	result, err := tx.ExecContext(ctx, `UPDATE content_import_jobs SET status=?,lease_token=?,lease_until=?,updated_at=? WHERE tenant_id=? AND import_id=? AND (status='queued' OR (status='resolving' AND request_sent=FALSE AND lease_until<=?) OR (status='downloading' AND resolution_json IS NOT NULL AND (lease_until IS NULL OR lease_until<=?)))`, status, token, until, now, s.tenant, row.ID, now, now)
	if err = checkedImportUpdate("claim_update", result, err); err != nil {
		return Job{}, err
	}
	if err := tx.Commit(); err != nil {
		return Job{}, importStoreError("claim_commit", err)
	}
	row.Status, row.LeaseToken, row.LeaseUntil, row.UpdatedAt = status, token, until, now
	return row.Job, nil
}

func (s *SQLStore) BeginResolve(ctx context.Context, id, leaseToken string) error {
	if !storeIDPattern.MatchString(id) || !storeIDPattern.MatchString(leaseToken) {
		return errInvalidRequest
	}
	now := s.now().UTC()
	result, err := s.db.ExecContext(ctx, `UPDATE content_import_jobs SET request_sent=TRUE,updated_at=? WHERE tenant_id=? AND import_id=? AND status='resolving' AND request_sent=FALSE AND lease_token=? AND lease_until>?`, now, s.tenant, id, leaseToken, now)
	return checkedImportUpdate("begin_resolve", result, err)
}

func (s *SQLStore) SaveResolution(ctx context.Context, id, leaseToken string, document Document) error {
	if !storeIDPattern.MatchString(id) || !storeIDPattern.MatchString(leaseToken) {
		return errInvalidRequest
	}
	payload, err := json.Marshal(document)
	if err != nil || len(payload) == 0 || len(payload) > maxResolutionBytes {
		return errInvalidRequest
	}
	now := s.now().UTC()
	result, err := s.db.ExecContext(ctx, `UPDATE content_import_jobs SET status='downloading',resolution_json=?,error_code='',updated_at=? WHERE tenant_id=? AND import_id=? AND status='resolving' AND request_sent=TRUE AND lease_token=? AND lease_until>?`, payload, now, s.tenant, id, leaseToken, now)
	return checkedImportUpdate("save_resolution", result, err)
}

func (s *SQLStore) Renew(ctx context.Context, id, leaseToken string, now time.Time, extension time.Duration) error {
	if !storeIDPattern.MatchString(id) || !storeIDPattern.MatchString(leaseToken) || now.IsZero() || extension < time.Second || extension > 10*time.Minute {
		return errInvalidRequest
	}
	now = now.UTC()
	result, err := s.db.ExecContext(ctx, `UPDATE content_import_jobs SET lease_until=?,updated_at=? WHERE tenant_id=? AND import_id=? AND status IN ('resolving','downloading') AND lease_token=? AND lease_until>?`, now.Add(extension), now, s.tenant, id, leaseToken, now)
	return checkedImportUpdate("renew", result, err)
}

func (s *SQLStore) Finish(ctx context.Context, id, leaseToken, status, contentID, errorCode string) error {
	if !storeIDPattern.MatchString(id) || !storeIDPattern.MatchString(leaseToken) || !validTerminal(status, contentID, errorCode) {
		return errInvalidRequest
	}
	now := s.now().UTC()
	result, err := s.db.ExecContext(ctx, `UPDATE content_import_jobs SET status=?,content_id=?,error_code=?,resolution_json=CASE WHEN ?='ready' THEN NULL ELSE resolution_json END,lease_token=NULL,lease_until=NULL,updated_at=? WHERE tenant_id=? AND import_id=? AND status IN ('resolving','downloading') AND lease_token=? AND lease_until>?`, status, contentID, errorCode, status, now, s.tenant, id, leaseToken, now)
	return checkedImportUpdate("finish", result, err)
}

// Retry is intentionally actor-scoped. Uncertain paid requests require manual
// reconciliation and can never be moved back to the automatic resolver queue.
func (s *SQLStore) Retry(ctx context.Context, actor int64, id string) (Job, error) {
	if actor <= 0 || !storeIDPattern.MatchString(id) {
		return Job{}, errInvalidRequest
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return Job{}, importStoreError("retry_begin", err)
	}
	defer tx.Rollback()
	row, err := queryJob(ctx, tx, jobSelect+` WHERE tenant_id=? AND actor_id=? AND import_id=? FOR UPDATE`, s.tenant, actor, id)
	if errors.Is(err, sql.ErrNoRows) {
		return Job{}, ErrNotFound
	}
	if err != nil {
		return Job{}, importStoreError("retry_select", err)
	}
	if !canRetryJob(row.Job) {
		return Job{}, ErrConflict
	}
	nextStatus := Downloading
	requestSent := true
	refreshResolution := row.Document != nil && row.ErrorCode == "media_source_expired"
	if row.Document == nil || refreshResolution {
		if row.RequestSent && !retryableConfigurationError(row.ErrorCode) {
			if !refreshResolution {
				return Job{}, ErrConflict
			}
		}
		nextStatus, requestSent = Queued, false
	}
	var pending int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM content_import_jobs WHERE tenant_id=? AND actor_id=? AND status IN ('queued','resolving','downloading') FOR UPDATE`, s.tenant, actor).Scan(&pending); err != nil {
		return Job{}, importStoreError("retry_pending_count", err)
	}
	if pending >= maxPendingPerActor {
		return Job{}, ErrConflict
	}
	now := s.now().UTC()
	result, err := tx.ExecContext(ctx, `UPDATE content_import_jobs SET status=?,request_sent=?,content_id='',error_code='',resolution_json=CASE WHEN ? THEN NULL ELSE resolution_json END,lease_token=NULL,lease_until=NULL,updated_at=? WHERE tenant_id=? AND actor_id=? AND import_id=? AND status IN ('failed','blocked')`, nextStatus, requestSent, refreshResolution, now, s.tenant, actor, id)
	if err = checkedImportUpdate("retry_update", result, err); err != nil {
		return Job{}, err
	}
	if err := tx.Commit(); err != nil {
		return Job{}, importStoreError("retry_commit", err)
	}
	row.Status, row.RequestSent, row.ContentID, row.ErrorCode, row.LeaseToken, row.LeaseUntil, row.UpdatedAt = nextStatus, requestSent, "", "", "", time.Time{}, now
	if refreshResolution {
		row.Document = nil
	}
	return publicJob(row.Job), nil
}

type jobRow struct {
	Job
	semanticsHash string
}

const jobSelect = `SELECT tenant_id,import_id,actor_id,owner_id,idempotency_key,semantics_hash,source_url,platform,rights,target_mp_appid,status,content_id,error_code,request_sent,resolution_json,lease_token,lease_until,created_at,updated_at FROM content_import_jobs`

// Polling needs snapshot presence for retry eligibility, not the potentially
// 2 MiB resolution. Full snapshots are read only by retry/worker transactions.
var metadataJobSelect = strings.Replace(jobSelect, "resolution_json", "CASE WHEN resolution_json IS NULL THEN NULL ELSE '{}' END AS resolution_json", 1)

type rowScanner interface{ Scan(...any) error }
type rowQueryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func queryJob(ctx context.Context, queryer rowQueryer, query string, args ...any) (jobRow, error) {
	return scanJob(queryer.QueryRowContext(ctx, query, args...))
}

func scanJob(scanner rowScanner) (jobRow, error) {
	var row jobRow
	var payload []byte
	var leaseToken sql.NullString
	var leaseUntil sql.NullTime
	err := scanner.Scan(&row.Tenant, &row.ID, &row.Actor, &row.Owner, &row.Key, &row.semanticsHash, &row.URL, &row.Platform, &row.Rights, &row.TargetMPAppID, &row.Status, &row.ContentID, &row.ErrorCode, &row.RequestSent, &payload, &leaseToken, &leaseUntil, &row.CreatedAt, &row.UpdatedAt)
	if err != nil {
		return jobRow{}, err
	}
	if len(payload) > maxResolutionBytes {
		return jobRow{}, errors.New("resolution snapshot too large")
	}
	if len(payload) != 0 {
		row.Document = &Document{}
		if err := json.Unmarshal(payload, row.Document); err != nil {
			return jobRow{}, errors.New("invalid resolution snapshot")
		}
	}
	if leaseToken.Valid {
		row.LeaseToken = leaseToken.String
	}
	if leaseUntil.Valid {
		row.LeaseUntil = leaseUntil.Time
	}
	return row, nil
}

func (s *SQLStore) validateRequest(request Request) (Request, string, error) {
	if request.Tenant != s.tenant || request.Actor <= 0 || !storeIDPattern.MatchString(request.Owner) || !storeIDPattern.MatchString(request.Key) || len(request.Key) < 8 || !targetAppPattern.MatchString(request.TargetMPAppID) || request.Rights != "own" && request.Rights != "authorized" {
		return Request{}, "", errInvalidRequest
	}
	normalized, platform, err := normalizeImportURL(request.URL)
	if err != nil || platform != request.Platform {
		return Request{}, "", errInvalidRequest
	}
	request.URL = normalized
	material := strings.Join([]string{string(request.Platform), request.URL, request.Owner, request.Rights, request.TargetMPAppID}, "\x00")
	hash := sha256.Sum256([]byte(material))
	return request, hex.EncodeToString(hash[:]), nil
}

func normalizeImportURL(raw string) (string, Platform, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Scheme != "https" || parsed.Hostname() == "" || parsed.User != nil || parsed.Port() != "" && parsed.Port() != "443" {
		return "", "", errInvalidRequest
	}
	parsed.Scheme = "https"
	parsed.Host = strings.ToLower(parsed.Hostname())
	parsed.Fragment = ""
	parsed.RawQuery = parsed.Query().Encode()
	host := parsed.Hostname()
	var platform Platform
	switch {
	case host == "mp.weixin.qq.com" && (parsed.Path == "/s" || strings.HasPrefix(parsed.Path, "/s/")):
		platform = WechatArticle
	case host == "douyin.com" || strings.HasSuffix(host, ".douyin.com") || host == "iesdouyin.com" || strings.HasSuffix(host, ".iesdouyin.com"):
		platform = Douyin
	case host == "xiaohongshu.com" || strings.HasSuffix(host, ".xiaohongshu.com") || host == "xhslink.com" || strings.HasSuffix(host, ".xhslink.com"):
		platform = Xiaohongshu
	case host == "channels.weixin.qq.com" && strings.HasPrefix(parsed.Path, "/s/"):
		platform = Channels
	case host == "weixin.qq.com" && strings.HasPrefix(parsed.Path, "/sph/"):
		platform = Channels
	default:
		return "", "", errInvalidRequest
	}
	normalized := parsed.String()
	if platform == Douyin {
		normalized = canonicalProviderURL(platform, parsed)
	}
	if platform == Xiaohongshu {
		link, err := ParseSourceLink(parsed.String())
		if err != nil {
			return "", "", errInvalidRequest
		}
		normalized = link.URL
	}
	if len(normalized) > 2048 {
		return "", "", errInvalidRequest
	}
	return normalized, platform, nil
}

func canRetryJob(job Job) bool {
	if job.Status != Failed && job.Status != Blocked {
		return false
	}
	if job.Document == nil {
		return !job.RequestSent || retryableConfigurationError(job.ErrorCode)
	}
	// A saved resolution normally avoids a second provider charge. An explicitly
	// retried expired-media error is the exception: Retry clears that stale
	// snapshot and queues one fresh resolution. Uncertain jobs never reach here.
	switch job.ErrorCode {
	case "download_failed", "partial_media", "import_failed", "draft_save_failed", "permission_revoked", "private_storage_required", "media_source_expired":
		return true
	}
	return false
}

func publicJob(job Job) Job {
	job.CanRetry = canRetryJob(job)
	job.Tenant, job.Actor, job.Rights, job.Key, job.LeaseToken = "", 0, "", "", ""
	job.LeaseUntil, job.RequestSent, job.Document = time.Time{}, false, nil
	return job
}

func validTerminal(status, contentID, errorCode string) bool {
	switch status {
	case Ready:
		return storeIDPattern.MatchString(contentID) && errorCode == ""
	case Failed, Blocked, Uncertain:
		return contentID == "" && errorCodePattern.MatchString(errorCode)
	default:
		return false
	}
}

func retryableConfigurationError(code string) bool {
	return code == "provider_not_configured" || code == "browser_required" || code == "requires_login" || code == "capture_timeout" || code == "capture_failed" || code == "article_fetch_failed" || code == "xhs_page_fetch_failed"
}

func secureStoreToken() (string, error) {
	value := make([]byte, 24)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return hex.EncodeToString(value), nil
}

func checkedImportUpdate(operation string, result sql.Result, err error) error {
	if err != nil {
		return importStoreError(operation, err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return importStoreError(operation+"_result", err)
	}
	if changed != 1 {
		return ErrConflict
	}
	return nil
}

func importStoreError(operation string, err error) error {
	log.Printf("[contentimport] store_op=%s error_type=%T", operation, err)
	return fmt.Errorf("%w: %s", ErrUnavailable, operation)
}
