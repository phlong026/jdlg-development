package route

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"net/http"
	"service/model"
	"service/tracking"
	"strings"
	"testing"
	"time"
)

type costSnapshotTrackingStore map[string]tracking.Record

func (s costSnapshotTrackingStore) Transaction(_ context.Context, _ string, fn func(tracking.Repository) error) error {
	return fn(s)
}
func (s costSnapshotTrackingStore) Get(kind, id string) (tracking.Record, error) {
	if v, ok := s[kind+":"+id]; ok {
		return v, nil
	}
	return tracking.Record{}, tracking.ErrNotFound
}
func (s costSnapshotTrackingStore) List(kind string) (out []tracking.Record, err error) {
	for _, v := range s {
		if v.Kind == kind {
			out = append(out, v)
		}
	}
	return out, nil
}
func (s costSnapshotTrackingStore) Put(v tracking.Record) error { s[v.Kind+":"+v.ID] = v; return nil }
func (s costSnapshotTrackingStore) Insert(v tracking.Record) error {
	if _, e := s.Get(v.Kind, v.ID); e == nil {
		return errors.New("duplicate fixture")
	}
	return s.Put(v)
}
func (s costSnapshotTrackingStore) Remove(kind, id string) error { delete(s, kind+":"+id); return nil }

func TestPublicCostSnapshotAuthoritativeTrackingAndRevocation(t *testing.T) {
	ctx := context.Background()
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr(), MaxRetries: -1})
	defer client.Close()
	snapshots := model.NewCostEstimateShareRedisModel(client)
	snapshot, err := snapshots.Create(ctx, 42, validCostEstimateShareResult())
	if err != nil {
		t.Fatal(err)
	}
	store := costSnapshotTrackingStore{}
	cfg := tracking.Config{Enabled: true, Tenant: "snapshot-test", DefaultOwner: "sales-one", ConsentVersion: "explicit-v1", RawDays: 90, Routes: map[string]tracking.Route{
		tracking.CostSnapshotRoute:        {Title: "公开造价测算报告", Type: "detail", Public: true},
		"pages/aicostresult/aicostresult": {Title: "私有测算结果", Type: "excluded", Public: false},
	}}
	svc := tracking.New(store, cfg)
	svc.ResolvePublicSnapshot = makeTrackingCostSnapshotResolver(snapshots)
	input := tracking.EntryInput{Route: tracking.CostSnapshotRoute, Content: tracking.CostSnapshotID(snapshot.ReportID), Client: "random-installation-identifier", Consent: "explicit-v1"}
	first, err := svc.Enter(ctx, input, 0)
	if err != nil {
		t.Fatal(err)
	}
	if first.Content != input.Content || first.Version != tracking.SourceSnapshotVersion(costSnapshotMetadata(snapshot)) || !strings.HasPrefix(first.Visitor, "a:") {
		t.Fatalf("unbound anonymous snapshot %+v", first)
	}
	for i, account := range []int64{0, 73} {
		in := input
		in.Client = "repeat-client-" + strings.Repeat("a", i+8)
		entry, e := svc.Enter(ctx, in, account)
		if e != nil {
			t.Fatal(e)
		}
		if account == 73 && entry.Visitor != "u:73" {
			t.Fatal("authenticated visitor was not server bound", entry)
		}
		if entry.Content != first.Content || entry.Version != first.Version {
			t.Fatal("repeat snapshot identity changed")
		}
	}
	share, err := svc.CreateShare(ctx, tracking.ShareInput{Route: input.Route, Content: input.Content, Version: first.Version, Channel: "timeline", Key: "snapshot-share-first"}, first.Token, 0)
	if err != nil {
		t.Fatal(err)
	}
	opened := input
	opened.Client = "another-random-installation"
	opened.ShareToken = share["share_token"].(string)
	recipient, err := svc.Enter(ctx, opened, 73)
	if err != nil || recipient.Share != share["share_id"] {
		t.Fatalf("recipient lost snapshot attribution %+v %v", recipient, err)
	}
	forwarded, err := svc.CreateShare(ctx, tracking.ShareInput{Route: input.Route, Content: input.Content, Version: first.Version, Channel: "timeline", Parent: recipient.Share, Key: "snapshot-share-forward"}, recipient.Token, 73)
	if err != nil {
		t.Fatal(err)
	}
	if forwarded["share_token"] == "" {
		t.Fatal("forward unavailable")
	}
	for _, bad := range []tracking.EntryInput{
		{Route: input.Route, Content: "costsnapshot:bad", Client: input.Client, Consent: input.Consent},
		{Route: input.Route, Content: tracking.CostSnapshotID(strings.Repeat("z", 32)), Client: input.Client, Consent: input.Consent},
		{Route: "pages/aicostresult/aicostresult", Content: input.Content, Client: input.Client, Consent: input.Consent},
		{Route: "pages/report/questionnaire", Content: input.Content, Client: input.Client, Consent: input.Consent},
	} {
		if _, e := svc.Enter(ctx, bad, 0); e == nil {
			t.Fatalf("private or forged source accepted %+v", bad)
		}
	}
	for _, row := range store {
		if row.Kind == "version" || row.Kind == "legacy_catalog" {
			raw, _ := json.Marshal(row)
			if strings.Contains(string(raw), "福建") || strings.Contains(string(raw), "basicInfo") || strings.Contains(string(raw), "80.00万元") {
				t.Fatal("report fields leaked into tracking metadata", string(raw))
			}
		}
	}
	if err = snapshots.Revoke(ctx, snapshot.ReportID, 42); err != nil {
		t.Fatal(err)
	}
	if _, err = svc.Enter(ctx, input, 0); err == nil {
		t.Fatal("revoked public snapshot still trackable")
	}
	if _, err = svc.CreateShare(ctx, tracking.ShareInput{Route: input.Route, Content: input.Content, Version: first.Version, Key: "snapshot-after-revoke"}, first.Token, 0); err == nil {
		t.Fatal("revoked snapshot reforwarded")
	}
	expired, err := snapshots.Create(ctx, 42, validCostEstimateShareResult())
	if err != nil {
		t.Fatal(err)
	}
	mr.FastForward(31 * 24 * time.Hour)
	if _, err = makeTrackingCostSnapshotResolver(snapshots)(ctx, expired.ReportID); err == nil {
		t.Fatal("expired snapshot remained public")
	}
}

