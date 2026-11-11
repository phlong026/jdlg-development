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
	job              Job
	order            []string
	finished         string
	renewed          int
	renewedSignal    chan struct{}
	renew            func(context.Context) error
	finishContextErr error
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

func (m *workerMemoryStore) Renew(ctx context.Context, _ string, _ string, _ time.Time, _ time.Duration) error {
	m.renewed++
	if m.renewedSignal != nil {
		select {
		case m.renewedSignal <- struct{}{}:
		default:
		}
	}
	if m.renew != nil {
		return m.renew(ctx)
	}
	return nil
}
func (m *workerMemoryStore) Finish(ctx context.Context, _, _, status, contentID, code string) error {
	m.finishContextErr = ctx.Err()
	m.finished = status
	m.order = append(m.order, "finish:"+status)
	return nil
}

func TestWorkerRenewsLeaseDuringLongMediaSave(t *testing.T) {
	w, m := workerFixture()
	w.LeaseDuration = 60 * time.Millisecond
	m.renewedSignal = make(chan struct{}, 3)
	d, _ := w.Resolvers[Douyin].ResolveDocument(context.Background(), m.job.URL)
	m.job.Document = &d
	m.order = nil
	w.Resolvers = nil
	release := make(chan struct{})
	w.SaveMedia = func(ctx context.Context, _ Job, _ int, _ RemoteMedia) (tracking.Asset, error) {
		select {
		case <-release:
			return tracking.Asset{ID: "saved-media-0001", Kind: "video"}, nil
		case <-ctx.Done():
			return tracking.Asset{}, ctx.Err()
		}
	}
	done := make(chan error, 1)
	go func() { _, err := w.RunOnce(context.Background()); done <- err }()
	for i := 0; i < 3; i++ {
		select {
		case <-m.renewedSignal:
		case <-time.After(time.Second):
			t.Fatal("lease renewal timed out")
		}
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	// One renewal happens before SaveMedia and at least two happen while it is
	// blocked, so another worker cannot reclaim a large in-flight download.
	if m.renewed < 3 {
		t.Fatalf("renewals=%d", m.renewed)
	}
}

func TestWorkerFinishesWithBoundedContextAfterOperationDeadline(t *testing.T) {
	w, m := workerFixture()
	d, _ := w.Resolvers[Douyin].ResolveDocument(context.Background(), m.job.URL)
	m.job.Document = &d
	w.Resolvers = nil
	w.SaveMedia = func(ctx context.Context, _ Job, _ int, _ RemoteMedia) (tracking.Asset, error) {
		return tracking.Asset{}, ctx.Err()
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	worked, err := w.RunOnce(ctx)
	if !worked || err != nil {
		t.Fatalf("worked=%v err=%v", worked, err)
	}
	if m.finished != Failed || m.finishContextErr != nil {
		t.Fatalf("status=%q finish_context_err=%v", m.finished, m.finishContextErr)
	}
}

func TestWorkerBoundsLeaseRenewalAndCancelsMediaSave(t *testing.T) {
	w, m := workerFixture()
	w.LeaseDuration = 60 * time.Millisecond
	w.leaseRenewTimeout = 30 * time.Millisecond
	d, _ := w.Resolvers[Douyin].ResolveDocument(context.Background(), m.job.URL)
	m.job.Document = &d
	w.Resolvers = nil
	m.renew = func(ctx context.Context) error {
		if m.renewed == 1 {
			return nil
		}
		<-ctx.Done()
		return ctx.Err()
	}
	w.SaveMedia = func(ctx context.Context, _ Job, _ int, _ RemoteMedia) (tracking.Asset, error) {
		<-ctx.Done()
		return tracking.Asset{}, ctx.Err()
	}
	started := time.Now()
	worked, err := w.RunOnce(context.Background())
	if !worked || err != nil || m.finished != Failed {
		t.Fatalf("worked=%v err=%v status=%q", worked, err, m.finished)
	}
	if elapsed := time.Since(started); elapsed > 300*time.Millisecond {
		t.Fatalf("blocked renewal took %v", elapsed)
	}
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

type sourceResolverFunc func(context.Context, string) (Source, error)

func (f sourceResolverFunc) Resolve(ctx context.Context, url string) (Source, error) {
	return f(ctx, url)
}
func TestGalleryDocumentKeepsFullTextAndMediaOrderWithoutDuplicatingCover(t *testing.T) {
	r := VideoDocumentResolver{Resolver: sourceResolverFunc(func(context.Context, string) (Source, error) {
		return Source{Platform: Xiaohongshu, ID: "0123456789abcdef01234567", Title: "图文", Text: "第一段完整正文\n第二段完整正文", Media: []RemoteMedia{{ID: "first", Kind: "image", URL: "https://a.xhscdn.com/first.jpg"}, {ID: "second", Kind: "image", URL: "https://a.xhscdn.com/second.jpg"}}, CoverURL: "https://a.xhscdn.com/first.jpg"}, nil
	})}
	doc, err := r.ResolveDocument(context.Background(), "https://www.xiaohongshu.com/explore/0123456789abcdef01234567")
	if err != nil || len(doc.Body.Blocks) != 4 || len(doc.Source.Media) != 2 || doc.CoverRef != "remote-0000" || doc.Body.Blocks[0].Text != "第一段完整正文" || doc.Body.Blocks[1].Text != "第二段完整正文" || doc.Body.Blocks[3].MediaID != "remote-0001" {
		t.Fatalf("doc=%+v err=%v", doc, err)
	}
}
