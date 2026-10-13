package route

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"service/model"
)

// ManagementAuditRecorder 管理端写操作审计中间件（权限与审计方案 阶段 A）。
//
// 记录所有非 GET 的管理请求：谁（session 用户）、何时、对什么资源（target_type/target_id
// 从路径提取）、用了什么方法、结果状态码、耗时。请求体刻意不记录（可能含密码/密钥明文）。
func ManagementAuditRecorder(writer *model.AuditWriter) gin.HandlerFunc {
	return func(c *gin.Context) {
		if writer == nil || c.Request.Method == http.MethodGet {
			c.Next()
			return
		}
		start := time.Now()
		c.Next()

		// 路径形如 /api/v1/management/<target_type>[/<id>[/...]]：取资源段做目标
		targetType, targetID := parseAuditTarget(c.Request.URL.Path)
		writer.Offer(&model.ManagementOperationAudit{
			UserID:     GetUserID(c),
			Username:   GetUsername(c),
			Method:     c.Request.Method,
			Path:       c.Request.URL.Path,
			TargetType: targetType,
			TargetID:   targetID,
			Status:     c.Writer.Status(),
			DurationMs: int(time.Since(start).Milliseconds()),
			IP:         c.ClientIP(),
		})
	}
}

// parseAuditTarget 从管理路径提取资源类型与资源 ID。
// 例：/api/v1/management/users/42/stones → ("users", "42")。
func parseAuditTarget(path string) (string, string) {
	rest := strings.TrimPrefix(path, "/api/v1/management/")
	rest = strings.Trim(rest, "/")
	if rest == "" {
		return "", ""
	}
	parts := strings.SplitN(rest, "/", 3)
	targetType := parts[0]
	targetID := ""
	if len(parts) > 1 {
		seg := parts[1]
		// 仅数字段视为资源 ID；非数字（如 users/export）归零
		if _, err := strconv.ParseInt(seg, 10, 64); err == nil {
			targetID = seg
		}
	}
	return targetType, targetID
}

// RegisterManagementAuditRoutes 操作审计查询接口（阶段 A：审计页数据源）。
func RegisterManagementAuditRoutes(r *gin.RouterGroup, auditModel *model.ManagementAuditModel) {
	audit := r.Group("/audit")
	audit.GET("", func(c *gin.Context) {
		if auditModel == nil {
			// 未配置审计模块与"已配置但无审计记录"是两种状态，不能都按成功空列表返回。
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "操作审计模块未配置，服务不可用"})
			return
		}
		page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
		pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
		filter := model.AuditListFilter{
			Username:   strings.TrimSpace(c.Query("username")),
			TargetType: strings.TrimSpace(c.Query("target_type")),
			Method:     strings.ToUpper(strings.TrimSpace(c.Query("method"))),
			Page:       page,
			PageSize:   clampManagementPageSize(pageSize),
		}
		list, total, err := auditModel.List(filter)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "查询操作审计失败: " + err.Error()})
			return
		}
		if list == nil {
			list = make([]*model.ManagementOperationAudit, 0)
		}
		c.JSON(http.StatusOK, gin.H{
			"code": 0,
			"msg":  "success",
			"data": gin.H{
				"list":      list,
				"total":     total,
				"page":      filter.Page,
				"page_size": filter.PageSize,
			},
		})
	})
}
