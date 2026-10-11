package config

import "testing"

func TestLaoZhangImageConfigDefaults(t *testing.T) {
	t.Setenv("LAOZHANG_IMAGE_BASE_URL", "")
	t.Setenv("LAOZHANG_IMAGE_MODEL", "")

	cfg := Init()
	if cfg.AI.LaoZhangImageBaseURL != "https://api2.laozhang.ai" {
		t.Fatalf("LaoZhangImageBaseURL = %q, want production image gateway", cfg.AI.LaoZhangImageBaseURL)
	}
	if cfg.AI.LaoZhangImageModel != "gpt-image-2-vip" {
		t.Fatalf("LaoZhangImageModel = %q, want production image model", cfg.AI.LaoZhangImageModel)
	}
}

func TestLaoZhangImageConfigNormalizesDeploymentValues(t *testing.T) {
	t.Setenv("LAOZHANG_IMAGE_BASE_URL", "  https://images.example.com///  ")
	t.Setenv("LAOZHANG_IMAGE_MODEL", "  custom-image-model  ")

	cfg := Init()
	if cfg.AI.LaoZhangImageBaseURL != "https://images.example.com" {
		t.Fatalf("LaoZhangImageBaseURL = %q, want trimmed base URL", cfg.AI.LaoZhangImageBaseURL)
	}
	if cfg.AI.LaoZhangImageModel != "custom-image-model" {
		t.Fatalf("LaoZhangImageModel = %q, want trimmed model", cfg.AI.LaoZhangImageModel)
	}
}

func TestLaoZhangImageConfigWhitespaceFallsBack(t *testing.T) {
	t.Setenv("LAOZHANG_IMAGE_BASE_URL", "   ")
	t.Setenv("LAOZHANG_IMAGE_MODEL", "   ")

	cfg := Init()
	if cfg.AI.LaoZhangImageBaseURL != "https://api2.laozhang.ai" {
		t.Fatalf("LaoZhangImageBaseURL = %q, want safe default", cfg.AI.LaoZhangImageBaseURL)
	}
	if cfg.AI.LaoZhangImageModel != "gpt-image-2-vip" {
		t.Fatalf("LaoZhangImageModel = %q, want safe default", cfg.AI.LaoZhangImageModel)
	}
}
