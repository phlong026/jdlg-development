package route

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"github.com/gin-gonic/gin"
	"log"
	"os"
	"service/aivideostudio"
	"service/model"
	"strings"
	"time"
)

func startStudioWorker(ctx context.Context, s *model.AIVideoStudioStore, media *model.AIVideoStudioMediaStore) func() {
	workerCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	w := model.StudioWorker{Store: s, Provider: aivideostudio.NewMetasoProvider(os.Getenv("AI_VIDEO_STUDIO_METASO_API_KEY")), Build: func(ctx context.Context, t model.StudioWorkerTask) (aivideostudio.GenerationRequest, error) {
		return buildStudioGeneration(ctx, media, t)
	}}
	s.Ready = true
	go func() {
		defer close(done)
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-workerCtx.Done():
				return
			case <-ticker.C:
				if studioProviderConfigured() {
					if e := w.Step(workerCtx); e != nil && workerCtx.Err() == nil {
						log.Printf("[AI视频工作台] worker operation failed")
					}
				}
			}
		}
	}()
	return func() { cancel(); <-done }
}
func handleStudioBatchCreate(c *gin.Context, s *model.AIVideoStudioStore) {
	var in struct {
		QuoteID string `json:"quote_id"`
		Key     string `json:"idempotency_key"`
	}
	if c.ShouldBindJSON(&in) != nil || in.QuoteID == "" || strings.TrimSpace(in.Key) == "" || len(in.Key) > 128 {
		studioError(c, 400, "报价或请求标识不合法")
		return
	}
	uid := GetTokenUserID(c)
	// Replay is available even while credentials/configuration are disabled.
	var id, q string
	e := s.DB.QueryRowContext(c, `SELECT id,quote_id FROM ai_video_studio_batches WHERE user_id=? AND idempotency_key=?`, uid, in.Key).Scan(&id, &q)
	if e == nil {
		if q != in.QuoteID {
			studioError(c, 409, "请求标识已用于其他报价")
			return
		}
	} else {
		if !errors.Is(e, sql.ErrNoRows) {
			studioError(c, 503, "任务暂不可用")
			return
		}
		if !s.Ready || !studioProviderConfigured() {
			studioError(c, 503, "生成服务尚未配置")
			return
		}
		id, _, e = s.CreatePaidBatch(c, uid, in.QuoteID, in.Key)
		if e != nil {
			status := 409
			msg := "报价已失效，请重新报价"
			if errors.Is(e, model.ErrInsufficientStones) {
				status = 402
				msg = "灵石余额不足"
			} else if !errors.Is(e, model.ErrAIVideoStudioConflict) && !errors.Is(e, sql.ErrNoRows) {
				status = 503
				msg = "任务暂不可用"
			}
			studioError(c, status, msg)
			return
		}
	}
	item, e := s.GetBatch(c, uid, id)
	if e != nil {
		studioError(c, 503, "任务已创建，请从记录查看")
		return
	}
	studioOK(c, item)
}

func registerStudioReconciliation(g *gin.RouterGroup, s *model.AIVideoStudioStore) {
	g.POST("/results/:id/reconcile", func(c *gin.Context) {
		var in struct {
			Action   string `json:"action"`
			External string `json:"external_id"`
			Evidence string `json:"evidence"`
		}
		if c.ShouldBindJSON(&in) != nil {
			studioError(c, 400, "处理参数不合法")
			return
		}
		var external, batch string
		if e := s.DB.QueryRowContext(c, `SELECT external_id,batch_id FROM ai_video_studio_tasks WHERE id=?`, c.Param("id")).Scan(&external, &batch); e != nil {
			studioError(c, 404, "任务不存在")
			return
		}
		provider := aivideostudio.NewMetasoProvider(os.Getenv("AI_VIDEO_STUDIO_METASO_API_KEY"))
		if in.Action == "query" {
			if external == "" {
				studioError(c, 409, "该任务尚无供应商标识")
				return
			}
			if _, e := provider.Query(c, external); e != nil {
				studioError(c, 503, "供应商状态暂不可查询")
				return
			}
			if _, e := s.DB.ExecContext(c, `UPDATE ai_video_studio_tasks SET next_poll_at=NULL WHERE id=?`, c.Param("id")); e != nil {
				studioError(c, 503, "查询任务暂不可用")
				return
			}
		} else {
			if in.Action == "attach_external_id" {
				if _, e := provider.Query(c, in.External); e != nil {
					studioError(c, 409, "供应商尚未确认该任务标识")
					return
				}
			}
			if e := s.ReconcileStudioTask(c, c.Param("id"), in.Action, in.External, in.Evidence, int64(GetUserID(c)), GetUsername(c)); e != nil {
				studioError(c, 409, "任务状态或证据不符合处理条件")
				return
			}
		}
		item, e := s.GetBatch(c, 0, batch)
		if e != nil {
			studioError(c, 503, "任务读取失败")
			return
		}
		studioOK(c, item)
	})
}

func buildStudioGeneration(ctx context.Context, media *model.AIVideoStudioMediaStore, t model.StudioWorkerTask) (aivideostudio.GenerationRequest, error) {
	d := t.Snapshot
	if d.Mode == "reference" {
		d.Prompt = d.ReferencePrompt
	}
	r := aivideostudio.GenerationRequest{IdempotencyKey: t.ID, Mode: d.Mode, Prompt: d.Prompt, Ratio: d.Ratio, Resolution: strings.ToUpper(d.Resolution), Duration: d.Duration}
	signed := func(id string) (string, error) {
		if id == "" {
			return "", nil
		}
		m, e := media.Get(ctx, t.UserID, id)
		if e != nil || m.Status != "ready" {
			return "", errors.New("media unavailable")
		}
		return studioMediaSignedURL(id, time.Now().Add(2*time.Hour))
	}
	var e error
	if r.FirstFrameURL, e = signed(d.FirstFrameMediaID); e != nil {
		return r, e
	}
	if r.LastFrameURL, e = signed(d.LastFrameMediaID); e != nil {
		return r, e
	}
	var refs []aiVideoStudioReferenceInput
	if e = json.Unmarshal(d.References, &refs); e != nil {
		return r, e
	}
	for _, ref := range refs {
		m, e := media.Get(ctx, t.UserID, ref.MediaID)
		if e != nil {
			return r, e
		}
		u, e := signed(ref.MediaID)
		if e != nil {
			return r, e
		}
		r.References = append(r.References, aivideostudio.GenerationReference{MediaID: ref.MediaID, Type: ref.MediaType, URL: u, Purpose: ref.Purpose, Position: ref.Position, Duration: m.DurationSeconds})
	}
	return r, aivideostudio.ValidateGenerationRequest(r)
}
