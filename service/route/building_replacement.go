package route

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"service/function"
	"service/model"

	"github.com/gin-gonic/gin"
)

// RegisterBuildingReplacementRoutes 注册建筑换新路由
func RegisterBuildingReplacementRoutes(r *gin.RouterGroup, codeSessionModel *model.CodeSessionRedisModel, userModel *model.UserRedisModel, pricingModel *model.AIPricingModel, taskModel *model.AITaskModel, stoneRecordModel *model.StoneRecordModel, userOrderModel *model.UserOrderModel, inviteRelationModel *model.InviteRelationModel) {
	// 建筑换新接口
	r.POST("/ai/building-replacement", func(c *gin.Context) {
		handleBuildingReplacement(c, codeSessionModel, userModel, pricingModel, taskModel, stoneRecordModel, userOrderModel, inviteRelationModel)
	})
}

// BuildingReplacementRequest 建筑换新请求
type BuildingReplacementRequest struct {
	FacadeImage   string `json:"facade_image"`   // 外立面图片URL
	InteriorImage string `json:"interior_image"` // 内部图片URL
	Direction     string `json:"direction" binding:"required,oneof=facade beautification cultural overall"` // 改造方向
	Material      string `json:"material" binding:"required"` // 材料/风格
	CustomPrompt  string `json:"custom_prompt"` // 自定义提示词
}

// handleBuildingReplacement 处理建筑换新请求
func handleBuildingReplacement(c *gin.Context, codeSessionModel *model.CodeSessionRedisModel, userModel *model.UserRedisModel, pricingModel *model.AIPricingModel, taskModel *model.AITaskModel, stoneRecordModel *model.StoneRecordModel, userOrderModel *model.UserOrderModel, inviteRelationModel *model.InviteRelationModel) {
	// 验证token
	codeSession := GetTokenCodeSession(c)
	if codeSession == nil {
		c.JSON(http.StatusUnauthorized, gin.H{
			"code": 401,
			"msg":  "未通过token验证",
		})
		return
	}

	var req BuildingReplacementRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code": 400,
			"msg":  FormatValidationError("参数错误: "+err.Error()),
		})
		return
	}

	// 至少需要一张图片
	if req.FacadeImage == "" && req.InteriorImage == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"code": 400,
			"msg":  "请至少上传一张图片（外立面或内部）",
		})
		return
	}

	// 获取计费配置
	scene := "building_replacement"
	pricing, err := pricingModel.GetByScene(scene)
	if err != nil {
		// 如果没有配置，使用默认值
		pricing = &model.AIPricing{
			Scene:  scene,
			Stones: 20, // 默认20灵石（专业类）
		}
	}

	// 检查用户余额
	currentStones, err := userModel.GetStones(codeSession.UserID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code": 500,
			"msg":  "查询余额失败: " + err.Error(),
		})
		return
	}

	if currentStones < pricing.Stones {
		c.JSON(http.StatusPaymentRequired, gin.H{
			"code": 402,
			"msg":  "余额不足",
			"data": gin.H{
				"required": pricing.Stones,
				"current":  currentStones,
			},
		})
		return
	}

	// 扣除灵石
	if err := userModel.DeductStones(codeSession.UserID, pricing.Stones); err != nil {
		c.JSON(http.StatusPaymentRequired, gin.H{
			"code": 402,
			"msg":  "扣费失败: " + err.Error(),
		})
		return
	}

	// 写入灵石明细
	if stoneRecordModel != nil {
		_ = stoneRecordModel.Create(codeSession.UserID, "consume", pricing.Stones, "AI生成-建筑换新", "")
	}

	// 写入订单
	if userOrderModel != nil {
		orderNo := model.GenerateOrderNo("ORD")
		_ = userOrderModel.Create(codeSession.UserID, orderNo, "consume", -pricing.Stones, "success", "AI生成-建筑换新", "")
	}

	// 构建提示词
	prompt := buildBuildingReplacementPrompt(req)

	// 选择主要参考图：优先使用外立面，如果没有则使用内部
	referenceImageURL := req.FacadeImage
	if referenceImageURL == "" {
		referenceImageURL = req.InteriorImage
	}

	// 构建请求payload
	payload := map[string]interface{}{
		"prompt":      prompt,
		"direction":   req.Direction,
		"material":    req.Material,
		"facade_image": req.FacadeImage,
		"interior_image": req.InteriorImage,
	}

	if referenceImageURL != "" {
		payload["image_url"] = referenceImageURL
		// 如果有两张图片，都作为参考图
		referenceImages := []string{}
		if req.FacadeImage != "" {
			referenceImages = append(referenceImages, req.FacadeImage)
		}
		if req.InteriorImage != "" {
			referenceImages = append(referenceImages, req.InteriorImage)
		}
		if len(referenceImages) > 0 {
			payload["reference_images"] = referenceImages
		}
	}

	// 将请求payload转为JSON字符串
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		// 扣费成功但保存失败，需要回退
		userModel.AddStones(codeSession.UserID, pricing.Stones)
		c.JSON(http.StatusInternalServerError, gin.H{
			"code": 500,
			"msg":  "处理请求数据失败: " + err.Error(),
		})
		return
	}

	// 生成32位唯一任务编号
	taskNo := function.GenerateTaskNo()

	// 创建任务并加入队列
	task := &model.AITask{
		TaskNo:         taskNo,
		UserID:         codeSession.UserID,
		Scene:          scene,
		RequestPayload: string(payloadJSON),
		Status:         "pending",
		StonesUsed:     pricing.Stones,
	}

	if err := taskModel.Create(task); err != nil {
		// 创建任务失败，回退灵石
		userModel.AddStones(codeSession.UserID, pricing.Stones)
		c.JSON(http.StatusInternalServerError, gin.H{
			"code": 500,
			"msg":  "创建任务失败: " + err.Error(),
		})
		return
	}

	// 返回成功
	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"msg":  "任务已提交",
		"data": gin.H{
			"task_id": task.ID,
			"task_no": taskNo,
		},
	})
}

