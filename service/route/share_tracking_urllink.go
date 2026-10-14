package route

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"service/config"
	"service/tracking"
)

const trackingReaderRoute = "pages/contentreader/contentreader"

// wechatCredentials are passed in, not read from global configuration here, so
// the code that spends them can be tested without process-wide state.
type wechatCredentials struct{ AppID, Secret string }

func miniprogramCredentials() wechatCredentials {
	wx := config.Get().Wechat
	return wechatCredentials{AppID: wx.AppID, Secret: wx.AppSecret}
}

func trackingURLLinkEnabled() bool { return os.Getenv("TRACKING_WECHAT_URLLINK_ENABLED") == "true" }

func trackingWechatEnv() string {
	if environment := os.Getenv("TRACKING_WECHAT_ENV"); environment != "" {
		return environment
	}
	return "release"
}

// writeFileAtomically never leaves a half-written cache entry for a concurrent
// reader: it writes a private temporary file and renames it into place.
func writeFileAtomically(name string, data []byte) error {
	temp, err := os.CreateTemp(filepath.Dir(name), ".tmp-")
	if err != nil {
		return err
	}
	defer os.Remove(temp.Name())
	if _, err = temp.Write(data); err != nil {
		temp.Close()
		return err
	}
	if err = temp.Chmod(0600); err != nil {
		temp.Close()
		return err
	}
	if err = temp.Close(); err != nil {
		return err
	}
	return os.Rename(temp.Name(), name)
}

// trackingURLLink returns a mini-program URL Link that opens the content reader
// at one published version with a fresh share token for the acting account. It
// applies the same checks as the mini-program code: the actor must manage the
// content's owner, and the version must be published and readable. A link is
// cached per actor, content, version, environment and UTC day, like the code, so
// repeated requests do not mint a link (and a share) each time.
func trackingURLLink(ctx context.Context, svc *tracking.Service, api *tracking.WechatAPI, creds wechatCredentials, scope tracking.Scope, contentID string, version int) (tracking.URLLinkResult, error) {
	var out tracking.URLLinkResult
	if !trackingURLLinkEnabled() || !scope.Manage {
		return out, &tracking.Error{Status: 403, Message: "URL Link权限未接通"}
	}
	cards, err := svc.Contents(ctx, scope, tracking.Filter{}, contentID)
	if err != nil || len(cards.Items.([]tracking.Content)) == 0 {
		return out, &tracking.Error{Status: 404, Message: "素材不可访问"}
	}
	if _, err = svc.ReadContent(ctx, contentID, version); err != nil {
		return out, err
	}
	root, err := trackingMediaRoot()
	if err != nil {
		return out, err
	}
	now := svc.Now()
	environment := trackingWechatEnv()
	key := trackingCodeCacheKey(svc.Config.Tenant, scope.Actor, contentID, version, environment+"/urllink", now)
	// Creating the share first re-validates authorization even on a cache hit.
	share, err := svc.CreateShare(ctx, tracking.ShareInput{Route: trackingReaderRoute, Content: contentID, Version: version, Key: "ul-" + key}, "", scope.Actor)
	if err != nil {
		return out, err
	}
	name := filepath.Join(root, "urllink-"+key)
	if cached, readErr := os.ReadFile(name); readErr == nil {
		var hit tracking.URLLinkResult
		if json.Unmarshal(cached, &hit) == nil && hit.URL != "" && hit.Expires.After(now.Add(24*time.Hour)) {
			return hit, nil
		}
	}
	token, ok := share["share_token"].(string)
	if !ok || token == "" {
		return out, tracking.ErrUnavailable
	}
	query := url.Values{"content_id": {contentID}, "content_version": {strconv.Itoa(version)}, "st": {token}}.Encode()
	out, err = tracking.WechatURLLink(ctx, api, creds.AppID, creds.Secret, trackingReaderRoute, query, environment, now)
	if err != nil {
		return tracking.URLLinkResult{}, err
	}
	if data, marshalErr := json.Marshal(out); marshalErr == nil {
		_ = writeFileAtomically(name, data)
	}
	return out, nil
}

// trackingURLLinkProblem turns an upstream failure into a safe operator message.
func trackingURLLinkProblem(err error) string {
	var wechat *tracking.WechatError
	if errors.As(err, &wechat) {
		return "URL Link生成失败（微信返回错误码 " + strconv.Itoa(wechat.Code) + "）。请核对小程序是否已发布、是否具备该接口权限"
	}
	var known *tracking.Error
	if errors.As(err, &known) {
		return known.Message
	}
	return "URL Link生成失败，请稍后重试"
}
