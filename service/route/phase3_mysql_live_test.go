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
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
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
	exerciseProjectNoteMySQLLifecycle(t, ctx, db, inbox, svc, scope)
	exerciseNativeReferenceHandoffMySQL(t, ctx, db, inbox, svc, scope)
}

func exerciseNativeReferenceHandoffMySQL(t *testing.T, ctx context.Context, db *sql.DB, inbox *notification.SQLStore, svc *tracking.Service, scope tracking.Scope) {
	t.Helper()
	svc.IsContentOperator = func(_ context.Context, actor int64, owner string) (bool, error) {
		return actor == scope.Actor && scope.Allows(owner), nil
	}
	draft, err := svc.SaveContent(ctx, scope, "", tracking.Draft{Title: "数据库交接验证", Body: "独立两端访问事实", BodyFormat: "plain", Owner: scope.Owners[0], Rights: "own"})
	if err != nil {
		t.Fatal(err)
	}
	preview, err := svc.Preview(ctx, scope, draft.ID, draft.Revision)
	if err != nil {
		t.Fatal(err)
	}
	result, err := svc.Publish(ctx, scope, draft.ID, draft.Revision, preview["preview_token"].(string), "mysql-handoff-publish")
	if err != nil {
		t.Fatal(err)
	}
	content := result["content"].(tracking.Content)
	ref, err := svc.CreateNativeReference(ctx, scope, tracking.NativeReferenceInput{ContentID: content.ID, ContentVersion: content.Version, IdempotencyKey: "mysql-native-reference"})
	if err != nil {
		t.Fatal(err)
	}
	if target, err := svc.NativeReferenceTarget(ctx, scope, ref.ReferenceID); err != nil || !strings.Contains(target["path"], "content_version=1") {
		t.Fatal(target, err)
	}
	actorEntry, err := svc.Enter(ctx, tracking.EntryInput{Route: "pages/contentreader/contentreader", Content: content.ID, Version: content.Version, Client: "mysql-handoff-sharer-client", Consent: "v1"}, scope.Actor)
	if err != nil {
		t.Fatal(err)
	}
	created, err := svc.CreateShare(ctx, tracking.ShareInput{Route: actorEntry.Route, Content: content.ID, Version: content.Version, Channel: "app_message", Key: "mysql-handoff-share"}, actorEntry.Token, scope.Actor)
	if err != nil {
		t.Fatal(err)
	}
	code := created["share_token"].(string)
	web, err := svc.Enter(ctx, tracking.EntryInput{Route: actorEntry.Route, Content: content.ID, Version: content.Version, ShareToken: code, Client: "mysql-web-client", Anonymous: "mysql-web-anonymous", Consent: "v1", WebSource: true}, 0)
	if err != nil {
		t.Fatal(err)
	}
	eventFor := func(entry tracking.Entry, id string) tracking.Event {
		return tracking.Event{Schema: 1, ID: id, Type: "page_view", Session: entry.Session, View: id + "-view", Route: entry.Route, Content: entry.Content, Version: entry.Version, Share: entry.Share, Occurred: svc.Now(), AppState: "foreground", PageState: "visible", Monotonic: 1000}
	}
	webEvent := eventFor(web, "mysql-handoff-web-page")
	if accepted, err := svc.Accept(ctx, web.Token, []tracking.Event{webEvent}); err != nil || len(accepted) != 1 || accepted[0].Status != "accepted" {
		t.Fatal(accepted, err)
	}
	handoffToken := strings.Repeat("h", 32)
	proof, err := svc.CreateShareHandoff(ctx, code, web.Token, webEvent.ID, handoffToken)
	if err != nil || !proof.Ready {
		t.Fatal(proof, err)
	}
	mini, err := svc.Enter(ctx, tracking.EntryInput{Route: web.Route, Content: content.ID, Version: content.Version, ShareToken: code, Client: "mysql-mini-client", Consent: "v1", HandoffToken: handoffToken}, 404)
	if err != nil {
		t.Fatal(err)
	}
	miniEvent := eventFor(mini, "mysql-handoff-mini-page")
	if accepted, err := svc.Accept(ctx, mini.Token, []tracking.Event{miniEvent}); err != nil || len(accepted) != 1 || accepted[0].Status != "accepted" {
		t.Fatal(accepted, err)
	}
	for kind, want := range map[string]int{"event": 2, "notice_event": 1} {
		var count int
		if err = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM tracking_records WHERE tenant_id=? AND kind=? AND record_id IN (?,?)`, phase3MySQLLiveTenant, kind, webEvent.ID, miniEvent.ID).Scan(&count); err != nil || count != want {
			t.Fatalf("handoff %s count=%d want=%d err=%v", kind, count, want, err)
		}
	}
	var merged int
	if err = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM tracking_records WHERE tenant_id=? AND kind='identity' AND record_id IN (?,?)`, phase3MySQLLiveTenant, web.Session, mini.Session).Scan(&merged); err != nil || merged != 0 {
		t.Fatalf("cross-device identity merged=%d err=%v", merged, err)
	}
	consumer, err := notification.NewConsumer(inbox, notification.ConsumerConfig{Channel: "official_account", PollInterval: time.Millisecond, LeaseDuration: time.Minute, MaxAttempts: 3, Eligibility: func(ctx context.Context, event notification.Event) (bool, error) {
		return svc.NoticeEligible(ctx, event.ID, event.RecipientID, scope.Owners...)
	}})
	if err != nil {
		t.Fatal(err)
	}
	if worked, err := consumer.RunOnce(ctx); err != nil || !worked {
		t.Fatal(worked, err)
	}
	var count int
	if err = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM mini_notifications WHERE tenant_id=? AND recipient_id=? AND event_id IN (?,?)`, phase3MySQLLiveTenant, scope.Actor, webEvent.ID, miniEvent.ID).Scan(&count); err != nil || count != 1 {
		t.Fatalf("handoff inbox count=%d err=%v", count, err)
	}
	var refs int
	if err = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM tracking_records WHERE tenant_id=? AND kind='native_ref' AND record_id=?`, phase3MySQLLiveTenant, ref.ReferenceID).Scan(&refs); err != nil || refs != 1 {
		t.Fatal(refs, err)
	}
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
	parsed.Addr = phase3MySQLContainerAddr(t, live.Container)
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

