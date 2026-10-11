package route

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"io"
	"strings"
	"testing"
	"time"
)

// This driver exercises the real model scan and route resolver, not a fake
// tracking resolver. It never opens a connection to a production database.
type legacyTestDriver struct{}
type legacyTestConn struct{}
type legacyTestRows struct {
	columns []string
	values  []driver.Value
	done    bool
}

func (legacyTestDriver) Open(string) (driver.Conn, error)  { return legacyTestConn{}, nil }
func (legacyTestConn) Prepare(string) (driver.Stmt, error) { return nil, driver.ErrSkip }
func (legacyTestConn) Close() error                        { return nil }
func (legacyTestConn) Begin() (driver.Tx, error)           { return nil, driver.ErrSkip }
func (legacyTestConn) QueryContext(_ context.Context, q string, args []driver.NamedValue) (driver.Rows, error) {
	start := strings.Index(q, "SELECT ") + 7
	end := strings.Index(q, "FROM ")
	names := strings.Split(q[start:end], ",")
	r := &legacyTestRows{columns: names}
	id := args[0].Value.(int64)
	if id == 404 {
		return r, nil
	}
	for _, raw := range names {
		n := strings.TrimSpace(raw)
		var v driver.Value = ""
		switch n {
		case "id":
			v = id
		case "created_at", "updated_at":
			v = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
		case "name":
			v = "公共庭院"
		case "thumbnail":
			v = "https://public.example/cover.jpg"
		case "description":
			v = "公开描述\n\n提示词: private-prompt"
		case "internal_prompt":
			v = "private-system-prompt"
		case "status":
			v = "published"
			if id == 2 {
				v = "draft"
			}
		case "publish_scope":
			v = "square"
			if id == 3 {
				v = "private"
			}
		case "image_width":
			v = int64(900)
		case "image_height":
			v = int64(1600)
		case "price", "is_free", "is_featured", "download_count", "like_count", "creator_user_id", "original_task_id":
			v = int64(0)
		}
		r.values = append(r.values, v)
	}
	return r, nil
}
func (r *legacyTestRows) Columns() []string { return r.columns }
func (r *legacyTestRows) Close() error      { return nil }
func (r *legacyTestRows) Next(dest []driver.Value) error {
	if r.done || r.values == nil {
		return io.EOF
	}
	copy(dest, r.values)
	r.done = true
	return nil
}
func init() { sql.Register("tracking-legacy-test", legacyTestDriver{}) }
func TestLegacyTemplateResolverEnforcesPublicSource(t *testing.T) {
	db, e := sql.Open("tracking-legacy-test", "")
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	resolve := makeTrackingLegacyResolver(db)
	route := "pages/templatesquaredetails/templatesquaredetails"
	for _, id := range []int64{2, 3, 404} {
		if _, e := resolve(route, id); e == nil {
			t.Fatalf("accepted private/draft/missing %d", id)
		}
	}
	m, e := resolve(route, 1)
	if e != nil {
		t.Fatal(e)
	}
	if m.Title != "公共庭院" || m.Width != 900 || m.Height != 1600 || strings.Contains(m.VersionSeed, "private") {
		t.Fatalf("bad safe metadata %+v", m)
	}
}
