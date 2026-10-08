package route

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"net/url"
	"sort"

	"github.com/gin-gonic/gin"

	"service/component/virtualpay"
	"service/config"
	"service/model"
)

// xpayInnerFields 解密后的明文 JSON 中我们关心的字段子集。
//
// 完整字段在联调阶段补齐（见历史遗留问题阶段 3.1）。
// 当前只提取分发所必需的字段。
type xpayInnerFields struct {
	Event      string
	OutTradeNo string
	OpenID     string
}

// extractInnerFields 用 map 解析推送明文，兼容大小写不一的字段命名。
func extractInnerFields(b []byte) (*xpayInnerFields, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil {
		return nil, err
	}
	pick := func(keys ...string) string {
		for _, k := range keys {
			if v, ok := raw[k]; ok {
				var s string
				if err := json.Unmarshal(v, &s); err == nil && s != "" {
					return s
				}
			}
		}
		return ""
	}
	return &xpayInnerFields{
		Event:      pick("Event", "event"),
		OutTradeNo: pick("OutTradeNo", "out_trade_no", "outTradeNo"),
		OpenID:     pick("OpenId", "openid", "OpenID"),
	}, nil
}

// extractOuterFields 解析推送外层 JSON。
//
// 外层字段名各家文档约定不一（公众号/小程序 token 配置时大写、回调推送时混用），
// 用 map 取值统一兼容。
type xpayOuterFields struct {
	Encrypt   string
	Signature string
	Timestamp string
	Nonce     string
}

func extractOuterFields(b []byte) (*xpayOuterFields, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil {
		return nil, err
	}
	pick := func(keys ...string) string {
		for _, k := range keys {
			if v, ok := raw[k]; ok {
				var s string
				if err := json.Unmarshal(v, &s); err == nil && s != "" {
					return s
				}
			}
		}
		return ""
	}
	return &xpayOuterFields{
		Encrypt:   pick("Encrypt", "encrypt"),
		Signature: pick("MsgSignature", "Signature", "signature", "msg_signature"),
		Timestamp: pick("TimeStamp", "timestamp"),
		Nonce:     pick("Nonce", "nonce"),
	}, nil
}

func firstQueryValue(query url.Values, keys ...string) string {
	for _, key := range keys {
		if value := query.Get(key); value != "" {
			return value
		}
	}
	return ""
}

func extractOuterFieldsWithQuery(b []byte, query url.Values) (*xpayOuterFields, error) {
	outer, err := extractOuterFields(b)
	if err != nil {
		return nil, err
	}
	if outer.Signature == "" {
		outer.Signature = firstQueryValue(query, "msg_signature", "signature")
	}
	if outer.Timestamp == "" {
		outer.Timestamp = firstQueryValue(query, "timestamp")
	}
	if outer.Nonce == "" {
		outer.Nonce = firstQueryValue(query, "nonce")
	}
	return outer, nil
}

// RegisterWXMsgRoutes 注册 /api/v1/wxmsg/push 推送统一入口。
//
// 不需要 token 验证（来源是微信，验签靠 signature + AES 解密）。
//
// 响应：始终返回 {"errcode":0,"errmsg":"ok"}（HTTP 200），微信侧据此判断是否需要重试。
// 任何业务处理错误都只写日志，不外泄给微信，避免触发 15 次重试。
func RegisterWXMsgRoutes(v1 *gin.RouterGroup, cfg *config.Config, deps VirtualPayDeps) {
	// GET /wxmsg/push —— 微信消息推送 URL 验证（仅在 mp 后台「提交」配置时触发一次）。
	//
	// 微信发送 GET 请求，参数：signature / timestamp / nonce / echostr。
	// 验签算法：sha1(sort([token, timestamp, nonce]))，通过后原样返回 echostr 即完成验证。
	v1.GET("/wxmsg/push", func(c *gin.Context) {
		signature := c.Query("signature")
		timestamp := c.Query("timestamp")
		nonce := c.Query("nonce")
		echostr := c.Query("echostr")
		if signature == "" || timestamp == "" || nonce == "" || echostr == "" {
			c.String(http.StatusBadRequest, "missing params")
			return
		}
		// 复用 VerifyPushSignature：URL 验证时 encrypt 参数为空，排序拼接 [token, timestamp, nonce]
		arr := []string{cfg.VirtualPay.MessageToken, timestamp, nonce}
		sort.Strings(arr)
		h := sha1.New()
		for _, s := range arr {
			h.Write([]byte(s))
		}
		if hex.EncodeToString(h.Sum(nil)) != signature {
			log.Printf("[WXMsg] URL verify failed: signature mismatch")
			c.String(http.StatusForbidden, "signature mismatch")
			return
		}
		c.String(http.StatusOK, echostr)
	})

	v1.POST("/wxmsg/push", func(c *gin.Context) {
		body, err := io.ReadAll(c.Request.Body)
		if err != nil {
			log.Printf("[WXMsg] read body err: %v", err)
			c.JSON(http.StatusOK, gin.H{"errcode": 0, "errmsg": "ok"})
			return
		}
		event, outTradeNo, openID, processErr := handleXPayPush(body, c.Request.URL.Query(), cfg, deps)
		if processErr != nil {
			log.Printf("[WXMsg] handle err: event=%s outTradeNo=%s openid=%s err=%v",
				event, outTradeNo, openID, processErr)
		}
		c.JSON(http.StatusOK, gin.H{"errcode": 0, "errmsg": "ok"})
	})
}

