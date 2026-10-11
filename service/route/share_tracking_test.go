package route

import (
	"github.com/gin-gonic/gin"
	"net/http/httptest"
	"service/tracking"
	"strings"
	"testing"
)

func TestTrackingStrictBodyRejectsClientIdentity(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, body := range []string{`{"route_key":"pages/home/home","user_id":1}`, `{"route_key":"pages/home/home","tenant_id":"other"}`, `{"route_key":"pages/home/home","owner_id":"other"}`, `{} {}`} {
		r := gin.New()
		r.POST("/", func(c *gin.Context) {
			var in tracking.EntryInput
			if trackingBind(c, &in) {
				c.Status(204)
			}
		})
		w := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		if w.Code != 400 {
			t.Fatalf("body %s status %d", body, w.Code)
		}
	}
}
func TestTrackingBodyLimit(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/", func(c *gin.Context) { var in tracking.EntryInput; trackingBind(c, &in) })
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("POST", "/", strings.NewReader(`{"route_key":"`+strings.Repeat("a", 70000)+`"}`)))
	if w.Code != 413 {
		t.Fatal(w.Code)
	}
}
func TestTrackingMutationCSRF(t *testing.T) {
	t.Setenv("ADMIN_ALLOWED_ORIGINS", "https://admin.example.test")
	t.Setenv("CRM_ALLOWED_ORIGINS", "")
	for _, tc := range []struct {
		origin, header string
		want           bool
	}{{"", "1", false}, {"https://evil.example", "1", false}, {"https://admin.example.test", "1", true}, {"https://admin.example.test", "", false}} {
		r := httptest.NewRequest("POST", "https://admin.example.test/api/v1/management/tracking/contents", nil)
		r.Header.Set("Origin", tc.origin)
		r.Header.Set("X-Tracking-Request", tc.header)
		if trackingMutationAllowed(r) != tc.want {
			t.Fatalf("%+v", tc)
		}
	}
}
func TestTrackingReadScopeCannotUseClientTenant(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("GET", "/?tenant_id=other&owner=alice", nil)
	f, e := trackingFilter(c)
	if e != nil || f.Owner != "alice" {
		t.Fatal(f, e)
	}
	if f.Content != "" {
		t.Fatal(f)
	}
}
func TestTrackingErrorResponseRedactsInternalFailures(t *testing.T) {
	r := gin.New()
	r.GET("/", func(c *gin.Context) { trackingResponse(c, nil, tracking.ErrUnavailable) })
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
	if w.Code != 503 || strings.Contains(w.Body.String(), "tracking unavailable") {
		t.Fatal(w.Code, w.Body.String())
	}
}
func TestTrackingMP4RejectsMalformedAtoms(t *testing.T) {
	for _, b := range [][]byte{nil, []byte("not a video"), {0, 0, 0, 2, 'm', 'o', 'o', 'v'}, {0, 0, 0, 99, 'm', 'o', 'o', 'v'}} {
		if _, _, e := trackingMP4Dimensions(b); e == nil {
			t.Fatal("invalid video accepted")
		}
	}
}
func TestTrackingCRMTargetRequiresCanonicalAccessibleCustomer(t *testing.T) {
	for _, tc := range []struct {
		body string
		want bool
	}{{`{"code":0,"data":{"customer":{"id":"C1","customerNumber":"B1","mergedInto":null}}}`, true}, {`{"code":0,"data":{"customer":{"id":"C2","customerNumber":"B1"}}}`, false}, {`{"code":0,"data":{"customer":{"id":"C1","customerNumber":"B2"}}}`, false}, {`{"code":0,"data":{"customer":{"id":"C1","customerNumber":"B1","mergedInto":"C2"}}}`, false}, {`{"code":0,"data":{"customer":{"id":"C1","customerNumber":"B1","lifecycle":"deleted"}}}`, false}, {`{"data":{"customer":{"id":"C1","customerNumber":"B1"}}}`, false}, {`{`, false}} {
		if validTrackingCRMTarget([]byte(tc.body), "C1", "B1") != tc.want {
			t.Fatal(tc)
		}
	}
}
