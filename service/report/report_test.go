package report

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestServiceIsolatesUsersAndCreatesIdempotently(t *testing.T) {
	store := newMemoryStore()
	service := testService(t, store, fixedCatalog{pack: approvedPack(t)})

	first, err := service.CreatePreview(context.Background(), 11, validInput())
	if err != nil {
		t.Fatal(err)
	}
	second, err := service.CreatePreview(context.Background(), 11, validInput())
	if err != nil {
		t.Fatal(err)
	}
	if first.ID != second.ID {
		t.Fatalf("idempotent create returned %q and %q", first.ID, second.ID)
	}
	other, err := service.CreatePreview(context.Background(), 12, validInput())
	if err != nil {
		t.Fatal(err)
	}
	if other.ID == first.ID {
		t.Fatal("different users shared one report")
	}
	if _, err := service.Get(context.Background(), 12, first.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-user read error = %v, want ErrNotFound", err)
	}
}

func TestLatestReturnsNewestReportForCurrentUser(t *testing.T) {
	store := newMemoryStore()
	base := time.Date(2026, 9, 6, 9, 0, 0, 0, time.UTC)
	store.reports["A"] = &Report{ID: "A", UserID: 11, Status: StatusPreviewReady, CreatedAt: base}
	store.reports["B"] = &Report{ID: "B", UserID: 11, Status: StatusFullQueued, Paid: true, FullSnapshot: json.RawMessage(`{"secret":true}`), CreatedAt: base.Add(time.Minute)}
	store.reports["C"] = &Report{ID: "C", UserID: 11, Status: StatusPreviewReady, CreatedAt: base.Add(time.Minute)}
	store.reports["Z"] = &Report{ID: "Z", UserID: 22, Status: StatusReady, Paid: true, FullSnapshot: json.RawMessage(`{"secret":true}`), CreatedAt: base.Add(time.Hour)}
	service := testService(t, store, fixedCatalog{pack: approvedPack(t)})

	latest, err := service.Latest(context.Background(), 11)
	if err != nil || latest.ID != "C" {
		t.Fatalf("latest = %#v, err=%v", latest, err)
	}
	other, err := service.Latest(context.Background(), 22)
	if err != nil || other.ID != "Z" {
		t.Fatalf("other latest = %#v, err=%v", other, err)
	}
	publicJSON, _ := json.Marshal(other)
	if strings.Contains(string(publicJSON), "secret") || strings.Contains(string(publicJSON), "full_snapshot") {
		t.Fatalf("latest leaked paid content: %s", publicJSON)
	}
	if _, err := service.Latest(context.Background(), 33); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing latest error = %v", err)
	}
}

func TestInputWhitespaceNormalizesToStableHash(t *testing.T) {
	plain := validInput()
	spaced := validInput()
	spaced.Site.Region = "  福建·泉州\n"
	spaced.Household.Residents = "父母常住，  夫妻与孩子\n周末居住"
	spaced.Preference.Goal = "平时方便，\t团聚时互不打扰"
	plain.Household.Residents = "父母常住， 夫妻与孩子 周末居住"
	plain.Preference.Goal = "平时方便， 团聚时互不打扰"
	plainJSON, plainHash, err := MarshalInput(plain)
	if err != nil {
		t.Fatal(err)
	}
	spacedJSON, spacedHash, err := MarshalInput(spaced)
	if err != nil {
		t.Fatal(err)
	}
	if plainHash != spacedHash || string(plainJSON) != string(spacedJSON) {
		t.Fatalf("equivalent whitespace changed frozen input: %s != %s", plainJSON, spacedJSON)
	}
}

func TestFrozenInputAcceptsEquivalentMySQLJSONSerialization(t *testing.T) {
	canonical, hash, err := MarshalInput(validInput())
	if err != nil {
		t.Fatal(err)
	}
	var generic map[string]any
	if err := json.Unmarshal(canonical, &generic); err != nil {
		t.Fatal(err)
	}
	reordered, err := json.MarshalIndent(generic, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if string(reordered) == string(canonical) {
		t.Fatal("test fixture must differ from canonical JSON bytes")
	}
	record := &Report{ID: "R-JSON", ProtocolVersion: "v1", InputHash: hash, InputJSON: reordered}
	snapshotJSON, _, err := NewGenerator(nil).BuildPreview(context.Background(), record)
	if err != nil {
		t.Fatalf("equivalent MySQL JSON serialization rejected: %v", err)
	}
	var snapshot Snapshot
	if err := json.Unmarshal(snapshotJSON, &snapshot); err != nil {
		t.Fatal(err)
	}
	if snapshot.Facts.Region != validInput().Site.Region || snapshot.Meta.InputHash != hash {
		t.Fatalf("normalized facts changed: %#v", snapshot.Facts)
	}
}

func TestFrozenInputRejectsSemanticTampering(t *testing.T) {
	canonical, hash, err := MarshalInput(validInput())
	if err != nil {
		t.Fatal(err)
	}
	var tampered Input
	if err := json.Unmarshal(canonical, &tampered); err != nil {
		t.Fatal(err)
	}
	tampered.Site.Region = "福建·厦门"
	tamperedJSON, err := json.MarshalIndent(tampered, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	record := &Report{ID: "R-TAMPER", ProtocolVersion: "v1", InputHash: hash, InputJSON: tamperedJSON}
	if _, _, err := NewGenerator(nil).BuildPreview(context.Background(), record); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("semantic tampering error = %v", err)
	}
}

func TestInputRejectsControlCharactersOutsideNarrativeFields(t *testing.T) {
	input := validInput()
	input.Site.RoadSide = "东侧\x00道路"
	if _, _, err := MarshalInput(input); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("control character error = %v", err)
	}
}

