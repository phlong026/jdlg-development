package report

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

type countingReportRenderer struct{ calls int }

func (r *countingReportRenderer) Render(context.Context, []byte, string) ([]byte, error) {
	r.calls++
	return []byte("%PDF-1.7\nvalidated-fixture"), nil
}

func TestArtifactCacheReusesValidatedPDFAndSeparatesVersions(t *testing.T) {
	base := &countingReportRenderer{}
	dir := filepath.Join(t.TempDir(), "private")
	renderer, err := NewCachedRenderer(base, dir, "template-and-font-v1")
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if _, err := renderer.Render(context.Background(), []byte(`{"pages":[]}`), ModePreview); err != nil {
			t.Fatal(err)
		}
	}
	if base.calls != 1 {
		t.Fatalf("cached PDF rendered %d times", base.calls)
	}
	other, err := NewCachedRenderer(base, dir, "template-and-font-v2")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := other.Render(context.Background(), []byte(`{"pages":[]}`), ModePreview); err != nil {
		t.Fatal(err)
	}
	if base.calls != 2 {
		t.Fatal("changed template reused stale file")
	}
	info, err := os.Stat(dir)
	if err != nil || info.Mode().Perm() != 0700 {
		t.Fatalf("cache directory permissions: %v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil || info.Mode().Perm() != 0600 {
			t.Fatal("cache artifact is not private")
		}
	}
}

func TestArtifactCacheRejectsPublicDirectoryAndSymlink(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "public")
	if err := os.Mkdir(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if _, err := NewCachedRenderer(&countingReportRenderer{}, dir, "v1"); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("public directory allowed: %v", err)
	}
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(dir, link); err != nil {
		t.Fatal(err)
	}
	if _, err := NewCachedRenderer(&countingReportRenderer{}, link, "v1"); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("symlink allowed: %v", err)
	}
}
