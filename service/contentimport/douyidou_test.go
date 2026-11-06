package contentimport

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

type failingReadCloser struct{}

func (failingReadCloser) Read([]byte) (int, error) { return 0, errors.New("body interrupted") }
func (failingReadCloser) Close() error             { return nil }

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) { return f(request) }

func response(status int, body string, headers ...http.Header) *http.Response {
	header := make(http.Header)
	if len(headers) > 0 {
		header = headers[0]
	}
	return &http.Response{
		StatusCode: status,
		Header:     header,
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func TestDouyidouSignatureFixedVector(t *testing.T) {
	query, signature := douyidouSignature(
		"demo-app",
		"demo-secret",
		"https://www.douyin.com/video/1234567890123456789",
	)
	if query != "app_id=demo-app&url=https%3A%2F%2Fwww.douyin.com%2Fvideo%2F1234567890123456789" {
		t.Fatalf("query = %q", query)
	}
	if signature != "c043584b2f7c10e1b3f482691dfc752a" {
		t.Fatalf("signature = %q", signature)
	}
}

func TestDouyidouResolveHandlesProviderFieldVariants(t *testing.T) {
	var captured *http.Request
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		captured = request.Clone(request.Context())
		return response(http.StatusOK, `{
            "code": 0,
            "data": {
                "note_id": "0123456789ABCDEF01234567",
                "text": "这是一条小红书视频文案",
                "author": {"name": "作者"},
                "cover": "https://img.example/cover.jpg",
                "video": [{"play_url": "https://video.example/item.mp4"}],
                "other": {"duration": "12001"}
            }
        }`), nil
	})}
	resolver := NewDouyidouResolver(DouyidouConfig{AppID: "demo-app", AppSecret: "demo-secret", HTTPClient: client})

	source, err := resolver.Resolve(context.Background(), "https://www.xiaohongshu.com/explore/0123456789abcdef01234567?xsec_token=abc")
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if captured.URL.Host != "gateway.diadi.cn" || captured.URL.Path != "/api/parse" {
		t.Fatalf("request target = %s", captured.URL)
	}
	if captured.Header.Get("Sign") == "" {
		t.Fatal("Sign header is empty")
	}
	if source.Platform != Xiaohongshu || source.ID != "0123456789abcdef01234567" {
		t.Fatalf("source identity = %#v", source)
	}
	if source.Title != "这是一条小红书视频文案" || source.Author != "作者" {
		t.Fatalf("source metadata = %#v", source)
	}
	if len(source.Media) != 1 || source.Media[0].URL != "https://video.example/item.mp4" || source.Media[0].DurationMS != 12001 {
		t.Fatalf("source media = %#v", source.Media)
	}
	if !source.WatermarkExtraction {
		t.Fatal("provider extraction intent was not recorded")
	}
}

func TestDouyidouRejectsUnsupportedPlatformAndMissingVideo(t *testing.T) {
	called := false
	resolver := NewDouyidouResolver(DouyidouConfig{
		AppID: "app", AppSecret: "secret",
		HTTPClient: &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			called = true
			return response(http.StatusOK, `{"code":0,"data":{"aweme_id":"1234567890123456789","video":[]}}`), nil
		})},
	})
	if _, err := resolver.Resolve(context.Background(), "https://channels.weixin.qq.com/s/example"); errorCode(err) != "unsupported_platform" {
		t.Fatalf("channels error code = %q", errorCode(err))
	}
	if called {
		t.Fatal("provider called for unsupported platform")
	}
	if _, err := resolver.Resolve(context.Background(), "https://www.douyin.com/video/1234567890123456789"); errorCode(err) != "submission_uncertain" {
		t.Fatalf("missing video code = %q", errorCode(err))
	}
}

func TestDouyidouRequiresNativePlatformID(t *testing.T) {
	resolver := NewDouyidouResolver(DouyidouConfig{
		AppID: "app", AppSecret: "secret",
		HTTPClient: &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			return response(http.StatusOK, `{"code":0,"data":{"aweme_id":"not-native","video":["https://video.example/item.mp4"]}}`), nil
		})},
	})
	if _, err := resolver.Resolve(context.Background(), "https://www.douyin.com/video/1234567890123456789"); errorCode(err) != "submission_uncertain" {
		t.Fatalf("error code = %q", errorCode(err))
	}
}

