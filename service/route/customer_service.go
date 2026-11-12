package route

import (
	"context"
	"database/sql"
	"errors"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"service/model"
	"service/tracking"
)

func parseCustomerLeadPageParams(c *gin.Context) (int, int, int) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}
	if pageSize > 100 {
		pageSize = 100
	}
	return page, pageSize, (page - 1) * pageSize
}

func parseCustomerLeadNextFollowupAt(value string) (*time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, nil
	}
	for _, layout := range []string{time.RFC3339, "2006-01-02 15:04:05", "2006-01-02T15:04", "2006-01-02"} {
		parsed, err := time.ParseInLocation(layout, value, time.Local)
		if err == nil {
			return &parsed, nil
		}
	}
	return nil, strconv.ErrSyntax
}

type leadPhoneLookup func(userID int64) []string
type publishedContentValidator func(ctx context.Context, id string, version int) error
type businessContactConsentStore interface {
	BusinessContactConsent(userID int64) (bool, error)
	SetBusinessContactConsent(userID int64, accepted bool, source string) error
}
type businessContactLeadStore interface {
	CreateLeadWithBusinessContact(input *model.CustomerLeadCreateInput) (leadID int64, usedBoundPhone bool, err error)
}

var errInvalidConsultationContent = errors.New("咨询内容已不可访问")

func sanitizeContentConsultation(ctx context.Context, source string, payload map[string]interface{}, validate publishedContentValidator) error {
	if len(payload) == 0 {
		return nil
	}
	plan, ok := payload["consultation_plan"].(map[string]interface{})
	if !ok {
		return nil
	}
	ref, ok := plan["contentRef"].(map[string]interface{})
	if !ok {
		return nil
	}
	contentID, _ := ref["contentId"].(string)
	versionValue, _ := ref["version"].(float64)
	version := int(versionValue)
	if strings.TrimSpace(source) != "content_reader" || !validConsultationContentID(contentID) || versionValue != float64(version) || version < 1 || validate == nil {
		return errInvalidConsultationContent
	}
	if err := validate(ctx, contentID, version); err != nil {
		return errInvalidConsultationContent
	}
	// Content consultation stores a stable public snapshot reference only. Client
	// titles, body text, share credentials and visitor identity are not trusted.
	payload["consultation_plan"] = map[string]interface{}{
		"visible": true,
		"summary": "正在咨询内容版本 v" + strconv.Itoa(version),
		"contentRef": map[string]interface{}{
			"contentId": contentID,
			"version":   version,
		},
	}
	return nil
}

func validConsultationContentID(value string) bool {
	if len(value) < 8 || len(value) > 128 {
		return false
	}
	for _, char := range value {
		if !(char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' || char >= '0' && char <= '9' || char == '-' || char == '_') {
			return false
		}
	}
	return true
}

func RegisterCustomerServiceRoutes(r *gin.RouterGroup, customerServiceModel *model.CustomerServiceModel, trackingServices ...*tracking.Service) {
	var store customerLeadStore
	if customerServiceModel != nil && customerServiceModel.DB != nil {
		store = customerServiceModel
	}
	var confirmed consultationConfirmed
	var validateContent publishedContentValidator
	if len(trackingServices) > 0 && trackingServices[0] != nil {
		confirmed = trackingServices[0].ConfirmConsultation
		validateContent = func(ctx context.Context, id string, version int) error {
			_, err := trackingServices[0].ReadContent(ctx, id, version)
			return err
		}
	}
	registerCustomerLeadRoutes(r, store, confirmed, nil, validateContent)
}

