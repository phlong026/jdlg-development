package route

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"service/model"
	"service/tracking"
)

// This driver runs the production SQLStore, source resolver, model SQL/scans,
// and transaction/context plumbing with exactly one available pool connection.
// It is a deterministic regression test, not a live-MySQL concurrency claim.
type legacyTxContextKey struct{}
type legacyTxState struct {
	records                   map[string][]driver.Value
	opens, sources, commits   int
	requestContext, badSource bool
}
type legacyTxConnector struct{ state *legacyTxState }

func (c legacyTxConnector) Connect(context.Context) (driver.Conn, error) {
	c.state.opens++
	return &legacyTxConn{state: c.state}, nil
}
func (legacyTxConnector) Driver() driver.Driver { return legacyTestDriver{} }

type legacyTxConn struct {
	state   *legacyTxState
	pending map[string][]driver.Value
}

func (*legacyTxConn) Prepare(string) (driver.Stmt, error) { return nil, driver.ErrSkip }
func (*legacyTxConn) Close() error                        { return nil }
func (c *legacyTxConn) Begin() (driver.Tx, error) {
	c.pending = map[string][]driver.Value{}
	for k, v := range c.state.records {
		c.pending[k] = append([]driver.Value(nil), v...)
	}
	return legacyTxTransaction{c}, nil
}
func (c *legacyTxConn) BeginTx(context.Context, driver.TxOptions) (driver.Tx, error) {
	return c.Begin()
}
func (c *legacyTxConn) ExecContext(_ context.Context, q string, args []driver.NamedValue) (driver.Result, error) {
	if c.pending == nil {
		return nil, errors.New("write outside transaction")
	}
	if strings.HasPrefix(q, "INSERT IGNORE INTO tracking_tenant_lock") {
		return driver.RowsAffected(1), nil
	}
	if !strings.HasPrefix(q, "INSERT INTO tracking_records") || len(args) != 10 {
		return nil, fmt.Errorf("unexpected write: %s", q)
	}
	key := fmt.Sprint(args[0].Value, ":", args[1].Value, ":", args[2].Value)
	values := make([]driver.Value, 9)
	for i := range values {
		values[i] = args[i+1].Value
	}
	c.pending[key] = values
	return driver.RowsAffected(1), nil
}
func (c *legacyTxConn) QueryContext(ctx context.Context, q string, args []driver.NamedValue) (driver.Rows, error) {
	if strings.HasPrefix(q, "SELECT tenant_id FROM tracking_tenant_lock") {
		return &legacyTestRows{columns: []string{"tenant_id"}, values: []driver.Value{args[0].Value}}, nil
	}
	if strings.Contains(q, "FROM tracking_records WHERE") {
		key := fmt.Sprint(args[1].Value, ":", args[2].Value, ":", args[3].Value)
		return &legacyTestRows{
			columns: []string{"kind", "record_id", "owner_id", "visitor_key", "session_id", "content_id", "share_id", "occurred_at", "payload"},
			values:  c.pending[key],
		}, nil
	}
	if strings.Contains(q, "FROM tracking_records r WHERE") {
		rows := &legacyTxRows{}
		for _, values := range c.pending {
			if values[0] == args[2].Value {
				rows.values = append(rows.values, values)
			}
		}
		return rows, nil
	}
	c.state.sources++
	c.state.requestContext = ctx.Value(legacyTxContextKey{}) == "request"
	if !c.state.requestContext {
		return nil, errors.New("source lost request context")
	}
	if c.pending == nil {
		c.state.badSource = true
		return nil, errors.New("source escaped tracking transaction")
	}
	if strings.HasPrefix(q, "SELECT t.owner_id FROM tracking_access") {
		return &legacyTestRows{columns: []string{"owner_id"}, values: []driver.Value{"owner-a"}}, nil
	}
	if strings.HasPrefix(q, "SELECT 1 FROM user_favorites") || strings.HasPrefix(q, "SELECT 1 FROM customer_leads") {
		return &legacyTestRows{columns: []string{"present"}, values: []driver.Value{int64(1)}}, nil
	}
	return legacyTestConn{}.QueryContext(ctx, q, args)
}

type legacyTxTransaction struct{ conn *legacyTxConn }

func (tx legacyTxTransaction) Commit() error {
	tx.conn.state.records = tx.conn.pending
	tx.conn.pending = nil
	tx.conn.state.commits++
	return nil
}
func (tx legacyTxTransaction) Rollback() error { tx.conn.pending = nil; return nil }

