package route

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestCRMRouteAndManagementScopeAllowlist(t *testing.T) {
	for _, tc := range []struct {
		method, path string
		ok           bool
	}{
		{"GET", "/api/v1/management/crm/workbench", true}, {"POST", "/api/v1/management/crm/ingest/miniprogram", false}, {"GET", "/api/v1/management/crm/customers/a/attachments/file", true}, {"POST", "/api/v1/management/crm/customers/a/transfer", true}, {"POST", "/api/v1/management/crm/report-review", true}, {"POST", "/api/v1/management/crm/notices/a/retry-push", true},
		{"GET", "/api/v1/management/me", true}, {"POST", "/api/v1/management/logout", true},
		{"GET", "/api/v1/management/crm/customers", true}, {"POST", "/api/v1/management/crm/customers/a/followups", true},
		{"POST", "/api/v1/management/crm/customers", true}, {"POST", "/api/v1/management/crm/customers/a/claim", true},
		{"PATCH", "/api/v1/management/crm/customers/a/report", true}, {"GET", "/api/v1/management/crm/customers/a/claim", false},
		{"GET", "/api/v1/management/users", false}, {"GET", "/api/v1/management/crm/access", false},
		{"POST", "/api/v1/management/crm/provision/accounts", false}, {"GET", "/api/v1/management/crm/customers/a/extra", false},
		{"DELETE", "/api/v1/management/crm/customers/a", false}, {"GET", "/api/v1/management/crm/customers/..", false},
	} {
		if got := managementCRMPath(tc.method, tc.path); got != tc.ok {
			t.Errorf("%s %s: got %v", tc.method, tc.path, got)
		}
	}
}
func TestCRMMutationRequiresExplicitOriginAndHeader(t *testing.T) {
	t.Setenv("CRM_ALLOWED_ORIGINS", "https://admin.example.test,http://localhost:8091")
	r := httptest.NewRequest("POST", "http://localhost/api/v1/management/crm/sync", nil)
	if crmMutationAllowed(r) {
		t.Fatal("missing origin accepted")
	}
	r.Header.Set("X-CRM-Request", "1")
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Origin", "https://evil.example.test")
	if crmMutationAllowed(r) {
		t.Fatal("foreign origin accepted")
	}
	r.Header.Set("Origin", "https://admin.example.test")
	if !crmMutationAllowed(r) {
		t.Fatal("configured origin refused")
	}
	r.Header.Set("Content-Type", "text/plain")
	if crmMutationAllowed(r) {
		t.Fatal("simple content type accepted")
	}
}
func TestCRMSharedSignatureVector(t *testing.T) {
	digest := sha256.Sum256(nil)
	canonical := strings.Join([]string{"CRM-BRIDGE-V1", "v1", "crm-test", "managed-1", "1", "1700000000", "abcdefghijklmnopqrstuvwx", "GET", "/internal/management/v1/customers", "keyword=%E5%AE%A2%E6%88%B7&page=1", hex.EncodeToString(digest[:])}, "\n")
	if got := crmSignature("0123456789abcdef0123456789abcdef", canonical); got != "FYsUOul5xYtgnJ9Uhk46yrcD_YMSeKmmeGhjtjdlVzY" {
		t.Fatal(got)
	}
}
func TestCRMRemoteSignsExactRequestAndNeverFollowsRedirect(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		body, _ := io.ReadAll(r.Body)
		digest := sha256.Sum256(body)
		canonical := strings.Join([]string{"CRM-BRIDGE-V1", r.Header.Get("X-CRM-Key-Id"), r.Header.Get("X-CRM-Audience"), r.Header.Get("X-CRM-Subject"), r.Header.Get("X-CRM-User-Id"), r.Header.Get("X-CRM-Timestamp"), r.Header.Get("X-CRM-Nonce"), r.Method, r.URL.EscapedPath(), r.URL.RawQuery, hex.EncodeToString(digest[:])}, "\n")
		if r.Header.Get("X-CRM-Signature") != crmSignature("0123456789abcdef0123456789abcdef", canonical) {
			t.Error("wire signature mismatch")
		}
		if r.Header.Get("Cookie") != "" {
			t.Error("browser cookie forwarded")
		}
		if r.URL.Query().Get("keyword") != "客户 & 设计" {
			t.Error("query changed")
		}
		w.Header().Set("Location", "/unexpected")
		w.WriteHeader(302)
	}))
	defer server.Close()
	t.Setenv("CRM_BRIDGE_URL", server.URL)
	t.Setenv("CRM_BRIDGE_KEY", "0123456789abcdef0123456789abcdef")
	t.Setenv("CRM_BRIDGE_AUDIENCE", "crm-test")
	status, _, _, err := crmRemote(context.Background(), "PATCH", "/customers/test", "managed-1", 1, url.Values{"keyword": {"客户 & 设计"}}, []byte(`{"demand":"建房"}`))
	if err != nil || status != 302 || calls != 1 {
		t.Fatalf("status=%d calls=%d err=%v", status, calls, err)
	}
}

func TestCRMSourceTruncatesOnUTF16Boundary(t *testing.T) {
	if got := crmSourceText("甲😀乙", 3); got != "甲😀" {
		t.Fatalf("unicode truncation failed")
	}
	if got := crmSourceText("甲😀乙", 2); got != "甲" {
		t.Fatalf("surrogate boundary failed")
	}
}
