package route

import (
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"service/model"
	"service/tracking"
)

// Management-side visitor→lead conversion: the only automated bridge from
// tracking intent to the customer-lead pipeline. Every conversion carries its
// evidence snapshot so a salesperson can audit why this visitor was surfaced.

const (
	trackingLeadSource       = "share_tracking"
	trackingLeadEventType    = "share_intent_convert"
	trackingCustomerIdentity = "乡墅业主"
	trackingLeadEvidenceCap  = 20
	trackingLeadSummaryTopN  = 3
)

// B-side accounts distribute and consume tooling; they are channels, never
// house-building prospects. Fail closed on unknown identity values. The
// canonical list lives in the tracking package (tracking.BSideIdentities).

func trackingVisitorAccount(key string) (int64, bool) {
	if !strings.HasPrefix(key, "u:") {
		return 0, false
	}
	id, err := strconv.ParseInt(strings.TrimPrefix(key, "u:"), 10, 64)
	// Canonical form only, mirroring tracking.visitorAccount: "u:01" must not
	// resolve to a different spelling of the same account.
	if err != nil || id <= 0 || key != "u:"+strconv.FormatInt(id, 10) {
		return 0, false
	}
	return id, true
}

// trackingLeadGate keeps the intent→lead route reserved for identified
// 乡墅业主 accounts. Anonymous visitors have no contactable UserID. Visitors
// without a declared identity pass only when their customer-domain reading
// share clears the gray-zone threshold (behaviour over self-declaration).
const trackingLeadGrayShare = 0.7

func trackingLeadGate(state, identityType string, customerShare float64) *tracking.Error {
	if state != "identified" {
		return &tracking.Error{Status: 422, Message: "匿名或已删除访客无法转化为线索，请先引导客户留资"}
	}
	identityType = strings.TrimSpace(identityType)
	if identityType == "" {
		if customerShare >= trackingLeadGrayShare {
			return nil
		}
		return &tracking.Error{Status: 422, Message: "该访客尚未确认身份，且客户域浏览占比不足70%，请人工核实后再转化"}
	}
	if tracking.BSideIdentities[identityType] {
		return &tracking.Error{Status: 422, Message: "该访客为B端用户（" + identityType + "），不进入客户线索流程"}
	}
	if identityType != trackingCustomerIdentity {
		return &tracking.Error{Status: 422, Message: "无法识别的访客身份：" + identityType}
	}
	return nil
}

// Placeholders leak no information and invite the salesperson to fill the
// real name during follow-up; an empty name beats a fabricated one.
func trackingLeadDisplayName(nickname string) string {
	nickname = strings.TrimSpace(nickname)
	if nickname == "" || nickname == "已识别账号" || strings.HasPrefix(nickname, "匿名访客") {
		return ""
	}
	return nickname
}

func trackingVisibleMinutes(visibleMS int64) int64 {
	if visibleMS <= 0 {
		return 0
	}
	minutes := visibleMS / 60000
	if minutes < 1 {
		return 1
	}
	return minutes
}

// buildTrackingLeadSummary renders the human-readable demand digest stored on
// the lead row. It must stay factual: metrics and content titles only.
func buildTrackingLeadSummary(v tracking.Visitor, contents []tracking.VisitorContent) string {
	var b strings.Builder
	b.WriteString("近30天到访 ")
	b.WriteString(strconv.Itoa(v.Sessions))
	b.WriteString(" 次，累计浏览 ")
	b.WriteString(strconv.Itoa(v.Views))
	b.WriteString(" 个页面、可见时长约 ")
	b.WriteString(strconv.FormatInt(trackingVisibleMinutes(v.Visible), 10))
	b.WriteString(" 分钟")
	if len(contents) > 0 {
		b.WriteString("，重点查看《")
		b.WriteString(contents[0].Title)
		b.WriteString("》")
		if len(contents) > 1 {
			b.WriteString("等 ")
			b.WriteString(strconv.Itoa(len(contents)))
			b.WriteString(" 份素材")
		}
	} else {
		b.WriteString("，暂无素材浏览记录")
	}
	b.WriteString("；分享追踪判定意向：")
	b.WriteString(strings.TrimSpace(v.Intent.Level))
	if len(v.Intent.Reasons) > 0 && strings.TrimSpace(v.Intent.Reasons[0]) != "" {
		b.WriteString("（")
		b.WriteString(strings.TrimSpace(v.Intent.Reasons[0]))
		b.WriteString("）")
	}
	return b.String()
}

// buildTrackingLeadEvidence snapshots the conversion decision. The evidence
// travels with the lead event so deleting the visitor later keeps the lead's
// contact fields while removing this derived behavioural copy.
func buildTrackingLeadEvidence(v tracking.Visitor, contents []tracking.VisitorContent, actor int64, now time.Time) map[string]interface{} {
	evidence := make([]map[string]interface{}, 0, len(v.Intent.Evidence))
	for i, e := range v.Intent.Evidence {
		if i >= trackingLeadEvidenceCap {
			break
		}
		evidence = append(evidence, map[string]interface{}{
			"kind": e.Kind, "record_id": e.ID, "session_id": e.Session,
			"action": e.Action, "occurred_at": e.At.Format(time.RFC3339),
			"content_id": e.Content,
		})
	}
	top := make([]map[string]interface{}, 0, len(contents))
	for _, c := range contents {
		top = append(top, map[string]interface{}{
			"title": c.Title, "type": c.Type,
			"page_views": c.PageViews, "visible_ms": c.VisibleMS,
		})
	}
	return map[string]interface{}{
		"visitor_key":     v.Key,
		"intent_level":    strings.TrimSpace(v.Intent.Level),
		"intent_reasons":  v.Intent.Reasons,
		"intent_evidence": evidence,
		"metrics": map[string]interface{}{
			"sessions": v.Sessions, "page_views": v.Views, "visible_ms": v.Visible,
		},
		"top_contents": top,
		"converted_by": actor,
		"converted_at": now.Format(time.RFC3339),
	}
}

