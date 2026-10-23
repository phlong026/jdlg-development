package route

import (
	"context"
	"errors"
	"service/model"
	"service/tracking"
)

func costSnapshotMetadata(snapshot *model.CostEstimateShareSnapshot) tracking.LegacyMetadata {
	// The public report is immutable. Analytics stores its identity and a generic
	// label, not the house inputs, price breakdown, region, or report body.
	return tracking.LegacyMetadata{Title: "公开造价测算报告", Type: "cost_snapshot", VersionSeed: legacySeed([]any{snapshot.ReportID, snapshot.Result})}
}

func makeTrackingCostSnapshotResolver(store *model.CostEstimateShareRedisModel) func(context.Context, string) (tracking.LegacyMetadata, error) {
	return func(ctx context.Context, id string) (tracking.LegacyMetadata, error) {
		snapshot, err := store.Get(ctx, id)
		if errors.Is(err, model.ErrCostEstimateShareNotFound) {
			return tracking.LegacyMetadata{}, &tracking.Error{Status: 404, Message: "公开报告已失效或撤销"}
		}
		if err != nil {
			return tracking.LegacyMetadata{}, tracking.ErrUnavailable
		}
		if !validCostEstimateShare(snapshot.Result) {
			return tracking.LegacyMetadata{}, tracking.ErrUnavailable
		}
		return costSnapshotMetadata(snapshot), nil
	}
}
