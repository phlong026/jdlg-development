package route

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"service/tracking"
	"strings"
	"testing"
	"time"
)

var favoriteSourceCreated = time.Date(2026, 10, 11, 10, 0, 0, 0, time.UTC)

type businessVerifierDriver struct{}
type businessVerifierConn struct{ fail bool }
type businessVerifierRows struct{ found, done bool }

func (businessVerifierDriver) Open(name string) (driver.Conn, error) {
	return businessVerifierConn{fail: name == "error"}, nil
}
func (businessVerifierConn) Prepare(string) (driver.Stmt, error) { return nil, driver.ErrSkip }
func (businessVerifierConn) Close() error                        { return nil }
func (businessVerifierConn) Begin() (driver.Tx, error)           { return nil, driver.ErrSkip }
func (c businessVerifierConn) QueryContext(ctx context.Context, q string, args []driver.NamedValue) (driver.Rows, error) {
	if e := ctx.Err(); e != nil {
		return nil, e
	}
	if c.fail {
		return nil, errors.New("private database diagnostic")
	}
	rows := &businessVerifierRows{}
	switch q {
	case "SELECT 1 FROM user_favorites WHERE user_id=? AND target_type=? AND target_id=? AND id=? AND created_at=? LIMIT 1":
		rows.found = len(args) == 5 && args[0].Value == int64(7) && args[1].Value == "template" && args[2].Value == int64(12) && args[3].Value == int64(101) && args[4].Value == favoriteSourceCreated
	case "SELECT 1 FROM customer_leads WHERE id=? AND user_id=? AND status IN ('new','contacted','converted') LIMIT 1":
		// ID 13 represents an invalidated lead; 404 represents deletion.
		rows.found = len(args) == 2 && args[0].Value == int64(12) && args[1].Value == int64(7)
	default:
		return nil, errors.New("query must use the approved identifier-only predicates")
	}
	return rows, nil
}
func (r *businessVerifierRows) Columns() []string { return []string{"exists"} }
func (r *businessVerifierRows) Close() error      { return nil }
func (r *businessVerifierRows) Next(dest []driver.Value) error {
	if r.done || !r.found {
		return io.EOF
	}
	r.done = true
	dest[0] = int64(1)
	return nil
}
func init() { sql.Register("tracking-business-source", businessVerifierDriver{}) }
func TestBusinessVerifierChecksRealSourceOwnerAndValidity(t *testing.T) {
	db, e := sql.Open("tracking-business-source", "")
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	verify := makeTrackingBusinessVerifier(db)
	tests := []struct {
		p     tracking.BusinessProof
		valid bool
	}{
		{tracking.BusinessProof{Action: "favorite", Account: 7, TargetType: "template", TargetID: 12, FavoriteReceiptID: 101, FavoriteCreatedAt: favoriteSourceCreated}, true},
		{tracking.BusinessProof{Action: "favorite", Account: 8, TargetType: "template", TargetID: 12, FavoriteReceiptID: 101, FavoriteCreatedAt: favoriteSourceCreated}, false},
		{tracking.BusinessProof{Action: "favorite", Account: 7, TargetType: "template", TargetID: 404, FavoriteReceiptID: 101, FavoriteCreatedAt: favoriteSourceCreated}, false},
		{tracking.BusinessProof{Action: "favorite", Account: 7, TargetType: "arbitrary_table", TargetID: 12, FavoriteReceiptID: 101, FavoriteCreatedAt: favoriteSourceCreated}, false},
		{tracking.BusinessProof{Action: "favorite", Account: 7, TargetType: "template", TargetID: 12}, false},
		{tracking.BusinessProof{Action: "favorite", Account: 7, TargetType: "template", TargetID: 12, FavoriteReceiptID: 100, FavoriteCreatedAt: favoriteSourceCreated}, false},
		{tracking.BusinessProof{Action: "favorite", Account: 7, TargetType: "template", TargetID: 12, FavoriteReceiptID: 101, FavoriteCreatedAt: favoriteSourceCreated.Add(time.Second)}, false},
		{tracking.BusinessProof{Action: "consultation", Account: 7, TargetType: "lead", TargetID: 12}, true},
		{tracking.BusinessProof{Action: "consultation", Account: 7, TargetType: "lead", TargetID: 13}, false},
		{tracking.BusinessProof{Action: "consultation", Account: 7, TargetType: "lead", TargetID: 404}, false},
		{tracking.BusinessProof{Action: "consultation", Account: 8, TargetType: "lead", TargetID: 12}, false},
		{tracking.BusinessProof{Action: "consultation", Account: 7, TargetType: "template", TargetID: 12}, false},
		{tracking.BusinessProof{Action: "appointment", Account: 7, TargetType: "lead", TargetID: 12}, false},
		{tracking.BusinessProof{Action: "quote", Account: 7, TargetType: "lead", TargetID: 12}, false},
	}
	for _, tc := range tests {
		valid, e := verify(context.Background(), tc.p)
		if e != nil || valid != tc.valid {
			t.Fatalf("%+v => %v %v", tc.p, valid, e)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if valid, e := verify(ctx, tests[0].p); e == nil || valid {
		t.Fatal("cancelled caller context ignored")
	}
}
func TestBusinessVerifierFailureIsClosedAndRedacted(t *testing.T) {
	db, e := sql.Open("tracking-business-source", "error")
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	proof := tracking.BusinessProof{Action: "favorite", Account: 7, TargetType: "template", TargetID: 12, FavoriteReceiptID: 101, FavoriteCreatedAt: favoriteSourceCreated}
	for _, verify := range []func(context.Context, tracking.BusinessProof) (bool, error){makeTrackingBusinessVerifier(db), makeTrackingBusinessVerifier(nil)} {
		valid, e := verify(context.Background(), proof)
		if valid || !errors.Is(e, tracking.ErrUnavailable) || strings.Contains(e.Error(), "private") {
			t.Fatalf("fail-open/leaked source: %v %v", valid, e)
		}
	}
}
