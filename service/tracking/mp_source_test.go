package tracking

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type fakeOfficialAccount struct {
	t        *testing.T
	total    int
	failWith string
	calls    []map[string]any
	tokens   int
	pad      int // extra bytes of HTML per article, to make long articles
}

func (f *fakeOfficialAccount) api() *WechatAPI {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/cgi-bin/token":
			f.tokens++
			if r.URL.Query().Get("appid") != "mp-app" || r.URL.Query().Get("secret") != "mp-secret" {
				f.t.Errorf("wrong credentials %v", r.URL.Query())
			}
			io.WriteString(w, `{"access_token":"mp-token"}`)
		case "/cgi-bin/freepublish/batchget":
			if r.URL.Query().Get("access_token") != "mp-token" || r.Method != "POST" {
				f.t.Errorf("bad request %s %s", r.Method, r.URL)
			}
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			f.calls = append(f.calls, body)
			if f.failWith != "" {
				io.WriteString(w, f.failWith)
				return
			}
			offset, count := int(body["offset"].(float64)), int(body["count"].(float64))
			items := []string{}
			for i := offset; i < offset+count && i < f.total; i++ {
				// Every third publish record holds two articles.
				extra := ""
				if i%3 == 0 {
					extra = fmt.Sprintf(`,{"title":"次条 %d","content":"<p>次条正文 %d</p>","url":"https://mp.weixin.qq.com/s/b%d","thumb_url":""}`, i, i, i)
				}
				items = append(items, fmt.Sprintf(`{"article_id":"pub-%d","update_time":%d,"content":{"update_time":0,"news_item":[{"title":"头条 %d","content":"<p>正文 %d</p>%s","url":"https://mp.weixin.qq.com/s/a%d","thumb_url":"https://mmbiz.qpic.cn/t/%d","is_deleted":%v}%s]}}`, i, 1760000000+i, i, i, strings.Repeat("x", f.pad), i, i, i == 4, extra))
			}
			fmt.Fprintf(w, `{"total_count":%d,"item_count":%d,"item":[%s]}`, f.total, len(items), joinStrings(items))
		default:
			http.NotFound(w, r)
		}
	}))
	f.t.Cleanup(server.Close)
	api := NewWechatAPI()
	api.Base = server.URL
	return api
}

func joinStrings(values []string) string {
	out := ""
	for i, v := range values {
		if i > 0 {
			out += ","
		}
		out += v
	}
	return out
}

func TestOfficialAccountSourcePagesAndFlattensMultiArticlePushes(t *testing.T) {
	f := &fakeOfficialAccount{t: t, total: 45}
	source := &OfficialAccountSource{API: f.api(), AppID: "mp-app", Secret: "mp-secret"}
	got, err := source.Articles(context.Background(), 30)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 30 {
		t.Fatalf("limit not honoured: %d articles", len(got))
	}
	if f.tokens != 1 || len(f.calls) < 2 {
		t.Fatalf("one token and several pages expected: %d tokens, %d pages", f.tokens, len(f.calls))
	}
	for i, call := range f.calls {
		if int(call["offset"].(float64)) != i*20 || call["count"].(float64) != 20 {
			t.Fatalf("page %d requested %v", i, call)
		}
	}
	seen := map[string]bool{}
	for _, a := range got {
		if seen[a.ID] || a.ID == "" {
			t.Fatalf("article IDs must be unique and stable: %q", a.ID)
		}
		seen[a.ID] = true
	}
	if got[0].ID != "pub-0#0" || got[1].ID != "pub-0#1" || got[1].Title != "次条 0" {
		t.Fatalf("a two-article push must yield two articles: %+v %+v", got[0], got[1])
	}
	if !got[0].UpdatedAt.Equal(time.Unix(1760000000, 0).UTC()) || got[0].URL != "https://mp.weixin.qq.com/s/a0" || got[0].ThumbURL != "https://mmbiz.qpic.cn/t/0" {
		t.Fatalf("article fields: %+v", got[0])
	}
	deleted := 0
	for _, a := range got {
		if a.Deleted {
			deleted++
			if a.ID != "pub-4#0" {
				t.Fatalf("wrong article marked deleted: %s", a.ID)
			}
		}
	}
	if deleted != 1 {
		t.Fatalf("deleted flag lost: %d", deleted)
	}
}

