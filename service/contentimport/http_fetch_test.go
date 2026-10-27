package contentimport

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

type importRoundTripFunc func(*http.Request) (*http.Response, error)

func (f importRoundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestImportFetcherRejectsDangerousHostsAndNeverSendsCredentials(t *testing.T) {
	f := NewHTTPFetcher()
	calls := 0
	f.client.Transport = importRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.Header.Get("Cookie") != "" || r.Header.Get("Authorization") != "" {
			t.Fatal("credential leak")
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"video/mp4"}}, Body: io.NopCloser(strings.NewReader("mp4")), ContentLength: 3}, nil
	})
	for _, u := range []string{"https://127.0.0.1/video.mp4", "https://mpvideo.qpic.cn.evil.example/video.mp4", "file:///etc/passwd", "https://user:pass@mpvideo.qpic.cn/v.mp4", "https://mpvideo.qpic.cn:8443/v.mp4"} {
		if _, _, err := f.FetchMedia(context.Background(), WechatArticle, u, 100); err == nil {
			t.Fatal("unsafe fetch", u)
		}
	}
	if calls != 0 {
		t.Fatal(calls)
	}
	if _, _, err := f.FetchMedia(context.Background(), WechatArticle, "https://mpvideo.qpic.cn/v.mp4", 100); err != nil {
		t.Fatal(err)
	}
}
func TestImportFetcherBoundsBodiesAndRejectsPartialMedia(t *testing.T) {
	f := NewHTTPFetcher()
	f.client.Transport = importRoundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 206, Header: http.Header{"Content-Range": []string{"bytes 0-2/200"}}, Body: io.NopCloser(strings.NewReader("abc")), ContentLength: 3}, nil
	})
	if _, _, err := f.FetchMedia(context.Background(), WechatArticle, "https://mpvideo.qpic.cn/v.mp4", 10); err == nil {
		t.Fatal("partial media accepted")
	}
	f.client.Transport = importRoundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("0123456789abcd")), ContentLength: -1}, nil
	})
	if _, _, err := f.FetchMedia(context.Background(), WechatArticle, "https://mpvideo.qpic.cn/v.mp4", 10); err == nil {
		t.Fatal("unbounded body")
	}
}
func TestSourceLinkIncludesWechatArticleAndRejectsProfile(t *testing.T) {
	link, err := ParseSourceLink("文章 https://mp.weixin.qq.com/s/article-slug?scene=1 。")
	if err != nil || link.Platform != WechatArticle {
		t.Fatal(link, err)
	}
	if _, err = ParseSourceLink("https://mp.weixin.qq.com/mp/profile"); err == nil {
		t.Fatal("non-article accepted")
	}
}
func TestExpiredMediaLinkIsNotAdvertisedAsTransientDownloadFailure(t *testing.T) {
	f := NewHTTPFetcher()
	f.client.Transport = importRoundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 403, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("expired"))}, nil
	})
	if _, _, err := f.FetchMedia(context.Background(), Douyin, "https://test.douyinvod.com/file.mp4", 100); errorCode(err) != "media_source_expired" {
		t.Fatal(err)
	}
}
func TestArticleFetchTransientErrorHasFreeRetryCode(t *testing.T) {
	f := NewHTTPFetcher()
	f.client.Transport = importRoundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 503, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("unavailable"))}, nil
	})
	if _, err := f.FetchArticle(context.Background(), "https://mp.weixin.qq.com/s/test"); errorCode(err) != "article_fetch_failed" {
		t.Fatal(err)
	}
}