func TestPaymentRequiresReadyPreviewReviewedPackAndReservedOrder(t *testing.T) {
	store := newMemoryStore()
	pack := approvedPack(t)
	service := testService(t, store, fixedCatalog{pack: pack})
	view, err := service.CreatePreview(context.Background(), 11, validInput())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.ReservePaymentOrder(context.Background(), 11, view.ID, "ORDER-1"); err == nil {
		t.Fatal("payment order was reserved before preview was ready")
	}
	worker := testWorker(t, store, fixedCatalog{pack: pack}, fakeRenderer{}, newMemoryStorage(), 3)
	if worked, err := worker.RunOnce(context.Background()); err != nil || !worked {
		t.Fatalf("preview worker = %t, %v", worked, err)
	}
	quote, err := service.GetPaymentQuote(context.Background(), 11, view.ID)
	if err != nil || !quote.CanPay || quote.ProductID != "REPORT-A" || quote.PriceFen != 9900 || quote.PackID != pack.ID {
		t.Fatalf("quote = %#v, err=%v", quote, err)
	}
	order, err := service.ReservePaymentOrder(context.Background(), 11, view.ID, "ORDER-1")
	if err != nil || order.OrderNo != "ORDER-1" || order.PriceFen != 9900 || order.PackID != pack.ID {
		t.Fatalf("reserve = %q, %v", order, err)
	}
	order, err = service.ReservePaymentOrder(context.Background(), 11, view.ID, "ORDER-2")
	if err != nil || order.OrderNo != "ORDER-1" {
		t.Fatalf("duplicate reserve = %q, %v", order, err)
	}
	if _, err := service.AuthorizeVerifiedPayment(context.Background(), 11, view.ID, "ORDER-2"); !errors.Is(err, ErrConflict) {
		t.Fatalf("unreserved payment error = %v, want conflict", err)
	}
	paid, err := service.AuthorizeVerifiedPayment(context.Background(), 11, view.ID, "ORDER-1")
	if err != nil || !paid.Paid || paid.Status != StatusFullQueued {
		t.Fatalf("paid view = %#v, err=%v", paid, err)
	}
	if _, err := service.AuthorizeVerifiedPayment(context.Background(), 11, view.ID, "ORDER-1"); err != nil {
		t.Fatalf("same payment callback must be idempotent: %v", err)
	}
	if _, err := service.AuthorizeVerifiedPayment(context.Background(), 11, view.ID, "ORDER-3"); !errors.Is(err, ErrConflict) {
		t.Fatalf("payment reference changed: %v", err)
	}
}

func TestWorkerFailureKeepsFrozenPreviewAndRetryCannotLosePayment(t *testing.T) {
	store := newMemoryStore()
	pack := approvedPack(t)
	service := testService(t, store, fixedCatalog{pack: pack})
	view, _ := service.CreatePreview(context.Background(), 11, validInput())
	previewWorker := testWorker(t, store, fixedCatalog{pack: pack}, fakeRenderer{}, newMemoryStorage(), 1)
	if _, err := previewWorker.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	before, _ := store.GetForUser(context.Background(), 11, view.ID)
	frozen := append([]byte(nil), before.PreviewSnapshot...)
	if _, err := service.ReservePaymentOrder(context.Background(), 11, view.ID, "ORDER-1"); err != nil {
		t.Fatal(err)
	}
	if _, err := service.AuthorizeVerifiedPayment(context.Background(), 11, view.ID, "ORDER-1"); err != nil {
		t.Fatal(err)
	}
	failing := fakeRenderer{errMode: ModeFull}
	fullWorker := testWorker(t, store, fixedCatalog{pack: pack}, failing, newMemoryStorage(), 1)
	if _, err := fullWorker.RunOnce(context.Background()); err == nil {
		t.Fatal("full rendering failure was hidden")
	}
	after, _ := store.GetForUser(context.Background(), 11, view.ID)
	if after.Status != StatusFailed || !after.Paid || string(after.PreviewSnapshot) != string(frozen) {
		t.Fatalf("failed full job changed entitlement or preview: %#v", after)
	}
	retried, err := service.Retry(context.Background(), 11, view.ID)
	if err != nil || retried.Status != StatusFullQueued || !retried.Paid {
		t.Fatalf("retry = %#v, %v", retried, err)
	}
}

