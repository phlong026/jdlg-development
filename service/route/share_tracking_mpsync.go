package route

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"service/tracking"
)

// mpSyncSettings is the official-account sync configuration. Everything defaults
// to off, and a partly configured sync stays off rather than guessing.
type mpSyncSettings struct {
	Enabled  bool
	Config   tracking.MPSyncConfig
	Secret   string
	Actor    int64
	Interval time.Duration // 0: run only when an operator asks
	Problems []string
}

func loadMPSyncSettings(getenv func(string) string) mpSyncSettings {
	var s mpSyncSettings
	if getenv("TRACKING_MP_SYNC_ENABLED") != "true" {
		return s
	}
	bad := func(format string) { s.Problems = append(s.Problems, format) }
	s.Config = tracking.MPSyncConfig{AppID: strings.TrimSpace(getenv("TRACKING_MP_APPID")), Owner: strings.TrimSpace(getenv("TRACKING_MP_OWNER")), Rights: getenv("TRACKING_MP_RIGHTS")}
	s.Secret = getenv("TRACKING_MP_SECRET")
	if s.Config.AppID == "" || s.Secret == "" {
		bad("TRACKING_MP_APPID 与 TRACKING_MP_SECRET 须同时配置（公众号自己的凭据，不能用小程序的）")
	}
	if s.Config.Owner == "" {
		bad("TRACKING_MP_OWNER 未配置：同步的草稿须有明确的跟进归属")
	}
	// The rights declaration is recorded on every draft and in the audit trail, so
	// it must be made deliberately: a default would assert "own" for articles
	// nobody has vouched for.
	switch s.Config.Rights {
	case "own", "authorized":
	case "":
		bad("TRACKING_MP_RIGHTS 未配置：须明确声明对这些文章的使用权，own（自有）或 authorized（已获授权）")
	default:
		bad("TRACKING_MP_RIGHTS 只能是 own 或 authorized")
	}
	actor, err := strconv.ParseInt(strings.TrimSpace(getenv("TRACKING_MP_ACTOR")), 10, 64)
	if err != nil || actor <= 0 {
		bad("TRACKING_MP_ACTOR 须是已授权素材管理的后台账号 ID")
	}
	s.Actor = actor
	s.Config.MaxArticles = 30
	if raw := getenv("TRACKING_MP_MAX_ARTICLES"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > 200 {
			bad("TRACKING_MP_MAX_ARTICLES 须在 1 到 200 之间")
		} else {
			s.Config.MaxArticles = n
		}
	}
	if getenv("TRACKING_MP_IMPORT_IMAGES") == "true" {
		s.Config.ImportImages = true
	}
	if raw := getenv("TRACKING_MP_SYNC_INTERVAL_MINUTES"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || (n != 0 && (n < 15 || n > 1440)) {
			bad("TRACKING_MP_SYNC_INTERVAL_MINUTES 须为 0（仅手动）或 15 到 1440")
		} else {
			s.Interval = time.Duration(n) * time.Minute
		}
	}
	s.Enabled = len(s.Problems) == 0
	return s
}

var errMPSyncBusy = errors.New("official account sync already running")

// mpSyncRunner runs one sync at a time in this process. Several processes may
// run it; the engine is idempotent, so that costs duplicate work, not duplicates.
type mpSyncRunner struct {
	svc        *tracking.Service
	settings   mpSyncSettings
	source     tracking.MPSource
	fetcher    tracking.MediaFetcher
	scopeFor   func(ctx context.Context) (tracking.Scope, error)
	mediaStore func(tracking.Scope) tracking.MediaStore

	mu      sync.Mutex
	running bool
	lastRun time.Time
}

// manualRunGap is the least time between two operator-triggered runs.
const manualRunGap = time.Minute

func (m *mpSyncRunner) Run(ctx context.Context, manual bool) (tracking.MPSyncResult, error) {
	var empty tracking.MPSyncResult
	m.mu.Lock()
	if m.running {
		m.mu.Unlock()
		return empty, errMPSyncBusy
	}
	if manual && !m.lastRun.IsZero() && time.Since(m.lastRun) < manualRunGap {
		m.mu.Unlock()
		return empty, &tracking.Error{Status: 429, Message: "同步刚刚运行过，请稍后再试"}
	}
	m.running, m.lastRun = true, time.Now()
	m.mu.Unlock()
	defer func() { m.mu.Lock(); m.running = false; m.mu.Unlock() }()

	scope, err := m.scopeFor(ctx)
	if err != nil {
		return empty, err
	}
	var store tracking.MediaStore
	if m.settings.Config.ImportImages && m.mediaStore != nil {
		store = m.mediaStore(scope)
	}
	return m.svc.SyncOfficialAccount(ctx, scope, m.settings.Config, m.source, m.fetcher, store)
}

