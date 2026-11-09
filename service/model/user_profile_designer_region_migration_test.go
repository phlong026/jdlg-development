package model

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"strings"
	"testing"

	"github.com/go-sql-driver/mysql"
)

type designerRegionMigrationConnector struct{ migrationErr error }

func (s *designerRegionMigrationConnector) Driver() driver.Driver { return nil }
func (s *designerRegionMigrationConnector) Connect(context.Context) (driver.Conn, error) {
	return s, nil
}
func (*designerRegionMigrationConnector) Prepare(string) (driver.Stmt, error) {
	return nil, driver.ErrSkip
}
func (*designerRegionMigrationConnector) Close() error              { return nil }
func (*designerRegionMigrationConnector) Begin() (driver.Tx, error) { return nil, driver.ErrSkip }
func (s *designerRegionMigrationConnector) ExecContext(_ context.Context, query string, _ []driver.NamedValue) (driver.Result, error) {
	if strings.Contains(query, "ADD COLUMN designer_region") {
		return driver.RowsAffected(0), s.migrationErr
	}
	return driver.RowsAffected(0), nil
}

func TestDesignerRegionSchemaMigrationOnlyIgnoresExistingColumn(t *testing.T) {
	permissionErr := errors.New("schema permission denied")
	for _, test := range []struct {
		name    string
		err     error
		wantErr bool
	}{{"new column", nil, false}, {"existing column", &mysql.MySQLError{Number: 1060, Message: "Duplicate column name"}, false}, {"permission failure", permissionErr, true}} {
		t.Run(test.name, func(t *testing.T) {
			db := sql.OpenDB(&designerRegionMigrationConnector{migrationErr: test.err})
			t.Cleanup(func() { db.Close() })
			err := NewUserProfileModel(db).InitTable()
			if (err != nil) != test.wantErr || test.wantErr && !errors.Is(err, permissionErr) {
				t.Fatalf("migration err=%v wantErr=%v", err, test.wantErr)
			}
		})
	}
}
