package tracking

import (
	"context"
	"fmt"
	"math/rand"
	"sort"
	"testing"
	"time"
)

type comparableFact struct {
	Owner, Visitor, Session, ID, Route, Content, Title string
	Version                                            int
	Started, Last                                      time.Time
	Count                                              bool
	Visible                                            int64
}

func comparableFacts(facts []factView) []comparableFact {
	out := make([]comparableFact, 0, len(facts))
	for _, f := range facts {
		out = append(out, comparableFact{f.Owner, f.Visitor, f.Session, f.ID, f.Route, f.Content, f.Title, f.Version, f.Started.UTC(), f.Last.UTC(), f.Count, f.Visible})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Session != out[j].Session {
			return out[i].Session < out[j].Session
		}
		return out[i].ID < out[j].ID
	})
	return out
}

// generateTrackingTraffic drives real Enter/Accept calls with overlapping
// segments, midnight crossings, late and out-of-order batches, device clocks
// slightly ahead, and anonymous sessions that log in midway. It returns
// intermediate clocks usable as historical snapshots.
func generateTrackingTraffic(t *testing.T, s *Service, rng *rand.Rand, seed, visitors int, start time.Time) (time.Time, []time.Time) {
	t.Helper()
	clock := start
	s.Now = func() time.Time { return clock }
	routes := []string{"pages/home/home", "pages/list/list"}
	snapshots := []time.Time{}
	id := func(kind string, n ...int) string {
		return fmt.Sprintf("%s-%d-%d-%d", kind, seed, len(snapshots), rng.Int63())
	}
	for v := 0; v < visitors; v++ {
		// Spread visits over several days, often close to Shanghai midnight.
		clock = clock.Add(time.Duration(30+rng.Intn(240)) * time.Minute)
		if rng.Intn(3) == 0 {
			midnight := businessDay(clock).AddDate(0, 0, 1)
			if lead := time.Duration(rng.Intn(90)) * time.Second; midnight.Add(-lead).After(clock) {
				clock = midnight.Add(-lead)
			}
		}
		client, anon := fmt.Sprintf("client-%d-%04d", seed, v), fmt.Sprintf("anon-%d-%04d", seed, v)
		account := int64(0)
		if rng.Intn(3) == 0 {
			account = int64(1000 + v)
		}
		linkLater := account == 0 && rng.Intn(2) == 0
		var en Entry
		pages := 1 + rng.Intn(3)
		for k := 0; k < pages; k++ {
			in := EntryInput{Route: routes[rng.Intn(len(routes))], Client: client, Anonymous: anon, Consent: "v1"}
			if k > 0 {
				clock = clock.Add(time.Duration(1+rng.Intn(60)) * time.Second)
				in.Previous = en.Token
				if linkLater && k == 1 {
					account = int64(2000 + v)
				}
			}
			var err error
			if en, err = s.Enter(context.Background(), in, account); err != nil {
				t.Fatal(err)
			}
			view := id("view")
			base := func(typ string, at time.Time) Event {
				return Event{Schema: 1, ID: id("event"), Type: typ, Session: en.Session, View: view, Route: en.Route, Occurred: at, AppState: "foreground", PageState: "visible", Monotonic: 200000}
			}
			pv := clock
			events := []Event{base("page_view", pv)}
			at, segment := pv, id("segment")
			for seq := 1; seq <= rng.Intn(9); seq++ {
				delta := 1000 + rng.Intn(14001)
				at = at.Add(time.Duration(delta) * time.Millisecond)
				if rng.Intn(4) == 0 {
					at = at.Add(-time.Duration(rng.Intn(6000)) * time.Millisecond) // overlap
				}
				x := base("visible_segment", at)
				x.Segment, x.Seq, x.Delta = segment, seq, int64(delta)
				events = append(events, x)
			}
			if rng.Intn(3) == 0 {
				span := at.Sub(pv)
				if span < 0 {
					span = 0
				}
				x := base("content_click", pv.Add(time.Duration(rng.Int63n(int64(span)+1))))
				x.Target = "target-0001"
				events = append(events, x)
			}
			if rng.Intn(2) == 0 {
				at = at.Add(time.Duration(rng.Intn(3000)) * time.Millisecond)
				events = append(events, base("page_end", at))
			}
			// One to three batches, sometimes heartbeats before their PV.
			rng.Shuffle(len(events), func(i, j int) { events[i], events[j] = events[j], events[i] })
			for len(events) > 0 {
				n := 1 + rng.Intn(len(events))
				batch := events[:n]
				events = events[n:]
				latest := batch[0].Occurred
				for _, x := range batch {
					if x.Occurred.After(latest) {
						latest = x.Occurred
					}
				}
				send := latest
				switch rng.Intn(6) {
				case 0: // device clock slightly ahead: events up to 110s in the future
					send = latest.Add(-time.Duration(rng.Intn(110)) * time.Second)
				case 1: // offline, delivered late
					send = latest.Add(time.Duration(2+rng.Intn(40)) * time.Minute)
				}
				if send.After(clock) {
					clock = send
				}
				if latest.After(clock.Add(2 * time.Minute)) {
					clock = latest.Add(-2 * time.Minute)
				}
				results, err := s.Accept(context.Background(), en.Token, batch)
				if err != nil {
					t.Fatal(err)
				}
				for _, r := range results {
					if r.Status != "accepted" {
						t.Fatalf("seed %d: generated event rejected: %+v", seed, r)
					}
				}
				// Snapshots between batches straddle sessions that keep changing.
				if rng.Intn(4) == 0 {
					snapshots = append(snapshots, clock)
				}
			}
			if at.After(clock) {
				clock = at
			}
		}
		if rng.Intn(3) == 0 {
			snapshots = append(snapshots, clock)
		}
	}
	return clock, snapshots
}

