package route

import (
	"errors"
	"net/http"
	"service/model"
	"strconv"

	"github.com/gin-gonic/gin"
)

type templateUpdateStore interface {
	GetByID(int64) (*model.Template, error)
	UpdatePartial(*model.Template, *model.Template, model.TemplateUpdate) error
}

// The validators keep the existing tab assignment and image publication rules;
// a non-image edit must not regenerate or rewrite the existing image assets.
func newTemplateUpdateHandler(store templateUpdateStore, validateTabs func(*model.Template) error, validateImage func(*model.Template) error, prepareImages func(*model.Template) error) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, err := strconv.ParseInt(c.Param("id"), 10, 64)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "无效的模板ID"})
			return
		}
		var update model.TemplateUpdate
		if err := c.ShouldBindJSON(&update); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "参数错误: " + err.Error()})
			return
		}
		before, err := store.GetByID(id)
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"code": 404, "msg": "模板不存在"})
			return
		}
		after := *before
		if err := update.Apply(&after); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": err.Error()})
			return
		}
		if err := validateTabs(&after); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": err.Error()})
			return
		}
		if err := validateImage(&after); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": err.Error()})
			return
		}
		if update.ImagesChanged(before) {
			if err := prepareImages(&after); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "生成模板衍生图失败: " + err.Error()})
				return
			}
		}
		if err := store.UpdatePartial(before, &after, update); err != nil {
			if errors.Is(err, model.ErrTemplateUpdateConflict) {
				c.JSON(http.StatusConflict, gin.H{"code": 409, "msg": err.Error()})
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "更新模板失败: " + err.Error()})
			return
		}
		// A concurrent status/scope/source update may have occurred since before.
		// Return the database record, never the now-stale in-memory merge.
		current, err := store.GetByID(id)
		if err != nil {
			c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "修改已保存，但最新数据暂时无法读取，请刷新列表核实", "data": nil, "refresh_required": true})
			return
		}
		c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "更新成功", "data": current})
	}
}
