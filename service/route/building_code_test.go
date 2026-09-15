package route

import (
	"strings"
	"testing"

	"service/component"
)

func TestNormalizeBuildingCodeFile(t *testing.T) {
	tests := []struct {
		name        string
		fileName    string
		contentType string
		wantType    string
		wantContent string
	}{
		{name: "pdf", fileName: "住宅设计规范.pdf", contentType: "application/octet-stream", wantType: "pdf", wantContent: "application/pdf"},
		{name: "docx", fileName: "消防.docx", contentType: "", wantType: "docx", wantContent: "application/vnd.openxmlformats-officedocument.wordprocessingml.document"},
		{name: "txt", fileName: "条文.txt", contentType: "text/plain", wantType: "txt", wantContent: "text/plain"},
		{name: "md", fileName: "审图要点.md", contentType: "", wantType: "md", wantContent: "text/markdown"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotType, gotContent, err := normalizeBuildingCodeFile(tt.fileName, tt.contentType)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if gotType != tt.wantType || gotContent != tt.wantContent {
				t.Fatalf("got (%s,%s), want (%s,%s)", gotType, gotContent, tt.wantType, tt.wantContent)
			}
		})
	}
}

func TestNormalizeBuildingCodeFileRejectsUnsupported(t *testing.T) {
	if _, _, err := normalizeBuildingCodeFile("规范.zip", "application/zip"); err == nil {
		t.Fatal("expected unsupported extension error")
	}
}

func TestBuildBuildingCodeObjectKey(t *testing.T) {
	key := buildBuildingCodeObjectKey(123, "住宅设计规范.pdf")
	if !strings.HasPrefix(key, "building_codes/123/") {
		t.Fatalf("unexpected prefix: %s", key)
	}
	if !strings.HasSuffix(key, ".pdf") {
		t.Fatalf("unexpected suffix: %s", key)
	}
}

func TestNormalizeFastGPTMessagesRoleMapping(t *testing.T) {
	req := buildingCodeChatRequest{
		Messages: []component.FastGPTChatMessage{{Role: "ai", Content: "我建议..."}},
	}
	msgs := normalizeFastGPTMessages(req)
	if len(msgs) != 1 {
		t.Fatalf("want 1 message, got %d", len(msgs))
	}
	if msgs[0].Role != "assistant" {
		t.Fatalf("ai should map to assistant, got %s", msgs[0].Role)
	}
}

func TestNormalizeFastGPTMessagesUnknownRoleBecomesUser(t *testing.T) {
	req := buildingCodeChatRequest{
		Messages: []component.FastGPTChatMessage{{Role: "custom-role", Content: "hello"}},
	}
	msgs := normalizeFastGPTMessages(req)
	if msgs[0].Role != "user" {
		t.Fatalf("unknown role should coerce to user, got %s", msgs[0].Role)
	}
}

func TestNormalizeFastGPTMessagesFallbackToQuestion(t *testing.T) {
	req := buildingCodeChatRequest{Question: "  楼梯宽度 ？  "}
	msgs := normalizeFastGPTMessages(req)
	if len(msgs) != 1 || msgs[0].Content != "楼梯宽度 ？" || msgs[0].Role != "user" {
		t.Fatalf("unexpected fallback result: %+v", msgs)
	}
}

func TestNormalizeFastGPTMessagesDropsEmpty(t *testing.T) {
	req := buildingCodeChatRequest{Messages: []component.FastGPTChatMessage{
		{Role: "user", Content: "有效"},
		{Role: "user", Content: "   "},
	}}
	msgs := normalizeFastGPTMessages(req)
	if len(msgs) != 1 || msgs[0].Content != "有效" {
		t.Fatalf("empty content should be dropped, got %+v", msgs)
	}
}

func TestNormalizeFastGPTMessagesTruncatesTo12(t *testing.T) {
	req := buildingCodeChatRequest{}
	for i := 0; i < 20; i++ {
		req.Messages = append(req.Messages, component.FastGPTChatMessage{Role: "user", Content: "m"})
	}
	msgs := normalizeFastGPTMessages(req)
	if len(msgs) != 12 {
		t.Fatalf("should truncate to 12, got %d", len(msgs))
	}
}
