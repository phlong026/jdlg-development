package wechatpay

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sync"

	"github.com/wechatpay-apiv3/wechatpay-go/core"
	"github.com/wechatpay-apiv3/wechatpay-go/core/option"
	"github.com/wechatpay-apiv3/wechatpay-go/services/payments/jsapi"
	"github.com/wechatpay-apiv3/wechatpay-go/utils"
)

// Config 微信支付配置（与 config.WechatPayConfig 一致，避免循环依赖）
type Config struct {
	MchID     string
	APIv3Key  string
	CertDir   string
	NotifyURL string
	Enabled   bool
}

var (
	client   *core.Client
	clientMu sync.RWMutex
)

// Init 使用商户私钥、证书序列号、APIv3 密钥初始化 Client（具备签名与平台证书自动更新）
func Init(ctx context.Context, cfg Config) error {
	if !cfg.Enabled || cfg.MchID == "" || cfg.APIv3Key == "" || cfg.CertDir == "" {
		return nil
	}
	keyPath := filepath.Join(cfg.CertDir, "apiclient_key.pem")
	certPath := filepath.Join(cfg.CertDir, "apiclient_cert.pem")
	privateKey, err := utils.LoadPrivateKeyWithPath(keyPath)
	if err != nil {
		return fmt.Errorf("load wechat pay private key: %w", err)
	}
	serialNo, err := certSerialNumberFromPEM(certPath)
	if err != nil {
		return fmt.Errorf("read wechat pay cert serial: %w", err)
	}
	opts := []core.ClientOption{
		option.WithWechatPayAutoAuthCipher(cfg.MchID, serialNo, privateKey, cfg.APIv3Key),
	}
	c, err := core.NewClient(ctx, opts...)
	if err != nil {
		return fmt.Errorf("new wechat pay client: %w", err)
	}
	clientMu.Lock()
	client = c
	clientMu.Unlock()
	log.Println("[WechatPay] client inited, mchID=", cfg.MchID)
	return nil
}

func certSerialNumberFromPEM(pemPath string) (string, error) {
	b, err := os.ReadFile(pemPath)
	if err != nil {
		return "", err
	}
	block, _ := pem.Decode(b)
	if block == nil {
		return "", fmt.Errorf("no PEM block in %s", pemPath)
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return "", err
	}
	// 微信支付要求证书序列号为十六进制字符串
	return fmt.Sprintf("%X", cert.SerialNumber), nil
}

// Client 返回已初始化的微信支付 Client，未初始化时返回 nil
func Client() *core.Client {
	clientMu.RLock()
	defer clientMu.RUnlock()
	return client
}

// JSAPIPrepay 发起 JSAPI 预支付，返回小程序 wx.requestPayment 所需参数
// amountTotal 单位：分；description 商品描述；outTradeNo 商户订单号；openID 用户 openid
func JSAPIPrepay(ctx context.Context, appID string, cfg Config, openID string, amountTotal int64, description, outTradeNo string) (map[string]string, error) {
	c := Client()
	if c == nil {
		return nil, fmt.Errorf("wechat pay client not inited")
	}
	svc := jsapi.JsapiApiService{Client: c}
	req := jsapi.PrepayRequest{
		Appid:       core.String(appID),
		Mchid:       core.String(cfg.MchID),
		Description: core.String(description),
		OutTradeNo:  core.String(outTradeNo),
		NotifyUrl:   core.String(cfg.NotifyURL),
		Amount: &jsapi.Amount{
			Total: core.Int64(amountTotal),
		},
		Payer: &jsapi.Payer{
			Openid: core.String(openID),
		},
	}
	resp, _, err := svc.PrepayWithRequestPayment(ctx, req)
	if err != nil {
		return nil, err
	}
	out := make(map[string]string)
	if resp.Appid != nil {
		out["appId"] = *resp.Appid
	}
	if resp.TimeStamp != nil {
		out["timeStamp"] = *resp.TimeStamp
	}
	if resp.NonceStr != nil {
		out["nonceStr"] = *resp.NonceStr
	}
	if resp.Package != nil {
		out["package"] = *resp.Package
	}
	if resp.SignType != nil {
		out["signType"] = *resp.SignType
	}
	if resp.PaySign != nil {
		out["paySign"] = *resp.PaySign
	}
	return out, nil
}

// TransferSceneReportInfo 转账场景报备信息
type TransferSceneReportInfo struct {
	InfoType    *string `json:"info_type,omitempty"`
	InfoContent *string `json:"info_content,omitempty"`
}

// TransferBillRequest 发起转账请求 body
type TransferBillRequest struct {
	Appid                    string                   `json:"appid"`
	OutBillNo                string                   `json:"out_bill_no"`
	TransferSceneId          string                   `json:"transfer_scene_id"`
	Openid                   string                   `json:"openid"`
	TransferAmount           int64                    `json:"transfer_amount"`
	TransferRemark           string                   `json:"transfer_remark"`
	NotifyUrl                string                   `json:"notify_url,omitempty"`
	UserRecvPerception       string                   `json:"user_recv_perception,omitempty"`
	TransferSceneReportInfos []TransferSceneReportInfo `json:"transfer_scene_report_infos"`
}

// TransferBillResponse 发起转账响应（仅列出需用字段）
type TransferBillResponse struct {
	OutBillNo      string  `json:"out_bill_no"`
	TransferBillNo string  `json:"transfer_bill_no"`
	CreateTime     string  `json:"create_time"`
	State          string  `json:"state"`
	PackageInfo    *string `json:"package_info,omitempty"`
}

const transferBillURL = "https://api.mch.weixin.qq.com/v3/fund-app/mch-transfer/transfer-bills"

// CreateTransferBill 发起商家转账（用户确认模式）。amountFen 单位分；state 为 WAIT_USER_CONFIRM 时返回 package_info 用于调起收款确认页。
func CreateTransferBill(ctx context.Context, appID, outBillNo, transferSceneId, openid string, amountFen int64, transferRemark, notifyURL string, reportInfos []TransferSceneReportInfo) (*TransferBillResponse, error) {
	c := Client()
	if c == nil {
		return nil, fmt.Errorf("wechat pay client not inited")
	}
	req := TransferBillRequest{
		Appid:                    appID,
		OutBillNo:                outBillNo,
		TransferSceneId:          transferSceneId,
		Openid:                   openid,
		TransferAmount:           amountFen,
		TransferRemark:           transferRemark,
		NotifyUrl:                notifyURL,
		TransferSceneReportInfos: reportInfos,
	}
	if len(req.TransferSceneReportInfos) == 0 {
		req.TransferSceneReportInfos = []TransferSceneReportInfo{
			{InfoType: strPtr("活动名称"), InfoContent: strPtr("余额提现")},
			{InfoType: strPtr("奖励说明"), InfoContent: strPtr("设计师/企业认证用户提现")},
		}
	}
	result, err := c.Post(ctx, transferBillURL, req)
	if err != nil {
		return nil, err
	}
	if result.Response == nil || result.Response.Body == nil {
		return nil, fmt.Errorf("empty response body")
	}
	body, _ := io.ReadAll(result.Response.Body)
	_ = result.Response.Body.Close()
	var resp TransferBillResponse
	if err := jsonUnmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("parse transfer response: %w", err)
	}
	return &resp, nil
}

func strPtr(s string) *string { return &s }

func jsonUnmarshal(b []byte, v interface{}) error {
	return json.Unmarshal(b, v)
}
