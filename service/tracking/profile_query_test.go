package tracking

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestAuthorizedProfilePresentationAndChineseSearch(t *testing.T) {
	s, rows, scope, at := reviewFixture()
	reviewPV(rows, "profile-view1", "one", "u:1", "session1", "", at.Add(-time.Hour))
	reviewPV(rows, "profile-view2", "one", "u:2", "session2", "", at.Add(-time.Hour))
	reviewPV(rows, "profile-view3", "one", "a:anonymous1", "session3", "", at.Add(-time.Hour))
	reviewPV(rows, "profile-hidden", "two", "u:3", "session4", "", at.Add(-time.Hour))
	reviewPV(rows, "profile-deleted", "one", "u:4", "session5", "", at.Add(-time.Hour))
	rows.Put(record("deletion", "u:4", "", at, map[string]bool{"deleted": true}))
	calls := 0
	s.ResolveVisitorProfiles = func(_ context.Context, ids []int64) (map[int64]VisitorProfile, error) {
		calls++
		if !reflect.DeepEqual(ids, []int64{1, 2}) {
			t.Fatalf("unscoped profile IDs: %v", ids)
		}
		return map[int64]VisitorProfile{1: {Nickname: "  林间访客  ", AvatarURL: "https://images.example/avatar.png"}, 2: {Nickname: "", AvatarURL: "javascript:alert(1)"}, 3: {Nickname: "must not leak"}}, nil
	}
	out, err := s.Visitors(context.Background(), scope, Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 || out.Identified != 2 || out.Anonymous != 1 {
		t.Fatalf("counts=%+v calls=%d", out, calls)
	}
	by := map[string]Visitor{}
	for _, v := range out.Items.([]Visitor) {
		by[v.Key] = v
	}
	if by["u:1"].Nickname != "林间访客" || by["u:1"].AvatarURL != "https://images.example/avatar.png" {
		t.Fatal(by)
	}
	if by["u:2"].Nickname != "已识别账号" || by["u:2"].AvatarURL != "" || by["a:anonymous1"].AvatarURL != "" {
		t.Fatal(by)
	}
	got, err := s.Visitors(context.Background(), scope, Filter{Search: "林间"})
	if err != nil || got.Identified != 1 || len(got.Items.([]Visitor)) != 1 {
		t.Fatal(got, err)
	}
	got, err = s.Visitors(context.Background(), scope, Filter{VisitorSearch: "u:2"})
	if err != nil || got.Identified != 1 || got.Items.([]Visitor)[0].Key != "u:2" {
		t.Fatal(got, err)
	}
}
func TestProfileCursorRejectsRenamedCandidatesAndKeepsOverrideFirstPage(t *testing.T) {
	s, rows, scope, at := reviewFixture()
	for i := 1; i <= 3; i++ {
		reviewPV(rows, fmt.Sprintf("profile-view-%d", i), "one", fmt.Sprintf("u:%d", i), fmt.Sprintf("session-%d", i), "", at.Add(-time.Hour))
	}
	names := map[int64]string{1: "目标甲", 2: "目标乙", 3: "别名"}
	s.ResolveVisitorProfiles = func(_ context.Context, ids []int64) (map[int64]VisitorProfile, error) {
		p := map[int64]VisitorProfile{}
		for _, id := range ids {
			p[id] = VisitorProfile{Nickname: names[id]}
		}
		return p, nil
	}
	f := Filter{AsOf: at, Limit: 1, Search: "目标"}
	first, err := s.Visitors(context.Background(), scope, f)
	if err != nil || first.Cursor == "" || first.FirstCursor == "" {
		t.Fatal(first, err)
	}
	var c map[string]json.RawMessage
	b, _ := base64.RawURLEncoding.DecodeString(first.FirstCursor)
	json.Unmarshal(b, &c)
	if c["intent_override_revision"] == nil || c["profile_revision"] == nil {
		t.Fatal("lost cursor revision", string(b))
	}
	f.Cursor = first.Cursor
	second, err := s.Visitors(context.Background(), scope, f)
	if err != nil || len(second.Items.([]Visitor)) != 1 || second.Items.([]Visitor)[0].Key == first.Items.([]Visitor)[0].Key {
		t.Fatal(second, err)
	}
	f.Cursor = first.FirstCursor
	back, err := s.Visitors(context.Background(), scope, f)
	if err != nil || back.Items.([]Visitor)[0].Key != first.Items.([]Visitor)[0].Key {
		t.Fatal(back, err)
	}
	// A previously excluded candidate is now a match: membership cannot silently shift.
	names[3] = "目标丙"
	for _, cursor := range []string{first.Cursor, first.FirstCursor} {
		f.Cursor = cursor
		_, err = s.Visitors(context.Background(), scope, f)
		requireQueryError(t, err, 409)
	}
}
func TestProfileBatchesCanonicalIdentitiesAndReadBudget(t *testing.T) {
	s, _, _, _ := reviewFixture()
	facts := []factView{}
	for i := 1; i <= 401; i++ {
		facts = append(facts, factView{Visitor: fmt.Sprintf("u:%d", i)})
	}
	for _, key := range []string{"a:42", "u:01", "u:+2", "u:0", "u:-2", "u:9223372036854775808"} {
		facts = append(facts, factView{Visitor: key})
	}
	calls := []int{}
	s.ResolveVisitorProfiles = func(_ context.Context, ids []int64) (map[int64]VisitorProfile, error) {
		calls = append(calls, len(ids))
		return map[int64]VisitorProfile{}, nil
	}
	r := boundedQueryRepository(reviewStore{}).(*queryRepository)
	_, revision, err := s.queryProfiles(context.Background(), r, facts)
	if err != nil || revision == "" || !reflect.DeepEqual(calls, []int{200, 200, 1}) || r.budget.rows != 401 {
		t.Fatal(calls, r.budget, err)
	}
	r.budget.rows = maxQueryRecords
	_, _, err = s.queryProfiles(context.Background(), r, facts[:1])
	requireQueryError(t, err, 422)
}
func TestProfileResolverErrorsNeverMasqueradeAsEmptySearch(t *testing.T) {
	s, rows, scope, at := reviewFixture()
	reviewPV(rows, "profile-view", "one", "u:1", "session1", "", at)
	sourceErr := errors.New("isolated source failure")
	s.ResolveVisitorProfiles = func(context.Context, []int64) (map[int64]VisitorProfile, error) { return nil, sourceErr }
	if _, err := s.Visitors(context.Background(), scope, Filter{Search: "name"}); !errors.Is(err, sourceErr) {
		t.Fatal(err)
	}
}
func TestProfileAvatarRejectsUnsafeSources(t *testing.T) {
	for _, raw := range []string{"javascript:alert(1)", "data:image/png;base64,aaaa", "http://example.org/a", "https://me:secret@example.org/a", "https://example.org/a#x", "https://localhost/a", "https://localhost./a", "https://127.1/a", "https://0x7f.0.0.1/a", "https://0x7f.0x.0.1/a", "https://0x7f.0.0.0x/a", "https://0xA.0.0.1/a", "https://0x7f.1/a", "https://0x7f.00.0x0.01/a", "https://１２７.０.０.１/a", "https://[::ffff:127.0.0.1%25eth0]/a", "https://0177.0.0.1/a", "https://127.0.0.1/a", "https://10.0.0.1/a", "https://[::1]/a", "https://169.254.169.254/a", "/uploads/a.png", strings.Repeat("x", 2049)} {
		if got := safeProfileAvatar(raw); got != "" {
			t.Fatalf("unsafe avatar accepted %q", got)
		}
	}
	if safeProfileAvatar("https://thirdwx.qlogo.cn/mmopen/image/132") == "" {
		t.Fatal("public HTTPS profile image rejected")
	}
}

func TestProfileRevisionPreservesSameClockManualOverrideReplay(t *testing.T) {
	s, scope := fixture(t)
	ctx := context.Background()
	at := s.Now()
	seedOverrideVisitors(t, s, scope, 3, false)
	s.ResolveVisitorProfiles = func(_ context.Context, ids []int64) (map[int64]VisitorProfile, error) {
		out := map[int64]VisitorProfile{}
		for _, id := range ids {
			out[id] = VisitorProfile{Nickname: fmt.Sprintf("账号%d", id)}
		}
		return out, nil
	}
	f := Filter{AsOf: at, Limit: 1, Intent: "high"}
	first, err := s.Visitors(ctx, scope, f)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.OverrideIntent(ctx, scope, "u:1", "low", "本次人工核实修正", at.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	f.Cursor = first.Cursor
	second, err := s.Visitors(ctx, scope, f)
	if err != nil || second.Identified != 3 || second.Items.([]Visitor)[0].Key != "u:2" {
		t.Fatal(second, err)
	}
	f.Cursor = first.FirstCursor
	back, err := s.Visitors(ctx, scope, f)
	if err != nil || back.Items.([]Visitor)[0].Intent.Level != "high" || back.FirstCursor != first.FirstCursor {
		t.Fatal(back, err)
	}
}
