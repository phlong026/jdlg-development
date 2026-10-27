package officialaccount

import (
	"context"
	"errors"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"service/notification"
	"service/tracking"
)

type SendStatus string

const (
	SendAccepted SendStatus = "accepted"
	SendFailed   SendStatus = "failed"
	SendUnknown  SendStatus = "unknown"
)

type SendRequest struct {
	RecipientID    int64
	BindingVersion int64
	DetailQuery    string
	// Data keys come from the template verified in the service-account console.
	// This package deliberately does not invent field names before that check.
	Data map[string]string
}

type SendResult struct {
	Status            SendStatus `json:"status"`
	ProviderMessageID string     `json:"provider_msg_id,omitempty"`
	ErrorCode         string     `json:"error_code,omitempty"`
	Retryable         bool       `json:"retryable,omitempty"`
}

// DeliverySender is the boundary consumed by the notification worker adapter.
// BindingVersion must be captured before a delivery is claimed; a mismatch is
// permanent and prevents an old delivery from reaching a replacement binding.
type DeliverySender interface {
	Send(context.Context, SendRequest) SendResult
}

type Sender struct {
	config Config
	store  BindingStore
	api    *tracking.WechatAPI
}

func NewSender(config Config, store BindingStore, api *tracking.WechatAPI) *Sender {
	return &Sender{config: config, store: store, api: api}
}

func (s *Sender) Send(ctx context.Context, request SendRequest) SendResult {
	if s == nil || s.store == nil || s.api == nil || !s.config.Status().DeliveryAvailable {
		return SendResult{Status: SendFailed, ErrorCode: "not_configured"}
	}
	if request.RecipientID <= 0 || request.BindingVersion <= 0 || !validTemplateData(request.Data) {
		return SendResult{Status: SendFailed, ErrorCode: "invalid_request"}
	}
	pagePath, ok := safeMiniProgramPage(s.config.MiniProgramPagePath, request.DetailQuery)
	if !ok {
		return SendResult{Status: SendFailed, ErrorCode: "invalid_detail_target"}
	}
	binding, err := s.store.ResolveVersion(ctx, request.RecipientID, s.config.AppID, request.BindingVersion)
	if err != nil {
		code := "binding_unavailable"
		switch {
		case errors.Is(err, ErrBindingChanged):
			code = "binding_changed"
		case errors.Is(err, ErrNotBound):
			code = "not_bound"
		case errors.Is(err, ErrNotSubscribed):
			code = "not_subscribed"
		}
		return SendResult{Status: SendFailed, ErrorCode: code}
	}
	token, err := s.api.Token(ctx, s.config.AppID, s.config.AppSecret)
	if err != nil {
		return SendResult{Status: SendFailed, ErrorCode: "token_unavailable", Retryable: true}
	}
	data := make(map[string]map[string]string, len(request.Data))
	for key, value := range request.Data {
		data[key] = map[string]string{"value": value}
	}
	payload := map[string]any{
		"touser":      binding.OpenID,
		"template_id": s.config.TemplateID,
		"data":        data,
		"miniprogram": map[string]string{"appid": s.config.MiniProgramAppID, "pagepath": pagePath},
	}
	var response struct {
		MessageID int64 `json:"msgid"`
	}
	err = s.api.PostJSON(ctx, "/cgi-bin/message/template/send", token, payload, &response, 64<<10)
	if err == nil && response.MessageID > 0 {
		return SendResult{Status: SendAccepted, ProviderMessageID: strconv.FormatInt(response.MessageID, 10)}
	}
	var provider *tracking.WechatError
	if errors.As(err, &provider) {
		return SendResult{Status: SendFailed, ErrorCode: "provider_" + strconv.Itoa(provider.Code), Retryable: provider.Code == -1 || provider.Code == 45009}
	}
	// A timeout or broken response may have happened after WeChat accepted the
	// request. Mark it unknown so callers investigate instead of blindly resend.
	return SendResult{Status: SendUnknown, ErrorCode: "provider_unavailable"}
}

type DeliveryRequestMapper func(context.Context, notification.DeliveryJob) (SendRequest, error)

// DeliveryAdapter is the exact notification worker bridge. Recipient and
// binding version always come from the durable delivery row, never the mapper.
func DeliveryAdapter(sender DeliverySender, mapper DeliveryRequestMapper) notification.DeliverySend {
	return func(ctx context.Context, job notification.DeliveryJob) notification.DeliveryResult {
		if sender == nil || mapper == nil {
			return notification.DeliveryResult{Status: notification.DeliveryFailed, Reason: "sender_not_configured"}
		}
		request, err := mapper(ctx, job)
		if err != nil {
			return notification.DeliveryResult{Status: notification.DeliveryFailed, Reason: "template_mapping_failed"}
		}
		request.RecipientID = job.Delivery.RecipientID
		request.BindingVersion = job.Delivery.BindingVersion
		result := sender.Send(ctx, request)
		return notification.DeliveryResult{Status: string(result.Status), ProviderMessageID: result.ProviderMessageID, Reason: result.ErrorCode, Retryable: result.Retryable}
	}
}

var templateFieldPattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]{0,63}$`)

func validTemplateData(data map[string]string) bool {
	if len(data) == 0 || len(data) > 20 {
		return false
	}
	for key, value := range data {
		if !templateFieldPattern.MatchString(key) || strings.TrimSpace(value) == "" || len([]rune(value)) > 200 {
			return false
		}
	}
	return true
}

func safeMiniProgramPage(pagePath, rawQuery string) (string, bool) {
	if !validMiniProgramPagePath(pagePath) || rawQuery == "" || len(rawQuery) > 512 {
		return "", false
	}
	query, err := url.ParseQuery(rawQuery)
	if err != nil || len(query) != 1 {
		return "", false
	}
	for key, values := range query {
		if key != "notification_id" || len(values) != 1 || strings.TrimSpace(values[0]) == "" || len(values[0]) > 128 {
			return "", false
		}
	}
	return pagePath + "?" + query.Encode(), true
}
