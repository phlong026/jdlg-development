package contentimport

import (
	"context"
	"errors"
	"service/tracking"
	"testing"
	"time"
)

type workerMemoryStore struct {
	JobStore
	job      Job
	order    []string
	finished string
}

func (m *workerMemoryStore) Claim(context.Context, time.Time, time.Duration) (Job, error) {
	return m.job, nil
}
func (m *workerMemoryStore) BeginResolve(context.Context, string, string) error {
	m.order = append(m.order, "request_sent")
	return nil
}
func (m *workerMemoryStore) SaveResolution(_ context.Context, _, _ string, d Document) error {
	m.order = append(m.order, "resolution_saved")
	m.job.Document = &d
	return nil
}
func (m *workerMemoryStore) Renew(context.Context, string, string, time.Time, time.Duration) error {
	return nil
}
func (m *workerMemoryStore) Finish(_ context.Context, _, _, status, contentID, code string) error {
	m.finished = status
	m.order = append(m.order, "finish:"+status)
	return nil
}

type documentResolverFunc func(context.Context, string) (Document, error)

func (f documentResolverFunc) ResolveDocument(c context.Context, u string) (Document, error) {
	return f(c, u)
}
func workerFixture() (*Worker, *workerMemoryStore) {
	m := &workerMemoryStore{job: Job{ID: "import-job-0001", Actor: 7, Owner: "one", Platform: Douyin, URL: "https://v.douyin.com/sample/", LeaseToken: "lease-one"}}
	d := Document{Source: Source{Title: "原视频", Media: []RemoteMedia{{ID: "source-one", Kind: "video", URL: "https://cdn.example/media.mp4"}}}, Body: tracking.ArticleDocument{Schema: 1, Blocks: []tracking.ArticleBlock{{Kind: "video", MediaID: "remote-0000"}}}}
	w := &Worker{Store: m, LeaseDuration: time.Minute, PollInterval: time.Millisecond, Authorize: func(context.Context, Job) error { return nil }, Resolvers: map[Platform]DocumentResolver{Douyin: documentResolverFunc(func(context.Context, string) (Document, error) { m.order = append(m.order, "resolve"); return d, nil })}, SaveMedia: func(context.Context, Job, int, RemoteMedia) (tracking.Asset, error) {
		m.order = append(m.order, "media")
		return tracking.Asset{ID: "saved-media-0001", Kind: "video"}, nil
	}, SaveDraft: func(_ context.Context, _ Job, doc Document, assets []tracking.Asset) (string, error) {
		m.order = append(m.order, "draft")
		return "saved-content-0001", nil
	}}
	return w, m
}
func TestWorkerPersistsBeforeExternalCallAndNeverPublishes(t *testing.T) {
	w, m := workerFixture()
	worked, err := w.RunOnce(context.Background())
	if !worked || err != nil {
		t.Fatal(worked, err)
	}
	if m.finished != Ready {
		t.Fatal(m.finished)
	}
	want := []string{"request_sent", "resolve", "resolution_saved", "media", "draft", "finish:ready"}
	if len(m.order) != len(want) {
		t.Fatal(m.order)
	}
	for i := range want {
		if m.order[i] != want[i] {
			t.Fatal(m.order)
		}
	}
}
func TestWorkerRestartsFromResolutionWithoutChargingParserAgain(t *testing.T) {
	w, m := workerFixture()
	d, _ := w.Resolvers[Douyin].ResolveDocument(context.Background(), m.job.URL)
	m.job.Document = &d
	m.order = nil
	w.Resolvers = nil
	if _, err := w.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if m.finished != Ready || m.order[0] != "media" {
		t.Fatal(m.order)
	}
}
func TestWorkerUncertainAndMissingMediaCannotBecomeReady(t *testing.T) {
	for _, kind := range []string{"uncertain", "missing"} {
		t.Run(kind, func(t *testing.T) {
			w, m := workerFixture()
			if kind == "uncertain" {
				w.Resolvers[Douyin] = documentResolverFunc(func(context.Context, string) (Document, error) {
					return Document{}, &ResolveError{Code: "provider_unavailable", Uncertain: true}
				})
			} else {
				w.SaveMedia = func(context.Context, Job, int, RemoteMedia) (tracking.Asset, error) {
					return tracking.Asset{}, errors.New("download failed")
				}
			}
			_, _ = w.RunOnce(context.Background())
			if m.finished == Ready {
				t.Fatal("incomplete import succeeded")
			}
			for _, op := range m.order {
				if op == "draft" {
					t.Fatal("saved incomplete draft")
				}
			}
		})
	}
}
func TestWorkerPermissionRevocationBlocksFinalDraft(t *testing.T) {
	w, m := workerFixture()
	n := 0
	w.Authorize = func(context.Context, Job) error {
		n++
		if n > 1 {
			return errors.New("revoked")
		}
		return nil
	}
	_, _ = w.RunOnce(context.Background())
	if m.finished != Blocked {
		t.Fatal(m.finished)
	}
	for _, op := range m.order {
		if op == "draft" {
			t.Fatal("revoked writer saved draft")
		}
	}
}
