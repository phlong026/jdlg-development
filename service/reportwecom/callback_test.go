package reportwecom

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
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
