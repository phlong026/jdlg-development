package route

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"
	"service/report"
)

func registerDecisionQuestions(public *gin.RouterGroup, module *ReportModule) {
	public.GET("/reports/v5/questions", func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		if module == nil || !module.DecisionEnabled {
			reportError(c, report.ErrDisabled)
			return
		}
		goal := c.Query("primary_goal")
		questions, ok := decisionQuestions(goal)
		if !ok {
			reportError(c, report.ErrInvalidInput)
			return
		}
		c.JSON(http.StatusOK, gin.H{"code": 0, "data": gin.H{"version": report.ProtocolDecisionV5, "primary_goal": goal, "preview_pages": 4, "full_pages": 10, "questions": questions}})
	})
}

func registerDecisionReportRoutes(group *gin.RouterGroup, module *ReportModule) {
	group.POST("/v5", func(c *gin.Context) {
		var input report.DecisionInput
		if !decodeReportBody(c, &input, 16<<10) {
			return
		}
		result, err := module.Service.CreateDecisionPreview(c.Request.Context(), GetTokenCodeSession(c).UserID, input)
		if err != nil {
			reportError(c, err)
			return
		}
		c.JSON(http.StatusAccepted, gin.H{"code": 0, "data": result})
	})
	group.POST("/:id/confirmation", func(c *gin.Context) {
		var input report.DecisionConfirmation
		if !decodeReportBody(c, &input, 4<<10) {
			return
		}
		result, err := module.Service.ConfirmDecision(c.Request.Context(), GetTokenCodeSession(c).UserID, c.Param("id"), input)
		if err != nil {
			reportError(c, err)
			return
		}
		c.JSON(http.StatusOK, gin.H{"code": 0, "data": result})
	})
}

func decodeReportBody(c *gin.Context, target any, limit int64) bool {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, limit)
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	var tail any
	if decoder.Decode(target) != nil || decoder.Decode(&tail) != io.EOF {
		reportError(c, report.ErrInvalidInput)
		return false
	}
	return true
}

type decisionQuestion struct {
	Field    string           `json:"field"`
	Label    string           `json:"label"`
	Type     string           `json:"type"`
	Required bool             `json:"required"`
	Options  []questionOption `json:"options,omitempty"`
	Help     string           `json:"help,omitempty"`
}
type questionOption struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

// This is the question contract for the four entry goals. Numeric unknowns are
// omitted, never submitted as zero. Nested field formats are documented in report/README.md.
func decisionQuestions(goal string) ([]decisionQuestion, bool) {
	if goal != report.GoalSpace && goal != report.GoalBudget && goal != report.GoalFamily && goal != report.GoalReadiness {
		return nil, false
	}
	questions := []decisionQuestion{
		{Field: "region", Label: "准备在哪里建房？", Type: "text"},
		{Field: "approximate_dimensions", Label: "宅基地大约多宽、多深？", Type: "dimensions", Help: "只填大约尺寸；不确定可以留空，这不代表审批可建范围。"},
		{Field: "buildable_scope", Label: "是否已核实可建范围？", Type: "select", Required: true, Options: []questionOption{{"known", "已核实"}, {"unknown", "还不确定"}}},
		{Field: "buildable_constraints", Label: "已核实的边界或限制", Type: "text", Help: "选择已核实时必填；填写确认过的条件。"},
		{Field: "permanent_count", Label: "平时常住几人？", Type: "integer", Required: goal == report.GoalSpace},
		{Field: "returning_count", Label: "集中返乡时另有几人入住？", Type: "integer", Required: goal == report.GoalSpace},
		{Field: "return_frequency", Label: "家人多久返乡一次？", Type: "select", Required: true, Options: []questionOption{{"none", "没有额外返乡居住"}, {"monthly", "每月"}, {"quarterly", "每季度"}, {"holidays", "节假日"}, {"yearly", "每年"}, {"unknown", "还不确定"}}},
		{Field: "floors", Label: "希望建几层？", Type: "integer"},
		{Field: "bedrooms", Label: "希望有几间卧室？", Type: "integer"},
		{Field: "flexible_use", Label: "是否接受客房平时兼作其他用途？", Type: "select", Required: true, Options: []questionOption{{"yes", "可以接受"}, {"no", "希望独立使用"}, {"unknown", "需要先比较"}}},
		{Field: "priorities", Label: "最想优先解决什么？", Type: "text_list", Required: true, Help: "按重要程度填写一至三项；报告会保留这个顺序。"},
		{Field: "budget_wan", Label: "目前准备投入多少万元？", Type: "number", Help: "尚未确定可留空；报告不会编造报价。"},
		{Field: "budget_scope", Label: "这笔预算包括哪些项目？", Type: "scope_items", Required: goal == report.GoalBudget, Help: "最多八项，逐项标记包括、不包括或尚不清楚；用于各类报告的询价清单。"},
	}
	switch goal {
	case report.GoalFamily:
		questions = append(questions, decisionQuestion{Field: "family_perspectives", Label: "家人分别关心什么？", Type: "family_perspectives", Required: true, Help: "只记录已经表达的意见，不代家人猜测。"})
	case report.GoalReadiness:
		questions = append(questions, decisionQuestion{Field: "preparation_stage", Label: "目前推进到哪一步？", Type: "select", Required: true, Options: []questionOption{{"idea", "刚有想法"}, {"information", "收集资料"}, {"design", "设计沟通"}, {"approval", "审批确认"}, {"bidding", "询价选团队"}, {"preconstruction", "开工准备"}, {"unknown", "还不确定"}}}, decisionQuestion{Field: "preparation_items", Label: "哪些准备已完成？", Type: "preparation_items", Required: true, Help: "逐项记录已有、缺失或尚不清楚的材料。"})
	}
	return questions, true
}
