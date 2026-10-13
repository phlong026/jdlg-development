package tracking

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"time"
)

const publishedReaderRoute = "pages/contentreader/contentreader"

// PublishedMaterial deliberately excludes mutable draft fields, body, private
// ownership metadata and analytics. Mini-program grants expose only snapshots.
type PublishedMaterial struct {
	ID          string     `json:"content_id"`
	Version     int        `json:"content_version"`
	Title       string     `json:"title"`
	Status      string     `json:"status"`
	Cover       *Asset     `json:"cover,omitempty"`
	PublishedAt *time.Time `json:"published_at,omitempty"`
	Route       string     `json:"route_key"`
}
type PublishedMaterials struct {
	Items  []PublishedMaterial `json:"items"`
	Cursor string              `json:"next_cursor,omitempty"`
}

func (s *Service) publishedMaterial(ctx context.Context, r Repository, scope Scope, id string, version int) (PublishedMaterial, error) {
	var out PublishedMaterial
	master, err := decode[Content](r, "content", id)
	if err != nil || !scope.Allows(master.Owner) || master.ReadOnly || master.Status == "revoked" || master.Version < 1 {
		return out, fail(404, "素材不可访问")
	}
	if version == 0 {
		version = master.Version
	}
	if err = s.available(ctx, r, publishedReaderRoute, id, version); err != nil {
		return out, err
	}
	snapshot, err := decode[Content](r, "version", contentKey(id, version))
	if err != nil || !scope.Allows(snapshot.Owner) || snapshot.Owner != master.Owner {
		return out, fail(404, "素材不可访问")
	}
	return PublishedMaterial{ID: snapshot.ID, Version: snapshot.Version, Title: snapshot.Title, Status: "published", Cover: snapshot.Cover, PublishedAt: snapshot.PublishedAt, Route: publishedReaderRoute}, nil
}
func (s *Service) publishedMaterialAccess(scope Scope) error {
	if err := s.authorized(scope); err != nil {
		return err
	}
	if !scope.Manage {
		return fail(403, "无素材分享权限")
	}
	if !s.Config.ContentEnabled {
		return fail(404, "内容未启用")
	}
	return nil
}
func (s *Service) PublishedMaterials(ctx context.Context, scope Scope, cursor string, limit int) (PublishedMaterials, error) {
	out := PublishedMaterials{Items: []PublishedMaterial{}}
	if err := s.publishedMaterialAccess(scope); err != nil {
		return out, err
	}
	if cursor != "" && !validID(cursor) {
		return out, fail(400, "分页标识无效")
	}
	if limit <= 0 {
		limit = 20
	}
	if limit > 50 {
		limit = 50
	}
	err := s.readQueryTransaction(ctx, func(r Repository) error {
		rows, err := scopedRecords(r, "content", RecordQuery{Owners: scope.Owners})
		if err != nil {
			return err
		}
		items := []PublishedMaterial{}
		for _, row := range rows {
			var c Content
			if json.Unmarshal(row.Payload, &c) != nil {
				return ErrUnavailable
			}
			if c.ReadOnly || c.Version < 1 || c.Status == "revoked" {
				continue
			}
			item, err := s.publishedMaterial(ctx, r, scope, c.ID, 0)
			if err != nil {
				var unavailable *Error
				if errors.As(err, &unavailable) && unavailable.Status == 404 {
					continue
				}
				return err
			}
			items = append(items, item)
		}
		// Stable ID ordering makes a small cursor sufficient without exposing owners.
		sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
		for _, item := range items {
			if item.ID <= cursor {
				continue
			}
			if len(out.Items) == limit {
				out.Cursor = out.Items[len(out.Items)-1].ID
				break
			}
			out.Items = append(out.Items, item)
		}
		return nil
	})
	return out, err
}
func (s *Service) OpenPublishedMaterial(ctx context.Context, scope Scope, id string, version int) (PublishedMaterial, error) {
	var out PublishedMaterial
	if err := s.publishedMaterialAccess(scope); err != nil {
		return out, err
	}
	if !validID(id) || version < 1 {
		return out, fail(404, "素材不可访问")
	}
	err := s.readQueryTransaction(ctx, func(r Repository) error {
		var err error
		out, err = s.publishedMaterial(ctx, r, scope, id, version)
		return err
	})
	return out, err
}
