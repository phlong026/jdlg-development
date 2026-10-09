package route

import (
    "database/sql"
    "fmt"
    "net/http"
    "strconv"
    "strings"

    "github.com/gin-gonic/gin"

    "service/function"
    "service/model"
)

func mapVideoTaskStatus(status string) string {
    return model.AIVideoStatusForManagement(status)
}

func supportTicketPriorityByTaskStatus(status string) string {
	return model.AIVideoSupportTicketPriority(status)
}

// createAITaskSupportTicketFromTask 为图片任务创建异常工单（存在未关闭工单则直接返回其 ID）。
// 单条与批量转单共用；ticketID=0 且 err=nil 表示创建成功（见返回值）。
func createAITaskSupportTicketFromTask(supportTicketModel *model.SupportTicketModel, task *model.AITask, operator string) (ticketID int64, existed bool, err error) {
	resolvedModel := task.GetResolvedModel()
	resolvedAPIEndpoint := task.GetResolvedAPIEndpoint()
	sourceID := "image-" + strconv.FormatInt(task.ID, 10)
	if existingID, existingErr := supportTicketModel.GetLatestOpenTicketIDBySource("task", sourceID); existingErr == nil {
		return existingID, true, nil
	} else if existingErr != sql.ErrNoRows {
		return 0, false, existingErr
	}
	ticketID, err = supportTicketModel.Create(&model.SupportTicketCreateInput{
		UserID:     task.UserID,
		Type:       "task",
		SourceType: "task",
		SourceID:   sourceID,
		Title:      fmt.Sprintf("AI图片任务待排查：%s", task.TaskNo),
		Content:    fmt.Sprintf("任务状态：%s；场景：%s；模型：%s；灵石：%d；错误信息：%s", task.Status, task.Scene, resolvedModel, task.StonesUsed, task.GetErrorMessage()),
		Priority:   supportTicketPriorityByTaskStatus(task.Status),
		CreatedBy:  operator,
		SourcePayload: map[string]interface{}{
			"task_id":         task.ID,
			"task_no":         task.TaskNo,
			"task_type":       "image",
			"status":          task.Status,
			"scene":           task.Scene,
			"model":           resolvedModel,
			"api_endpoint":    resolvedAPIEndpoint,
			"stones_used":     task.StonesUsed,
			"error_message":   task.GetErrorMessage(),
			"request_payload": task.RequestPayload,
			"result_payload":  task.GetResultPayload(),
			"created_at":      task.CreatedAt,
			"updated_at":      task.UpdatedAt,
		},
	})
	return ticketID, false, err
}

