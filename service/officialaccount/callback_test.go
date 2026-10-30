package officialaccount

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"encoding/xml"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func encryptCallbackForTest(t *testing.T, aesKey, appID string, plain []byte) string {
	t.Helper()
	key, err := base64.StdEncoding.DecodeString(aesKey + "=")
	if err != nil {
		t.Fatal(err)
	}
	random := make([]byte, 16)
	if _, err = rand.Read(random); err != nil {
		t.Fatal(err)
	}
	buf := append([]byte{}, random...)
	length := make([]byte, 4)
	binary.BigEndian.PutUint32(length, uint32(len(plain)))
	buf = append(buf, length...)
	buf = append(buf, plain...)
	buf = append(buf, appID...)
	pad := 32 - len(buf)%32
	buf = append(buf, make([]byte, pad)...)
	for i := len(buf) - pad; i < len(buf); i++ {
		buf[i] = byte(pad)
	}
	block, _ := aes.NewCipher(key)
	out := make([]byte, len(buf))
	cipher.NewCBCEncrypter(block, key[:aes.BlockSize]).CryptBlocks(out, buf)
	return base64.StdEncoding.EncodeToString(out)
}

func TestOAuthProviderUsesProviderCodeAndOfficialOpenID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/sns/oauth2/access_token":
			if r.URL.Query().Get("code") != "one-use-code" || r.URL.Query().Get("appid") != "wx-official" {
				t.Fatalf("unexpected oauth query: %s", r.URL.RawQuery)
			}
			_, _ = w.Write([]byte(`{"openid":"official-openid","scope":"snsapi_base"}`))
		case "/cgi-bin/token":
			_, _ = w.Write([]byte(`{"errcode":0,"access_token":"provider-token","expires_in":7200}`))
		case "/cgi-bin/user/info":
			if r.URL.Query().Get("openid") != "official-openid" || r.URL.Query().Get("access_token") != "provider-token" {
				t.Fatalf("unexpected user info query: %s", r.URL.RawQuery)
			}
			_, _ = w.Write([]byte(`{"subscribe":1,"openid":"official-openid"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	config := Config{Enabled: true, AppID: "wx-official", AppSecret: "official-secret", CallbackToken: "token", EncodingAESKey: "1234567890123456789012345678901234567890123", PublicOrigin: "https://www.jiadilingguang.com"}
	provider := NewHTTPProvider(config)
	provider.base = server.URL
	provider.api.Base = server.URL
	provider.client.Timeout = time.Second
	provider.api.Client.Timeout = time.Second
	identity, err := provider.ExchangeCode(context.Background(), "one-use-code")
	if err != nil || identity.OpenID != "official-openid" || !identity.Subscribed {
		t.Fatalf("identity=%+v err=%v", identity, err)
	}
}

func TestParseEncryptedCallbackVerifiesSignatureAndAppID(t *testing.T) {
	keyBytes := make([]byte, 32)
	for i := range keyBytes {
		keyBytes[i] = byte(i + 1)
	}
	aesKey := base64.StdEncoding.EncodeToString(keyBytes)[:43]
	plain := []byte(`<xml><ToUserName>wx-official</ToUserName><FromUserName>official-openid</FromUserName><CreateTime>1</CreateTime><MsgType>event</MsgType><Event>unsubscribe</Event><MsgId>99</MsgId></xml>`)
	encrypted := encryptCallbackForTest(t, aesKey, "wx-official", plain)
	sig := SignCallback("callback-token", "100", "nonce", encrypted)
	body, _ := xml.Marshal(struct {
		XMLName xml.Name `xml:"xml"`
		Encrypt string   `xml:"Encrypt"`
	}{Encrypt: encrypted})
	event, err := ParseEncryptedCallback(Config{AppID: "wx-official", CallbackToken: "callback-token", EncodingAESKey: aesKey}, "100", "nonce", sig, body)
	if err != nil || event.OpenID != "official-openid" || event.Type != EventUnsubscribe || event.DedupKey == "" {
		t.Fatalf("event=%+v err=%v", event, err)
	}
	if _, err := ParseEncryptedCallback(Config{AppID: "other-app", CallbackToken: "callback-token", EncodingAESKey: aesKey}, "100", "nonce", sig, body); err == nil {
		t.Fatal("appid mismatch accepted")
	}
}

func TestParseEncryptedTemplateReceiptKeepsProviderMessageID(t *testing.T) {
	keyBytes := make([]byte, 32)
	for i := range keyBytes {
		keyBytes[i] = byte(i + 1)
	}
	aesKey := base64.StdEncoding.EncodeToString(keyBytes)[:43]
	plain := []byte(`<xml><FromUserName>official-openid</FromUserName><MsgType>event</MsgType><Event>TEMPLATESENDJOBFINISH</Event><MsgID>200163840</MsgID><Status>success</Status></xml>`)
	encrypted := encryptCallbackForTest(t, aesKey, "wx-official", plain)
	sig := SignCallback("callback-token", "100", "nonce", encrypted)
	body, _ := xml.Marshal(struct {
		XMLName xml.Name `xml:"xml"`
		Encrypt string   `xml:"Encrypt"`
	}{Encrypt: encrypted})
	event, err := ParseEncryptedCallback(Config{AppID: "wx-official", CallbackToken: "callback-token", EncodingAESKey: aesKey}, "100", "nonce", sig, body)
	if err != nil || event.Type != EventTemplateSendFinished || event.ProviderMessageID != "200163840" || event.ProviderStatus != "success" {
		t.Fatalf("event=%+v err=%v", event, err)
	}
}
