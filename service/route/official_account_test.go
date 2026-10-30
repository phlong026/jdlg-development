package route

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"encoding/xml"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"service/officialaccount"
)

type officialRouteStore struct {
	intent        officialaccount.BindingIntent
	createdFor    int64
	bound         officialaccount.Binding
	lastEvent     officialaccount.CallbackEvent
	receiptID     string
	receiptStatus string
	eventRepeats  map[string]bool
}

func (s *officialRouteStore) RecordProviderReceipt(_ context.Context, providerID, status string, _ time.Time) (bool, error) {
	s.receiptID, s.receiptStatus = providerID, status
	return true, nil
}

func (s *officialRouteStore) CreateIntent(_ context.Context, userID int64, now time.Time, ttl time.Duration) (officialaccount.BindingIntent, error) {
	s.createdFor = userID
	s.intent = officialaccount.BindingIntent{State: "server-owned-state", UserID: userID, ExpiresAt: now.Add(ttl)}
	return s.intent, nil
}
func (s *officialRouteStore) ConsumeAndBind(_ context.Context, state, appID, openID, unionID string, subscribed bool, now time.Time) (officialaccount.Binding, error) {
	if state != s.intent.State || now.After(s.intent.ExpiresAt) {
		return officialaccount.Binding{}, officialaccount.ErrInvalidState
	}
	if s.intent.ConsumedAt != nil {
		return officialaccount.Binding{}, officialaccount.ErrConsumedState
	}
	s.intent.ConsumedAt = &now
	s.bound = officialaccount.Binding{AppID: appID, OpenID: openID, UnionID: unionID, UserID: s.intent.UserID, Version: 1, Active: true, Subscribed: subscribed, UpdatedAt: now}
	return s.bound, nil
}
func (s *officialRouteStore) ResolveBinding(_ context.Context, userID int64, appID string) (officialaccount.Binding, error) {
	if s.bound.UserID != userID || s.bound.AppID != appID || !s.bound.Active {
		return officialaccount.Binding{}, officialaccount.ErrNotBound
	}
	if !s.bound.Subscribed {
		return officialaccount.Binding{}, officialaccount.ErrNotSubscribed
	}
	return s.bound, nil
}
func (s *officialRouteStore) Unbind(_ context.Context, userID int64, appID string, now time.Time) error {
	if s.bound.UserID != userID || s.bound.AppID != appID || !s.bound.Active {
		return officialaccount.ErrNotBound
	}
	s.bound.Active = false
	s.bound.Subscribed = false
	s.bound.Version++
	s.bound.UpdatedAt = now
	return nil
}
func (s *officialRouteStore) ResolveVersion(ctx context.Context, userID int64, appID string, version int64) (officialaccount.Binding, error) {
	b, err := s.ResolveBinding(ctx, userID, appID)
	if err == nil && b.Version != version {
		return officialaccount.Binding{}, officialaccount.ErrBindingChanged
	}
	return b, err
}
func (s *officialRouteStore) ApplyEvent(_ context.Context, event officialaccount.CallbackEvent, now time.Time) (bool, error) {
	if s.eventRepeats == nil {
		s.eventRepeats = map[string]bool{}
	}
	if s.eventRepeats[event.DedupKey] {
		return true, nil
	}
	s.eventRepeats[event.DedupKey] = true
	s.lastEvent = event
	if event.Type == officialaccount.EventUnsubscribe && s.bound.OpenID == event.OpenID {
		s.bound.Subscribed = false
		s.bound.Version++
		s.bound.UpdatedAt = now
	}
	return false, nil
}

type officialOAuthFake struct{ identity officialaccount.OAuthIdentity }

func (p officialOAuthFake) ExchangeCode(_ context.Context, code string) (officialaccount.OAuthIdentity, error) {
	if code != "verified-provider-code" {
		return officialaccount.OAuthIdentity{}, errors.New("bad code")
	}
	return p.identity, nil
}

