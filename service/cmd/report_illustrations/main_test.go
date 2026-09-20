package main

import (
	"bytes"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"service/component"
)

func TestReuseCompletedRequiresMatchingValidatedAsset(t *testing.T) {
	dir := t.TempDir()
	imageData := valid2KPNG(t)
	if err := os.WriteFile(filepath.Join(dir, "hero.png"), imageData, 0o600); err != nil {
		t.Fatal(err)
	}
	receipt := illustrationReceipt{
		ID: "hero", Status: "complete", InputHash: "input", Provider: component.ReportIllustrationProvider,
		Model: "model", Width: component.ReportIllustrationWidth, Height: component.ReportIllustrationHeight,
		MIMEType: "image/png", ImageFile: "hero.png", ImageSHA256: hashBytes(imageData),
	}
	receiptData, _ := json.Marshal(receipt)
	receiptPath := filepath.Join(dir, "hero.receipt.json")
	if err := os.WriteFile(receiptPath, receiptData, 0o600); err != nil {
		t.Fatal(err)
	}
	reused, err := reuseCompleted(receiptPath, dir, "input", "model")
	if err != nil || !reused {
		t.Fatalf("reused/error = %v/%v", reused, err)
	}
	if _, err := reuseCompleted(receiptPath, dir, "changed", "model"); err == nil || !strings.Contains(err.Error(), "不匹配") {
		t.Fatalf("mismatch error = %v", err)
	}
}

func TestReuseCompletedBlocksSubmittingReceipt(t *testing.T) {
	dir := t.TempDir()
	receipt := illustrationReceipt{
		ID: "hero", Status: "submitting", InputHash: "input", Provider: component.ReportIllustrationProvider,
		Model: "model", Width: component.ReportIllustrationWidth, Height: component.ReportIllustrationHeight,
	}
	data, _ := json.Marshal(receipt)
	path := filepath.Join(dir, "hero.receipt.json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := reuseCompleted(path, dir, "input", "model"); err == nil || !strings.Contains(err.Error(), "submitting") {
		t.Fatalf("error = %v", err)
	}
}

func TestLoadPlanRejectsDuplicateID(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "plan.json")
	data := []byte(`[{"id":"hero","prompt":"one"},{"id":"hero","prompt":"two"}]`)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := loadPlan(path); err == nil || !strings.Contains(err.Error(), "重复") {
		t.Fatalf("error = %v", err)
	}
}

func valid2KPNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewUniform(color.RGBA{R: 40, G: 90, B: 120, A: 255})
	var buf bytes.Buffer
	if err := png.Encode(&buf, struct {
		image.Image
	}{Image: croppedImage{Image: img, rect: image.Rect(0, 0, component.ReportIllustrationWidth, component.ReportIllustrationHeight)}}); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

type croppedImage struct {
	image.Image
	rect image.Rectangle
}

func (i croppedImage) Bounds() image.Rectangle { return i.rect }