func TestClaimIsAtomicAcrossWorkers(t *testing.T) {
	store := newMemoryStore()
	service := testService(t, store, fixedCatalog{pack: approvedPack(t)})
	if _, err := service.CreatePreview(context.Background(), 11, validInput()); err != nil {
		t.Fatal(err)
	}
	var successes atomic.Int32
	var group sync.WaitGroup
	for range 8 {
		group.Add(1)
		go func() {
			defer group.Done()
			if _, err := store.Claim(context.Background(), time.Now(), time.Minute, 3); err == nil {
				successes.Add(1)
			}
		}()
	}
	group.Wait()
	if successes.Load() != 1 {
		t.Fatalf("claims = %d, want 1", successes.Load())
	}
}

func TestAITextCannotRewriteFrozenFacts(t *testing.T) {
	inputJSON, hash, _ := MarshalInput(validInput())
	record := &Report{ID: "R-1", InputHash: hash, ProtocolVersion: "v1", InputJSON: inputJSON}
	generator := NewGenerator(fakeTextClient{result: TextResult{ProfileInsight: "AI画像解释", CoreTradeoff: "AI取舍解释", Recommendation: "AI建议解释"}})
	snapshotJSON, mode, err := generator.BuildPreview(context.Background(), record)
	if err != nil {
		t.Fatal(err)
	}
	var snapshot Snapshot
	if err := json.Unmarshal(snapshotJSON, &snapshot); err != nil {
		t.Fatal(err)
	}
	if mode != GenerationAIAssisted || snapshot.Facts.Region != validInput().Site.Region || snapshot.Facts.BudgetWan != validInput().Budget.Wan || snapshot.Meta.InputHash != hash {
		t.Fatalf("AI altered fixed fields: %#v", snapshot)
	}
	if snapshot.Pages[2].Blocks[0].Body != "AI画像解释" {
		t.Fatal("bounded AI explanation was not placed in its slot")
	}
}

func TestInvalidManifestIsRejected(t *testing.T) {
	pack := approvedPack(t)
	delete(pack.Assets, pack.Schemes[0].FacadeAssetID)
	manifest := struct {
		Version string         `json:"version"`
		Packs   []ReviewedPack `json:"packs"`
	}{Version: "reviewed-pack-v1", Packs: []ReviewedPack{pack}}
	data, _ := json.Marshal(manifest)
	path := filepath.Join(t.TempDir(), "manifest.json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewFilePackCatalog(path); !errors.Is(err, ErrPackUnavailable) {
		t.Fatalf("invalid manifest error = %v", err)
	}
}

func TestManifestRejectsDuplicatePackIdentity(t *testing.T) {
	pack := approvedPack(t)
	manifest := struct {
		Version string         `json:"version"`
		Packs   []ReviewedPack `json:"packs"`
	}{Version: "reviewed-pack-v1", Packs: []ReviewedPack{pack, pack}}
	data, _ := json.Marshal(manifest)
	path := filepath.Join(t.TempDir(), "manifest.json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewFilePackCatalog(path); !errors.Is(err, ErrPackUnavailable) {
		t.Fatalf("duplicate pack identity error = %v", err)
	}
}

func TestManifestCopyIsBoundedForPDFSlots(t *testing.T) {
	pack := approvedPack(t)
	pack.Schemes[0].Plans[0].Summary = strings.Repeat("字", 121)
	if err := ValidateReviewedPack(pack); !errors.Is(err, ErrPackUnavailable) {
		t.Fatalf("oversized floor copy error = %v", err)
	}
	pack = approvedPack(t)
	pack.Schemes[0].Tradeoff = "第一行\n第二行"
	if err := ValidateReviewedPack(pack); !errors.Is(err, ErrPackUnavailable) {
		t.Fatalf("multiline scheme copy error = %v", err)
	}
	pack = approvedPack(t)
	pack.Schemes[0].Plans[0].Name = strings.Repeat("层", 13)
	if err := ValidateReviewedPack(pack); !errors.Is(err, ErrPackUnavailable) {
		t.Fatalf("oversized floor name error = %v", err)
	}
}

