package route

import (
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"service/model"
)

// ───────────────────────── AI 运营总览（管理端） ─────────────────────────
// 数据源全部为现有表的只读聚合。成本口径：已落库 api_cost 优先 + 历史任务按 model.AIModelUnitCostYuan 估算，
// 任务级精确成本（api_cost 落库）上线后仅需替换估算函数。

// 供应商告警工单使用的来源标识（processor 健康检查创建、本页查询共用 model 里的常量）
const aiProviderAlertSourceType = model.SupportTicketSourceTypeAIProviderAlert

// 成本估算口径：模型单价表统一由 model.AIModelUnitCostYuan 提供（processor 落库与页面估算共用）；
// 金额口径 = 已落库 api_cost 合计 + 未落库部分（历史任务）按单价估算，页面标注切换时间。

// aiSupplierChainNode 降级链节点。
// 与 component.AITaskProcessor.getFallbackAIConfigs 的硬编码链路一一对应：
// 调整执行链路时必须同步更新此处描述（本页只读展示，在线调序属后续能力）。
type aiSupplierChainNode struct {
	Role          string `json:"role"` // primary / fallback_1 / fallback_2
	Model         string `json:"model"`
	ProviderName  string `json:"provider_name"`
	ProviderCode  string `json:"provider_code"`
	Protocol      string `json:"protocol"`
	ProtocolLabel string `json:"protocol_label"`
	Note          string `json:"note,omitempty"`
}

func aiProtocolLabel(protocol string) string {
	switch protocol {
	case "openai_image_sync":
		return "同步返回（OpenAI Image）"
	case "gemini_sync":
		return "同步返回（Gemini）"
	case "toapis_async":
		return "异步任务返回（ToAPIs）"
	case "chat_sync":
		return "聊天同步返回"
	default:
		return protocol
	}
}

func aiDrawSupplierChain() []aiSupplierChainNode {
	return []aiSupplierChainNode{
		{Role: "primary", Model: "gpt-image-2-vip", ProviderName: "GPT-Image-2 VIP", ProviderCode: "gpt-image-2-vip", Protocol: "openai_image_sync", ProtocolLabel: aiProtocolLabel("openai_image_sync"), Note: "主力：后台可配的主配置（AI 配置 → API 配置）"},
		{Role: "fallback_1", Model: "gemini-3-pro-image-preview", ProviderName: "Gemini 3 Pro Image", ProviderCode: "laozhang", Protocol: "gemini_sync", ProtocolLabel: aiProtocolLabel("gemini_sync"), Note: "备用1：主力失败自动切换"},
		{Role: "fallback_2", Model: "seedream-4-5-251128", ProviderName: "Seedream 4.5", ProviderCode: "laozhang", Protocol: "openai_image_sync", ProtocolLabel: aiProtocolLabel("openai_image_sync"), Note: "备用2：最后承接"},
	}
}

// aiSceneMeta 场景中文名与计费单位（与前端 ai-config 的映射同源，后端再放一份供接口返回）
var aiSceneMeta = map[string]struct {
	Label    string
	CostUnit string // image / image4 / chat / video / doc
}{
	"ai_draw_single":                 {"AI 绘画-单图", "image"},
	"ai_draw_multi":                  {"AI 绘画-多图", "image4"},
	"new_minnan_tool_text_to_image":  {"新闽派工具-文生图", "image"},
	"new_minnan_tool_image_to_image": {"新闽派工具-图生图", "image"},
	"ai_chat_single":                 {"AI 对话-单轮", "chat"},
	"ai_chat_multi":                  {"AI 对话-多轮", "chat"},
	"building_code_chat":             {"建规快查", "chat"},
	"rural_villa_design":             {"乡村别墅设计", "image"},
	"allround_design":                {"全能设计", "image"},
	"building_replacement":           {"建筑换新", "image"},
	"parent_child_design":            {"亲子设计", "image"},
	"ai_cost_doc":                    {"AI 造价文档", "doc"},
	"ai_video_1":                     {"AI 视频-1段", "video"},
	"ai_video_2":                     {"AI 视频-2段", "video"},
	"ai_video_3":                     {"AI 视频-3段", "video"},
	"ai_video_4":                     {"AI 视频-4段", "video"},
}

