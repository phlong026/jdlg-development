package notification

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"strings"
	"testing"
	"time"
)

// Reuse the existing transaction/exec recorder and add only materialization reads.
type presentationConnector struct {
	state       *collectionCancelState
	merge       bool
	mergeResult driver.Result
	tx          *presentationTx
}

func (c presentationConnector) Driver() driver.Driver { return collectionCancelDriver{} }
func (c presentationConnector) Connect(context.Context) (driver.Conn, error) {
	return presentationConn{collectionCancelConn: collectionCancelConn{state: c.state}, merge: c.merge, mergeResult: c.mergeResult, tx: c.tx}, nil
}

type presentationConn struct {
	collectionCancelConn
	merge       bool
	mergeResult driver.Result
	tx          *presentationTx
}

type presentationTx struct{ commits, rollbacks int }

func (tx *presentationTx) Commit() error   { tx.commits++; return nil }
func (tx *presentationTx) Rollback() error { tx.rollbacks++; return nil }
func (c presentationConn) BeginTx(ctx context.Context, options driver.TxOptions) (driver.Tx, error) {
	if c.tx != nil {
		return c.tx, nil
	}
	return c.collectionCancelConn.BeginTx(ctx, options)
}
func (c presentationConn) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	result, err := c.collectionCancelConn.ExecContext(ctx, query, args)
	if strings.Contains(query, "UPDATE mini_notifications SET") && c.mergeResult != nil {
		return c.mergeResult, err
	}
	return result, err
}

type presentationRowsError struct{}

func (presentationRowsError) LastInsertId() (int64, error) { return 0, nil }
func (presentationRowsError) RowsAffected() (int64, error) {
	return 0, errors.New("rows affected unavailable")
}

func TestSQLMergeConflictRollsBackWithoutProcessingOrDelivery(t *testing.T) {
	for _, tc := range []struct {
		name     string
		result   driver.Result
		conflict bool
	}{
		{"read_or_cancelled_after_lookup", driver.RowsAffected(0), true},
		{"rows_affected_error", presentationRowsError{}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			state, tx := &collectionCancelState{}, &presentationTx{}
			db := sql.OpenDB(presentationConnector{state: state, merge: true, mergeResult: tc.result, tx: tx})
			defer db.Close()
			store, err := NewSQLStore(db, "tenant-a")
			if err != nil {
				t.Fatal(err)
			}
			store.SetBrowseMergeWindow(time.Hour)
			event := browseEvent("event-racing-merge", 42)
			err = store.MaterializeEvent(context.Background(), &EventJob{Event: event, LeaseToken: "lease"}, "official_account", Eligibility{Eligible: true, DeliveryEligible: true, BindingVersion: 1})
			if err == nil || tc.conflict && !errors.Is(err, ErrConflict) || !tc.conflict && !strings.Contains(err.Error(), "materialize_merge_rows") {
				t.Fatalf("unexpected error: %v", err)
			}
			if tx.commits != 0 || tx.rollbacks != 1 || len(state.queries) != 1 || !strings.Contains(state.queries[0], "UPDATE mini_notifications SET") {
				t.Fatalf("merge failure must roll back without marking event processed or creating delivery: tx=%+v writes=%v", tx, state.queries)
			}
		})
	}
}

func (c presentationConn) QueryContext(_ context.Context, query string, _ []driver.NamedValue) (driver.Rows, error) {
	if strings.Contains(query, "SELECT status,lease_token") {
		return &presentationRows{columns: []string{"status", "lease_token"}, values: []driver.Value{"processing", "lease"}}, nil
	}
	if strings.Contains(query, "SELECT notification_id") {
		rows := &presentationRows{columns: []string{"notification_id"}}
		if c.merge {
			rows.values = []driver.Value{"notice-original"}
		}
		return rows, nil
	}
	return nil, errors.New("unexpected materialization query")
}

type presentationRows struct {
	columns []string
	values  []driver.Value
}

func (r *presentationRows) Columns() []string { return r.columns }
func (*presentationRows) Close() error        { return nil }
func (r *presentationRows) Next(dest []driver.Value) error {
	if r.values == nil {
		return io.EOF
	}
	copy(dest, r.values)
	r.values = nil
	return nil
}

func TestSQLMaterializesPresentationAndMergesLatestFields(t *testing.T) {
	for _, merge := range []bool{false, true} {
		t.Run(map[bool]string{false: "insert", true: "merge"}[merge], func(t *testing.T) {
			state := &collectionCancelState{}
			db := sql.OpenDB(presentationConnector{state: state, merge: merge})
			defer db.Close()
			store, err := NewSQLStore(db, "tenant-a")
			if err != nil {
				t.Fatal(err)
			}
			store.SetBrowseMergeWindow(time.Hour)
			event := browseEvent("event-new", 42)
			event.ContentID = "content-new"
			body := strings.Repeat("新🙂", 300)
			if err := store.MaterializeEvent(context.Background(), &EventJob{Event: event, LeaseToken: "lease"}, "official_account", Eligibility{Eligible: true, Body: body}); err != nil {
				t.Fatal(err)
			}
			if len(state.queries) != 2 {
				t.Fatalf("unexpected materialization writes: %v", state.queries)
			}
			query, args := state.queries[0], state.args[0]
			wantBody := strings.Repeat("新🙂", 256)
			if merge {
				if !strings.Contains(query, "SET created_at=?, event_id=?, title=?, body=?, route_key=?, content_id=?") || len(args) != 8 || args[0].Value != event.OccurredAt || args[1].Value != event.ID || args[2].Value != noticeFromEvent(event).Title || args[3].Value != wantBody || args[4].Value != event.Route || args[5].Value != event.ContentID || args[7].Value != "notice-original" {
					t.Fatalf("incoherent merge: query=%s args=%v", query, args)
				}
			} else if !strings.Contains(query, "INSERT INTO mini_notifications") || len(args) != 11 || args[6].Value != wantBody {
				t.Fatalf("presentation not persisted: query=%s args=%v", query, args)
			}
			for _, write := range state.queries {
				if strings.Contains(write, "mini_notification_deliveries") {
					t.Fatalf("inbox-only/merge created delivery: %s", write)
				}
			}
		})
	}
}
