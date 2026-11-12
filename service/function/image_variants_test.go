package function

import (
	"bytes"
	"context"
	"crypto/sha256"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"

	cos "github.com/tencentyun/cos-go-sdk-v5"

	"service/config"
)

func TestImageVariantsReuseSameContentWithinNamespace(t *testing.T) {
	imageData := testPNG(t)
	storage := newVariantStorageServer(t, imageData)

	first, err := GenerateRemoteImageVariants(context.Background(), storage.cfg, storage.client, storage.server.URL+"/source-a.png", "templates/42", []ImageVariantSpec{
		{Name: "list_thumb", MaxWidth: 480, Quality: 74},
	})
	if err != nil {
		t.Fatal(err)
	}
	second, err := GenerateRemoteImageVariants(context.Background(), storage.cfg, storage.client, storage.server.URL+"/source-b.png?token=changed", "templates/42", []ImageVariantSpec{
		{Name: "list_thumb", MaxWidth: 480, Quality: 74},
	})
	if err != nil {
		t.Fatal(err)
	}

	if first.URLs["list_thumb"] != second.URLs["list_thumb"] {
		t.Fatalf("same content produced different URLs: %q != %q", first.URLs["list_thumb"], second.URLs["list_thumb"])
	}
	if got := storage.putCount(); got != 1 {
		t.Fatalf("PUT count = %d, want 1", got)
	}
	if !regexp.MustCompile(`/derived_images/templates/42/[0-9a-f]{40}_list_thumb\.jpg$`).MatchString(first.URLs["list_thumb"]) {
		t.Fatalf("variant URL does not preserve public filename contract: %q", first.URLs["list_thumb"])
	}
}

func TestImageVariantsKeepSpecificationAndNamespaceBoundaries(t *testing.T) {
	imageData := testPNG(t)
	storage := newVariantStorageServer(t, imageData)

	first, err := GenerateImageVariantsFromBytes(context.Background(), storage.cfg, storage.client, "original-a", imageData, "templates/42", []ImageVariantSpec{
		{Name: "list_thumb", MaxWidth: 480, Quality: 74},
	})
	if err != nil {
		t.Fatal(err)
	}
	changedSpec, err := GenerateImageVariantsFromBytes(context.Background(), storage.cfg, storage.client, "original-b", imageData, "templates/42", []ImageVariantSpec{
		{Name: "list_thumb", MaxWidth: 320, Quality: 74},
	})
	if err != nil {
		t.Fatal(err)
	}
	changedQuality, err := GenerateImageVariantsFromBytes(context.Background(), storage.cfg, storage.client, "original-b", imageData, "templates/42", []ImageVariantSpec{
		{Name: "list_thumb", MaxWidth: 480, Quality: 82},
	})
	if err != nil {
		t.Fatal(err)
	}
	otherNamespace, err := GenerateImageVariantsFromBytes(context.Background(), storage.cfg, storage.client, "original-c", imageData, "templates/43", []ImageVariantSpec{
		{Name: "list_thumb", MaxWidth: 480, Quality: 74},
	})
	if err != nil {
		t.Fatal(err)
	}

	if first.URLs["list_thumb"] == changedSpec.URLs["list_thumb"] {
		t.Fatal("different width reused the same variant key")
	}
	if first.URLs["list_thumb"] == changedQuality.URLs["list_thumb"] {
		t.Fatal("different quality reused the same variant key")
	}
	if first.URLs["list_thumb"] == otherNamespace.URLs["list_thumb"] {
		t.Fatal("different namespace reused the same object URL")
	}
	if got := storage.putCount(); got != 4 {
		t.Fatalf("PUT count = %d, want 4", got)
	}
}

