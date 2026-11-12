package contentimport

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
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

func TestImportFetcherStreamsMediaToTemporaryFile(t *testing.T) {
	f := NewHTTPFetcher()
	payload := strings.Repeat("0123456789abcdef", 4096)
	f.client.Transport = importRoundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"video/mp4"}}, Body: io.NopCloser(strings.NewReader(payload)), ContentLength: int64(len(payload))}, nil
	})
	media, err := f.FetchMediaFile(context.Background(), Channels, "https://mpvideo.qpic.cn/v.mp4", int64(len(payload)))
	if err != nil {
		t.Fatal(err)
	}
	defer media.Remove()
	info, err := os.Stat(media.Path)
	if err != nil || info.Size() != int64(len(payload)) || media.Size != int64(len(payload)) || media.SHA256 == "" || media.MIME != "video/mp4" {
		t.Fatalf("media=%+v info=%v err=%v", media, info, err)
	}

	f.client.Transport = importRoundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(payload + "x")), ContentLength: -1}, nil
	})
	if _, err = f.FetchMediaFile(context.Background(), Channels, "https://mpvideo.qpic.cn/v.mp4", int64(len(payload))); errorCode(err) != "media_too_large" {
		t.Fatalf("oversize err=%v", err)
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
	xhs, err := ParseSourceLink("https://www.xiaohongshu.com/discovery/item/6A9206BC000000001E0160EB?xsec_token=token%2Bvalue&xsec_source=pc_feed&ignored=1")
	if err != nil {
		t.Fatal(err)
	}
	if xhs.URL != "https://www.xiaohongshu.com/explore/6a9206bc000000001e0160eb?xsec_source=pc_feed&xsec_token=token%2Bvalue" {
		t.Fatalf("XHS source URL = %q", xhs.URL)
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

func TestXHSPageFetchPreservesTokenAndSendsNoCredentials(t *testing.T) {
	f := NewHTTPFetcher()
	var seen *http.Request
	f.client.Transport = importRoundTripFunc(func(request *http.Request) (*http.Response, error) {
		seen = request.Clone(request.Context())
		return &http.Response{
			StatusCode:    http.StatusOK,
			Header:        http.Header{"Content-Type": []string{"text/html; charset=utf-8"}},
			Body:          io.NopCloser(strings.NewReader("<html>public note</html>")),
			ContentLength: 24,
		}, nil
	})
	body, err := f.FetchXHSPage(context.Background(), "https://www.xiaohongshu.com/discovery/item/6A9206BC000000001E0160EB?xsec_token=token%2Bvalue&xsec_source=pc_feed&ignored=1")
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "<html>public note</html>" {
		t.Fatalf("body = %q", body)
	}
	if seen == nil || seen.URL.String() != "https://www.xiaohongshu.com/explore/6a9206bc000000001e0160eb?xsec_source=pc_feed&xsec_token=token%2Bvalue" {
		t.Fatalf("request = %#v", seen)
	}
	if seen.Header.Get("Cookie") != "" || seen.Header.Get("Authorization") != "" {
		t.Fatal("credentials were sent to public page")
	}
	if !strings.Contains(seen.Header.Get("User-Agent"), "Mozilla/5.0") {
		t.Fatalf("user agent = %q", seen.Header.Get("User-Agent"))
	}

	seen = nil
	if _, err := f.FetchXHSPage(context.Background(), "https://xhslink.com/m/1gJ5tOG"); err != nil {
		t.Fatal(err)
	}
	if seen == nil || seen.URL.String() != "https://xhslink.com/m/1gJ5tOG" {
		t.Fatalf("short-link request = %#v", seen)
	}
}

func TestXHSPageFetchRejectsUnknownPathsAndRedirectHosts(t *testing.T) {
	f := NewHTTPFetcher()
	calls := 0
	f.client.Transport = importRoundTripFunc(func(request *http.Request) (*http.Response, error) {
		calls++
		return nil, errors.New("network should not run")
	})
	for _, raw := range []string{
		"https://www.xiaohongshu.com/user/profile/6a9206bc000000001e0160eb",
		"https://www.xiaohongshu.com/explore/not-a-native-id",
		"https://www.xiaohongshu.com:8443/explore/6a9206bc000000001e0160eb",
		"https://user:pass@www.xiaohongshu.com/explore/6a9206bc000000001e0160eb",
	} {
		if _, err := f.FetchXHSPage(context.Background(), raw); err == nil {
			t.Fatalf("unsafe XHS page accepted: %s", raw)
		}
	}
	if calls != 0 {
		t.Fatalf("network calls = %d", calls)
	}

	f.client.Transport = importRoundTripFunc(func(request *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusFound,
			Header:     http.Header{"Location": []string{"https://evil.example/steal"}},
			Body:       io.NopCloser(strings.NewReader("")),
		}, nil
	})
	_, err := f.FetchXHSPage(context.Background(), "https://www.xiaohongshu.com/explore/6a9206bc000000001e0160eb")
	if errorCode(err) != "xhs_page_fetch_failed" {
		t.Fatalf("redirect error = %#v", err)
	}
}
