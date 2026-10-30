package route

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"service/notification"
)

type fakeInboxStore struct {
	recipient int64
	cursor    string
	limit     int
	readID    string
	getID     string
	readAll   bool
	fail      error
}

func (s *fakeInboxStore) ListInbox(_ context.Context, recipient int64, cursor string, limit int) (notification.Page, error) {
	s.recipient, s.cursor, s.limit = recipient, cursor, limit
	return notification.Page{Items: []notification.Notification{{ID: "event-0001", RecipientID: recipient}}}, s.fail
}
func (s *fakeInboxStore) MarkRead(_ context.Context, recipient int64, id string, _ time.Time) error {
	s.recipient, s.readID = recipient, id
	return s.fail
}
func (s *fakeInboxStore) GetNotification(_ context.Context, recipient int64, id string) (notification.Notification, error) {
	s.recipient, s.getID = recipient, id
	return notification.Notification{ID: id, RecipientID: recipient}, s.fail
}
func (s *fakeInboxStore) UnreadCount(_ context.Context, recipient int64) (int64, error) {
	s.recipient = recipient
	return 3, s.fail
}
func (s *fakeInboxStore) MarkAllRead(_ context.Context, recipient int64, _ time.Time) (int64, error) {
	s.recipient, s.readAll = recipient, true
	return 2, s.fail
}
func (s *fakeInboxStore) DeleteVisitorData(context.Context, string, time.Time) (int64, error) {
	return 0, nil
}
func (s *fakeInboxStore) CancelPending(context.Context, string, string, time.Time) (int64, error) {
	return 0, nil
}

func TestNotificationDetailUnreadAndReadAllStayRecipientScoped(t *testing.T) {
	store := &fakeInboxStore{}
	router := notificationRouter(store)
	for _, target := range []struct {
		method string
		path   string
		check  func(*httptest.ResponseRecorder) bool
	}{
		{http.MethodGet, "/notifications/unread-count?user_id=99", func(w *httptest.ResponseRecorder) bool {
			return w.Code == 200 && store.recipient == 42 && strings.Contains(w.Body.String(), `"unread_count":3`)
		}},
		{http.MethodGet, "/notifications/event-1?user_id=99", func(w *httptest.ResponseRecorder) bool {
			return w.Code == 200 && store.recipient == 42 && store.getID == "event-1"
		}},
		{http.MethodPatch, "/notifications/read-all?user_id=99", func(w *httptest.ResponseRecorder) bool {
			return w.Code == 200 && store.recipient == 42 && store.readAll && strings.Contains(w.Body.String(), `"updated":2`)
		}},
	} {
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest(target.method, target.path, nil))
		if !target.check(w) {
			t.Fatalf("%s %s status=%d body=%s store=%+v", target.method, target.path, w.Code, w.Body.String(), store)
		}
	}
}

func notificationRouter(store notification.InboxStore) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	group := router.Group("/notifications", func(c *gin.Context) {
		c.Set("user_id", int64(42))
		c.Next()
	})
	registerNotificationHandlers(group, store)
	return router
}

func TestNotificationInboxUsesAuthenticatedRecipientOnly(t *testing.T) {
	store := &fakeInboxStore{}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/notifications?cursor=opaque&limit=10&user_id=99", nil)
	notificationRouter(store).ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK || store.recipient != 42 || store.cursor != "opaque" || store.limit != 10 {
		t.Fatalf("status=%d store=%+v body=%s", recorder.Code, store, recorder.Body.String())
	}
	if recorder.Header().Get("Cache-Control") != "private, no-store" {
		t.Fatal("inbox response may be cached")
	}
}

func TestNotificationReadCannotSelectAnotherRecipient(t *testing.T) {
	store := &fakeInboxStore{}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPatch, "/notifications/event-0001/read?user_id=99", nil)
	notificationRouter(store).ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK || store.recipient != 42 || store.readID != "event-0001" {
		t.Fatalf("status=%d store=%+v", recorder.Code, store)
	}
}

func TestNotificationRouteMapsScopedMissAndInvalidLimit(t *testing.T) {
	store := &fakeInboxStore{fail: notification.ErrConflict}
	recorder := httptest.NewRecorder()
	notificationRouter(store).ServeHTTP(recorder, httptest.NewRequest(http.MethodPatch, "/notifications/other/read", nil))
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	store.fail = errors.New("must not be called")
	recorder = httptest.NewRecorder()
	notificationRouter(store).ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/notifications?limit=nope", nil))
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestNotificationNilStoreReturnsUnavailable(t *testing.T) {
	w := httptest.NewRecorder()
	notificationRouter(nil).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/notifications", nil))
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
}
