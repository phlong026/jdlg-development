package route

import (
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestTrackingMediaSignatureSupportsExpiryAndTamperChecks(t *testing.T) {
	id := strings.Repeat("a", 32)
	expires := time.Now().Add(time.Minute).Unix()
	rawExpires := strconv.FormatInt(expires, 10)
	signature := trackingMediaSignature(id, expires)
	if !trackingMediaSignatureValid(id, rawExpires, signature) {
		t.Fatal("valid signature rejected")
	}
	if trackingMediaSignatureValid(id, rawExpires, signature+"0") {
		t.Fatal("tampered signature accepted")
	}
	if trackingMediaSignatureValid(strings.Repeat("b", 32), rawExpires, signature) {
		t.Fatal("tampered media id accepted")
	}
	expired := time.Now().Add(-time.Minute).Unix()
	if trackingMediaSignatureValid(id, strconv.FormatInt(expired, 10), trackingMediaSignature(id, expired)) {
		t.Fatal("expired signature accepted")
	}
	path := trackingMediaSignedPath(id, expires)
	if !strings.Contains(path, "/api/v1/miniprogram/tracking/media/"+id+"?exp=") || !strings.Contains(path, "&sig=") {
		t.Fatalf("signed path = %q", path)
	}
}
