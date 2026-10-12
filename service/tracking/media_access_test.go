package tracking

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
)

// Exercises production SQLStore/budgets with deterministic database/sql rows;
// this is not a live MySQL or query-optimizer/load test.
type mediaReproState struct {
	payloadRows, payloadBytes int
	query                     string
	args                      []driver.NamedValue
}
type mediaReproConnector struct {
	versions int
	state    *mediaReproState
}

func (c mediaReproConnector) Connect(context.Context) (driver.Conn, error) {
	return mediaReproConn{c.versions, c.state}, nil
}
func (c mediaReproConnector) Driver() driver.Driver { return scopedSQLDriver{} }

type mediaReproConn struct {
	versions int
	state    *mediaReproState
}

func (c mediaReproConn) Prepare(string) (driver.Stmt, error) { return nil, errors.New("unexpected") }
func (c mediaReproConn) Close() error                        { return nil }
func (c mediaReproConn) Begin() (driver.Tx, error)           { return mediaReproTx{}, nil }
func (c mediaReproConn) BeginTx(context.Context, driver.TxOptions) (driver.Tx, error) {
	return c.Begin()
}
func (c mediaReproConn) ExecContext(context.Context, string, []driver.NamedValue) (driver.Result, error) {
	return driver.RowsAffected(1), nil
}
func (c mediaReproConn) QueryContext(_ context.Context, q string, args []driver.NamedValue) (driver.Rows, error) {
	if strings.HasPrefix(q, "SELECT EXISTS (") {
		c.state.query = q
		c.state.args = append([]driver.NamedValue{}, args...)
		return &mediaReproRows{exists: true, n: 1}, nil
	}
	if strings.HasPrefix(q, "SELECT tenant_id") {
		return &mediaReproRows{lock: true, n: 1}, nil
	}
	kind := args[2].Value.(string)
	n := 1
	if kind == "content_revoked" {
		n = 0
	}
	if strings.Contains(q, "FROM tracking_records r WHERE") {
		n = c.versions
	}
	return &mediaReproRows{kind: kind, n: n, state: c.state}, nil
}

type mediaReproTx struct{}

func (mediaReproTx) Commit() error   { return nil }
func (mediaReproTx) Rollback() error { return nil }

type mediaReproRows struct {
	state        *mediaReproState
	lock, exists bool
	kind         string
	n, i         int
}

func (r *mediaReproRows) Columns() []string {
	if r.lock {
		return []string{"tenant"}
	}
	if r.exists {
		return []string{"referenced"}
	}
	return []string{"kind", "record_id", "owner_id", "visitor_key", "session_id", "content_id", "share_id", "occurred_at", "payload"}
}
func (r *mediaReproRows) Close() error { return nil }
func (r *mediaReproRows) Next(out []driver.Value) error {
	if r.i >= r.n {
		return io.EOF
	}
	r.i++
	if r.lock {
		out[0] = "org-a"
		return nil
	}
	if r.exists {
		out[0] = int64(1)
		return nil
	}
	asset := Asset{ID: "0123456789abcdefghijklmnopqrstuv", Kind: "image", URL: "/api/v1/miniprogram/tracking/media/0123456789abcdefghijklmnopqrstuv", Width: 1, Height: 1, MIME: "image/png"}
	content := Content{ID: "content-target", Version: 1, Status: "published", Owner: "owner-a", Body: "small article", Media: []Asset{asset}}
	var payload []byte
	if r.kind == "media" {
		payload, _ = json.Marshal(asset)
	} else {
		if r.kind == "version" && r.i > 1 {
			content.ID = fmt.Sprintf("unrelated-content-%06d", r.i)
			content.Owner = "owner-a"
			if r.n > 1000 {
				content.Body = "x"
			} else {
				content.Body = strings.Repeat("x", 60000)
			}
			content.Media = nil
		}
		payload, _ = json.Marshal(content)
	}
	if r.state != nil {
		r.state.payloadRows++
		r.state.payloadBytes += len(payload)
	}
	out[0], out[1], out[2], out[3], out[4], out[5], out[6], out[7], out[8] = r.kind, fmt.Sprintf("record-%06d", r.i), content.Owner, "", "", "", "", "2026-10-11 00:00:00.000000", payload
	return nil
}
func TestPublicMediaSQLDoesNotLoadUnrelatedVersions(t *testing.T) {
	for _, n := range []int{1, 560, 20002} {
		t.Run(fmt.Sprintf("versions-%d", n), func(t *testing.T) {
			state := &mediaReproState{}
			db := sql.OpenDB(mediaReproConnector{n, state})
			defer db.Close()
			s := New(&SQLStore{DB: db}, Config{Tenant: "org-a", ContentEnabled: true, Routes: map[string]Route{publishedReaderRoute: {Public: true}}})
			if _, err := s.ReadContent(context.Background(), "content-target", 1); err != nil {
				t.Fatal(err)
			}
			state.payloadRows, state.payloadBytes = 0, 0
			asset, err := s.Media(context.Background(), nil, "0123456789abcdefghijklmnopqrstuv")
			if err != nil || asset.ID == "" {
				t.Fatalf("unrelated versions must not disable media: %v", err)
			}
			if state.payloadRows != 1 || state.payloadBytes > 2048 {
				t.Fatalf("unrelated bodies materialized: rows=%d bytes=%d", state.payloadRows, state.payloadBytes)
			}
			t.Logf("published versions=%d; payload rows=%d bytes=%d", n, state.payloadRows, state.payloadBytes)
		})
	}
}
func TestPublicMediaSQLPredicateScope(t *testing.T) {
	state := &mediaReproState{}
	db := sql.OpenDB(mediaReproConnector{1, state})
	defer db.Close()
	s := New(&SQLStore{DB: db}, Config{Tenant: "org-a", ContentEnabled: true})
	id := "0123456789abcdefghijklmnopqrstuv"
	if _, err := s.Media(context.Background(), nil, id); err != nil {
		t.Fatal(err)
	}
	for _, part := range []string{"v.tenant_id=?", "v.owner_id=?", "m.tenant_id=v.tenant_id", "m.owner_id=v.owner_id", "revoked.tenant_id=v.tenant_id", "revoked.kind='content_revoked'", "JSON_CONTAINS", "$.media", "$.cover.media_id", "$.status", "$.owner_id"} {
		if !strings.Contains(state.query, part) {
			t.Errorf("missing authorization predicate %s", part)
		}
	}
	if strings.Contains(state.query, id) {
		t.Fatal("asset interpolated")
	}
	if len(state.args) != 4 || state.args[0].Value != "org-a" || state.args[1].Value != "owner-a" || state.args[3].Value != id {
		t.Fatal(state.args)
	}
	var needle map[string]string
	if err := json.Unmarshal([]byte(state.args[2].Value.(string)), &needle); err != nil || needle["media_id"] != id {
		t.Fatal(needle, err)
	}
}