func TestFilePackCatalogRequiresAssetsInsideVerifiedRoot(t *testing.T) {
	root := t.TempDir()
	pack := approvedPack(t)
	for id := range pack.Assets {
		path := filepath.Join(root, id+".png")
		writePNGFile(t, path)
		pack.Assets[id] = path
	}
	manifest := struct {
		Version string         `json:"version"`
		Packs   []ReviewedPack `json:"packs"`
	}{Version: "reviewed-pack-v1", Packs: []ReviewedPack{pack}}
	data, _ := json.Marshal(manifest)
	path := filepath.Join(t.TempDir(), "manifest.json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	catalog, err := NewFilePackCatalog(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := catalog.Match(validInput()); !errors.Is(err, ErrPackUnavailable) {
		t.Fatalf("unverified catalog became payable: %v", err)
	}
	if err := catalog.VerifyAssetRoot(root); err != nil {
		t.Fatalf("verify asset root: %v", err)
	}
	if _, err := catalog.Match(validInput()); err != nil {
		t.Fatalf("verified catalog did not match: %v", err)
	}
}

func TestFilePackCatalogRejectsAssetEscapingRoot(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside.png")
	writePNGFile(t, outside)
	pack := approvedPack(t)
	for id := range pack.Assets {
		pack.Assets[id] = outside
	}
	manifest := struct {
		Version string         `json:"version"`
		Packs   []ReviewedPack `json:"packs"`
	}{Version: "reviewed-pack-v1", Packs: []ReviewedPack{pack}}
	data, _ := json.Marshal(manifest)
	path := filepath.Join(t.TempDir(), "manifest.json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	catalog, err := NewFilePackCatalog(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := catalog.VerifyAssetRoot(root); !errors.Is(err, ErrPackUnavailable) {
		t.Fatalf("escaping asset error = %v", err)
	}
}

func TestFilePackCatalogRejectsInvalidImageAndSymlinkEscape(t *testing.T) {
	for _, test := range []struct {
		name  string
		asset func(*testing.T, string, string) string
	}{
		{name: "invalid extension", asset: func(t *testing.T, root, _ string) string {
			path := filepath.Join(root, "asset.txt")
			writePNGFile(t, path)
			return path
		}},
		{name: "fake image", asset: func(t *testing.T, root, _ string) string {
			path := filepath.Join(root, "asset.png")
			if err := os.WriteFile(path, []byte("not-an-image"), 0o600); err != nil {
				t.Fatal(err)
			}
			return path
		}},
		{name: "truncated image", asset: func(t *testing.T, root, _ string) string {
			path := filepath.Join(root, "asset.png")
			writePNGFile(t, path)
			data, err := os.ReadFile(path)
			if err != nil || len(data) < 33 {
				t.Fatalf("read PNG fixture: bytes=%d err=%v", len(data), err)
			}
			if err := os.WriteFile(path, data[:33], 0o600); err != nil {
				t.Fatal(err)
			}
			return path
		}},
		{name: "symlink escape", asset: func(t *testing.T, root, outside string) string {
			path := filepath.Join(root, "asset.png")
			if err := os.Symlink(outside, path); err != nil {
				t.Fatal(err)
			}
			return path
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			outside := filepath.Join(t.TempDir(), "outside.png")
			writePNGFile(t, outside)
			badAsset := test.asset(t, root, outside)
			pack := approvedPack(t)
			for id := range pack.Assets {
				pack.Assets[id] = badAsset
			}
			manifest := struct {
				Version string         `json:"version"`
				Packs   []ReviewedPack `json:"packs"`
			}{Version: "reviewed-pack-v1", Packs: []ReviewedPack{pack}}
			data, _ := json.Marshal(manifest)
			path := filepath.Join(t.TempDir(), "manifest.json")
			if err := os.WriteFile(path, data, 0o600); err != nil {
				t.Fatal(err)
			}
			catalog, err := NewFilePackCatalog(path)
			if err != nil {
				t.Fatal(err)
			}
			if err := catalog.VerifyAssetRoot(root); !errors.Is(err, ErrPackUnavailable) {
				t.Fatalf("unsafe image error = %v", err)
			}
		})
	}
}

func TestFreePreviewDoesNotRequirePaymentConfiguration(t *testing.T) {
	store := newMemoryStore()
	config := testConfig(3)
	config.PaymentEnabled = false
	service, err := NewService(config, store, fixedCatalog{pack: approvedPack(t)}, nil)
	if err != nil {
		t.Fatal(err)
	}
	view, err := service.CreatePreview(context.Background(), 11, validInput())
	if err != nil || view.CanPay || view.PaymentBlockReason != "payment_disabled" {
		t.Fatalf("free preview = %#v, %v", view, err)
	}
}

