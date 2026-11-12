package route

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"service/model"
)

type linearizedConsentStore struct {
	mu            sync.Mutex
	accepted      bool
	createEntered chan struct{}
	createRelease chan struct{}
	setEntered    chan struct{}
	setRelease    chan struct{}
	lastPhone     string
}

func newLinearizedConsentStore(accepted bool) *linearizedConsentStore {
	return &linearizedConsentStore{
		accepted:      accepted,
		createEntered: make(chan struct{}),
		createRelease: make(chan struct{}),
		setEntered:    make(chan struct{}),
		setRelease:    make(chan struct{}),
	}
}

func (s *linearizedConsentStore) CreateLead(in *model.CustomerLeadCreateInput) (int64, error) {
	s.lastPhone = in.Phone
	return 73, nil
}
func (s *linearizedConsentStore) CreateEvent(*model.CustomerServiceEventCreateInput) (int64, error) {
	return 82, nil
}
func (s *linearizedConsentStore) BusinessContactConsent(int64) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.accepted, nil
}
func (s *linearizedConsentStore) SetBusinessContactConsent(_ int64, accepted bool, _ string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	close(s.setEntered)
	<-s.setRelease
	s.accepted = accepted
	return nil
}
func (s *linearizedConsentStore) CreateLeadWithBusinessContact(in *model.CustomerLeadCreateInput) (int64, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	close(s.createEntered)
	<-s.createRelease
	if s.accepted {
		in.Phone = "13800000000"
	}
	s.lastPhone = in.Phone
	return 73, s.accepted, nil
}

type consultationStoreFixture struct {
	fail                  bool
	failEvent             bool
	lastEvent             string
	events                []string
	leadCalls, eventCalls int
	user                  int64
	submitFailures        int                                      // 前 N 次 lead_submit 写入失败
	submitAttempts        []*model.CustomerServiceEventCreateInput // 每次 lead_submit 的入参
	lastLead              *model.CustomerLeadCreateInput
	contactConsent        bool
	consentReads          int
	consentWrites         []bool
}

func (s *consultationStoreFixture) CreateLead(in *model.CustomerLeadCreateInput) (int64, error) {
	s.leadCalls++
	s.user = in.UserID
	copy := *in
	s.lastLead = &copy
	if s.fail {
		return 0, errors.New("test failure")
	}
	return 73, nil
}
func (s *consultationStoreFixture) BusinessContactConsent(userID int64) (bool, error) {
	s.consentReads++
	return s.contactConsent, nil
}
func (s *consultationStoreFixture) SetBusinessContactConsent(userID int64, accepted bool, source string) error {
	s.contactConsent = accepted
	s.consentWrites = append(s.consentWrites, accepted)
	return nil
}
func (s *consultationStoreFixture) CreateLeadWithBusinessContact(in *model.CustomerLeadCreateInput) (int64, bool, error) {
	if s.contactConsent {
		in.Phone = "13800000000"
		in.PhoneSource = model.CustomerLeadPhoneSourceAccountBound
		in.PhoneConsentRevision = 1
	}
	id, err := s.CreateLead(in)
	return id, s.contactConsent, err
}
func (s *consultationStoreFixture) CreateEvent(in *model.CustomerServiceEventCreateInput) (int64, error) {
	s.eventCalls++
	s.lastEvent = in.EventType
	s.events = append(s.events, in.EventType)
	if in.EventType == "lead_submit" {
		s.submitAttempts = append(s.submitAttempts, in)
		if len(s.submitAttempts) <= s.submitFailures {
			return 0, errors.New("payload too large")
		}
	}
	if s.failEvent && in.EventType == "consultation_request_submitted" {
		return 0, errors.New("event unavailable")
	}
	return 82, nil
}

