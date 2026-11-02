package route

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestShareWebProfileGateHidesContentUntilNativeLogin(t *testing.T) {
	router, _, store, code, content := shareWebFixture(t)
	before := len(store.records)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/share/s/"+code, nil))
	body := w.Body.String()
	if w.Code != http.StatusOK || !strings.Contains(body, "登录后查看完整内容") || strings.Contains(body, content.Title) || strings.Contains(body, "安全正文") || strings.Contains(body, "<video") || strings.Contains(body, `id="consent"`) {
		t.Fatalf("anonymous landing leaked content or missed login: %d %s", w.Code, body)
	}
	if len(store.records) != before {
		t.Fatal("login landing created browsing facts")
	}
}

func TestShareWebProfileGateHidesCollectionAndPreservesExactTarget(t *testing.T) {
	router, _, store, code, collection, content := shareWebCollectionFixture(t)
	before := len(store.records)
	for _, suffix := range []string{"", "/items/0"} {
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/share/s/"+code+suffix, nil))
		body := w.Body.String()
		if w.Code != http.StatusOK || !strings.Contains(body, "登录后查看完整内容") || strings.Contains(body, collection.Title) || strings.Contains(body, content.Title) || strings.Contains(body, "合集作品正文") {
			t.Fatalf("anonymous collection leaked content: %d %s", w.Code, body)
		}
		if suffix != "" && !strings.Contains(body, `data-item-position="0"`) {
			t.Fatal("login handoff lost collection item")
		}
	}
	if len(store.records) != before {
		t.Fatal("collection landing created browsing facts")
	}
}

func TestShareWebProfileGateRejectsAnonymousTrackingAndMedia(t *testing.T) {
	router, _, store, code, _ := shareWebFixture(t)
	before := len(store.records)
	for _, test := range []struct{ method, path string }{
		{http.MethodPost, "/api/v1/share-web/entry"},
		{http.MethodPost, "/api/v1/share-web/events"},
		{http.MethodGet, "/api/v1/share-web/media/" + code + "/" + shareWebVideoID},
		{http.MethodHead, "/api/v1/share-web/media/" + code + "/" + shareWebVideoID},
		{http.MethodGet, "/api/v1/share-web/collection-media/" + code + "/0/" + shareWebVideoID},
		{http.MethodHead, "/api/v1/share-web/collection-media/" + code + "/0/" + shareWebVideoID},
	} {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(test.method, test.path, bytes.NewBufferString(`{}`))
		req.Header.Set("Origin", "https://www.jiadilingguang.com")
		req.Header.Set("X-Share-Web", "1")
		router.ServeHTTP(w, req)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("anonymous %s %s = %d %s", test.method, test.path, w.Code, w.Body.String())
		}
	}
	if len(store.records) != before {
		t.Fatal("rejected web requests wrote browsing facts")
	}
}
