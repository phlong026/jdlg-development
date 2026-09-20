package report

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"hash/crc32"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestHouseReviewerActuallyReceivesImageContent(t *testing.T) {
	var received map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
			t.Error(err)
		}
		_, _ = w.Write([]byte(`{"choices":[{"finish_reason":"stop","message":{"content":"{\"approved\":true,\"issues\":[]}"}}]}`))
	}))
	defer server.Close()
	client, err := NewHTTPAgentClient(MultiAgentHTTPConfig{Endpoint: server.URL, APIKey: "test-only", ConfigVersion: "test", PlannerModel: "m", WriterModel: "m", VisualModel: "m", ReviewerModel: "m", Timeout: time.Second, MaxResponseBytes: 4096}, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 8, 8))); err != nil {
		t.Fatal(err)
	}
	request := AgentRequest{Role: "reviewer", Model: "m", PromptVersion: "test", SystemPrompt: "审核实际图像", Input: json.RawMessage(`{"scheme":"A"}`), Images: []AgentImage{{MIMEType: "image/png", Data: buf.Bytes()}}}
	if _, err = client.Run(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	content := received["messages"].([]any)[1].(map[string]any)["content"]
	parts, ok := content.([]any)
	if !ok || len(parts) != 2 {
		t.Fatalf("reviewer did not receive multimodal content: %T", content)
	}
	url := parts[1].(map[string]any)["image_url"].(map[string]any)["url"].(string)
	if !strings.HasPrefix(url, "data:image/png;base64,") {
		t.Fatal("missing bounded local image data")
	}
	request.Images[0].MIMEType = "image/jpeg"
	if _, err = client.Run(context.Background(), request); err == nil {
		t.Fatal("mismatched image MIME accepted")
	}
}

// A provider may include a large ancillary PNG chunk without changing its 2K
// pixels. Preserve the original report asset but bound the vision payload.
func TestHouseLargeValidImageCanStillBeReviewed(t *testing.T) {
	var b bytes.Buffer
	if err := png.Encode(&b, image.NewRGBA(image.Rect(0, 0, 2048, 1152))); err != nil {
		t.Fatal(err)
	}
	original := b.Bytes()
	payload := append([]byte("Comment\x00"), bytes.Repeat([]byte("x"), 11<<20)...)
	var chunk bytes.Buffer
	if err := binary.Write(&chunk, binary.BigEndian, uint32(len(payload))); err != nil {
		t.Fatal(err)
	}
	chunk.WriteString("tEXt")
	chunk.Write(payload)
	sum := crc32.ChecksumIEEE(chunk.Bytes()[4:])
	if err := binary.Write(&chunk, binary.BigEndian, sum); err != nil {
		t.Fatal(err)
	}
	data := append(append(append([]byte(nil), original[:len(original)-12]...), chunk.Bytes()...), original[len(original)-12:]...)
	if err := validateHouseImageBytes(data); err != nil {
		t.Fatal(err)
	}
	before := imageDigest(data)
	req := AgentRequest{Role: "reviewer", Input: json.RawMessage(`{"asset":"test"}`), Images: []AgentImage{{MIMEType: "image/png", Data: data}}}
	content, err := agentUserContent(req)
	if err != nil {
		t.Fatalf("generated 2K asset is unreviewable: %v", err)
	}
	parts := content.([]map[string]any)
	url := parts[1]["image_url"].(map[string]string)["url"]
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(url, "data:image/png;base64,"))
	if err != nil || len(raw) > 10<<20 {
		t.Fatalf("vision payload too large or invalid: %d %v", len(raw), err)
	}
	if err := validateHouseImageBytes(raw); err != nil {
		t.Fatal(err)
	}
	if imageDigest(data) != before {
		t.Fatal("original 2K asset was modified")
	}
}