func TestBoundPhoneRequiresExplicitBusinessContactConsent(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name       string
		body       string
		stored     bool
		wantPhone  string
		wantWrites []bool
	}{
		{name: "no consent keeps bound phone private", body: `{"demand_summary":"test"}`},
		{name: "stored consent permits bound phone", body: `{"demand_summary":"test"}`, stored: true, wantPhone: "13800000000"},
		{name: "ordinary request cannot grant consent", body: `{"demand_summary":"test","contact_phone_consent":true}`},
		{name: "ordinary request cannot withdraw consent", body: `{"demand_summary":"test","contact_phone_consent":false}`, stored: true, wantPhone: "13800000000"},
		{name: "typed phone remains the current request", body: `{"demand_summary":"test","phone":"13900000000"}`, wantPhone: "13900000000"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := &consultationStoreFixture{contactConsent: tc.stored}
			router := gin.New()
			router.Use(func(c *gin.Context) { c.Set("code_session", &model.CodeSession{UserID: 41}); c.Next() })
			registerCustomerLeadRoutes(router.Group("/api"), store, nil, nil)
			req := httptest.NewRequest(http.MethodPost, "/api/customer-service/leads", strings.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)
			if rec.Code != http.StatusOK {
				t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
			}
			if store.lastLead == nil || store.lastLead.Phone != tc.wantPhone {
				t.Fatalf("lead phone=%q, want %q", store.lastLead.Phone, tc.wantPhone)
			}
			if len(store.consentWrites) != len(tc.wantWrites) {
				t.Fatalf("consent writes=%v, want %v", store.consentWrites, tc.wantWrites)
			}
			for i := range tc.wantWrites {
				if store.consentWrites[i] != tc.wantWrites[i] {
					t.Fatalf("consent writes=%v, want %v", store.consentWrites, tc.wantWrites)
				}
			}
		})
	}
}

func TestBusinessContactConsentCanBeReadAndWithdrawn(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := &consultationStoreFixture{contactConsent: true}
	router := gin.New()
	router.Use(func(c *gin.Context) { c.Set("code_session", &model.CodeSession{UserID: 41}); c.Next() })
	registerCustomerLeadRoutes(router.Group("/api"), store, nil, nil)

	get := httptest.NewRequest(http.MethodGet, "/api/customer-service/contact-consent", nil)
	getRec := httptest.NewRecorder()
	router.ServeHTTP(getRec, get)
	if getRec.Code != http.StatusOK || !strings.Contains(getRec.Body.String(), `"accepted":true`) {
		t.Fatalf("GET status=%d body=%s", getRec.Code, getRec.Body.String())
	}

	put := httptest.NewRequest(http.MethodPut, "/api/customer-service/contact-consent", strings.NewReader(`{"accepted":false,"purpose":"use_bound_phone_for_business_contact","version":"phone_contact_v1"}`))
	put.Header.Set("Content-Type", "application/json")
	putRec := httptest.NewRecorder()
	router.ServeHTTP(putRec, put)
	if putRec.Code != http.StatusOK || store.contactConsent || len(store.consentWrites) != 1 || store.consentWrites[0] {
		t.Fatalf("PUT status=%d consent=%v writes=%v body=%s", putRec.Code, store.contactConsent, store.consentWrites, putRec.Body.String())
	}

	for name, body := range map[string]string{
		"stale version": `{"accepted":true,"purpose":"use_bound_phone_for_business_contact","version":"phone_contact_v0"}`,
		"old client":    `{"accepted":true}`,
		"wrong purpose": `{"accepted":true,"purpose":"analytics","version":"phone_contact_v1"}`,
	} {
		t.Run(name, func(t *testing.T) {
			stale := httptest.NewRequest(http.MethodPut, "/api/customer-service/contact-consent", strings.NewReader(body))
			stale.Header.Set("Content-Type", "application/json")
			staleRec := httptest.NewRecorder()
			router.ServeHTTP(staleRec, stale)
			if staleRec.Code != http.StatusConflict || len(store.consentWrites) != 1 {
				t.Fatalf("stale PUT status=%d writes=%v body=%s", staleRec.Code, store.consentWrites, staleRec.Body.String())
			}
		})
	}
}

