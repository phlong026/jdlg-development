package route

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"service/function"
	"service/model"
)

const (
	clientSignatureTestSessionID = "client-signature-test-session"
	clientSignatureTestDeviceID  = "client-signature-test-device"
)

type clientSignatureFixtureInput struct {
	SignerPath       string `json:"signerPath"`
	Token            string `json:"token"`
	Body             string `json:"body"`
	Path             string `json:"path"`
	DeviceID         string `json:"deviceID"`
	ExplicitDefaults bool   `json:"explicitDefaults"`
}

type clientSignatureRequest struct {
	name   string
	method string
	path   string
	body   string
}

func miniprogramClientSignatureRequests() []clientSignatureRequest {
	return []clientSignatureRequest{
		{name: "profile GET", method: http.MethodGet, path: "/api/v1/miniprogram/profile", body: "{}"},
		{name: "GET with query", method: http.MethodGet, path: "/api/v1/miniprogram/templates?page=2&keyword=%E4%B9%A1%E5%A2%85", body: "{}"},
		{name: "POST JSON", method: http.MethodPost, path: "/api/v1/miniprogram/profile/identity", body: `{"identity":"designer","source":"test"}`},
	}
}

// TestMiniprogramClientSignerDefaultsPassTokenAuthRequired protects the
// checked-in generated JavaScript artifact. It does not reproduce the signing
// algorithm in Go or prove which artifact a real-device compiler selects.
func TestMiniprogramClientSignerDefaultsPassTokenAuthRequired(t *testing.T) {
	testMiniprogramClientSignerAgainstTokenAuth(t, false)
}

// This control proves that the cross-language algorithms agree when the
// public development defaults are supplied explicitly. A failure in the test
// above with this test passing points to stale generated JavaScript defaults.
func TestMiniprogramClientSignerExplicitDefaultsPassTokenAuthRequired(t *testing.T) {
	testMiniprogramClientSignerAgainstTokenAuth(t, true)
}