func TestLegacySourcesReuseTrackingTransactionWithOneConnection(t *testing.T) {
	for _, route := range []string{
		"pages/aitooldetail/aitooldetail",
		"pages/aitoolworkbench/aitoolworkbench",
		"pages/inspirationdetail/inspirationdetail",
		"pages/utilitytooldetail/utilitytooldetail",
		"pages/templatesquaredetails/templatesquaredetails",
		"pages/designerhome/designerhome",
	} {
		t.Run(route, func(t *testing.T) {
			state := &legacyTxState{}
			db := sql.OpenDB(legacyTxConnector{state})
			db.SetMaxOpenConns(1)
			defer db.Close()
			ctx, cancel := context.WithTimeout(context.WithValue(context.Background(), legacyTxContextKey{}, "request"), 3*time.Second)
			defer cancel()
			s := tracking.New(&tracking.SQLStore{DB: db}, tracking.Config{
				Enabled: true, Tenant: "org-a", DefaultOwner: "owner-a", ConsentVersion: "v1",
				Routes: map[string]tracking.Route{route: {Title: "公开原素材", Public: true}},
			})
			s.ResolveLegacyContext = makeTrackingLegacyResolver(db)
			s.ResolveLegacy = func(string, int64) (tracking.LegacyMetadata, error) {
				t.Fatal("context-free compatibility resolver selected in production path")
				return tracking.LegacyMetadata{}, errors.New("unexpected resolver")
			}
			entry, err := s.Enter(ctx, tracking.EntryInput{
				Route: route, Content: tracking.LegacyID(route, 1), Client: "client-legacy", Consent: "v1",
			}, 0)
			if err != nil {
				t.Fatalf("enter with one connection: %v", err)
			}
			share, err := s.CreateShare(ctx, tracking.ShareInput{
				Route: route, Content: entry.Content, Version: entry.Version, Key: "share-legacy-one",
			}, entry.Token, 0)
			if err != nil {
				t.Fatalf("share with one connection: %v", err)
			}
			resolved, err := s.ResolveShare(ctx, share["share_token"].(string))
			if err != nil || resolved["content_version"] != entry.Version {
				t.Fatalf("resolve with one connection: result=%+v error=%v", resolved, err)
			}
			if state.opens != 1 || state.sources < 6 || state.commits != 3 || state.badSource || !state.requestContext || db.Stats().WaitCount != 0 {
				t.Fatalf("source did not reuse transaction/context: state=%+v stats=%+v", state, db.Stats())
			}
			for key, values := range state.records {
				payload := fmt.Sprint(values[8])
				if strings.Contains(payload, "private-account-data") || strings.Contains(payload, "private-prompt") {
					t.Fatalf("private source fields persisted in %s", key)
				}
			}
		})
	}
}

func TestLegacyResolverPreservesPublicSourceChecksAndMetadataAllowlist(t *testing.T) {
	db, err := sql.Open("tracking-legacy-test", "")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	resolve := makeTrackingLegacyResolver(db)
	for _, tc := range []struct {
		route  string
		hidden []int64
	}{
		{"pages/aitooldetail/aitooldetail", []int64{2, 404}},
		{"pages/aitoolworkbench/aitoolworkbench", []int64{2, 404}},
		{"pages/inspirationdetail/inspirationdetail", []int64{2, 404}},
		{"pages/utilitytooldetail/utilitytooldetail", []int64{2, 404}},
		{"pages/templatesquaredetails/templatesquaredetails", []int64{2, 3, 404}},
		{"pages/designerhome/designerhome", []int64{2, 3, 404}},
	} {
		t.Run(tc.route, func(t *testing.T) {
			for _, id := range tc.hidden {
				if _, err := resolve(context.Background(), tc.route, id); err == nil {
					t.Fatalf("accepted hidden or missing source %d", id)
				}
			}
			metadata, err := resolve(context.Background(), tc.route, 1)
			if err != nil || metadata.Title != "公共庭院" || metadata.VersionSeed == "" {
				t.Fatalf("public source metadata=%+v error=%v", metadata, err)
			}
			serialized, err := json.Marshal(metadata)
			if err != nil || strings.Contains(string(serialized), "private-") {
				t.Fatalf("private data escaped allowlist: %s error=%v", serialized, err)
			}
		})
	}
}

// Query fixtures all use one tenant/owner and in-window facts. These rows test
// callback transaction plumbing; SQL predicate enforcement has separate tests.
type legacyTxRows struct {
	values [][]driver.Value
	i      int
}

func (*legacyTxRows) Columns() []string {
	return []string{"kind", "record_id", "owner_id", "visitor_key", "session_id", "content_id", "share_id", "occurred_at", "payload"}
}
func (*legacyTxRows) Close() error { return nil }
func (r *legacyTxRows) Next(values []driver.Value) error {
	if r.i >= len(r.values) {
		return io.EOF
	}
	copy(values, r.values[r.i])
	r.i++
	return nil
}

