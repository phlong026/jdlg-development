package route

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"service/model"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

type editHandlerStore struct {
	current       model.Template
	reads, writes int
	readErrAt     int
	writeErr      error
	onWrite       func()
	before, after *model.Template
	patch         model.TemplateUpdate
}

func (s *editHandlerStore) GetByID(int64) (*model.Template, error) {
	s.reads++
	if s.reads == s.readErrAt {
		return nil, errors.New("read unavailable")
	}
	copy := s.current
	return &copy, nil
}
func (s *editHandlerStore) UpdatePartial(before, after *model.Template, patch model.TemplateUpdate) error {
	s.writes++
	s.before = before
	s.after = after
	s.patch = patch
	if s.writeErr != nil {
		return s.writeErr
	}
	if s.onWrite != nil {
		s.onWrite()
	}
	// Fake persistence boundary; selective SQL/CAS is tested against the real
	// TemplateModel with a database/sql driver in model/template_update_test.go.
	if err := patch.Apply(&s.current); err != nil {
		return err
	}
	if patch.ImagesChanged(before) {
		s.current.Thumbnail = after.Thumbnail
		s.current.PreviewURL = after.PreviewURL
		s.current.Images = after.Images
		s.current.ImageWidth = after.ImageWidth
		s.current.ImageHeight = after.ImageHeight
	}
	return nil
}

type editHandlerFixture struct {
	store                        *editHandlerStore
	tabs, image, derived         int
	tabErr, imageErr, derivedErr error
}

func handlerFixture() *editHandlerFixture {
	return &editHandlerFixture{store: &editHandlerStore{current: model.Template{ID: 7, Name: "Existing", Category: "villa", Description: "Description", MainTab: "main", SubTab: "sub", ThirdTab: "third", Thumbnail: "/thumb.jpg", PreviewURL: "/preview.jpg", Images: `["/original.jpg"]`, Price: 10, IsFree: true, Status: "rejected", PublishScope: "homepage_only", RejectReason: "Needs revision", SourceType: "album_upload", InternalPrompt: "hidden"}}}
}
func (f *editHandlerFixture) request(body string) *httptest.ResponseRecorder {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.PUT("/templates/:id", newTemplateUpdateHandler(f.store,
		func(*model.Template) error { f.tabs++; return f.tabErr },
		func(template *model.Template) error {
			f.image++
			template.ImageWidth = 800
			template.ImageHeight = 1000
			return f.imageErr
		},
		func(template *model.Template) error {
			f.derived++
			template.Thumbnail = "/derived.jpg"
			template.PreviewURL = "/derived-preview.jpg"
			return f.derivedErr
		},
	))
	response := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/templates/7", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(response, req)
	return response
}
func assertHTTP(t *testing.T, response *httptest.ResponseRecorder, code int) {
	t.Helper()
	if response.Code != code {
		t.Fatalf("expected HTTP %d, got %d: %s", code, response.Code, response.Body.String())
	}
}

