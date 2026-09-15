package reportwecom

import (
	"context"
	"encoding/xml"
	"io"
	"log"
	"net/http"
	"service/component/virtualpay"
	"strconv"
	"time"
)

// The query signature and encrypted recipient are both checked before parsing events.
func Callback(config Config, service *Service) http.Handler {
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
		if err := service.HandleEvent(ctx, event); err != nil {
			log.Printf("[report-wecom] warn callback=not_applied")
			http.Error(w, "event not applied", 503)
			return
		}
		_, _ = w.Write([]byte("success"))
	})
}