func TestFullSnapshotPreservesFirstFourPagesAndUsesDistinctVolumeAssets(t *testing.T) {
	inputJSON, hash, _ := MarshalInput(validInput())
	record := &Report{ID: "R-1", InputHash: hash, ProtocolVersion: "v1", InputJSON: inputJSON}
	generator := NewGenerator(nil)
	previewJSON, _, err := generator.BuildPreview(context.Background(), record)
	if err != nil {
		t.Fatal(err)
	}
	pack := approvedPack(t)
	fullJSON, err := generator.BuildFull(previewJSON, pack)
	if err != nil {
		t.Fatal(err)
	}
	var preview, full Snapshot
	_ = json.Unmarshal(previewJSON, &preview)
	_ = json.Unmarshal(fullJSON, &full)
	firstFour, _ := json.Marshal(full.Pages[:4])
	want, _ := json.Marshal(preview.Pages)
	if string(firstFour) != string(want) || full.Schemes[0].FacadeAsset == full.Schemes[0].VolumeAsset {
		t.Fatal("full report changed frozen pages or reused facade as volume asset")
	}
	pack.Schemes[0].VolumeAssetID = pack.Schemes[0].FacadeAssetID
	if err := ValidateReviewedPack(pack); !errors.Is(err, ErrPackUnavailable) {
		t.Fatalf("shared facade/volume asset error = %v", err)
	}
}

func TestPackMatchRequiresBedroomElderRoomAndBudgetScope(t *testing.T) {
	pack := approvedPack(t)
	catalog := fixedCatalog{pack: pack}
	input := validInput()
	if _, err := catalog.Match(input); err != nil {
		t.Fatalf("matching hard requirements rejected: %v", err)
	}

	for _, mutate := range []func(*ReviewedPack){
		func(p *ReviewedPack) { p.Schemes[1].Bedrooms-- },
		func(p *ReviewedPack) { p.Schemes[1].FirstFloorElderRoom = false },
		func(p *ReviewedPack) { p.BudgetScope = "主体毛坯" },
	} {
		candidate := approvedPack(t)
		mutate(&candidate)
		if packSupportsInput(candidate, input) {
			t.Fatal("pack with mismatched hard requirement was accepted")
		}
	}
}

func TestComparisonUsesReviewedSchemeFactsAndDeterministicRanking(t *testing.T) {
	inputJSON, hash, _ := MarshalInput(validInput())
	record := &Report{ID: "R-1", InputHash: hash, ProtocolVersion: "v1", InputJSON: inputJSON}
	generator := NewGenerator(nil)
	preview, _, err := generator.BuildPreview(context.Background(), record)
	if err != nil {
		t.Fatal(err)
	}
	pack := approvedPack(t)
	fullJSON, err := generator.BuildFull(preview, pack)
	if err != nil {
		t.Fatal(err)
	}
	var full Snapshot
	if err := json.Unmarshal(fullJSON, &full); err != nil {
		t.Fatal(err)
	}
	comparison := full.Pages[11]
	if comparison.Table[1][1] != "5卧室" || comparison.Table[4][1] != pack.Schemes[0].Gain || comparison.Table[5][1] != pack.Schemes[0].Tradeoff {
		t.Fatalf("comparison did not use reviewed scheme facts: %#v", comparison.Table)
	}
	if !strings.Contains(comparison.Blocks[0].Body, "方案 A") {
		t.Fatalf("deterministic recommendation missing: %q", comparison.Blocks[0].Body)
	}
	if full.Schemes[0].Bedrooms != 5 || !full.Schemes[0].FirstFloorElderRoom {
		t.Fatalf("snapshot lost hard requirements: %#v", full.Schemes[0])
	}
}

func TestLeaseTokenMakesRenderedObjectImmutable(t *testing.T) {
	record := &Report{ID: "R-1", UserID: 11}
	first := reportObjectKey(record, ModeFull, "lease-1")
	second := reportObjectKey(record, ModeFull, "lease-2")
	if first == second {
		t.Fatal("different worker leases would overwrite the same object")
	}
}

func TestFreePreviewCompletesWithoutPDFInfrastructure(t *testing.T) {
	store := newMemoryStore()
	config := testConfig(3)
	config.PaymentEnabled = false
	service, err := NewService(config, store, fixedCatalog{pack: approvedPack(t)}, newMemoryStorage())
	if err != nil {
		t.Fatal(err)
	}
	view, err := service.CreatePreview(context.Background(), 11, validInput())
	if err != nil {
		t.Fatal(err)
	}
	worker, err := NewWorker(config, store, NewGenerator(nil), fixedCatalog{pack: approvedPack(t)}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if worked, err := worker.RunOnce(context.Background()); err != nil || !worked {
		t.Fatalf("free preview worker = %v, %v", worked, err)
	}
	record, _ := store.GetForUser(context.Background(), 11, view.ID)
	if record.Status != StatusPreviewReady || len(record.PreviewSnapshot) == 0 || record.PreviewObjectKey != "" {
		t.Fatalf("free preview not frozen without PDF: %#v", record)
	}
}

func TestPaidWorkerRequiresPDFInfrastructure(t *testing.T) {
	config := testConfig(3)
	if _, err := NewWorker(config, newMemoryStore(), NewGenerator(nil), fixedCatalog{pack: approvedPack(t)}, nil, nil); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("paid worker without PDF infrastructure error = %v", err)
	}
}

