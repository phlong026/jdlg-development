package route

import (
	"database/sql"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"service/model"
)

type favoriteRequest struct {
	TargetType string `json:"target_type"`
	TargetID   int64  `json:"target_id"`
}

type favoriteRef struct {
	TargetType string
	TargetID   int64
	CreatedAt  time.Time
}

// Supported target_type values: template, ai_tool, designer, inspiration.
func RegisterFavoriteRoutes(r *gin.RouterGroup, codeSessionModel *model.CodeSessionRedisModel, userFavoriteModel *model.UserFavoriteModel, templateLikeModel *model.TemplateLikeModel, templateModel *model.TemplateModel, inspirationModel *model.InspirationAssetModel, aiToolModel *model.AIToolModel, userProfileModel *model.UserProfileModel, userDBModel *model.UserModel) {
	favorites := r.Group("/favorites")
	favorites.Use(TokenAuthRequired(codeSessionModel))
	{
		favorites.GET("", func(c *gin.Context) {
			userID := GetTokenUserID(c)
			targetType := model.NormalizeFavoriteTargetType(c.Query("type"))
			if strings.TrimSpace(c.Query("type")) != "" && targetType == "" {
				c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "不支持的收藏分类"})
				return
			}
			page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
			pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
			if page < 1 {
				page = 1
			}
			if pageSize < 1 || pageSize > 50 {
				pageSize = 20
			}

			refs, err := collectFavoriteRefs(userID, targetType, userFavoriteModel, templateLikeModel)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "获取收藏列表失败: " + err.Error()})
				return
			}
			total := len(refs)
			start := (page - 1) * pageSize
			if start >= total {
				c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "success", "data": gin.H{"list": []gin.H{}, "total": total, "page": page, "page_size": pageSize}})
				return
			}
			end := start + pageSize
			if end > total {
				end = total
			}
			list := make([]gin.H, 0, end-start)
			for _, ref := range refs[start:end] {
				card := buildFavoriteCard(ref, templateModel, inspirationModel, aiToolModel, userProfileModel, userDBModel)
				if card != nil {
					list = append(list, card)
				}
			}
			c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "success", "data": gin.H{"list": list, "total": total, "page": page, "page_size": pageSize}})
		})

		favorites.GET("/status", func(c *gin.Context) {
			userID := GetTokenUserID(c)
			targetType := model.NormalizeFavoriteTargetType(c.Query("target_type"))
			targetID, _ := strconv.ParseInt(strings.TrimSpace(c.Query("target_id")), 10, 64)
			if targetType == "" || targetID <= 0 {
				c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "无效的收藏对象"})
				return
			}
			favorited, err := favoriteExists(userID, targetType, targetID, userFavoriteModel, templateLikeModel)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "获取收藏状态失败: " + err.Error()})
				return
			}
			c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "success", "data": gin.H{"favorited": favorited}})
		})

		favorites.POST("", func(c *gin.Context) {
			userID := GetTokenUserID(c)
			req, ok := parseFavoriteRequest(c)
			if !ok {
				return
			}
			if err := validateFavoriteTarget(req.TargetType, req.TargetID, templateModel, inspirationModel, aiToolModel, userProfileModel, userDBModel); err != nil {
				c.JSON(http.StatusNotFound, gin.H{"code": 404, "msg": err.Error()})
				return
			}
			if userFavoriteModel != nil {
				if err := userFavoriteModel.Add(userID, req.TargetType, req.TargetID); err != nil {
					c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "收藏失败: " + err.Error()})
					return
				}
			}
			if req.TargetType == model.FavoriteTargetTemplate && templateLikeModel != nil {
				liked, err := templateLikeModel.HasLiked(userID, req.TargetID)
				if err != nil {
					c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "同步模板收藏失败: " + err.Error()})
					return
				}
				if !liked {
					if err := templateLikeModel.Like(userID, req.TargetID); err != nil {
						c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "同步模板收藏失败: " + err.Error()})
						return
					}
					if templateModel != nil {
						_ = templateModel.IncrementLikeCount(req.TargetID)
					}
				}
			}
			c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "收藏成功", "data": gin.H{"favorited": true}})
		})

		favorites.DELETE("", func(c *gin.Context) {
			userID := GetTokenUserID(c)
			req, ok := parseFavoriteRequest(c)
			if !ok {
				return
			}
			if userFavoriteModel != nil {
				if err := userFavoriteModel.Remove(userID, req.TargetType, req.TargetID); err != nil {
					c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "取消收藏失败: " + err.Error()})
					return
				}
			}
			if req.TargetType == model.FavoriteTargetTemplate && templateLikeModel != nil {
				liked, err := templateLikeModel.HasLiked(userID, req.TargetID)
				if err != nil {
					c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "同步模板收藏失败: " + err.Error()})
					return
				}
				if liked {
					if err := templateLikeModel.Unlike(userID, req.TargetID); err != nil {
						c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "同步模板收藏失败: " + err.Error()})
						return
					}
					if templateModel != nil {
						_ = templateModel.DecrementLikeCount(req.TargetID)
					}
				}
			}
			c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "已取消收藏", "data": gin.H{"favorited": false}})
		})
	}
}

