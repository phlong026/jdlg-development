package model

import (
	"math"
	"strings"
	"time"
)

// SupportTicketSourceTypeAIProviderAlert 供应商失败率告警工单的来源标识。
// processor 的健康检查创建、总览页的活动告警查询共用，保证读写一致。
const SupportTicketSourceTypeAIProviderAlert = "ai_provider_alert"

// ───────────────────────── 模型单价（成本估算的唯一数据源） ─────────────────────────
// processor 结算落库、总览/定价页估算共用这一份；按模型名前缀匹配。
// 单价为 2026-10 供应商账单的量级估算（元/张），调价时同步更新这里并注明日期。
var aiModelUnitCostYuan = []struct {
	prefix string
	yuan   float64
}{
	{"gpt-image-2-vip", 0.20},
	{"gemini-3-pro-image-preview", 0.12},
	{"seedream-4", 0.10},
	{"gemini-3.1-flash-image-preview", 0.05},
	{"gemini-3.1-flash", 0.05},
}

const (
	AIChatCostYuanPerCall     = 0.01
	AIVideoCostYuanPerSegment = 0.60
)

// AIModelUnitCostYuan 按模型名取估算单价（元/张），未命中按最低档
func AIModelUnitCostYuan(model string) float64 {
	m := strings.ToLower(strings.TrimSpace(model))
	for _, item := range aiModelUnitCostYuan {
		if strings.HasPrefix(m, item.prefix) {
			return item.yuan
		}
	}
	return 0.05
}

// AIModelUnitCostFen 单价换算为分（int64），落库用
func AIModelUnitCostFen(model string, units int64) int64 {
	return int64(math.Round(AIModelUnitCostYuan(model) * float64(units) * 100))
}

// ───────────────────────── AI 运营统计（管理端总览页数据源） ─────────────────────────
// 全部为对现有 ai_tasks / ai_video_tasks 表的只读聚合，不新增写入字段。
// 约定：金额类成本均为"按模型单价的估算值"（口径见 route/ai_overview_management.go），
// 任务级精确成本（api_cost 落库）上线后由估算切换为精确值。

// AIDailyStat 单日调用量
type AIDailyStat struct {
	Date   string `json:"date"`
	Total  int64  `json:"total"`
	Failed int64  `json:"failed"`
}

// AISceneStat 场景分布（近 N 日）
type AISceneStat struct {
	Scene          string  `json:"scene"`
	Total          int64   `json:"total"`
	Success        int64   `json:"success"`
	Failed         int64   `json:"failed"`
	FailedRate     float64 `json:"failed_rate"`
	Stones         int64   `json:"stones"` // 成功任务的灵石收入
	CostFen        int64   `json:"cost_fen"`         // 已落库成本合计（分；二期上线后的任务才有）
	StonesWithCost int64   `json:"stones_with_cost"` // 已落库成本任务对应的灵石收入（估算残量用）
}

// AISceneModelStat 场景 × 模型分布，用于成本估算（取每场景占比最高的模型为"主模型"）
type AISceneModelStat struct {
	Scene string `json:"scene"`
	Model string `json:"model"`
	Total int64  `json:"total"`
}

// AIModelStat 供应商/模型健康度（近 N 日）
type AIModelStat struct {
	Model      string  `json:"model"`
	Total      int64   `json:"total"`
	Failed     int64   `json:"failed"`
	FailedRate float64 `json:"failed_rate"`
	AvgSeconds float64 `json:"avg_seconds"`
}

// AIQueueCounts 任务队列计数
type AIQueueCounts struct {
	Failed     int64 `json:"failed"`      // 近7日失败
	Timeout    int64 `json:"timeout"`     // 处理中超30分钟
	Running    int64 `json:"running"`     // 处理中（含待处理）
	TodayNew   int64 `json:"today_new"`   // 今日新增
	TotalOpen  int64 `json:"total_open"`  // 未完结（处理中+失败近7日）
}

// AIToolUsageStat 工具用量（近 N 日）
type AIToolUsageStat struct {
	ToolID      int64   `json:"tool_id"`
	Total       int64   `json:"total"`
	Success     int64   `json:"success"`
	FailedRate  float64 `json:"failed_rate"`
	Stones      int64   `json:"stones"`
	DailySeries []int64 `json:"daily_series"` // 近N日逐日调用，旧→新
}

// AIFailReasonStat 失败原因分类
type AIFailReasonStat struct {
	Category string `json:"category"`
	Count    int64  `json:"count"`
}

