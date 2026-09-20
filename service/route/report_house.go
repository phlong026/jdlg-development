package route

import (
	"github.com/gin-gonic/gin"
	"net/http"
	"service/report"
)

func registerHouseQuestions(public *gin.RouterGroup, module *ReportModule) {
	public.GET("/reports/house/questions", func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		if module == nil || !module.HouseEnabled {
			reportError(c, report.ErrDisabled)
			return
		}
		goal := c.DefaultQuery("primary_goal", report.GoalSpace)
		questions, ok := decisionQuestions(goal)
		if !ok {
			reportError(c, report.ErrInvalidInput)
			return
		}
		questions = append(questions,
			decisionQuestion{Field: "house.style", Label: "希望采用什么建筑风格？", Type: "text", Required: true, Help: "尚未决定可填写待比较。"},
			decisionQuestion{Field: "house.first_floor_elder_room", Label: "是否需要首层老人房？", Type: "boolean"},
			decisionQuestion{Field: "house.road_side", Label: "道路和入口在哪一侧？", Type: "text"},
			decisionQuestion{Field: "house.orientation", Label: "地块朝向", Type: "text"},
			decisionQuestion{Field: "house.neighbor_notes", Label: "周边采光、邻居与地块限制", Type: "text"},
			decisionQuestion{Field: "house.courtyard", Label: "是否需要庭院？", Type: "select", Required: true, Options: []questionOption{{"yes", "需要"}, {"no", "不需要"}, {"unknown", "待比较"}}},
			decisionQuestion{Field: "house.parking", Label: "是否需要停车空间？", Type: "select", Required: true, Options: []questionOption{{"yes", "需要"}, {"no", "不需要"}, {"unknown", "待比较"}}},
			decisionQuestion{Field: "house.start_window", Label: "预计什么时候推进建房？", Type: "text"},
			decisionQuestion{Field: "house.members", Label: "家人分别怎样居住？", Type: "house_members", Help: "可选完整家庭清单；填写时人数合计需与常住和返乡人数一致。"},
			decisionQuestion{Field: "house.rooms", Label: "希望有哪些空间？", Type: "house_rooms", Help: "记录用途、数量、楼层偏好和是否必须满足。"},
		)
		for i := range questions {
			if questions[i].Field == "floors" || questions[i].Field == "bedrooms" {
				questions[i].Required = true
			}
		}
		c.JSON(http.StatusOK, gin.H{"code": 0, "data": gin.H{"version": report.ProtocolHouseV1, "preview_pages": 4, "full_pages": nil, "pagination": "content_driven", "confirmation_choices": []string{"confirm", "undecided"}, "questions": questions}})
	})
}

func registerHouseRoutes(group *gin.RouterGroup, module *ReportModule) {
	group.POST("/house", func(c *gin.Context) {
		if module == nil || !module.HouseEnabled {
			reportError(c, report.ErrDisabled)
			return
		}
		var input report.DecisionInput
		if !decodeReportBody(c, &input, 32<<10) {
			return
		}
		view, err := module.Service.CreateHousePreview(c.Request.Context(), GetTokenCodeSession(c).UserID, input)
		if err != nil {
			reportError(c, err)
			return
		}
		c.JSON(http.StatusAccepted, gin.H{"code": 0, "data": view})
	})
}
