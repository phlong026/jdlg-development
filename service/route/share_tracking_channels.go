package route

import (
	"log"
	"strings"

	"github.com/gin-gonic/gin"
	"service/model"
	"service/tracking"
)

const trackingLeadLookupBatchSize = 200

func trackingLeadUsersBatched(ids []int64, lookup func([]int64) (map[int64]bool, error)) (map[int64]bool, error) {
	unique := make([]int64, 0, len(ids))
	seen := make(map[int64]bool, len(ids))
	for _, id := range ids {
		if id <= 0 || seen[id] {
			continue
		}
		seen[id] = true
		unique = append(unique, id)
	}
	converted := make(map[int64]bool, len(unique))
	for start := 0; start < len(unique); start += trackingLeadLookupBatchSize {
		end := start + trackingLeadLookupBatchSize
		if end > len(unique) {
			end = len(unique)
		}
		batch, err := lookup(unique[start:end])
		if err != nil {
			return nil, err
		}
		for id, exists := range batch {
			if exists {
				converted[id] = true
			}
		}
	}
	return converted, nil
}

// The channel view ranks forwarding visitors as distribution channels. Lead
// outcomes are joined here (not inside tracking) because customer_leads is
// sales-pipeline data: tracking supplies behaviour, the route supplies the
// CRM-side consequence, and downstream rosters never reach the response.
func registerTrackingChannelRoutes(group *gin.RouterGroup, svc *tracking.Service, leads *model.CustomerServiceModel) {
	group.GET("/channels", func(c *gin.Context) {
		if !c.MustGet("tracking.scope").(tracking.Scope).View {
			trackingResponse(c, nil, &tracking.Error{Status: 403, Message: "无客户轨迹查看权限"})
			return
		}
		f, e := trackingFilter(c)
		if e != nil {
			trackingResponse(c, nil, e)
			return
		}
		list, e := svc.Channels(c.Request.Context(), c.MustGet("tracking.scope").(tracking.Scope), f)
		if e != nil {
			trackingResponse(c, nil, e)
			return
		}
		summaries, _ := list.Items.([]tracking.ChannelSummary)
		ids := make([]int64, 0, 64)
		for _, item := range summaries {
			for _, visitor := range item.VisitorKeys {
				if id, ok := trackingVisitorAccount(visitor); ok {
					ids = append(ids, id)
				}
			}
		}
		converted := map[int64]bool{}
		leadHistoryAvailable := true
		if len(ids) > 0 {
			if leads == nil || leads.DB == nil {
				leadHistoryAvailable = false
			} else if converted, e = trackingLeadUsersBatched(ids, leads.HasLeadUsers); e != nil {
				log.Printf("tracking channels: historical lead lookup failed: %v", e)
				leadHistoryAvailable = false
				converted = map[int64]bool{}
			}
		}
		out := make([]gin.H, 0, len(summaries))
		for _, item := range summaries {
			leadCount := 0
			countedLeadAccounts := map[int64]bool{}
			for _, visitor := range item.VisitorKeys {
				if id, ok := trackingVisitorAccount(visitor); ok && converted[id] && !countedLeadAccounts[id] {
					countedLeadAccounts[id] = true
					leadCount++
				}
			}
			var convertedLeads any = leadCount
			if !leadHistoryAvailable {
				convertedLeads = nil
			}
			nickname := strings.TrimSpace(item.Nickname)
			if nickname == "" || nickname == "已识别账号" || strings.HasPrefix(nickname, "匿名访客") {
				nickname = ""
			}
			out = append(out, gin.H{
				"visitor_key":            item.Sharer,
				"nickname":               nickname,
				"identity_type":          item.IdentityType,
				"share_count":            item.Shares,
				"brought_visitors":       item.Visitors,
				"brought_identified":     item.Identified,
				"brought_high":           item.High,
				"converted_leads":        convertedLeads,
				"lead_history_available": leadHistoryAvailable,
			})
		}
		list.Items = out
		trackingResponse(c, list, nil)
	})
}
