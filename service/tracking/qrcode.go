package tracking

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"time"
)

// WechatCode uses the same official endpoints as the existing invite-code integration,
// with explicit timeout, no redirects, size bounds and no secret-bearing error output.
func WechatCode(ctx context.Context, appID, secret, scene, page, environment string) ([]byte, error) {
	if appID == "" || secret == "" || len(scene) != 32 || (environment != "release" && environment != "trial" && environment != "develop") {
		return nil, ErrUnavailable
	}
	client := &http.Client{Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	tokenURL := "https://api.weixin.qq.com/cgi-bin/token?" + url.Values{"grant_type": {"client_credential"}, "appid": {appID}, "secret": {secret}}.Encode()
	request, e := http.NewRequestWithContext(ctx, "GET", tokenURL, nil)
	if e != nil {
		return nil, ErrUnavailable
	}
	response, e := client.Do(request)
	if e != nil {
		return nil, ErrUnavailable
	}
	b, e := io.ReadAll(io.LimitReader(response.Body, 65537))
	response.Body.Close()
	if e != nil || response.StatusCode != 200 || len(b) > 65536 {
		return nil, ErrUnavailable
	}
	var token struct {
		Token string `json:"access_token"`
		Error int    `json:"errcode"`
	}
	if json.Unmarshal(b, &token) != nil || token.Error != 0 || token.Token == "" {
		return nil, ErrUnavailable
	}
	payload := marshal(map[string]any{"scene": scene, "page": page, "check_path": true, "env_version": environment, "width": 430})
	request, e = http.NewRequestWithContext(ctx, "POST", "https://api.weixin.qq.com/wxa/getwxacodeunlimit?access_token="+url.QueryEscape(token.Token), bytes.NewReader(payload))
	if e != nil {
		return nil, ErrUnavailable
	}
	request.Header.Set("Content-Type", "application/json")
	response, e = client.Do(request)
	if e != nil {
		return nil, ErrUnavailable
	}
	defer response.Body.Close()
	b, e = io.ReadAll(io.LimitReader(response.Body, 2<<20))
	mime := http.DetectContentType(b)
	if e != nil || response.StatusCode != 200 || len(b) >= 2<<20 || (mime != "image/png" && mime != "image/jpeg") {
		return nil, ErrUnavailable
	}
	return b, nil
}