func TestSummaryReductionMatchesRawReduction(t *testing.T) {
	for seed := 1; seed <= 12; seed++ {
		seed := seed
		t.Run(fmt.Sprintf("seed-%d", seed), func(t *testing.T) {
			t.Parallel()
			checkSummaryEquivalence(t, seed)
		})
	}
}

func checkSummaryEquivalence(t *testing.T, seed int) {
	{
		rng := rand.New(rand.NewSource(int64(seed)))
		s, scope := fixture(t)
		start := time.Date(2026, 10, 5, 8, 0, 0, 0, time.UTC)
		end, snapshots := generateTrackingTraffic(t, s, rng, seed, 30, start)
		// Leave time for every deferred event to become admissible.
		final := end.Add(5 * time.Minute)
		s.Now = func() time.Time { return final }
		// A bounded random sample of historical snapshots, many of which fall
		// between batches of a still-changing session.
		rng.Shuffle(len(snapshots), func(i, j int) { snapshots[i], snapshots[j] = snapshots[j], snapshots[i] })
		if len(snapshots) > 6 {
			snapshots = snapshots[:6]
		}
		asOfs := append([]time.Time{final, end}, snapshots...)
		compared, summarizedTotal := 0, 0
		for _, asOf := range asOfs {
			filters := []Filter{
				{AsOf: asOf}, // "all": unaligned start, end after as_of
				{AsOf: asOf, Start: asOf.AddDate(0, 0, -30), End: asOf.Add(time.Nanosecond)}, // intent window
			}
			for day := businessDay(start); day.Before(end.Add(24 * time.Hour)); day = day.AddDate(0, 0, 1) {
				for _, span := range []int{1, 2, 3} {
					filters = append(filters, Filter{AsOf: asOf, Start: day, End: day.AddDate(0, 0, span)})
				}
			}
			for i := 0; i < 4; i++ { // arbitrary windows exercise the raw fallback
				a := start.Add(time.Duration(rng.Int63n(int64(end.Sub(start)))))
				b := a.Add(time.Duration(1 + rng.Int63n(int64(48*time.Hour))))
				filters = append(filters, Filter{AsOf: asOf, Start: a, End: b})
			}
			for _, raw := range filters {
				f, err := s.Normalize(raw)
				if err != nil {
					continue // e.g. an empty window after clamping to as_of
				}
				err = s.readTransaction(context.Background(), func(r Repository) error {
					want, err := s.factsSelected(r, scope, f, factSelection{})
					if err != nil {
						return err
					}
					got, err := s.factsSelected(r, scope, f, factSelection{Summaries: true})
					if err != nil {
						return err
					}
					w, g := comparableFacts(want), comparableFacts(got)
					if fmt.Sprint(w) != fmt.Sprint(g) {
						t.Fatalf("seed %d as_of %s window [%s,%s):\nraw     %+v\nsummary %+v", seed, f.AsOf, f.Start, f.End, w, g)
					}
					compared += len(w)
					if asOf.Equal(final) && summaryWindowExact(f) {
						_, summarized, _, err := s.summaryFactsSelected(r, scope, f, factSelection{}, true)
						if err != nil {
							return err
						}
						summarizedTotal += len(summarized)
					}
					return nil
				})
				if err != nil {
					t.Fatalf("seed %d: %v", seed, err)
				}
			}
		}
		if compared == 0 || summarizedTotal == 0 {
			t.Fatalf("seed %d: nothing compared (%d facts) or no session used projections (%d)", seed, compared, summarizedTotal)
		}
	}
}
