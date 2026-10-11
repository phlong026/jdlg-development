package route

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"service/model"
	"service/tracking"
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
	return func(ctx context.Context, route string, id int64) (tracking.LegacyMetadata, error) {
		var m tracking.LegacyMetadata
		missing := func() (tracking.LegacyMetadata, error) {
			return m, &tracking.Error{Status: 404, Message: "原内容不可访问"}
		}
		q := tracking.SourceReadDB(ctx, db)
		if q == nil {
			return missing()
		}
		switch route {
		case "pages/aitooldetail/aitooldetail", "pages/aitoolworkbench/aitoolworkbench":
			v, e := model.GetAIToolByID(ctx, q, id)
			if e != nil || v == nil || !v.IsPublished {
				return missing()
			}
			m = tracking.LegacyMetadata{Title: v.Name, Type: "ai_tool", CoverURL: v.ListCoverImage, VersionSeed: legacySeed([]any{v.Name, v.ShortDescription, v.DetailDescription, v.ListCoverImage, v.DetailBeforeImage, v.DetailAfterImage, v.Category, v.PromptPlaceholder, v.UploadHint, v.ShowUsageTips, v.UsageTipsTitle, v.UsageTipsContent, v.PresetReferenceImages, v.StylePresetsRaw})}
		case "pages/inspirationdetail/inspirationdetail":
			v, e := model.GetInspirationAssetByID(ctx, q, id)
			if e != nil || v == nil || v.Status != "published" {
				return missing()
			}
			m = tracking.LegacyMetadata{Title: v.Title, Type: "inspiration", CoverURL: v.CoverImage, Width: v.ImageWidth, Height: v.ImageHeight, VersionSeed: legacySeed([]any{v.Title, v.Description, v.CoverImage, v.Images, v.Tags, v.Scene, v.Style, v.Topic})}
		case "pages/utilitytooldetail/utilitytooldetail":
			v, e := model.GetUtilityToolByID(ctx, q, id)
			if e != nil || v == nil || !v.IsPublished {
				return missing()
			}
			m = tracking.LegacyMetadata{Title: v.Title, Type: "utility_tool", CoverURL: v.CoverImage, VersionSeed: legacySeed([]any{v.Title, v.Category, v.Content, v.CoverImage, v.VideoURL, v.FileURL})}
		case "pages/templatesquaredetails/templatesquaredetails":
			v, e := model.GetTemplateByID(ctx, q, id)
			if e != nil || !isPublicSquareTemplate(v) {
				return missing()
			}
			m = tracking.LegacyMetadata{Title: v.Name, Type: "template", CoverURL: normalizeCosImageURL(v.Thumbnail), Width: v.ImageWidth, Height: v.ImageHeight, VersionSeed: legacySeed([]any{v.Name, v.Description, v.Thumbnail, v.PreviewURL, v.Images, v.Price, v.IsFree, v.Category, v.MainTab, v.SubTab, v.ThirdTab})}
		case "pages/designerhome/designerhome":
			u, e := model.GetUserByID(ctx, q, id)
			if e != nil || u == nil {
				return missing()
			}
			p, e := model.GetUserProfileByUserID(ctx, q, id)
			if e != nil && e != sql.ErrNoRows {
				return missing()
			}
			if !isDesignerPublicVisible(u, p, nil) {
				return missing()
			}
			title := strings.TrimSpace(u.Username)
			cover := ""
			var publicProfile any
			if p != nil {
				if strings.TrimSpace(p.Nickname) != "" {
					title = p.Nickname
				}
				cover = sanitizePublicImageURL(p.Avatar)
				publicProfile = []any{p.Nickname, p.Avatar, p.DesignerBio, p.SpecialtyStyles, p.DesignerExperienceYears, p.ServiceTitle, p.ServiceQuote, p.ServiceIntro, p.ServiceEnabled}
			}
			if title == "" {
				title = "设计师主页"
			}
			m = tracking.LegacyMetadata{Title: title, Type: "designer", CoverURL: cover, VersionSeed: legacySeed([]any{title, publicProfile})}
		default:
			return missing()
		}
		return m, nil
	}
}
