package route

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"io"
	"log"
	"math"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"service/model"
	"service/tracking"
)

const studioMediaMaxBytes = int64(50 << 20)

var studioMediaWriteSlots = make(chan struct{}, 2)

type studioMediaInspection struct {
	Kind, MIME      string
	Width, Height   int
	DurationSeconds float64
}

func inspectStudioMedia(ctx context.Context, data []byte, name string) (studioMediaInspection, error) {
	info := studioMediaInspection{MIME: http.DetectContentType(data)}
	if len(data) == 0 || int64(len(data)) > studioMediaMaxBytes {
		return info, errors.New("素材为空或超过50MB")
	}
	if strings.HasPrefix(info.MIME, "image/") {
		if len(data) > 10<<20 {
			return info, errors.New("图片不能超过10MB")
		}
		config, _, err := image.DecodeConfig(bytes.NewReader(data))
		if err != nil || config.Width < 1 || config.Height < 1 || int64(config.Width)*int64(config.Height) > 40000000 {
			return info, errors.New("图片尺寸或结构无效")
		}
		if _, _, err = image.Decode(bytes.NewReader(data)); err != nil {
			return info, errors.New("图片不完整")
		}
		info.Kind = "image"
		info.Width = config.Width
		info.Height = config.Height
		return info, nil
	}
	// Reject playlists/documents before ffprobe can interpret a nested input.
	switch info.MIME {
	case "video/mp4", "audio/mpeg", "audio/wave", "audio/wav", "audio/x-wav", "application/ogg", "audio/ogg", "audio/flac":
	default:
		return info, errors.New("支持图片、MP4、MP3、WAV、OGG和FLAC素材")
	}
	probe, err := exec.LookPath("ffprobe")
	if err != nil {
		return info, errors.New("媒体时长探测服务尚未配置")
	}
	file, err := os.CreateTemp("", "studio-media-*")
	if err != nil {
		return info, errors.New("媒体校验暂不可用")
	}
	tmp := file.Name()
	defer os.Remove(tmp)
	if _, err = file.Write(data); err != nil {
		file.Close()
		return info, errors.New("媒体读取失败")
	}
	if err = file.Close(); err != nil {
		return info, errors.New("媒体读取失败")
	}
	probeCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	command := exec.CommandContext(probeCtx, probe, "-v", "error", "-protocol_whitelist", "file,pipe", "-show_entries", "format=duration:stream=codec_type,width,height", "-of", "json", tmp)
	output, err := command.Output()
	if err != nil {
		return info, errors.New("媒体不完整或无法探测时长")
	}
	var result struct {
		Format struct {
			Duration string `json:"duration"`
		} `json:"format"`
		Streams []struct {
			CodecType     string `json:"codec_type"`
			Width, Height int
		} `json:"streams"`
	}
	if json.Unmarshal(output, &result) != nil {
		return info, errors.New("媒体探测结果无效")
	}
	info.DurationSeconds, err = strconv.ParseFloat(result.Format.Duration, 64)
	if err != nil || math.IsNaN(info.DurationSeconds) || math.IsInf(info.DurationSeconds, 0) || info.DurationSeconds <= 0 {
		return info, errors.New("媒体时长无效")
	}
	for _, stream := range result.Streams {
		if stream.CodecType == "video" && stream.Width > 0 && stream.Height > 0 {
			info.Kind = "video"
			info.Width = stream.Width
			info.Height = stream.Height
			break
		}
		if stream.CodecType == "audio" {
			info.Kind = "audio"
		}
	}
	if info.Kind == "" {
		return info, errors.New("素材没有可用音视频轨道")
	}
	_ = name // Client filename is presentation only; actual bytes establish kind.
	return info, nil
}

func studioMediaSignedURL(id string, expires time.Time) (string, error) {
	if len(strings.TrimSpace(os.Getenv("TRACKING_MEDIA_SIGN_KEY"))) < 32 || !validStoredMediaID(id) {
		return "", errors.New("素材安全访问尚未配置")
	}
	exp := expires.Unix()
	return "https://api.jiadilingguang.com/api/v1/ai-video-studio-media/" + id + "?exp=" + strconv.FormatInt(exp, 10) + "&sig=" + trackingMediaSignature("studio:"+id, exp), nil
}

