package route

import (
	"sort"
	"time"

	"github.com/gin-gonic/gin"
)

// 先在全部公开作品中排序，再截取预览，避免只排序最新的两张作品。
func designerWorkPreviews(value interface{}, mainTab string, latest bool) []gin.H {
	works, _ := value.([]gin.H)
	selected := make([]gin.H, 0, len(works))
	category := normalizeDesignerCategoryValue(mainTab)
	for _, work := range works {
		if category == "" || category == "all" || designerWorkMatchesCategory(work, category) {
			selected = append(selected, work)
		}
	}
	sort.SliceStable(selected, func(i, j int) bool {
		a, b := selected[i], selected[j]
		if !latest {
			al, _ := a["likes"].(int64)
			bl, _ := b["likes"].(int64)
			if al != bl {
				return al > bl
			}
		}
		at, _ := a["created_at"].(time.Time)
		bt, _ := b["created_at"].(time.Time)
		if !at.Equal(bt) {
			return at.After(bt)
		}
		ai, _ := a["id"].(int64)
		bi, _ := b["id"].(int64)
		return ai > bi
	})
	if len(selected) > 2 {
		selected = selected[:2]
	}
	return selected
}
