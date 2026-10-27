package officialaccount

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"errors"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"service/component/virtualpay"
	"service/tracking"
)

type OAuthIdentity struct {
	OpenID     string
	UnionID    string
	Subscribed bool
}

type OAuthProvider interface {
	ExchangeCode(context.Context, string) (OAuthIdentity, error)
}

type HTTPProvider struct {
	config Config
	base   string
	client *http.Client
	api    *tracking.WechatAPI
}

func NewHTTPProvider(config Config) *HTTPProvider {
	return &HTTPProvider{
		config: config,
		base:   "https://api.weixin.qq.com",
		client: &http.Client{Timeout: 8 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }},
		api:    tracking.NewWechatAPI(),
	}
}

func (p *HTTPProvider) ExchangeCode(ctx context.Context, code string) (OAuthIdentity, error) {
	if p == nil || !p.config.Status().BindingAvailable || code == "" || len(code) > 256 {
		return OAuthIdentity{}, ErrNotConfigured
	}
	query := url.Values{"appid": {p.config.AppID}, "secret": {p.config.AppSecret}, "code": {code}, "grant_type": {"authorization_code"}}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.base+"/sns/oauth2/access_token?"+query.Encode(), nil)
	if err != nil {
		return OAuthIdentity{}, ErrUnavailable
	}
	response, err := p.client.Do(req)
	if err != nil {
		return OAuthIdentity{}, ErrUnavailable
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, (64<<10)+1))
	if err != nil || len(body) > 64<<10 || response.StatusCode != http.StatusOK {
		return OAuthIdentity{}, ErrUnavailable
	}
	var result struct {
		OpenID  string `json:"openid"`
		UnionID string `json:"unionid"`
		ErrCode int    `json:"errcode"`
	}
	if json.Unmarshal(body, &result) != nil || result.ErrCode != 0 || result.OpenID == "" {
		return OAuthIdentity{}, ErrUnavailable
	}
	subscribed, err := p.subscription(ctx, result.OpenID)
	if err != nil {
		return OAuthIdentity{}, err
	}
	return OAuthIdentity{OpenID: result.OpenID, UnionID: result.UnionID, Subscribed: subscribed}, nil
}

func (p *HTTPProvider) subscription(ctx context.Context, openID string) (bool, error) {
	token, err := p.api.Token(ctx, p.config.AppID, p.config.AppSecret)
	if err != nil {
		return false, ErrUnavailable
	}
	query := url.Values{"access_token": {token}, "openid": {openID}, "lang": {"zh_CN"}}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.base+"/cgi-bin/user/info?"+query.Encode(), nil)
	if err != nil {
		return false, ErrUnavailable
	}
	response, err := p.client.Do(req)
	if err != nil {
		return false, ErrUnavailable
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, (64<<10)+1))
	if err != nil || len(body) > 64<<10 || response.StatusCode != http.StatusOK {
		return false, ErrUnavailable
	}
	var result struct {
		Subscribe int `json:"subscribe"`
		ErrCode   int `json:"errcode"`
	}
	if json.Unmarshal(body, &result) != nil || result.ErrCode != 0 || (result.Subscribe != 0 && result.Subscribe != 1) {
		return false, ErrUnavailable
	}
	return result.Subscribe == 1, nil
}

type callbackEnvelope struct {
	Encrypt string `xml:"Encrypt"`
}

type callbackXML struct {
	FromUserName  string `xml:"FromUserName"`
	MsgType       string `xml:"MsgType"`
	Event         string `xml:"Event"`
	EventKey      string `xml:"EventKey"`
	MsgID         string `xml:"MsgId"`
	ProviderMsgID string `xml:"MsgID"`
	Status        string `xml:"Status"`
}

var ErrUnsupportedEvent = errors.New("officialaccount: unsupported event")

const EventTemplateSendFinished = "templatesendjobfinish"

type ReceiptStore interface {
	RecordProviderReceipt(context.Context, string, string, time.Time) (bool, error)
}

func SignCallback(token, timestamp, nonce, encrypted string) string {
	parts := []string{token, timestamp, nonce}
	if encrypted != "" {
		parts = append(parts, encrypted)
	}
	sort.Strings(parts)
	h := sha1.New()
	for _, part := range parts {
		_, _ = h.Write([]byte(part))
	}
	return hex.EncodeToString(h.Sum(nil))
}

func DecryptEcho(config Config, timestamp, nonce, signature, encrypted string) ([]byte, error) {
	if !virtualpay.VerifyPushSignature(config.CallbackToken, timestamp, nonce, encrypted, signature) {
		return nil, ErrInvalidInput
	}
	plain, err := virtualpay.DecryptPushMessage(config.EncodingAESKey, encrypted, config.AppID)
	if err != nil {
		return nil, ErrInvalidInput
	}
	return plain, nil
}

func ParseEncryptedCallback(config Config, timestamp, nonce, signature string, body []byte) (CallbackEvent, error) {
	if len(body) == 0 || len(body) > 128<<10 {
		return CallbackEvent{}, ErrInvalidInput
	}
	var envelope callbackEnvelope
	if xml.Unmarshal(body, &envelope) != nil || envelope.Encrypt == "" {
		return CallbackEvent{}, ErrInvalidInput
	}
	plain, err := DecryptEcho(config, timestamp, nonce, signature, envelope.Encrypt)
	if err != nil {
		return CallbackEvent{}, err
	}
	var message callbackXML
	if xml.Unmarshal(plain, &message) != nil || message.MsgType != "event" || message.FromUserName == "" {
		return CallbackEvent{}, ErrInvalidInput
	}
	eventType := strings.ToLower(message.Event)
	if eventType == EventTemplateSendFinished {
		providerID := message.ProviderMsgID
		if providerID == "" {
			providerID = message.MsgID
		}
		if providerID == "" || len(providerID) > 128 || message.Status == "" || len(message.Status) > 128 {
			return CallbackEvent{}, ErrInvalidInput
		}
		return CallbackEvent{DedupKey: providerID, AppID: config.AppID, OpenID: message.FromUserName, Type: eventType, ProviderMessageID: providerID, ProviderStatus: message.Status}, nil
	}
	if eventType != EventSubscribe && eventType != EventUnsubscribe {
		return CallbackEvent{}, ErrUnsupportedEvent
	}
	dedupKey := message.MsgID
	if dedupKey == "" {
		digest := sha1.Sum(plain)
		dedupKey = hex.EncodeToString(digest[:])
	}
	state := ""
	if eventType == EventSubscribe {
		state = strings.TrimPrefix(message.EventKey, "qrscene_")
		if state == message.EventKey {
			state = ""
		}
	}
	return CallbackEvent{DedupKey: dedupKey, AppID: config.AppID, OpenID: message.FromUserName, Type: eventType, BindingState: state}, nil
}