func registerAIVideoStudioMediaContentRoute(v1 *gin.RouterGroup, media *model.AIVideoStudioMediaStore, svc *tracking.Service) {
	v1.Match([]string{http.MethodGet, http.MethodHead}, "/ai-video-studio-media/:id", func(c *gin.Context) {
		id := c.Param("id")
		if len(strings.TrimSpace(os.Getenv("TRACKING_MEDIA_SIGN_KEY"))) < 32 || !validStoredMediaID(id) || !trackingMediaSignatureValid("studio:"+id, c.Query("exp"), c.Query("sig")) {
			studioError(c, 403, "素材访问凭证无效或已过期")
			return
		}
		item, err := media.GetSigned(c.Request.Context(), id)
		if err != nil {
			studioError(c, 404, "素材不可访问")
			return
		}
		c.Header("Cache-Control", "private, no-store")
		serveContentMediaBytes(c, svc, tracking.Asset{ID: item.StorageKey, Kind: item.Kind, MIME: item.MIME, Width: item.Width, Height: item.Height})
	})
}

func registerAIVideoStudioMediaRoutes(g *gin.RouterGroup, store *model.AIVideoStudioStore, media *model.AIVideoStudioMediaStore, svc *tracking.Service, users *model.UserModel) {
	g.GET("/media/:id", func(c *gin.Context) {
		if !validStoredMediaID(c.Param("id")) {
			studioError(c, 404, "素材不可访问")
			return
		}
		item, err := media.Get(c.Request.Context(), GetTokenUserID(c), c.Param("id"))
		if errors.Is(err, sql.ErrNoRows) {
			studioError(c, 404, "素材不可访问")
			return
		}
		if err != nil {
			studioMediaFailure(c, "metadata", err)
			return
		}
		item.URL, err = studioMediaSignedURL(item.ID, time.Now().Add(10*time.Minute))
		if err != nil {
			studioMediaFailure(c, "metadata_sign", err)
			return
		}
		studioOK(c, item)
	})
	g.GET("/media", func(c *gin.Context) {
		page, size := studioPage(c)
		kind := c.Query("media_type")
		if kind == "" {
			kind = c.Query("kind")
		}
		if kind != "" && kind != "image" && kind != "video" && kind != "audio" {
			studioError(c, 400, "素材类型不支持")
			return
		}
		items, total, err := media.List(c.Request.Context(), GetTokenUserID(c), kind, c.Query("keyword"), size, (page-1)*size)
		if err != nil {
			studioMediaFailure(c, "list", err)
			return
		}
		for _, item := range items {
			item.URL, _ = studioMediaSignedURL(item.ID, time.Now().Add(10*time.Minute))
		}
		studioOK(c, gin.H{"items": items, "total": total, "page": page, "page_size": size})
	})
	g.POST("/media/uploads", func(c *gin.Context) {
		select {
		case studioMediaWriteSlots <- struct{}{}:
			defer func() { <-studioMediaWriteSlots }()
		default:
			studioError(c, 429, "素材处理繁忙，请稍后重试")
			return
		}
		if !contentMediaConfigured(svc) {
			studioError(c, 503, "素材存储尚未配置")
			return
		}
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, studioMediaMaxBytes+(1<<20))
		if err := c.Request.ParseMultipartForm(1 << 20); err != nil {
			studioError(c, 413, "素材上传超过限制或格式无效")
			return
		}
		defer c.Request.MultipartForm.RemoveAll()
		if rights := c.PostForm("rights"); rights != "own" && rights != "authorized" {
			studioError(c, 400, "请声明素材权属")
			return
		}
		file, header, err := c.Request.FormFile("file")
		if err != nil {
			studioError(c, 400, "请选择素材文件")
			return
		}
		defer file.Close()
		data, err := io.ReadAll(io.LimitReader(file, studioMediaMaxBytes+1))
		if err != nil || int64(len(data)) > studioMediaMaxBytes {
			studioError(c, 413, "素材读取失败或超过50MB")
			return
		}
		info, err := inspectStudioMedia(c.Request.Context(), data, header.Filename)
		if err != nil {
			studioError(c, 422, err.Error())
			return
		}
		if info.Kind != "image" && (info.DurationSeconds < 2 || info.DurationSeconds > 15) {
			studioError(c, 422, "参考视频和音频单条须为2–15秒")
			return
		}
		item, err := persistStudioMedia(c.Request.Context(), media, svc, GetTokenUserID(c), strings.ReplaceAll(uuid.NewString(), "-", ""), header.Filename, "", data, info)
		if err != nil {
			studioMediaFailure(c, "upload", err)
			return
		}
		studioOK(c, item)
	})
	g.POST("/media/register", func(c *gin.Context) {
		var input struct {
			MediaID string `json:"media_id"`
		}
		if c.ShouldBindJSON(&input) != nil || !validStoredMediaID(input.MediaID) {
			studioError(c, 400, "素材标识无效")
			return
		}
		scope, err := loadMiniTrackingMaterialScope(c.Request.Context(), users.DB, svc.Config.Tenant, GetTokenUserID(c))
		if err != nil {
			studioError(c, 403, "没有素材访问权限")
			return
		}
		asset, err := svc.Media(c.Request.Context(), &scope, input.MediaID)
		if err != nil {
			studioError(c, 404, "素材不可访问")
			return
		}
		data, _, err := readContentMedia(c.Request.Context(), svc, asset.ID, studioMediaMaxBytes)
		if err != nil {
			studioMediaFailure(c, "import_read", err)
			return
		}
		info, err := inspectStudioMedia(c.Request.Context(), data, asset.Title)
		if err != nil {
			studioError(c, 422, err.Error())
			return
		}
		if info.Kind != "image" && (info.DurationSeconds < 2 || info.DurationSeconds > 15) {
			studioError(c, 422, "参考视频和音频单条须为2–15秒")
			return
		}
		key := sha256.Sum256([]byte(fmt.Sprintf("studio-import:%d:%s", scope.Actor, asset.ID)))
		id := hex.EncodeToString(key[:16])
		if item, e := media.Get(c.Request.Context(), scope.Actor, id); e == nil {
			item.URL, _ = studioMediaSignedURL(id, time.Now().Add(10*time.Minute))
			studioOK(c, item)
			return
		}
		item, err := persistStudioMedia(c.Request.Context(), media, svc, scope.Actor, id, asset.Title, "", data, info)
		if err != nil {
			studioMediaFailure(c, "import", err)
			return
		}
		studioOK(c, item)
	})
	g.POST("/results/:id/playback", func(c *gin.Context) {
		status, raw, stableID, err := studioResult(c.Request.Context(), store, GetTokenUserID(c), c.Param("id"))
		if err != nil {
			studioError(c, 404, "成片不可访问")
			return
		}
		if status != "completed" && status != "succeeded" {
			studioError(c, 409, "该任务尚未交付成片")
			return
		}
		if stableID != "" {
			item, e := media.Get(c.Request.Context(), GetTokenUserID(c), stableID)
			if e != nil {
				studioMediaFailure(c, "playback", e)
				return
			}
			link, e := studioMediaSignedURL(item.ID, time.Now().Add(10*time.Minute))
			if e != nil {
				studioMediaFailure(c, "sign", e)
				return
			}
			studioOK(c, gin.H{"url": link, "delivery_type": "retained", "retention_status": "retained", "retained": true, "media_id": item.ID, "expires_at": time.Now().Add(10 * time.Minute)})
			return
		}
		if !safeStudioRemoteURL(raw) {
			studioError(c, 409, "成片地址暂不可用，请稍后刷新")
			return
		}
		studioOK(c, gin.H{"url": raw, "delivery_type": "direct", "retention_status": "not_retained", "retained": false})
	})
	g.POST("/results/:id/retain", func(c *gin.Context) {
		userID, taskID := GetTokenUserID(c), c.Param("id")
		status, raw, _, err := studioResult(c.Request.Context(), store, userID, taskID)
		if err != nil {
			studioError(c, 404, "成片不可访问")
			return
		}
		if status != "completed" && status != "succeeded" {
			studioError(c, 409, "该任务尚未交付成片")
			return
		}
		if item, e := media.BySource(c.Request.Context(), userID, taskID); e == nil {
			if e = attachStudioRetainedMedia(c.Request.Context(), store, userID, taskID, item.ID); e != nil {
				studioMediaFailure(c, "retain_attach", e)
				return
			}
			item.URL, _ = studioMediaSignedURL(item.ID, time.Now().Add(10*time.Minute))
			studioOK(c, studioRetainedResponse(item))
			return
		} else if !errors.Is(e, sql.ErrNoRows) {
			studioMediaFailure(c, "retained_lookup", e)
			return
		}
		ctx, cancel := context.WithTimeout(c.Request.Context(), 100*time.Second)
		defer cancel()
		select {
		case studioMediaWriteSlots <- struct{}{}:
			defer func() { <-studioMediaWriteSlots }()
		default:
			studioError(c, 429, "素材处理繁忙，请稍后重试")
			return
		}
		data, err := downloadStudioResult(ctx, raw)
		if err != nil {
			studioMediaFailure(c, "retain_download", err)
			return
		}
		info, err := inspectStudioMedia(ctx, data, "generated.mp4")
		if err != nil || info.Kind != "video" {
			studioError(c, 422, "成片文件不完整，留存未完成")
			return
		}
		hash := sha256.Sum256([]byte(fmt.Sprintf("studio-result:%d:%s", userID, taskID)))
		item, err := persistStudioMedia(ctx, media, svc, userID, hex.EncodeToString(hash[:16]), "AI视频成片", taskID, data, info)
		if err != nil {
			if existing, e := media.BySource(ctx, userID, taskID); e == nil {
				item = existing
			} else {
				studioMediaFailure(c, "retain", err)
				return
			}
		}
		if err = attachStudioRetainedMedia(ctx, store, userID, taskID, item.ID); err != nil {
			studioMediaFailure(c, "retain_attach", err)
			return
		}
		studioOK(c, studioRetainedResponse(item))
	})
}