func aiSceneLabel(scene string) string {
	if meta, ok := aiSceneMeta[scene]; ok {
		return meta.Label
	}
	return scene
}

// aiEstimateSceneCostYuan 场景级成本估算：成功收入灵石 ÷ 单价灵石 = 单位数，再乘单位成本
func aiEstimateSceneCostYuan(scene string, stones int64, unitStones int64, dominantModel string) float64 {
	if unitStones <= 0 || stones <= 0 {
		return 0
	}
	units := float64(stones) / float64(unitStones)
	meta, ok := aiSceneMeta[scene]
	if !ok {
		meta.CostUnit = "image"
	}
	switch {
	case meta.CostUnit == "chat":
		return units * model.AIChatCostYuanPerCall
	case meta.CostUnit == "image4":
		return units * 4 * model.AIModelUnitCostYuan(dominantModel)
	case meta.CostUnit == "video":
		// 视频任务不在 ai_tasks 表，场景级成本由视频段数单独估算（见 video 段聚合）
		return 0
	case meta.CostUnit == "doc":
		return units * model.AIModelUnitCostYuan("gemini-3-pro-image-preview")
	default:
		return units * model.AIModelUnitCostYuan(dominantModel)
	}
}

func aiMarginRate(stones int64, costYuan float64) float64 {
	revenueYuan := float64(stones) / 10.0 // 灵石 → 元：10 灵石 = ¥1（充值 1:10）
	if revenueYuan <= 0 {
		return 0
	}
	return (revenueYuan - costYuan) / revenueYuan
}

type aiOverviewKPI struct {
	Total        int64   `json:"total"`
	Failed       int64   `json:"failed"`
	SuccessRate  float64 `json:"success_rate"`
	StonesRevenue int64  `json:"stones_revenue"`
	CostYuan     float64 `json:"cost_yuan"`
}

func buildAIOverviewKPI(imageKPI model.DayKPI, videoKPI model.DayKPI, videoCostYuan float64) aiOverviewKPI {
	k := aiOverviewKPI{
		Total:  imageKPI.Total + videoKPI.Total,
		Failed: imageKPI.Failed + videoKPI.Failed,
		StonesRevenue: imageKPI.Stones,
	}
	if k.Total > 0 {
		k.SuccessRate = float64(k.Total-k.Failed) / float64(k.Total)
	}
	k.CostYuan = videoCostYuan
	return k
}

// aiChainSuggestion 基于近7日健康度给出降级链调序建议（只读建议，不自动执行）。
// 规则：相邻两个备用节点中，若低优先级节点（B）同时满足 失败率明显更低（差 ≥5 个百分点）
// 且 平均耗时更短，且两者 7 日调用量都 ≥30（样本可信），建议对调。
func aiChainSuggestion(suppliers []gin.H) *gin.H {
	type node struct {
		role        string
		model       string
		name        string
		total       int64
		failedRate  float64
		avgSeconds  float64
	}
	byRole := make(map[string]node)
	for _, s := range suppliers {
		role, _ := s["role"].(string)
		total, _ := s["total_7d"].(int64)
		failedRate, _ := s["failed_rate_7d"].(float64)
		avgSeconds, _ := s["avg_seconds_7d"].(float64)
		model, _ := s["model"].(string)
		name, _ := s["provider_name"].(string)
		byRole[role] = node{role: role, model: model, name: name, total: total, failedRate: failedRate, avgSeconds: avgSeconds}
	}
	fb1, ok1 := byRole["fallback_1"]
	fb2, ok2 := byRole["fallback_2"]
	if !ok1 || !ok2 {
		return nil
	}
	if fb1.total < 30 || fb2.total < 30 {
		return &gin.H{"suggested": false, "reason": "备用节点近7日样本不足（各需 ≥30 次），暂不给调序建议"}
	}
	if fb2.failedRate+0.05 <= fb1.failedRate && fb2.avgSeconds < fb1.avgSeconds {
		return &gin.H{
			"suggested": true,
			"swap":      []string{fb1.name, fb2.name},
			"reason": fmt.Sprintf("%s 近7日失败率 %.1f%%、平均耗时 %.1fs，两项均优于 %s（%.1f%% / %.1fs）。建议把 %s 提为备用1——需改服务端降级链并发版，本页不自动执行。",
				fb2.name, fb2.failedRate*100, fb2.avgSeconds, fb1.name, fb1.failedRate*100, fb1.avgSeconds, fb2.name),
		}
	}
	return &gin.H{"suggested": false, "reason": "当前顺序与健康度表现一致，无需调整"}
}

