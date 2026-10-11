package route

import (
	"context"
	"database/sql"
	"errors"
	"service/model"
	"service/tracking"
)

// Source revocation wins over historical analytics snapshots. Only existence,
// account binding and non-invalid state are read, never private customer fields.
func makeTrackingBusinessVerifier(db *sql.DB) func(context.Context, tracking.BusinessProof) (bool, error) {
	return func(ctx context.Context, proof tracking.BusinessProof) (bool, error) {
		if proof.Account <= 0 || proof.TargetID <= 0 {
			return false, nil
		}
		reader := tracking.SourceReadDB(ctx, db)
		if reader == nil {
			return false, tracking.ErrUnavailable
		}
		var row *sql.Row
		switch proof.Action {
		case "favorite":
			if model.NormalizeFavoriteTargetType(proof.TargetType) != proof.TargetType || proof.TargetType == "" {
				return false, nil
			}
			// UserFavoriteModel.Remove hard-deletes this exact account/target tuple.
			row = reader.QueryRowContext(ctx, `SELECT 1 FROM user_favorites WHERE user_id=? AND target_type=? AND target_id=? LIMIT 1`, proof.Account, proof.TargetType, proof.TargetID)
		case "consultation":
			if proof.TargetType != "lead" {
				return false, nil
			}
			row = reader.QueryRowContext(ctx, `SELECT 1 FROM customer_leads WHERE id=? AND user_id=? AND status IN ('new','contacted','converted') LIMIT 1`, proof.TargetID, proof.Account)
		default:
			// No live appointment or quote-request source has been verified for this release.
			return false, nil
		}
		var exists int
		e := row.Scan(&exists)
		if errors.Is(e, sql.ErrNoRows) {
			return false, nil
		}
		if e != nil {
			return false, tracking.ErrUnavailable
		}
		return exists == 1, nil
	}
}