func testMiniprogramClientSignerAgainstTokenAuth(t *testing.T, explicitDefaults bool) {
	t.Helper()
	strict := os.Getenv("JDLG_REQUIRE_CLIENT_AUTH_CONTRACT") == "1"
	if !strict {
		if err := function.InitCrypto("", "", base64.StdEncoding.EncodeToString([]byte("1234567890123456"))); err != nil {
			t.Fatal(err)
		}
	} else if expected := os.Getenv("JDLG_AUTH_CRYPTO_SHA256"); expected != "" {
		source, err := os.ReadFile(filepath.Join("..", "function", "crypto.go"))
		if err != nil {
			t.Fatal(err)
		}
		digest := sha256.Sum256(source)
		if hex.EncodeToString(digest[:]) != expected {
			t.Fatal("strict auth contract is not using the explicitly injected crypto source")
		}
	}

	db := sql.OpenDB(profileGateAuthConnector{})
	defer db.Close()
	mr := miniredis.RunT(t)
	redisClient := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer redisClient.Close()

	session := model.CodeSession{
		SessionID: clientSignatureTestSessionID,
		DeviceID:  clientSignatureTestDeviceID,
		UserID:    41,
	}
	rawSession, err := json.Marshal(session)
	if err != nil {
		t.Fatal(err)
	}
	if err := redisClient.HSet(context.Background(), "code_session:session:"+session.SessionID, "data", rawSession).Err(); err != nil {
		t.Fatal(err)
	}
	token, err := function.GenerateToken(session.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	probe, err := signWithMiniprogramClient(t, token, "{}", "/api/v1/miniprogram/profile", session.DeviceID, explicitDefaults)
	if err != nil {
		t.Fatal(err)
	}
	if !function.VerifyTokenSignature(token, probe["token-signature"]) {
		if os.Getenv("JDLG_REQUIRE_CLIENT_AUTH_CONTRACT") == "1" || !backendSourceUsesReleasePlaceholders(t) {
			t.Fatal("protected backend HMAC does not match the checked-in mini-program signer")
		}
		t.Skip("committed backend source contains release placeholders; protected release build must inject crypto.go and require this contract")
	}

	router := gin.New()
	router.Use(TokenAuthRequired(model.NewCodeSessionRedisModel(db, redisClient)))
	router.GET("/api/v1/miniprogram/profile", clientSignatureOK)
	router.GET("/api/v1/miniprogram/templates", clientSignatureOK)
	router.POST("/api/v1/miniprogram/profile/identity", clientSignatureOK)

	for _, test := range miniprogramClientSignatureRequests() {
		t.Run(test.name, func(t *testing.T) {
			headers, err := signWithMiniprogramClient(t, token, test.body, test.path, session.DeviceID, explicitDefaults)
			if err != nil {
				t.Fatal(err)
			}
			var body *bytes.Reader
			if test.method == http.MethodGet {
				body = bytes.NewReader(nil)
			} else {
				body = bytes.NewReader([]byte(test.body))
			}
			request := httptest.NewRequest(test.method, test.path, body)
			for key, value := range headers {
				request.Header.Set(key, value)
			}
			if test.method == http.MethodPost {
				request.Header.Set("Content-Type", "application/json")
			}
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != http.StatusNoContent {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
		})
	}
}

func TestMiniprogramClientSignerStrictSM4Contract(t *testing.T) {
	if backendSourceUsesReleasePlaceholders(t) {
		t.Skip("committed backend source contains release placeholders")
	}
	for _, test := range []struct {
		name    string
		mode    string
		wantErr bool
	}{
		{name: "matching default", mode: "matching"},
		{name: "wrong SM4", mode: "wrong", wantErr: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			command := exec.Command(os.Args[0], "-test.run=^TestMiniprogramClientSignerStrictSM4Probe$", "-test.count=1")
			command.Env = append(os.Environ(), "JDLG_REQUIRE_CLIENT_AUTH_CONTRACT=1", "JDLG_AUTH_SM4_PROBE="+test.mode)
			output, err := command.CombinedOutput()
			if test.wantErr && err == nil {
				t.Fatal("strict auth contract accepted a mismatched SM4 key")
			}
			if !test.wantErr && err != nil {
				t.Fatalf("strict auth contract rejected matching defaults: %v: %s", err, bytes.TrimSpace(output))
			}
			if test.wantErr && !bytes.Contains(output, []byte("sin验证失败")) {
				t.Fatalf("wrong SM4 failed for an unexpected reason: %s", bytes.TrimSpace(output))
			}
		})
	}
}

func TestMiniprogramClientSignerStrictSM4Probe(t *testing.T) {
	mode := os.Getenv("JDLG_AUTH_SM4_PROBE")
	if mode == "" {
		t.Skip("subprocess probe only")
	}
	if mode == "wrong" {
		if err := function.InitCrypto("", "", base64.StdEncoding.EncodeToString([]byte("6543210987654321"))); err != nil {
			t.Fatal(err)
		}
	}
	testMiniprogramClientSignerAgainstTokenAuth(t, false)
}

func backendSourceUsesReleasePlaceholders(t *testing.T) bool {
	t.Helper()
	source, err := os.ReadFile(filepath.Join("..", "function", "crypto.go"))
	if err != nil {
		t.Fatal(err)
	}
	return bytes.Contains(source, []byte(`sm4Key = []byte("REPLACE_SM4_KEY!")`)) &&
		bytes.Contains(source, []byte(`hmacKey = []byte("REPLACE_WITH_LOCAL_HMAC_KEY")`))
}

func clientSignatureOK(c *gin.Context) {
	if GetTokenUserID(c) != 41 {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"code": 401, "msg": "missing authenticated user"})
		return
	}
	c.Status(http.StatusNoContent)
}

func signWithMiniprogramClient(t *testing.T, token, body, path, deviceID string, explicitDefaults bool) (map[string]string, error) {
	t.Helper()
	workingDirectory, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	fixturePath := filepath.Join(workingDirectory, "testdata", "miniprogram_client_sign.js")
	signerPath := filepath.Join(workingDirectory, "..", "..", "miniprogram", "miniprogram", "utils", "parameter.js")
	input, err := json.Marshal(clientSignatureFixtureInput{
		SignerPath:       signerPath,
		Token:            token,
		Body:             body,
		Path:             path,
		DeviceID:         deviceID,
		ExplicitDefaults: explicitDefaults,
	})
	if err != nil {
		return nil, err
	}
	command := exec.Command("node", fixturePath)
	command.Stdin = bytes.NewReader(input)
	output, err := command.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("miniprogram signer failed: %w: %s", err, bytes.TrimSpace(output))
	}
	var headers map[string]string
	if err := json.Unmarshal(output, &headers); err != nil {
		return nil, fmt.Errorf("decode miniprogram signer output: %w: %s", err, bytes.TrimSpace(output))
	}
	return headers, nil
}