func registerCustomerLeadRoutes(r *gin.RouterGroup, customerServiceModel customerLeadStore, confirmed consultationConfirmed, phoneLookup leadPhoneLookup, contentValidators ...publishedContentValidator) {
	contactConsent, _ := customerServiceModel.(businessContactConsentStore)
	contactLeadStore, _ := customerServiceModel.(businessContactLeadStore)
	_ = phoneLookup // kept only for source-compatible route fixtures; never used for account-phone access.
	var validateContent publishedContentValidator
	if len(contentValidators) > 0 {
		validateContent = contentValidators[0]
	}
	group := r.Group("/customer-service")

	group.GET("/contact-consent", func(c *gin.Context) {
		codeSession := GetTokenCodeSession(c)
		if codeSession == nil {
			c.JSON(http.StatusUnauthorized, gin.H{"code": 401, "msg": "未通过token验证"})
			return
		}
		if contactConsent == nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"code": 503, "msg": "业务联系授权服务不可用"})
			return
		}
		accepted, err := contactConsent.BusinessContactConsent(codeSession.UserID)
		if err != nil {
			log.Printf("[CustomerService] read business contact consent failed user=%d: %v", codeSession.UserID, err)
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "读取业务联系授权失败"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "success", "data": gin.H{"accepted": accepted, "purpose": model.BusinessContactConsentPurpose, "version": model.BusinessContactConsentVersion}})
	})

	group.PUT("/contact-consent", func(c *gin.Context) {
		codeSession := GetTokenCodeSession(c)
		if codeSession == nil {
			c.JSON(http.StatusUnauthorized, gin.H{"code": 401, "msg": "未通过token验证"})
			return
		}
		if contactConsent == nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"code": 503, "msg": "业务联系授权服务不可用"})
			return
		}
		var req struct {
			Accepted *bool  `json:"accepted" binding:"required"`
			Purpose  string `json:"purpose"`
			Version  string `json:"version"`
		}
		if err := c.ShouldBindJSON(&req); err != nil || req.Accepted == nil {
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "请明确选择是否允许使用账号手机号进行业务联系"})
			return
		}
		if strings.TrimSpace(req.Purpose) != model.BusinessContactConsentPurpose || strings.TrimSpace(req.Version) != model.BusinessContactConsentVersion {
			c.JSON(http.StatusConflict, gin.H{"code": 409, "msg": "联系授权说明已更新，请刷新页面后重新选择"})
			return
		}
		if err := contactConsent.SetBusinessContactConsent(codeSession.UserID, *req.Accepted, "customer_service_settings"); err != nil {
			log.Printf("[CustomerService] write business contact consent failed user=%d: %v", codeSession.UserID, err)
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "保存业务联系授权失败"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "success", "data": gin.H{"accepted": *req.Accepted, "version": model.BusinessContactConsentVersion}})
	})

	group.POST("/events", func(c *gin.Context) {
		if customerServiceModel == nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "智能客服服务不可用"})
			return
		}
		codeSession := GetTokenCodeSession(c)
		if codeSession == nil {
			c.JSON(http.StatusUnauthorized, gin.H{"code": 401, "msg": "未通过token验证"})
			return
		}
		var req struct {
			LeadID        int64                  `json:"lead_id"`
			SessionNo     string                 `json:"session_no"`
			EventType     string                 `json:"event_type" binding:"required"`
			Source        string                 `json:"source"`
			SourceTaskNo  string                 `json:"source_task_no"`
			IntentLevel   string                 `json:"intent_level"`
			DemandSummary string                 `json:"demand_summary"`
			Payload       map[string]interface{} `json:"payload"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": FormatValidationError("参数错误: " + err.Error())})
			return
		}
		// 这些事件只由对应的服务端接口写入。客户端自报不仅会污染线索排序，
		// 还可以绕过用户必须亲自选择的业务联系授权。
		eventType := strings.TrimSpace(req.EventType)
		if eventType == "lead_submit" ||
			eventType == "consultation_request_submitted" ||
			eventType == model.BusinessContactConsentGranted ||
			eventType == model.BusinessContactConsentWithdrawn {
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "该事件类型只能由服务端记录"})
			return
		}
		id, err := customerServiceModel.CreateEvent(&model.CustomerServiceEventCreateInput{
			UserID:        codeSession.UserID,
			LeadID:        req.LeadID,
			SessionNo:     req.SessionNo,
			EventType:     req.EventType,
			Source:        req.Source,
			SourceTaskNo:  req.SourceTaskNo,
			IntentLevel:   req.IntentLevel,
			DemandSummary: req.DemandSummary,
			Payload:       req.Payload,
		})
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "记录智能客服事件失败: " + err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "success", "data": gin.H{"id": id}})
	})

	createLead := func(isConsultation bool) gin.HandlerFunc {
		return func(c *gin.Context) {
			if customerServiceModel == nil {
				c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "智能客服服务不可用"})
				return
			}
			codeSession := GetTokenCodeSession(c)
			if codeSession == nil {
				c.JSON(http.StatusUnauthorized, gin.H{"code": 401, "msg": "未通过token验证"})
				return
			}
			var req struct {
				Name                    string                 `json:"name"`
				Phone                   string                 `json:"phone"`
				Wechat                  string                 `json:"wechat"`
				EnterpriseWechatContact string                 `json:"enterprise_wechat_contact"`
				DemandSummary           string                 `json:"demand_summary" binding:"required"`
				HouseFloors             string                 `json:"house_floors"`
				HouseStyle              string                 `json:"house_style"`
				LandWidth               string                 `json:"land_width"`
				LandDepth               string                 `json:"land_depth"`
				RoomRequirement         string                 `json:"room_requirement"`
				Source                  string                 `json:"source"`
				SourceTaskNo            string                 `json:"source_task_no"`
				IntentLevel             string                 `json:"intent_level"`
				Status                  string                 `json:"status"`
				Remark                  string                 `json:"remark"`
				SessionNo               string                 `json:"session_no"`
				Payload                 map[string]interface{} `json:"payload"`
			}
			if err := c.ShouldBindJSON(&req); err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": FormatValidationError("参数错误: " + err.Error())})
				return
			}
			source := strings.TrimSpace(req.Source)
			if source == "" {
				source = "customer_service"
			}
			if err := sanitizeContentConsultation(c.Request.Context(), source, req.Payload, validateContent); err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": err.Error()})
				return
			}
			leadInput := &model.CustomerLeadCreateInput{
				UserID:                  codeSession.UserID,
				Name:                    req.Name,
				Phone:                   req.Phone,
				PhoneSource:             model.CustomerLeadPhoneSourceUserProvided,
				Wechat:                  req.Wechat,
				EnterpriseWechatContact: req.EnterpriseWechatContact,
				DemandSummary:           req.DemandSummary,
				HouseFloors:             req.HouseFloors,
				HouseStyle:              req.HouseStyle,
				LandWidth:               req.LandWidth,
				LandDepth:               req.LandDepth,
				RoomRequirement:         req.RoomRequirement,
				Source:                  source,
				SessionNo:               req.SessionNo,
				SourceTaskNo:            req.SourceTaskNo,
				IntentLevel:             req.IntentLevel,
				Status:                  req.Status,
				Remark:                  req.Remark,
			}
			var id int64
			var err error
			if strings.TrimSpace(req.Phone) == "" && contactLeadStore != nil {
				id, _, err = contactLeadStore.CreateLeadWithBusinessContact(leadInput)
			} else {
				id, err = customerServiceModel.CreateLead(leadInput)
			}
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "创建客服线索失败: " + err.Error()})
				return
			}
			submit := model.CustomerServiceEventCreateInput{
				UserID:        codeSession.UserID,
				LeadID:        id,
				SessionNo:     req.SessionNo,
				EventType:     "lead_submit",
				Source:        source,
				SourceTaskNo:  req.SourceTaskNo,
				IntentLevel:   req.IntentLevel,
				DemandSummary: req.DemandSummary,
				Payload:       req.Payload,
			}
			_, eventErr := customerServiceModel.CreateEvent(&submit)
			if eventErr != nil {
				// lead_submit 是"提交线索"统计和列表排序的依据，不能静默丢失。
				// 超长 payload 最常见，去掉 payload 与需求摘要（线索表里已有）再写一次。
				log.Printf("[CustomerService] lead_submit event failed, retrying minimal user=%d lead=%d session=%s: %v", codeSession.UserID, id, req.SessionNo, eventErr)
				minimal := submit
				minimal.DemandSummary = ""
				minimal.Payload = nil
				_, eventErr = customerServiceModel.CreateEvent(&minimal)
			}
			if eventErr != nil {
				// 线索已写入；客户端重试时同会话会更新同一条线索，不会重复建档。
				log.Printf("[CustomerService] lead_submit event lost user=%d lead=%d session=%s: %v", codeSession.UserID, id, req.SessionNo, eventErr)
				c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "留资记录失败，请重试"})
				return
			}
			if isConsultation && id > 0 {
				// Preserve the existing lead_submit funnel and add a separate,
				// minimal immutable proof of this explicit business command.
				_, proofErr := customerServiceModel.CreateEvent(&model.CustomerServiceEventCreateInput{
					UserID: codeSession.UserID, LeadID: id, SessionNo: req.SessionNo,
					EventType: "consultation_request_submitted", Source: source,
				})
				if proofErr == nil && confirmed != nil {
					confirmConsultationAfterCommit(confirmed, c.GetHeader("X-Tracking-Token"), codeSession.UserID, id)
				}
			}
			c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "success", "data": gin.H{"id": id}})
		}
	}
	group.POST("/leads", createLead(false))
	// A dedicated business command distinguishes an explicit request for human
	// consultation from automatic requirement drafts and image-generation leads.
	group.POST("/consultation-requests", createLead(true))
}

func RegisterPublicCustomerServiceRoutes(r *gin.RouterGroup, customerServiceModel *model.CustomerServiceModel) {
	group := r.Group("/customer-service")

	group.POST("/events/anonymous", func(c *gin.Context) {
		if customerServiceModel == nil || customerServiceModel.DB == nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "智能客服服务不可用"})
			return
		}
		var req struct {
			SessionNo     string                 `json:"session_no"`
			EventType     string                 `json:"event_type" binding:"required"`
			Source        string                 `json:"source"`
			SourceTaskNo  string                 `json:"source_task_no"`
			IntentLevel   string                 `json:"intent_level"`
			DemandSummary string                 `json:"demand_summary"`
			Payload       map[string]interface{} `json:"payload"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": FormatValidationError("参数错误: " + err.Error())})
			return
		}
		if strings.TrimSpace(req.EventType) != "welcome_shown" {
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "匿名事件仅支持 welcome_shown"})
			return
		}
		id, err := customerServiceModel.CreateEvent(&model.CustomerServiceEventCreateInput{
			UserID:        0,
			SessionNo:     req.SessionNo,
			EventType:     req.EventType,
			Source:        req.Source,
			SourceTaskNo:  req.SourceTaskNo,
			IntentLevel:   req.IntentLevel,
			DemandSummary: req.DemandSummary,
			Payload:       req.Payload,
		})
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "记录智能客服事件失败: " + err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "success", "data": gin.H{"id": id}})
	})
}

