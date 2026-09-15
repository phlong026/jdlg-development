package route

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	"service/model"

	"github.com/gin-gonic/gin"
)

// RegisterAITaskInfoRoutes 注册AI任务信息路由
func RegisterAITaskInfoRoutes(r *gin.RouterGroup, codeSessionModel *model.CodeSessionRedisModel, taskModel *model.AITaskModel) {
	// 根据AI任务ID获取提示词和图片
	r.GET("/ai/task/:task_id/info", func(c *gin.Context) {
		handleGetTaskInfo(c, codeSessionModel, taskModel)
	})
}

// handleGetTaskInfo 处理获取任务信息请求
func handleGetTaskInfo(c *gin.Context, codeSessionModel *model.CodeSessionRedisModel, taskModel *model.AITaskModel) {
	// 验证token
	codeSession := GetTokenCodeSession(c)
	if codeSession == nil {
		c.JSON(http.StatusUnauthorized, gin.H{
			"code": 401,
			"msg":  "未通过token验证",
		})
		return
	}

	// 获取任务ID
	taskIDStr := c.Param("task_id")
	if taskIDStr == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"code": 400,
			"msg":  "任务ID不能为空",
		})
		return
	}

	// 解析任务ID
	var taskID int64
	var err error
	if taskID, err = parseTaskID(taskIDStr); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code": 400,
			"msg":  "无效的任务ID",
		})
		return
	}

	// 获取任务
	task, err := taskModel.GetByID(taskID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"code": 404,
			"msg":  "任务不存在",
		})
		return
	}

	// 验证任务是否属于当前用户
	if task.UserID != codeSession.UserID {
		c.JSON(http.StatusForbidden, gin.H{
			"code": 403,
			"msg":  "无权访问该任务",
		})
		return
	}

	// 解析请求payload
	var payload map[string]interface{}
	if err := json.Unmarshal([]byte(task.RequestPayload), &payload); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code": 500,
			"msg":  "解析任务数据失败",
		})
		return
	}

	// 提取提示词
	prompt := ""
	if !shouldHidePromptFromPayloadMap(payload) {
		if v, ok := payload["prompt"].(string); ok {
			prompt = v
		}
	}
	originalImageURLs := parseOriginalImagesFromPayload(task.RequestPayload)
	referenceImageURLs := parseReferenceImagesFromPayload(task.RequestPayload)
	orderedImageURLs := parseOrderedImagesFromPayload(task.RequestPayload)

	// 提取图片URL（支持多种字段名）
	var imageURLs []string

	// 检查 images 数组
	if images, ok := payload["images"].([]interface{}); ok {
		for _, img := range images {
			if imgStr, ok := img.(string); ok && imgStr != "" {
				imageURLs = append(imageURLs, imgStr)
			}
		}
	}

	// 检查 image_urls 数组
	if imageUrls, ok := payload["image_urls"].([]interface{}); ok {
		for _, img := range imageUrls {
			if imgStr, ok := img.(string); ok && imgStr != "" {
				imageURLs = append(imageURLs, imgStr)
			}
		}
	}

	// 检查 reference_images 数组
	if refImages, ok := payload["reference_images"].([]interface{}); ok {
		for _, img := range refImages {
			if imgStr, ok := img.(string); ok && imgStr != "" {
				imageURLs = append(imageURLs, imgStr)
			}
		}
	}

	// 如果没有数组，检查单个图片字段
	if len(imageURLs) == 0 {
		if v, ok := payload["image"].(string); ok && v != "" {
			imageURLs = append(imageURLs, v)
		}
		if v, ok := payload["image_url"].(string); ok && v != "" {
			imageURLs = append(imageURLs, v)
		}
		if v, ok := payload["original_image"].(string); ok && v != "" {
			imageURLs = append(imageURLs, v)
		}
	}

	if len(orderedImageURLs) > 0 {
		imageURLs = orderedImageURLs
	}

	// 返回结果
	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"msg":  "success",
		"data": gin.H{
			"prompt":               prompt,
			"image_urls":           imageURLs,
			"reference_image_url":  parseReferenceImageFromPayload(task.RequestPayload),
			"original_image_urls":  originalImageURLs,
			"reference_image_urls": referenceImageURLs,
			"ordered_image_urls":   orderedImageURLs,
		},
	})
}

// parseTaskID 解析任务ID（支持数字ID或任务编号）
func parseTaskID(taskIDStr string) (int64, error) {
	// 先尝试作为数字ID解析
	if taskID, err := strconv.ParseInt(taskIDStr, 10, 64); err == nil {
		return taskID, nil
	}

	// 如果不是数字，可能是任务编号，需要通过任务编号查询
	// 这里简化处理，如果前端传的是任务编号，需要修改调用方式
	return 0, fmt.Errorf("invalid task ID format")
}
