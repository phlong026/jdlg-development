package tracking

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"time"
)

const eventWindowMessage = "事件时间超出允许窗口"

// RejectionDay is a finite owner/day counter. It never carries visitor, session,
// event or device identifiers, so it needs no deletion or retention linkage.
type RejectionDay struct {
	Day    string         `json:"day"`
	Owner  string         `json:"owner_id"`
	Counts map[string]int `json:"counts"`
}

// eventClockRejection classifies an out-of-window event time. A device clock
// that is ahead of or behind the server is the usual cause, and the SDK drops
// rejected events, so without this counter the visitor simply appears to have
// no data. Zero times are malformed input, not clock skew.
func eventClockRejection(now, started, occurred time.Time) string {
	switch {
	case occurred.IsZero():
		return ""
	case occurred.After(now.Add(2 * time.Minute)):
		return "clock_ahead"
	// An event older than its own server-issued session can only come from a
	// clock that is behind; check it before the generic offline-queue limit.
	case occurred.Before(started.Add(-2 * time.Minute)):
		return "clock_behind"
	case occurred.Before(now.Add(-72 * time.Hour)):
		return "stale_queue"
	}
	return ""
}

func rejectionDayStart(t time.Time) time.Time {
	t = t.UTC()
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

// addRejectionCounts merges counts into the owner's current UTC day row inside
// the caller's acceptance transaction.
func addRejectionCounts(r Repository, owner string, now time.Time, counts map[string]int) error {
	if len(counts) == 0 {
		return nil
	}
	day := rejectionDayStart(now)
	id := owner + ":" + day.Format("2006-01-02")
	row, err := decode[RejectionDay](r, "rejection_stat", id)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return err
	}
	if row.Counts == nil {
		row = RejectionDay{Day: day.Format("2006-01-02"), Owner: owner, Counts: map[string]int{}}
	}
	for reason, n := range counts {
		row.Counts[reason] += n
	}
	return r.Put(record("rejection_stat", id, owner, day, row))
}

// RejectionStats returns the authorized owners' rejected-event counters for the
// last `days` UTC days, newest first.
func (s *Service) RejectionStats(ctx context.Context, scope Scope, days int) ([]RejectionDay, error) {
	out := []RejectionDay{}
	if e := s.authorizeView(scope); e != nil {
		return out, e
	}
	if days < 1 || days > 90 {
		return out, fail(400, "统计天数需在1到90之间")
	}
	from := rejectionDayStart(s.Now()).AddDate(0, 0, -(days - 1))
	err := s.queryTransaction(ctx, func(r Repository) error {
		rows, e := scopedRecords(r, "rejection_stat", RecordQuery{Owners: permittedOwners(scope, ""), AtFrom: from})
		if e != nil {
			return e
		}
		for _, row := range rows {
			var day RejectionDay
			if json.Unmarshal(row.Payload, &day) != nil || !scope.Allows(row.Owner) {
				continue
			}
			day.Owner = row.Owner
			out = append(out, day)
		}
		return nil
	})
	sort.Slice(out, func(i, j int) bool {
		if out[i].Day == out[j].Day {
			return out[i].Owner < out[j].Owner
		}
		return out[i].Day > out[j].Day
	})
	return out, err
}
