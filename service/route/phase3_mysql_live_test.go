package route

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"image"
	"image/png"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	mysql "github.com/go-sql-driver/mysql"
	"service/contentimport"
	"service/notification"
	"service/tracking"
)

const phase3MySQLLiveTenant = "phase3-mysql-live"

type phase3MySQLLiveConfig struct {
	MySQLDSN  string `json:"mysql_dsn"`
	Container string `json:"container"`
}

// TestPhase3MySQLLiveLifecycle is intentionally opt-in. It exercises the real
// MySQL stores against one disposable, loopback-only database; it never calls
// an external content platform, COS, WeChat, or a production database.
func TestPhase3MySQLLiveLifecycle(t *testing.T) {
	db := openPhase3MySQLLiveDB(t)
	defer db.Close()
	applyPhase3MySQLMigrations(t, db)
	cleanPhase3MySQLTenant(t, db)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	importStore, err := contentimport.NewSQLStore(db, phase3MySQLLiveTenant)
	if err != nil {
		t.Fatal(err)
	}
	if err = importStore.VerifySchema(ctx); err != nil {
		t.Fatal(err)
	}
	inbox, err := notification.NewSQLStore(db, phase3MySQLLiveTenant)
	if err != nil {
		t.Fatal(err)
	}
	if err = inbox.VerifySchema(ctx); err != nil {
		t.Fatal(err)
	}

	store := tracking.NewSQLStore(db, 4, 8)
	svc := tracking.New(store, tracking.Config{
		Enabled:        true,
		ContentEnabled: true,
		Tenant:         phase3MySQLLiveTenant,
		DefaultOwner:   "owner-phase3",
		ConsentVersion: "v1",
		RawDays:        90,
		Routes: map[string]tracking.Route{
			"pages/contentreader/contentreader": {Title: "内容阅读", Public: true},
		},
	})
	now := time.Now().UTC().Truncate(time.Microsecond)
	svc.Now = func() time.Time { return now }
	svc.NoticeRules.BrowseEnabled = true
	svc.OnNoticeEvent = func(ctx context.Context, event tracking.NoticeEvent) error {
		tx, ok := tracking.NoticeWriteTx(ctx)
		if !ok {
			return notification.ErrUnavailable
		}
		return inbox.EnqueueEventTx(ctx, tx, event.ID, event.OccurredAt)
	}

	scope := tracking.Scope{Tenant: phase3MySQLLiveTenant, Actor: 202, Owners: []string{"owner-phase3"}, View: true, Manage: true}
	content := exercisePhase3ImportLifecycle(t, ctx, importStore, svc, scope)
	exercisePhase3ShareNoticeLifecycle(t, ctx, db, inbox, svc, scope, content)
}

func openPhase3MySQLLiveDB(t *testing.T) *sql.DB {
	t.Helper()
	path := os.Getenv("JDLG_PHASE3_MYSQL_LIVE_CONFIG")
	if path == "" {
		t.Skip("set JDLG_PHASE3_MYSQL_LIVE_CONFIG to a private disposable database config")
	}
	if !filepath.IsAbs(path) {
		t.Fatal("refusing a relative live-test config path")
	}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
		t.Fatal("live-test config must be a private regular file")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal("cannot read live-test config")
	}
	var live phase3MySQLLiveConfig
	if err = json.Unmarshal(raw, &live); err != nil || live.MySQLDSN == "" || live.Container == "" {
		t.Fatal("invalid live-test config")
	}
	if len(live.Container) < len("jdlg-phase3-verify-") || live.Container[:len("jdlg-phase3-verify-")] != "jdlg-phase3-verify-" {
		t.Fatal("refusing an unrecognized live-test container")
	}
	parsed, err := mysql.ParseDSN(live.MySQLDSN)
	if err != nil {
		t.Fatal("invalid live-test mysql configuration")
	}
	host, _, err := net.SplitHostPort(parsed.Addr)
	if err != nil || parsed.Net != "tcp" || (host != "127.0.0.1" && host != "localhost") || parsed.DBName != "phase3_verify" {
		t.Fatal("refusing any database except loopback phase3_verify")
	}
	parsed.ParseTime = true
	parsed.MultiStatements = true
	parsed.Loc = time.UTC
	db, err := sql.Open("mysql", parsed.FormatDSN())
	if err != nil {
		t.Fatal("cannot initialize live-test mysql client")
	}
	db.SetMaxOpenConns(8)
	db.SetMaxIdleConns(8)
	deadline := time.Now().Add(45 * time.Second)
	for {
		pingCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		err = db.PingContext(pingCtx)
		cancel()
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			_ = db.Close()
			t.Fatal("disposable phase3_verify database was not ready within 45 seconds")
		}
		time.Sleep(5 * time.Second)
	}
	var selected string
	if err = db.QueryRow(`SELECT DATABASE()`).Scan(&selected); err != nil || selected != "phase3_verify" {
		_ = db.Close()
		t.Fatal("database selection guard failed")
	}
	return db
}

