package route

import (
	"errors"
	"testing"
)

func TestTrackingLeadUsersBatchedDeduplicatesAndBoundsQueries(t *testing.T) {
	ids := make([]int64, 0, 604)
	for id := int64(1); id <= 401; id++ {
		ids = append(ids, id)
		if id <= 203 {
			ids = append(ids, id)
		}
	}
	var batchSizes []int
	got, err := trackingLeadUsersBatched(ids, func(batch []int64) (map[int64]bool, error) {
		batchSizes = append(batchSizes, len(batch))
		out := map[int64]bool{}
		for _, id := range batch {
			if id%2 == 0 {
				out[id] = true
			}
		}
		return out, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(batchSizes) != 3 || batchSizes[0] != 200 || batchSizes[1] != 200 || batchSizes[2] != 1 {
		t.Fatalf("unexpected bounded batches: %v", batchSizes)
	}
	if !got[2] || got[3] || !got[400] {
		t.Fatalf("unexpected merged lookup: %#v", got)
	}
}

func TestTrackingLeadUsersBatchedStopsOnLookupFailure(t *testing.T) {
	want := errors.New("database unavailable")
	calls := 0
	got, err := trackingLeadUsersBatched([]int64{1, 2, 3}, func([]int64) (map[int64]bool, error) {
		calls++
		return nil, want
	})
	if !errors.Is(err, want) || got != nil || calls != 1 {
		t.Fatalf("got=%#v err=%v calls=%d", got, err, calls)
	}
}