func parseFavoriteRequest(c *gin.Context) (favoriteRequest, bool) {
	req := favoriteRequest{}
	_ = c.ShouldBindJSON(&req)
	if req.TargetType == "" {
		req.TargetType = c.Query("target_type")
	}
	if req.TargetID <= 0 {
		req.TargetID, _ = strconv.ParseInt(strings.TrimSpace(c.Query("target_id")), 10, 64)
	}
	req.TargetType = model.NormalizeFavoriteTargetType(req.TargetType)
	if req.TargetType == "" || req.TargetID <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "无效的收藏对象"})
		return req, false
	}
	return req, true
}

func collectFavoriteRefs(userID int64, targetType string, userFavoriteModel *model.UserFavoriteModel, templateLikeModel *model.TemplateLikeModel) ([]favoriteRef, error) {
	refs := make([]favoriteRef, 0)
	seen := map[string]bool{}
	if userFavoriteModel != nil {
		list, _, err := userFavoriteModel.List(userID, targetType, 1000, 0)
		if err != nil {
			return nil, err
		}
		for _, item := range list {
			key := favoriteKey(item.TargetType, item.TargetID)
			if seen[key] {
				continue
			}
			seen[key] = true
			refs = append(refs, favoriteRef{TargetType: item.TargetType, TargetID: item.TargetID, CreatedAt: item.CreatedAt})
		}
	}
	// Preserve legacy template_likes data so V1 template collections keep working.
	if (targetType == "" || targetType == model.FavoriteTargetTemplate) && templateLikeModel != nil {
		ids, err := templateLikeModel.GetLikedTemplateIDs(userID)
		if err != nil {
			return nil, err
		}
		for _, id := range ids {
			key := favoriteKey(model.FavoriteTargetTemplate, id)
			if seen[key] {
				continue
			}
			seen[key] = true
			refs = append(refs, favoriteRef{TargetType: model.FavoriteTargetTemplate, TargetID: id})
		}
	}
	return refs, nil
}

func favoriteKey(targetType string, targetID int64) string {
	return fmt.Sprintf("%s:%d", targetType, targetID)
}

func favoriteExists(userID int64, targetType string, targetID int64, userFavoriteModel *model.UserFavoriteModel, templateLikeModel *model.TemplateLikeModel) (bool, error) {
	if userFavoriteModel != nil {
		exists, err := userFavoriteModel.Exists(userID, targetType, targetID)
		if err != nil || exists {
			return exists, err
		}
	}
	if targetType == model.FavoriteTargetTemplate && templateLikeModel != nil {
		return templateLikeModel.HasLiked(userID, targetID)
	}
	return false, nil
}

func validateFavoriteTarget(targetType string, targetID int64, templateModel *model.TemplateModel, inspirationModel *model.InspirationAssetModel, aiToolModel *model.AIToolModel, userProfileModel *model.UserProfileModel, userDBModel *model.UserModel) error {
	switch targetType {
	case model.FavoriteTargetTemplate:
		template, err := templateModel.GetByID(targetID)
		if err != nil || !isPublicSquareTemplate(template) {
			return fmt.Errorf("模板不存在")
		}
	case model.FavoriteTargetAITool:
		tool, err := aiToolModel.GetByID(targetID)
		if err != nil || tool == nil || !tool.IsPublished {
			return fmt.Errorf("AI工具不存在")
		}
	case model.FavoriteTargetInspiration:
		asset, err := inspirationModel.GetByID(targetID)
		if err != nil || asset == nil || asset.Status != "published" {
			return fmt.Errorf("灵感不存在")
		}
	case model.FavoriteTargetDesigner:
		if userDBModel == nil {
			return fmt.Errorf("设计师不存在")
		}
		user, err := userDBModel.GetByID(targetID)
		if err != nil || user == nil || user.UserType != "miniprogram" {
			return fmt.Errorf("设计师不存在")
		}
		if userProfileModel != nil {
			if profile, err := userProfileModel.GetByUserID(targetID); err == nil && profile != nil && !profile.DesignerVisible {
				return fmt.Errorf("设计师主页不可见")
			}
		}
	default:
		return fmt.Errorf("不支持的收藏分类")
	}
	return nil
}