// handleXPayPush 解析、验签、解密、幂等去重、业务分发。
//
// 抽成纯函数（不依赖 *gin.Context）便于阶段 3.1 单测覆盖。
//
// 返回 (event, outTradeNo, openID, err)：
//   - 任意校验失败时 err 非空，但 event/outTradeNo/openID 仍尽力填，便于日志定位
//   - 幂等命中（重复推送）返回 nil 错误，调用方按"已 ack"对待
func handleXPayPush(rawBody []byte, query url.Values, cfg *config.Config, deps VirtualPayDeps) (event, outTradeNo, openID string, err error) {
	if !cfg.VirtualPay.Enabled {
		return "", "", "", errors.New("virtualpay not enabled")
	}

	var outer *xpayOuterFields
	outer, err = extractOuterFieldsWithQuery(rawBody, query)
	if err != nil {
		return "", "", "", err
	}
	if outer.Encrypt == "" || outer.Signature == "" || outer.Timestamp == "" || outer.Nonce == "" {
		return "", "", "", errors.New("missing required push fields")
	}

	// 1. 验签
	if !virtualpay.VerifyPushSignature(cfg.VirtualPay.MessageToken, outer.Timestamp, outer.Nonce, outer.Encrypt, outer.Signature) {
		return "", "", "", errors.New("signature verify failed")
	}

	// 2. 解密
	plain, err := virtualpay.DecryptPushMessage(cfg.VirtualPay.MessageEncodingAESKey, outer.Encrypt, cfg.Wechat.AppID)
	if err != nil {
		return "", "", "", err
	}

	// 3. 解析内层 JSON
	inner, err := extractInnerFields(plain)
	if err != nil {
		return "", "", "", err
	}
	event = inner.Event
	outTradeNo = inner.OutTradeNo
	openID = inner.OpenID
	if event == "" {
		return "", outTradeNo, openID, errors.New("push missing Event")
	}

	// 4. 业务分发（先 dispatch 再写日志，保证 dispatch 失败时下次推送能再次触发）
	if dispatchErr := dispatchXPayEvent(event, outTradeNo, openID, cfg, deps); dispatchErr != nil {
		return event, outTradeNo, openID, dispatchErr
	}

	// 5. 写日志做审计与重复推送追踪。
	//    幂等保护本身依赖 applyVirtualPayOrderSuccess 内的状态机锁，
	//    本表的唯一索引仅作"重复推送计数"参考。
	if deps.XPayMessageLogModel != nil {
		insErr := deps.XPayMessageLogModel.Insert(&model.XPayMessageLog{
			Event:      event,
			OpenID:     openID,
			OutTradeNo: outTradeNo,
			RawPayload: string(plain),
			Processed:  true,
		})
		if insErr != nil && !errors.Is(insErr, model.ErrXPayMessageDuplicated) {
			// 业务已成功，日志失败仅打告警；不要让"日志失败"覆盖业务成功
			log.Printf("[WXMsg] log insert err: event=%s outTradeNo=%s err=%v", event, outTradeNo, insErr)
		}
	}
	return event, outTradeNo, openID, nil
}

// dispatchXPayEvent 按事件类型分发。
//
// 当前实现：
//   - xpay_goods_deliver_notify：道具支付完成 → applyVirtualPayOrderSuccess（认证费工单推进）
//   - xpay_refund_notify：退款 → markVirtualPayOrderRefunded（详细回滚等业务确认后补，见历史遗留 3.3）
//   - 其它事件（coin_pay_notify / complaint_notify）：仅记录，不做业务动作
func dispatchXPayEvent(event, outTradeNo, openID string, cfg *config.Config, deps VirtualPayDeps) error {
	switch event {
	case "xpay_goods_deliver_notify":
		if outTradeNo == "" {
			return errors.New("goods_deliver_notify missing OutTradeNo")
		}
		if err := applyVirtualPayOrderSuccess(deps, outTradeNo); err != nil {
			return err
		}
		meta, err := deps.UserOrderModel.GetVirtualPayOrderMeta(outTradeNo)
		if err != nil {
			return err
		}
		if meta.OpenID == "" {
			meta.OpenID = openID
		}
		return reportVirtualPayGoodsDelivered(context.Background(), cfg, deps, meta, "")
	case "xpay_refund_notify":
		if outTradeNo == "" {
			return errors.New("refund_notify missing OutTradeNo")
		}
		return markVirtualPayOrderRefunded(deps, outTradeNo)
	case "xpay_coin_pay_notify":
		// 代币模式（充值）：微信支付成功后推送此事件，触发发货与 goods_deliver_notify 对齐。
		if outTradeNo == "" {
			return errors.New("coin_pay_notify missing OutTradeNo")
		}
		return applyVirtualPayOrderSuccess(deps, outTradeNo)
	case "xpay_complaint_notify":
		// 投诉：当前无运营对接，仅落日志由后台告警关注
		log.Printf("[WXMsg] complaint received: outTradeNo=%s", outTradeNo)
		return nil
	default:
		log.Printf("[WXMsg] unknown event: %s outTradeNo=%s", event, outTradeNo)
		return nil
	}
}

// markVirtualPayOrderRefunded 退款收尾：订单落 refunded 终态 + 会员整单回收。
//
// 原"最小处理"（仅改状态）升级为 SettleVirtualPayRefundCompleted：
// 退款在微信侧确认完成后，该订单授予的会员同步回收（T8 策略：整单回收）；
// 会员已被后续订单续期时跳过并留日志，由运营人工评估。
func markVirtualPayOrderRefunded(deps VirtualPayDeps, orderNo string) error {
	if deps.UserOrderModel == nil {
		return errors.New("UserOrderModel not injected")
	}
	return SettleVirtualPayRefundCompleted(deps, orderNo)
}
