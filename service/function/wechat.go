package function

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"
)

// accessTokenCache 内存缓存 access_token，避免频繁请求微信
var accessTokenCache struct {
	mu       sync.Mutex
	token    string
	expireAt time.Time
	appID    string
}

// WechatTokenResponse 微信 token 接口响应
type WechatTokenResponse struct {
	AccessToken string `json:"access_token"`
	ExpiresIn   int    `json:"expires_in"`
	ErrCode     int    `json:"errcode"`
	ErrMsg      string `json:"errmsg"`
}

// GetAccessToken 获取小程序 access_token（带缓存，提前 5 分钟刷新）
func GetAccessToken(appID, appSecret string) (string, error) {
	if appID == "" || appSecret == "" {
		return "", fmt.Errorf("微信 AppID 或 AppSecret 未配置")
	}
	accessTokenCache.mu.Lock()
	defer accessTokenCache.mu.Unlock()
	now := time.Now()
	if accessTokenCache.appID == appID && accessTokenCache.token != "" && accessTokenCache.expireAt.After(now.Add(5*time.Minute)) {
		return accessTokenCache.token, nil
	}
	url := fmt.Sprintf("https://api.weixin.qq.com/cgi-bin/token?grant_type=client_credential&appid=%s&secret=%s", appID, appSecret)
	resp, err := http.Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	var result WechatTokenResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return "", err
	}
	if result.ErrCode != 0 {
		return "", fmt.Errorf("wechat token api error: %d, %s", result.ErrCode, result.ErrMsg)
	}
	if result.AccessToken == "" {
		return "", fmt.Errorf("wechat token 为空")
	}
	expiresIn := result.ExpiresIn
	if expiresIn <= 0 {
		expiresIn = 7200
	}
	accessTokenCache.token = result.AccessToken
	accessTokenCache.expireAt = now.Add(time.Duration(expiresIn) * time.Second)
	accessTokenCache.appID = appID
	return result.AccessToken, nil
}

// GetWxacodeUnlimitRequest 生成小程序码请求体（与官方 getUnlimitedQRCode 文档一致）
// 文档：https://developers.weixin.qq.com/miniprogram/dev/OpenApiDoc/qrcode-link/qr-code/getUnlimitedQRCode.html
type GetWxacodeUnlimitRequest struct {
	Scene     string `json:"scene"`                // 必填，最大 32 个可见字符，只支持数字、大小写英文及 !#$&'()*+,/:;=?@-._~
	Page      string `json:"page,omitempty"`      // 可选，默认主页，根路径前不要加 /
	CheckPath *bool  `json:"check_path,omitempty"` // 可选，默认 true，为 true 时 page 必须是已发布小程序存在的页面
	EnvVersion string `json:"env_version,omitempty"` // 可选，release / trial / develop，默认 release
	Width    int    `json:"width,omitempty"`      // 可选，默认 430，最小 280，最大 1280
}

// GetWxacodeUnlimit 生成小程序码（无数量限制）。成功返回图片二进制，失败返回错误。
func GetWxacodeUnlimit(accessToken, scene, page string, width int) (imageData []byte, err error) {
	if accessToken == "" || scene == "" {
		return nil, fmt.Errorf("access_token 或 scene 为空")
	}
	if len(scene) > 32 {
		return nil, fmt.Errorf("scene 最长 32 个字符")
	}
	if page == "" {
		page = "pages/index/index"
	}
	// 根路径前不要加 /
	if len(page) > 0 && page[0] == '/' {
		page = page[1:]
	}
	// width 默认 430，范围 280～1280（官方文档）
	if width <= 0 {
		width = 430
	}
	if width < 280 {
		width = 280
	}
	if width > 1280 {
		width = 1280
	}
	checkPath := true
	reqBody := GetWxacodeUnlimitRequest{
		Scene:      scene,
		Page:       page,
		CheckPath:  &checkPath,
		EnvVersion: "release",
		Width:      width,
	}
	body, _ := json.Marshal(reqBody)
	url := fmt.Sprintf("https://api.weixin.qq.com/wxa/getwxacodeunlimit?access_token=%s", accessToken)
	resp, err := http.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	// 微信失败时返回 JSON（errcode/errmsg），成功时返回图片二进制；需先识别错误再当图片返回
	contentType := resp.Header.Get("Content-Type")
	tryJSONError := contentType == "application/json" || (len(data) >= 1 && data[0] == '{')
	if tryJSONError {
		var errResp struct {
			ErrCode int    `json:"errcode"`
			ErrMsg  string `json:"errmsg"`
		}
		if json.Unmarshal(data, &errResp) == nil && errResp.ErrCode != 0 {
			return nil, fmt.Errorf("wechat getwxacodeunlimit error: %d, %s", errResp.ErrCode, errResp.ErrMsg)
		}
	}
	if len(data) == 0 {
		return nil, fmt.Errorf("小程序码返回为空")
	}
	return data, nil
}

type GetUserPhoneNumberRequest struct {
	Code string `json:"code"`
}

type GetUserPhoneNumberResponse struct {
	ErrCode   int    `json:"errcode"`
	ErrMsg    string `json:"errmsg"`
	PhoneInfo struct {
		PhoneNumber     string `json:"phoneNumber"`
		PurePhoneNumber string `json:"purePhoneNumber"`
		CountryCode     string `json:"countryCode"`
	} `json:"phone_info"`
}

func GetUserPhoneNumber(accessToken, code string) (string, error) {
	if accessToken == "" || code == "" {
		return "", fmt.Errorf("access_token 或 code 为空")
	}
	reqBody, _ := json.Marshal(GetUserPhoneNumberRequest{Code: code})
	url := fmt.Sprintf("https://api.weixin.qq.com/wxa/business/getuserphonenumber?access_token=%s", accessToken)
	resp, err := http.Post(url, "application/json", bytes.NewReader(reqBody))
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	var result GetUserPhoneNumberResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return "", err
	}
	if result.ErrCode != 0 {
		return "", fmt.Errorf("wechat getuserphonenumber error: %d, %s", result.ErrCode, result.ErrMsg)
	}
	if result.PhoneInfo.PhoneNumber != "" {
		return result.PhoneInfo.PhoneNumber, nil
	}
	if result.PhoneInfo.PurePhoneNumber != "" {
		return result.PhoneInfo.PurePhoneNumber, nil
	}
	return "", fmt.Errorf("未获取到手机号")
}

// WechatCode2SessionResponse 微信code2session响应
type WechatCode2SessionResponse struct {
	OpenID     string `json:"openid"`
	SessionKey string `json:"session_key"`
	UnionID    string `json:"unionid"`
	ErrCode    int    `json:"errcode"`
	ErrMsg     string `json:"errmsg"`
}

// Code2Session 微信code换取openid
func Code2Session(appID, appSecret, code string) (*WechatCode2SessionResponse, error) {
	url := fmt.Sprintf("https://api.weixin.qq.com/sns/jscode2session?appid=%s&secret=%s&js_code=%s&grant_type=authorization_code",
		appID, appSecret, code)

	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var result WechatCode2SessionResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}

	if result.ErrCode != 0 {
		return nil, fmt.Errorf("wechat api error: %d, %s", result.ErrCode, result.ErrMsg)
	}

	return &result, nil
}