func officialRouteFixture(t *testing.T, config officialaccount.Config, store *officialRouteStore) (*gin.Engine, time.Time) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	now := time.Date(2026, 10, 27, 10, 30, 0, 0, time.UTC)
	router := gin.New()
	public := router.Group("/api/v1")
	authed := router.Group("/api/v1/miniprogram/official-account", func(c *gin.Context) {
		c.Set("user_id", int64(42))
		c.Next()
	})
	registerOfficialAccountHandlers(public, authed, OfficialAccountDeps{Config: config, Store: store, Provider: officialOAuthFake{identity: officialaccount.OAuthIdentity{OpenID: "verified-official-openid"}}, Receipts: store, Now: func() time.Time { return now }})
	return router, now
}

func enabledOfficialConfig(t *testing.T) officialaccount.Config {
	t.Helper()
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 1)
	}
	return officialaccount.Config{Enabled: true, AppID: "wx-official", AppSecret: "secret", CallbackToken: "callback-token", EncodingAESKey: base64.StdEncoding.EncodeToString(key)[:43], TemplateID: "template", PublicOrigin: "https://www.jiadilingguang.com"}
}

func TestOfficialBindingUsesAuthenticatedMiniUserAndVerifiedOAuthIdentity(t *testing.T) {
	store := &officialRouteStore{}
	router, _ := officialRouteFixture(t, enabledOfficialConfig(t), store)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/miniprogram/official-account/binding?user_id=99&openid=client-forged", nil)
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK || store.createdFor != 42 || strings.Contains(w.Body.String(), "client-forged") {
		t.Fatalf("status=%d user=%d body=%s", w.Code, store.createdFor, w.Body.String())
	}
	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/v1/wechat/official/oauth/callback?code=verified-provider-code&state=server-owned-state&user_id=99&openid=client-forged", nil)
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK || store.bound.UserID != 42 || store.bound.OpenID != "verified-official-openid" || store.bound.Subscribed {
		t.Fatalf("status=%d binding=%+v body=%s", w.Code, store.bound, w.Body.String())
	}
}

func TestOfficialUnbindUsesAuthenticatedMiniUser(t *testing.T) {
	config := enabledOfficialConfig(t)
	store := &officialRouteStore{bound: officialaccount.Binding{AppID: config.AppID, OpenID: "official-openid", UserID: 42, Version: 3, Active: true, Subscribed: true}}
	router, _ := officialRouteFixture(t, config, store)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/miniprogram/official-account/binding?user_id=99", nil)
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK || store.bound.Active || store.bound.UserID != 42 || store.bound.Version != 4 {
		t.Fatalf("status=%d binding=%+v body=%s", w.Code, store.bound, w.Body.String())
	}
}