func TestPublicCostSnapshotHTTPMetadataMatchesAuthoritativeBinding(t *testing.T) {
	router, mr := newCostEstimateShareRouter(t)
	raw, _ := json.Marshal(validCostEstimateShareResult())
	created := costShareRequest(router, http.MethodPost, "", string(raw), "42")
	if created.Code != http.StatusOK {
		t.Fatal(created.Code, created.Body.String())
	}
	var identity struct {
		Data struct {
			ReportID string `json:"report_id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &identity); err != nil {
		t.Fatal(err)
	}
	id := identity.Data.ReportID
	var firstVersion int
	for _, account := range []string{"", "43", "42"} {
		response := costShareRequest(router, http.MethodGet, "/"+id, "", account)
		if response.Code != http.StatusOK {
			t.Fatal(response.Code, response.Body.String())
		}
		var body struct {
			Data struct {
				Tracking struct {
					Route   string `json:"route_key"`
					Content string `json:"content_id"`
					Version int    `json:"content_version"`
				} `json:"tracking"`
			} `json:"data"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		binding := body.Data.Tracking
		if binding.Route != tracking.CostSnapshotRoute || binding.Content != tracking.CostSnapshotID(id) || binding.Version <= 0 {
			t.Fatalf("HTTP read returned unbound public metadata: %+v", binding)
		}
		if firstVersion != 0 && binding.Version != firstVersion {
			t.Fatal("snapshot version varied by viewer")
		}
		firstVersion = binding.Version
	}
	mr.FastForward(31 * 24 * time.Hour)
	if response := costShareRequest(router, http.MethodGet, "/"+id, "", ""); response.Code != http.StatusNotFound {
		t.Fatal("expired snapshot still returned public identity", response.Code, response.Body.String())
	}
}

func TestRenovationCostSnapshotTrackingAcceptsV5WithoutEmbeddingReportFields(t *testing.T) {
	ctx := context.Background()
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr(), MaxRetries: -1})
	defer client.Close()
	snapshots := model.NewCostEstimateShareRedisModel(client)
	result := validRenovationShareResult()
	result.BasicInfo[0].Value = "敏感地区测试值"
	snapshot, err := snapshots.Create(ctx, 42, result)
	if err != nil {
		t.Fatal(err)
	}
	metadata, err := makeTrackingCostSnapshotResolver(snapshots)(ctx, snapshot.ReportID)
	if err != nil {
		t.Fatal("v5 public snapshot was not accepted by tracking", err)
	}
	raw, _ := json.Marshal(metadata)
	if metadata.Title != "公开造价测算报告" || metadata.Type != "cost_snapshot" || strings.Contains(string(raw), "敏感地区测试值") || strings.Contains(string(raw), "basicInfo") || strings.Contains(string(raw), "2348") {
		t.Fatal("renovation details leaked into tracking metadata", string(raw))
	}
}
