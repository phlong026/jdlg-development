package reportwecom

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"
)

func encryptedTestMessage(plain, corp string) (string, string) {
	key := bytes.Repeat([]byte{7}, 32)
	buf := make([]byte, 20)
	binary.BigEndian.PutUint32(buf[16:20], uint32(len(plain)))
	buf = append(buf, []byte(plain+corp)...)
	pad := 32 - len(buf)%32
	buf = append(buf, bytes.Repeat([]byte{byte(pad)}, pad)...)
	block, _ := aes.NewCipher(key)
	encrypted := make([]byte, len(buf))
	cipher.NewCBCEncrypter(block, key[:16]).CryptBlocks(encrypted, buf)
	return strings.TrimRight(base64.StdEncoding.EncodeToString(key), "="), base64.StdEncoding.EncodeToString(encrypted)
}
func signedQuery(token, encrypted string) url.Values {
	timestamp := strconv.FormatInt(time.Now().Unix(), 10)
	parts := []string{token, timestamp, "nonce", encrypted}
	sort.Strings(parts)
	digest := sha1.Sum([]byte(strings.Join(parts, "")))
	return url.Values{"timestamp": {timestamp}, "nonce": {"nonce"}, "msg_signature": {hex.EncodeToString(digest[:])}, "echostr": {encrypted}}
}
func TestEncryptedChallengeAndWrongRecipient(t *testing.T) {
	s, _, _, _, _ := setup()
	key, encrypted := encryptedTestMessage("challenge", "corp")
	for _, corp := range []string{"corp", "other"} {
		cfg := Config{Enabled: true, Token: "test-token", AESKey: key, CorpID: corp}
		q := signedQuery(cfg.Token, encrypted)
		response := httptest.NewRecorder()
		Callback(cfg, s).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/callback?"+q.Encode(), nil))
		if corp == "corp" {
			if response.Code != 200 || response.Body.String() != "challenge" {
				t.Fatal("valid challenge rejected")
			}
		} else if response.Code != 403 {
			t.Fatal("recipient not checked")
		}
	}
}
func TestInvalidSignatureNeverCallsBinding(t *testing.T) {
	s, _, _, b, _ := setup()
	key, encrypted := encryptedTestMessage("<xml/>", "corp")
	cfg := Config{Enabled: true, Token: "test", AESKey: key, CorpID: "corp"}
	q := signedQuery(cfg.Token, encrypted)
	q.Set("msg_signature", "forged")
	response := httptest.NewRecorder()
	Callback(cfg, s).ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/callback?"+q.Encode(), strings.NewReader("<xml><Encrypt>"+encrypted+"</Encrypt></xml>")))
	if response.Code != 403 || b.calls != 0 {
		t.Fatal("untrusted callback accepted")
	}
}
func TestAuthenticXMLBindsOnlyVerifiedOwner(t *testing.T) {
	s, p, st, b, _ := setup()
	s.Now = time.Now
	_, err := s.EnsureContact(t.Context(), "report", 1)
	if err != nil {
		t.Fatal(err)
	}
	xml := "<xml><Event>change_external_contact</Event><ChangeType>add_external_contact</ChangeType><State>" + st.t.State + "</State><ExternalUserID>ext</ExternalUserID><UserID>advisor</UserID><WelcomeCode>welcome</WelcomeCode><CreateTime>" + strconv.FormatInt(time.Now().Unix(), 10) + "</CreateTime></xml>"
	key, encrypted := encryptedTestMessage(xml, "corp")
	cfg := Config{Enabled: true, Token: "test", AESKey: key, CorpID: "corp"}
	q := signedQuery(cfg.Token, encrypted)
	for i := 0; i < 2; i++ {
		response := httptest.NewRecorder()
		Callback(cfg, s).ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/callback?"+q.Encode(), strings.NewReader("<xml><Encrypt>"+encrypted+"</Encrypt></xml>")))
		if response.Code != 200 {
			t.Fatal(response.Code)
		}
	}
	if p.sends != 1 || b.external != "ext" {
		t.Fatal("duplicate or wrong recipient")
	}
}

