package route

import (
	"fmt"
	"net/http"
	"strconv"

	"service/config"
	"service/model"

	"github.com/gin-gonic/gin"
)

// RegisterAICostRoutes 注册AI造价路由（含根据造价表生成测算表）
func RegisterAICostRoutes(r *gin.RouterGroup, codeSessionModel *model.CodeSessionRedisModel, userModel *model.UserRedisModel, pricingModel *model.AIPricingModel, taskModel *model.AITaskModel, stoneRecordModel *model.StoneRecordModel, userOrderModel *model.UserOrderModel, inviteRelationModel *model.InviteRelationModel, cfg *config.Config) {
	r.POST("/ai/cost/calculate", func(c *gin.Context) {
		handleAICostCalculate(c, codeSessionModel, userModel, pricingModel, taskModel, stoneRecordModel, userOrderModel, inviteRelationModel)
	})
	if cfg != nil {
		RegisterAICostDocRoute(r, codeSessionModel, userModel, pricingModel, taskModel, stoneRecordModel, userOrderModel, inviteRelationModel, cfg)
	}
}

// AICostCalculateRequest AI造价计算请求
type AICostCalculateRequest struct {
	City              string `json:"city"`                // 所在城市
	HouseWidth        string `json:"house_width"`         // 房屋面宽（米）
	HouseDepth        string `json:"house_depth"`         // 房屋进深（米）
	CourtyardDeduct   string `json:"courtyard_deduct"`    // 扣除庭院（m²）
	TerraceDeduct     string `json:"terrace_deduct"`      // 露台扣除（m²）
	StructureType     string `json:"structure_type"`      // 结构形式
	RoofType          string `json:"roof_type"`           // 屋顶形式
	FloorsAboveGround string `json:"floors_above_ground"` // 地上层数
	Basement          string `json:"basement"`            // 地下室
}

// AICostCalculateResponse AI造价计算响应
type AICostCalculateResponse struct {
	TotalCost               string `json:"total_cost"`                // 总造价
	BuildingArea            string `json:"building_area"`             // 建筑面积
	BuildingHeight          string `json:"building_height"`           // 建筑高度
	Foundation              string `json:"foundation"`                // 地基
	MainStructure           string `json:"main_structure"`            // 主体
	Roof                    string `json:"roof"`                      // 屋顶
	DoorsWindows            string `json:"doors_windows"`             // 门窗
	ExteriorDecoration      string `json:"exterior_decoration"`       // 外墙装饰
	WaterElectricityHeating string `json:"water_electricity_heating"` // 水电暖费
}

// handleAICostCalculate 处理AI造价计算请求
func handleAICostCalculate(c *gin.Context, codeSessionModel *model.CodeSessionRedisModel, userModel *model.UserRedisModel, pricingModel *model.AIPricingModel, taskModel *model.AITaskModel, stoneRecordModel *model.StoneRecordModel, userOrderModel *model.UserOrderModel, inviteRelationModel *model.InviteRelationModel) {
	// 验证token
	codeSession := GetTokenCodeSession(c)
	if codeSession == nil {
		c.JSON(http.StatusUnauthorized, gin.H{
			"code": 401,
			"msg":  "未通过token验证",
		})
		return
	}

	var req AICostCalculateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code": 400,
			"msg":  FormatValidationError("参数错误: " + err.Error()),
		})
		return
	}

	// 参数验证
	if req.City == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"code": 400,
			"msg":  "所在城市不能为空",
		})
		return
	}
	if req.HouseWidth == "" || req.HouseDepth == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"code": 400,
			"msg":  "房屋面宽和进深不能为空",
		})
		return
	}

	// 计算建筑面积（简化计算：面宽 × 进深 × 层数 - 扣除）
	width, _ := strconv.ParseFloat(req.HouseWidth, 64)
	depth, _ := strconv.ParseFloat(req.HouseDepth, 64)
	floors, _ := strconv.ParseFloat(req.FloorsAboveGround, 64)
	if floors == 0 {
		floors = 1
	}
	courtyardDeduct, _ := strconv.ParseFloat(req.CourtyardDeduct, 64)
	terraceDeduct, _ := strconv.ParseFloat(req.TerraceDeduct, 64)

	buildingArea := width*depth*floors - courtyardDeduct - terraceDeduct
	if buildingArea < 0 {
		buildingArea = 0
	}

	// 计算建筑高度（简化：每层3米）
	buildingHeight := floors * 3
	if floors == 0 {
		buildingHeight = 3
	}

	// 简化造价计算（示例算法，实际应该调用专业的造价计算服务）
	// 这里使用简化的单价计算
	basePricePerSqm := 800.0 // 基础单价：800元/平方米

	// 根据结构形式调整单价
	structureMultiplier := 1.0
	switch req.StructureType {
	case "框架结构":
		structureMultiplier = 1.2
	case "钢结构":
		structureMultiplier = 1.5
	case "木结构":
		structureMultiplier = 1.1
	default: // 砖混结构
		structureMultiplier = 1.0
	}

	// 根据屋顶形式调整
	roofMultiplier := 1.0
	switch req.RoofType {
	case "平屋顶":
		roofMultiplier = 0.9
	case "斜屋顶":
		roofMultiplier = 1.1
	default: // 坡屋顶
		roofMultiplier = 1.0
	}

	// 地下室成本
	basementCost := 0.0
	if req.Basement == "一层" {
		basementCost = buildingArea * 0.3 * basePricePerSqm
	} else if req.Basement == "二层" {
		basementCost = buildingArea * 0.6 * basePricePerSqm
	}

	// 计算各项费用
	mainStructureCost := buildingArea * basePricePerSqm * structureMultiplier
	foundationCost := buildingArea * 200 * structureMultiplier // 地基：200元/平方米
	roofCost := buildingArea / floors * 150 * roofMultiplier   // 屋顶：150元/平方米
	doorsWindowsCost := buildingArea * 80                      // 门窗：80元/平方米
	exteriorDecorationCost := buildingArea * 120               // 外墙装饰：120元/平方米
	waterElectricityHeatingCost := buildingArea * 60           // 水电暖：60元/平方米

	totalCost := mainStructureCost + foundationCost + roofCost + doorsWindowsCost + exteriorDecorationCost + waterElectricityHeatingCost + basementCost

	// 构建响应
	response := AICostCalculateResponse{
		TotalCost:               fmt.Sprintf("%.0f", totalCost),
		BuildingArea:            fmt.Sprintf("%.0f", buildingArea),
		BuildingHeight:          fmt.Sprintf("%.0f", buildingHeight),
		Foundation:              fmt.Sprintf("%.0f", foundationCost),
		MainStructure:           fmt.Sprintf("%.0f", mainStructureCost),
		Roof:                    fmt.Sprintf("%.0f", roofCost),
		DoorsWindows:            fmt.Sprintf("%.0f", doorsWindowsCost),
		ExteriorDecoration:      fmt.Sprintf("%.0f", exteriorDecorationCost),
		WaterElectricityHeating: fmt.Sprintf("%.0f", waterElectricityHeatingCost),
	}

	// 返回结果
	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"msg":  "计算成功",
		"data": response,
	})
}
