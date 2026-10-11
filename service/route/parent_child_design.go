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

// RegisterParentChildDesignRoutes 注册亲子设计路由
func RegisterParentChildDesignRoutes(r *gin.RouterGroup, codeSessionModel *model.CodeSessionRedisModel, userModel *model.UserRedisModel, pricingModel *model.AIPricingModel, taskModel *model.AITaskModel, stoneRecordModel *model.StoneRecordModel, userOrderModel *model.UserOrderModel, inviteRelationModel *model.InviteRelationModel) {
	// 亲子设计接口
	r.POST("/ai/parent-child-design", func(c *gin.Context) {
		handleParentChildDesign(c, codeSessionModel, userModel, pricingModel, taskModel, stoneRecordModel, userOrderModel, inviteRelationModel)
	})
}

// ParentChildDesignRequest 亲子设计请求
type ParentChildDesignRequest struct {
	DesignType    string `json:"design_type" binding:"required,oneof=doll poster cultural"` // 设计类型
	Prototype     string `json:"prototype"`                                                 // 原型
	Style         string `json:"style"`                                                     // 风格
	Quality       string `json:"quality"`                                                   // 清晰度
	Canvas        string `json:"canvas"`                                                    // 画布比例
	GenerateCount int64  `json:"generate_count"`                                            // 生成张数
	UserPrompt    string `json:"user_prompt"`                                               // 用户补充描述

	// 玩偶设计相关
	Theme           string   `json:"theme"`            // 主题
	Requirements    string   `json:"requirements"`     // 要求
	ReferenceImages []string `json:"reference_images"` // 参考图片URL列表

	// 海报设计相关
	Text string `json:"text"` // 文字内容

	// 文创设计相关
	ProductType string `json:"product_type"` // 产品类型

	ReferenceImageURLs []string `json:"reference_image_urls"` // 参考图数组
	OrderedImageURLs   []string `json:"ordered_image_urls"`   // 展示顺序图数组
}

