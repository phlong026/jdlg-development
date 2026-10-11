package route

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"

	"service/component"
	"service/config"
	"service/function"
	"service/model"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// RegisterRuralVillaDesignRoutes 注册乡村别墅设计路由
func RegisterRuralVillaDesignRoutes(r *gin.RouterGroup, codeSessionModel *model.CodeSessionRedisModel, userModel *model.UserRedisModel, pricingModel *model.AIPricingModel, taskModel *model.AITaskModel, stoneRecordModel *model.StoneRecordModel, userOrderModel *model.UserOrderModel, inviteRelationModel *model.InviteRelationModel) {
	// 乡村别墅AI设计接口
	r.POST("/ai/rural-villa-design", func(c *gin.Context) {
		handleRuralVillaDesign(c, codeSessionModel, userModel, pricingModel, taskModel, stoneRecordModel, userOrderModel, inviteRelationModel)
	})
}

// RuralVillaDesignRequest 乡村别墅设计请求
type RuralVillaDesignRequest struct {
	// 尺寸选择
	Template      string   `json:"template"`       // 标准模板值
	CustomLength  string   `json:"custom_length"`  // 自定义长度
	CustomWidth   string   `json:"custom_width"`    // 自定义宽度
	CustomArea    string   `json:"custom_area"`    // 自定义面积
	SizeImages    []string `json:"size_images"`    // 地形图/简图照片URL列表

	// 风格选择
	Style string `json:"style" binding:"required"` // 风格：modern, chinese, min, traditional, shanghai, countryside

	// 地块补充
	PlotDescription string `json:"plot_description"`  // 地块描述
	PlotRequirements string `json:"plot_requirements"` // 特殊要求
	CustomPrompt     string `json:"custom_prompt"`     // 自定义提示词

	// 参考图上传
	ReferenceImages []string `json:"reference_images"` // 参考图片URL列表
}

// handleRuralVillaDesign 处理乡村别墅设计请求
func handleRuralVillaDesign(c *gin.Context, codeSessionModel *model.CodeSessionRedisModel, userModel *model.UserRedisModel, pricingModel *model.AIPricingModel, taskModel *model.AITaskModel, stoneRecordModel *model.StoneRecordModel, userOrderModel *model.UserOrderModel, inviteRelationModel *model.InviteRelationModel) {
	// 验证token
	codeSession := GetTokenCodeSession(c)
	if codeSession == nil {
		c.JSON(http.StatusUnauthorized, gin.H{
			"code": 401,
			"msg":  "未通过token验证",
		})
		return
	}

	var req RuralVillaDesignRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code": 400,
			"msg":  FormatValidationError("参数错误: "+err.Error()),
		})
		return
	}

	// 获取计费配置
	scene := "rural_villa_design"
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
		_ = stoneRecordModel.Create(codeSession.UserID, "consume", pricing.Stones, "AI生成-乡村别墅设计", "")
	}

	// 写入订单
	if userOrderModel != nil {
		orderNo := model.GenerateOrderNo("ORD")
		_ = userOrderModel.Create(codeSession.UserID, orderNo, "consume", -pricing.Stones, "success", "AI生成-乡村别墅设计", "")
	}

	// 构建提示词
	prompt := buildRuralVillaPrompt(req)

	// 处理参考图：如果有参考图，选择第一张作为主要参考图
	referenceImageURL := ""
	if len(req.ReferenceImages) > 0 {
		referenceImageURL = req.ReferenceImages[0]
	} else if len(req.SizeImages) > 0 {
		// 如果没有参考图，使用第一张地形图作为参考
		referenceImageURL = req.SizeImages[0]
	}

	// 构建请求payload
	payload := map[string]interface{}{
		"prompt":      prompt,
		"style":       req.Style,
		"template":    req.Template,
		"custom_area": req.CustomArea,
	}
	if referenceImageURL != "" {
		payload["image_url"] = referenceImageURL
		payload["reference_images"] = req.ReferenceImages
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

// buildRuralVillaPrompt 构建乡村别墅设计提示词
func buildRuralVillaPrompt(req RuralVillaDesignRequest) string {
	// 如果提供了自定义提示词，优先使用自定义提示词
	if strings.TrimSpace(req.CustomPrompt) != "" {
		return req.CustomPrompt
	}

	var prompt strings.Builder

	// 基础提示
	prompt.WriteString("请帮我生成一张乡村别墅设计图，")

	// 尺寸信息
	if req.Template != "" {
		prompt.WriteString(fmt.Sprintf("面积约%s平方米，", req.Template))
	} else if req.CustomArea != "" && req.CustomArea != "0" {
		// 将毫米转换为平方米（假设是平方毫米）
		prompt.WriteString(fmt.Sprintf("面积约%s平方毫米，", req.CustomArea))
	} else if req.CustomLength != "" && req.CustomWidth != "" {
		length := req.CustomLength
		width := req.CustomWidth
		if length != "0" && width != "0" {
			prompt.WriteString(fmt.Sprintf("长%s毫米，宽%s毫米，", length, width))
		}
	}

	// 风格信息
	styleMap := map[string]string{
		"modern":      "现代简约风格",
		"chinese":     "新中式风格",
		"min":         "新闽派风格",
		"traditional": "传统古建风格",
		"shanghai":    "海派风格",
		"countryside": "田园风格",
	}
	if styleName, ok := styleMap[req.Style]; ok {
		prompt.WriteString(fmt.Sprintf("采用%s，", styleName))
	}

	// 地块描述
	if req.PlotDescription != "" {
		prompt.WriteString(fmt.Sprintf("地块情况：%s，", req.PlotDescription))
	}

	// 特殊要求
	if req.PlotRequirements != "" {
		prompt.WriteString(fmt.Sprintf("特殊要求：%s，", req.PlotRequirements))
	}

	// 如果有地形图，说明有地形图参考
	if len(req.SizeImages) > 0 {
		prompt.WriteString("请参考提供的地形图进行设计，")
	}

	// 如果有参考图，说明有参考图
	if len(req.ReferenceImages) > 0 {
		prompt.WriteString("请参考提供的参考图片进行设计，")
	}

	// 结尾
	prompt.WriteString("要求设计美观、实用、符合乡村建筑特色。")

	return prompt.String()
}

// 上传图片到OSS的辅助函数（如果需要）
func uploadImageToOSS(ctx context.Context, imageURL string, userID int64, cfg *config.Config) (string, error) {
	// 如果已经是OSS URL，直接返回
	if strings.Contains(imageURL, "jiadilingguangcos") || strings.Contains(imageURL, cfg.COS.Bucket) {
		return imageURL, nil
	}

	// 下载图片
	resp, err := http.Get(imageURL)
	if err != nil {
		return "", fmt.Errorf("下载图片失败: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("下载图片失败: HTTP %d", resp.StatusCode)
	}

	// 读取图片数据
	imageData, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("读取图片数据失败: %v", err)
	}

	// 确定文件扩展名
	ext := filepath.Ext(imageURL)
	if ext == "" {
		ext = ".jpg"
	}

	// 上传到OSS
	cosClient := component.GetCOSClient()
	if cosClient == nil {
		return "", fmt.Errorf("COS客户端未初始化")
	}

	objectKey := fmt.Sprintf("rural_villa/%d/%s%s", userID, uuid.New().String(), ext)
	contentType := resp.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "image/jpeg"
	}

	fileURL, err := function.UploadBytes(ctx, cosClient, cfg, objectKey, imageData, contentType)
	if err != nil {
		return "", fmt.Errorf("上传图片失败: %v", err)
	}

	return fileURL, nil
}