type studioRetainedMediaResponse struct {
	*model.AIVideoStudioMedia
	Retained        bool   `json:"retained"`
	RetentionStatus string `json:"retention_status"`
	DeliveryType    string `json:"delivery_type"`
}

func studioRetainedResponse(item *model.AIVideoStudioMedia) studioRetainedMediaResponse {
	return studioRetainedMediaResponse{item, true, "retained", "retained"}
}

func attachStudioRetainedMedia(ctx context.Context, store *model.AIVideoStudioStore, userID int64, taskID, mediaID string) error {
	result, err := store.DB.ExecContext(ctx, `UPDATE ai_video_studio_tasks SET stable_media_id=?,delivery_type='retained' WHERE id=? AND user_id=? AND status IN ('completed','succeeded') AND (stable_media_id='' OR stable_media_id=?)`, mediaID, taskID, userID, mediaID)
	if err != nil {
		return err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed == 1 {
		return nil
	}
	// MySQL may report zero changed rows for a repeated identical attachment.
	// Re-read under the same owner rather than treating every no-op as success.
	status, _, existing, err := studioResult(ctx, store, userID, taskID)
	if err != nil {
		return err
	}
	if existing == mediaID && (status == "completed" || status == "succeeded") {
		return nil
	}
	return model.ErrAIVideoStudioConflict
}

func studioResult(ctx context.Context, store *model.AIVideoStudioStore, userID int64, id string) (status, raw, stableID string, err error) {
	err = store.DB.QueryRowContext(ctx, `SELECT status,COALESCE(result_url,''),stable_media_id FROM ai_video_studio_tasks WHERE id=? AND user_id=?`, id, userID).Scan(&status, &raw, &stableID)
	return
}

func persistStudioMedia(ctx context.Context, store *model.AIVideoStudioMediaStore, svc *tracking.Service, userID int64, id, name, source string, data []byte, info studioMediaInspection) (*model.AIVideoStudioMedia, error) {
	digest := sha256.Sum256(data)
	if err := storeContentMedia(ctx, svc, id, data, info.MIME); err != nil {
		return nil, err
	}
	actual, _, err := readContentMedia(ctx, svc, id, studioMediaMaxBytes)
	if err != nil {
		return nil, err
	}
	if sha256.Sum256(actual) != digest {
		return nil, errors.New("媒体留存完整性校验失败")
	}
	name = filepath.Base(strings.ReplaceAll(name, "\\", "/"))
	if len([]rune(name)) > 200 {
		name = string([]rune(name)[:200])
	}
	if name == "" || name == "." {
		name = "创作素材"
	}
	item := &model.AIVideoStudioMedia{ID: id, UserID: userID, OwnerID: "designer-" + strconv.FormatInt(userID, 10), Kind: info.Kind, MIME: info.MIME, SizeBytes: int64(len(data)), DurationSeconds: info.DurationSeconds, StorageKey: id, Status: "ready", SourceTaskID: source, SHA256: hex.EncodeToString(digest[:]), Width: info.Width, Height: info.Height, Name: name}
	if err := store.Insert(ctx, item); err != nil {
		return nil, err
	}
	item.URL, _ = studioMediaSignedURL(id, time.Now().Add(10*time.Minute))
	return item, nil
}

func safeStudioRemoteURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || (u.Port() != "" && u.Port() != "443") {
		return false
	}
	host := strings.TrimSuffix(strings.ToLower(u.Hostname()), ".")
	if host == "localhost" || strings.HasSuffix(host, ".localhost") || strings.HasSuffix(host, ".local") || !strings.Contains(host, ".") {
		return false
	}
	if address, err := netip.ParseAddr(host); err == nil {
		return tracking.IsPublicImportAddress(address)
	}
	return true
}

