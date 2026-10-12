package tracking

import (
	"context"
	"database/sql"
)

// SQLSourceQueryer is read-only. Production source adapters borrow the tracking
// transaction instead of borrowing a second pool connection while holding its
// tenant lock. This also works when the pool has only one connection.
type SQLSourceQueryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}
type sourceTransactionKey struct{}

func SourceReadDB(ctx context.Context, fallback *sql.DB) SQLSourceQueryer {
	if tx, ok := ctx.Value(sourceTransactionKey{}).(*sql.Tx); ok {
		return tx
	}
	if fallback == nil {
		return nil
	}
	return fallback
}
func sourceReadContext(ctx context.Context, r Repository) context.Context {
	switch repo := r.(type) {
	case *queryRepository:
		return sourceReadContext(ctx, repo.Repository)
	case *memoryQuerySnapshot:
		return sourceReadContext(ctx, repo.Repository)
	case *sqlRepo:
		return context.WithValue(ctx, sourceTransactionKey{}, repo.tx)
	default:
		return ctx
	}
}
