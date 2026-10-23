package model

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"strings"
	"testing"
)

func TestNormalizeNickname(t *testing.T) {
	for _, raw := range []string{"", " \t\n", strings.Repeat("中", 33), string([]byte{0xff})} {
		if _, err := NormalizeNickname(raw); err == nil {
			t.Fatalf("accepted invalid nickname %q", raw)
		}
	}
	for _, raw := range []string{" 林间昵称 ", strings.Repeat("😀", 32)} {
		got, err := NormalizeNickname(raw)
		if err != nil || got != strings.TrimSpace(raw) {
			t.Fatalf("%q: %q %v", raw, got, err)
		}
	}
}

type profileReadFailure struct{ inserts int }

var errProfileReadFixture = errors.New("profile read fixture failure")

func (c *profileReadFailure) Driver() driver.Driver                        { return nil }
func (c *profileReadFailure) Connect(context.Context) (driver.Conn, error) { return c, nil }
func (c *profileReadFailure) Prepare(string) (driver.Stmt, error)          { return nil, errProfileReadFixture }
func (c *profileReadFailure) Close() error                                 { return nil }
func (c *profileReadFailure) Begin() (driver.Tx, error)                    { return nil, errProfileReadFixture }
func (c *profileReadFailure) QueryContext(context.Context, string, []driver.NamedValue) (driver.Rows, error) {
	return nil, errProfileReadFixture
}
func (c *profileReadFailure) ExecContext(context.Context, string, []driver.NamedValue) (driver.Result, error) {
	c.inserts++
	return nil, errors.New("unexpected insert")
}
func TestGetOrCreatePropagatesProfileReadFailure(t *testing.T) {
	fixture := &profileReadFailure{}
	db := sql.OpenDB(fixture)
	defer db.Close()
	_, err := NewUserProfileModel(db).GetOrCreate(1, "")
	if !errors.Is(err, errProfileReadFixture) || fixture.inserts != 0 {
		t.Fatalf("read failure became creation: %v inserts=%d", err, fixture.inserts)
	}
}