func buildFavoriteCard(ref favoriteRef, templateModel *model.TemplateModel, inspirationModel *model.InspirationAssetModel, aiToolModel *model.AIToolModel, userProfileModel *model.UserProfileModel, userDBModel *model.UserModel) gin.H {
	switch ref.TargetType {
	case model.FavoriteTargetTemplate:
		template, err := templateModel.GetByID(ref.TargetID)
		if err != nil || !isPublicSquareTemplate(template) {
			return nil
		}
		creatorInfo := buildTemplateCreatorInfo(template.CreatorUserID, userProfileModel, userDBModel)
		author := strings.TrimSpace(template.Creator)
		if creatorInfo != nil {
			if name, ok := (*creatorInfo)["name"].(string); ok && strings.TrimSpace(name) != "" {
				author = name
			}
		}
		priceText := "免费"
		if !template.IsFree {
			priceText = fmt.Sprintf("%d 灵石", template.Price)
		}
		return gin.H{
			"id":          template.ID,
			"target_id":   template.ID,
			"target_type": ref.TargetType,
			"type_label":  "模板",
			"title":       template.Name,
			"subtitle":    template.Description,
			"author":      author,
			"image":       normalizeCosImageURL(firstNonEmpty(template.Thumbnail, template.PreviewURL, template.Images)),
			"tags":        compactStrings([]string{template.MainTab, template.SubTab, template.ThirdTab, template.Category}),
			"meta":        []string{fmt.Sprintf("%d人收藏", template.LikeCount), fmt.Sprintf("%d次下载", template.DownloadCount)},
			"badge_text":  priceText,
			"created_at":  ref.CreatedAt,
		}
	case model.FavoriteTargetAITool:
		tool, err := aiToolModel.GetByID(ref.TargetID)
		if err != nil || tool == nil || !tool.IsPublished {
			return nil
		}
		return gin.H{
			"id":          tool.ID,
			"target_id":   tool.ID,
			"target_type": ref.TargetType,
			"type_label":  "AI工具",
			"title":       tool.Name,
			"subtitle":    tool.ShortDescription,
			"author":      "AI工具库",
			"image":       normalizeCosImageURL(firstNonEmpty(tool.ListCoverImage, tool.DetailAfterImage, tool.DetailBeforeImage)),
			"tags":        compactStrings([]string{tool.Category}),
			"meta":        []string{fmt.Sprintf("%d次使用", tool.UsageCount)},
			"badge_text":  "工具",
			"created_at":  ref.CreatedAt,
		}
	case model.FavoriteTargetInspiration:
		asset, err := inspirationModel.GetByID(ref.TargetID)
		if err != nil || asset == nil || asset.Status != "published" {
			return nil
		}
		return gin.H{
			"id":          asset.ID,
			"target_id":   asset.ID,
			"target_type": ref.TargetType,
			"type_label":  "灵感",
			"title":       asset.Title,
			"subtitle":    asset.Description,
			"author":      firstNonEmpty(asset.Creator, "灵感库"),
			"image":       normalizeCosImageURL(firstNonEmpty(asset.CoverImage, firstJSONStringArrayValue(asset.Images))),
			"tags":        compactStrings(append(parseJSONStringArray(asset.Tags), asset.Scene, asset.Style, asset.Topic)),
			"meta":        []string{fmt.Sprintf("%d次浏览", asset.ViewCount), fmt.Sprintf("%d人喜欢", asset.LikeCount)},
			"badge_text":  "灵感",
			"created_at":  ref.CreatedAt,
		}
	case model.FavoriteTargetDesigner:
		user, err := userDBModel.GetByID(ref.TargetID)
		if err != nil || user == nil {
			return nil
		}
		title := strings.TrimSpace(user.Username)
		avatar := ""
		subtitle := "设计师主页"
		tags := []string{"设计师"}
		if userProfileModel != nil {
			if profile, err := userProfileModel.GetByUserID(ref.TargetID); err == nil && profile != nil {
				if !profile.DesignerVisible {
					return nil
				}
				if strings.TrimSpace(profile.Nickname) != "" {
					title = strings.TrimSpace(profile.Nickname)
				}
				avatar = sanitizePublicImageURL(profile.Avatar)
				subtitle = firstNonEmpty(profile.ServiceTitle, profile.DesignerBio, subtitle)
				tags = compactStrings([]string{profile.IdentityType, profile.ServiceTitle})
			} else if err != nil && err != sql.ErrNoRows {
				return nil
			}
		}
		workCount := int64(0)
		if templateModel != nil {
			workCount, _ = templateModel.CountPublishedByCreatorUserID(ref.TargetID, "")
		}
		return gin.H{
			"id":          ref.TargetID,
			"target_id":   ref.TargetID,
			"target_type": ref.TargetType,
			"type_label":  "设计师",
			"title":       firstNonEmpty(title, "设计师主页"),
			"subtitle":    subtitle,
			"author":      "设计师主页",
			"image":       avatar,
			"tags":        tags,
			"meta":        []string{fmt.Sprintf("%d个作品", workCount)},
			"badge_text":  "主页",
			"created_at":  ref.CreatedAt,
		}
	default:
		return nil
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if clean := strings.TrimSpace(value); clean != "" {
			return clean
		}
	}
	return ""
}

func compactStrings(values []string) []string {
	result := make([]string, 0, len(values))
	seen := map[string]bool{}
	for _, value := range values {
		clean := strings.TrimSpace(value)
		if clean == "" || seen[clean] {
			continue
		}
		seen[clean] = true
		result = append(result, clean)
	}
	return result
}

func firstJSONStringArrayValue(raw string) string {
	values := parseJSONStringArray(raw)
	if len(values) == 0 {
		return ""
	}
	return values[0]
}