// RegisterAIOverviewManagementRoutes 注册 AI 运营总览相关路由
func RegisterAIOverviewManagementRoutes(
	r *gin.RouterGroup,
	taskModel *model.AITaskModel,
	videoTaskModel *model.AIVideoTaskModel,
	pricingModel *model.AIPricingModel,
	pricingVersionModel *model.AIPricingVersionModel,
	aiToolModel *model.AIToolModel,
	supportTicketModel *model.SupportTicketModel,
	opsSettingsModel *model.AIOpsSettingsModel,
) {
	ai := r.Group("/ai")

	// 告警设置（供应商管理页"告警设置"卡）：processor 每 1 分钟读取一次，改完下个周期生效
	ai.GET("/ops-settings", func(c *gin.Context) {
		if opsSettingsModel == nil {
			c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "success", "data": model.DefaultAIOpsSettings()})
			return
		}
		settings, err := opsSettingsModel.Get()
		if err != nil {
			settings = model.DefaultAIOpsSettings()
		}
		c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "success", "data": settings})
	})

	ai.PUT("/ops-settings", func(c *gin.Context) {
		if opsSettingsModel == nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "设置服务不可用"})
			return
		}
		var req model.AIOpsSettings
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": FormatValidationError("参数错误: " + err.Error())})
			return
		}
		if err := opsSettingsModel.Update(&req); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "保存失败: " + err.Error()})
			return
		}
		saved, _ := opsSettingsModel.Get()
		if saved == nil {
			saved = model.DefaultAIOpsSettings()
		}
		c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "保存成功，下个检查周期生效", "data": saved})
	})

	// AI 运营总览
	ai.GET("/overview", func(c *gin.Context) {
		if taskModel == nil {
			c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "success", "data": gin.H{}})
			return
		}
		now := time.Now()
		todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
		since7d := now.AddDate(0, 0, -7)
		since14d := now.AddDate(0, 0, -14)

		// ── KPI（今日 / 昨日）──
		imgToday, _ := taskModel.ImageDayKPI(todayStart)
		imgYesterday, _ := taskModel.ImageDayKPI(todayStart.AddDate(0, 0, -1))
		var vidToday, vidYesterday model.DayKPI
		if videoTaskModel != nil {
			vidToday, _ = videoTaskModel.VideoDayKPI(todayStart)
			vidYesterday, _ = videoTaskModel.VideoDayKPI(todayStart.AddDate(0, 0, -1))
		}

		// 场景级收入与成本估算（今日与 7 日共用一份数据，7 日给分布、今日给 KPI 精确值）
		sceneStats, _ := taskModel.SceneStats(since7d)
		sceneModelStats, _ := taskModel.SceneModelStats(since7d)
		dominantModel := make(map[string]string)
		sceneModelCount := make(map[string]map[string]int64)
		for _, sm := range sceneModelStats {
			if sceneModelCount[sm.Scene] == nil {
				sceneModelCount[sm.Scene] = make(map[string]int64)
			}
			sceneModelCount[sm.Scene][sm.Model] += sm.Total
		}
		for scene, counts := range sceneModelCount {
			best, bestN := "", int64(-1)
			for m, n := range counts {
				if n > bestN {
					best, bestN = m, n
				}
			}
			dominantModel[scene] = best
		}
		pricingList, _ := pricingModel.GetAll()
		unitStones := make(map[string]int64)
		for _, p := range pricingList {
			unitStones[p.Scene] = p.Stones
		}
		// 场景级成本（混合口径）：已落库 api_cost 合计 + 未落库残量按单价估算
		sceneCost7d := make(map[string]float64)
		for _, s := range sceneStats {
			if s.Scene == "" {
				continue
			}
			residualStones := s.Stones - s.StonesWithCost
			if residualStones < 0 {
				residualStones = 0
			}
			sceneCost7d[s.Scene] = float64(s.CostFen)/100 +
				aiEstimateSceneCostYuan(s.Scene, residualStones, unitStones[s.Scene], dominantModel[s.Scene])
		}

		// 视频今日成本（按段估算）
		videoCostToday := 0.0
		videoStats7d := gin.H{"total": int64(0), "failed": int64(0), "segments": int64(0), "stones": int64(0), "cost_yuan": 0.0}
		if videoTaskModel != nil {
			segRows, err := videoTaskModel.DB.Query(
				`SELECT segment_count, COUNT(*), COALESCE(SUM(status = 'failed'), 0),
				        COALESCE(SUM(CASE WHEN status = 'completed' THEN 1 ELSE 0 END), 0)
				 FROM ai_video_tasks WHERE created_at >= ? GROUP BY segment_count`, since7d)
			if err == nil {
				var totalN, failedN, segmentsN, stonesN int64
				for segRows.Next() {
					var seg, n, failed, done int64
					if err := segRows.Scan(&seg, &n, &failed, &done); err != nil {
						continue
					}
					totalN += n
					failedN += failed
					segmentsN += seg * n
					if pricingModel != nil {
						stonesN += done * getVideoStones(pricingModel, int(seg))
					}
				}
				segRows.Close()
				videoCost7d := float64(segmentsN) * model.AIVideoCostYuanPerSegment
				videoStats7d = gin.H{"total": totalN, "failed": failedN, "segments": segmentsN, "stones": stonesN, "cost_yuan": videoCost7d}
				// 今日视频成本按今日任务数占 7 日比例粗估（今日段数单独查太细，量级即可）
				if totalN > 0 && vidToday.Total > 0 {
					videoCostToday = videoCost7d * float64(vidToday.Total) / float64(totalN)
				}
			}
		}

		// 今日图片成本（混合口径）：当日落库 api_cost 直接用；未落库残量按当日灵石占 7 日残量比例折算
		imageCostToday := 0.0
		if imgToday.Stones > 0 {
			var total7dStones, total7dResidualStones int64
			var total7dResidualEst float64
			for _, s := range sceneStats {
				total7dStones += s.Stones
				residual := s.Stones - s.StonesWithCost
				if residual < 0 {
					residual = 0
				}
				total7dResidualStones += residual
				total7dResidualEst += aiEstimateSceneCostYuan(s.Scene, residual, unitStones[s.Scene], dominantModel[s.Scene])
			}
			todayResidualStones := imgToday.Stones - imgToday.StonesWithCost
			if todayResidualStones < 0 {
				todayResidualStones = 0
			}
			residualToday := 0.0
			if total7dResidualStones > 0 && total7dStones > 0 {
				residualToday = total7dResidualEst * float64(todayResidualStones) / float64(total7dResidualStones)
			}
			imageCostToday = float64(imgToday.CostFen)/100 + residualToday
		}

		kpiToday := buildAIOverviewKPI(imgToday, vidToday, videoCostToday)
		kpiToday.CostYuan += imageCostToday
		kpiYesterday := buildAIOverviewKPI(imgYesterday, vidYesterday, 0)

		// ── 趋势（14 日，图+视频合并）──
		imgDaily, _ := taskModel.DailyImageStats(since14d)
		dailyMap := make(map[string]*gin.H)
		trend := make([]gin.H, 0, 14)
		for i := 13; i >= 0; i-- {
			date := now.AddDate(0, 0, -i).Format("2006-01-02")
			entry := gin.H{"date": date, "total": int64(0), "failed": int64(0), "video_total": int64(0)}
			dailyMap[date] = &entry
			trend = append(trend, entry)
		}
		for _, d := range imgDaily {
			if entry, ok := dailyMap[d.Date]; ok {
				(*entry)["total"] = d.Total
				(*entry)["failed"] = d.Failed
			}
		}
		if videoTaskModel != nil {
			vidDaily, _ := videoTaskModel.DailyVideoStats(since14d)
			for _, d := range vidDaily {
				if entry, ok := dailyMap[d.Date]; ok {
					(*entry)["video_total"] = d.Total
					(*entry)["total"] = (*entry)["total"].(int64) + d.Total
					(*entry)["failed"] = (*entry)["failed"].(int64) + d.Failed
				}
			}
		}

		// ── 场景分布（7 日）──
		scenes := make([]gin.H, 0, len(sceneStats))
		for _, s := range sceneStats {
			if s.Scene == "" {
				continue
			}
			cost := sceneCost7d[s.Scene]
			scenes = append(scenes, gin.H{
				"scene": s.Scene, "label": aiSceneLabel(s.Scene),
				"total": s.Total, "success": s.Success, "failed": s.Failed, "failed_rate": s.FailedRate,
				"stones": s.Stones, "cost_yuan": cost, "margin_rate": aiMarginRate(s.Stones, cost),
			})
		}

		// ── 供应商健康度（7 日 + 近1小时告警窗口）──
		modelStats, _ := taskModel.ModelStats(since7d)
		windowStats, _ := taskModel.ModelWindowStats(now.Add(-1 * time.Hour))
		statByModel := make(map[string]model.AIModelStat)
		for _, s := range modelStats {
			statByModel[strings.ToLower(s.Model)] = s
		}
		windowByModel := make(map[string]model.AIModelStat)
		for _, s := range windowStats {
			windowByModel[strings.ToLower(s.Model)] = s
		}
		chain := aiDrawSupplierChain()
		suppliers := make([]gin.H, 0, len(chain))
		fallbackOut := int64(0)
		for _, node := range chain {
			key := strings.ToLower(node.Model)
			stat := statByModel[key]
			window := windowByModel[key]
			alerting := window.Total >= 20 && window.FailedRate > 0.15
			if node.Role != "primary" {
				fallbackOut += stat.Total
			}
			suppliers = append(suppliers, gin.H{
				"role": node.Role, "model": node.Model, "provider_name": node.ProviderName,
				"provider_code": node.ProviderCode, "protocol": node.Protocol, "protocol_label": node.ProtocolLabel,
				"note": node.Note,
				"total_7d": stat.Total, "failed_rate_7d": stat.FailedRate, "avg_seconds_7d": stat.AvgSeconds,
				"window_total_1h": window.Total, "window_failed_rate_1h": window.FailedRate,
				"alerting": alerting,
				"cost_yuan_per_unit": model.AIModelUnitCostYuan(node.Model),
			})
		}
		// 模型统计里出现、但不在链路描述中的模型（如历史/试验模型）也展示，标记 role=other
		chainModels := make(map[string]bool)
		for _, node := range chain {
			chainModels[strings.ToLower(node.Model)] = true
		}
		for _, stat := range modelStats {
			if !chainModels[strings.ToLower(stat.Model)] {
				suppliers = append(suppliers, gin.H{
					"role": "other", "model": stat.Model, "provider_name": stat.Model,
					"provider_code": "", "protocol": "", "protocol_label": "历史/其他",
					"note": "近7日有执行记录，但不在当前降级链描述中",
					"total_7d": stat.Total, "failed_rate_7d": stat.FailedRate, "avg_seconds_7d": stat.AvgSeconds,
					"window_total_1h": windowByModel[strings.ToLower(stat.Model)].Total,
					"window_failed_rate_1h": windowByModel[strings.ToLower(stat.Model)].FailedRate,
					"alerting": false,
					"cost_yuan_per_unit": model.AIModelUnitCostYuan(stat.Model),
				})
			}
		}

		// ── 失败原因（7 日）──
		failReasons, _ := taskModel.FailReasonStats(since7d)

		// ── 工具热度（7 日 Top5）──
		toolsTop := make([]gin.H, 0, 5)
		if aiToolModel != nil {
			toolStats, _ := taskModel.ToolUsageStats(since7d, 7)
			type toolRow struct {
				id    int64
				name  string
				total int64
			}
			rows := make([]toolRow, 0, len(toolStats))
			for toolID, stat := range toolStats {
				tool, err := aiToolModel.GetByID(toolID)
				if err != nil || tool == nil {
					continue
				}
				rows = append(rows, toolRow{id: toolID, name: tool.Name, total: stat.Total})
			}
			sort.Slice(rows, func(i, j int) bool { return rows[i].total > rows[j].total })
			for i, row := range rows {
				if i >= 5 {
					break
				}
				toolsTop = append(toolsTop, gin.H{"tool_id": row.id, "name": row.name, "total_7d": row.total})
			}
		}

		// ── 队列计数 ──
		imgQueues, _ := taskModel.QueueCounts()
		queues := gin.H{"image": imgQueues, "video": nil}
		if videoTaskModel != nil {
			vidQueues, _ := videoTaskModel.VideoQueueCounts()
			queues["video"] = vidQueues
		}

		// ── 活动告警（未关闭的供应商告警工单）──
		alerts := make([]gin.H, 0)
		if supportTicketModel != nil && supportTicketModel.DB != nil {
			tickets, _, err := supportTicketModel.ListForManagement(model.SupportTicketListParams{
				Status: "open", SourceType: aiProviderAlertSourceType, Limit: 5,
			})
			if err == nil {
				for _, t := range tickets {
					alerts = append(alerts, gin.H{"id": t.ID, "title": t.Title, "priority": t.Priority, "created_at": t.CreatedAt})
				}
			}
		}

		stonesToday := imgToday.Stones
		if v, ok := videoStats7d["stones"].(int64); ok {
			// 视频灵石收入并入今日 KPI（按 7 日均值折算，口径注明）
			if total7d, ok2 := videoStats7d["total"].(int64); ok2 && total7d > 0 && vidToday.Total > 0 {
				stonesToday += v * vidToday.Total / total7d
			}
		}

		// 成本落库占比（今日）：衡量口径从估算切到精确的进度，前端展示用
		costLandedRatio := 0.0
		if imgToday.Stones > 0 {
			costLandedRatio = float64(imgToday.StonesWithCost) / float64(imgToday.Stones)
		}

		c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "success", "data": gin.H{
			"kpi": gin.H{
				"today":     gin.H{"total": kpiToday.Total, "failed": kpiToday.Failed, "success_rate": kpiToday.SuccessRate, "stones_revenue": stonesToday, "cost_yuan": kpiToday.CostYuan, "margin_rate": aiMarginRate(stonesToday, kpiToday.CostYuan)},
				"yesterday": gin.H{"total": kpiYesterday.Total, "failed": kpiYesterday.Failed, "success_rate": kpiYesterday.SuccessRate},
				"cost_note":         "成本口径：已落库任务用精确值（api_cost），历史任务按模型单价估算（图片 ¥0.05~0.20/张、对话 ¥0.01/次、视频 ¥0.60/段）",
				"cost_landed_ratio": costLandedRatio,
			},
			"trend":        trend,
			"scenes":       scenes,
			"video_7d":     videoStats7d,
			"suppliers":    suppliers,
			"chain":        chain,
			"fallback_out_7d": fallbackOut,
			"fail_reasons": failReasons,
			"tools_top":    toolsTop,
			"queues":       queues,
			"alerts":       alerts,
			"chain_suggestion": aiChainSuggestion(suppliers),
		}})
	})

	// 定价运营视角：定价 × 成本估算 × 毛利 × 7日调用量 × 最近调价
	ai.GET("/pricing/ops", func(c *gin.Context) {
		if pricingModel == nil {
			c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "success", "data": []gin.H{}})
			return
		}
		pricings, err := pricingModel.GetAll()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "获取定价失败: " + err.Error()})
			return
		}
		since7d := time.Now().AddDate(0, 0, -7)
		var sceneStats []model.AISceneStat
		var sceneModelStats []model.AISceneModelStat
		if taskModel != nil {
			sceneStats, _ = taskModel.SceneStats(since7d)
			sceneModelStats, _ = taskModel.SceneModelStats(since7d)
		}
		statByScene := make(map[string]model.AISceneStat)
		for _, s := range sceneStats {
			statByScene[s.Scene] = s
		}
		dominantModel := make(map[string]string)
		sceneModelCount := make(map[string]map[string]int64)
		for _, sm := range sceneModelStats {
			if sceneModelCount[sm.Scene] == nil {
				sceneModelCount[sm.Scene] = make(map[string]int64)
			}
			sceneModelCount[sm.Scene][sm.Model] += sm.Total
		}
		for scene, counts := range sceneModelCount {
			best, bestN := "", int64(-1)
			for m, n := range counts {
				if n > bestN {
					best, bestN = m, n
				}
			}
			dominantModel[scene] = best
		}

		var latestVersions map[string]*model.AIPricingVersion
		if pricingVersionModel != nil {
			latestVersions, _ = pricingVersionModel.LatestVersionMap()
		}

		list := make([]gin.H, 0, len(pricings))
		for _, p := range pricings {
			stat := statByScene[p.Scene]
			// 混合口径：落库成本 + 未落库残量估算
			residualStones := stat.Stones - stat.StonesWithCost
			if residualStones < 0 {
				residualStones = 0
			}
			cost := float64(stat.CostFen)/100 + aiEstimateSceneCostYuan(p.Scene, residualStones, p.Stones, dominantModel[p.Scene])
			item := gin.H{
				"scene": p.Scene, "label": aiSceneLabel(p.Scene),
				"stones": p.Stones,
				"usage_7d": stat.Total, "success_7d": stat.Success, "failed_rate_7d": stat.FailedRate,
				"stones_revenue_7d": stat.Stones,
				"cost_yuan_est": cost,
				"margin_rate": aiMarginRate(stat.Stones, cost),
				"updated_at": p.UpdatedAt,
			}
			if v, ok := latestVersions[p.Scene]; ok {
				item["latest_version"] = gin.H{
					"old_stones": v.OldStones, "new_stones": v.NewStones,
					"operator": v.Operator, "note": v.Note, "created_at": v.CreatedAt,
				}
			}
			list = append(list, item)
		}
		c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "success", "data": list})
	})

	// 调价历史
	ai.GET("/pricing/versions", func(c *gin.Context) {
		if pricingVersionModel == nil {
			c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "success", "data": []gin.H{}})
			return
		}
		scene := strings.TrimSpace(c.Query("scene"))
		if scene == "" {
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "缺少 scene 参数"})
			return
		}
		limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))
		versions, err := pricingVersionModel.ListByScene(scene, limit)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "获取调价历史失败: " + err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "success", "data": versions})
	})
}