func TestBusinessContactConsentAndPhoneUseHaveLinearOrder(t *testing.T) {
	gin.SetMode(gin.TestMode)
	serve := func(store *linearizedConsentStore) *gin.Engine {
		router := gin.New()
		router.Use(func(c *gin.Context) { c.Set("code_session", &model.CodeSession{UserID: 41}); c.Next() })
		registerCustomerLeadRoutes(router.Group("/api"), store, nil, nil)
		return router
	}
	request := func(router *gin.Engine, method, path, body string) <-chan *httptest.ResponseRecorder {
		result := make(chan *httptest.ResponseRecorder, 1)
		go func() {
			req := httptest.NewRequest(method, path, strings.NewReader(body))
			if body != "" {
				req.Header.Set("Content-Type", "application/json")
			}
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)
			result <- rec
		}()
		return result
	}

	t.Run("phone write first then withdrawal", func(t *testing.T) {
		store := newLinearizedConsentStore(true)
		router := serve(store)
		leadResult := request(router, http.MethodPost, "/api/customer-service/leads", `{"demand_summary":"test"}`)
		<-store.createEntered
		withdrawResult := request(router, http.MethodPut, "/api/customer-service/contact-consent", `{"accepted":false,"purpose":"use_bound_phone_for_business_contact","version":"phone_contact_v1"}`)
		select {
		case <-store.setEntered:
			t.Fatal("withdrawal entered its critical section before the phone write released the consent lock")
		case <-time.After(20 * time.Millisecond):
		}
		close(store.createRelease)
		if rec := <-leadResult; rec.Code != http.StatusOK {
			t.Fatalf("lead status=%d body=%s", rec.Code, rec.Body.String())
		}
		<-store.setEntered
		close(store.setRelease)
		if rec := <-withdrawResult; rec.Code != http.StatusOK {
			t.Fatalf("withdraw status=%d body=%s", rec.Code, rec.Body.String())
		}
		if store.lastPhone != "13800000000" || store.accepted {
			t.Fatalf("phone=%q accepted=%v", store.lastPhone, store.accepted)
		}
	})

	t.Run("withdrawal first prevents later phone write", func(t *testing.T) {
		store := newLinearizedConsentStore(true)
		router := serve(store)
		withdrawResult := request(router, http.MethodPut, "/api/customer-service/contact-consent", `{"accepted":false,"purpose":"use_bound_phone_for_business_contact","version":"phone_contact_v1"}`)
		<-store.setEntered
		leadResult := request(router, http.MethodPost, "/api/customer-service/leads", `{"demand_summary":"test"}`)
		select {
		case <-store.createEntered:
			t.Fatal("phone write entered its critical section before withdrawal released the consent lock")
		case <-time.After(20 * time.Millisecond):
		}
		close(store.setRelease)
		if rec := <-withdrawResult; rec.Code != http.StatusOK {
			t.Fatalf("withdraw status=%d body=%s", rec.Code, rec.Body.String())
		}
		<-store.createEntered
		close(store.createRelease)
		if rec := <-leadResult; rec.Code != http.StatusOK {
			t.Fatalf("lead status=%d body=%s", rec.Code, rec.Body.String())
		}
		if store.lastPhone != "" || store.accepted {
			t.Fatalf("phone=%q accepted=%v", store.lastPhone, store.accepted)
		}
	})
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
		{"generic client event never high", "events", `{"event_type":"lead_submit","lead_id":73}`, "opaque", 41, false, false, 400},
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
			}, nil)
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
	registerCustomerLeadRoutes(router.Group("/api"), &consultationStoreFixture{}, func(context.Context, string, int64, int64) error { close(entered); <-release; return nil }, nil)
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

