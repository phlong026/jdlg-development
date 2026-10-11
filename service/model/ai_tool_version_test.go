package model

import "testing"

func snapshotMapForTest(fields map[string]interface{}) map[string]interface{} {
	return fields
}

func TestDiffAIToolSnapshots(t *testing.T) {
	published := map[string]interface{}{
		"name":           "总平填色",
		"default_prompt": "高质量总平填色 v1",
		"sort_order":     float64(1),
		"is_published":   true,
		"style_presets": []interface{}{
			map[string]interface{}{"id": "s1", "name": "专业方案"},
			map[string]interface{}{"id": "s2", "name": "手绘风"},
		},
	}
	draft := map[string]interface{}{
		"name":           "总平填色",
		"default_prompt": "高质量总平填色 v2（强化线稿）",
		"sort_order":     float64(1),
		"is_published":   true,
		"upload_hint":    "请上传总平面图",
		"style_presets": []interface{}{
			map[string]interface{}{"id": "s1", "name": "专业方案"},
			map[string]interface{}{"id": "s3", "name": "夜景表现"},
		},
	}
	items := DiffAIToolSnapshots(published, draft)
	byField := make(map[string]AIToolDiffItem, len(items))
	for _, item := range items {
		byField[item.Field] = item
	}

	if len(items) != 3 {
		t.Fatalf("期望 3 处差异，got %d: %+v", len(items), items)
	}
	if item, ok := byField["default_prompt"]; !ok || item.Kind != "changed" {
		t.Errorf("default_prompt 应为 changed: %+v", item)
	} else if item.Before != "高质量总平填色 v1" || item.After != "高质量总平填色 v2（强化线稿）" {
		t.Errorf("default_prompt 前后值不对: %+v", item)
	}
	if item, ok := byField["upload_hint"]; !ok || item.Kind != "added" {
		t.Errorf("upload_hint 应为 added: %+v", item)
	}
	if item, ok := byField["style_presets"]; !ok || item.Kind != "changed" {
		t.Errorf("style_presets 应为 changed: %+v", item)
	} else if item.Before != "专业方案、手绘风" || item.After != "专业方案、夜景表现" {
		t.Errorf("style_presets 摘要不对: %+v", item)
	}
	// 未变化字段不应出现
	if _, ok := byField["name"]; ok {
		t.Errorf("name 未变化不应出现在 diff 中")
	}
}

func TestDiffAIToolSnapshotsNoPublished(t *testing.T) {
	// 从未发布过：全部视为新增
	draft := map[string]interface{}{
		"name":    "新工具",
		"is_published": false,
	}
	items := DiffAIToolSnapshots(nil, draft)
	if len(items) == 0 {
		t.Fatalf("无线上版本时应全量输出新增项")
	}
	for _, item := range items {
		if item.Kind != "added" {
			t.Errorf("无线上版本时全部应为 added: %+v", item)
		}
	}
}

func TestDiffAIToolSnapshotsIdentical(t *testing.T) {
	same := map[string]interface{}{
		"name":       "工具A",
		"sort_order": float64(2),
	}
	items := DiffAIToolSnapshots(same, map[string]interface{}{
		"name":       "工具A",
		"sort_order": float64(2),
	})
	if len(items) != 0 {
		t.Fatalf("相同快照不应有差异，got %d", len(items))
	}
}

func TestNormalizeAIToolSnapshotJSON(t *testing.T) {
	a := `{"name":"x","sort_order":1}`
	b := `{"sort_order":1,"name":"x"}` // 字段顺序不同
	if normalizeAIToolSnapshotJSON(a) != normalizeAIToolSnapshotJSON(b) {
		t.Errorf("字段顺序不同但内容相同的快照应视为相等")
	}
	if normalizeAIToolSnapshotJSON("") != "" {
		t.Errorf("空串应返回空串")
	}
	if normalizeAIToolSnapshotJSON("{bad json") != "{bad json" {
		t.Errorf("坏 JSON 应回退原文")
	}
}

func TestSnapshotValueText(t *testing.T) {
	if got := snapshotValueText("is_published", true); got != "是" {
		t.Errorf("bool true 应显示 是, got %q", got)
	}
	if got := snapshotValueText("sort_order", float64(3)); got != "3" {
		t.Errorf("整数浮点应显示 3, got %q", got)
	}
	if got := snapshotValueText("style_presets", []interface{}{}); got != "" {
		t.Errorf("空数组应显示空, got %q", got)
	}
}
