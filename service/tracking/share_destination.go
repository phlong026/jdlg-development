package tracking

import (
	"context"
	"net/url"
	"strconv"
	"strings"
)

// SanitizePublishedRichText re-applies the persistence whitelist before a
// browser renderer marks stored rich text as trusted HTML.
func SanitizePublishedRichText(body string) (string, error) {
	clean, format, err := normalizeBody(body, "rich_text")
	if err != nil || format != "rich_text" {
		return "", err
	}
	return clean, nil
}

// ResolveShareRecord returns the current validated share without creating an
// analytics entry. It is safe for link previews and server-side rendering.
func (s *Service) ResolveShareRecord(ctx context.Context, token string) (Share, error) {
	var out Share
	if !validID(token) {
		return out, fail(404, "分享入口无效")
	}
	err := s.readTransaction(ctx, func(r Repository) error {
		share, err := decode[Share](r, "share", hash(token))
		if err != nil || share.Revoked || !s.Now().Before(share.Expires) {
			return fail(404, "分享入口无效")
		}
		if err = s.available(ctx, r, share.Route, share.Content, share.Version); err != nil {
			return err
		}
		if LegacyRoute(share.Route) {
			current, resolveErr := s.resolveLegacy(sourceReadContext(ctx, r), share.Route, share.Content)
			if resolveErr != nil || current.Version != share.Version {
				return fail(404, "分享入口无效")
			}
		}
		out = share
		return nil
	})
	return out, err
}

// OwnShare verifies that an authenticated mini-program account is the actual
// sharer. Owner membership or content authorship never transfers this right.
func (s *Service) OwnShare(ctx context.Context, scope Scope, actor int64, token string) (Share, error) {
	var out Share
	if scope.Tenant != s.Config.Tenant || actor <= 0 || scope.Actor != actor {
		return out, fail(403, "分享账号无效")
	}
	share, err := s.ResolveShareRecord(ctx, token)
	if err != nil || share.Origin != "miniprogram" || share.Sharer != actorVisitorKey(actor) {
		return out, fail(404, "分享入口不可访问")
	}
	return share, nil
}

// ShareNativeDestination builds the original mini-program target while keeping
// the same opaque share token and therefore the same actual sharer.
func ShareNativeDestination(share Share, token string) (string, string, error) {
	if !validID(token) || share.Route == "" {
		return "", "", fail(404, "分享入口无效")
	}
	values := url.Values{"st": {token}}
	switch {
	case share.Route == publishedReaderRoute:
		if !validID(share.Content) || share.Version <= 0 {
			return "", "", fail(404, "分享入口无效")
		}
		values.Set("content_id", share.Content)
		values.Set("content_version", strconv.Itoa(share.Version))
	case share.Route == CostSnapshotRoute:
		reportID, ok := validCostSnapshotID(share.Content)
		if !ok {
			return "", "", fail(404, "分享入口无效")
		}
		values.Set("report_id", reportID)
	case LegacyRoute(share.Route):
		index := strings.LastIndexByte(share.Content, ':')
		if index < 0 {
			return "", "", fail(404, "分享入口无效")
		}
		id, err := strconv.ParseInt(share.Content[index+1:], 10, 64)
		if err != nil || id <= 0 || share.Content != LegacyID(share.Route, id) {
			return "", "", fail(404, "分享入口无效")
		}
		key := "id"
		if share.Route == "pages/designerhome/designerhome" {
			key = "userId"
		}
		values.Set(key, strconv.FormatInt(id, 10))
	default:
		if share.Content != "" || share.Version != 0 {
			return "", "", fail(404, "分享入口无效")
		}
	}
	return share.Route, values.Encode(), nil
}
