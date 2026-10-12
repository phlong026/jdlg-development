package tracking

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestFavoriteDurableReceiptRetriesNeverRefreshWindow(t *testing.T) {
	s, scope := fixture(t)
	ctx := context.Background()
	now := time.Date(2026, 10, 10, 23, 59, 0, 0, time.UTC)
	s.Now = func() time.Time { return now }
	en := enter(t, s, 1)
	accept(t, s, en, ev(s, en, "receipt-page001", "page_view", now))
	ruleFixturePV(t, s, "owner-a", "u:1", "session-two", "view-two0001", now.Add(-time.Minute))
	receipt := FavoriteReceipt{ID: 1234, CreatedAt: now}
	s.VerifyBusiness = func(_ context.Context, p BusinessProof) (bool, error) {
		return p.Action == "favorite" && p.Account == 1 && p.TargetType == "template" && p.TargetID == 123 && p.FavoriteReceiptID == receipt.ID && p.FavoriteCreatedAt.Equal(receipt.CreatedAt), nil
	}
	p := defaultRule("owner-a").RuleParameters
	p.DailyActionCap = 5
	p.MediumFavorites = 2
	p.MediumSessions = 2
	if _, e := s.UpdateRules(ctx, scope, ruleUpdate(p)); e != nil {
		t.Fatal(e)
	}
	for _, delta := range []time.Duration{0, 2 * time.Minute, 24 * time.Hour, 48 * time.Hour} {
		now = receipt.CreatedAt.Add(delta)
		if e := s.ConfirmFavorite(ctx, en.Token, 1, "template", 123, receipt); e != nil {
			t.Fatal(e)
		}
		v := ruleVisitor(t, s, scope, Filter{})
		if v.Intent.Metrics.Favorites != 1 || v.Intent.Level != "low" {
			t.Fatalf("retry %s inflated intent: %+v", delta, v.Intent)
		}
	}
	if e := s.transaction(ctx, func(r Repository) error {
		rows, e := r.List("business")
		if e != nil {
			return e
		}
		if len(rows) != 1 || !rows[0].At.Equal(receipt.CreatedAt) || rows[0].Session != en.Session {
			t.Fatalf("retry replaced receipt: %+v", rows)
		}
		return nil
	}); e != nil {
		t.Fatal(e)
	}
	// An unchanged source tuple retried after the intent window must not return to it.
	now = receipt.CreatedAt.AddDate(0, 0, 31)
	late := enter(t, s, 1)
	if e := s.ConfirmFavorite(ctx, late.Token, 1, "template", 123, receipt); e != nil {
		t.Fatal(e)
	}
	if v := ruleVisitor(t, s, scope, Filter{}); v.Intent.Metrics.Favorites != 0 {
		t.Fatal("old success refreshed window", v.Intent)
	}
}

func TestFavoriteReceiptRemoveReaddCannotResurrectOldSource(t *testing.T) {
	s, scope := fixture(t)
	ctx := context.Background()
	now := s.Now()
	s.Now = func() time.Time { return now }
	en := enter(t, s, 1)
	accept(t, s, en, ev(s, en, "receipt-page002", "page_view", now))
	first := FavoriteReceipt{ID: 100, CreatedAt: now}
	active := first
	s.VerifyBusiness = func(_ context.Context, p BusinessProof) (bool, error) {
		return p.FavoriteReceiptID == active.ID && p.FavoriteCreatedAt.Equal(active.CreatedAt), nil
	}
	if e := s.ConfirmFavorite(ctx, en.Token, 1, "template", 12, first); e != nil {
		t.Fatal(e)
	}
	active = FavoriteReceipt{}
	if v := ruleVisitor(t, s, scope, Filter{}); v.Intent.Metrics.Favorites != 0 {
		t.Fatal("cancelled source counted", v.Intent)
	}
	now = now.Add(time.Minute)
	active = FavoriteReceipt{ID: 101, CreatedAt: now}
	if e := s.ConfirmFavorite(ctx, en.Token, 1, "template", 12, active); e != nil {
		t.Fatal(e)
	}
	if e := s.ConfirmFavorite(ctx, en.Token, 1, "template", 12, first); e != nil {
		t.Fatal(e)
	}
	if v := ruleVisitor(t, s, scope, Filter{}); v.Intent.Metrics.Favorites != 1 {
		t.Fatal("old receipt resurrected", v.Intent)
	}
	// Even if old telemetry was absent, a stale hook cannot recreate its proof.
	if e := s.transaction(ctx, func(r Repository) error { return r.Remove("business", hash("favorite:receipt:100")) }); e != nil {
		t.Fatal(e)
	}
	if e := s.ConfirmFavorite(ctx, en.Token, 1, "template", 12, first); e == nil {
		t.Fatal("deleted source accepted after re-add")
	}
}

func TestFavoriteReceiptIdentityOwnerExpiryAndLegacyBoundaries(t *testing.T) {
	s, scope := fixture(t)
	ctx := context.Background()
	now := s.Now()
	en := enter(t, s, 1)
	receipt := FavoriteReceipt{ID: 200, CreatedAt: now}
	if e := s.ConfirmFavorite(ctx, en.Token, 2, "template", 12, receipt); e == nil {
		t.Fatal("foreign account accepted")
	}
	if e := s.ConfirmFavorite(ctx, en.Token, 1, "template", 12); e == nil {
		t.Fatal("receipt-less hook accepted")
	}
	old := receipt
	old.CreatedAt = now.Add(-24 * time.Hour)
	if e := s.ConfirmFavorite(ctx, en.Token, 1, "template", 12, old); e == nil {
		t.Fatal("old success reassigned to later session")
	}
	if e := s.ConfirmFavorite(ctx, en.Token, 1, "template", 12, receipt); e != nil {
		t.Fatal(e)
	}
	s.Config.DefaultOwner = "owner-b"
	other := enter(t, s, 1)
	if e := s.ConfirmFavorite(ctx, other.Token, 1, "template", 12, receipt); e == nil {
		t.Fatal("receipt rebound to different owner")
	}
	s.Now = func() time.Time { return now.Add(73 * time.Hour) }
	if e := s.ConfirmFavorite(ctx, en.Token, 1, "template", 12, receipt); e == nil {
		t.Fatal("expired entry accepted")
	}
	s.Now = func() time.Time { return now }
	s.VerifyBusiness = func(context.Context, BusinessProof) (bool, error) { return false, errors.New("source offline") }
	if e := s.ConfirmFavorite(ctx, en.Token, 1, "template", 13, FavoriteReceipt{ID: 201, CreatedAt: now}); !errors.Is(e, ErrUnavailable) {
		t.Fatal(e)
	}
	s.VerifyBusiness = func(context.Context, BusinessProof) (bool, error) { return true, nil }
	// Pre-receipt facts remain stored but cannot be verified against a new row.
	if e := s.transaction(ctx, func(r Repository) error {
		row := record("business", "legacy-favorite", "owner-a", now, map[string]string{"action": "favorite", "target_type": "template", "target_id": "14"})
		row.Visitor = "u:1"
		return r.Insert(row)
	}); e != nil {
		t.Fatal(e)
	}
	dataErr := s.queryTransaction(ctx, func(r Repository) error {
		data, e := s.loadIntentFacts(ctx, r, scope, now, []string{"u:1"})
		if e == nil && len(data.business["u:1"]) != 1 {
			t.Fatalf("legacy receipt guessed: %+v", data.business)
		}
		return e
	})
	if dataErr != nil {
		t.Fatal(dataErr)
	}
}