func applyPhase3MySQLMigrations(t *testing.T, db *sql.DB) {
	t.Helper()
	_, current, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate migration directory")
	}
	root := filepath.Join(filepath.Dir(current), "..", "sql")
	files := []string{
		"share_tracking_up.sql",
		"share_tracking_bounded_queries_up.sql",
		"phase3_share_actor_index_up.sql",
		"phase3_content_import_up.sql",
		"phase3_notifications_up.sql",
	}
	for _, name := range files {
		data, err := os.ReadFile(filepath.Join(root, name))
		if err != nil {
			t.Fatalf("read migration %s: %v", name, err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		_, err = db.ExecContext(ctx, string(data))
		cancel()
		if err != nil {
			t.Fatalf("apply migration %s: %v", name, err)
		}
	}
}

func cleanPhase3MySQLTenant(t *testing.T, db *sql.DB) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for _, query := range []string{
		`DELETE FROM mini_notification_provider_receipts WHERE tenant_id=?`,
		`DELETE FROM mini_notification_deliveries WHERE tenant_id=?`,
		`DELETE FROM mini_notifications WHERE tenant_id=?`,
		`DELETE FROM mini_notification_event_jobs WHERE tenant_id=?`,
		`DELETE FROM content_import_jobs WHERE tenant_id=?`,
		`DELETE FROM tracking_records WHERE tenant_id=?`,
		`DELETE FROM tracking_tenant_lock WHERE tenant_id=?`,
	} {
		if _, err := db.ExecContext(ctx, query, phase3MySQLLiveTenant); err != nil {
			t.Fatal(err)
		}
	}
}