// Loop runs a sync every interval until ctx ends. It starts after a short random
// delay so several processes do not all call WeChat at the same instant.
func (m *mpSyncRunner) Loop(ctx context.Context) {
	if m.settings.Interval <= 0 {
		return
	}
	var jitter [1]byte
	_, _ = rand.Read(jitter[:])
	timer := time.NewTimer(30*time.Second + time.Duration(jitter[0])*time.Second/4)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
		func() {
			defer func() {
				if recovered := recover(); recovered != nil {
					log.Printf("[Tracking] official account sync panicked: %v", recovered)
				}
			}()
			runCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
			defer cancel()
			res, err := m.Run(runCtx, false)
			switch {
			case errors.Is(err, errMPSyncBusy):
			case err != nil:
				var wechat *tracking.WechatError
				if errors.As(err, &wechat) {
					log.Printf("[Tracking] official account sync failed: wechat errcode %d", wechat.Code)
				} else {
					log.Printf("[Tracking] official account sync failed: %s", trackingErrorLabel(err))
				}
			default:
				log.Printf("[Tracking] official account sync: fetched=%d created=%d updated=%d unchanged=%d skipped=%d conflicts=%d failed=%d", res.Counts.Fetched, res.Counts.Created, res.Counts.Updated, res.Counts.Unchanged, res.Counts.Skipped, res.Counts.Conflicts, res.Counts.Failed)
			}
		}()
		timer.Reset(m.settings.Interval)
	}
}

// trackingErrorLabel names a failure for the log without echoing its detail.
func trackingErrorLabel(err error) string {
	var known *tracking.Error
	if errors.As(err, &known) {
		return known.Message
	}
	return "unavailable"
}

// trackingMPMediaStore validates and stores one downloaded picture exactly as an
// operator's upload does: sniffed type, decoded dimensions, a private random file
// name, and a media record under the owner. Video is never imported.
func trackingMPMediaStore(svc *tracking.Service, scope tracking.Scope) tracking.MediaStore {
	return func(ctx context.Context, owner string, data []byte, name string) (tracking.Asset, error) {
		var asset tracking.Asset
		root, err := trackingMediaRoot()
		if err != nil {
			return asset, err
		}
		sniff := data
		if len(sniff) > 512 {
			sniff = sniff[:512]
		}
		mime := http.DetectContentType(sniff)
		if (mime != "image/png" && mime != "image/jpeg" && mime != "image/gif") || len(data) == 0 || len(data) > 10<<20 {
			return asset, tracking.ErrUnavailable
		}
		decoded, _, err := image.DecodeConfig(bytes.NewReader(data))
		if err != nil || decoded.Width < 1 || decoded.Height < 1 || decoded.Width > 20000 || decoded.Height > 20000 {
			return asset, tracking.ErrUnavailable
		}
		random := make([]byte, 24)
		if _, err = rand.Read(random); err != nil {
			return asset, err
		}
		id := base64.RawURLEncoding.EncodeToString(random)
		path := filepath.Join(root, id)
		file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			return asset, err
		}
		_, writeErr := file.Write(data)
		closeErr := file.Close()
		if writeErr != nil || closeErr != nil {
			_ = os.Remove(path)
			return asset, tracking.ErrUnavailable
		}
		asset = tracking.Asset{ID: id, Kind: "image", URL: "/api/v1/miniprogram/tracking/media/" + id, Width: decoded.Width, Height: decoded.Height, MIME: mime, Title: name}
		if err = svc.AddMedia(ctx, scope, asset, owner); err != nil {
			_ = os.Remove(path)
			return tracking.Asset{}, err
		}
		return asset, nil
	}
}

// mpSyncVisible says whether the caller may see and trigger the sync.
func mpSyncVisible(settings mpSyncSettings, scope tracking.Scope) bool {
	return settings.Enabled && scope.Manage && scope.Allows(settings.Config.Owner)
}

func registerMPSyncRoutes(group *gin.RouterGroup, svc *tracking.Service, runner *mpSyncRunner, settings mpSyncSettings, scopeOf func(*gin.Context) tracking.Scope) {
	group.GET("/mp-sync", func(c *gin.Context) {
		scope := scopeOf(c)
		if !scope.Manage {
			trackingResponse(c, nil, &tracking.Error{Status: 403, Message: "无素材管理权限"})
			return
		}
		if !settings.Enabled && len(settings.Problems) > 0 {
			// Configuration mistakes contain no credential, and the operator who
			// enabled the sync needs to see them.
			trackingResponse(c, gin.H{"enabled": false, "problems": settings.Problems}, nil)
			return
		}
		if !mpSyncVisible(settings, scope) {
			trackingResponse(c, gin.H{"enabled": false}, nil)
			return
		}
		state, found, err := svc.MPSyncStatus(c.Request.Context(), scope, settings.Config.Owner)
		info := gin.H{"enabled": true, "owner_id": settings.Config.Owner, "rights": settings.Config.Rights, "max_articles": settings.Config.MaxArticles, "import_images": settings.Config.ImportImages, "interval_minutes": int(settings.Interval / time.Minute), "publishes": false}
		if found {
			info["last_run"] = state
		}
		trackingResponse(c, info, err)
	})
	group.POST("/mp-sync", func(c *gin.Context) {
		scope := scopeOf(c)
		if !mpSyncVisible(settings, scope) || runner == nil {
			trackingResponse(c, nil, &tracking.Error{Status: 403, Message: "公众号同步未启用或无权限"})
			return
		}
		ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Minute)
		defer cancel()
		result, err := runner.Run(ctx, true)
		var wechat *tracking.WechatError
		switch {
		case errors.Is(err, errMPSyncBusy):
			err = &tracking.Error{Status: 409, Message: "同步正在运行"}
		case errors.As(err, &wechat):
			log.Printf("[Tracking] official account sync: wechat errcode %d", wechat.Code)
			err = &tracking.Error{Status: 502, Message: "公众号接口返回错误（错误码 " + strconv.Itoa(wechat.Code) + "），请核对账号是否已认证并具备发布接口权限"}
		}
		trackingResponse(c, result, err)
	})
}