func TestEnsurePreviewPDFBackfillsFrozenPreview(t *testing.T) {
	store := newMemoryStore()
	storage := newMemoryStorage()
	config := testConfig(3)
	config.PaymentEnabled = false
	service, err := NewService(config, store, fixedCatalog{pack: approvedPack(t)}, storage)
	if err != nil {
		t.Fatal(err)
	}
	view, err := service.CreatePreview(context.Background(), 11, validInput())
	if err != nil {
		t.Fatal(err)
	}
	worker, _ := NewWorker(config, store, NewGenerator(nil), fixedCatalog{pack: approvedPack(t)}, nil, nil)
	if _, err := worker.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := service.EnsurePreviewPDF(context.Background(), 11, view.ID, fakeRenderer{}); err != nil {
		t.Fatalf("backfill preview PDF: %v", err)
	}
	record, _ := store.GetForUser(context.Background(), 11, view.ID)
	if record.PreviewObjectKey == "" || len(storage.objects) != 1 {
		t.Fatalf("preview PDF was not persisted: key=%q objects=%d", record.PreviewObjectKey, len(storage.objects))
	}
	if err := service.EnsurePreviewPDF(context.Background(), 11, view.ID, fakeRenderer{}); err != nil || len(storage.objects) != 1 {
		t.Fatalf("existing preview PDF should be reused: err=%v objects=%d", err, len(storage.objects))
	}
}

func validInput() Input {
	return Input{
		Site:       SiteStep{Region: "福建·泉州", WidthM: 12, DepthM: 10},
		Household:  HouseholdStep{FamilyType: "三代同堂", Residents: "父母常住，夫妻与孩子周末居住"},
		Space:      SpaceStep{Floors: 3, Bedrooms: 5, FirstFloorElderRoom: true},
		Preference: PreferenceStep{Style: "新中式", Priorities: []string{"老人起居", "预算控制", "家庭团聚"}, Goal: "平时方便，团聚时互不打扰"},
		Budget:     BudgetStep{Wan: 120, Scope: "含室内装修"},
	}
}

func approvedPack(t *testing.T) ReviewedPack {
	t.Helper()
	pack := ReviewedPack{
		ID: "PACK-001", Version: "V1", ReviewStatus: "approved", Styles: []string{"新中式"},
		MinWidthM: 10, MaxWidthM: 15, MinDepthM: 8, MaxDepthM: 15, Floors: 3,
		BudgetScope:    "含室内装修",
		Assets:         map[string]string{},
		BudgetTemplate: []BudgetAllocation{{Name: "主体", Percent: 35}, {Name: "外装", Percent: 20}, {Name: "内装", Percent: 25}, {Name: "机电", Percent: 10}, {Name: "预留", Percent: 10}},
	}
	priorityFits := [][]string{{"老人起居", "家庭团聚", "预算控制"}, {"采光通风", "庭院生活", "家庭团聚"}, {"外观体面", "采光通风", "家庭团聚"}}
	for index, id := range []string{"A", "B", "C"} {
		scheme := PackScheme{ID: id, Name: "方案" + id, Tagline: "取舍方向", Tradeoff: "需要接受的取舍" + id, Gain: "获得的价值" + id, Facade: "成套立面", FacadeCaution: "需要专业深化", Bedrooms: 5, FirstFloorElderRoom: true, PriorityFit: priorityFits[index], CostSensitive: []string{"外墙材料", "门窗性能"}}
		for floor := 1; floor <= 3; floor++ {
			assetID := fmt.Sprintf("%s-plan-%d", id, floor)
			pack.Assets[assetID] = writeAsset(t, assetID)
			scheme.Plans = append(scheme.Plans, PackPlan{Floor: floor, Name: fmt.Sprintf("%d层", floor), Summary: "已审核平面说明", AssetID: assetID})
		}
		scheme.FacadeAssetID = id + "-facade"
		pack.Assets[scheme.FacadeAssetID] = writeAsset(t, scheme.FacadeAssetID)
		scheme.VolumeAssetID = id + "-volume"
		pack.Assets[scheme.VolumeAssetID] = writeAsset(t, scheme.VolumeAssetID)
		pack.Schemes = append(pack.Schemes, scheme)
	}
	return pack
}

func writeAsset(t *testing.T, name string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "assets")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, name+".png")
	writePNGFile(t, path)
	return path
}

func writePNGFile(t *testing.T, path string) {
	t.Helper()
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	asset := image.NewRGBA(image.Rect(0, 0, 2, 2))
	asset.Set(0, 0, color.RGBA{R: 180, G: 150, B: 100, A: 255})
	if err := png.Encode(file, asset); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}

type fixedCatalog struct{ pack ReviewedPack }

func (c fixedCatalog) Match(input Input) (ReviewedPack, error) {
	if err := ValidateReviewedPack(c.pack); err != nil {
		return ReviewedPack{}, err
	}
	if !packSupportsInput(c.pack, input) {
		return ReviewedPack{}, ErrPackUnavailable
	}
	return c.pack, nil
}

