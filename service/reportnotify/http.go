package reportnotify

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const gatewayTimeout = 10 * time.Second
const maxGatewayResponse = 64 << 10

type HTTPProvider struct {
	endpoint string
	token    string
	client   *http.Client
}

// Gateway contract: POST endpoint accepts Idempotency-Key and the notification;
// GET endpoint/{event_id} authoritatively queries that same idempotency identity.
// Only {"status":"sent","receipt":"nonempty"} confirms completed delivery.
func NewHTTPProvider(config Config, client *http.Client) (*HTTPProvider, error) {
	if err := config.Validate(); err != nil || !config.Configured() {
		return nil, ErrInvalidConfig
	}
	if client == nil {
		client = &http.Client{}
	}
	bounded := *client
	bounded.Timeout = gatewayTimeout
	bounded.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &HTTPProvider{endpoint: strings.TrimRight(config.Endpoint, "/"), token: config.Token, client: &bounded}, nil
}

func (p *HTTPProvider) Send(ctx context.Context, notification Notification) (Receipt, error) {
	if !validText(notification.ReportID, 64) || !validText(notification.Text, 600) ||
		notification.EventID != EventID(notification.ReportID) || !validText(notification.ExternalUserID, 128) ||
		notification.UserID <= 0 || notification.MiniProgramPath != "pages/report/delivery?id="+url.QueryEscape(notification.ReportID) {
		return Receipt{}, ErrInvalidInput
	}
	body, err := json.Marshal(notification)
	if err != nil {
		return Receipt{}, ErrInvalidInput
	}
	return p.request(ctx, http.MethodPost, p.endpoint, notification.EventID, body)
}

func (p *HTTPProvider) Lookup(ctx context.Context, eventID string) (Receipt, error) {
	if !validEventID(eventID) {
		return Receipt{}, ErrInvalidInput
	}
	return p.request(ctx, http.MethodGet, p.endpoint+"/"+eventID, eventID, nil)
}

func (p *HTTPProvider) request(ctx context.Context, method, endpoint, eventID string, body []byte) (Receipt, error) {
	if ctx == nil {
		return Receipt{}, ErrInvalidInput
	}
	requestCtx, cancel := context.WithTimeout(ctx, gatewayTimeout)
	defer cancel()
	request, err := http.NewRequestWithContext(requestCtx, method, endpoint, bytes.NewReader(body))
	if err != nil {
		return Receipt{}, ErrInvalidInput
	}
	request.Header.Set("Authorization", "Bearer "+p.token)
	request.Header.Set("Idempotency-Key", eventID)
	request.Header.Set("Content-Type", "application/json")
	response, err := p.client.Do(request)
	if err != nil {
		return Receipt{}, ErrUncertain
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusBadRequest || response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden || response.StatusCode == http.StatusUnprocessableEntity {
		return Receipt{}, ErrRejected
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return Receipt{}, ErrUncertain
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, maxGatewayResponse+1))
	if err != nil || len(data) > maxGatewayResponse {
		return Receipt{}, ErrUncertain
	}
	var receipt Receipt
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&receipt) != nil {
		return Receipt{}, ErrUncertain
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return Receipt{}, ErrUncertain
	}
	if receipt.Status == StateSent && validText(receipt.Receipt, 256) {
		return receipt, nil
	}
	return Receipt{}, ErrUncertain
}

func validEventID(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, char := range value {
		if !((char >= 'a' && char <= 'f') || (char >= '0' && char <= '9')) {
			return false
		}
	}
	return true
}