func downloadStudioResult(ctx context.Context, raw string) ([]byte, error) {
	if !safeStudioRemoteURL(raw) {
		return nil, errors.New("成片地址不符合安全访问要求")
	}
	dialer := &net.Dialer{Timeout: 10 * time.Second, Control: func(_, address string, _ syscall.RawConn) error {
		host, _, err := net.SplitHostPort(address)
		if err != nil {
			return errors.New("成片地址无效")
		}
		ip, err := netip.ParseAddr(host)
		if err != nil || !tracking.IsPublicImportAddress(ip) {
			return errors.New("成片地址不可访问")
		}
		return nil
	}}
	transport := &http.Transport{Proxy: nil, DialContext: dialer.DialContext, TLSHandshakeTimeout: 10 * time.Second, ResponseHeaderTimeout: 20 * time.Second, DisableCompression: true}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 90 * time.Second, CheckRedirect: func(r *http.Request, via []*http.Request) error {
		if len(via) >= 3 || !safeStudioRemoteURL(r.URL.String()) {
			return errors.New("成片跳转地址不可访问")
		}
		return nil
	}}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if err != nil {
		return nil, errors.New("成片地址无效")
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, errors.New("成片留存下载失败，可稍后重试")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, errors.New("成片地址已失效或暂时不可下载")
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, studioMediaMaxBytes+1))
	if err != nil || int64(len(data)) > studioMediaMaxBytes {
		return nil, errors.New("成片下载不完整或超过50MB")
	}
	return data, nil
}

func studioMediaFailure(c *gin.Context, operation string, err error) {
	log.Printf("[ai-video-studio] operation=media_%s actor=%d error_type=%T", operation, GetTokenUserID(c), err)
	if errors.Is(err, model.ErrAIVideoStudioConflict) {
		studioError(c, 409, "任务状态已更新，请刷新后重试")
		return
	}
	studioError(c, 503, "素材操作未完成，请稍后重试")
}