// handleParentChildDesign 处理亲子设计请求
func handleParentChildDesign(c *gin.Context, codeSessionModel *model.CodeSessionRedisModel, userModel *model.UserRedisModel, pricingModel *model.AIPricingModel, taskModel *model.AITaskModel, stoneRecordModel *model.StoneRecordModel, userOrderModel *model.UserOrderModel, inviteRelationModel *model.InviteRelationModel) {
	// 验证token
	codeSession := GetTokenCodeSession(c)
	if codeSession == nil {
		c.JSON(http.StatusUnauthorized, gin.H{
			"code": 401,
			"msg":  "未通过token验证",
		})
		return
	}

	var req ParentChildDesignRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code": 400,
			"msg":  FormatValidationError("参数错误: " + err.Error()),
		})
		return
	}

	// 获取计费配置
	scene := "parent_child_design"
	pricing, err := pricingModel.GetByScene(scene)
	if err != nil {
		// 如果没有配置，使用默认值
		pricing = &model.AIPricing{
			Scene:  scene,
			Stones: 20, // 默认20灵石（专业类）
		}
	}
	generateCount := normalizeDesignGenerateCount(req.GenerateCount)
	totalStones := pricing.Stones * generateCount

	// 检查用户余额
	currentStones, err := userModel.GetStones(codeSession.UserID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code": 500,
			"msg":  "查询余额失败: " + err.Error(),
		})
		return
	}

	if currentStones < totalStones {
		c.JSON(http.StatusPaymentRequired, gin.H{
			"code": 402,
			"msg":  "余额不足",
			"data": gin.H{
				"required": totalStones,
				"current":  currentStones,
			},
		})
		return
	}

	// 扣除灵石
	if err := userModel.DeductStones(codeSession.UserID, totalStones); err != nil {
		c.JSON(http.StatusPaymentRequired, gin.H{
			"code": 402,
			"msg":  "扣费失败: " + err.Error(),
		})
		return
	}

	// 写入灵石明细
	if stoneRecordModel != nil {
		_ = stoneRecordModel.Create(codeSession.UserID, "consume", totalStones, fmt.Sprintf("AI生成-亲子设计-%d张", generateCount), "")
	}

	// 写入订单
	if userOrderModel != nil {
		orderNo := model.GenerateOrderNo("ORD")
		_ = userOrderModel.Create(codeSession.UserID, orderNo, "consume", -totalStones, "success", fmt.Sprintf("AI生成-亲子设计-%d张", generateCount), "")
	}

	// 构建提示词
	prompt := buildParentChildDesignPrompt(req)

	// 处理参考图：选择第一张作为主要参考图
	referenceImageURL := ""
	if len(req.ReferenceImages) > 0 {
		referenceImageURL = req.ReferenceImages[0]
	}

	// 构建请求payload
	payload := map[string]interface{}{
		"prompt":         prompt,
		"design_type":    req.DesignType,
		"prototype":      req.Prototype,
		"style":          req.Style,
		"quality":        req.Quality,
		"canvas":         req.Canvas,
		"generate_count": generateCount,
		"user_prompt":    strings.TrimSpace(req.UserPrompt),
	}

	if req.DesignType == "doll" {
		payload["theme"] = req.Theme
		payload["requirements"] = req.Requirements
	} else if req.DesignType == "poster" {
		payload["theme"] = req.Theme
		payload["text"] = req.Text
	} else if req.DesignType == "cultural" {
		payload["product_type"] = req.ProductType
		payload["requirements"] = req.Requirements
	}

	if referenceImageURL != "" {
		payload["image_url"] = referenceImageURL
		if len(req.ReferenceImages) > 0 {
			payload["reference_images"] = req.ReferenceImages
		}
	}
	if len(req.ReferenceImageURLs) > 0 {
		payload["reference_image_urls"] = req.ReferenceImageURLs
	}
	if len(req.OrderedImageURLs) > 0 {
		payload["ordered_image_urls"] = req.OrderedImageURLs
	}

	// 将请求payload转为JSON字符串
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		// 扣费成功但保存失败，需要回退
		userModel.AddStones(codeSession.UserID, totalStones)
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
		StonesUsed:     totalStones,
	}

	if err := taskModel.Create(task); err != nil {
		// 创建任务失败，回退灵石
		userModel.AddStones(codeSession.UserID, totalStones)
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

// buildParentChildDesignPrompt 构建亲子设计提示词
func buildParentChildDesignPrompt(req ParentChildDesignRequest) string {
	var prompt strings.Builder

	// 基础提示
	prompt.WriteString("请帮我生成一个适合亲子一起设计的")

	// 原型说明
	prototypeMap := map[string]string{
		"landmark":  "城市地标",
		"residence": "普通住宅",
		"building":  "建筑",
	}
	if prototypeName, ok := prototypeMap[req.Prototype]; ok {
		prompt.WriteString(fmt.Sprintf("%s主题的", prototypeName))
	}

	// 设计类型
	if req.DesignType == "doll" {
		// 玩偶设计
		prompt.WriteString("可爱玩偶，")
		if req.Theme != "" {
			prompt.WriteString(fmt.Sprintf("主题：%s，", req.Theme))
		}
		if req.Requirements != "" {
			prompt.WriteString(fmt.Sprintf("设计要求：%s，", req.Requirements))
		}
		prompt.WriteString("要求设计可爱、温馨、适合孩子，")

	} else if req.DesignType == "poster" {
		// 海报设计
		prompt.WriteString("可爱海报，")
		if req.Theme != "" {
			prompt.WriteString(fmt.Sprintf("主题：%s，", req.Theme))
		}
		if req.Text != "" {
			prompt.WriteString(fmt.Sprintf("文字内容：%s，", req.Text))
		}
		prompt.WriteString("要求设计可爱、温馨、色彩丰富、适合亲子场景，")

	} else if req.DesignType == "cultural" {
		// 文创设计
		prompt.WriteString("文创产品，")
		if req.ProductType != "" {
			prompt.WriteString(fmt.Sprintf("产品类型：%s，", req.ProductType))
		}
		if req.Requirements != "" {
			prompt.WriteString(fmt.Sprintf("设计要求：%s，", req.Requirements))
		}
		prompt.WriteString("要求设计可爱、实用、适合亲子使用，")
	}

	// 风格说明
	styleMap := map[string]string{
		"cute":  "可爱风格",
		"cool":  "酷炫风格",
		"retro": "复古风格",
		"scifi": "科幻风格",
	}
	if styleName, ok := styleMap[req.Style]; ok {
		prompt.WriteString(fmt.Sprintf("采用%s，", styleName))
	}

	// 参考图说明
	if len(req.ReferenceImages) > 0 {
		prompt.WriteString("请参考提供的参考图片，")
	}
	if strings.TrimSpace(req.UserPrompt) != "" {
		prompt.WriteString(fmt.Sprintf("补充要求：%s，", strings.TrimSpace(req.UserPrompt)))
	}

	// 结尾
	prompt.WriteString("要求设计温馨、有趣、适合亲子互动。")

	return prompt.String()
}
