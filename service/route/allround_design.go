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

// RegisterAllroundDesignRoutes 注册全能设计路由
func RegisterAllroundDesignRoutes(r *gin.RouterGroup, codeSessionModel *model.CodeSessionRedisModel, userModel *model.UserRedisModel, pricingModel *model.AIPricingModel, taskModel *model.AITaskModel, stoneRecordModel *model.StoneRecordModel, userOrderModel *model.UserOrderModel, inviteRelationModel *model.InviteRelationModel) {
	// 全能设计接口
	r.POST("/ai/allround-design", func(c *gin.Context) {
		handleAllroundDesign(c, codeSessionModel, userModel, pricingModel, taskModel, stoneRecordModel, userOrderModel, inviteRelationModel)
	})
}

// AllroundDesignRequest 全能设计请求
type AllroundDesignRequest struct {
	DesignType    string `json:"design_type" binding:"required,oneof=style_transform poster cultural"` // 设计类型
	Quality       string `json:"quality"`                                                              // 清晰度
	Canvas        string `json:"canvas"`                                                               // 画布比例
	GenerateCount int64  `json:"generate_count"`                                                       // 生成张数
	UserPrompt    string `json:"user_prompt"`                                                          // 用户补充描述

	// 风格变换相关
	OriginalImage string `json:"original_image"` // 原图URL
	TargetStyle   string `json:"target_style"`   // 目标风格

	// 海报设计相关
	Theme           string   `json:"theme"`            // 海报主题
	Text            string   `json:"text"`             // 海报文字内容
	Style           string   `json:"style"`            // 风格
	ReferenceImages []string `json:"reference_images"` // 参考图片URL列表

	// 文创设计相关
	ProductType  string `json:"product_type"` // 产品类型
	Requirements string `json:"requirements"` // 设计要求

	OriginalImageURLs  []string `json:"original_image_urls"`  // 原始图数组
	ReferenceImageURLs []string `json:"reference_image_urls"` // 参考图数组
	OrderedImageURLs   []string `json:"ordered_image_urls"`   // 展示顺序图数组
}

func normalizeDesignGenerateCount(value int64) int64 {
	if value < 1 {
		return 1
	}
	if value > 3 {
		return 3
	}
	return value
}

