package route

import (
    "database/sql"
    "net/http"
    "sort"
    "strconv"
    "time"

    "github.com/gin-gonic/gin"

    "service/model"
)

type workbenchTaskRow struct {
    CreatedAt time.Time
    Payload   gin.H
}

func listWorkbenchSameDeviceUsers(db *sql.DB, userID int64, deviceID string, limit int) ([]gin.H, int64, error) {
    if db == nil || deviceID == "" {
        return []gin.H{}, 0, nil
    }

    var otherCount int64
    if err := db.QueryRow(`SELECT COUNT(*) FROM user_profiles WHERE device_id = ? AND user_id <> ?`, deviceID, userID).Scan(&otherCount); err != nil {
        return nil, 0, err
    }
    if otherCount == 0 {
        return []gin.H{}, 1, nil
    }

    rows, err := db.Query(`
        SELECT u.id, COALESCE(u.username, ''), COALESCE(NULLIF(p.nickname, ''), '')
        FROM user_profiles p
        LEFT JOIN users u ON u.id = p.user_id
        WHERE p.device_id = ? AND p.user_id <> ?
        ORDER BY COALESCE(u.updated_at, u.created_at) DESC, u.id DESC
        LIMIT ?
    `, deviceID, userID, limit)
    if err != nil {
        return nil, 0, err
    }
    defer rows.Close()

    users := make([]gin.H, 0)
    for rows.Next() {
        var peerUserID int64
        var username string
        var displayName string
        if err := rows.Scan(&peerUserID, &username, &displayName); err != nil {
            return nil, 0, err
        }
        users = append(users, gin.H{
            "user_id":      peerUserID,
            "username":     username,
            "display_name": displayName,
        })
    }
    if err := rows.Err(); err != nil {
        return nil, 0, err
    }

    return users, otherCount + 1, nil
}