func TestOfficialAccountSourceStopsAtTheEndAndReportsErrors(t *testing.T) {
	f := &fakeOfficialAccount{t: t, total: 7}
	source := &OfficialAccountSource{API: f.api(), AppID: "mp-app", Secret: "mp-secret"}
	got, err := source.Articles(context.Background(), 100)
	if err != nil || len(got) != 7+3 || len(f.calls) != 1 {
		t.Fatalf("short list: %d articles, %d pages, %v", len(got), len(f.calls), err)
	}
	empty := &fakeOfficialAccount{t: t, total: 0}
	if got, err = (&OfficialAccountSource{API: empty.api(), AppID: "mp-app", Secret: "mp-secret"}).Articles(context.Background(), 10); err != nil || len(got) != 0 {
		t.Fatalf("empty account: %v %v", got, err)
	}
	denied := &fakeOfficialAccount{t: t, total: 5, failWith: `{"errcode":48001,"errmsg":"api unauthorized hint: mp-secret"}`}
	_, err = (&OfficialAccountSource{API: denied.api(), AppID: "mp-app", Secret: "mp-secret"}).Articles(context.Background(), 10)
	var we *WechatError
	if !errors.As(err, &we) || we.Code != 48001 {
		t.Fatalf("want typed error, got %v", err)
	}
	if _, err = source.Articles(context.Background(), 0); err == nil {
		t.Fatal("zero limit accepted")
	}
	if _, err = (&OfficialAccountSource{API: f.api(), AppID: "", Secret: ""}).Articles(context.Background(), 5); err == nil {
		t.Fatal("missing credentials accepted")
	}
}

func withPageLimit(t *testing.T, limit int64) {
	t.Helper()
	saved := mpPageLimit
	mpPageLimit = limit
	t.Cleanup(func() { mpPageLimit = saved })
}

// A page carries every article's full HTML. When a page is bigger than the reply
// limit, the source must ask for fewer per call instead of failing the whole sync
// on every run.
func TestOfficialAccountSourceShrinksThePageWhenTheReplyIsTooLarge(t *testing.T) {
	withPageLimit(t, 6000)
	f := &fakeOfficialAccount{t: t, total: 12, pad: 400}
	got, err := (&OfficialAccountSource{API: f.api(), AppID: "mp-app", Secret: "mp-secret"}).Articles(context.Background(), 100)
	if err != nil {
		t.Fatalf("long articles must not fail the sync: %v", err)
	}
	if len(got) != 12+4 || got[0].ID != "pub-0#0" || got[len(got)-1].ID != "pub-11#0" {
		t.Fatalf("every article must still be read, in order: %d articles", len(got))
	}
	counts, offsets := []int{}, []int{}
	for _, call := range f.calls {
		counts, offsets = append(counts, int(call["count"].(float64))), append(offsets, int(call["offset"].(float64)))
	}
	if fmt.Sprint(counts) != "[20 10 5 5 5]" || fmt.Sprint(offsets) != "[0 0 0 5 10]" {
		t.Fatalf("the same offset must be retried with half the count, then stay small: counts=%v offsets=%v", counts, offsets)
	}
}

// If even one publication is bigger than any reply we accept, shrinking cannot
// help. That must end with a clear reason rather than loop or fail anonymously.
func TestOfficialAccountSourceNamesTheReasonWhenOneArticleIsTooLarge(t *testing.T) {
	withPageLimit(t, 2000)
	f := &fakeOfficialAccount{t: t, total: 5, pad: 3000}
	_, err := (&OfficialAccountSource{API: f.api(), AppID: "mp-app", Secret: "mp-secret"}).Articles(context.Background(), 100)
	var known *Error
	if !errors.As(err, &known) || known.Status != 502 || !strings.Contains(known.Message, "超过接口响应上限") {
		t.Fatalf("expected a specific, operator-readable reason: %v", err)
	}
	if last := f.calls[len(f.calls)-1]; last["count"].(float64) != 1 || len(f.calls) > 6 {
		t.Fatalf("the source must stop at one per call and not keep retrying: %d calls, last %v", len(f.calls), last)
	}
}

// An oversize reply is still "unavailable" to every caller that cannot ask for less.
func TestOversizeReplyIsStillUnavailableToOtherCallers(t *testing.T) {
	if !errors.Is(errResponseTooLarge, ErrUnavailable) {
		t.Fatal("an oversize reply must satisfy errors.Is(err, ErrUnavailable)")
	}
}