func exercisePhase3ImportLifecycle(t *testing.T, ctx context.Context, jobs *contentimport.SQLStore, svc *tracking.Service, scope tracking.Scope) tracking.Content {
	t.Helper()
	request := contentimport.Request{
		Tenant:   phase3MySQLLiveTenant,
		Actor:    scope.Actor,
		Owner:    scope.Owners[0],
		URL:      "https://mp.weixin.qq.com/s/synthetic-db-fixture",
		Platform: contentimport.WechatArticle,
		Rights:   "authorized",
		Key:      "mysql-live-import-0001",
	}
	first, err := jobs.Submit(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	again, err := jobs.Submit(ctx, request)
	if err != nil || again.ID != first.ID {
		t.Fatalf("idempotent submit mismatch: first=%s again=%s err=%v", first.ID, again.ID, err)
	}
	conflict := request
	conflict.URL = "https://mp.weixin.qq.com/s/changed-synthetic-fixture"
	if _, err = jobs.Submit(ctx, conflict); !errors.Is(err, contentimport.ErrConflict) {
		t.Fatalf("changed idempotent request should conflict: %v", err)
	}

	claims := make(chan contentimport.Job, 2)
	errs := make(chan error, 2)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			job, claimErr := jobs.Claim(ctx, time.Now().UTC(), time.Minute)
			if claimErr != nil {
				errs <- claimErr
				return
			}
			claims <- job
		}()
	}
	close(start)
	wg.Wait()
	close(claims)
	close(errs)
	var claimed contentimport.Job
	for job := range claims {
		if claimed.ID != "" {
			t.Fatal("two workers claimed the same queued import")
		}
		claimed = job
	}
	noJob := 0
	for claimErr := range errs {
		if errors.Is(claimErr, contentimport.ErrNoJob) {
			noJob++
			continue
		}
		t.Fatalf("unexpected concurrent claim error: %v", claimErr)
	}
	if claimed.ID != first.ID || noJob != 1 {
		t.Fatalf("claim arbitration mismatch: claimed=%s no_job=%d", claimed.ID, noJob)
	}
	if err = jobs.BeginResolve(ctx, claimed.ID, claimed.LeaseToken); err != nil {
		t.Fatal(err)
	}
	doc := contentimport.Document{
		Source: contentimport.Source{
			Platform:  contentimport.WechatArticle,
			ID:        "synthetic-article-0001",
			SourceURL: request.URL,
			Title:     "MySQL 联调合成原排版",
			Media: []contentimport.RemoteMedia{{
				ID: "synthetic-image-0001", Kind: "image", URL: "https://fixture.invalid/synthetic.png", Caption: "合成数据库测试图",
			}},
		},
		Body: tracking.ArticleDocument{
			Schema:     1,
			LayoutHTML: `<section><h2>合成原排版</h2><jd-image data-media-id="remote-0000"></jd-image></section>`,
			Blocks:     []tracking.ArticleBlock{{Kind: "heading", Text: "合成原排版", Level: 2}, {Kind: "image", MediaID: "remote-0000", Caption: "合成数据库测试图"}},
		},
		CoverRef: "remote-0000",
	}
	if err = jobs.SaveResolution(ctx, claimed.ID, claimed.LeaseToken, doc); err != nil {
		t.Fatal(err)
	}

	mediaRoot := t.TempDir()
	if err = os.Chmod(mediaRoot, 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CONTENT_MEDIA_STORAGE", "filesystem")
	t.Setenv("TRACKING_MEDIA_DIR", mediaRoot)
	var imageBytes bytes.Buffer
	if err = png.Encode(&imageBytes, image.NewRGBA(image.Rect(0, 0, 3, 2))); err != nil {
		t.Fatal(err)
	}
	fetches := 0
	asset, err := saveImportedMedia(ctx, svc, scope, claimed, 0, doc.Source.Media[0], func(context.Context, contentimport.Platform, string, int64) ([]byte, string, error) {
		fetches++
		return imageBytes.Bytes(), "image/png", nil
	})
	if err != nil || fetches != 1 {
		t.Fatalf("save private synthetic media: fetches=%d err=%v", fetches, err)
	}
	stored, err := os.Stat(filepath.Join(mediaRoot, asset.ID))
	if err != nil || stored.Mode().Perm() != 0600 {
		t.Fatalf("private media mode=%v err=%v", stored, err)
	}
	if _, err = svc.Media(ctx, nil, asset.ID); err == nil {
		t.Fatal("draft media became public before publication")
	}
	draft, err := importDraft(claimed, doc, []tracking.Asset{asset})
	if err != nil {
		t.Fatal(err)
	}
	content, err := svc.SaveImportedDraft(ctx, scope, claimed.ID, draft, map[string]string{"fixture": "synthetic-db-only"})
	if err != nil {
		t.Fatal(err)
	}
	replayed, err := svc.SaveImportedDraft(ctx, scope, claimed.ID, draft, map[string]string{"fixture": "synthetic-db-only"})
	if err != nil || replayed.ID != content.ID {
		t.Fatalf("import draft replay mismatch: content=%s replay=%s err=%v", content.ID, replayed.ID, err)
	}
	if err = jobs.Finish(ctx, claimed.ID, claimed.LeaseToken, contentimport.Ready, content.ID, ""); err != nil {
		t.Fatal(err)
	}
	ready, err := jobs.Get(ctx, scope.Actor, claimed.ID)
	if err != nil || ready.Status != contentimport.Ready || ready.ContentID != content.ID {
		t.Fatalf("ready job mismatch: %+v err=%v", ready, err)
	}

	expiryRequest := request
	expiryRequest.Key = "mysql-live-import-expiry-0001"
	expiryRequest.URL = "https://mp.weixin.qq.com/s/synthetic-expiry-fixture"
	expiring, err := jobs.Submit(ctx, expiryRequest)
	if err != nil {
		t.Fatal(err)
	}
	expiring, err = jobs.Claim(ctx, time.Now().UTC(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if err = jobs.BeginResolve(ctx, expiring.ID, expiring.LeaseToken); err != nil {
		t.Fatal(err)
	}
	if _, err = jobs.Claim(ctx, expiring.LeaseUntil.Add(time.Second), time.Minute); !errors.Is(err, contentimport.ErrNoJob) {
		t.Fatalf("expired paid request should not be reclaimed: %v", err)
	}
	uncertain, err := jobs.Get(ctx, scope.Actor, expiring.ID)
	if err != nil || uncertain.Status != contentimport.Uncertain || uncertain.ErrorCode != "provider_result_unknown" {
		t.Fatalf("expired paid request did not become uncertain: %+v err=%v", uncertain, err)
	}
	return content
}

func exercisePhase3ShareNoticeLifecycle(t *testing.T, ctx context.Context, db *sql.DB, inbox *notification.SQLStore, svc *tracking.Service, scope tracking.Scope, draft tracking.Content) {
	t.Helper()
	preview, err := svc.Preview(ctx, scope, draft.ID, draft.Revision)
	if err != nil {
		t.Fatal(err)
	}
	published, err := svc.Publish(ctx, scope, draft.ID, draft.Revision, preview["preview_token"].(string), "mysql-live-publish-0001")
	if err != nil {
		t.Fatal(err)
	}
	content := published["content"].(tracking.Content)
	if _, err = svc.Media(ctx, nil, content.Media[0].ID); err != nil {
		t.Fatalf("published media not readable through tracking authorization: %v", err)
	}

	sharer, err := svc.Enter(ctx, tracking.EntryInput{
		Route: "pages/contentreader/contentreader", Content: content.ID, Version: content.Version,
		Client: "actor-b-client-0001", Anonymous: "actor-b-anon-0001", Consent: "v1",
	}, scope.Actor)
	if err != nil {
		t.Fatal(err)
	}
	share, err := svc.CreateShare(ctx, tracking.ShareInput{
		Route: sharer.Route, Content: sharer.Content, Version: sharer.Version,
		Channel: "app_message", Key: "mysql-live-share-0001",
	}, sharer.Token, scope.Actor)
	if err != nil {
		t.Fatal(err)
	}
	visitor, err := svc.Enter(ctx, tracking.EntryInput{
		Route: "pages/contentreader/contentreader", Content: content.ID, Version: content.Version,
		ShareToken: share["share_token"].(string), Client: "anonymous-client-0001", Anonymous: "anonymous-user-0001", Consent: "v1",
	}, 0)
	if err != nil || visitor.Attribution != "share" || visitor.Visitor != "a:anonymous-user-0001" {
		t.Fatalf("anonymous attributed entry mismatch: %+v err=%v", visitor, err)
	}
	event := tracking.Event{
		Schema: 1, ID: "mysql-live-page-view-0001", Type: "page_view", Session: visitor.Session,
		View: "mysql-live-view-0001", Route: visitor.Route, Content: visitor.Content, Version: visitor.Version,
		Share: visitor.Share, Occurred: svc.Now(), AppState: "foreground", PageState: "visible", Monotonic: 1000,
	}
	results, err := svc.Accept(ctx, visitor.Token, []tracking.Event{event, event})
	if err != nil || len(results) != 2 || results[0].Status != "accepted" || results[1].Status != "duplicate" {
		t.Fatalf("page view acceptance mismatch: %+v err=%v", results, err)
	}
	for _, kind := range []string{"event", "notice_event"} {
		var count int
		if err = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM tracking_records WHERE tenant_id=? AND kind=? AND record_id=?`, phase3MySQLLiveTenant, kind, event.ID).Scan(&count); err != nil || count != 1 {
			t.Fatalf("same-transaction %s count=%d err=%v", kind, count, err)
		}
	}
	var jobCount int
	if err = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM mini_notification_event_jobs WHERE tenant_id=? AND event_id=? AND status='pending'`, phase3MySQLLiveTenant, event.ID).Scan(&jobCount); err != nil || jobCount != 1 {
		t.Fatalf("same-transaction notification job count=%d err=%v", jobCount, err)
	}

	consumer, err := notification.NewConsumer(inbox, notification.ConsumerConfig{
		Channel: "official_account", PollInterval: time.Millisecond, LeaseDuration: time.Minute, MaxAttempts: 3,
		Eligibility: func(ctx context.Context, event notification.Event) (bool, error) {
			return svc.NoticeEligible(ctx, event.ID, event.RecipientID, scope.Owners...)
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	worked, err := consumer.RunOnce(ctx)
	if err != nil || !worked {
		t.Fatalf("notification consumer did not materialize: worked=%v err=%v", worked, err)
	}
	worked, err = consumer.RunOnce(ctx)
	if err != nil || worked {
		t.Fatalf("notification job was not idempotently drained: worked=%v err=%v", worked, err)
	}
	page, err := inbox.ListInbox(ctx, scope.Actor, "", 10)
	if err != nil || len(page.Items) != 1 || page.Items[0].RecipientID != scope.Actor || page.Items[0].EventID != event.ID {
		t.Fatalf("actual sharer inbox mismatch: %+v err=%v", page, err)
	}
	other, err := inbox.ListInbox(ctx, scope.Actor+1, "", 10)
	if err != nil || len(other.Items) != 0 {
		t.Fatalf("non-sharer received notification: %+v err=%v", other, err)
	}
	unread, err := inbox.UnreadCount(ctx, scope.Actor)
	if err != nil || unread != 1 {
		t.Fatalf("unread count=%d err=%v", unread, err)
	}
	if err = inbox.MarkRead(ctx, scope.Actor, page.Items[0].ID, svc.Now()); err != nil {
		t.Fatal(err)
	}
	unread, err = inbox.UnreadCount(ctx, scope.Actor)
	if err != nil || unread != 0 {
		t.Fatalf("read transition count=%d err=%v", unread, err)
	}

	if _, err = svc.Revoke(ctx, scope, content.ID, content.Revision, "MySQL 联调验证撤回后不可访问", "mysql-live-revoke-0001"); err != nil {
		t.Fatal(err)
	}
	if _, err = svc.ReadContent(ctx, content.ID, content.Version); err == nil {
		t.Fatal("revoked content remained readable")
	}
	if _, err = svc.Enter(ctx, tracking.EntryInput{
		Route: "pages/contentreader/contentreader", Content: content.ID, Version: content.Version,
		ShareToken: share["share_token"].(string), Client: "late-client-0001", Anonymous: "late-anonymous-0001", Consent: "v1",
	}, 0); err == nil {
		t.Fatal("revoked share still opened revoked content")
	}
}