func RegisterAITaskManagementRoutes(r *gin.RouterGroup, pricingModel *model.AIPricingModel, taskModel *model.AITaskModel, videoTaskModel *model.AIVideoTaskModel, supportTicketModel *model.SupportTicketModel) {
    ai := r.Group("/ai")

    ai.POST("/tasks/backfill-models", func(c *gin.Context) {
        if taskModel == nil {
            c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "AI任务服务不可用"})
            return
        }
        var req struct {
            Limit int `json:"limit"`
        }
        if err := c.ShouldBindJSON(&req); err != nil {
            c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "参数错误: " + err.Error()})
            return
        }
        updatedCount, inspectedCount, err := taskModel.BackfillResolvedExecutionMetaForSuccessTasks(req.Limit)
        if err != nil {
            c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "批量回填AI任务模型失败: " + err.Error()})
            return
        }
        limit := req.Limit
        if limit < 1 {
            limit = 200
        }
        if limit > 5000 {
            limit = 5000
        }
        c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "success", "data": gin.H{
            "updated_count":   updatedCount,
            "inspected_count": inspectedCount,
            "limit":           limit,
        }})
    })

    ai.GET("/tasks", func(c *gin.Context) {
        if taskModel == nil {
            c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "success", "data": gin.H{"list": []gin.H{}, "summary": gin.H{}, "total": 0, "page": 1, "page_size": 20}})
            return
        }
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
        keyword := strings.TrimSpace(c.Query("keyword"))
        status := strings.TrimSpace(c.DefaultQuery("status", "all"))
        scene := strings.TrimSpace(c.DefaultQuery("scene", "all"))
        queue := strings.TrimSpace(c.DefaultQuery("queue", "all"))
        offset := (page - 1) * pageSize
        var list []*model.AITaskManagementItem
        var total int64
        var err error
        if queue == "failed" || queue == "timeout" || queue == "running" || queue == "today" {
            list, total, err = taskModel.ListForManagementQueue(queue, keyword, status, scene, pageSize, offset)
        } else {
            list, total, err = taskModel.ListForManagement(keyword, status, scene, pageSize, offset)
        }
        if err != nil {
            c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "获取AI任务列表失败: " + err.Error()})
            return
        }
        summary, err := taskModel.SummaryForManagement(keyword, status, scene)
        if err != nil {
            c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "获取AI任务汇总失败: " + err.Error()})
            return
        }
        queueCounts, err := taskModel.QueueCounts()
        if err != nil {
            queueCounts = &model.AIQueueCounts{}
        }
        c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "success", "data": gin.H{"list": list, "summary": summary, "queues": queueCounts, "total": total, "page": page, "page_size": pageSize}})
    })

    ai.GET("/tasks/:id", func(c *gin.Context) {
        if taskModel == nil {
            c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "AI任务服务不可用"})
            return
        }
        id, err := strconv.ParseInt(c.Param("id"), 10, 64)
        if err != nil || id <= 0 {
            c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "无效的任务ID"})
            return
        }
        task, err := taskModel.GetByID(id)
        if err != nil || task == nil {
            c.JSON(http.StatusNotFound, gin.H{"code": 404, "msg": "任务不存在"})
            return
        }
        resolvedModel := task.GetResolvedModel()
        resolvedAPIEndpoint := task.GetResolvedAPIEndpoint()
        c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "success", "data": gin.H{
            "id": task.ID,
            "type": "image",
            "task_no": task.TaskNo,
            "user_id": task.UserID,
            "scene": task.Scene,
            "model": resolvedModel,
            "api_endpoint": resolvedAPIEndpoint,
            "status": task.Status,
            "stones_used": task.StonesUsed,
            "error_message": task.GetErrorMessage(),
            "request_payload": task.RequestPayload,
            "result_payload": task.GetResultPayload(),
            "created_at": task.CreatedAt,
            "updated_at": task.UpdatedAt,
        }})
    })

	ai.POST("/tasks/:id/support-ticket", func(c *gin.Context) {
		if taskModel == nil || supportTicketModel == nil || supportTicketModel.DB == nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "AI任务或工单服务不可用"})
			return
		}
		id, err := strconv.ParseInt(c.Param("id"), 10, 64)
		if err != nil || id <= 0 {
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "无效的任务ID"})
			return
		}
		task, err := taskModel.GetByID(id)
		if err != nil || task == nil {
			c.JSON(http.StatusNotFound, gin.H{"code": 404, "msg": "任务不存在"})
			return
		}
		ticketID, existed, createErr := createAITaskSupportTicketFromTask(supportTicketModel, task, GetUsername(c))
		if createErr != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "创建异常工单失败: " + createErr.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "success", "data": gin.H{"id": ticketID, "existed": existed}})
	})

	// 批量转异常工单：失败队列勾选多条一次性建单（已有未关闭工单的任务自动跳过）
	ai.POST("/tasks/batch-support-tickets", func(c *gin.Context) {
		if taskModel == nil || supportTicketModel == nil || supportTicketModel.DB == nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "AI任务或工单服务不可用"})
			return
		}
		var req struct {
			IDs []int64 `json:"ids" binding:"required"`
		}
		if err := c.ShouldBindJSON(&req); err != nil || len(req.IDs) == 0 {
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "参数错误：ids 不能为空"})
			return
		}
		if len(req.IDs) > 50 {
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "单次最多批量转 50 条"})
			return
		}
		operator := GetUsername(c)
		createdIDs := make([]int64, 0, len(req.IDs))
		existedIDs := make([]int64, 0, len(req.IDs))
		var firstErrMsg string
		for _, id := range req.IDs {
			task, err := taskModel.GetByID(id)
			if err != nil || task == nil {
				continue
			}
			ticketID, existed, createErr := createAITaskSupportTicketFromTask(supportTicketModel, task, operator)
			if createErr != nil {
				if firstErrMsg == "" {
					firstErrMsg = createErr.Error()
				}
				continue
			}
			if existed {
				existedIDs = append(existedIDs, ticketID)
			} else {
				createdIDs = append(createdIDs, ticketID)
			}
		}
		c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "success", "data": gin.H{
			"created_count": len(createdIDs),
			"existed_count": len(existedIDs),
			"created_ids":   createdIDs,
			"first_error":   firstErrMsg,
		}})
	})

	// 重新执行失败任务：以当前启用配置重新入队，不重复扣费（stones=0，属管理员补偿动作）
	ai.POST("/tasks/:id/retry", func(c *gin.Context) {
		if taskModel == nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "AI任务服务不可用"})
			return
		}
		id, err := strconv.ParseInt(c.Param("id"), 10, 64)
		if err != nil || id <= 0 {
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "无效的任务ID"})
			return
		}
		task, err := taskModel.GetByID(id)
		if err != nil || task == nil {
			c.JSON(http.StatusNotFound, gin.H{"code": 404, "msg": "任务不存在"})
			return
		}
		if task.Status != "failed" {
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "仅失败任务支持重新执行"})
			return
		}
		// 造价文档走专用异步流水线（通用处理器会跳过），在此重试会永远滞留 pending
		if task.Scene == "ai_cost_doc" {
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "造价文档任务不支持在此重试（走专用流水线），请转工单人工处理"})
			return
		}
		retryTask := &model.AITask{
			TaskNo:         function.GenerateTaskNo(),
			UserID:         task.UserID,
			ToolID:         task.ToolID,
			Scene:          task.Scene,
			RequestPayload: task.RequestPayload,
			Status:         "pending",
			StonesUsed:     0,
		}
		if err := taskModel.Create(retryTask); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "创建重试任务失败: " + err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "success", "data": gin.H{
			"new_task_id": retryTask.ID,
			"new_task_no": retryTask.TaskNo,
			"retry_of":    task.TaskNo,
			"note":        "重试任务已入队，按当前启用配置执行，不重复扣费",
		}})
	})

    ai.GET("/video-tasks", func(c *gin.Context) {
        if videoTaskModel == nil {
            c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "success", "data": gin.H{"list": []gin.H{}, "summary": gin.H{}, "total": 0, "page": 1, "page_size": 20}})
            return
        }
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
        keyword := strings.TrimSpace(c.Query("keyword"))
        status := strings.TrimSpace(c.DefaultQuery("status", "all"))
        queue := strings.TrimSpace(c.DefaultQuery("queue", "all"))
        offset := (page - 1) * pageSize
        var list []*model.AIVideoTaskManagementItem
        var total int64
        var err error
        if queue == "failed" || queue == "timeout" || queue == "running" || queue == "today" {
            list, total, err = videoTaskModel.ListForManagementQueue(queue, keyword, pageSize, offset)
        } else {
            list, total, err = videoTaskModel.ListForManagement(keyword, status, pageSize, offset)
        }
        if err != nil {
            c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "获取视频任务列表失败: " + err.Error()})
            return
        }
        totalCount, queuedCount, processingCount, failedCount, err := videoTaskModel.SummaryForManagement(keyword, status)
        if err != nil {
            c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "获取视频任务汇总失败: " + err.Error()})
            return
        }
        queues, queueErr := videoTaskModel.VideoQueueCounts()
        if queueErr != nil {
            queues = &model.AIQueueCounts{}
        }
        result := make([]gin.H, 0, len(list))
        for _, item := range list {
            stonesUsed := getVideoStones(pricingModel, item.SegmentCount)
            result = append(result, gin.H{
                "id": item.ID,
                "task_no": item.TaskNo,
                "user_id": item.UserID,
                "username": item.Username,
                "model": item.Model,
                "prompt": item.Prompt,
                "status": mapVideoTaskStatus(item.Status),
                "raw_status": item.Status,
                "segment_count": item.SegmentCount,
                "duration": item.Duration,
                "resolution": item.Resolution,
                "stones_used": stonesUsed,
                "error_message": item.ErrorMessage,
                "raw_error_message": item.RawErrorMessage,
                "created_at": item.CreatedAt,
                "updated_at": item.UpdatedAt,
            })
        }
        c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "success", "data": gin.H{"list": result, "summary": gin.H{"total_count": totalCount, "pending_count": queuedCount, "running_count": processingCount, "failed_count": failedCount}, "queues": queues, "total": total, "page": page, "page_size": pageSize}})
    })

    ai.GET("/video-tasks/:id", func(c *gin.Context) {
        if videoTaskModel == nil {
            c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "视频任务服务不可用"})
            return
        }
        id, err := strconv.ParseInt(c.Param("id"), 10, 64)
        if err != nil || id <= 0 {
            c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "无效的视频任务ID"})
            return
        }
        task, err := videoTaskModel.GetByID(id)
        if err != nil || task == nil {
            c.JSON(http.StatusNotFound, gin.H{"code": 404, "msg": "视频任务不存在"})
            return
        }
        c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "success", "data": gin.H{
            "id": task.ID,
            "type": "video",
            "task_no": "v" + strconv.FormatInt(task.ID, 10),
            "user_id": task.UserID,
            "external_id": task.ExternalID,
            "model": task.Model,
            "prompt": task.Prompt,
            "status": mapVideoTaskStatus(task.Status),
            "raw_status": task.Status,
            "stones_used": getVideoStones(pricingModel, task.SegmentCount),
            "segment_count": task.SegmentCount,
            "duration": task.Duration,
            "resolution": task.Resolution,
            "oss_url": task.OSSURL,
            "error_message": task.GetErrorMessage(),
            "raw_error_message": task.GetRawErrorMessage(),
            "created_at": task.CreatedAt,
            "updated_at": task.UpdatedAt,
        }})
    })

	ai.POST("/video-tasks/:id/support-ticket", func(c *gin.Context) {
		if videoTaskModel == nil || supportTicketModel == nil || supportTicketModel.DB == nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "视频任务或工单服务不可用"})
			return
		}
		id, err := strconv.ParseInt(c.Param("id"), 10, 64)
		if err != nil || id <= 0 {
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "无效的视频任务ID"})
			return
		}
		task, err := videoTaskModel.GetByID(id)
		if err != nil || task == nil {
			c.JSON(http.StatusNotFound, gin.H{"code": 404, "msg": "视频任务不存在"})
			return
		}
		sourceID := "video-" + strconv.FormatInt(task.ID, 10)
		if existingID, existingErr := supportTicketModel.GetLatestOpenTicketIDBySource("task", sourceID); existingErr == nil {
			c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "success", "data": gin.H{"id": existingID, "existed": true}})
			return
		} else if existingErr != sql.ErrNoRows {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "查询已有工单失败: " + existingErr.Error()})
			return
		}
		mappedStatus := mapVideoTaskStatus(task.Status)
		ticketID, createErr := supportTicketModel.Create(&model.SupportTicketCreateInput{
			UserID:     task.UserID,
			Type:       "task",
			SourceType: "task",
			SourceID:   sourceID,
			Title:      fmt.Sprintf("AI视频任务待排查：v%d", task.ID),
			Content:    fmt.Sprintf("任务状态：%s；模型：%s；分段：%d；时长：%d；错误信息：%s", mappedStatus, task.Model, task.SegmentCount, task.Duration, task.GetErrorMessage()),
			Priority:   supportTicketPriorityByTaskStatus(mappedStatus),
			CreatedBy:  GetUsername(c),
			SourcePayload: map[string]interface{}{
				"task_id":        task.ID,
				"task_no":        "v" + strconv.FormatInt(task.ID, 10),
				"task_type":      "video",
				"status":         mappedStatus,
				"raw_status":     task.Status,
				"model":          task.Model,
				"prompt":         task.Prompt,
				"external_id":    task.ExternalID,
				"oss_url":        task.OSSURL,
				"duration":       task.Duration,
				"resolution":     task.Resolution,
				"segment_count":  task.SegmentCount,
				"error_message":  task.GetErrorMessage(),
				"raw_error_message": task.GetRawErrorMessage(),
				"created_at":     task.CreatedAt,
				"updated_at":     task.UpdatedAt,
			},
		})
		if createErr != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "创建异常工单失败: " + createErr.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "success", "data": gin.H{"id": ticketID, "existed": false}})
	})
}