// ClassifyAIFailReason 把失败任务的错误信息归入运营可读的类别。
// 规则按线上实际错误文本的关键字归纳；未命中归"其他"。
func ClassifyAIFailReason(errMsg string) string {
	msg := strings.ToLower(errMsg)
	switch {
	case strings.Contains(msg, "timeout") || strings.Contains(msg, "超时") ||
		strings.Contains(msg, "context deadline") || strings.Contains(msg, "轮询"):
		return "上游超时"
	case strings.Contains(msg, "quota") || strings.Contains(msg, "配额") ||
		strings.Contains(msg, "余额") || strings.Contains(msg, "insufficient") ||
		strings.Contains(msg, "429"):
		return "配额/余额不足"
	case strings.Contains(msg, "safety") || strings.Contains(msg, "安全") ||
		strings.Contains(msg, "blocked") || strings.Contains(msg, "content") ||
		strings.Contains(msg, "敏感") || strings.Contains(msg, "拦截"):
		return "内容安全拦截"
	case strings.Contains(msg, "parse") || strings.Contains(msg, "解析") ||
		strings.Contains(msg, "image") || strings.Contains(msg, "图片") ||
		strings.Contains(msg, "b64") || strings.Contains(msg, "decode"):
		return "结果解析失败"
	default:
		return "其他"
	}
}

func dateStr(t time.Time) string { return t.Format("2006-01-02") }