type recordingSink struct {
	records   int
	dup       bool
	recordErr error
	settled   []struct {
		key       string
		err       error
		permanent bool
	}
}

func (r *recordingSink) Record(ctx context.Context, plain []byte, encrypted string, e Event) (string, bool, error) {
	r.records++
	return "k", r.dup, r.recordErr
}
func (r *recordingSink) Settled(key string, handleErr error, permanent bool) {
	r.settled = append(r.settled, struct {
		key       string
		err       error
		permanent bool
	}{key, handleErr, permanent})
}

func addEventXML(state string) string {
	return "<xml><Event>change_external_contact</Event><ChangeType>add_external_contact</ChangeType><State>" + state + "</State><ExternalUserID>ext</ExternalUserID><UserID>advisor</UserID><WelcomeCode>welcome</WelcomeCode><CreateTime>" + strconv.FormatInt(time.Now().Unix(), 10) + "</CreateTime></xml>"
}

func TestSinkPersistenceOwnsRetries(t *testing.T) {
	s, p, st, b, _ := setup()
	s.Now = time.Now
	sink := &recordingSink{}
	if _, err := s.EnsureContact(t.Context(), "report", 1); err != nil {
		t.Fatal(err)
	}
	xml := addEventXML(st.t.State)
	key, encrypted := encryptedTestMessage(xml, "corp")
	cfg := Config{Enabled: true, Token: "test", AESKey: key, CorpID: "corp"}
	q := signedQuery(cfg.Token, encrypted)
	body := "<xml><Encrypt>" + encrypted + "</Encrypt></xml>"

	// 报告业务失败（永久的非本人事件）也必须应答成功，由 sink 负责结果与重试
	wrongEmployee := strings.Replace(xml, "advisor", "someone", 1)
	_, encrypted2 := encryptedTestMessage(wrongEmployee, "corp")
	q2 := signedQuery(cfg.Token, encrypted2)
	sink2 := &recordingSink{}
	Callback(cfg, s, sink2).ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/callback?"+q2.Encode(), strings.NewReader("<xml><Encrypt>"+encrypted2+"</Encrypt></xml>")))
	if len(sink2.settled) != 1 || !sink2.settled[0].permanent {
		t.Fatal("permanent event error not classified")
	}

	// 正常事件：落库 → 处理 → Settled(nil)
	response := httptest.NewRecorder()
	Callback(cfg, s, sink).ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/callback?"+q.Encode(), strings.NewReader(body)))
	if response.Code != 200 || sink.records != 1 || p.sends != 1 || b.external != "ext" {
		t.Fatalf("sink flow broken: code=%d records=%d sends=%d", response.Code, sink.records, p.sends)
	}
	if len(sink.settled) != 1 || sink.settled[0].err != nil {
		t.Fatal("successful event not settled")
	}

	// 重复推送：不再重复处理
	dupSink := &recordingSink{dup: true}
	Callback(cfg, s, dupSink).ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/callback?"+q.Encode(), strings.NewReader(body)))
	if dupSink.settled != nil || p.sends != 1 {
		t.Fatal("duplicate event reprocessed")
	}

	// 落库失败：必须让企微重推（503），不能丢事件
	failSink := &recordingSink{recordErr: errors.New("db down")}
	response = httptest.NewRecorder()
	Callback(cfg, s, failSink).ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/callback?"+q.Encode(), strings.NewReader(body)))
	if response.Code != 503 {
		t.Fatalf("record failure must return 503, got %d", response.Code)
	}

	// 未注入 sink 时保持旧行为：业务失败 → 503
	_, encrypted3 := encryptedTestMessage(wrongEmployee, "corp")
	q3 := signedQuery(cfg.Token, encrypted3)
	response = httptest.NewRecorder()
	Callback(cfg, s).ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/callback?"+q3.Encode(), strings.NewReader("<xml><Encrypt>"+encrypted3+"</Encrypt></xml>")))
	if response.Code != 503 {
		t.Fatalf("legacy behavior must stay 503, got %d", response.Code)
	}
}
