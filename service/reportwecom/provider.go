package reportwecom

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

var (
	ErrNotConfigured   = errors.New("reportwecom: not configured")
	ErrUnionIDRequired = errors.New("reportwecom: unionid required")
	ErrNotOwner        = errors.New("reportwecom: identity mismatch")
	ErrProvider        = errors.New("reportwecom: provider unavailable")
	ErrExpired         = errors.New("reportwecom: ticket expired")
)

type Config struct {
	Enabled                                                         bool
	CorpID, Secret, EmployeeID, Token, AESKey, AppID, ThumbnailPath string
}

func LoadConfig() (Config, error) {
	c := Config{Enabled: os.Getenv("REPORT_WECOM_ENABLED") == "true", CorpID: os.Getenv("REPORT_WECOM_CORP_ID"), Secret: os.Getenv("REPORT_WECOM_SECRET"), EmployeeID: os.Getenv("REPORT_WECOM_EMPLOYEE_ID"), Token: os.Getenv("REPORT_WECOM_TOKEN"), AESKey: os.Getenv("REPORT_WECOM_AES_KEY"), AppID: os.Getenv("REPORT_WECOM_APP_ID"), ThumbnailPath: os.Getenv("REPORT_WECOM_THUMBNAIL")}
	if !c.Enabled {
		return c, nil
	}
	if c.CorpID == "" || c.Secret == "" || c.EmployeeID == "" || c.Token == "" || len(c.AESKey) != 43 || c.AppID == "" || !filepath.IsAbs(c.ThumbnailPath) {
		return Config{}, ErrNotConfigured
	}
	info, err := os.Stat(c.ThumbnailPath)
	if err != nil || !info.Mode().IsRegular() || info.Size() > 1<<20 || (filepath.Ext(c.ThumbnailPath) != ".png" && filepath.Ext(c.ThumbnailPath) != ".jpg") {
		return Config{}, ErrNotConfigured
	}
	return c, nil
}

type ContactWay struct {
	ConfigID  string `json:"-"`
	QRCodeURL string `json:"qr_url,omitempty"`
	Bound     bool   `json:"bound"`
}
type ExternalContact struct {
	UnionID, ExternalUserID string
	FollowUsers             []string
}
type Provider interface {
	CreateContactWay(context.Context, string) (ContactWay, error)
	GetExternalContact(context.Context, string) (ExternalContact, error)
	SendWelcome(context.Context, string, string) error
}
type HTTPProvider struct {
	config                 Config
	client                 *http.Client
	mu                     sync.Mutex
	token, mediaID         string
	tokenUntil, mediaUntil time.Time
}

