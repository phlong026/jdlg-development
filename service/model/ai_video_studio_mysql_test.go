package model

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	mysql "github.com/go-sql-driver/mysql"
)

// Explicitly opt in with a disposable loopback-only MySQL schema. This never
// reads the application's DSN and cannot be pointed at the production database.
func TestAIVideoStudioMySQLIsolationAndRevisions(t *testing.T) {
	dsn := os.Getenv("AI_VIDEO_STUDIO_TEST_DSN")
	if dsn == "" {
		t.Skip("requires disposable AI_VIDEO_STUDIO_TEST_DSN")
	}
	cfg, err := mysql.ParseDSN(dsn)
	if err != nil || cfg.Net != "tcp" || !strings.HasPrefix(cfg.Addr, "127.0.0.1:") || !strings.HasPrefix(cfg.DBName, "jdlg_studio_test") {
		t.Fatal("test DSN must use a disposable loopback studio schema")
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err = db.PingContext(ctx); err != nil {
		t.Fatal(err)
	}
	store := NewAIVideoStudioStore(db)
	if err = store.InitTables(); err != nil {
		t.Fatal(err)
	}
	media := NewAIVideoStudioMediaStore(db)
	if err = media.InitTable(); err != nil {
		t.Fatal(err)
	}
	config, err := store.Config(ctx, false)
	if err != nil || config.Enabled || config.ProviderConfigured {
		t.Fatalf("default readiness: %+v %v", config, err)
	}
	draft := AIVideoStudioDraft{UserID: 1001, Name: "可恢复草稿", Mode: "text", References: json.RawMessage(`[]`), Ratio: "16:9", Resolution: "768p", Duration: 4, Quantity: 1}
	if err = store.CreateDraft(ctx, &draft); err != nil {
		t.Fatal(err)
	}
	if draft.ID < 1 || draft.Revision != 1 {
		t.Fatalf("new draft=%+v", draft)
	}
	if _, err = store.GetDraft(ctx, 1002, draft.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("cross account draft: %v", err)
	}
	stale := draft
	draft.Prompt = "新的画面描述"
	if err = store.UpdateDraft(ctx, &draft, 1); err != nil {
		t.Fatal(err)
	}
	if err = store.UpdateDraft(ctx, &stale, 1); !errors.Is(err, ErrAIVideoStudioConflict) {
		t.Fatalf("stale update: %v", err)
	}
	if err = store.DeleteDraft(ctx, 1002, draft.ID, draft.Revision); !errors.Is(err, ErrAIVideoStudioConflict) {
		t.Fatalf("cross account delete: %v", err)
	}
	items, total, err := store.ListDrafts(ctx, 1001, "draft", "text", 20, 0)
	if err != nil || total != 1 || len(items) != 1 || items[0].Prompt != draft.Prompt {
		t.Fatalf("persisted drafts: %v %d %v", items, total, err)
	}
	wrongItems, total, err := store.ListDrafts(ctx, 1002, "", "", 20, 0)
	if err != nil || total != 0 || len(wrongItems) != 0 {
		t.Fatalf("cross account list: %v %d %v", wrongItems, total, err)
	}
	prompt := AIVideoStudioPrompt{Name: "庭院镜头", Mode: "text", Prompt: "缓慢推进"}
	if err = store.CreatePrompt(ctx, 1001, &prompt); err != nil {
		t.Fatal(err)
	}
	if err = store.DeletePrompt(ctx, 1002, prompt.ID, prompt.Revision); !errors.Is(err, ErrAIVideoStudioConflict) {
		t.Fatalf("cross account prompt: %v", err)
	}
	asset := AIVideoStudioMedia{ID: strings.Repeat("a", 32), UserID: 1001, OwnerID: "designer-1001", Kind: "audio", MIME: "audio/wave", SizeBytes: 32044, DurationSeconds: 2, StorageKey: strings.Repeat("a", 32), Status: "ready", SHA256: strings.Repeat("b", 64), Name: "本人音频"}
	if err = media.Insert(ctx, &asset); err != nil {
		t.Fatal(err)
	}
	if _, err = media.Get(ctx, 1002, asset.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("cross account media: %v", err)
	}
	loaded, err := media.Get(ctx, 1001, asset.ID)
	if err != nil || loaded.DurationSeconds != 2 || loaded.Kind != "audio" {
		t.Fatalf("stored probe metadata: %+v %v", loaded, err)
	}
	config.Modes = map[string]bool{"text": false}
	expected := config.Version
	if err = store.UpdateConfig(ctx, config, expected); err != nil {
		t.Fatal(err)
	}
	if err = store.UpdateConfig(ctx, config, expected); !errors.Is(err, ErrAIVideoStudioConflict) {
		t.Fatalf("stale config: %v", err)
	}
	if err = store.DeleteDraft(ctx, 1001, draft.ID, draft.Revision); err != nil {
		t.Fatal(err)
	}
	if _, err = store.GetDraft(ctx, 1001, draft.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("deleted draft: %v", err)
	}
}
