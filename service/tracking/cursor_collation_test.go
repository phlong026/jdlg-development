package tracking

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"sort"
	"strings"
	"testing"
	"time"
)

type cursorCollationConnector struct{ records []Record }

type cursorCollationDriver struct{}

type cursorCollationConn struct{ records []Record }

type cursorCollationTx struct{}

type cursorCollationRows struct {
	records []Record
	index   int
}

func (c cursorCollationConnector) Connect(context.Context) (driver.Conn, error) {
	return cursorCollationConn{records: c.records}, nil
}
func (cursorCollationConnector) Driver() driver.Driver { return cursorCollationDriver{} }
func (cursorCollationDriver) Open(string) (driver.Conn, error) {
	return nil, errors.New("use connector")
}
func (cursorCollationConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("unexpected prepare")
}
func (cursorCollationConn) Close() error              { return nil }
func (cursorCollationConn) Begin() (driver.Tx, error) { return cursorCollationTx{}, nil }
func (cursorCollationConn) BeginTx(context.Context, driver.TxOptions) (driver.Tx, error) {
	return cursorCollationTx{}, nil
}
func (cursorCollationTx) Commit() error   { return nil }
func (cursorCollationTx) Rollback() error { return nil }
func (c cursorCollationConn) QueryContext(_ context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	binaryOrder := strings.Contains(query, "BINARY r.record_id")
	rows := append([]Record(nil), c.records...)
	lessID := func(a, b string) bool {
		if binaryOrder {
			return a < b
		}
		return strings.ToLower(a) < strings.ToLower(b)
	}
	sort.Slice(rows, func(i, j int) bool { return lessID(rows[i].ID, rows[j].ID) })
	if strings.Contains(query, "r.record_id") && strings.Contains(query, "occurred_at>?") {
		cursor := args[len(args)-2].Value.(string)
		kept := rows[:0:0]
		for _, row := range rows {
			if lessID(cursor, row.ID) {
				kept = append(kept, row)
			}
		}
		rows = kept
	}
	limit := int(args[len(args)-1].Value.(int64))
	if len(rows) > limit {
		rows = rows[:limit]
	}
	return &cursorCollationRows{records: rows}, nil
}
func (r *cursorCollationRows) Columns() []string {
	return []string{"kind", "record_id", "owner_id", "visitor_key", "session_id", "content_id", "share_id", "occurred_at", "payload"}
}
func (r *cursorCollationRows) Close() error { return nil }
func (r *cursorCollationRows) Next(values []driver.Value) error {
	if r.index >= len(r.records) {
		return io.EOF
	}
	row := r.records[r.index]
	r.index++
	values[0], values[1], values[2], values[3] = row.Kind, row.ID, row.Owner, row.Visitor
	values[4], values[5], values[6] = row.Session, row.Content, row.Share
	values[7], values[8] = row.At.UTC().Format("2006-01-02 15:04:05.999999"), []byte(`{}`)
	return nil
}

func TestSQLCursorMatchesGoRecordIDOrderAcrossCase(t *testing.T) {
	at := time.Date(2026, 10, 22, 1, 0, 0, 0, time.UTC)
	db := sql.OpenDB(cursorCollationConnector{records: []Record{
		record("summary_state", "aaaaaaaa", "owner-a", at, nil),
		record("summary_state", "ZZZZZZZZ", "owner-a", at, nil),
	}})
	defer db.Close()
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	repo := &sqlRepo{ctx: context.Background(), tx: tx, tenant: "tenant"}

	seen := []string{}
	q := RecordQuery{Limit: 2}
	for {
		page, err := pageRecords(repo, "summary_state", q)
		if err != nil {
			t.Fatal(err)
		}
		hasMore := len(page) > 1
		if hasMore {
			page = page[:1]
		}
		for _, row := range page {
			seen = append(seen, row.ID)
		}
		if !hasMore {
			break
		}
		q = cursorAfter(page, q)
	}
	if strings.Join(seen, ",") != "ZZZZZZZZ,aaaaaaaa" {
		t.Fatalf("cursor skipped mixed-case record IDs: %v", seen)
	}
}