func TestContentConsultationRequiresPublishedVersionAndStoresOnlySafeReference(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name, body string
		valid      bool
		status     int
	}{
		{
			name:   "published reference",
			body:   `{"source":"content_reader","demand_summary":"test","payload":{"reason":"handoff_human","consultation_plan":{"visible":true,"contentRef":{"contentId":"content-0001","version":7},"body":"private body","share_token":"secret","visitor_key":"visitor"}}}`,
			valid:  true,
			status: http.StatusOK,
		},
		{
			name:   "unpublished reference",
			body:   `{"source":"content_reader","demand_summary":"test","payload":{"consultation_plan":{"contentRef":{"contentId":"content-0001","version":8}}}}`,
			status: http.StatusBadRequest,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := &consultationStoreFixture{}
			router := gin.New()
			router.Use(func(c *gin.Context) { c.Set("code_session", &model.CodeSession{UserID: 41}); c.Next() })
			registerCustomerLeadRoutes(router.Group("/api"), store, nil, nil, func(_ context.Context, id string, version int) error {
				if tc.valid && id == "content-0001" && version == 7 {
					return nil
				}
				return errors.New("not published")
			})
			req := httptest.NewRequest(http.MethodPost, "/api/customer-service/consultation-requests", strings.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)
			if rec.Code != tc.status {
				t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
			}
			if !tc.valid {
				if store.leadCalls != 0 {
					t.Fatal("invalid reference created a lead")
				}
				return
			}
			plan := store.submitAttempts[0].Payload["consultation_plan"].(map[string]interface{})
			if plan["body"] != nil || plan["share_token"] != nil || plan["visitor_key"] != nil {
				t.Fatalf("unsafe consultation fields persisted: %#v", plan)
			}
			ref := plan["contentRef"].(map[string]interface{})
			if ref["contentId"] != "content-0001" || ref["version"] != 7 {
				t.Fatalf("wrong safe reference: %#v", ref)
			}
		})
	}
}

// lead_submit / consultation_request_submitted 只由服务端在真实留资后写入；
// 通用事件接口若放行，任何登录用户都能把任意线索顶到列表最前、虚增"提交线索"。
func TestGenericEventsRejectServerOnlyTypes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		eventType string
		status    int
		stored    bool
	}{
		{"lead_submit", http.StatusBadRequest, false},
		{"consultation_request_submitted", http.StatusBadRequest, false},
		{model.BusinessContactConsentGranted, http.StatusBadRequest, false},
		{model.BusinessContactConsentWithdrawn, http.StatusBadRequest, false},
		{"welcome_shown", http.StatusOK, true},
	} {
		t.Run(tc.eventType, func(t *testing.T) {
			store := &consultationStoreFixture{}
			router := gin.New()
			router.Use(func(c *gin.Context) { c.Set("code_session", &model.CodeSession{UserID: 41}); c.Next() })
			registerCustomerLeadRoutes(router.Group("/api"), store, nil, nil)
			req := httptest.NewRequest(http.MethodPost, "/api/customer-service/events", strings.NewReader(`{"event_type":"`+tc.eventType+`","lead_id":73}`))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)
			if rec.Code != tc.status || (store.eventCalls > 0) != tc.stored {
				t.Fatalf("status=%d events=%d, want status=%d stored=%v", rec.Code, store.eventCalls, tc.status, tc.stored)
			}
		})
	}
}

// lead_submit 是"提交线索"统计与列表排序的依据，不能在写入失败时静默返回成功：
// 先去掉 payload 与需求摘要重试一次（超长 payload 最常见），仍失败则返回 500 让客户端重试（同会话重试幂等）。
func TestLeadSubmitEventFailureIsNotSilent(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name     string
		failures int
		status   int
		attempts int
	}{
		{"first write ok", 0, http.StatusOK, 1},
		{"retry without payload succeeds", 1, http.StatusOK, 2},
		{"retry also fails", 2, http.StatusInternalServerError, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := &consultationStoreFixture{submitFailures: tc.failures}
			router := gin.New()
			router.Use(func(c *gin.Context) { c.Set("code_session", &model.CodeSession{UserID: 41}); c.Next() })
			registerCustomerLeadRoutes(router.Group("/api"), store, nil, nil)
			req := httptest.NewRequest(http.MethodPost, "/api/customer-service/leads", strings.NewReader(`{"demand_summary":"三层 新闽派","payload":{"reason":"structured_requirement"}}`))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)
			if rec.Code != tc.status || len(store.submitAttempts) != tc.attempts {
				t.Fatalf("status=%d attempts=%d, want status=%d attempts=%d", rec.Code, len(store.submitAttempts), tc.status, tc.attempts)
			}
			if tc.attempts == 2 {
				retry := store.submitAttempts[1]
				if retry.Payload != nil || retry.DemandSummary != "" || retry.LeadID != 73 || retry.UserID != 41 {
					t.Fatalf("retry should be minimal but keep lead/user, got %+v", retry)
				}
			}
		})
	}
}
