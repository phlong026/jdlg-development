package contentcapture

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
	"sync"

	"golang.org/x/net/websocket"
)

const maxCDPMessageBytes = 1 << 20

type Event struct {
	Method    string
	SessionID string
	Params    json.RawMessage
}

type CDPConnection interface {
	Call(context.Context, string, string, any, any) error
	Events() <-chan Event
	Close() error
}

type CDPDialer interface {
	Dial(context.Context, string) (CDPConnection, error)
}

type WebSocketDialer struct{}

func (WebSocketDialer) Dial(ctx context.Context, endpoint string) (CDPConnection, error) {
	configuration, err := websocket.NewConfig(endpoint, "http://localhost/")
	if err != nil {
		return nil, errors.New("invalid cdp websocket configuration")
	}
	connection, err := configuration.DialContext(ctx)
	if err != nil {
		return nil, errors.New("cdp websocket unavailable")
	}
	connection.MaxPayloadBytes = maxCDPMessageBytes
	client := &webSocketCDP{
		connection: connection,
		pending:    make(map[int64]chan cdpResponse),
		events:     make(chan Event, 1024),
		done:       make(chan struct{}),
	}
	go client.readLoop()
	return client, nil
}

type webSocketCDP struct {
	connection *websocket.Conn
	sendMu     sync.Mutex
	mu         sync.Mutex
	nextID     int64
	pending    map[int64]chan cdpResponse
	events     chan Event
	done       chan struct{}
	closeOnce  sync.Once
}

type cdpResponse struct {
	result json.RawMessage
	err    error
}

type cdpProtocolError struct {
	Code int `json:"code"`
}

func (client *webSocketCDP) Call(ctx context.Context, sessionID, method string, params, result any) error {
	if method == "" {
		return errors.New("empty cdp method")
	}
	client.mu.Lock()
	select {
	case <-client.done:
		client.mu.Unlock()
		return errors.New("cdp connection closed")
	default:
	}
	client.nextID++
	id := client.nextID
	responseChannel := make(chan cdpResponse, 1)
	client.pending[id] = responseChannel
	client.mu.Unlock()

	request := struct {
		ID        int64  `json:"id"`
		Method    string `json:"method"`
		SessionID string `json:"sessionId,omitempty"`
		Params    any    `json:"params,omitempty"`
	}{ID: id, Method: method, SessionID: sessionID, Params: params}
	client.sendMu.Lock()
	err := websocket.JSON.Send(client.connection, request)
	client.sendMu.Unlock()
	if err != nil {
		client.removePending(id)
		return errors.New("cdp send failed")
	}

	select {
	case response := <-responseChannel:
		if response.err != nil {
			return response.err
		}
		if result != nil && len(response.result) > 0 {
			if err := json.Unmarshal(response.result, result); err != nil {
				return errors.New("invalid cdp response")
			}
		}
		return nil
	case <-ctx.Done():
		client.removePending(id)
		return ctx.Err()
	case <-client.done:
		client.removePending(id)
		return errors.New("cdp connection closed")
	}
}

func (client *webSocketCDP) Events() <-chan Event { return client.events }

func (client *webSocketCDP) Close() error {
	client.shutdown(errors.New("cdp connection closed"))
	return nil
}

func (client *webSocketCDP) readLoop() {
	defer close(client.events)
	for {
		var message struct {
			ID        int64             `json:"id"`
			Method    string            `json:"method"`
			SessionID string            `json:"sessionId"`
			Params    json.RawMessage   `json:"params"`
			Result    json.RawMessage   `json:"result"`
			Error     *cdpProtocolError `json:"error"`
		}
		if err := websocket.JSON.Receive(client.connection, &message); err != nil {
			client.shutdown(errors.New("cdp receive failed"))
			return
		}
		if message.ID != 0 {
			response := cdpResponse{result: message.Result}
			if message.Error != nil {
				response.err = fmt.Errorf("cdp command failed (%d)", message.Error.Code)
			}
			client.deliver(message.ID, response)
			continue
		}
		if message.Method == "" {
			client.shutdown(errors.New("invalid cdp event"))
			return
		}
		select {
		case client.events <- Event{Method: message.Method, SessionID: message.SessionID, Params: message.Params}:
		default:
			client.shutdown(errors.New("cdp event limit exceeded"))
			return
		}
	}
}

func (client *webSocketCDP) deliver(id int64, response cdpResponse) {
	client.mu.Lock()
	channel := client.pending[id]
	delete(client.pending, id)
	client.mu.Unlock()
	if channel != nil {
		channel <- response
	}
}

func (client *webSocketCDP) removePending(id int64) {
	client.mu.Lock()
	delete(client.pending, id)
	client.mu.Unlock()
}

func (client *webSocketCDP) shutdown(reason error) {
	client.closeOnce.Do(func() {
		_ = client.connection.Close()
		client.mu.Lock()
		pending := client.pending
		client.pending = make(map[int64]chan cdpResponse)
		client.mu.Unlock()
		for _, channel := range pending {
			channel <- cdpResponse{err: reason}
		}
		close(client.done)
	})
}

func validCDPURL(raw string) bool {
	parsed, err := url.Parse(raw)
	if err != nil || (parsed.Scheme != "ws" && parsed.Scheme != "wss") || parsed.User != nil || parsed.Fragment != "" || parsed.RawQuery != "" || parsed.Hostname() == "" || !strings.HasPrefix(parsed.Path, "/devtools/browser/") || len(strings.TrimPrefix(parsed.Path, "/devtools/browser/")) < 1 {
		return false
	}
	host := parsed.Hostname()
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
