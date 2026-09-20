package component

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"hash/crc32"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"service/config"
)

func TestReportIllustrationGenerateUses2KBodyAndReferenceDataURI(t *testing.T) {
	generated := testReportImage(t, "png", ReportIllustrationWidth, ReportIllustrationHeight)
	reference := testReportImage(t, "jpeg", 32, 18)
	var gotBody map[string]interface{}
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Fatalf("authorization = %q", r.Header.Get("Authorization"))
		}
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatal(err)
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"data": []interface{}{map[string]interface{}{"b64_json": base64.StdEncoding.EncodeToString(generated)}},
		})
	}))
	defer server.Close()

	client, err := NewReportIllustrationClient(&config.Config{AI: config.AIConfig{
		LaoZhangAPIKey:       "test-key",
		LaoZhangImageBaseURL: server.URL,
		LaoZhangImageModel:   "test-image-model",
	}}, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	result, err := client.Generate(context.Background(), ReportIllustrationRequest{
		Prompt:        "a private family brief",
		ReferenceData: reference,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(result.Data, generated) {
		t.Fatal("generated image bytes changed")
	}
	if result.Width != 2048 || result.Height != 1152 || result.MIMEType != "image/png" {
		t.Fatalf("metadata = %dx%d %q", result.Width, result.Height, result.MIMEType)
	}
	if result.Provider != ReportIllustrationProvider || result.Model != "test-image-model" {
		t.Fatalf("provider/model = %q/%q", result.Provider, result.Model)
	}
	if gotBody["size"] != "2048x1152" || gotBody["response_format"] != "b64_json" {
		t.Fatalf("request body = %#v", gotBody)
	}
	imageValue, _ := gotBody["image"].(string)
	if !strings.HasPrefix(imageValue, "data:image/jpeg;base64,") {
		t.Fatalf("reference was not sent as a JPEG data URI: %.40q", imageValue)
	}
	if gotBody["prompt"] != "a private family brief" {
		t.Fatalf("prompt = %#v", gotBody["prompt"])
	}
}

func TestReportIllustrationGeneratePrefersBase64OverURL(t *testing.T) {
	preferred := testReportImage(t, "jpeg", ReportIllustrationWidth, ReportIllustrationHeight)
	downloads := 0
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/result.png" {
			downloads++
			w.Write(testReportImage(t, "png", ReportIllustrationWidth, ReportIllustrationHeight))
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"data": []interface{}{map[string]interface{}{
				"b64_json": base64.StdEncoding.EncodeToString(preferred),
				"url":      serverURL(r) + "/result.png",
			}},
		})
	}))
	defer server.Close()
	client, err := NewReportIllustrationClient(&config.Config{AI: config.AIConfig{
		LaoZhangAPIKey: "test-key", LaoZhangImageBaseURL: server.URL, LaoZhangImageModel: "test-model",
	}}, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	result, err := client.Generate(context.Background(), ReportIllustrationRequest{Prompt: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if result.MIMEType != "image/jpeg" || downloads != 0 {
		t.Fatalf("mime/downloads = %q/%d", result.MIMEType, downloads)
	}
}

func TestReportIllustrationGenerateDownloadsHTTPSWithoutAuthorization(t *testing.T) {
	generated := testReportImage(t, "png", ReportIllustrationWidth, ReportIllustrationHeight)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/images/generations":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"data": []interface{}{map[string]interface{}{"url": serverURL(r) + "/asset.png"}},
			})
		case "/asset.png":
			if r.Header.Get("Authorization") != "" {
				t.Fatalf("download leaked Authorization header")
			}
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write(generated)
		}
	}))
	defer server.Close()
	client, err := NewReportIllustrationClient(&config.Config{AI: config.AIConfig{
		LaoZhangAPIKey: "secret", LaoZhangImageBaseURL: server.URL, LaoZhangImageModel: "test-model",
	}}, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Generate(context.Background(), ReportIllustrationRequest{Prompt: "test"}); err != nil {
		t.Fatal(err)
	}
}

func TestReportIllustrationDownloadRejectsRedirectWithUserInfo(t *testing.T) {
	finalDownloads := 0
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/images/generations":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"data": []interface{}{map[string]interface{}{"url": serverURL(r) + "/asset.png"}},
			})
		case "/asset.png":
			http.Redirect(w, r, "https://user:pass@"+r.Host+"/final.png", http.StatusFound)
		case "/final.png":
			finalDownloads++
		}
	}))
	defer server.Close()
	client, err := NewReportIllustrationClient(&config.Config{AI: config.AIConfig{
		LaoZhangAPIKey: "secret", LaoZhangImageBaseURL: server.URL, LaoZhangImageModel: "test-model",
	}}, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Generate(context.Background(), ReportIllustrationRequest{Prompt: "test"}); err == nil {
		t.Fatal("expected redirect with userinfo to be rejected")
	}
	if finalDownloads != 0 {
		t.Fatalf("final downloads = %d, want 0", finalDownloads)
	}
}