func RegisterCustomerLeadManagementRoutes(r *gin.RouterGroup, customerServiceModel *model.CustomerServiceModel) {
	leads := r.Group("/customer-leads")
	leads.Use(func(c *gin.Context) {
		if crmEnabled() && c.Request.Method != "GET" {
			crmError(c, 409, "客户已统一纳管，请在客户档案中跟进、交接或申请成交")
			return
		}
		c.Next()
	})

	leads.GET("/overview", func(c *gin.Context) {
		if customerServiceModel == nil || customerServiceModel.DB == nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "客服线索服务不可用"})
			return
		}
		overview, err := customerServiceModel.Overview()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "获取客服线索概览失败: " + err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "success", "data": overview})
	})

	leads.GET("/funnel", func(c *gin.Context) {
		if customerServiceModel == nil || customerServiceModel.DB == nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "客服线索服务不可用"})
			return
		}
		// Default window: trailing 30 days. days=0 means all time.
		days, _ := strconv.Atoi(c.DefaultQuery("days", "30"))
		var start time.Time
		if days > 0 {
			start = time.Now().AddDate(0, 0, -days)
		}
		metrics, err := customerServiceModel.FunnelMetrics(start, time.Time{})
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "获取客服漏斗数据失败: " + err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "success", "data": gin.H{"metrics": metrics}})
	})

	leads.GET("", func(c *gin.Context) {
		if customerServiceModel == nil || customerServiceModel.DB == nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "客服线索服务不可用"})
			return
		}
		page, pageSize, offset := parseCustomerLeadPageParams(c)
		// 非法或缺省的 user_id 视为不过滤，与其余筛选项（all）的容错方式一致
		userID, _ := strconv.ParseInt(strings.TrimSpace(c.Query("user_id")), 10, 64)
		list, total, err := customerServiceModel.ListLeads(model.CustomerLeadListParams{
			Keyword:     c.Query("keyword"),
			UserID:      userID,
			Status:      c.DefaultQuery("status", "all"),
			IntentLevel: c.DefaultQuery("intent_level", "all"),
			Source:      c.DefaultQuery("source", "all"),
			Queue:       c.DefaultQuery("queue", "all"),
			AssignedTo:  c.DefaultQuery("assigned_to", "all"),
			Dedupe:      c.DefaultQuery("dedupe", "all"),
			Limit:       pageSize,
			Offset:      offset,
		})
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "获取客服线索列表失败: " + err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "success", "data": gin.H{"list": list, "total": total, "page": page, "page_size": pageSize}})
	})

	leads.POST("/batch", func(c *gin.Context) {
		if customerServiceModel == nil || customerServiceModel.DB == nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "customer lead service unavailable"})
			return
		}
		var req struct {
			IDs            []int64 `json:"ids" binding:"required"`
			Status         string  `json:"status" binding:"required"`
			Remark         string  `json:"remark" binding:"required"`
			AssignedTo     string  `json:"assigned_to"`
			NextFollowupAt string  `json:"next_followup_at"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": FormatValidationError("invalid params: " + err.Error())})
			return
		}
		if len(req.IDs) == 0 {
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "please select leads first"})
			return
		}
		nextFollowupAt, err := parseCustomerLeadNextFollowupAt(req.NextFollowupAt)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "invalid next_followup_at"})
			return
		}
		input := model.CustomerLeadActionInput{
			Status:         req.Status,
			Remark:         req.Remark,
			AssignedTo:     req.AssignedTo,
			NextFollowupAt: nextFollowupAt,
		}
		updated, err := customerServiceModel.BatchApplyLeadActionWithFollowups(req.IDs, input, GetUserID(c), GetUsername(c))
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "customer lead action failed: " + err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "success", "data": gin.H{"updated": updated}})
	})
	leads.GET("/:id", func(c *gin.Context) {
		if customerServiceModel == nil || customerServiceModel.DB == nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "客服线索服务不可用"})
			return
		}
		id, err := strconv.ParseInt(c.Param("id"), 10, 64)
		if err != nil || id <= 0 {
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "无效的线索ID"})
			return
		}
		lead, err := customerServiceModel.GetLead(id)
		if err != nil {
			if err == sql.ErrNoRows {
				c.JSON(http.StatusNotFound, gin.H{"code": 404, "msg": "线索不存在"})
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "获取客服线索详情失败: " + err.Error()})
			return
		}
		events, err := customerServiceModel.ListEventsForLead(lead, 100)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "获取客服事件时间线失败: " + err.Error()})
			return
		}
		followups, err := customerServiceModel.ListFollowups(lead.ID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "获取客服跟进记录失败: " + err.Error()})
			return
		}
		summary := customerServiceModel.BuildLeadSummary(lead, events, followups)
		analysis := customerServiceModel.AnalyzeLeadIntentFromInput(&model.CustomerLeadCreateInput{
			UserID:                  lead.UserID,
			Name:                    lead.Name,
			Phone:                   lead.Phone,
			Wechat:                  lead.Wechat,
			EnterpriseWechatContact: lead.EnterpriseWechatContact,
			DemandSummary:           lead.DemandSummary,
			HouseFloors:             lead.HouseFloors,
			HouseStyle:              lead.HouseStyle,
			LandWidth:               lead.LandWidth,
			LandDepth:               lead.LandDepth,
			RoomRequirement:         lead.RoomRequirement,
			Source:                  lead.Source,
			SessionNo:               lead.SessionNo,
			SourceTaskNo:            lead.SourceTaskNo,
			IntentLevel:             lead.IntentLevel,
			Status:                  lead.Status,
			Remark:                  lead.Remark,
		})
		c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "success", "data": gin.H{"lead": lead, "events": events, "followups": followups, "summary": summary, "intent_analysis": analysis}})
	})

	leads.POST("/:id/followups", func(c *gin.Context) {
		if customerServiceModel == nil || customerServiceModel.DB == nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "客服线索服务不可用"})
			return
		}
		id, err := strconv.ParseInt(c.Param("id"), 10, 64)
		if err != nil || id <= 0 {
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "无效的线索ID"})
			return
		}
		var req struct {
			Status         string `json:"status" binding:"required"`
			Remark         string `json:"remark" binding:"required"`
			AssignedTo     string `json:"assigned_to"`
			NextFollowupAt string `json:"next_followup_at"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": FormatValidationError("参数错误: " + err.Error())})
			return
		}
		nextFollowupAt, err := parseCustomerLeadNextFollowupAt(req.NextFollowupAt)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "invalid next_followup_at"})
			return
		}
		followupID, err := customerServiceModel.ApplyLeadActionWithFollowup(id, model.CustomerLeadActionInput{
			Status:         req.Status,
			Remark:         req.Remark,
			AssignedTo:     req.AssignedTo,
			NextFollowupAt: nextFollowupAt,
		}, &model.CustomerLeadFollowupCreateInput{
			Status:         req.Status,
			Remark:         req.Remark,
			AssignedTo:     req.AssignedTo,
			NextFollowupAt: nextFollowupAt,
			OperatorID:     GetUserID(c),
			OperatorName:   GetUsername(c),
		})
		if err != nil {
			if err == sql.ErrNoRows {
				c.JSON(http.StatusNotFound, gin.H{"code": 404, "msg": "线索不存在"})
				return
			}
			if err == model.ErrInvalidCustomerLeadID {
				c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "无效的线索ID"})
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "更新客服线索失败: " + err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "success", "data": gin.H{"id": followupID}})
	})

	leads.POST("/:id/status", func(c *gin.Context) {
		if customerServiceModel == nil || customerServiceModel.DB == nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "客服线索服务不可用"})
			return
		}
		id, err := strconv.ParseInt(c.Param("id"), 10, 64)
		if err != nil || id <= 0 {
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "无效的线索ID"})
			return
		}
		var req struct {
			Status         string `json:"status" binding:"required"`
			Remark         string `json:"remark" binding:"required"`
			AssignedTo     string `json:"assigned_to"`
			NextFollowupAt string `json:"next_followup_at"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": FormatValidationError("参数错误: " + err.Error())})
			return
		}
		nextFollowupAt, err := parseCustomerLeadNextFollowupAt(req.NextFollowupAt)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "invalid next_followup_at"})
			return
		}
		if _, err := customerServiceModel.ApplyLeadActionWithFollowup(id, model.CustomerLeadActionInput{
			Status:         req.Status,
			Remark:         req.Remark,
			AssignedTo:     req.AssignedTo,
			NextFollowupAt: nextFollowupAt,
		}, &model.CustomerLeadFollowupCreateInput{
			Status:         req.Status,
			Remark:         req.Remark,
			AssignedTo:     req.AssignedTo,
			NextFollowupAt: nextFollowupAt,
			OperatorID:     GetUserID(c),
			OperatorName:   GetUsername(c),
		}); err != nil {
			if err == sql.ErrNoRows {
				c.JSON(http.StatusNotFound, gin.H{"code": 404, "msg": "线索不存在"})
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "customer lead action failed: " + err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "success"})
	})

}
