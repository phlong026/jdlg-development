package route

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"service/model"
	"service/tracking"
	"strings"
	"testing"
	"time"
)

type profileSQLState struct {
	legacy    legacyTxState
	profiles  map[int64][3]string
	query     string
	args      []driver.NamedValue
	batches   int
	fail      error
	duplicate bool
	extra     bool
	closed    bool
}
type profileSQLConnector struct{ state *profileSQLState }

func (c profileSQLConnector) Connect(context.Context) (driver.Conn, error) {
	c.state.legacy.opens++
	return &profileSQLConn{legacyTxConn: &legacyTxConn{state: &c.state.legacy}, profile: c.state}, nil
}
func (profileSQLConnector) Driver() driver.Driver { return legacyTestDriver{} }

type profileSQLConn struct {
	*legacyTxConn
	profile *profileSQLState
}

func (c *profileSQLConn) QueryContext(ctx context.Context, q string, args []driver.NamedValue) (driver.Rows, error) {
	if !strings.Contains(q, "FROM user_profiles p JOIN users u") {
		return c.legacyTxConn.QueryContext(ctx, q, args)
	}
	c.profile.batches++
	c.profile.query = q
	c.profile.args = append([]driver.NamedValue{}, args...)
	if c.pending == nil || ctx.Value(legacyTxContextKey{}) != "request" {
		return nil, errors.New("profile source escaped transaction or request context")
	}
	if c.profile.fail != nil {
		return nil, c.profile.fail
	}
	rows := &profileSQLRows{state: c.profile}
	for _, arg := range args[:len(args)-1] {
		id := arg.Value.(int64)
			if p, ok := c.profile.profiles[id]; ok {
				rows.values = append(rows.values, []driver.Value{id, p[0], p[1], p[2]})
			}
		}
		if c.profile.duplicate && len(rows.values) > 0 {
			rows.values = append(rows.values, rows.values[0])
		}
		if c.profile.extra {
			rows.values = append(rows.values, []driver.Value{int64(9999), "unrequested", "", ""})
		}
	return rows, nil
}

type profileSQLRows struct {
	state  *profileSQLState
	values [][]driver.Value
	i      int
}

func (*profileSQLRows) Columns() []string { return []string{"user_id", "nickname", "avatar", "identity_type"} }
func (r *profileSQLRows) Close() error    { r.state.closed = true; return nil }
func (r *profileSQLRows) Next(v []driver.Value) error {
	if r.i >= len(r.values) {
		return io.EOF
	}
	copy(v, r.values[r.i])
	r.i++
	return nil
}

