package model

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// TemplateUpdate is a partial edit. Omitted fields preserve the current database
// value; empty strings, false and zero are explicit values. Null is not a value.
// Creation defaults belong to Create, never to fields omitted from an edit.
type TemplateUpdate struct {
	Name         *string `json:"name"`
	Category     *string `json:"category"`
	MainTab      *string `json:"main_tab"`
	SubTab       *string `json:"sub_tab"`
	ThirdTab     *string `json:"third_tab"`
	Description  *string `json:"description"`
	Thumbnail    *string `json:"thumbnail"`
	PreviewURL   *string `json:"preview_url"`
	Images       *string `json:"images"`
	Price        *int64  `json:"price"`
	IsFree       *bool   `json:"is_free"`
	Status       *string `json:"status"`
	PublishScope *string `json:"publish_scope"`
	RejectReason *string `json:"reject_reason"`
	SourceType   *string `json:"source_type"`
}

func (u *TemplateUpdate) UnmarshalJSON(data []byte) error {
	type plain TemplateUpdate
	var decoded plain
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	if fields == nil {
		return errors.New("模板更新必须为对象")
	}
	for key, value := range fields {
		switch key {
		case "name", "category", "main_tab", "sub_tab", "third_tab", "description", "thumbnail", "preview_url", "images", "price", "is_free", "status", "publish_scope", "reject_reason", "source_type":
			if bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
				return fmt.Errorf("%s 不能为 null；保留原值请省略字段", key)
			}
		}
	}
	*u = TemplateUpdate(decoded)
	return nil
}

func (u TemplateUpdate) Apply(template *Template) error {
	if u.Name != nil && strings.TrimSpace(*u.Name) == "" {
		return errors.New("名称不能为空")
	}
	if u.Category != nil && strings.TrimSpace(*u.Category) == "" {
		return errors.New("分类不能为空")
	}
	setString := func(value *string, destination *string) {
		if value != nil {
			*destination = *value
		}
	}
	setString(u.Name, &template.Name)
	setString(u.Category, &template.Category)
	setString(u.MainTab, &template.MainTab)
	setString(u.SubTab, &template.SubTab)
	setString(u.ThirdTab, &template.ThirdTab)
	setString(u.Thumbnail, &template.Thumbnail)
	setString(u.PreviewURL, &template.PreviewURL)
	setString(u.Images, &template.Images)
	setString(u.Status, &template.Status)
	if u.Description != nil {
		template.Description = strings.TrimSpace(*u.Description)
	}
	if u.Price != nil {
		template.Price = *u.Price
	}
	if u.IsFree != nil {
		template.IsFree = *u.IsFree
	}
	if u.PublishScope != nil {
		template.PublishScope = strings.TrimSpace(*u.PublishScope)
		if template.PublishScope == "" {
			template.PublishScope = "square"
		}
	}
	if u.RejectReason != nil {
		template.RejectReason = strings.TrimSpace(*u.RejectReason)
	}
	if u.SourceType != nil {
		template.SourceType = strings.TrimSpace(*u.SourceType)
		if template.SourceType == "" {
			template.SourceType = "admin_upload"
			if template.CreatorUserID > 0 {
				template.SourceType = "ai_generated"
			}
		}
	}
	return nil
}

func (u TemplateUpdate) ImagesChanged(template *Template) bool {
	return (u.Thumbnail != nil && *u.Thumbnail != template.Thumbnail) ||
		(u.PreviewURL != nil && *u.PreviewURL != template.PreviewURL) ||
		(u.Images != nil && *u.Images != template.Images)
}

func (u TemplateUpdate) TabsChanged(template *Template) bool {
	return (u.MainTab != nil && *u.MainTab != template.MainTab) ||
		(u.SubTab != nil && *u.SubTab != template.SubTab) ||
		(u.ThirdTab != nil && *u.ThirdTab != template.ThirdTab)
}

var ErrTemplateUpdateConflict = errors.New("模板已被其他操作修改，请重新打开后再保存")

type templateEditColumn struct {
	name   string
	before interface{}
	after  interface{}
}

