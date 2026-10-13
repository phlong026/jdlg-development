package route

import (
	"context"
	"database/sql"
	"service/model"
	"service/tracking"
)

func makeTrackingProfileResolver(db *sql.DB) tracking.VisitorProfileResolver {
	return func(ctx context.Context, ids []int64) (map[int64]tracking.VisitorProfile, error) {
		q := tracking.SourceReadDB(ctx, db)
		if q == nil {
			return nil, tracking.ErrUnavailable
		}
		profiles, err := model.GetUserProfilePresentations(ctx, q, ids)
		if err != nil {
			return nil, err
		}
		out := make(map[int64]tracking.VisitorProfile, len(profiles))
		for id, p := range profiles {
			out[id] = tracking.VisitorProfile{Nickname: p.Nickname, AvatarURL: sanitizePublicImageURL(p.Avatar)}
		}
		return out, nil
	}
}
