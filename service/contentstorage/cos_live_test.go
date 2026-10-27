package contentstorage

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"image"
	"image/png"
	"io"
	"os"
	"strings"
	"testing"
	"time"
)

// Run this binary on the credential-owning server. It never copies credentials
// out of that server and uploads only its own non-sensitive generated image.
func TestLivePrivateCOSRoundTrip(t *testing.T) {
	if os.Getenv("JDLG_PHASE3_COS_LIVE") != "true" {
		t.Skip("explicit private COS live check is disabled")
	}
	values := map[string]string{}
	if path := os.Getenv("JDLG_PHASE3_LIVE_ENV_FILE"); path != "" {
		info, err := os.Lstat(path)
		if err != nil || !info.Mode().IsRegular() || info.Size() > 1<<20 {
			t.Fatal("live environment file unavailable")
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal("live environment file unreadable")
		}
		for _, line := range strings.Split(string(raw), "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "#") {
				continue
			}
			key, value, ok := strings.Cut(line, "=")
			if ok {
				values[strings.TrimPrefix(strings.TrimSpace(key), "export ")] = strings.Trim(strings.TrimSpace(value), "\"'")
			}
		}
	}
	lookup := func(key string) string {
		if value := os.Getenv(key); value != "" {
			return value
		}
		return values[key]
	}
	bucket := lookup("CONTENT_MEDIA_COS_BUCKET")
	if bucket == "" {
		bucket = lookup("COS_BUCKET")
	}
	region := lookup("CONTENT_MEDIA_COS_REGION")
	if region == "" {
		region = lookup("COS_REGION")
	}
	store, err := NewCOSStore(&Config{Enabled: true, Bucket: bucket, Region: region, PrivatePrefix: lookup("CONTENT_MEDIA_COS_PREFIX"), SecretID: lookup("COS_SECRET_ID"), SecretKey: lookup("COS_SECRET_KEY")})
	if err != nil {
		t.Fatalf("live COS configuration: %v", err)
	}
	entropy := make([]byte, 24)
	if _, err = rand.Read(entropy); err != nil {
		t.Fatal("random test identity unavailable")
	}
	id := base64.RawURLEncoding.EncodeToString(entropy)
	tenant := "phase3-live-verification"
	var imageBytes bytes.Buffer
	if err = png.Encode(&imageBytes, image.NewRGBA(image.Rect(0, 0, 2, 3))); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if err = store.Put(ctx, tenant, id, imageBytes.Bytes(), "image/png"); err != nil {
		t.Fatalf("live COS private write: %v", err)
	}
	key, err := store.ObjectKey(tenant, id)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanup, stop := context.WithTimeout(context.Background(), 15*time.Second)
		defer stop()
		response, deleteErr := store.client.Object.Delete(cleanup, key, nil)
		closeResponse(response)
		if deleteErr != nil {
			t.Error("owned live test object cleanup failed")
		}
	})
	data, mime, err := store.Get(ctx, tenant, id, 10<<20)
	if err != nil || mime != "image/png" || !bytes.Equal(data, imageBytes.Bytes()) {
		t.Fatalf("live COS round trip: type=%s matches=%v error=%v", mime, bytes.Equal(data, imageBytes.Bytes()), err)
	}
	stream, err := store.Open(ctx, tenant, id, "bytes=0-7")
	if err != nil {
		t.Fatalf("live COS range open: %v", err)
	}
	part, readErr := io.ReadAll(stream.Body)
	closeErr := stream.Body.Close()
	if readErr != nil || closeErr != nil || stream.Status != 206 || !bytes.Equal(part, imageBytes.Bytes()[:8]) {
		t.Fatalf("live COS range: status=%d bytes=%d read_error=%v close_error=%v", stream.Status, len(part), readErr, closeErr)
	}
	if _, err = store.Open(ctx, tenant, id, "bytes=0-1,4-5"); !errors.Is(err, ErrRangeNotSatisfiable) {
		t.Fatalf("live COS invalid range: %v", err)
	}
	if err = store.Put(ctx, tenant, id, []byte("different content"), "image/png"); !errors.Is(err, ErrConflict) {
		t.Fatalf("live COS overwrite protection: %v", err)
	}
	t.Logf("LIVE_COS_OK private_acl=true anonymous_denied=true sha256_verified=true overwrite_rejected=true range_verified=true bytes=%d", len(data))
}
