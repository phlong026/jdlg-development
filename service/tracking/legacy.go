package tracking

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/fnv"
	"strconv"
	"strings"
	"time"
)

// LegacyMetadata is an allowlisted public snapshot, never a source body or profile.
// VersionSeed must represent source content changes, not engagement counters.
type LegacyMetadata struct {
	Title, Type, CoverURL, VersionSeed string
	Width, Height                      int
}

const CostSnapshotRoute = "pages/aicostshare/aicostshare"

// Public snapshots use unguessable, immutable report IDs, never private task IDs.
func CostSnapshotID(reportID string) string { return "costsnapshot:" + reportID }

func validCostSnapshotID(content string) (string, bool) {
	id := strings.TrimPrefix(content, "costsnapshot:")
	if content != CostSnapshotID(id) || len(id) != 32 {
		return "", false
	}
	for _, ch := range id {
		if !(ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9' || ch == '-' || ch == '_') {
			return "", false
		}
	}
	return id, true
}

func LegacyRoute(route string) bool {
	switch route {
	case CostSnapshotRoute:
		return true
	case "pages/aitooldetail/aitooldetail", "pages/aitoolworkbench/aitoolworkbench", "pages/inspirationdetail/inspirationdetail", "pages/utilitytooldetail/utilitytooldetail", "pages/templatesquaredetails/templatesquaredetails", "pages/designerhome/designerhome":
		return true
	}
	return false
}
func LegacyID(route string, id int64) string {
	h := fnv.New32a()
	_, _ = h.Write([]byte(route))
	return fmt.Sprintf("legacy:%x:%d", h.Sum32(), id)
}
func (s *Service) resolveLegacy(ctx context.Context, route, content string) (Content, error) {
	var c Content
	if !LegacyRoute(route) {
		return c, fail(404, "原内容不可访问")
	}
	var m LegacyMetadata
	var err error
	if route == CostSnapshotRoute {
		id, valid := validCostSnapshotID(content)
		if !valid || s.ResolvePublicSnapshot == nil {
			return c, fail(404, "公开快照标识无效")
		}
		m, err = s.ResolvePublicSnapshot(ctx, id)
	} else {
		if s.ResolveLegacyContext == nil && s.ResolveLegacy == nil {
			return c, fail(404, "原内容不可访问")
		}
		parts := strings.Split(content, ":")
		if len(parts) != 3 {
			return c, fail(404, "原内容标识无效")
		}
		id, parseErr := strconv.ParseInt(parts[2], 10, 64)
		if parseErr != nil || id <= 0 || content != LegacyID(route, id) {
			return c, fail(404, "原内容标识无效")
		}
		if s.ResolveLegacyContext != nil {
			m, err = s.ResolveLegacyContext(ctx, route, id)
		} else {
			// Compatibility for in-memory integrations. Production source readers use
			// ResolveLegacyContext so they share the active tracking transaction.
			m, err = s.ResolveLegacy(route, id)
		}
	}
	if err != nil {
		return c, err
	}
	if strings.TrimSpace(m.Title) == "" || m.VersionSeed == "" {
		return c, fail(404, "原内容不可访问")
	}
	version := SourceSnapshotVersion(m)
	c = Content{ID: content, Version: version, Title: m.Title, Type: m.Type, Status: "published", Media: []Asset{}, BodyFormat: "text", Rights: "source", ReadOnly: true, Route: route, VersionMode: "source_snapshot", SourceNote: "原业务内容快照，仅保留访问时标题和封面；原页面不支持历史正文回放。"}
	if safeURL(m.CoverURL) {
		c.Cover = &Asset{ID: content + ":cover", Kind: "image", URL: m.CoverURL, Width: m.Width, Height: m.Height}
	}
	return c, nil
}

func SourceSnapshotVersion(m LegacyMetadata) int {
	sum := sha256.Sum256(marshal(m))
	version := int(binary.BigEndian.Uint64(sum[:8]) & ((1 << 53) - 1))
	if version == 0 {
		return 1
	}
	return version
}
func saveLegacy(r Repository, c Content, owner string, now time.Time) error {
	c.Owner = owner
	catalogKey := hash(owner + ":" + c.ID)
	created := now.UTC()
	old, err := loadCatalogContent(r, "legacy_catalog", catalogKey)
	if err == nil {
		created = *old.CreatedAt
	} else if !errors.Is(err, ErrNotFound) {
		return err
	}
	c.CreatedAt = &created
	// Snapshot versions are immutable. Catalog rows are owner-scoped, not author-owned.
	key := contentKey(c.ID, c.Version)
	if _, err := r.Get("version", key); err == ErrNotFound {
		if err = r.Insert(record("version", key, "", now, c)); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	return r.Put(record("legacy_catalog", catalogKey, owner, now, c))
}
