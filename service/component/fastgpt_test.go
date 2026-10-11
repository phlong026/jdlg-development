package component

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"service/config"
)

func TestFastGPTImportLocalFile(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/core/dataset/collection/create/localFile" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Fatalf("unexpected auth header: %s", got)
		}
		if err := r.ParseMultipartForm(2 << 20); err != nil {
			t.Fatalf("parse multipart: %v", err)
		}
		if _, _, err := r.FormFile("file"); err != nil {
			t.Fatalf("file field missing: %v", err)
		}
		var payload map[string]interface{}
		if err := json.Unmarshal([]byte(r.FormValue("data")), &payload); err != nil {
			t.Fatalf("data json invalid: %v", err)
		}
		if payload["datasetId"] != "dataset-1" {
			t.Fatalf("unexpected datasetId: %#v", payload["datasetId"])
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"code":200,"data":{"id":"collection-1"}}`)
	}))
	defer server.Close()

	client := NewFastGPTClient(config.AIConfig{
		FastGPTBaseURL:   server.URL,
		FastGPTAPIKey:    "test-key",
		FastGPTDatasetID: "dataset-1",
	})
	result, err := client.ImportLocalFile(t.Context(), "a.pdf", []byte("pdf"), map[string]string{"scope": "public"})
	if err != nil {
		t.Fatalf("ImportLocalFile error: %v", err)
	}
	if result.CollectionID != "collection-1" {
		t.Fatalf("unexpected collection id: %s", result.CollectionID)
	}
}

func TestFastGPTChat(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/chat/completions" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Fatalf("unexpected auth header: %s", got)
		}
		body, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(body), `"stream":false`) {
			t.Fatalf("chat request must be non-streaming: %s", body)
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"choices":[{"message":{"content":"按规范应先核对建筑类别。"}}]}`)
	}))
	defer server.Close()

	client := NewFastGPTClient(config.AIConfig{
		FastGPTBaseURL:      server.URL,
		FastGPTAPIKey:       "test-key",
		FastGPTChatIDPrefix: "spec",
	})
	result, err := client.Chat(t.Context(), "u-1", []FastGPTChatMessage{{Role: "user", Content: "楼梯宽度？"}})
	if err != nil {
		t.Fatalf("Chat error: %v", err)
	}
	if result.Content != "按规范应先核对建筑类别。" {
		t.Fatalf("unexpected content: %s", result.Content)
	}
}

func TestParseFastGPTChatContent(t *testing.T) {
	// 完整消息
	full := []byte(`{"choices":[{"message":{"content":"按规范应先核对类别"}}]}`)
	if got := ParseFastGPTChatContent(full); got != "按规范应先核对类别" {
		t.Fatalf("full message: got %q", got)
	}
	// 流式增量
	delta := []byte(`{"choices":[{"delta":{"content":"按规范"}}]}`)
	if got := ParseFastGPTChatContent(delta); got != "按规范" {
		t.Fatalf("delta: got %q", got)
	}
	// 无 choices 返回空
	if got := ParseFastGPTChatContent([]byte(`{"foo":"bar"}`)); got != "" {
		t.Fatalf("no choices: got %q", got)
	}
	// 非法 JSON 返回空
	if got := ParseFastGPTChatContent([]byte(`not json`)); got != "" {
		t.Fatalf("invalid json: got %q", got)
	}
}

func TestParseFastGPTQuotes(t *testing.T) {
	// 形态 1：对象 + responseData
	one := []byte(`{"responseData":[{"quoteList":[{"sourceName":"住宅规范","q":"楼梯宽度"}]}]}`)
	if quotes := ParseFastGPTQuotes(one); len(quotes) != 1 || quotes[0]["sourceName"] != "住宅规范" {
		t.Fatalf("responseData form: got %+v", quotes)
	}
	// 形态 2：对象 + 直接 quoteList
	two := []byte(`{"quoteList":[{"sourceName":"防火规范"}]}`)
	if quotes := ParseFastGPTQuotes(two); len(quotes) != 1 || quotes[0]["sourceName"] != "防火规范" {
		t.Fatalf("direct quoteList: got %+v", quotes)
	}
	// 形态 3：数组顶层
	three := []byte(`[{"quoteList":[{"sourceName":"采光规范"}]}]`)
	if quotes := ParseFastGPTQuotes(three); len(quotes) != 1 || quotes[0]["sourceName"] != "采光规范" {
		t.Fatalf("array top: got %+v", quotes)
	}
	// 无引用
	if quotes := ParseFastGPTQuotes([]byte(`{"choices":[{"delta":{"content":"x"}}]}`)); quotes != nil {
		t.Fatalf("no quotes should return nil, got %+v", quotes)
	}
	// 非法 JSON
	if quotes := ParseFastGPTQuotes([]byte(`not json`)); quotes != nil {
		t.Fatalf("invalid json should return nil, got %+v", quotes)
	}
}

func TestFastGPTChatStreamParsesSSE(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/chat/completions" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		body, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(body), `"stream":true`) {
			t.Fatalf("expected stream=true, got: %s", body)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)
		lines := []string{
			`data: {"choices":[{"delta":{"content":"你好"}}]}`,
			``,
			`data: {"choices":[{"delta":{"content":"，世界"}}]}`,
			``,
			`data: {"quoteList":[{"sourceName":"规范"}]}`,
			``,
			`data: [DONE]`,
			``,
		}
		for _, line := range lines {
			io.WriteString(w, line+"\n")
			if flusher != nil {
				flusher.Flush()
			}
		}
	}))
	defer server.Close()

	client := NewFastGPTClient(config.AIConfig{
		FastGPTBaseURL: server.URL,
		FastGPTAPIKey:  "test-key",
	})

	var deltas []string
	var quotesSeen int
	err := client.ChatStream(t.Context(), "u-1", []FastGPTChatMessage{{Role: "user", Content: "hi"}}, func(raw []byte) error {
		if delta := ParseFastGPTChatContent(raw); delta != "" {
			deltas = append(deltas, delta)
		}
		if quotes := ParseFastGPTQuotes(raw); len(quotes) > 0 {
			quotesSeen += len(quotes)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("ChatStream error: %v", err)
	}
	if strings.Join(deltas, "") != "你好，世界" {
		t.Fatalf("deltas mismatch: %v", deltas)
	}
	if quotesSeen != 1 {
		t.Fatalf("expected 1 quote, got %d", quotesSeen)
	}
}

func TestFastGPTChatStreamRejectsNonOK(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		io.WriteString(w, `{"msg":"boom"}`)
	}))
	defer server.Close()

	client := NewFastGPTClient(config.AIConfig{FastGPTBaseURL: server.URL, FastGPTAPIKey: "k"})
	err := client.ChatStream(t.Context(), "u", []FastGPTChatMessage{{Role: "user", Content: "x"}}, func(raw []byte) error { return nil })
	if err == nil {
		t.Fatal("expected error on 500 response")
	}
}