// buildBuildingReplacementPrompt 构建建筑换新提示词
func buildBuildingReplacementPrompt(req BuildingReplacementRequest) string {
	// 如果提供了自定义提示词，优先使用自定义提示词
	if strings.TrimSpace(req.CustomPrompt) != "" {
		return req.CustomPrompt
	}

	var prompt strings.Builder

	// 基础提示
	prompt.WriteString("请帮我生成建筑改造后的效果图，")

	// 改造方向
	directionMap := map[string]string{
		"facade":         "外立面改造",
		"beautification": "空间美化",
		"cultural":       "文化创雕复兴",
		"overall":        "整体改造",
	}
	if directionName, ok := directionMap[req.Direction]; ok {
		prompt.WriteString(fmt.Sprintf("改造方向：%s，", directionName))
	}

	// 材料/风格
	materialMap := map[string]string{
		"modern":          "现代简约风格",
		"modern-material": "现代材料",
		"retro-brick":     "复古砖饰",
		"energy-saving":   "节能材料",
		"metal":           "金属材料",
		"concrete":        "混凝土",
		"wood":            "木质材料",
		"decorative":      "装饰材料",
		"chinese":         "新中式风格",
	}
	if materialName, ok := materialMap[req.Material]; ok {
		prompt.WriteString(fmt.Sprintf("采用%s，", materialName))
	}

	// 图片说明
	if req.FacadeImage != "" && req.InteriorImage != "" {
		prompt.WriteString("请参考提供的外立面和内部图片，")
	} else if req.FacadeImage != "" {
		prompt.WriteString("请参考提供的外立面图片，")
	} else if req.InteriorImage != "" {
		prompt.WriteString("请参考提供的内部图片，")
	}

	// 结尾
	prompt.WriteString("要求改造后的效果美观、实用、符合现代建筑标准，保持建筑的基本结构和功能。")

	return prompt.String()
}