func TestPublicMediaCurrentAuthorizationAndHistoricalReferences(t *testing.T) {
	for _, test := range []struct {
		name   string
		change func(*Content, *Content, *Record)
		marker bool
		want   bool
	}{
		{name: "historical published reference and current draft", want: true},
		{name: "revoked master", change: func(_ *Content, m *Content, _ *Record) { m.Status = "revoked" }},
		{name: "revocation tombstone", marker: true},
		{name: "different version owner", change: func(v *Content, _ *Content, _ *Record) { v.Owner = "owner-b" }},
		{name: "different master owner", change: func(_ *Content, m *Content, _ *Record) { m.Owner = "owner-b" }},
		{name: "different asset owner", change: func(_ *Content, _ *Content, a *Record) { a.Owner = "owner-b" }},
		{name: "unpublished reference", change: func(v *Content, _ *Content, _ *Record) { v.Status = "draft" }},
		{name: "no asset reference", change: func(v *Content, _ *Content, _ *Record) { v.Media = nil }},
		{name: "cover reference", change: func(v *Content, _ *Content, _ *Record) {
			a := Asset{ID: "0123456789abcdefghijklmnopqrstuv"}
			v.Cover = &a
			v.Media = nil
		}, want: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			s, _ := fixture(t)
			a := Asset{ID: "0123456789abcdefghijklmnopqrstuv", Kind: "image"}
			v := Content{ID: "historical-content", Version: 1, Status: "published", Owner: "owner-a", Media: []Asset{a}}
			master := v
			master.Status = "draft"
			master.Version = 2
			master.Media = nil
			ar := record("media", a.ID, "owner-a", s.Now(), a)
			if test.change != nil {
				test.change(&v, &master, &ar)
			}
			err := s.transaction(context.Background(), func(r Repository) error {
				r.Put(ar)
				r.Put(record("version", contentKey(v.ID, 1), v.Owner, s.Now(), v))
				r.Put(record("content", master.ID, master.Owner, s.Now(), master))
				if test.marker {
					r.Put(record("content_revoked", master.ID, master.Owner, s.Now(), map[string]bool{"revoked": true}))
				}
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			got, err := s.Media(context.Background(), nil, a.ID)
			if test.want {
				if err != nil || got.ID != a.ID {
					t.Fatal(got, err)
				}
			} else {
				requireQueryError(t, err, 404)
			}
		})
	}
}