func TestDouyidouRejectsPrivateProviderMediaURL(t *testing.T) {
	resolver := NewDouyidouResolver(DouyidouConfig{
		AppID: "app", AppSecret: "secret",
		HTTPClient: &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			return response(http.StatusOK, `{"code":0,"data":{"aweme_id":"1234567890123456789","video":["http://127.0.0.1/internal.mp4"]}}`), nil
		})},
	})
	if _, err := resolver.Resolve(context.Background(), "https://www.douyin.com/video/1234567890123456789"); errorCode(err) != "submission_uncertain" {
		t.Fatalf("error code = %q", errorCode(err))
	}
}

func TestDouyidouTreatsUnreadableOrMalformedSuccessAsUncertain(t *testing.T) {
	tests := []struct {
		name     string
		response func() *http.Response
	}{
		{name: "interrupted body", response: func() *http.Response {
			return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: failingReadCloser{}}
		}},
		{name: "truncated json", response: func() *http.Response { return response(http.StatusOK, `{"code":0,"data":`) }},
		{name: "malformed success", response: func() *http.Response {
			return response(http.StatusOK, `{"code":0,"data":{"aweme_id":"1234567890123456789"}}`)
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resolver := NewDouyidouResolver(DouyidouConfig{AppID: "app", AppSecret: "secret", HTTPClient: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) { return tt.response(), nil })}})
			_, err := resolver.Resolve(context.Background(), "https://www.douyin.com/video/1234567890123456789")
			resolved, ok := err.(*ResolveError)
			if !ok || resolved.Code != "submission_uncertain" || !resolved.Uncertain || resolved.Retryable {
				t.Fatalf("Resolve() error = %#v", err)
			}
		})
	}
}

func TestDouyidouExplicitBusinessFailureRemainsTerminal(t *testing.T) {
	resolver := NewDouyidouResolver(DouyidouConfig{AppID: "app", AppSecret: "secret", HTTPClient: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return response(http.StatusOK, `{"code":422,"msg":"视频已下架"}`), nil
	})}})
	_, err := resolver.Resolve(context.Background(), "https://www.douyin.com/video/1234567890123456789")
	if resolved, ok := err.(*ResolveError); !ok || resolved.Code != "source_unavailable" || resolved.Uncertain {
		t.Fatalf("Resolve() error = %#v", err)
	}
}

func TestDouyidouCancellationAfterRequestStartsIsUncertain(t *testing.T) {
	resolver := NewDouyidouResolver(DouyidouConfig{AppID: "app", AppSecret: "secret", HTTPClient: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, context.Canceled
	})}})
	_, err := resolver.Resolve(context.Background(), "https://www.douyin.com/video/1234567890123456789")
	if resolved, ok := err.(*ResolveError); !ok || resolved.Code != "submission_uncertain" || !resolved.Uncertain {
		t.Fatalf("Resolve() error = %#v", err)
	}
}

func TestDouyidouTimeoutAndServerFailureAreUncertainWithoutRetry(t *testing.T) {
	tests := []struct {
		name      string
		transport roundTripFunc
	}{
		{
			name: "timeout",
			transport: func(request *http.Request) (*http.Response, error) {
				<-request.Context().Done()
				return nil, request.Context().Err()
			},
		},
		{
			name: "server failure",
			transport: func(request *http.Request) (*http.Response, error) {
				return response(http.StatusBadGateway, `{}`), nil
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resolver := NewDouyidouResolver(DouyidouConfig{AppID: "app", AppSecret: "secret", HTTPClient: &http.Client{Transport: tt.transport}})
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
			defer cancel()
			_, err := resolver.Resolve(ctx, "https://www.douyin.com/video/1234567890123456789")
			resolved, ok := err.(*ResolveError)
			if !ok || resolved.Code != "submission_uncertain" || !resolved.Uncertain || resolved.Retryable {
				t.Fatalf("Resolve() error = %#v", err)
			}
		})
	}
}

func TestDouyidouCapsResponseAndDoesNotFollowRedirects(t *testing.T) {
	var mu sync.Mutex
	calls := 0
	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		mu.Lock()
		calls++
		mu.Unlock()
		if request.URL.Host == "gateway.diadi.cn" && request.URL.Path == "/api/parse" {
			headers := make(http.Header)
			headers.Set("Location", "https://evil.example/steal")
			return response(http.StatusFound, "", headers), nil
		}
		return nil, errors.New("unexpected redirect follow")
	})
	resolver := NewDouyidouResolver(DouyidouConfig{AppID: "app", AppSecret: "secret", HTTPClient: &http.Client{Transport: transport}})
	if _, err := resolver.Resolve(context.Background(), "https://www.douyin.com/video/1234567890123456789"); errorCode(err) != "provider_failed" {
		t.Fatalf("redirect error code = %q", errorCode(err))
	}
	mu.Lock()
	defer mu.Unlock()
	if calls != 1 {
		t.Fatalf("transport calls = %d, want 1", calls)
	}

	large := strings.Repeat("x", maxProviderResponseBytes+1)
	resolver = NewDouyidouResolver(DouyidouConfig{
		AppID: "app", AppSecret: "secret",
		HTTPClient: &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			return response(http.StatusOK, large), nil
		})},
	})
	if _, err := resolver.Resolve(context.Background(), "https://www.douyin.com/video/1234567890123456789"); errorCode(err) != "submission_uncertain" {
		t.Fatalf("body cap error code = %q", errorCode(err))
	}
}

