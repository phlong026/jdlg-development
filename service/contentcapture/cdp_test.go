package contentcapture

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"golang.org/x/net/websocket"
)

func TestWebSocketCDPRoutesResponsesAndEvents(t *testing.T) {
	server := httptest.NewServer(websocket.Handler(func(connection *websocket.Conn) {
		var request map[string]any
		if err := websocket.JSON.Receive(connection, &request); err != nil {
			return
		}
		_ = websocket.JSON.Send(connection, map[string]any{"method": "Page.loadEventFired", "sessionId": "session-1", "params": map[string]any{"timestamp": 1}})
		_ = websocket.JSON.Send(connection, map[string]any{"id": request["id"], "result": map[string]any{"frameId": "frame-1"}})
	}))
	defer server.Close()

	connection, err := WebSocketDialer{}.Dial(context.Background(), "ws"+strings.TrimPrefix(server.URL, "http"))
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	var result struct {
		FrameID string `json:"frameId"`
	}
	if err := connection.Call(context.Background(), "session-1", "Page.navigate", map[string]any{"url": "https://mp.weixin.qq.com/s/a"}, &result); err != nil {
		t.Fatal(err)
	}
	if result.FrameID != "frame-1" {
		t.Fatalf("result=%#v", result)
	}
	event := <-connection.Events()
	if event.Method != "Page.loadEventFired" || event.SessionID != "session-1" || !json.Valid(event.Params) {
		t.Fatalf("event=%#v", event)
	}
}