func (c fixedCatalog) Get(id, version string) (ReviewedPack, error) {
	if c.pack.ID != id || c.pack.Version != version {
		return ReviewedPack{}, ErrPackUnavailable
	}
	if err := ValidateReviewedPack(c.pack); err != nil {
		return ReviewedPack{}, err
	}
	return c.pack, nil
}

type fakeTextClient struct{ result TextResult }

func (c fakeTextClient) Generate(context.Context, TextRequest) (TextResult, error) {
	return c.result, nil
}

type fakeRenderer struct{ errMode string }

func (r fakeRenderer) Render(_ context.Context, _ []byte, mode string) ([]byte, error) {
	if r.errMode == mode {
		return nil, errors.New("renderer failed")
	}
	return []byte("%PDF-1.7\ntest"), nil
}

type memoryStorage struct {
	mu      sync.Mutex
	objects map[string][]byte
}

func newMemoryStorage() *memoryStorage { return &memoryStorage{objects: map[string][]byte{}} }

func (s *memoryStorage) UploadPrivate(_ context.Context, key string, data []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.objects[key] = append([]byte(nil), data...)
	return nil
}

func (s *memoryStorage) SignDownload(_ context.Context, key string, _ time.Duration) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.objects[key]; !ok {
		return "", ErrNotReady
	}
	return "https://private.invalid/signed", nil
}

func testConfig(maxAttempts int) Config {
	return Config{Enabled: true, PaymentEnabled: true, ProtocolVersion: "report-service-v1", ProductID: "REPORT-A", PriceFen: 9900, LeaseDuration: 90 * time.Second, JobTimeout: time.Minute, DownloadTTL: time.Minute, MaxAttempts: maxAttempts}
}

func testService(t *testing.T, store Store, catalog PackCatalog) *Service {
	t.Helper()
	service, err := NewService(testConfig(3), store, catalog, nil)
	if err != nil {
		t.Fatal(err)
	}
	return service
}

func testWorker(t *testing.T, store Store, catalog PackCatalog, renderer Renderer, storage PrivateStorage, attempts int) *Worker {
	t.Helper()
	worker, err := NewWorker(testConfig(attempts), store, NewGenerator(nil), catalog, renderer, storage)
	if err != nil {
		t.Fatal(err)
	}
	return worker
}

type memoryStore struct {
	mu      sync.Mutex
	reports map[string]*Report
	byKey   map[string]string
}

func newMemoryStore() *memoryStore {
	return &memoryStore{reports: map[string]*Report{}, byKey: map[string]string{}}
}

func (s *memoryStore) CreateOrGet(_ context.Context, record *Report) (*Report, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := fmt.Sprintf("%d/%s/%s", record.UserID, record.InputHash, record.ProtocolVersion)
	if id := s.byKey[key]; id != "" {
		return cloneReport(s.reports[id]), false, nil
	}
	s.reports[record.ID] = cloneReport(record)
	s.byKey[key] = record.ID
	return cloneReport(record), true, nil
}

func (s *memoryStore) GetForUser(_ context.Context, userID int64, reportID string) (*Report, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	record := s.reports[reportID]
	if record == nil || record.UserID != userID {
		return nil, ErrNotFound
	}
	return cloneReport(record), nil
}

func (s *memoryStore) GetLatestForUser(_ context.Context, userID int64) (*Report, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var latest *Report
	for _, record := range s.reports {
		if record.UserID != userID {
			continue
		}
		if latest == nil || record.CreatedAt.After(latest.CreatedAt) || record.CreatedAt.Equal(latest.CreatedAt) && record.ID > latest.ID {
			latest = record
		}
	}
	if latest == nil {
		return nil, ErrNotFound
	}
	return cloneReport(latest), nil
}

func (s *memoryStore) ReservePaymentOrder(_ context.Context, userID int64, reportID string, reservation PaymentReservation) (PaymentReservation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	record := s.reports[reportID]
	if record == nil || record.UserID != userID {
		return PaymentReservation{}, ErrNotFound
	}
	if record.PaymentRef != "" {
		return reservationFromRecord(record, record.PaymentRef), nil
	}
	if record.PendingOrderNo != "" {
		return reservationFromRecord(record, record.PendingOrderNo), nil
	}
	if record.Status != StatusPreviewReady {
		return PaymentReservation{}, ErrConflict
	}
	record.PendingOrderNo = reservation.OrderNo
	record.ReservedProductID, record.ReservedPriceFen = reservation.ProductID, reservation.PriceFen
	record.ReservedPackID, record.ReservedPackVersion = reservation.PackID, reservation.PackVersion
	return reservation, nil
}

