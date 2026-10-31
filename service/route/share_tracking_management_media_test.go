package route

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"service/model"
	"service/tracking"
)

func TestManagementTrackingMediaRouteUsesAuthorizedScope(t *testing.T) {
	gin.SetMode(gin.TestMode)
	registered := gin.New()
	registered.Use(sessions.Sessions("test", cookie.NewStore([]byte("test-only-cookie-signing-key-32-bytes"))))
	svc := RegisterTrackingRoutes(registered.Group("/api/v1"), &model.UserModel{}, nil, nil)
	svc.Config.Tenant = "management-media"
	svc.Config.ContentEnabled = true
	svc.Store = &governanceTestStore{records: map[string]tracking.Record{}}
	svc.Now = func() time.Time { return time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC) }
	svc.MediaObjects = &testPrivateObjects{
		data: map[string][]byte{},
		mime: map[string]string{},
	}

	const id = "0123456789abcdefghijklmnopqrstuv"
	payload := []byte("private-media")
	svc.MediaObjects.(*testPrivateObjects).data[svc.Config.Tenant+":"+id] = payload
	svc.MediaObjects.(*testPrivateObjects).mime[svc.Config.Tenant+":"+id] = "image/png"
	ownerScope := tracking.Scope{Tenant: svc.Config.Tenant, Actor: 7, Owners: []string{"owner-a"}, View: true, Manage: true}
	if err := svc.AddMedia(context.Background(), ownerScope, tracking.Asset{
		ID: id, Kind: "image", MIME: "image/png", Width: 2, Height: 2,
		URL: "/api/v1/miniprogram/tracking/media/" + id,
	}, "owner-a"); err != nil {
		t.Fatal(err)
	}

	handlers := map[string]gin.HandlerFunc{}
	for _, route := range registered.Routes() {
		if route.Path == "/api/v1/management/tracking/media/:id" {
			handlers[route.Method] = route.HandlerFunc
		}
	}
	for _, method := range []string{http.MethodGet, http.MethodHead} {
		if handlers[method] == nil {
			t.Fatalf("missing %s /api/v1/management/tracking/media/:id", method)
		}
	}
	unauthenticated := httptest.NewRecorder()
	registered.ServeHTTP(unauthenticated, httptest.NewRequest(http.MethodGet, "/api/v1/management/tracking/media/"+id, nil))
	if unauthenticated.Code != http.StatusUnauthorized {
		t.Fatalf("management media without session code=%d body=%q", unauthenticated.Code, unauthenticated.Body.String())
	}

	request := func(method string, scope tracking.Scope) *mediaDeadlineRecorder {
		t.Helper()
		router := gin.New()
		router.Handle(method, "/api/v1/management/tracking/media/:id", func(c *gin.Context) {
			c.Set("tracking.scope", scope)
			c.Next()
		}, handlers[method])
		response := newMediaDeadlineRecorder()
		router.ServeHTTP(response, httptest.NewRequest(method, "/api/v1/management/tracking/media/"+id, nil))
		return response
	}

	get := request(http.MethodGet, ownerScope)
	if get.Code != http.StatusOK || !bytes.Equal(get.Body.Bytes(), payload) || get.Header().Get("Cache-Control") != "private, no-store" {
		t.Fatalf("authorized GET code=%d body=%q cache=%q", get.Code, get.Body.String(), get.Header().Get("Cache-Control"))
	}
	head := request(http.MethodHead, ownerScope)
	if head.Code != http.StatusOK || head.Body.Len() != 0 {
		t.Fatalf("authorized HEAD code=%d body=%q", head.Code, head.Body.String())
	}
	foreign := ownerScope
	foreign.Owners = []string{"owner-b"}
	denied := request(http.MethodGet, foreign)
	if denied.Code != http.StatusNotFound || strings.Contains(denied.Body.String(), string(payload)) {
		t.Fatalf("foreign scope code=%d body=%q", denied.Code, denied.Body.String())
	}
}