// handleAllroundDesign 处理全能设计请求
func handleAllroundDesign(c *gin.Context, codeSessionModel *model.CodeSessionRedisModel, userModel *model.UserRedisModel, pricingModel *model.AIPricingModel, taskModel *model.AITaskModel, stoneRecordModel *model.StoneRecordModel, userOrderModel *model.UserOrderModel, inviteRelationModel *model.InviteRelationModel) {
	// 验证token
	codeSession := GetTokenCodeSession(c)
	if codeSession == nil {
		c.JSON(http.StatusUnauthorized, gin.H{
			"code": 401,
			"msg":  "未通过token验证",
		})
		return
	}

	var req AllroundDesignRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code": 400,
			"msg":  FormatValidationError("参数错误: " + err.Error()),
		})
		return
	}

	// 获取计费配置
	scene := "allround_design"
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
		_ = stoneRecordModel.Create(codeSession.UserID, "consume", totalStones, fmt.Sprintf("AI生成-全能设计-%d张", generateCount), "")
	}

	// 写入订单
	if userOrderModel != nil {
		orderNo := model.GenerateOrderNo("ORD")
		_ = userOrderModel.Create(codeSession.UserID, orderNo, "consume", -totalStones, "success", fmt.Sprintf("AI生成-全能设计-%d张", generateCount), "")
	}

	// 构建提示词
	prompt := buildAllroundDesignPrompt(req)

	// 处理参考图：根据设计类型选择主要参考图
	referenceImageURL := ""
	if req.DesignType == "style_transform" {
		// 风格变换：使用原图作为参考
		referenceImageURL = req.OriginalImage
	} else if len(req.ReferenceImages) > 0 {
		// 海报设计和文创设计：使用第一张参考图
		referenceImageURL = req.ReferenceImages[0]
	}

	// 构建请求payload
	payload := map[string]interface{}{
		"prompt":         prompt,
		"design_type":    req.DesignType,
		"quality":        req.Quality,
		"canvas":         req.Canvas,
		"generate_count": generateCount,
		"user_prompt":    strings.TrimSpace(req.UserPrompt),
	}
	if req.DesignType == "style_transform" {
		payload["target_style"] = req.TargetStyle
	} else if req.DesignType == "poster" {
		payload["style"] = req.Style
		payload["theme"] = req.Theme
		payload["text"] = req.Text
	} else if req.DesignType == "cultural" {
		payload["style"] = req.Style
		payload["product_type"] = req.ProductType
	}

	if referenceImageURL != "" {
		payload["image_url"] = referenceImageURL
		if len(req.ReferenceImages) > 0 {
			payload["reference_images"] = req.ReferenceImages
		}
	}
	if len(req.OriginalImageURLs) > 0 {
		payload["original_image_urls"] = req.OriginalImageURLs
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

// buildAllroundDesignPrompt 构建全能设计提示词
func buildAllroundDesignPrompt(req AllroundDesignRequest) string {
	var prompt strings.Builder

	if req.DesignType == "style_transform" {
		// 风格变换
		prompt.WriteString("请帮我将这张图片转换为")

		styleMap := map[string]string{
			"modern":      "现代简约风格",
			"chinese":     "新中式风格",
			"min":         "新闽派风格",
			"traditional": "传统古建风格",
			"shanghai":    "海派风格",
			"countryside": "田园风格",
			"cartoon":     "卡通风格",
			"watercolor":  "水彩风格",
			"oil":         "油画风格",
			"sketch":      "素描风格",
		}
		if styleName, ok := styleMap[req.TargetStyle]; ok {
			prompt.WriteString(styleName)
		} else {
			prompt.WriteString("指定风格")
		}
		prompt.WriteString("，保持原图的主要内容和构图，只改变风格和视觉效果。")

	} else if req.DesignType == "poster" {
		// 海报设计
		prompt.WriteString("请帮我设计一张海报，")

		if req.Theme != "" {
			prompt.WriteString(fmt.Sprintf("主题：%s，", req.Theme))
		}

		if req.Text != "" {
			prompt.WriteString(fmt.Sprintf("文字内容：%s，", req.Text))
		}

		styleMap := map[string]string{
			"business":     "简约商务风格",
			"fashion":      "时尚潮流风格",
			"literary":     "文艺清新风格",
			"tech":         "科技感风格",
			"vintage":      "复古风格",
			"illustration": "插画风格",
			"handdrawn":    "手绘风格",
			"minimalist":   "极简风格",
		}
		if styleName, ok := styleMap[req.Style]; ok {
			prompt.WriteString(fmt.Sprintf("采用%s，", styleName))
		}

		if len(req.ReferenceImages) > 0 {
			prompt.WriteString("请参考提供的参考图片，")
		}

		prompt.WriteString("要求设计美观、吸引人、符合主题。")

	} else if req.DesignType == "cultural" {
		// 文创设计
		prompt.WriteString("请帮我设计文创产品，")

		if req.ProductType != "" {
			prompt.WriteString(fmt.Sprintf("产品类型：%s，", req.ProductType))
		}

		styleMap := map[string]string{
			"guochao":      "国潮风格",
			"modern":       "简约现代风格",
			"traditional":  "传统元素风格",
			"illustration": "创意插画风格",
			"geometric":    "几何图案风格",
			"typography":   "文字设计风格",
			"abstract":     "抽象艺术风格",
			"nature":       "自然元素风格",
		}
		if styleName, ok := styleMap[req.Style]; ok {
			prompt.WriteString(fmt.Sprintf("采用%s，", styleName))
		}

		if req.Requirements != "" {
			prompt.WriteString(fmt.Sprintf("设计要求：%s，", req.Requirements))
		}

		if len(req.ReferenceImages) > 0 {
			prompt.WriteString("请参考提供的参考图片，")
		}

		prompt.WriteString("要求设计有创意、实用、符合产品特点。")
	}
	if strings.TrimSpace(req.UserPrompt) != "" {
		prompt.WriteString(fmt.Sprintf("补充要求：%s。", strings.TrimSpace(req.UserPrompt)))
	}

	return prompt.String()
}