func TestImageVariantsUploadWhenHeadCannotValidateObject(t *testing.T) {
	imageData := testPNG(t)
	storage := newVariantStorageServer(t, imageData)
	storage.failHead = true

	_, err := GenerateImageVariantsFromBytes(context.Background(), storage.cfg, storage.client, "original", imageData, "templates/42", []ImageVariantSpec{
		{Name: "list_thumb", MaxWidth: 480, Quality: 74},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := storage.putCount(); got != 1 {
		t.Fatalf("PUT count = %d, want 1", got)
	}
}

func TestImageVariantsDoNotReuseUnmarkedLegacyObject(t *testing.T) {
	imageData := testPNG(t)
	storage := newVariantStorageServer(t, imageData)
	spec := ImageVariantSpec{Name: "list_thumb", MaxWidth: 480, Quality: 74}
	hash := buildVariantHash(sha256.Sum256(imageData), spec)
	legacyPath := "/tenant-root/derived_images/templates/42/" + hash + "_list_thumb.jpg"
	storage.objects[legacyPath] = []byte("legacy object with the same key")

	_, err := GenerateImageVariantsFromBytes(context.Background(), storage.cfg, storage.client, "original", imageData, "templates/42", []ImageVariantSpec{spec})
	if err != nil {
		t.Fatal(err)
	}
	if got := storage.putCount(); got != 1 {
		t.Fatalf("PUT count = %d, want 1 for unmarked legacy object", got)
	}
}

func TestImageVariantsRejectUnsafeNamespace(t *testing.T) {
	imageData := testPNG(t)
	storage := newVariantStorageServer(t, imageData)
	_, err := GenerateImageVariantsFromBytes(context.Background(), storage.cfg, storage.client, "original", imageData, "../shared", []ImageVariantSpec{
		{Name: "list_thumb", MaxWidth: 480, Quality: 74},
	})
	if err == nil || !strings.Contains(err.Error(), "namespace") {
		t.Fatalf("error = %v, want unsafe namespace error", err)
	}
}

func TestImageVariantsRejectUnsafeVariantName(t *testing.T) {
	imageData := testPNG(t)
	storage := newVariantStorageServer(t, imageData)
	_, err := GenerateImageVariantsFromBytes(context.Background(), storage.cfg, storage.client, "original", imageData, "templates/42", []ImageVariantSpec{
		{Name: "../list_thumb", MaxWidth: 480, Quality: 74},
	})
	if err == nil || !strings.Contains(err.Error(), "variant name") {
		t.Fatalf("error = %v, want unsafe variant name error", err)
	}
}

type variantStorageServer struct {
	t         *testing.T
	server    *httptest.Server
	client    *cos.Client
	cfg       *config.Config
	imageData []byte
	mu        sync.Mutex
	objects   map[string][]byte
	metadata  map[string]string
	puts      int
	failHead  bool
}

func newVariantStorageServer(t *testing.T, imageData []byte) *variantStorageServer {
	t.Helper()
	storage := &variantStorageServer{t: t, imageData: imageData, objects: make(map[string][]byte), metadata: make(map[string]string)}
	storage.server = httptest.NewServer(http.HandlerFunc(storage.serveHTTP))
	t.Cleanup(storage.server.Close)
	bucketURL, err := url.Parse(storage.server.URL)
	if err != nil {
		t.Fatal(err)
	}
	storage.client = cos.NewClient(&cos.BaseURL{BucketURL: bucketURL}, storage.server.Client())
	storage.client.Conf.EnableCRC = false
	storage.cfg = &config.Config{COS: config.COSConfig{Prefix: "tenant-root", Domain: "https://assets.example.test"}}
	return storage
}

func (s *variantStorageServer) serveHTTP(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/source-") {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(s.imageData)
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	switch r.Method {
	case http.MethodHead:
		if s.failHead {
			http.Error(w, "head unavailable", http.StatusInternalServerError)
			return
		}
		data, ok := s.objects[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "image/jpeg")
		w.Header().Set("Content-Length", strconv.Itoa(len(data)))
		w.Header().Set(variantKeyMetadataHeader, s.metadata[r.URL.Path])
		w.WriteHeader(http.StatusOK)
	case http.MethodPut:
		data, err := io.ReadAll(r.Body)
		if err != nil {
			s.t.Error(err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		s.objects[r.URL.Path] = data
		s.metadata[r.URL.Path] = r.Header.Get(variantKeyMetadataHeader)
		s.puts++
		w.WriteHeader(http.StatusOK)
	default:
		http.Error(w, "unexpected method", http.StatusMethodNotAllowed)
	}
}

func (s *variantStorageServer) putCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.puts
}

func testPNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 16, 12))
	for y := 0; y < 12; y++ {
		for x := 0; x < 16; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x * 10), G: uint8(y * 15), B: 120, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}