func TestReportIllustrationRejectsUnsafeOrInvalidResults(t *testing.T) {
	truncatedPNG := testReportImage(t, "png", ReportIllustrationWidth, ReportIllustrationHeight)
	truncatedPNG = truncatedPNG[:64]
	tests := []struct {
		name string
		body func(t *testing.T) interface{}
		want string
	}{
		{
			name: "low resolution",
			body: func(t *testing.T) interface{} {
				return map[string]interface{}{"data": []interface{}{map[string]interface{}{"b64_json": base64.StdEncoding.EncodeToString(testReportImage(t, "png", 1024, 576))}}}
			},
			want: "2048x1152",
		},
		{
			name: "not image",
			body: func(t *testing.T) interface{} {
				return map[string]interface{}{"data": []interface{}{map[string]interface{}{"b64_json": base64.StdEncoding.EncodeToString([]byte("not an image"))}}}
			},
			want: "PNG or JPEG",
		},
		{
			name: "truncated image",
			body: func(t *testing.T) interface{} {
				return map[string]interface{}{"data": []interface{}{map[string]interface{}{"b64_json": base64.StdEncoding.EncodeToString(truncatedPNG)}}}
			},
			want: "PNG or JPEG",
		},
		{
			name: "http download",
			body: func(t *testing.T) interface{} {
				return map[string]interface{}{"data": []interface{}{map[string]interface{}{"url": "http://example.com/image.png"}}}
			},
			want: "HTTPS",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_ = json.NewEncoder(w).Encode(tt.body(t))
			}))
			defer server.Close()
			client, err := NewReportIllustrationClient(&config.Config{AI: config.AIConfig{
				LaoZhangAPIKey: "test-key", LaoZhangImageBaseURL: server.URL, LaoZhangImageModel: "test-model",
			}}, server.Client())
			if err != nil {
				t.Fatal(err)
			}
			_, err = client.Generate(context.Background(), ReportIllustrationRequest{Prompt: "test"})
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestReportIllustrationNon2xxDoesNotExposeResponseOrKey(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte("upstream-secret-body"))
	}))
	defer server.Close()
	client, err := NewReportIllustrationClient(&config.Config{AI: config.AIConfig{
		LaoZhangAPIKey: "very-secret-key", LaoZhangImageBaseURL: server.URL, LaoZhangImageModel: "test-model",
	}}, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Generate(context.Background(), ReportIllustrationRequest{Prompt: "private-prompt"})
	if err == nil {
		t.Fatal("expected an error")
	}
	for _, secret := range []string{"upstream-secret-body", "very-secret-key", "private-prompt"} {
		if strings.Contains(err.Error(), secret) {
			t.Fatalf("error leaked %q: %v", secret, err)
		}
	}
}

func TestReportIllustrationClientRequiresConfigurationAndPrompt(t *testing.T) {
	if _, err := NewReportIllustrationClient(&config.Config{}, nil); err == nil {
		t.Fatal("expected missing configuration error")
	}
	client, err := NewReportIllustrationClient(&config.Config{AI: config.AIConfig{
		LaoZhangAPIKey: "key", LaoZhangImageBaseURL: "https://images.example.com", LaoZhangImageModel: "model",
	}}, &http.Client{Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Generate(context.Background(), ReportIllustrationRequest{}); err == nil {
		t.Fatal("expected empty prompt error")
	}
}

func TestReportIllustrationRejectsPaidPOSTRedirectWithoutResubmitting(t *testing.T) {
	submissions := 0
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		submissions++
		http.Redirect(w, r, "/second-submission", http.StatusTemporaryRedirect)
	}))
	defer server.Close()
	client, err := NewReportIllustrationClient(&config.Config{AI: config.AIConfig{
		LaoZhangAPIKey: "test-key", LaoZhangImageBaseURL: server.URL, LaoZhangImageModel: "test-model",
	}}, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Generate(context.Background(), ReportIllustrationRequest{Prompt: "test"}); err == nil {
		t.Fatal("expected redirect rejection")
	}
	if submissions != 1 {
		t.Fatalf("paid submissions = %d, want 1", submissions)
	}
}

func TestReportIllustrationClientRejectsUnsafeBaseURL(t *testing.T) {
	for _, baseURL := range []string{
		"http://images.example.com",
		"https://user:pass@images.example.com",
		"https://images.example.com?route=other",
		"https://images.example.com#fragment",
	} {
		_, err := NewReportIllustrationClient(&config.Config{AI: config.AIConfig{
			LaoZhangAPIKey: "key", LaoZhangImageBaseURL: baseURL, LaoZhangImageModel: "model",
		}}, nil)
		if err == nil {
			t.Fatalf("accepted unsafe base URL %q", baseURL)
		}
	}
}

func TestInspectReportIllustrationRejectsPixelBombBeforeDecode(t *testing.T) {
	_, _, _, err := inspectReportIllustration(pngHeader(4001, 4000), false)
	if err == nil || !strings.Contains(err.Error(), "pixel count") {
		t.Fatalf("error = %v", err)
	}
}

func testReportImage(t *testing.T, format string, width, height int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x % 255), G: uint8(y % 255), B: 99, A: 255})
		}
	}
	var buf bytes.Buffer
	var err error
	if format == "jpeg" {
		err = jpeg.Encode(&buf, img, &jpeg.Options{Quality: 80})
	} else {
		err = png.Encode(&buf, img)
	}
	if err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func serverURL(r *http.Request) string {
	return "https://" + r.Host
}

func pngHeader(width, height uint32) []byte {
	data := make([]byte, 33)
	copy(data[:8], []byte{137, 80, 78, 71, 13, 10, 26, 10})
	binary.BigEndian.PutUint32(data[8:12], 13)
	copy(data[12:16], "IHDR")
	binary.BigEndian.PutUint32(data[16:20], width)
	binary.BigEndian.PutUint32(data[20:24], height)
	data[24] = 8
	data[25] = 2
	binary.BigEndian.PutUint32(data[29:33], crc32.ChecksumIEEE(data[12:29]))
	return data
}