func (s *memoryStore) AuthorizePaid(_ context.Context, userID int64, reportID, paymentRef string) (*Report, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	record := s.reports[reportID]
	if record == nil || record.UserID != userID {
		return nil, ErrNotFound
	}
	if record.Paid {
		if record.PaymentRef != paymentRef {
			return nil, ErrConflict
		}
		return cloneReport(record), nil
	}
	if record.PendingOrderNo != paymentRef || record.Status != StatusPreviewReady {
		return nil, ErrConflict
	}
	record.Paid, record.PaymentRef, record.PackID, record.PackVersion = true, paymentRef, record.ReservedPackID, record.ReservedPackVersion
	record.Status, record.AttemptCount = StatusFullQueued, 0
	return cloneReport(record), nil
}

func (s *memoryStore) BindEnterpriseContact(_ context.Context, userID int64, reportID, external string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	record := s.reports[reportID]
	if record == nil || record.UserID != userID {
		return ErrNotFound
	}
	if record.Bound && record.ExternalUserID != external {
		return ErrConflict
	}
	record.Bound, record.ExternalUserID = true, external
	return nil
}

func (s *memoryStore) RetryFailed(_ context.Context, userID int64, reportID string) (*Report, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	record := s.reports[reportID]
	if record == nil || record.UserID != userID {
		return nil, ErrNotFound
	}
	if record.Status != StatusFailed {
		return nil, ErrConflict
	}
	record.Status = StatusPreviewQueued
	if record.Paid {
		record.Status = StatusFullQueued
	}
	record.AttemptCount, record.ErrorCode, record.FailedStage = 0, "", ""
	return cloneReport(record), nil
}

func (s *memoryStore) Claim(_ context.Context, now time.Time, lease time.Duration, max int) (*Job, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, record := range s.reports {
		queued := record.Status == StatusPreviewQueued || record.Status == StatusFullQueued
		expired := (record.Status == StatusPreviewGenerating || record.Status == StatusFullGenerating) && record.LeaseUntil != nil && record.LeaseUntil.Before(now)
		if (!queued && !expired) || record.AttemptCount >= max {
			continue
		}
		mode, status := ModePreview, StatusPreviewGenerating
		if record.Status == StatusFullQueued || record.Status == StatusFullGenerating {
			if !record.Paid {
				return nil, ErrConflict
			}
			mode, status = ModeFull, StatusFullGenerating
		}
		token, until := fmt.Sprintf("lease-%d", record.AttemptCount+1), now.Add(lease)
		record.Status, record.AttemptCount, record.LeaseToken, record.LeaseUntil = status, record.AttemptCount+1, token, &until
		return &Job{Report: cloneReport(record), Mode: mode, LeaseToken: token}, nil
	}
	return nil, ErrNoJob
}

func (s *memoryStore) CompletePreview(_ context.Context, id, token string, snapshot json.RawMessage, mode, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	r := s.reports[id]
	if r == nil || r.Status != StatusPreviewGenerating || r.LeaseToken != token {
		return ErrConflict
	}
	r.PreviewSnapshot, r.GenerationMode, r.PreviewObjectKey, r.Status = append([]byte(nil), snapshot...), mode, key, StatusPreviewReady
	r.AttemptCount, r.LeaseToken, r.LeaseUntil = 0, "", nil
	return nil
}

func (s *memoryStore) SetPreviewObjectKeyIfEmpty(_ context.Context, userID int64, id, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	r := s.reports[id]
	if r == nil || r.UserID != userID || len(r.PreviewSnapshot) == 0 {
		return ErrConflict
	}
	if r.PreviewObjectKey == "" {
		r.PreviewObjectKey = key
	}
	return nil
}

func (s *memoryStore) CompleteFull(_ context.Context, id, token string, snapshot json.RawMessage, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	r := s.reports[id]
	if r == nil || !r.Paid || r.Status != StatusFullGenerating || r.LeaseToken != token {
		return ErrConflict
	}
	r.FullSnapshot, r.FullObjectKey, r.Status = append([]byte(nil), snapshot...), key, StatusReady
	r.LeaseToken, r.LeaseUntil = "", nil
	return nil
}

func (s *memoryStore) Fail(_ context.Context, id, token, stage, code string, retry bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	r := s.reports[id]
	if r == nil || r.LeaseToken != token {
		return ErrConflict
	}
	r.Status = StatusFailed
	if retry {
		r.Status = StatusPreviewQueued
		if stage == ModeFull {
			r.Status = StatusFullQueued
		}
	}
	r.FailedStage, r.ErrorCode, r.LeaseToken, r.LeaseUntil = stage, code, "", nil
	return nil
}

func cloneReport(source *Report) *Report {
	if source == nil {
		return nil
	}
	copy := *source
	copy.InputJSON = append([]byte(nil), source.InputJSON...)
	copy.PreviewSnapshot = append([]byte(nil), source.PreviewSnapshot...)
	copy.FullSnapshot = append([]byte(nil), source.FullSnapshot...)
	return &copy
}
