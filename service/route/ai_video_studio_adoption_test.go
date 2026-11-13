package route

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"service/model"
	"service/tracking"
)

func TestStudioAdoptionCopiesVerifiedBytesToIndependentMediaID(t *testing.T) {
	objects := &testPrivateObjects{data: map[string][]byte{}, mime: map[string]string{}}
	svc := &tracking.Service{Config: tracking.Config{Tenant: "tenant-a"}, MediaObjects: objects}
	payload := []byte("stored-ai-video")
	sourceKey := "sourceobject00000000000000000000"
	objects.data["tenant-a:"+sourceKey] = payload
	objects.mime["tenant-a:"+sourceKey] = "video/mp4"
	digest := sha256.Sum256(payload)
	source := &model.AIVideoStudioMedia{
		ID: "studio-media-0001", UserID: 17, Kind: "video", MIME: "video/mp4", SizeBytes: int64(len(payload)),
		StorageKey: sourceKey, SHA256: hex.EncodeToString(digest[:]), Width: 1280, Height: 720,
	}
	destination := studioAdoptionMediaID("content", 17, "draft-instance-0001", source.ID)

	asset, err := copyStudioAdoptionMedia(context.Background(), svc, source, destination, "/api/v1/miniprogram/tracking/media/"+destination)
	if err != nil {
		t.Fatal(err)
	}
	if asset.ID == source.ID || asset.ID == source.StorageKey || asset.Kind != "video" || asset.URL == "" {
		t.Fatalf("source channel leaked into adopted asset: %+v", asset)
	}
	if got := string(objects.data["tenant-a:"+destination]); got != string(payload) {
		t.Fatalf("copied bytes mismatch: %q", got)
	}
	if again := studioAdoptionMediaID("content", 17, "draft-instance-0001", source.ID); again != destination {
		t.Fatalf("response retry changed destination: %q != %q", again, destination)
	}
}

func TestStudioAdoptionRejectsTamperedOrNonVideoSource(t *testing.T) {
	objects := &testPrivateObjects{data: map[string][]byte{}, mime: map[string]string{}}
	svc := &tracking.Service{Config: tracking.Config{Tenant: "tenant-a"}, MediaObjects: objects}
	payload := []byte("stored-ai-video")
	sourceKey := "sourceobject00000000000000000000"
	objects.data["tenant-a:"+sourceKey] = payload
	objects.mime["tenant-a:"+sourceKey] = "video/mp4"
	source := &model.AIVideoStudioMedia{ID: "studio-media-0001", Kind: "video", MIME: "video/mp4", SizeBytes: int64(len(payload)), StorageKey: sourceKey, SHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Width: 1280, Height: 720}
	if _, err := copyStudioAdoptionMedia(context.Background(), svc, source, "destination000000000000000000000", "/api/v1/miniprogram/tracking/media/destination000000000000000000000"); err == nil {
		t.Fatal("tampered source bytes accepted")
	}
	sum := sha256.Sum256(payload)
	source.SHA256 = hex.EncodeToString(sum[:])
	source.Kind = "image"
	if _, err := copyStudioAdoptionMedia(context.Background(), svc, source, "destination000000000000000000001", "/api/v1/miniprogram/tracking/media/destination000000000000000000001"); err == nil {
		t.Fatal("non-video source accepted")
	}
}
