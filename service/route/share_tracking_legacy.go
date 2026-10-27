package route

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"service/model"
	"service/tracking"
	"strconv"
	"strings"
)

// Only explicitly public presentation fields enter tracking. Source body is hashed
// for change detection, never persisted. Engagement counter updates are excluded.
func legacySeed(v any) string {
	b, _ := json.Marshal(v)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

func makeTrackingLegacyResolver(db *sql.DB) func(context.Context, string, int64) (tracking.LegacyMetadata, error) {
	batch := makeTrackingLegacyBatchResolver(db)
	return func(ctx context.Context, route string, id int64) (tracking.LegacyMetadata, error) {
		content := tracking.LegacyID(route, id)
		items, err := batch(ctx, route, []string{content})
		if err != nil {
			return tracking.LegacyMetadata{}, err
		}
		metadata, ok := items[content]
		if !ok {
			return tracking.LegacyMetadata{}, &tracking.Error{Status: 404, Message: "原内容不可访问"}
		}
		return metadata, nil
	}
}

func makeTrackingLegacyBatchResolver(db *sql.DB) func(context.Context, string, []string) (map[string]tracking.LegacyMetadata, error) {
	return func(ctx context.Context, route string, contents []string) (map[string]tracking.LegacyMetadata, error) {
		out := map[string]tracking.LegacyMetadata{}
		ids, contentByID, err := legacyBatchIDs(route, contents)
		if err != nil || len(ids) == 0 {
			return out, err
		}
		q := tracking.SourceReadDB(ctx, db)
		if q == nil {
			return nil, tracking.ErrUnavailable
		}
		placeholders, args := legacyBatchArgs(ids)
		switch route {
		case "pages/aitooldetail/aitooldetail", "pages/aitoolworkbench/aitoolworkbench":
			rows, queryErr := q.QueryContext(ctx, `SELECT id, code, name, category, short_description, detail_description, list_cover_image, detail_before_image, detail_after_image, preset_reference_images, style_presets, prompt_placeholder, default_prompt, upload_hint, show_usage_tips, usage_tips_title, usage_tips_content, sort_order, is_published, is_common, usage_count, created_at, updated_at FROM ai_tools WHERE id IN (`+placeholders+`)`, args...)
			if queryErr != nil {
				return nil, queryErr
			}
			defer rows.Close()
			for rows.Next() {
				v := &model.AITool{}
				var showUsageTips, published, common int
				if err = rows.Scan(&v.ID, &v.Code, &v.Name, &v.Category, &v.ShortDescription, &v.DetailDescription, &v.ListCoverImage, &v.DetailBeforeImage, &v.DetailAfterImage, &v.PresetReferenceImages, &v.StylePresetsRaw, &v.PromptPlaceholder, &v.DefaultPrompt, &v.UploadHint, &showUsageTips, &v.UsageTipsTitle, &v.UsageTipsContent, &v.SortOrder, &published, &common, &v.UsageCount, &v.CreatedAt, &v.UpdatedAt); err != nil {
					return nil, err
				}
				v.ShowUsageTips, v.IsPublished, v.IsCommon = showUsageTips == 1, published == 1, common == 1
				if v.IsPublished {
					out[contentByID[v.ID]] = tracking.LegacyMetadata{Title: v.Name, Type: "ai_tool", CoverURL: v.ListCoverImage, VersionSeed: legacySeed([]any{v.Name, v.ShortDescription, v.DetailDescription, v.ListCoverImage, v.DetailBeforeImage, v.DetailAfterImage, v.Category, v.PromptPlaceholder, v.UploadHint, v.ShowUsageTips, v.UsageTipsTitle, v.UsageTipsContent, v.PresetReferenceImages, v.StylePresetsRaw})}
				}
			}
			return out, rows.Err()
		case "pages/inspirationdetail/inspirationdetail":
			rows, queryErr := q.QueryContext(ctx, `SELECT id, title, description, cover_image, images, image_width, image_height, tags, scene, style, topic, sort_order, status, creator, creator_user_id, view_count, like_count, created_at, updated_at FROM inspiration_assets WHERE id IN (`+placeholders+`)`, args...)
			if queryErr != nil {
				return nil, queryErr
			}
			defer rows.Close()
			for rows.Next() {
				v := &model.InspirationAsset{}
				var creatorUserID sql.NullInt64
				if err = rows.Scan(&v.ID, &v.Title, &v.Description, &v.CoverImage, &v.Images, &v.ImageWidth, &v.ImageHeight, &v.Tags, &v.Scene, &v.Style, &v.Topic, &v.SortOrder, &v.Status, &v.Creator, &creatorUserID, &v.ViewCount, &v.LikeCount, &v.CreatedAt, &v.UpdatedAt); err != nil {
					return nil, err
				}
				if v.Status == "published" {
					out[contentByID[v.ID]] = tracking.LegacyMetadata{Title: v.Title, Type: "inspiration", CoverURL: v.CoverImage, Width: v.ImageWidth, Height: v.ImageHeight, VersionSeed: legacySeed([]any{v.Title, v.Description, v.CoverImage, v.Images, v.Tags, v.Scene, v.Style, v.Topic})}
				}
			}
			return out, rows.Err()
		case "pages/utilitytooldetail/utilitytooldetail":
			rows, queryErr := q.QueryContext(ctx, `SELECT id, category, title, content, cover_image, video_url, file_url, sort_order, is_published, view_count, created_at, updated_at FROM utility_tools WHERE id IN (`+placeholders+`)`, args...)
			if queryErr != nil {
				return nil, queryErr
			}
			defer rows.Close()
			for rows.Next() {
				v := &model.UtilityTool{}
				var published int
				if err = rows.Scan(&v.ID, &v.Category, &v.Title, &v.Content, &v.CoverImage, &v.VideoURL, &v.FileURL, &v.SortOrder, &published, &v.ViewCount, &v.CreatedAt, &v.UpdatedAt); err != nil {
					return nil, err
				}
				v.IsPublished = published == 1
				if v.IsPublished {
					out[contentByID[v.ID]] = tracking.LegacyMetadata{Title: v.Title, Type: "utility_tool", CoverURL: v.CoverImage, VersionSeed: legacySeed([]any{v.Title, v.Category, v.Content, v.CoverImage, v.VideoURL, v.FileURL})}
				}
			}
			return out, rows.Err()
		case "pages/templatesquaredetails/templatesquaredetails":
			rows, queryErr := q.QueryContext(ctx, `SELECT id, name, category, main_tab, sub_tab, third_tab, description, thumbnail, preview_url, images, image_width, image_height, price, is_free, status, publish_scope, source_type, creator, creator_user_id FROM templates WHERE id IN (`+placeholders+`)`, args...)
			if queryErr != nil {
				return nil, queryErr
			}
			defer rows.Close()
			for rows.Next() {
				v := &model.Template{}
				var free int
				var creatorUserID sql.NullInt64
				if err = rows.Scan(&v.ID, &v.Name, &v.Category, &v.MainTab, &v.SubTab, &v.ThirdTab, &v.Description, &v.Thumbnail, &v.PreviewURL, &v.Images, &v.ImageWidth, &v.ImageHeight, &v.Price, &free, &v.Status, &v.PublishScope, &v.SourceType, &v.Creator, &creatorUserID); err != nil {
					return nil, err
				}
				v.IsFree = free == 1
				v.Description, _ = model.SplitTemplateDescriptionPrompt(v.Description)
				if isPublicSquareTemplate(v) {
					out[contentByID[v.ID]] = tracking.LegacyMetadata{Title: v.Name, Type: "template", CoverURL: normalizeCosImageURL(v.Thumbnail), Width: v.ImageWidth, Height: v.ImageHeight, VersionSeed: legacySeed([]any{v.Name, v.Description, v.Thumbnail, v.PreviewURL, v.Images, v.Price, v.IsFree, v.Category, v.MainTab, v.SubTab, v.ThirdTab})}
				}
			}
			return out, rows.Err()
		case "pages/designerhome/designerhome":
			return resolveDesignerBatch(ctx, q, ids, contentByID, placeholders, args)
		default:
			return out, &tracking.Error{Status: 404, Message: "原内容不可访问"}
		}
	}
}

func legacyBatchIDs(route string, contents []string) ([]int64, map[int64]string, error) {
	ids := make([]int64, 0, len(contents))
	contentByID := make(map[int64]string, len(contents))
	for _, content := range contents {
		index := strings.LastIndexByte(content, ':')
		if index < 0 {
			return nil, nil, &tracking.Error{Status: 404, Message: "原内容标识无效"}
		}
		id, err := strconv.ParseInt(content[index+1:], 10, 64)
		if err != nil || id <= 0 || content != tracking.LegacyID(route, id) {
			return nil, nil, &tracking.Error{Status: 404, Message: "原内容标识无效"}
		}
		if _, exists := contentByID[id]; !exists {
			ids = append(ids, id)
			contentByID[id] = content
		}
	}
	return ids, contentByID, nil
}

func legacyBatchArgs(ids []int64) (string, []any) {
	placeholders := make([]string, len(ids))
	args := make([]any, len(ids))
	for i, id := range ids {
		placeholders[i], args[i] = "?", id
	}
	return strings.Join(placeholders, ","), args
}

func resolveDesignerBatch(ctx context.Context, q tracking.SQLSourceQueryer, ids []int64, contentByID map[int64]string, placeholders string, args []any) (map[string]tracking.LegacyMetadata, error) {
	type designer struct {
		id       int64
		username string
		userType string
	}
	users := map[int64]designer{}
	rows, err := q.QueryContext(ctx, `SELECT id, username, user_type FROM users WHERE id IN (`+placeholders+`)`, args...)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var item designer
		if err = rows.Scan(&item.id, &item.username, &item.userType); err != nil {
			rows.Close()
			return nil, err
		}
		users[item.id] = item
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	if err = rows.Close(); err != nil {
		return nil, err
	}
	type profile struct {
		nickname, avatar, bio, styles, serviceTitle, serviceIntro string
		experience, serviceQuote                                  int64
		serviceEnabled, visible                                   bool
	}
	profiles := map[int64]profile{}
	rows, err = q.QueryContext(ctx, `SELECT user_id, COALESCE(nickname, ''), COALESCE(avatar, ''), COALESCE(designer_bio, ''), COALESCE(specialty_styles, ''), designer_experience_years, COALESCE(service_title, ''), service_quote, COALESCE(service_intro, ''), service_enabled, designer_visible FROM user_profiles WHERE user_id IN (`+placeholders+`)`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var item profile
		if err = rows.Scan(&id, &item.nickname, &item.avatar, &item.bio, &item.styles, &item.experience, &item.serviceTitle, &item.serviceQuote, &item.serviceIntro, &item.serviceEnabled, &item.visible); err != nil {
			return nil, err
		}
		profiles[id] = item
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	out := map[string]tracking.LegacyMetadata{}
	for _, id := range ids {
		user, ok := users[id]
		if !ok || strings.TrimSpace(user.userType) != "miniprogram" {
			continue
		}
		p, hasProfile := profiles[id]
		if hasProfile && !p.visible {
			continue
		}
		title, cover := strings.TrimSpace(user.username), ""
		var publicProfile any
		if hasProfile {
			if strings.TrimSpace(p.nickname) != "" {
				title = p.nickname
			}
			cover = sanitizePublicImageURL(p.avatar)
			publicProfile = []any{p.nickname, p.avatar, p.bio, p.styles, p.experience, p.serviceTitle, p.serviceQuote, p.serviceIntro, p.serviceEnabled}
		}
		if title == "" {
			title = "设计师主页"
		}
		out[contentByID[id]] = tracking.LegacyMetadata{Title: title, Type: "designer", CoverURL: cover, VersionSeed: legacySeed([]any{title, publicProfile})}
	}
	return out, nil
}
