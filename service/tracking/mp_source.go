package tracking

import (
	"context"
	"errors"
	"strconv"
	"time"
)

// MPArticle is one article of an official account's published list.
type MPArticle struct {
	// ID is stable across syncs: the publish record ID plus the position of the
	// article in a multi-article push.
	ID        string
	Title     string
	HTML      string
	URL       string
	ThumbURL  string
	UpdatedAt time.Time
	Deleted   bool
}

// MPSource lists the most recent published articles, newest first, up to limit.
type MPSource interface {
	Articles(ctx context.Context, limit int) ([]MPArticle, error)
}

// OfficialAccountSource reads the published list through the official account
// "free publish" interface. That interface belongs to the official account, so
// it uses the account's own AppID and secret, never the mini program's. It
// lists only what the account has already published; nothing is fetched from
// article URLs.
type OfficialAccountSource struct {
	API    *WechatAPI
	AppID  string
	Secret string
}

const mpPageSize = 20 // the interface allows at most 20 per call

// mpPageLimit bounds one reply. A page carries every article's full HTML, so a
// page of long articles can exceed it; the source then asks for fewer per call.
// It is a variable only so a test can use a small one.
var mpPageLimit int64 = 8 << 20

func (o *OfficialAccountSource) Articles(ctx context.Context, limit int) ([]MPArticle, error) {
	if limit <= 0 {
		return nil, ErrUnavailable
	}
	token, err := o.API.Token(ctx, o.AppID, o.Secret)
	if err != nil {
		return nil, err
	}
	out := []MPArticle{}
	size := mpPageSize
	offset := 0
	for len(out) < limit {
		var page struct {
			Total int `json:"total_count"`
			Items []struct {
				ArticleID string `json:"article_id"`
				Content   struct {
					News []struct {
						Title   string `json:"title"`
						Content string `json:"content"`
						URL     string `json:"url"`
						Thumb   string `json:"thumb_url"`
						Deleted bool   `json:"is_deleted"`
					} `json:"news_item"`
					UpdateTime int64 `json:"update_time"`
				} `json:"content"`
				UpdateTime int64 `json:"update_time"`
			} `json:"item"`
		}
		payload := map[string]any{"offset": offset, "count": size, "no_content": 0}
		if err = o.API.PostJSON(ctx, "/cgi-bin/freepublish/batchget", token, payload, &page, mpPageLimit); err != nil {
			if errors.Is(err, errResponseTooLarge) {
				if size == 1 {
					// A single publication is bigger than any reply we accept, so asking
					// for less cannot help; say so rather than failing without a reason.
					return nil, fail(502, "公众号有单次发布的内容超过接口响应上限，无法同步；请缩短该次发布的文章后重试")
				}
				// Retry this same offset with half as many per call, and keep the
				// smaller size for the rest of the run.
				size = max(1, size/2)
				continue
			}
			return nil, err
		}
		for _, item := range page.Items {
			updated := item.UpdateTime
			if updated == 0 {
				updated = item.Content.UpdateTime
			}
			for index, news := range item.Content.News {
				if item.ArticleID == "" || len(out) >= limit {
					continue
				}
				article := MPArticle{ID: item.ArticleID + "#" + strconv.Itoa(index), Title: news.Title, HTML: news.Content, URL: news.URL, ThumbURL: news.Thumb, Deleted: news.Deleted}
				if updated > 0 {
					article.UpdatedAt = time.Unix(updated, 0).UTC()
				}
				out = append(out, article)
			}
		}
		if len(page.Items) == 0 || offset+len(page.Items) >= page.Total {
			break
		}
		offset += size
	}
	return out, nil
}