func NewHTTPProvider(c Config) *HTTPProvider {
	return &HTTPProvider{config: c, client: &http.Client{Timeout: 8 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}
func (p *HTTPProvider) call(ctx context.Context, method, path string, query url.Values, body io.Reader, contentType string) (map[string]json.RawMessage, error) {
	if p == nil || !p.config.Enabled {
		return nil, ErrNotConfigured
	}
	callCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(callCtx, method, "https://qyapi.weixin.qq.com/cgi-bin/"+path+"?"+query.Encode(), body)
	if err != nil {
		return nil, ErrProvider
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	res, err := p.client.Do(req)
	if err != nil {
		return nil, ErrProvider
	}
	defer res.Body.Close()
	b, err := io.ReadAll(io.LimitReader(res.Body, (1<<20)+1))
	if err != nil || len(b) > 1<<20 || res.StatusCode != 200 {
		return nil, ErrProvider
	}
	var data map[string]json.RawMessage
	if json.Unmarshal(b, &data) != nil {
		return nil, ErrProvider
	}
	var code int
	if raw, ok := data["errcode"]; ok {
		if json.Unmarshal(raw, &code) != nil || code != 0 {
			return nil, ErrProvider
		}
	}
	return data, nil
}
func field(data map[string]json.RawMessage, key string) string {
	var v string
	_ = json.Unmarshal(data[key], &v)
	return v
}
func (p *HTTPProvider) access(ctx context.Context) (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if time.Now().Before(p.tokenUntil) {
		return p.token, nil
	}
	data, err := p.call(ctx, http.MethodGet, "gettoken", url.Values{"corpid": {p.config.CorpID}, "corpsecret": {p.config.Secret}}, nil, "")
	if err != nil {
		return "", err
	}
	token := field(data, "access_token")
	var expires int
	_ = json.Unmarshal(data["expires_in"], &expires)
	if token == "" || expires < 120 {
		return "", ErrProvider
	}
	p.token = token
	p.tokenUntil = time.Now().Add(time.Duration(expires-60) * time.Second)
	return token, nil
}
func (p *HTTPProvider) jsonCall(ctx context.Context, path string, body any) (map[string]json.RawMessage, error) {
	token, err := p.access(ctx)
	if err != nil {
		return nil, err
	}
	b, err := json.Marshal(body)
	if err != nil {
		return nil, ErrProvider
	}
	return p.call(ctx, http.MethodPost, path, url.Values{"access_token": {token}}, bytes.NewReader(b), "application/json")
}
func (p *HTTPProvider) CreateContactWay(ctx context.Context, state string) (ContactWay, error) {
	if len(state) > 30 || state == "" {
		return ContactWay{}, ErrProvider
	}
	data, err := p.jsonCall(ctx, "externalcontact/add_contact_way", map[string]any{"type": 1, "scene": 2, "skip_verify": true, "state": state, "user": []string{p.config.EmployeeID}})
	if err != nil {
		return ContactWay{}, err
	}
	way := ContactWay{ConfigID: field(data, "config_id"), QRCodeURL: field(data, "qr_code")}
	parsed, err := url.Parse(way.QRCodeURL)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || way.ConfigID == "" {
		return ContactWay{}, ErrProvider
	}
	return way, nil
}
func (p *HTTPProvider) GetExternalContact(ctx context.Context, externalID string) (ExternalContact, error) {
	token, err := p.access(ctx)
	if err != nil {
		return ExternalContact{}, err
	}
	data, err := p.call(ctx, http.MethodGet, "externalcontact/get", url.Values{"access_token": {token}, "external_userid": {externalID}}, nil, "")
	if err != nil {
		return ExternalContact{}, err
	}
	var contact struct {
		UnionID    string `json:"unionid"`
		ExternalID string `json:"external_userid"`
	}
	if json.Unmarshal(data["external_contact"], &contact) != nil || contact.ExternalID != externalID {
		return ExternalContact{}, ErrProvider
	}
	var follows []struct {
		UserID string `json:"userid"`
	}
	if json.Unmarshal(data["follow_user"], &follows) != nil {
		return ExternalContact{}, ErrProvider
	}
	result := ExternalContact{UnionID: contact.UnionID, ExternalUserID: contact.ExternalID}
	for _, f := range follows {
		result.FollowUsers = append(result.FollowUsers, f.UserID)
	}
	return result, nil
}
func (p *HTTPProvider) thumbnail(ctx context.Context) (string, error) {
	token, err := p.access(ctx)
	if err != nil {
		return "", err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if time.Now().Before(p.mediaUntil) {
		return p.mediaID, nil
	}
	file, err := os.Open(p.config.ThumbnailPath)
	if err != nil {
		return "", ErrProvider
	}
	defer file.Close()
	b, err := io.ReadAll(io.LimitReader(file, (1<<20)+1))
	if err != nil || len(b) > 1<<20 {
		return "", ErrProvider
	}
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("media", filepath.Base(p.config.ThumbnailPath))
	if err != nil {
		return "", ErrProvider
	}
	if _, err = part.Write(b); err != nil {
		return "", ErrProvider
	}
	if writer.Close() != nil {
		return "", ErrProvider
	}
	data, err := p.call(ctx, http.MethodPost, "media/upload", url.Values{"access_token": {token}, "type": {"image"}}, &body, writer.FormDataContentType())
	if err != nil {
		return "", err
	}
	p.mediaID = field(data, "media_id")
	if p.mediaID == "" {
		return "", ErrProvider
	}
	p.mediaUntil = time.Now().Add(48 * time.Hour)
	return p.mediaID, nil
}
func (p *HTTPProvider) Warmup(ctx context.Context) error { _, err := p.thumbnail(ctx); return err }
func (p *HTTPProvider) SendWelcome(ctx context.Context, welcomeCode, reportID string) error {
	if welcomeCode == "" || strings.ContainsAny(reportID, "/?&") {
		return ErrProvider
	}
	mediaID, err := p.thumbnail(ctx)
	if err != nil {
		return err
	}
	data, err := p.jsonCall(ctx, "externalcontact/send_welcome_msg", map[string]any{
		"welcome_code": welcomeCode, "text": map[string]string{"content": "您的建房报告正在准备。请使用付款账号打开下方入口，完成后即可领取 PDF。"},
		"attachments": []any{map[string]any{"msgtype": "miniprogram", "miniprogram": map[string]string{"title": "领取我的建房报告", "pic_media_id": mediaID, "appid": p.config.AppID, "page": "pages/report/delivery?id=" + reportID}}},
	})
	if err != nil {
		return err
	}
	var receipt *int
	if json.Unmarshal(data["errcode"], &receipt) != nil || receipt == nil || *receipt != 0 {
		return ErrProvider
	}
	return nil
}
