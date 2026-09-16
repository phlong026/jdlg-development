package reportwecom

import (
	"context"
	"encoding/xml"
	"errors"
	"io"
	"log"
	"net/http"
	"service/component/virtualpay"
	"strconv"
	"time"
)

// EventSink 由宿主（route 层）注入，在应答企微前持久化解密后的事件，
// 并接管重试：报告业务失败或下游 CRM 转发失败由持久化行补偿，
// 不再依赖企微超时重推。
type EventSink interface {
	// Record 保存原始事件（密文+明文）。duplicate=true 表示重复推送，直接应答成功。
	Record(ctx context.Context, plain []byte, encrypted string, e Event) (dedupKey string, duplicate bool, err error)
	// Settled 记录内联报告处理结果；permanent=true 的失败不再重试。
	Settled(dedupKey string, handleErr error, permanent bool)
}

// PermanentEventError 判断报告处理错误是否值得重试：
// 非本员工/未知 State/过期票据属于"不属于报告业务或已失效"，
// 重试无意义；其余（存储、企微接口）视为暂时失败。
func PermanentEventError(err error) bool {
	return errors.Is(err, ErrNotOwner) || errors.Is(err, ErrExpired) || errors.Is(err, ErrUnionIDRequired)
}

// The query signature and encrypted recipient are both checked before parsing events.
func Callback(config Config, service *Service, sinks ...EventSink) http.Handler {
	var sink EventSink
	for _, s := range sinks {
		if s != nil {
			sink = s
			break
		}
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if !config.Enabled || config.Token == "" || len(config.AESKey) != 43 || config.CorpID == "" || service == nil {
			http.Error(w, "unavailable", 503)
			return
		}
		q := r.URL.Query()
		timestamp, err := strconv.ParseInt(q.Get("timestamp"), 10, 64)
		delta := time.Now().Unix() - timestamp
		if err != nil || delta > 300 || delta < -300 || q.Get("nonce") == "" {
			http.Error(w, "invalid request", 403)
			return
		}
		encrypted := q.Get("echostr")
		if r.Method == http.MethodPost {
			b, err := io.ReadAll(io.LimitReader(r.Body, (128<<10)+1))
			var envelope struct {
				Encrypt string `xml:"Encrypt"`
			}
			if err != nil || len(b) > 128<<10 || xml.Unmarshal(b, &envelope) != nil {
				http.Error(w, "invalid request", 400)
				return
			}
			encrypted = envelope.Encrypt
		} else if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", 405)
			return
		}
		if encrypted == "" || len(encrypted) > 128<<10 || !virtualpay.VerifyPushSignature(config.Token, q.Get("timestamp"), q.Get("nonce"), encrypted, q.Get("msg_signature")) {
			http.Error(w, "invalid signature", 403)
			return
		}
		plain, err := virtualpay.DecryptPushMessage(config.AESKey, encrypted, config.CorpID)
		if err != nil {
			http.Error(w, "invalid message", 403)
			return
		}
		if r.Method == http.MethodGet {
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			_, _ = w.Write(plain)
			return
		}
		var event Event
		if xml.Unmarshal(plain, &event) != nil {
			http.Error(w, "invalid event", 400)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 4*time.Second)
		defer cancel()
		var dedupKey string
		if sink != nil {
			var duplicate bool
			var recordErr error
			dedupKey, duplicate, recordErr = sink.Record(ctx, plain, encrypted, event)
			if recordErr != nil {
				// 事件未落库：让企微重推，保证不丢事件
				log.Printf("[report-wecom] warn callback=event_not_persisted")
				http.Error(w, "event not persisted", 503)
				return
			}
			if duplicate {
				_, _ = w.Write([]byte("success"))
				return
			}
		}
		handleErr := service.HandleEvent(ctx, event)
		if sink != nil {
			// 事件已持久化，重试由 inbox 补偿完成，无需企微重推
			sink.Settled(dedupKey, handleErr, handleErr != nil && PermanentEventError(handleErr))
			if handleErr != nil {
				log.Printf("[report-wecom] warn callback=not_applied")
			}
			_, _ = w.Write([]byte("success"))
			return
		}
		if handleErr != nil {
			// 无持久化兜底时保持旧行为：让企微重推
			log.Printf("[report-wecom] warn callback=not_applied")
			http.Error(w, "event not applied", 503)
			return
		}
		_, _ = w.Write([]byte("success"))
	})
}