// DailyImageStats 近 N 日逐日调用量（图片任务）
func (m *AITaskModel) DailyImageStats(since time.Time) ([]AIDailyStat, error) {
	rows, err := m.DB.Query(
		`SELECT DATE(created_at), COUNT(*), COALESCE(SUM(status = 'failed'), 0)
		 FROM ai_tasks WHERE created_at >= ? GROUP BY DATE(created_at) ORDER BY DATE(created_at)`, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AIDailyStat
	for rows.Next() {
		var d AIDailyStat
		var day string
		if err := rows.Scan(&day, &d.Total, &d.Failed); err != nil {
			continue
		}
		d.Date = day
		out = append(out, d)
	}
	return out, rows.Err()
}

// DailyVideoStats 近 N 日逐日调用量（视频任务）
func (m *AIVideoTaskModel) DailyVideoStats(since time.Time) ([]AIDailyStat, error) {
	rows, err := m.DB.Query(
		`SELECT DATE(created_at), COUNT(*), COALESCE(SUM(status = 'failed'), 0)
		 FROM ai_video_tasks WHERE created_at >= ? GROUP BY DATE(created_at) ORDER BY DATE(created_at)`, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AIDailyStat
	for rows.Next() {
		var d AIDailyStat
		var day string
		if err := rows.Scan(&day, &d.Total, &d.Failed); err != nil {
			continue
		}
		d.Date = day
		out = append(out, d)
	}
	return out, rows.Err()
}

// DayKPI 单日 KPI（今日/昨日共用）
type DayKPI struct {
	Total          int64 `json:"total"`
	Failed         int64 `json:"failed"`
	Stones         int64 `json:"stones"` // 成功任务的灵石收入
	CostFen        int64 `json:"cost_fen"`
	StonesWithCost int64 `json:"stones_with_cost"`
}

// ImageDayKPI 图片任务某日 KPI（dayStart 为当天 0 点）
func (m *AITaskModel) ImageDayKPI(dayStart time.Time) (DayKPI, error) {
	var k DayKPI
	err := m.DB.QueryRow(
		`SELECT COUNT(*), COALESCE(SUM(status = 'failed'), 0),
		        COALESCE(SUM(CASE WHEN status = 'success' THEN stones_used ELSE 0 END), 0),
		        COALESCE(SUM(CASE WHEN status = 'success' AND api_cost IS NOT NULL THEN api_cost ELSE 0 END), 0),
		        COALESCE(SUM(CASE WHEN status = 'success' AND api_cost IS NOT NULL THEN stones_used ELSE 0 END), 0)
		 FROM ai_tasks WHERE created_at >= ? AND created_at < ?`, dayStart, dayStart.Add(24*time.Hour)).
		Scan(&k.Total, &k.Failed, &k.Stones, &k.CostFen, &k.StonesWithCost)
	if err != nil {
		return DayKPI{}, err
	}
	return k, nil
}

// VideoDayKPI 视频任务某日 KPI（视频按段计费，灵石由定价表推算，此处只给量）
func (m *AIVideoTaskModel) VideoDayKPI(dayStart time.Time) (DayKPI, error) {
	var k DayKPI
	err := m.DB.QueryRow(
		`SELECT COUNT(*), COALESCE(SUM(status = 'failed'), 0), 0
		 FROM ai_video_tasks WHERE created_at >= ? AND created_at < ?`, dayStart, dayStart.Add(24*time.Hour)).
		Scan(&k.Total, &k.Failed, &k.Stones)
	if err != nil {
		return DayKPI{}, err
	}
	return k, nil
}

// SceneStats 图片任务按场景聚合（近 N 日）
func (m *AITaskModel) SceneStats(since time.Time) ([]AISceneStat, error) {
	rows, err := m.DB.Query(
		`SELECT scene, COUNT(*), COALESCE(SUM(status = 'success'), 0),
		        COALESCE(SUM(CASE WHEN status = 'failed' THEN 1 ELSE 0 END), 0),
		        COALESCE(SUM(CASE WHEN status = 'success' THEN stones_used ELSE 0 END), 0),
		        COALESCE(SUM(CASE WHEN status = 'success' AND api_cost IS NOT NULL THEN api_cost ELSE 0 END), 0),
		        COALESCE(SUM(CASE WHEN status = 'success' AND api_cost IS NOT NULL THEN stones_used ELSE 0 END), 0)
		 FROM ai_tasks WHERE created_at >= ? GROUP BY scene ORDER BY COUNT(*) DESC`, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AISceneStat
	for rows.Next() {
		var s AISceneStat
		if err := rows.Scan(&s.Scene, &s.Total, &s.Success, &s.Failed, &s.Stones, &s.CostFen, &s.StonesWithCost); err != nil {
			continue
		}
		if s.Total > 0 {
			s.FailedRate = float64(s.Failed) / float64(s.Total)
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// SceneModelStats 场景 × 实际执行模型分布（近 N 日），用于成本估算
func (m *AITaskModel) SceneModelStats(since time.Time) ([]AISceneModelStat, error) {
	rows, err := m.DB.Query(
		`SELECT scene, COALESCE(NULLIF(TRIM(model), ''), '未知'), COUNT(*)
		 FROM ai_tasks WHERE created_at >= ? GROUP BY scene, COALESCE(NULLIF(TRIM(model), ''), '未知')`, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AISceneModelStat
	for rows.Next() {
		var s AISceneModelStat
		if err := rows.Scan(&s.Scene, &s.Model, &s.Total); err != nil {
			continue
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// ModelStats 图片任务按实际执行模型聚合（近 N 日），用于供应商健康度
func (m *AITaskModel) ModelStats(since time.Time) ([]AIModelStat, error) {
	rows, err := m.DB.Query(
		`SELECT COALESCE(NULLIF(TRIM(model), ''), '未知'), COUNT(*),
		        COALESCE(SUM(status = 'failed'), 0),
		        COALESCE(AVG(CASE WHEN status IN ('success', 'failed')
		              THEN TIMESTAMPDIFF(SECOND, created_at, updated_at) END), 0)
		 FROM ai_tasks WHERE created_at >= ?
		 GROUP BY COALESCE(NULLIF(TRIM(model), ''), '未知') ORDER BY COUNT(*) DESC`, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AIModelStat
	for rows.Next() {
		var s AIModelStat
		if err := rows.Scan(&s.Model, &s.Total, &s.Failed, &s.AvgSeconds); err != nil {
			continue
		}
		if s.Total > 0 {
			s.FailedRate = float64(s.Failed) / float64(s.Total)
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// ModelWindowStats 近 1 小时按模型聚合，供供应商失败率告警判定
func (m *AITaskModel) ModelWindowStats(since time.Time) ([]AIModelStat, error) {
	return m.ModelStats(since)
}

// QueueCounts 各处理队列计数
func (m *AITaskModel) QueueCounts() (*AIQueueCounts, error) {
	q := &AIQueueCounts{}
	err := m.DB.QueryRow(
		`SELECT
		   COALESCE(SUM(status = 'failed' AND created_at >= NOW() - INTERVAL 7 DAY), 0),
		   COALESCE(SUM(status IN ('pending', 'running', 'processing') AND updated_at < NOW() - INTERVAL 30 MINUTE), 0),
		   COALESCE(SUM(status IN ('pending', 'running', 'processing')), 0),
		   COALESCE(SUM(created_at >= CURDATE()), 0)
		 FROM ai_tasks`).Scan(&q.Failed, &q.Timeout, &q.Running, &q.TodayNew)
	if err != nil {
		return nil, err
	}
	q.TotalOpen = q.Running + q.Failed
	return q, nil
}

// ToolUsageStats 工具用量聚合（近 N 日，含逐日序列）
func (m *AITaskModel) ToolUsageStats(since time.Time, days int) (map[int64]*AIToolUsageStat, error) {
	if days < 1 {
		days = 7
	}
	stats := make(map[int64]*AIToolUsageStat)
	rows, err := m.DB.Query(
		`SELECT tool_id, COUNT(*), COALESCE(SUM(status = 'success'), 0),
		        COALESCE(SUM(CASE WHEN status = 'failed' THEN 1 ELSE 0 END), 0),
		        COALESCE(SUM(CASE WHEN status = 'success' THEN stones_used ELSE 0 END), 0)
		 FROM ai_tasks WHERE created_at >= ? AND tool_id IS NOT NULL GROUP BY tool_id`, since)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		s := &AIToolUsageStat{DailySeries: make([]int64, days)}
		var failed int64
		if err := rows.Scan(&s.ToolID, &s.Total, &s.Success, &failed, &s.Stones); err != nil {
			continue
		}
		if s.Total > 0 {
			s.FailedRate = float64(failed) / float64(s.Total)
		}
		stats[s.ToolID] = s
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// 逐日序列
	seriesRows, err := m.DB.Query(
		`SELECT tool_id, DATE(created_at), COUNT(*)
		 FROM ai_tasks WHERE created_at >= ? AND tool_id IS NOT NULL
		 GROUP BY tool_id, DATE(created_at)`, since)
	if err != nil {
		return stats, nil
	}
	defer seriesRows.Close()
	dayIndex := make(map[string]int)
	for i := 0; i < days; i++ {
		dayIndex[dateStr(since.AddDate(0, 0, i))] = i
	}
	for seriesRows.Next() {
		var toolID int64
		var day string
		var n int64
		if err := seriesRows.Scan(&toolID, &day, &n); err != nil {
			continue
		}
		if s, ok := stats[toolID]; ok {
			if idx, ok2 := dayIndex[day]; ok2 {
				s.DailySeries[idx] = n
			}
		}
	}
	return stats, nil
}

// FailReasonStats 失败原因分类统计（近 N 日，最多抽样 2000 条）
func (m *AITaskModel) FailReasonStats(since time.Time) ([]AIFailReasonStat, error) {
	rows, err := m.DB.Query(
		`SELECT COALESCE(error_message, '') FROM ai_tasks
		 WHERE status = 'failed' AND created_at >= ?
		 ORDER BY created_at DESC LIMIT 2000`, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	counts := make(map[string]int64)
	var sampled int64
	for rows.Next() {
		var msg string
		if err := rows.Scan(&msg); err != nil {
			continue
		}
		sampled++
		counts[ClassifyAIFailReason(msg)]++
	}
	out := make([]AIFailReasonStat, 0, len(counts))
	for category, count := range counts {
		out = append(out, AIFailReasonStat{Category: category, Count: count})
	}
	// 按数量降序
	for i := 0; i < len(out); i++ {
		for j := i + 1; j < len(out); j++ {
			if out[j].Count > out[i].Count {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	return out, nil
}

// VideoQueueCounts 视频任务队列计数（并入总览展示）
func (m *AIVideoTaskModel) VideoQueueCounts() (*AIQueueCounts, error) {
	q := &AIQueueCounts{}
	err := m.DB.QueryRow(
		`SELECT
		   COALESCE(SUM(status = 'failed' AND created_at >= NOW() - INTERVAL 7 DAY), 0),
		   COALESCE(SUM(status IN ('queued', 'processing') AND updated_at < NOW() - INTERVAL 30 MINUTE), 0),
		   COALESCE(SUM(status IN ('queued', 'processing')), 0),
		   COALESCE(SUM(created_at >= CURDATE()), 0)
		 FROM ai_video_tasks`).Scan(&q.Failed, &q.Timeout, &q.Running, &q.TodayNew)
	if err != nil {
		return nil, err
	}
	q.TotalOpen = q.Running + q.Failed
	return q, nil
}

// ToolWindowStat 工具在某时间窗内的调用统计（版本效果对比用）
type ToolWindowStat struct {
	Total   int64 `json:"total"`
	Success int64 `json:"success"`
}

// ToolWindowStats 某工具在 [from, to) 的调用量与成功数
func (m *AITaskModel) ToolWindowStats(toolID int64, from, to time.Time) (ToolWindowStat, error) {
	var s ToolWindowStat
	err := m.DB.QueryRow(
		`SELECT COUNT(*), COALESCE(SUM(status = 'success'), 0)
		 FROM ai_tasks WHERE tool_id = ? AND created_at >= ? AND created_at < ?`,
		toolID, from, to).Scan(&s.Total, &s.Success)
	return s, err
}

// ListRecentFailedErrorMessages 拉取近 N 日失败任务的错误文本（告警工单正文用）
func (m *AITaskModel) ListRecentFailedErrorMessages(since time.Time, limit int) ([]string, error) {
	if limit < 1 {
		limit = 20
	}
	rows, err := m.DB.Query(
		`SELECT COALESCE(error_message, '') FROM ai_tasks
		 WHERE status = 'failed' AND created_at >= ? ORDER BY created_at DESC LIMIT ?`, since, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var msg string
		if err := rows.Scan(&msg); err != nil {
			continue
		}
		out = append(out, msg)
	}
	return out, rows.Err()
}
