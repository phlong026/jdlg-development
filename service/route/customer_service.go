package route

import (
	"database/sql"
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

func RegisterCustomerServiceRoutes(r *gin.RouterGroup, customerServiceModel *model.CustomerServiceModel, trackingServices ...*tracking.Service) {
	var store customerLeadStore
	if customerServiceModel != nil && customerServiceModel.DB != nil {
		store = customerServiceModel
	}
	var confirmed consultationConfirmed
	if len(trackingServices) > 0 && trackingServices[0] != nil {
		confirmed = trackingServices[0].ConfirmConsultation
	}
	registerCustomerLeadRoutes(r, store, confirmed)
}

func registerCustomerLeadRoutes(r *gin.RouterGroup, customerServiceModel customerLeadStore, confirmed consultationConfirmed) {
	group := r.Group("/customer-service")

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
			id, err := customerServiceModel.CreateLead(&model.CustomerLeadCreateInput{
				UserID:                  codeSession.UserID,
				Name:                    req.Name,
				Phone:                   req.Phone,
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
			})
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "创建客服线索失败: " + err.Error()})
				return
			}
			_, eventErr := customerServiceModel.CreateEvent(&model.CustomerServiceEventCreateInput{
				UserID:        codeSession.UserID,
				LeadID:        id,
				SessionNo:     req.SessionNo,
				EventType:     "lead_submit",
				Source:        source,
				SourceTaskNo:  req.SourceTaskNo,
				IntentLevel:   req.IntentLevel,
				DemandSummary: req.DemandSummary,
				Payload:       req.Payload,
			})
			if isConsultation && eventErr == nil && id > 0 {
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
		metrics, err := customerServiceModel.FunnelMetrics()
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
			LeadType:    c.DefaultQuery("lead_type", "all"),
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