func TestPublishedReaderOperatorReusesTrackingTransactionWithOneConnection(t *testing.T) {
	state := &legacyTxState{}
	db := sql.OpenDB(legacyTxConnector{state})
	db.SetMaxOpenConns(1)
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.WithValue(context.Background(), legacyTxContextKey{}, "request"), 3*time.Second)
	defer cancel()
	store := &tracking.SQLStore{DB: db}
	route := "pages/contentreader/contentreader"
	s := tracking.New(store, tracking.Config{
		Enabled: true, ContentEnabled: true, Tenant: "org-a", DefaultOwner: "owner-a", ConsentVersion: "v1",
		Routes: map[string]tracking.Route{route: {Title: "公开素材", Public: true}},
	})
	// Install the production callback, not a test approximation of the grant rules.
	registerTrackingMaterialRoutes(gin.New().Group("/public"), s, model.NewUserModel(db), nil)
	content := tracking.Content{ID: "content-published", Version: 1, Owner: "owner-a", Status: "published"}
	payload, err := json.Marshal(content)
	if err != nil {
		t.Fatal(err)
	}
	if err = store.Transaction(ctx, "org-a", func(r tracking.Repository) error {
		if e := r.Insert(tracking.Record{Kind: "content", ID: content.ID, Owner: content.Owner, At: s.Now(), Payload: payload}); e != nil {
			return e
		}
		return r.Insert(tracking.Record{Kind: "version", ID: content.ID + ":1", Owner: content.Owner, At: s.Now(), Payload: payload})
	}); err != nil {
		t.Fatal(err)
	}
	entry, err := s.Enter(ctx, tracking.EntryInput{Route: route, Content: content.ID, Version: 1, Client: "operator-client", Consent: "v1"}, 7)
	if err != nil || !entry.Self || entry.Owner != "owner-a" {
		t.Fatalf("operator classification with one connection: entry=%+v error=%v", entry, err)
	}
	if state.opens != 1 || state.sources != 1 || state.commits != 2 || state.badSource || !state.requestContext || db.Stats().WaitCount != 0 {
		t.Fatalf("operator lookup escaped transaction: state=%+v stats=%+v", state, db.Stats())
	}
}

func TestBusinessRecomputeReusesTrackingTransactionWithOneConnection(t *testing.T) {
	for _, action := range []string{"favorite", "consultation"} {
		t.Run(action, func(t *testing.T) {
			state := &legacyTxState{}
			db := sql.OpenDB(legacyTxConnector{state})
			db.SetMaxOpenConns(1)
			defer db.Close()
			ctx, cancel := context.WithTimeout(context.WithValue(context.Background(), legacyTxContextKey{}, "request"), 3*time.Second)
			defer cancel()
			store := &tracking.SQLStore{DB: db}
			s := tracking.New(store, tracking.Config{Enabled: true, IntentEnabled: true, Tenant: "org-a", DefaultOwner: "owner-a"})
			s.VerifyBusiness = makeTrackingBusinessVerifier(db)
			occurred := s.Now().Add(-time.Minute)
			detail := map[string]string{"action": action, "target_type": "template", "target_id": "12", "favorite_receipt_id": "101", "favorite_created_at": occurred.UTC().Format(time.RFC3339Nano)}
			if action == "consultation" {
				detail = map[string]string{"action": action, "business_record_id": "lead:12"}
			}
			payload, err := json.Marshal(detail)
			if err != nil {
				t.Fatal(err)
			}
			if err = store.Transaction(ctx, "org-a", func(r tracking.Repository) error {
				return r.Insert(tracking.Record{Kind: "business", ID: "business-source", Owner: "owner-a", Visitor: "u:7", At: occurred, Payload: payload})
			}); err != nil {
				t.Fatal(err)
			}
			result, err := s.RecomputeIntents(ctx,
				tracking.Scope{Tenant: "org-a", Actor: 7, Owners: []string{"owner-a"}, View: true, Rules: true},
				tracking.RecomputeInput{RequestID: "recompute-source", Version: tracking.RuleVersion, Reason: "验证业务源事务读取"})
			if err != nil || result.Evaluated != 1 {
				t.Fatalf("business verification with one connection: result=%+v error=%v", result, err)
			}
			if state.opens != 1 || state.sources != 1 || state.commits != 2 || state.badSource || !state.requestContext || db.Stats().WaitCount != 0 {
				t.Fatalf("business lookup escaped transaction: state=%+v stats=%+v", state, db.Stats())
			}
		})
	}
}
