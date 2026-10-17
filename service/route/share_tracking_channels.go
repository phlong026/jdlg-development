package route

import (
	"strings"

	"github.com/gin-gonic/gin"
	"service/model"
	"service/tracking"
)

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
		if len(ids) > 0 && leads != nil && leads.DB != nil {
			if converted, e = leads.HasLeadUsers(ids); e != nil {
				// Behavioural ranking stays valuable without lead joins; log and
				// degrade rather than failing the whole channel view.
				converted = map[int64]bool{}
			}
		}
		out := make([]gin.H, 0, len(summaries))
		for _, item := range summaries {
			leadCount := 0
			for _, visitor := range item.VisitorKeys {
				if id, ok := trackingVisitorAccount(visitor); ok && converted[id] {
					leadCount++
				}
			}
			nickname := strings.TrimSpace(item.Nickname)
			if nickname == "" || nickname == "已识别账号" || strings.HasPrefix(nickname, "匿名访客") {
				nickname = ""
			}
			out = append(out, gin.H{
				"visitor_key":        item.Sharer,
				"nickname":           nickname,
				"identity_type":      item.IdentityType,
				"share_count":        item.Shares,
				"brought_visitors":   item.Visitors,
				"brought_identified": item.Identified,
				"brought_high":       item.High,
				"converted_leads":    leadCount,
			})
		}
		list.Items = out
		trackingResponse(c, list, nil)
	})
}