func phase3MySQLContainerAddr(t *testing.T, container string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	state, err := exec.CommandContext(ctx, "docker", "inspect", "-f", `{{index .Config.Labels "codex.phase3.verify"}} {{.State.Status}}`, container).Output()
	if err != nil || strings.TrimSpace(string(state)) != "true running" {
		t.Fatal("live-test container must be running and carry the disposable verification label")
	}
	port, err := exec.CommandContext(ctx, "docker", "port", container, "3306/tcp").Output()
	if err != nil {
		t.Fatal("cannot resolve disposable mysql loopback port")
	}
	lines := strings.Fields(string(port))
	if len(lines) != 1 {
		t.Fatal("disposable mysql must expose exactly one loopback port")
	}
	host, value, err := net.SplitHostPort(lines[0])
	if err != nil || host != "127.0.0.1" || value == "" {
		t.Fatal("disposable mysql port must stay bound to IPv4 loopback")
	}
	return net.JoinHostPort(host, value)
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

func exerciseProjectNoteMySQLLifecycle(t *testing.T, ctx context.Context, db *sql.DB, inbox *notification.SQLStore, svc *tracking.Service, scope tracking.Scope) {
	t.Helper()
	svc.IsContentOperator = func(context.Context, int64, string) (bool, error) { return true, nil }

	const (
		ownerA    = int64(303)
		ownerB    = int64(304)
		openEvent = "mysql-live-project-open-0001"
	)
	project, err := svc.ProjectNoteCreateProject(ctx, scope, tracking.ProjectNoteProjectInput{Name: "MySQL 私人方案", Owner: scope.Owners[0]})
	if err != nil {
		t.Fatal(err)
	}
	for _, actor := range []int64{ownerA, ownerB} {
		if _, err = svc.ProjectNoteInviteMember(ctx, scope, project.ID, actor); err != nil {
			t.Fatal(err)
		}
	}
	if err = svc.ProjectNoteSetConfirmer(ctx, scope, project.ID, ownerA); err != nil {
		t.Fatal(err)
	}

	asset := tracking.Asset{
		ID: "mysql-private-media-0001", Kind: "image", MIME: "image/png", Width: 3, Height: 2,
		URL: "/api/v1/miniprogram/project-notes/projects/" + project.ID + "/media/mysql-private-media-0001",
	}
	operation, already, err := svc.ProjectNotePrepareMedia(ctx, scope, project.ID, asset, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "upload")
	if err != nil || already {
		t.Fatalf("prepare private media: already=%v err=%v", already, err)
	}
	if _, err = svc.ProjectNoteFinalizeMedia(ctx, scope, project.ID, asset.ID, operation); err != nil {
		t.Fatal(err)
	}
	if _, err = svc.Media(ctx, nil, asset.ID); err == nil {
		t.Fatal("private project media leaked into the enterprise media catalog")
	}

	v1Body := `{"schema":1,"blocks":[{"kind":"paragraph","text":"固定版本一"},{"kind":"image","media_id":"mysql-private-media-0001"}]}`
	note, err := svc.ProjectNoteCreateNote(ctx, scope, project.ID, tracking.ProjectNoteDraft{
		Title: "方案一", Body: v1Body, BodyFormat: "article_v2", MediaIDs: []string{asset.ID}, CoverID: asset.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	v1, err := svc.ProjectNoteFreezeVersion(ctx, scope, note.ID, note.Revision)
	if err != nil {
		t.Fatal(err)
	}
	share1, err := svc.ProjectNoteCreateShare(ctx, scope, v1.ID, svc.Now().Add(time.Hour), "mysql-private-share-v1")
	if err != nil || share1.Token == "" {
		t.Fatalf("create fixed-version share: %+v err=%v", share1, err)
	}
	var plaintextTokens int
	if err = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM tracking_records WHERE tenant_id=? AND payload LIKE ?`, phase3MySQLLiveTenant, "%"+share1.Token+"%").Scan(&plaintextTokens); err != nil || plaintextTokens != 0 {
		t.Fatalf("raw private share token persisted: count=%d err=%v", plaintextTokens, err)
	}

	feedback, err := svc.ProjectNoteAddFeedback(ctx, share1.Token, ownerA, "门厅需要调整", "mysql-private-feedback-v1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = svc.ProjectNoteResolveFeedback(ctx, scope, feedback.ID, "设计师已处理", "mysql-private-resolve-v1"); err != nil {
		t.Fatal(err)
	}
	confirmedV1, err := svc.ProjectNoteConfirmShare(ctx, share1.Token, ownerA, "mysql-private-confirm-a-v1")
	if err != nil {
		t.Fatal(err)
	}

	note, err = svc.ProjectNoteSaveNote(ctx, scope, note.ID, tracking.ProjectNoteDraft{Title: "方案二", Body: "更新后的方案二", BodyFormat: "plain"}, note.Revision)
	if err != nil {
		t.Fatal(err)
	}
	v2, err := svc.ProjectNoteFreezeVersion(ctx, scope, note.ID, note.Revision)
	if err != nil {
		t.Fatal(err)
	}
	share2, err := svc.ProjectNoteCreateShare(ctx, scope, v2.ID, svc.Now().Add(2*time.Hour), "mysql-private-share-v2")
	if err != nil {
		t.Fatal(err)
	}
	if err = svc.ProjectNoteSetConfirmer(ctx, scope, project.ID, ownerB); err != nil {
		t.Fatal(err)
	}
	if _, err = svc.ProjectNoteConfirmShare(ctx, share1.Token, ownerB, "mysql-private-confirm-b-v1"); err == nil {
		t.Fatal("changed confirmer created a second confirmation for fixed version one")
	}
	v1View, err := svc.ProjectNoteReadShare(ctx, share1.Token, ownerB)
	if err != nil || v1View.Version.ID != v1.ID || v1View.Version.Body != v1.Body || v1View.Confirmation == nil || v1View.Confirmation.ID != confirmedV1.ID {
		t.Fatalf("fixed version changed after note update: version=%s body=%q confirmation=%+v err=%v", v1View.Version.ID, v1View.Version.Body, v1View.Confirmation, err)
	}
	confirmedV2, err := svc.ProjectNoteConfirmShare(ctx, share2.Token, ownerB, "mysql-private-confirm-b-v2")
	if err != nil || confirmedV2.VersionID != v2.ID {
		t.Fatalf("designated owner could not confirm version two: %+v err=%v", confirmedV2, err)
	}
	for _, versionID := range []string{v1.ID, v2.ID} {
		var count int
		if err = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM tracking_records WHERE tenant_id=? AND kind='note_confirm' AND content_id=?`, phase3MySQLLiveTenant, versionID).Scan(&count); err != nil || count != 1 {
			t.Fatalf("confirmation uniqueness for version %s: count=%d err=%v", versionID, count, err)
		}
	}

	opened, err := svc.ProjectNoteOpenShare(ctx, share2.Token, ownerA, openEvent)
	if err != nil || !opened.Notify || opened.ActualSharerID != scope.Actor || opened.VersionID != v2.ID {
		t.Fatalf("private open attribution mismatch: %+v err=%v", opened, err)
	}
	for _, kind := range []string{"note_open", "notice_event"} {
		var count int
		if err = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM tracking_records WHERE tenant_id=? AND kind=? AND record_id=?`, phase3MySQLLiveTenant, kind, openEvent).Scan(&count); err != nil || count != 1 {
			t.Fatalf("private open transaction %s count=%d err=%v", kind, count, err)
		}
	}
	var jobCount int
	if err = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM mini_notification_event_jobs WHERE tenant_id=? AND event_id=? AND status='pending'`, phase3MySQLLiveTenant, openEvent).Scan(&jobCount); err != nil || jobCount != 1 {
		t.Fatalf("private open notification job count=%d err=%v", jobCount, err)
	}

	consumer, err := notification.NewConsumer(inbox, notification.ConsumerConfig{
		Channel: "official_account", PollInterval: time.Millisecond, LeaseDuration: time.Minute, MaxAttempts: 3,
		Eligibility: func(ctx context.Context, event notification.Event) (bool, error) {
			return svc.ProjectNoteNoticeEligible(ctx, event.ID, event.RecipientID, scope.Owners...)
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	worked, err := consumer.RunOnce(ctx)
	if err != nil || !worked {
		t.Fatalf("private notification consumer did not materialize: worked=%v err=%v", worked, err)
	}
	var recipient int64
	var kind, route, contentID string
	if err = db.QueryRowContext(ctx, `SELECT recipient_id,kind,route_key,content_id FROM mini_notifications WHERE tenant_id=? AND event_id=?`, phase3MySQLLiveTenant, openEvent).Scan(&recipient, &kind, &route, &contentID); err != nil {
		t.Fatal(err)
	}
	wantRoute := "pages/projectnotes/projectnotes?project_id=" + project.ID + "&version_id=" + v2.ID
	if recipient != scope.Actor || kind != "project_note_open" || route != wantRoute || contentID != v2.ID {
		t.Fatalf("private notification projection mismatch: recipient=%d kind=%s route=%s content=%s", recipient, kind, route, contentID)
	}
}