func TestOfficialCapabilityDefaultsOff(t *testing.T) {
	store := &officialRouteStore{}
	router, _ := officialRouteFixture(t, officialaccount.Config{}, store)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/miniprogram/official-account/binding", nil))
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"enabled":false`) || store.createdFor != 0 {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
}

func encryptOfficialRouteEvent(t *testing.T, config officialaccount.Config, plain []byte) string {
	t.Helper()
	key, _ := base64.StdEncoding.DecodeString(config.EncodingAESKey + "=")
	random := make([]byte, 16)
	if _, err := rand.Read(random); err != nil {
		t.Fatal(err)
	}
	length := make([]byte, 4)
	binary.BigEndian.PutUint32(length, uint32(len(plain)))
	buffer := append(append(append(random, length...), plain...), config.AppID...)
	pad := 32 - len(buffer)%32
	for i := 0; i < pad; i++ {
		buffer = append(buffer, byte(pad))
	}
	block, _ := aes.NewCipher(key)
	encrypted := make([]byte, len(buffer))
	cipher.NewCBCEncrypter(block, key[:aes.BlockSize]).CryptBlocks(encrypted, buffer)
	return base64.StdEncoding.EncodeToString(encrypted)
}

func TestOfficialEncryptedUnsubscribeCallbackIsVerifiedAndDeduplicated(t *testing.T) {
	config := enabledOfficialConfig(t)
	store := &officialRouteStore{bound: officialaccount.Binding{AppID: config.AppID, OpenID: "verified-official-openid", UserID: 42, Version: 1, Subscribed: true}}
	router, now := officialRouteFixture(t, config, store)
	plain := []byte(`<xml><FromUserName>verified-official-openid</FromUserName><MsgType>event</MsgType><Event>unsubscribe</Event><MsgId>callback-1</MsgId></xml>`)
	encrypted := encryptOfficialRouteEvent(t, config, plain)
	envelope, _ := xml.Marshal(struct {
		XMLName xml.Name `xml:"xml"`
		Encrypt string   `xml:"Encrypt"`
	}{Encrypt: encrypted})
	timestamp := strconv.FormatInt(now.Unix(), 10)
	signature := officialaccount.SignCallback(config.CallbackToken, timestamp, "nonce", encrypted)
	path := "/api/v1/wechat/official/callback?timestamp=" + timestamp + "&nonce=nonce&msg_signature=" + signature
	for i := 0; i < 2; i++ {
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest(http.MethodPost, path, strings.NewReader(string(envelope))))
		if w.Code != http.StatusOK || w.Body.String() != "success" {
			t.Fatalf("attempt=%d status=%d body=%s", i, w.Code, w.Body.String())
		}
	}
	if store.lastEvent.Type != officialaccount.EventUnsubscribe || store.bound.Subscribed || store.bound.Version != 2 {
		t.Fatalf("event=%+v binding=%+v", store.lastEvent, store.bound)
	}
}

func TestOfficialTemplateReceiptUpdatesByProviderMessageID(t *testing.T) {
	config := enabledOfficialConfig(t)
	store := &officialRouteStore{}
	router, now := officialRouteFixture(t, config, store)
	plain := []byte(`<xml><FromUserName>official-openid</FromUserName><MsgType>event</MsgType><Event>TEMPLATESENDJOBFINISH</Event><MsgID>provider-77</MsgID><Status>success</Status></xml>`)
	encrypted := encryptOfficialRouteEvent(t, config, plain)
	envelope, _ := xml.Marshal(struct {
		XMLName xml.Name `xml:"xml"`
		Encrypt string   `xml:"Encrypt"`
	}{Encrypt: encrypted})
	timestamp := strconv.FormatInt(now.Unix(), 10)
	signature := officialaccount.SignCallback(config.CallbackToken, timestamp, "nonce", encrypted)
	w := httptest.NewRecorder()
	path := "/api/v1/wechat/official/callback?timestamp=" + timestamp + "&nonce=nonce&msg_signature=" + signature
	router.ServeHTTP(w, httptest.NewRequest(http.MethodPost, path, strings.NewReader(string(envelope))))
	if w.Code != http.StatusOK || store.receiptID != "provider-77" || store.receiptStatus != "success" {
		t.Fatalf("status=%d receipt=%q/%q body=%s", w.Code, store.receiptID, store.receiptStatus, w.Body.String())
	}
}

func TestOfficialBindingNilStoreReturnsUnavailable(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	public := router.Group("/api/v1")
	authed := router.Group("/api/v1/miniprogram/official-account", func(c *gin.Context) { c.Set("user_id", int64(42)); c.Next() })
	registerOfficialAccountHandlers(public, authed, OfficialAccountDeps{Config: enabledOfficialConfig(t)})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/miniprogram/official-account/binding", nil))
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestOfficialBindingReadAndDeleteDoNotRequireCallbackCrypto(t *testing.T) {
	config := officialaccount.Config{Enabled: true, AppID: "wx-official"}
	store := &officialRouteStore{bound: officialaccount.Binding{AppID: config.AppID, OpenID: "official-openid", UserID: 42, Version: 2, Active: true, Subscribed: true}}
	router, _ := officialRouteFixture(t, config, store)
	for _, method := range []string{http.MethodGet, http.MethodDelete} {
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest(method, "/api/v1/miniprogram/official-account/binding", nil))
		if w.Code != http.StatusOK {
			t.Fatalf("method=%s status=%d body=%s", method, w.Code, w.Body.String())
		}
	}
}

func TestOfficialGETCallbackRejectsPlaintextFallback(t *testing.T) {
	config := enabledOfficialConfig(t)
	store := &officialRouteStore{}
	router, now := officialRouteFixture(t, config, store)
	timestamp := strconv.FormatInt(now.Unix(), 10)
	signature := officialaccount.SignCallback(config.CallbackToken, timestamp, "nonce", "")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/wechat/official/callback?timestamp="+timestamp+"&nonce=nonce&signature="+signature+"&echostr=plain", nil))
	if w.Code != http.StatusForbidden {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
}