func TestTemplateEditHandlerNameOnlyPreservesCurrentMetadataAndReturnsReadback(t *testing.T) {
	f := handlerFixture()
	f.store.onWrite = func() {
		f.store.current.PublishScope = "square"
		f.store.current.RejectReason = "New review"
		f.store.current.SourceType = "ai_generated"
		f.store.current.Status = "pending"
	}
	r := f.request(`{"name":"Renamed"}`)
	assertHTTP(t, r, 200)
	if f.store.patch.PublishScope != nil || f.store.patch.RejectReason != nil || f.store.patch.SourceType != nil || f.store.patch.Status != nil || f.store.patch.Images != nil {
		t.Fatal("handler invented fields")
	}
	var result struct {
		Data model.Template `json:"data"`
	}
	if err := json.Unmarshal(r.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Data.Name != "Renamed" || result.Data.PublishScope != "square" || result.Data.RejectReason != "New review" || result.Data.SourceType != "ai_generated" || result.Data.Status != "pending" {
		t.Fatalf("returned stale data: %+v", result.Data)
	}
	if strings.Contains(r.Body.String(), "hidden") {
		t.Fatal("internal prompt exposed")
	}
	if f.tabs != 1 || f.image != 1 || f.derived != 0 || f.store.reads != 2 {
		t.Fatalf("validation/readback mismatch: %+v", f)
	}
}

func TestTemplateEditHandlerPreservesAllExistingValidationBeforeWriting(t *testing.T) {
	for _, body := range []string{`{"name":"Renamed"}`, `{"status":"published"}`, `{"source_type":"admin_upload"}`, `{"publish_scope":"square"}`, `{"third_tab":"new-third"}`} {
		t.Run(body, func(t *testing.T) {
			for _, stage := range []string{"tabs", "image"} {
				t.Run(stage, func(t *testing.T) {
					f := handlerFixture()
					if stage == "tabs" {
						f.tabErr = errors.New("invalid tab")
					} else {
						f.imageErr = errors.New("invalid image")
					}
					r := f.request(body)
					assertHTTP(t, r, 400)
					if f.store.writes != 0 || f.derived != 0 {
						t.Fatal("validation failure wrote or generated")
					}
				})
			}
		})
	}
}

func TestTemplateEditHandlerImageChangePreparesOnlyChangedImageGroup(t *testing.T) {
	f := handlerFixture()
	r := f.request(`{"images":"[\"/new.jpg\"]","thumbnail":"/new.jpg","preview_url":"/new.jpg"}`)
	assertHTTP(t, r, 200)
	if f.derived != 1 || f.store.after.Thumbnail != "/derived.jpg" || f.store.after.ImageWidth != 800 {
		t.Fatal("derived image fields did not reach persistence")
	}
	if f.store.current.SourceType != "album_upload" || f.store.current.RejectReason != "Needs revision" || f.store.current.PublishScope != "homepage_only" {
		t.Fatal("image edit changed metadata")
	}
	f = handlerFixture()
	f.derivedErr = errors.New("fake generator failed")
	r = f.request(`{"images":"[\"/new.jpg\"]"}`)
	assertHTTP(t, r, 500)
	if f.store.writes != 0 {
		t.Fatal("derivation failure wrote to DB")
	}
}

func TestTemplateEditHandlerNullAndInvalidInputsFailBeforeWrite(t *testing.T) {
	for _, body := range []string{`{"name":null}`, `{"status":null}`, `{"is_free":null}`, `{"price":"zero"}`, `{"name":"  "}`, `{"category":""}`, `null`, `[]`, `{`} {
		t.Run(body, func(t *testing.T) {
			f := handlerFixture()
			assertHTTP(t, f.request(body), 400)
			if f.store.writes != 0 {
				t.Fatal("invalid input wrote")
			}
		})
	}
}

func TestTemplateEditHandlerConflictAndDBErrorsDoNotPretendSuccess(t *testing.T) {
	for _, fixture := range []struct {
		err    error
		status int
	}{{model.ErrTemplateUpdateConflict, 409}, {errors.New("DB unavailable"), 500}} {
		f := handlerFixture()
		f.store.writeErr = fixture.err
		assertHTTP(t, f.request(`{"name":"Renamed"}`), fixture.status)
		if f.store.current.Name != "Existing" || f.store.reads != 1 {
			t.Fatal("failed write changed data or reread")
		}
	}
}

func TestTemplateEditHandlerCommittedWriteWithReadbackFailureStaysSuccessful(t *testing.T) {
	f := handlerFixture()
	f.store.readErrAt = 2
	r := f.request(`{"name":"Renamed"}`)
	assertHTTP(t, r, 200)
	var response struct {
		Code    int             `json:"code"`
		Data    *model.Template `json:"data"`
		Refresh bool            `json:"refresh_required"`
	}
	if err := json.Unmarshal(r.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Code != 0 || !response.Refresh || response.Data != nil || f.store.current.Name != "Renamed" || f.store.writes != 1 {
		t.Fatalf("committed state misreported: %s", r.Body.String())
	}
}

func TestTemplateEditHandlerExplicitEmptyFalseAndZeroSurviveBinding(t *testing.T) {
	f := handlerFixture()
	r := f.request(`{"description":"","main_tab":"","sub_tab":"","third_tab":"","price":0,"is_free":false}`)
	assertHTTP(t, r, 200)
	if f.store.current.Description != "" || f.store.current.MainTab != "" || f.store.current.Price != 0 || f.store.current.IsFree {
		t.Fatal("explicit empty values were dropped")
	}
}

func TestTemplateEditHandlerNoopStillReturnsCurrentRecord(t *testing.T) {
	f := handlerFixture()
	r := f.request(`{}`)
	assertHTTP(t, r, 200)
	if f.store.current.Name != "Existing" || f.derived != 0 || f.store.reads != 2 {
		t.Fatal("no-op changed record")
	}
}
