package route

import (
	"context"
	"errors"
	"github.com/gin-gonic/gin"
	"net/http"
	"net/http/httptest"
	"service/model"
	"strings"
	"testing"
	"time"
)

type consultationStoreFixture struct {
	fail                  bool
	failEvent             bool
	lastEvent             string
	events                []string
	leadCalls, eventCalls int
	user                  int64
}

func (s *consultationStoreFixture) CreateLead(in *model.CustomerLeadCreateInput) (int64, error) {
	s.leadCalls++
	s.user = in.UserID
	if s.fail {
		return 0, errors.New("test failure")
	}
	return 73, nil
}
func (s *consultationStoreFixture) CreateEvent(in *model.CustomerServiceEventCreateInput) (int64, error) {
	s.eventCalls++
	s.lastEvent = in.EventType
	s.events = append(s.events, in.EventType)
	if s.failEvent && in.EventType == "consultation_request_submitted" {
		return 0, errors.New("event unavailable")
	}
	return 82, nil
}
func TestConsultationHookOnlyAfterExplicitCommittedRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name, path, body, token string
		account                 int64
		fail, want              bool
		status                  int
	}{
		{"confirmed", "consultation-requests", `{"demand_summary":"test demand","user_id":999}`, "opaque", 41, false, true, 200},
		{"automatic lead never high", "leads", `{"demand_summary":"test","payload":{"reason":"handoff_human"}}`, "opaque", 41, false, false, 200},
		{"generic client event never high", "events", `{"event_type":"lead_submit","lead_id":73}`, "opaque", 41, false, false, 200},
		{"commit failed", "consultation-requests", `{"demand_summary":"test"}`, "opaque", 41, true, false, 500},
		{"business proof failed", "consultation-requests", `{"demand_summary":"test"}`, "opaque", 41, false, false, 200},
		{"invalid body", "consultation-requests", `{}`, "opaque", 41, false, false, 400},
		{"no consent context", "consultation-requests", `{"demand_summary":"test"}`, "", 41, false, false, 200},
		{"unauthenticated", "consultation-requests", `{"demand_summary":"test"}`, "opaque", 0, false, false, 401},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := &consultationStoreFixture{fail: tc.fail, failEvent: tc.name == "business proof failed"}
			calls := make(chan [2]int64, 1)
			router := gin.New()
			router.Use(func(c *gin.Context) {
				if tc.account > 0 {
					c.Set("code_session", &model.CodeSession{UserID: tc.account})
				}
				c.Next()
			})
			registerCustomerLeadRoutes(router.Group("/api"), store, func(ctx context.Context, token string, account, lead int64) error {
				if token != "opaque" {
					t.Errorf("unexpected token")
				}
				if _, ok := ctx.Deadline(); !ok {
					t.Errorf("missing timeout")
				}
				calls <- [2]int64{account, lead}
				return errors.New("analytics unavailable")
			})
			req := httptest.NewRequest(http.MethodPost, "/api/customer-service/"+tc.path, strings.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("X-Tracking-Token", tc.token)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
			if w.Code != tc.status {
				t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
			}
			if tc.path == "consultation-requests" && store.eventCalls > 1 && store.lastEvent != "consultation_request_submitted" {
				t.Fatal("proof not fixed by server")
			}
			if store.leadCalls > 0 && store.eventCalls > 0 && store.events[0] != "lead_submit" {
				t.Fatal("existing lead funnel event lost")
			}
			if tc.want {
				select {
				case got := <-calls:
					if got != [2]int64{41, 73} {
						t.Fatalf("wrong binding %v", got)
					}
				case <-time.After(time.Second):
					t.Fatal("no confirmed hook")
				}
			} else {
				select {
				case got := <-calls:
					t.Fatalf("unexpected hook %v", got)
				case <-time.After(10 * time.Millisecond):
				}
			}
		})
	}
}
func TestConsultationAnalyticsNeverBlocksBusinessResponse(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(func(c *gin.Context) { c.Set("code_session", &model.CodeSession{UserID: 41}); c.Next() })
	entered := make(chan struct{})
	release := make(chan struct{})
	defer close(release)
	registerCustomerLeadRoutes(router.Group("/api"), &consultationStoreFixture{}, func(context.Context, string, int64, int64) error { close(entered); <-release; return nil })
	req := httptest.NewRequest(http.MethodPost, "/api/customer-service/consultation-requests", strings.NewReader(`{"demand_summary":"test"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tracking-Token", "opaque")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatal(w.Code)
	}
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("hook did not start")
	}
}
