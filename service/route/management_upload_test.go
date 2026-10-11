package route

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestGenerateAdminUploadObjectKeyUsesPrefixAndExtension(t *testing.T) {
	key := generateAdminUploadObjectKey("IMG_2966.jpeg")

	if !strings.HasPrefix(key, "admin_uploads/") {
		t.Fatalf("expected admin upload prefix, got %q", key)
	}

	if ext := filepath.Ext(key); ext != ".jpeg" {
		t.Fatalf("expected extension .jpeg, got %q", ext)
	}
}

func TestGenerateAdminUploadObjectKeyIsUniqueForSameFilename(t *testing.T) {
	first := generateAdminUploadObjectKey("IMG_2966.jpeg")
	second := generateAdminUploadObjectKey("IMG_2966.jpeg")

	if first == second {
		t.Fatalf("expected unique object keys for same filename, got %q", first)
	}
}
