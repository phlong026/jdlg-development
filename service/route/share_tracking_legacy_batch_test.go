package route

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	"service/tracking"
)

type legacyBatchConnector struct{ queries int }

func (c *legacyBatchConnector) Connect(context.Context) (driver.Conn, error) {
	return legacyBatchConn{connector: c}, nil
}
func (*legacyBatchConnector) Driver() driver.Driver { return legacyTestDriver{} }

type legacyBatchConn struct{ connector *legacyBatchConnector }

func (legacyBatchConn) Prepare(string) (driver.Stmt, error) { return nil, driver.ErrSkip }
func (legacyBatchConn) Close() error                        { return nil }
func (legacyBatchConn) Begin() (driver.Tx, error)           { return nil, driver.ErrSkip }
func (c legacyBatchConn) QueryContext(_ context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	c.connector.queries++
	start := strings.Index(query, "SELECT ") + 7
	end := strings.Index(query, "FROM ")
	columns := legacySelectColumns(query[start:end])
	rows := make([][]driver.Value, 0, len(args))
	for _, arg := range args {
		id, ok := arg.Value.(int64)
		if !ok {
			return nil, fmt.Errorf("unexpected id %T", arg.Value)
		}
		values := make([]driver.Value, 0, len(columns))
		for _, raw := range columns {
			column := strings.TrimSpace(raw)
			var value driver.Value = ""
			switch column {
			case "id":
				value = id
			case "title":
				value = fmt.Sprintf("公共庭院%d", id)
			case "status":
				value = "published"
			case "image_width":
				value = int64(900)
			case "image_height":
				value = int64(1600)
			case "creator_user_id":
				value = nil
			case "view_count", "like_count", "sort_order":
				value = int64(0)
			case "created_at", "updated_at":
				value = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
			}
			values = append(values, value)
		}
		rows = append(rows, values)
	}
	return &legacyBatchRows{columns: columns, rows: rows}, nil
}

type legacyBatchRows struct {
	columns []string
	rows    [][]driver.Value
	index   int
}

func (r *legacyBatchRows) Columns() []string { return r.columns }
func (r *legacyBatchRows) Close() error      { return nil }
func (r *legacyBatchRows) Next(dest []driver.Value) error {
	if r.index >= len(r.rows) {
		return io.EOF
	}
	copy(dest, r.rows[r.index])
	r.index++
	return nil
}

func TestLegacyBatchResolverUsesOneQueryPerRoute(t *testing.T) {
	connector := &legacyBatchConnector{}
	db := sql.OpenDB(connector)
	defer db.Close()
	resolve := makeTrackingLegacyBatchResolver(db)
	route := "pages/inspirationdetail/inspirationdetail"
	contents := []string{tracking.LegacyID(route, 1), tracking.LegacyID(route, 2), tracking.LegacyID(route, 3)}

	got, err := resolve(context.Background(), route, contents)
	if err != nil {
		t.Fatal(err)
	}
	if connector.queries != 1 || len(got) != len(contents) {
		t.Fatalf("queries=%d metadata=%v", connector.queries, got)
	}
	for i, content := range contents {
		metadata := got[content]
		if metadata.Title != fmt.Sprintf("公共庭院%d", i+1) || metadata.VersionSeed == "" {
			t.Fatalf("metadata[%s] = %+v", content, metadata)
		}
	}
}
