package route

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"service/model"

	"github.com/gin-gonic/gin"
)

func TestCustomerServiceKnowledgeMatchesPublishedAnswers(t *testing.T) {
	tests := []struct {
		name          string
		input         string
		category      string
		replyContains string
		guideAction   string
	}{
		{name: "收费档位", input: "怎么收费", category: "product", replyContains: "50、300、500", guideAction: "generate"},
		{name: "单张价格", input: "多少钱一张", category: "product", replyContains: "一张图 2 元", guideAction: "generate"},
		{name: "AI方案用途", input: "AI出的图能用吗", category: "product", replyContains: "设计水平也比较高", guideAction: "generate"},
		{name: "风格区别", input: "新闽派和新中式", category: "style", replyContains: "现代元素组合融合", guideAction: "generate"},
		{name: "施工团队", input: "施工队推荐", category: "process", replyContains: "自有团队承建", guideAction: "human"},
		{name: "建房造价", input: "建三层大概多少钱", category: "cost", replyContains: "造价生成", guideAction: "cost"},
		{name: "装修预算", input: "装修预算", category: "cost", replyContains: "外装一般约 500-1000 元/㎡", guideAction: "cost"},
		{name: "房间数量", input: "几个房间", category: "function", replyContains: "5-8 个房间", guideAction: "collect"},
		{name: "地区管控", input: "福建建房", category: "regional", replyContains: "风貌管控", guideAction: "human"},
		{name: "结构选择", input: "砖混和框架哪个好", category: "technical", replyContains: "闽南地区做框架结构比较多", guideAction: "human"},
		{name: "软地基", input: "软地基", category: "technical", replyContains: "打桩", guideAction: "human"},
		{name: "混凝土", input: "混凝土是商混还是自拌", category: "technical", replyContains: "C30", guideAction: "human"},
		{name: "水电预埋", input: "水电预埋", category: "technical", replyContains: "不含穿线", guideAction: "human"},
		{name: "钢筋品牌", input: "钢筋品牌", category: "technical", replyContains: "三钢", guideAction: "human"},
		{name: "设计合同", input: "签合同吗", category: "contract", replyContains: "设计合同", guideAction: "human"},
		{name: "售后", input: "售后怎么处理", category: "contract", replyContains: "泉州有多家服务中心", guideAction: "human"},
		{name: "平台区别", input: "跟别的平台有什么区别", category: "competitor", replyContains: "AI 秒出图 + 专业设计师深化", guideAction: "generate"},
		{name: "大厅尺寸", input: "厅做多大", category: "custom", replyContains: "大厅小房", guideAction: "collect"},
		{name: "神龛", input: "神龛", category: "custom", replyContains: "风水位", guideAction: "human"},
		{name: "大门", input: "开大门", category: "custom", replyContains: "纳气", guideAction: "human"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			entry := matchKnowledgeBase(test.input)
			if entry == nil {
				t.Fatalf("matchKnowledgeBase(%q) returned nil", test.input)
			}
			if entry.category != test.category {
				t.Errorf("category = %q, want %q", entry.category, test.category)
			}
			if !strings.Contains(entry.reply, test.replyContains) {
				t.Errorf("reply = %q, want fragment %q", entry.reply, test.replyContains)
			}
			if entry.guideAction != test.guideAction {
				t.Errorf("guideAction = %q, want %q", entry.guideAction, test.guideAction)
			}
		})
	}
}

func TestCustomerServiceKnowledgeExcludesGenericBuildCost(t *testing.T) {
	const input = "建房多少钱"
	if entry := matchKnowledgeBase(input); entry != nil {
		t.Fatalf("matchKnowledgeBase(%q) = %#v, want nil", input, entry)
	}
	if !matchHighIntent(input) {
		t.Fatalf("matchHighIntent(%q) = false, want true", input)
	}
}

func TestCustomerServiceKnowledgeWinsOverHighIntentInStream(t *testing.T) {
	const input = "怎么收费"
	if !matchHighIntent(input) {
		t.Fatalf("test precondition failed: %q must also be high intent", input)
	}

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set("code_session", &model.CodeSession{UserID: 42})
		c.Next()
	})
	group := router.Group("/api/v1/miniprogram")
	RegisterAIIntentStreamRoute(group, nil, nil)

	body, err := json.Marshal(IntentStreamRequest{Text: input})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/miniprogram/ai/intent/stream", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", response.Code, http.StatusOK, response.Body.String())
	}
	if got := response.Header().Get("Content-Type"); !strings.HasPrefix(got, "text/event-stream") {
		t.Fatalf("Content-Type = %q, want text/event-stream", got)
	}

	events := decodeIntentSSE(t, response.Body.String())
	if len(events) != 3 {
		t.Fatalf("events = %#v, want intent, content, done", events)
	}
	if events[0].Type != "intent" || events[0].IntentType != "knowledge_hit" || events[0].GuideAction != "generate" {
		t.Errorf("intent event = %#v, want knowledge_hit/generate", events[0])
	}
	if events[1].Type != "content" || !strings.Contains(events[1].Content, "50、300、500") {
		t.Errorf("content event = %#v, want published pricing answer", events[1])
	}
	if events[2].Type != "done" {
		t.Errorf("last event = %#v, want done", events[2])
	}
	for _, event := range events {
		if event.IntentType == "handoff_human" {
			t.Fatalf("knowledge hit was incorrectly routed to high-intent handoff: %#v", events)
		}
	}
}

func decodeIntentSSE(t *testing.T, body string) []IntentSSEResponse {
	t.Helper()
	var events []IntentSSEResponse
	for _, line := range strings.Split(body, "\n") {
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		var event IntentSSEResponse
		if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &event); err != nil {
			t.Fatalf("decode SSE line %q: %v", line, err)
		}
		events = append(events, event)
	}
	return events
}