// UpdatePartial writes only edited columns. Compare-and-swap protects edited
// values and validation-dependent tab/image groups between reading and writing;
// unrelated concurrent metadata changes are neither compared nor overwritten.
func (m *TemplateModel) UpdatePartial(before, after *Template, update TemplateUpdate) error {
	columns := make([]templateEditColumn, 0, 17)
	add := func(present bool, name string, old, next interface{}) {
		if present && old != next {
			columns = append(columns, templateEditColumn{name, old, next})
		}
	}
	add(update.Name != nil, "name", before.Name, after.Name)
	add(update.Category != nil, "category", before.Category, after.Category)
	add(update.MainTab != nil, "main_tab", before.MainTab, after.MainTab)
	add(update.SubTab != nil, "sub_tab", before.SubTab, after.SubTab)
	add(update.ThirdTab != nil, "third_tab", before.ThirdTab, after.ThirdTab)
	if update.Description != nil && before.Description != after.Description {
		oldDescription, nextDescription := before.Description, after.Description
		if before.storedDescription != nil {
			oldDescription = *before.storedDescription
			// Legacy records carry a hidden prompt inside description. Keep that
			// exact suffix in storage when replacing only the public description;
			// this does not rewrite or default the separate internal_prompt field.
			if before.Description != strings.TrimSpace(oldDescription) {
				if index := strings.Index(oldDescription, LegacyTemplatePromptPrefix); index >= 0 {
					nextDescription += oldDescription[index:]
				}
			}
		}
		add(true, "description", oldDescription, nextDescription)
	}
	add(update.Price != nil, "price", before.Price, after.Price)
	add(update.IsFree != nil, "is_free", before.IsFree, after.IsFree)
	add(update.Status != nil, "status", before.Status, after.Status)
	add(update.PublishScope != nil, "publish_scope", before.PublishScope, after.PublishScope)
	add(update.RejectReason != nil, "reject_reason", before.RejectReason, after.RejectReason)
	add(update.SourceType != nil, "source_type", before.SourceType, after.SourceType)
	imagesChanged := update.ImagesChanged(before)
	add(imagesChanged, "thumbnail", before.Thumbnail, after.Thumbnail)
	add(imagesChanged, "preview_url", before.PreviewURL, after.PreviewURL)
	add(imagesChanged, "images", before.Images, after.Images)
	add(imagesChanged, "image_width", before.ImageWidth, after.ImageWidth)
	add(imagesChanged, "image_height", before.ImageHeight, after.ImageHeight)
	if len(columns) == 0 {
		// MySQL reports zero changed rows for an unchanged edit. Do not confuse a
		// no-op with a failed compare-and-swap, and do not touch updated_at.
		return nil
	}
	assignments := make([]string, 0, len(columns))
	args := make([]interface{}, 0, len(columns)*2+10)
	guards := append([]templateEditColumn(nil), columns...)
	for _, column := range columns {
		assignments = append(assignments, column.name+" = ?")
		args = append(args, column.after)
	}
	publicationChanged := (update.Status != nil && *update.Status != before.Status) ||
		(update.PublishScope != nil && after.PublishScope != before.PublishScope) ||
		(update.SourceType != nil && after.SourceType != before.SourceType)
	if update.TabsChanged(before) || publicationChanged {
		guards = append(guards, templateEditColumn{"main_tab", before.MainTab, nil}, templateEditColumn{"sub_tab", before.SubTab, nil}, templateEditColumn{"third_tab", before.ThirdTab, nil})
	}
	if imagesChanged || publicationChanged {
		guards = append(guards,
			templateEditColumn{"thumbnail", before.Thumbnail, nil}, templateEditColumn{"preview_url", before.PreviewURL, nil}, templateEditColumn{"images", before.Images, nil},
			templateEditColumn{"image_width", before.ImageWidth, nil}, templateEditColumn{"image_height", before.ImageHeight, nil},
			templateEditColumn{"source_type", before.SourceType, nil}, templateEditColumn{"creator_user_id", nullableInt64(before.CreatorUserID), nil})
	}
	query := "UPDATE templates SET " + strings.Join(assignments, ", ") + " WHERE id = ?"
	args = append(args, before.ID)
	seen := make(map[string]bool)
	for _, guard := range guards {
		if seen[guard.name] {
			continue
		}
		seen[guard.name] = true
		column := guard.name
		if _, isString := guard.before.(string); isString {
			column = "BINARY " + column
		}
		query += " AND " + column + " <=> ?"
		args = append(args, guard.before)
	}
	result, err := m.DB.Exec(query, args...)
	if err != nil {
		return err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed == 0 {
		return ErrTemplateUpdateConflict
	}
	return nil
}