func registerTrackingLeadRoutes(group *gin.RouterGroup, svc *tracking.Service, leads *model.CustomerServiceModel, users *model.UserModel) {
	identities := model.NewUserIdentityModel(users.DB)
	group.POST("/visitors/:id/lead", func(c *gin.Context) {
		scope := c.MustGet("tracking.scope").(tracking.Scope)
		if !scope.CRM {
			trackingResponse(c, nil, &tracking.Error{Status: 403, Message: "无客户跟进权限"})
			return
		}
		// Same handover rule as the lead workbench: once the CRM bridge owns
		// customers, conversions must happen there, not in the read-only lead view.
		if crmEnabled() {
			c.JSON(409, gin.H{"code": 409, "msg": "客户已统一纳管，请在客户档案中跟进、交接或申请成交"})
			return
		}
		if leads == nil || leads.DB == nil {
			trackingResponse(c, nil, &tracking.Error{Status: 503, Message: "客服线索服务不可用"})
			return
		}
		key := c.Param("id")
		userID, ok := trackingVisitorAccount(key)
		if !ok {
			trackingResponse(c, nil, &tracking.Error{Status: 422, Message: "匿名访客无法转化为线索，请先引导客户留资"})
			return
		}
		list, e := svc.Visitors(c.Request.Context(), scope, tracking.Filter{Visitor: key})
		if e != nil {
			trackingResponse(c, nil, e)
			return
		}
		items, _ := list.Items.([]tracking.Visitor)
		if len(items) == 0 {
			trackingResponse(c, nil, &tracking.Error{Status: 404, Message: "访客不存在或无权访问"})
			return
		}
		visitor := items[0]
		if e := trackingLeadGate(visitor.State, visitor.IdentityType, visitor.Intent.CustomerShare); e != nil {
			trackingResponse(c, nil, e)
			return
		}
		phone := ""
		if phones, err := identities.GetUserPhones(userID); err != nil {
			log.Printf("[Tracking] lead conversion phone lookup failed user=%d: %v", userID, err)
		} else if len(phones) > 0 {
			phone = phones[0]
		}
		// Content taste is enriching context, never a blocker: on failure the
		// snapshot simply carries no top contents and the summary says so.
		contents := []tracking.VisitorContent{}
		if cl, err := svc.VisitorContents(c.Request.Context(), scope, key, tracking.Filter{Limit: trackingLeadSummaryTopN}); err != nil {
			log.Printf("[Tracking] lead conversion contents failed visitor=%s: %v", key, err)
		} else if cs, ok := cl.Items.([]tracking.VisitorContent); ok {
			contents = cs
		}
		existingID, err := leads.FindLatestOpenLeadID(userID)
		if err != nil {
			log.Printf("[Tracking] lead dedupe failed user=%d: %v", userID, err)
			trackingResponse(c, nil, &tracking.Error{Status: 500, Message: "线索查重失败，请稍后重试"})
			return
		}
		summary := buildTrackingLeadSummary(visitor, contents)
		intentLevel := strings.TrimSpace(visitor.Intent.Level)
		event := &model.CustomerServiceEventCreateInput{
			UserID:        userID,
			EventType:     trackingLeadEventType,
			Source:        trackingLeadSource,
			IntentLevel:   intentLevel,
			DemandSummary: summary,
			Payload:       buildTrackingLeadEvidence(visitor, contents, scope.Actor, time.Now()),
		}
		if existingID > 0 {
			// Keep the open lead untouched; append the conversion evidence as a
			// follow-up signal instead of silently rewriting its fields.
			event.LeadID = existingID
			if _, err := leads.CreateEvent(event); err != nil {
				log.Printf("[Tracking] attach conversion event failed lead=%d: %v", existingID, err)
				trackingResponse(c, nil, &tracking.Error{Status: 500, Message: "追加转化记录失败，请稍后重试"})
				return
			}
			trackingResponse(c, gin.H{"lead_id": existingID, "existed": true, "intent_level": intentLevel, "demand_summary": summary}, nil)
			return
		}
		leadID, err := leads.CreateLead(&model.CustomerLeadCreateInput{
			UserID:        userID,
			Name:          trackingLeadDisplayName(visitor.Nickname),
			Phone:         phone,
			Source:        trackingLeadSource,
			IntentLevel:   intentLevel,
			Status:        model.CustomerLeadStatusNew,
			DemandSummary: summary,
		})
		if err != nil {
			log.Printf("[Tracking] create lead from visitor=%s failed: %v", key, err)
			trackingResponse(c, nil, &tracking.Error{Status: 500, Message: "创建线索失败，请稍后重试"})
			return
		}
		event.LeadID = leadID
		if _, err := leads.CreateEvent(event); err != nil {
			// The lead exists; a missing snapshot degrades auditability, not the
			// conversion itself. Surface it in logs and still report success.
			log.Printf("[Tracking] conversion event for lead=%d failed: %v", leadID, err)
		}
		trackingResponse(c, gin.H{"lead_id": leadID, "existed": false, "intent_level": intentLevel, "demand_summary": summary}, nil)
	})
}
