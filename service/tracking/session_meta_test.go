package tracking

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
)

type getCountingStore struct {
	*memoryStore
	mu   sync.Mutex
	gets map[string]int
}
type getCountingRepo struct {
	Repository
	store *getCountingStore
}

func (r getCountingRepo) Get(kind, id string) (Record, error) {
	r.store.mu.Lock()
	r.store.gets[kind]++
	r.store.mu.Unlock()
	return r.Repository.Get(kind, id)
}
func (s *getCountingStore) ReadTransaction(ctx context.Context, tenant string, fn func(Repository) error) error {
	return s.memoryStore.ReadTransaction(ctx, tenant, func(r Repository) error { return fn(getCountingRepo{r, s}) })
}
func (s *getCountingStore) Transaction(ctx context.Context, tenant string, fn func(Repository) error) error {
	return s.memoryStore.Transaction(ctx, tenant, func(r Repository) error { return fn(getCountingRepo{r, s}) })
}

// Session and event drilldowns used to read an identity link, a deletion
// tombstone and a projection state per session: three round trips each, while
// the tenant transaction was open. They are now batched by session ID.
func TestDrilldownReadsSessionMetadataInBatches(t *testing.T) {
	s, scope := fixture(t)
	for i := 0; i < 60; i++ {
		en, err := s.Enter(context.Background(), EntryInput{Route: "pages/home/home", Client: fmt.Sprintf("client-%07d", i), Anonymous: fmt.Sprintf("anonymous-%04d", i), Consent: "v1"}, 1)
		if err != nil {
			t.Fatal(err)
		}
		e := ev(s, en, fmt.Sprintf("meta-event-%04d", i), "page_view", s.Now())
		e.View = fmt.Sprintf("meta-view-%04d", i)
		accept(t, s, en, e)
	}
	counting := &getCountingStore{memoryStore: s.Store.(*memoryStore), gets: map[string]int{}}
	s.Store = counting
	sessions, err := s.Sessions(context.Background(), scope, "u:1", Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if got := len(sessions.Items.([]Session)); got != 30 { // default page size
		t.Fatalf("expected a full first page of sessions, got %d", got)
	}
	for _, kind := range []string{"deletion_session", "summary_state"} {
		if counting.gets[kind] != 0 {
			t.Fatalf("%d point reads of %s for 60 sessions; they must be batched", counting.gets[kind], kind)
		}
	}
}

func TestSessionMetaBatchesAndSkipsMissingRows(t *testing.T) {
	s, _ := fixture(t)
	if err := s.transaction(context.Background(), func(r Repository) error {
		for i := 0; i < 3; i++ {
			id := fmt.Sprintf("session-%d", i)
			if err := r.Put(record("summary_state", id, "owner-a", s.Now(), summaryState{Metric: MetricVersion, Complete: true})); err != nil {
				return err
			}
		}
		return r.Put(record("deletion_session", "session-1", "", s.Now(), map[string]any{"deleted_at": s.Now()}))
	}); err != nil {
		t.Fatal(err)
	}
	ids := []string{"session-2", "session-0", "session-1", "session-2", "", "session-missing"}
	err := s.readTransaction(context.Background(), func(r Repository) error {
		meta, err := loadSessionMeta(r, ids, true, true, true)
		if err != nil {
			return err
		}
		if len(meta.state) != 3 || !meta.state["session-0"].Complete || len(meta.identity) != 0 {
			t.Fatalf("unexpected metadata: %+v", meta)
		}
		if !meta.deleted["session-1"] || meta.deleted["session-0"] || meta.deleted["session-missing"] {
			t.Fatalf("unexpected tombstones: %+v", meta.deleted)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	// Corrupt state fails closed instead of reading as "no projection".
	_ = s.transaction(context.Background(), func(r Repository) error {
		return r.Put(Record{Kind: "summary_state", ID: "session-bad", Owner: "owner-a", At: s.Now(), Payload: []byte("not json")})
	})
	if err = s.readTransaction(context.Background(), func(r Repository) error {
		_, err := loadSessionMeta(r, []string{"session-bad"}, false, false, true)
		return err
	}); err == nil {
		t.Fatal("corrupt summary state was ignored")
	}
}

func TestShareLookupSQLUsesIndexedColumnAndKeepsLegacyRows(t *testing.T) {
	for _, kind := range []string{"share", "share_request"} {
		query, args := buildScopedQuery("tenant", kind, RecordQuery{Shares: []string{"share-a", "share-b"}}, 10)
		want := "(r.share_id IN (?,?) OR (r.share_id='' AND JSON_UNQUOTE(JSON_EXTRACT(r.payload,'$.share_id')) IN (?,?)))"
		if !strings.Contains(query, want) {
			t.Fatalf("%s: missing %q in %s", kind, want, query)
		}
		if strings.Count(query, "?") != len(args) {
			t.Fatalf("%s: %d placeholders for %d args", kind, strings.Count(query, "?"), len(args))
		}
		// Values appear once per branch, in placeholder order, after the tenant and kind.
		got := fmt.Sprint(args[3:7])
		if got != "[share-a share-b share-a share-b]" {
			t.Fatalf("%s: argument order %s", kind, got)
		}
	}
	if query, _ := buildScopedQuery("tenant", "share", RecordQuery{Shares: []string{}}, 10); !strings.Contains(query, "1=0") {
		t.Fatalf("an empty share list must select nothing: %s", query)
	}
	// Other kinds keep a plain column predicate: only share rows ever lacked it.
	if query, _ := buildScopedQuery("tenant", "event", RecordQuery{Shares: []string{"share-a"}}, 10); strings.Contains(query, "JSON_EXTRACT(r.payload,'$.share_id')") {
		t.Fatalf("event lookup must use the column only: %s", query)
	}
	if query, _ := buildScopedQuery("tenant", "event", RecordQuery{}, 10); strings.Contains(query[strings.Index(query, " WHERE "):], "share_id") {
		t.Fatalf("unrelated query filtered on share_id: %s", query)
	}
}