func TestTrackingProfilesUseMinimalBatchSQLAndOneConnectionTransaction(t *testing.T) {
	state := &profileSQLState{profiles: map[int64][3]string{1: {"真实账户昵称", "https://public.example/avatar.png", "乡墅业主"}, 2: {"", "wxfile://local", "设计师"}, 3: {"跨权昵称", "", ""}, 4: {"已删除昵称", "", "施工队"}}}
	db := sql.OpenDB(profileSQLConnector{state})
	defer db.Close()
	db.SetMaxOpenConns(1)
	ctx, cancel := context.WithTimeout(context.WithValue(context.Background(), legacyTxContextKey{}, "request"), 3*time.Second)
	defer cancel()
	store := &tracking.SQLStore{DB: db}
	s := tracking.New(store, tracking.Config{Enabled: true, Tenant: "org-a", Routes: map[string]tracking.Route{"page": {Title: "页面", Public: true}}})
	s.ResolveVisitorProfiles = makeTrackingProfileResolver(db)
	at := time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)
	s.Now = func() time.Time { return at }
	err := store.Transaction(ctx, "org-a", func(r tracking.Repository) error {
		for i, key := range []string{"u:1", "u:2", "a:anonymous", "u:3", "u:4"} {
			id := fmt.Sprintf("profile-view-%d", i)
			session := fmt.Sprintf("profile-session-%d", i)
			owner := "owner-a"
			if key == "u:3" {
				owner = "owner-b"
			}
			ev := tracking.Event{ID: id, Type: "page_view", View: id, Session: session, Route: "page", Occurred: at.Add(-time.Hour), Received: at.Add(-time.Hour)}
			payload, _ := json.Marshal(ev)
			if err := r.Insert(tracking.Record{Kind: "event", ID: id, Owner: owner, Visitor: key, Session: session, At: ev.Occurred, Payload: payload}); err != nil {
				return err
			}
		}
		return r.Insert(tracking.Record{Kind: "deletion", ID: "u:4", At: at, Payload: []byte(`{}`)})
	})
	if err != nil {
		t.Fatal(err)
	}
	scope := tracking.Scope{Tenant: "org-a", Actor: 7, Owners: []string{"owner-a"}, View: true}
	got, err := s.Visitors(ctx, scope, tracking.Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if got.Identified != 2 || got.Anonymous != 1 || state.batches != 1 || state.legacy.opens != 1 || db.Stats().WaitCount != 0 || !state.closed {
		t.Fatal(got, state, db.Stats())
	}
	args := []any{}
	for _, arg := range state.args {
		args = append(args, arg.Value)
	}
	if !reflect.DeepEqual(args, []any{int64(1), int64(2), int64(3)}) {
		t.Fatal(args)
	}
	if !strings.Contains(state.query, "WHERE p.user_id IN (?,?) ORDER BY p.user_id LIMIT ?") {
		t.Fatal(state.query)
	}
	for _, forbidden := range []string{"phone", "password", "device", "openid", "unionid", "SELECT *"} {
		if strings.Contains(state.query, forbidden) {
			t.Fatalf("excess profile field %s: %s", forbidden, state.query)
		}
	}
	by := map[string]tracking.Visitor{}
	for _, v := range got.Items.([]tracking.Visitor) {
		by[v.Key] = v
	}
	if by["u:1"].Nickname != "真实账户昵称" || by["u:1"].AvatarURL != "https://public.example/avatar.png" || by["u:2"].AvatarURL != "" {
		t.Fatal(by)
	}
	if by["u:1"].IdentityType != "乡墅业主" || by["u:2"].IdentityType != "设计师" || by["a:anonymous"].IdentityType != "" {
		t.Fatal("identity type not carried to visitor projection", by)
	}
	state.fail = errors.New("isolated profile SQL failure")
	if _, err = s.Visitors(ctx, scope, tracking.Filter{Search: "名字"}); !errors.Is(err, state.fail) {
		t.Fatal("source failure swallowed", err)
	}
}
func TestProfileBatchModelReadBoundsAndUnexpectedRows(t *testing.T) {
	state := &profileSQLState{profiles: map[int64][3]string{1: {"昵称", "", "乡墅业主"}}}
	db := sql.OpenDB(profileSQLConnector{state})
	defer db.Close()
	ctx := context.WithValue(context.Background(), legacyTxContextKey{}, "request")
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	for _, ids := range [][]int64{{0}, {-1}, {1, 1}, make([]int64, 201)} {
		if _, err = model.GetUserProfilePresentations(ctx, tx, ids); err == nil {
			t.Fatal("invalid batch accepted", ids)
		}
	}
	if state.batches != 0 {
		t.Fatal("invalid batches reached SQL")
	}
	empty, err := model.GetUserProfilePresentations(ctx, tx, nil)
	if err != nil || len(empty) != 0 || state.batches != 0 {
		t.Fatal(empty, err)
	}
	missing, err := model.GetUserProfilePresentations(ctx, tx, []int64{404})
	if err != nil || len(missing) != 0 {
		t.Fatal(missing, err)
	}
	state.duplicate = true
	if _, err = model.GetUserProfilePresentations(ctx, tx, []int64{1}); err == nil {
		t.Fatal("duplicate source accepted")
	}
	state.duplicate = false
	state.extra = true
	if _, err = model.GetUserProfilePresentations(ctx, tx, []int64{1}); err == nil {
		t.Fatal("unrequested source accepted")
	}
	if !state.closed {
		t.Fatal("profile rows not closed")
	}
}