func RegisterUserWorkbenchManagementRoutes(r *gin.RouterGroup, userDBModel *model.UserModel, userRedisModel *model.UserRedisModel, userProfileModel *model.UserProfileModel, stoneRecordModel *model.StoneRecordModel, userOrderModel *model.UserOrderModel, userMembershipModel *model.UserMembershipModel, taskModel *model.AITaskModel, videoTaskModel *model.AIVideoTaskModel, pricingModel *model.AIPricingModel) {
    users := r.Group("/users")

    users.GET("/:id/workbench", func(c *gin.Context) {
        id, err := strconv.ParseInt(c.Param("id"), 10, 64)
        if err != nil || id <= 0 {
            c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "无效的用户ID"})
            return
        }
        if userDBModel == nil {
            c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "用户服务不可用"})
            return
        }
        if _, err := userDBModel.GetByID(id); err != nil {
            c.JSON(http.StatusNotFound, gin.H{"code": 404, "msg": "用户不存在"})
            return
        }

        currentStones := int64(0)
        if userRedisModel != nil {
            if stones, stonesErr := userRedisModel.GetStones(id); stonesErr == nil {
                currentStones = stones
            } else if userDBModel.DB != nil {
                _ = userDBModel.DB.QueryRow("SELECT COALESCE(stones, 0) FROM users WHERE id = ?", id).Scan(&currentStones)
            }
        } else if userDBModel.DB != nil {
            _ = userDBModel.DB.QueryRow("SELECT COALESCE(stones, 0) FROM users WHERE id = ?", id).Scan(&currentStones)
        }

        profile, _ := func() (*model.UserProfile, error) {
            if userProfileModel == nil {
                return nil, nil
            }
            return userProfileModel.GetByUserID(id)
        }()

        recentConsume, recentGain, checkinTotal := int64(0), int64(0), int64(0)
        stoneRecords := make([]gin.H, 0)
        if stoneRecordModel != nil {
            recentConsume, recentGain, checkinTotal, _ = stoneRecordModel.Summary(id)
            records, _, recordErr := stoneRecordModel.List(id, "all", 5, 0)
            if recordErr == nil {
                for _, item := range records {
                    stoneRecords = append(stoneRecords, gin.H{
                        "id": item.ID,
                        "type": item.Type,
                        "amount": item.Amount,
                        "scene_desc": item.SceneDesc,
                        "remark": item.Remark,
                        "created_at": item.CreatedAt,
                    })
                }
            }
        }

        orders := make([]gin.H, 0)
        if userOrderModel != nil {
            orderList, _, orderErr := userOrderModel.List(id, "all", 5, 0)
            if orderErr == nil {
                for _, item := range orderList {
                    orders = append(orders, gin.H{
                        "id": item.ID,
                        "order_no": item.OrderNo,
                        "type": item.Type,
                        "order_category": item.OrderCategory,
                        "amount": item.Amount,
                        "status": item.Status,
                        "title": item.Title,
                        "description": item.Description,
                        "created_at": item.CreatedAt,
                        "completed_at": item.CompletedAt,
                    })
                }
            }
        }

        membershipPayload := gin.H(nil)
        if userMembershipModel != nil {
            membership, membershipErr := userMembershipModel.GetByUserID(id)
            if membershipErr == nil && membership != nil {
                membershipPayload = gin.H{
                    "plan_code": membership.PlanCode,
                    "plan_title": membership.PlanTitle,
                    "status": membership.Status,
                    "template_download_enabled": membership.TemplateDownloadEnabled,
                    "started_at": membership.StartedAt,
                    "granted_at": membership.GrantedAt,
                    "expired_at": membership.ExpiredAt,
                    "source_order_no": membership.SourceOrderNo,
                    "is_lifetime": model.IsLifetimeMembership(membership),
                }
            }
        }

        taskRows := make([]workbenchTaskRow, 0)
        if taskModel != nil {
            tasks, taskErr := taskModel.GetByUserID(id, 6, 0)
            if taskErr == nil {
                for _, item := range tasks {
                    taskRows = append(taskRows, workbenchTaskRow{
                        CreatedAt: item.CreatedAt,
                        Payload: gin.H{
                            "task_no": item.TaskNo,
                            "task_type": "image",
                            "scene": item.Scene,
                            "status": item.Status,
                            "stones_used": item.StonesUsed,
                            "error_message": item.GetErrorMessage(),
                            "created_at": item.CreatedAt,
                            "updated_at": item.UpdatedAt,
                        },
                    })
                }
            }
        }
        if videoTaskModel != nil {
            videoTasks, videoErr := videoTaskModel.GetByUserID(id, 6, 0)
            if videoErr == nil {
                for _, item := range videoTasks {
                    taskRows = append(taskRows, workbenchTaskRow{
                        CreatedAt: item.CreatedAt,
                        Payload: gin.H{
                            "task_no": "v" + strconv.FormatInt(item.ID, 10),
                            "task_type": "video",
                            "scene": "ai_video",
                            "status": model.AIVideoStatusForManagement(item.Status),
                            "stones_used": getVideoStones(pricingModel, item.SegmentCount),
                            "error_message": item.GetErrorMessage(),
                            "prompt": item.Prompt,
                            "created_at": item.CreatedAt,
                            "updated_at": item.UpdatedAt,
                        },
                    })
                }
            }
        }
        sort.Slice(taskRows, func(i, j int) bool {
            return taskRows[i].CreatedAt.After(taskRows[j].CreatedAt)
        })
        recentTasks := make([]gin.H, 0)
        for index, item := range taskRows {
            if index >= 6 {
                break
            }
            recentTasks = append(recentTasks, item.Payload)
        }

        sameDeviceUsers := make([]gin.H, 0)
        sameDeviceAccountCount := int64(0)
        riskTags := make([]string, 0)
        riskLevel := "low"
        deviceID := ""
        hasPassword := false
        var deviceBindTime interface{}
        var lastDeviceChangeTime interface{}
        if profile != nil {
            deviceID = profile.DeviceID
            hasPassword = profile.HasPassword
            if profile.DeviceBindTime != nil {
                deviceBindTime = profile.DeviceBindTime
            }
            if profile.LastDeviceChangeTime != nil {
                lastDeviceChangeTime = profile.LastDeviceChangeTime
            }
            if !profile.HasPassword {
                riskTags = append(riskTags, "no_password")
            }
            if profile.LastDeviceChangeTime != nil && profile.LastDeviceChangeTime.After(time.Now().Add(-7*24*time.Hour)) {
                riskTags = append(riskTags, "recent_device_change")
            }
            if profile.DeviceID != "" && userDBModel != nil && userDBModel.DB != nil {
                sameUsers, accountCount, sameDeviceErr := listWorkbenchSameDeviceUsers(userDBModel.DB, id, profile.DeviceID, 5)
                if sameDeviceErr == nil {
                    sameDeviceUsers = sameUsers
                    sameDeviceAccountCount = accountCount
                    if accountCount > 1 {
                        riskTags = append(riskTags, "same_device_multiple_accounts")
                    }
                }
            }
        }
        switch {
        case len(riskTags) >= 2:
            riskLevel = "high"
        case len(riskTags) == 1:
            riskLevel = "medium"
        }

        c.JSON(http.StatusOK, gin.H{
            "code": 0,
            "msg":  "success",
            "data": gin.H{
                "membership": membershipPayload,
                "stone_summary": gin.H{
                    "current_stones": currentStones,
                    "recent_consume": recentConsume,
                    "recent_gain": recentGain,
                    "checkin_total": checkinTotal,
                },
                "stone_records": stoneRecords,
                "recent_orders": orders,
                "recent_tasks": recentTasks,
                "device_risk": gin.H{
                    "device_id":                 deviceID,
                    "device_bind_time":          deviceBindTime,
                    "last_device_change_time":   lastDeviceChangeTime,
                    "has_password":              hasPassword,
                    "same_device_account_count": sameDeviceAccountCount,
                    "same_device_other_users":   sameDeviceUsers,
                    "risk_tags":                 riskTags,
                    "risk_level":                riskLevel,
                },
            },
        })
    })
}
