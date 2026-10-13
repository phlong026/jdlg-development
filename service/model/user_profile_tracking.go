package model

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

// SQLProfileQueryer is the read-only batch surface shared by sql.DB and sql.Tx.
type SQLProfileQueryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}
type UserProfilePresentation struct {
	UserID           int64
	Nickname, Avatar string
}

// GetUserProfilePresentations reads only account presentation, never phones,
// device identifiers or account credentials. Callers supply authorized IDs and
// an existing read transaction. It never creates a missing profile.
func GetUserProfilePresentations(ctx context.Context, q SQLProfileQueryer, ids []int64) (map[int64]UserProfilePresentation, error) {
	out := map[int64]UserProfilePresentation{}
	if len(ids) == 0 {
		return out, nil
	}
	if q == nil || len(ids) > 200 {
		return nil, fmt.Errorf("profile batch exceeds read bound")
	}
	args := make([]any, 0, len(ids))
	placeholders := make([]string, 0, len(ids))
	requested := map[int64]bool{}
	for _, id := range ids {
		if id <= 0 || requested[id] {
			return nil, fmt.Errorf("profile account IDs must be positive and unique")
		}
		requested[id] = true
		args = append(args, id)
		placeholders = append(placeholders, "?")
	}
	// Joining users suppresses orphan profiles after an account is removed.
	query := `SELECT p.user_id, COALESCE(p.nickname,''), COALESCE(p.avatar,'') FROM user_profiles p JOIN users u ON u.id=p.user_id WHERE p.user_id IN (` + strings.Join(placeholders, ",") + `) ORDER BY p.user_id LIMIT ?`
	args = append(args, len(ids)+1)
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		count++
		var p UserProfilePresentation
		if err := rows.Scan(&p.UserID, &p.Nickname, &p.Avatar); err != nil {
			return nil, err
		}
		if count > len(ids) || !requested[p.UserID] {
			return nil, fmt.Errorf("unexpected profile result")
		}
		if _, duplicate := out[p.UserID]; duplicate {
			return nil, fmt.Errorf("duplicate profile result")
		}
		if len(p.Nickname) > 1024 || len(p.Avatar) > 4096 {
			return nil, fmt.Errorf("profile presentation exceeds read bound")
		}
		out[p.UserID] = p
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}
