package route

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"service/contentimport"
	"service/model"
	"service/tracking"
)

var generationTaskNoPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{8,64}$`)
var generationSaveKeyPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{8,128}$`)

// generationResultImages 只提取"生成结果图"：images / image_urls / image_url。
// 刻意不含 reference_images 与 original_image——那是用户上传的参考图与模板原图，
// 属于任务输入而非作品输出，不得混入可发布内容。
func generationResultImages(payload map[string]interface{}) []string {
	var urls []string
	for _, key := range []string{"images", "image_urls"} {
		if list, ok := payload[key].([]interface{}); ok {
			for _, item := range list {
				if value, ok := item.(string); ok && value != "" {
					urls = append(urls, value)
				}
			}
		}
	}
	if len(urls) == 0 {
		if value, ok := payload["image_url"].(string); ok && value != "" {
			urls = append(urls, value)
		}
	}
	seen := map[string]bool{}
	unique := urls[:0]
	for _, value := range urls {
		if !seen[value] {
			seen[value] = true
			unique = append(unique, value)
		}
	}
	return unique
}

// registerGenerationImportRoute 挂载 B1 直连管线：本人已成功的 AI 生成任务 →
// 结果图转存私有媒体 → 新建 article_v2 图文草稿。前端在 generatedetails 一键
// "存为内容"，随后进入 contentedit 继续排版与发布。
func registerGenerationImportRoute(g *gin.RouterGroup, svc *tracking.Service, users *model.UserModel) {
	g.POST("/contents/from-generation", func(c *gin.Context) {
		scope := c.MustGet("workbench.scope").(tracking.Scope)
		var body struct {
			TaskNo    string `json:"task_no"`
			Title     string `json:"title"`
			OwnerID   string `json:"owner_id"`
			Rights    string `json:"rights"`
			ProjectID string `json:"project_id"`
			Key       string `json:"idempotency_key"`
		}
		if c.ShouldBindJSON(&body) != nil || !generationTaskNoPattern.MatchString(body.TaskNo) {
			trackingResponse(c, nil, &tracking.Error{Status: http.StatusBadRequest, Message: "请提供有效的生成任务编号"})
			return
		}
		if body.Key != "" && !generationSaveKeyPattern.MatchString(body.Key) {
			trackingResponse(c, nil, &tracking.Error{Status: http.StatusBadRequest, Message: "保存请求标识无效"})
			return
		}
		if users == nil || users.DB == nil {
			trackingResponse(c, nil, tracking.ErrUnavailable)
			return
		}
		owner := body.OwnerID
		if owner == "" && len(scope.Owners) > 0 {
			owner = scope.Owners[0]
		}
		if owner == "" || !scope.Manage || !scope.Allows(owner) {
			trackingResponse(c, nil, &tracking.Error{Status: http.StatusForbidden, Message: "没有向该归属保存内容的权限"})
			return
		}
		rights := body.Rights
		if rights == "" {
			rights = "own"
		}
		if rights != "own" && rights != "authorized" {
			trackingResponse(c, nil, &tracking.Error{Status: http.StatusBadRequest, Message: "素材版权来源仅支持自有素材或已获授权"})
			return
		}
		task, err := model.NewAITaskModel(users.DB).GetByTaskNo(body.TaskNo)
		if err != nil || task == nil {
			// 不区分"不存在"与"不是本人的任务"，避免探测他人任务号。
			trackingResponse(c, nil, &tracking.Error{Status: http.StatusNotFound, Message: "生成任务不存在"})
			return
		}
		if task.UserID != scope.Actor {
			trackingResponse(c, nil, &tracking.Error{Status: http.StatusNotFound, Message: "生成任务不存在"})
			return
		}
		if task.Status != "success" || !task.ResultPayload.Valid {
			trackingResponse(c, nil, &tracking.Error{Status: http.StatusBadRequest, Message: "生成任务尚未成功，暂无结果可保存"})
			return
		}
		var payload map[string]interface{}
		if err := json.Unmarshal([]byte(task.ResultPayload.String), &payload); err != nil {
			trackingResponse(c, nil, &tracking.Error{Status: http.StatusBadRequest, Message: "生成结果无法解析，请重试"})
			return
		}
		images := generationResultImages(payload)
		if len(images) == 0 {
			trackingResponse(c, nil, &tracking.Error{Status: http.StatusBadRequest, Message: "该任务没有可保存的结果图"})
			return
		}
		if len(images) > 9 {
			images = images[:9]
		}
		if !contentMediaConfigured(svc) {
			trackingResponse(c, nil, tracking.ErrUnavailable)
			return
		}
		ownerScope := scope
		ownerScope.Owners = []string{owner}
		fetcher := contentimport.NewHTTPFetcher()
		mediaIDs := make([]string, 0, len(images))
		for index, rawURL := range images {
			identity, _ := json.Marshal([]string{svc.Config.Tenant, owner, "ai_generation", body.TaskNo, strconv.Itoa(index)})
			sum := sha256.Sum256(identity)
			id := base64.RawURLEncoding.EncodeToString(sum[:24])
			asset, persistErr := persistImportedMedia(c.Request.Context(), svc, ownerScope, persistImportedMediaInput{
				ID: id, Kind: "image", URL: rawURL, Caption: strings.TrimSpace(body.Title),
				DefaultTitle: "AI生成结果图", Max: 10 << 20,
				Fetch:            fetcher.FetchMedia,
				PlatformForFetch: "ai_generation",
				Origin:           "ai-generation:" + body.TaskNo,
				Owner:            owner,
			})
			if persistErr != nil {
				trackingResponse(c, nil, persistErr)
				return
			}
			mediaIDs = append(mediaIDs, asset.ID)
		}
		blocks := make([]tracking.ArticleBlock, 0, len(mediaIDs))
		for _, mediaID := range mediaIDs {
			blocks = append(blocks, tracking.ArticleBlock{Kind: "image", MediaID: mediaID, Caption: ""})
		}
		document, err := json.Marshal(tracking.ArticleDocument{Schema: 1, Blocks: blocks})
		if err != nil {
			trackingResponse(c, nil, tracking.ErrUnavailable)
			return
		}
		title := strings.TrimSpace(body.Title)
		if title == "" {
			title = "AI生成结果 " + body.TaskNo
		}
		if len([]rune(title)) > 80 {
			title = string([]rune(title)[:80])
		}
		draft := tracking.Draft{
			Title: title, Body: string(document), BodyFormat: "article_v2",
			Rights: rights, Owner: owner, CoverID: mediaIDs[0], MediaIDs: mediaIDs,
			ProjectID:  body.ProjectID,
			SourceNote: "来自AI生成任务 " + body.TaskNo,
		}
		key := body.Key
		if key == "" {
			// 兼容旧客户端：没有操作键时，同一任务/归属默认返回原草稿。
			digest := sha256.Sum256([]byte(body.TaskNo + "\x00" + owner))
			key = "legacy-generation-" + base64.RawURLEncoding.EncodeToString(digest[:24])
		}
		content, err := svc.SaveContentFromGeneration(c.Request.Context(), ownerScope, body.TaskNo, key, draft)
		if err != nil {
			trackingResponse(c, nil, err)
			return
		}
		trackingResponse(c, gin.H{"content_id": content.ID, "revision": content.Revision, "media_count": len(mediaIDs)}, nil)
	})
}

// generationQuotaSnapshot 读取灵石余额与本月生成数；失败返回 -1 表示"暂不可知"，
// 前端据此隐藏配额条而不是显示误导值。
func generationQuotaSnapshot(ctx context.Context, users *model.UserModel, userID int64) (stones int64, monthGenerations int64) {
	stones, monthGenerations = -1, -1
	if users == nil || users.DB == nil || userID <= 0 {
		return
	}
	if balance, err := users.GetStonesByID(userID); err == nil {
		stones = balance
	}
	if count, err := model.NewAITaskModel(users.DB).CountByUserSince(userID, monthStartUTC()); err == nil {
		monthGenerations = count
	}
	return
}

func monthStartUTC() (t time.Time) {
	now := time.Now().UTC()
	return time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
}