// B18-B：快手白名单与图集 images 消费（官方契约字段）。
func TestDouyidouKuaishouVideoAndGallery(t *testing.T) {
	videoClient := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		return response(http.StatusOK, `{"code":0,"data":{"photo_id":"3xbdf9q9","title":"快手现场记录","text":"三层封顶","cover":"https://cover.example.com/c.jpg","video":["https://v.example.com/k.mp4"],"other":{"duration":12}}}`), nil
	})}
	resolver := NewDouyidouResolver(DouyidouConfig{AppID: "app", AppSecret: "secret", HTTPClient: videoClient})
	source, err := resolver.Resolve(context.Background(), "https://www.kuaishou.com/short-video/3xbdf9q9")
	if err != nil {
		t.Fatalf("快手视频应可解析 err=%v", err)
	}
	if source.Platform != Kuaishou || source.ID != "3xbdf9q9" || len(source.Media) != 1 || source.Media[0].Kind != "video" {
		t.Fatalf("source=%#v", source)
	}

	galleryClient := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		return response(http.StatusOK, `{"code":0,"data":{"photo_id":"gal0001","type":"note","title":"快手图集","text":"外观记录","images":["https://img.example.com/1.jpg","https://img.example.com/1.jpg","https://img.example.com/2.jpg"]}}`), nil
	})}
	galleryResolver := NewDouyidouResolver(DouyidouConfig{AppID: "app", AppSecret: "secret", HTTPClient: galleryClient})
	source, err = galleryResolver.Resolve(context.Background(), "https://www.kuaishou.com/short-video/gal0001")
	if err != nil {
		t.Fatalf("快手图集应可解析 err=%v", err)
	}
	if len(source.Media) != 2 || source.Media[0].Kind != "image" || source.Media[1].Kind != "image" {
		t.Fatalf("图集应产出两个图片媒体并去重 got=%#v", source.Media)
	}
	if source.Media[0].ID != "gal0001:image:0" {
		t.Fatalf("图集媒体 ID 应带序号 got=%s", source.Media[0].ID)
	}
}

func TestDouyidouDouyinGalleryConsumesImages(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		return response(http.StatusOK, `{"code":0,"data":{"aweme_id":"7419320667310492928","title":"抖音图文","text":"改造前后","images":["https://img.example.com/a.jpg","https://img.example.com/b.jpg"]}}`), nil
	})}
	resolver := NewDouyidouResolver(DouyidouConfig{AppID: "app", AppSecret: "secret", HTTPClient: client})
	source, err := resolver.Resolve(context.Background(), "https://www.douyin.com/note/7419320667310492928")
	if err != nil {
		t.Fatalf("抖音图集应可解析 err=%v", err)
	}
	if len(source.Media) != 2 || source.Media[0].Kind != "image" {
		t.Fatalf("got=%#v", source.Media)
	}
	if source.Text != "改造前后" || source.Media[0].Caption != "" || source.Media[1].Caption != "" {
		t.Fatalf("整条文案应独立保存，不逐图重复: %+v", source)
	}
}

func TestDouyidouLongCaptionBecomesOneBodyParagraph(t *testing.T) {
	text := strings.Repeat("乡村住宅完整设计说明。", 90)
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		return response(http.StatusOK, `{"code":0,"data":{"aweme_id":"7419320667310492928","title":"方案介绍","text":"`+text+`","video":["https://video.example/item.mp4"]}}`), nil
	})}
	r := VideoDocumentResolver{Resolver: NewDouyidouResolver(DouyidouConfig{AppID: "app", AppSecret: "secret", HTTPClient: client})}
	doc, err := r.ResolveDocument(context.Background(), "https://www.douyin.com/video/7419320667310492928")
	if err != nil || len(doc.Body.Blocks) != 2 || doc.Body.Blocks[0].Text != text || doc.Body.Blocks[1].Caption != "" {
		t.Fatalf("长文案未保留为独立正文: %+v err=%v", doc.Body.Blocks, err)
	}
}
