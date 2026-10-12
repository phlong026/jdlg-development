package model

import (
	"context"
	"database/sql"
)

// SQLRowQueryer is the read-only portion shared by *sql.DB and *sql.Tx.
// Source readers never need permission to execute statements or manage a transaction.
type SQLRowQueryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}
