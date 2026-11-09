package route

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"service/model"
)

type publicDesignerFilters struct {
	Keyword   string
	MainTab   string
	Specialty string
	Region    string
	Sort      string
}

func buildPublicDesignerWhere(filters publicDesignerFilters) (string, []interface{}) {
	// “全部”保留尚未发布作品的设计师；提现资格和普通用户账号不能单独证明设计师身份。
	conditions := []string{
		"u.user_type = 'miniprogram'",
		"COALESCE(up.designer_visible, 1) = 1",
		`(TRIM(COALESCE(up.identity_type, '')) = '设计师'
			OR TRIM(COALESCE(up.service_title, '')) <> ''
			OR TRIM(COALESCE(up.designer_bio, '')) <> ''
			OR TRIM(COALESCE(up.specialty_styles, '')) <> ''
			OR EXISTS (SELECT 1 FROM certification_applications ca WHERE ca.user_id = u.id AND ca.type = 'designer' AND TRIM(ca.identity_type) = '设计师')
			OR EXISTS (SELECT 1 FROM templates tp WHERE tp.creator_user_id = u.id AND tp.status = 'published'))`,
	}
	args := make([]interface{}, 0)
	if keyword := strings.TrimSpace(filters.Keyword); keyword != "" {
		like := "%" + keyword + "%"
		conditions = append(conditions, `(u.username LIKE ? OR COALESCE(up.nickname, '') LIKE ? OR COALESCE(up.service_title, '') LIKE ? OR COALESCE(up.designer_bio, '') LIKE ? OR COALESCE(up.specialty_styles, '') LIKE ? OR EXISTS (SELECT 1 FROM templates tx WHERE tx.creator_user_id = u.id AND tx.status = 'published' AND (tx.name LIKE ? OR tx.description LIKE ?)))`)
		args = append(args, like, like, like, like, like, like, like)
	}
	if mainTab := normalizeDesignerCategoryValue(filters.MainTab); mainTab != "" && mainTab != "all" {
		if condition, categoryArgs := buildDesignerCategoryMatchCondition("t", mainTab); condition != "" {
			conditions = append(conditions, `EXISTS (SELECT 1 FROM templates t WHERE t.creator_user_id = u.id AND t.status = 'published' AND `+condition+`)`)
			args = append(args, categoryArgs...)
		}
	}
	// 按设计师实际填写的专业领域筛选，不从作品分类或名字猜测擅长方向。
	if specialty := strings.TrimSpace(filters.Specialty); specialty != "" {
		conditions = append(conditions, "LOCATE(?, COALESCE(up.specialty_styles, '')) > 0")
		args = append(args, specialty)
	}
	if region := strings.Join(strings.Fields(filters.Region), " "); region != "" {
		conditions = append(conditions, "LOCATE(?, COALESCE(up.designer_region, '')) = 1")
		args = append(args, region)
	}
	return strings.Join(conditions, " AND "), args
}

func publicDesignerOrderBy(value string) string {
	switch value {
	case "popular":
		return "COALESCE((SELECT SUM(ts.like_count) FROM templates ts WHERE ts.creator_user_id = u.id AND ts.status = 'published'), 0) DESC, u.id DESC"
	case "latest":
		return "(SELECT MAX(ts.created_at) FROM templates ts WHERE ts.creator_user_id = u.id AND ts.status = 'published') DESC, u.id DESC"
	default:
		return "u.id DESC"
	}
}

func listPublicDesignerUserIDsWithFilters(userDBModel *model.UserModel, filters publicDesignerFilters, limit, offset int) ([]int64, int64, error) {
	if userDBModel == nil || userDBModel.DB == nil {
		return nil, 0, errors.New("服务不可用")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	where, args := buildPublicDesignerWhere(filters)
	from := " FROM users u LEFT JOIN user_profiles up ON up.user_id = u.id WHERE " + where
	var total int64
	if err := userDBModel.DB.QueryRowContext(ctx, "SELECT COUNT(*)"+from, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count public designers: %w", err)
	}
	query := "SELECT u.id" + from + " ORDER BY " + publicDesignerOrderBy(filters.Sort) + " LIMIT ? OFFSET ?"
	queryArgs := append(append([]interface{}{}, args...), limit, offset)
	rows, err := userDBModel.DB.QueryContext(ctx, query, queryArgs...)
	if err != nil {
		return nil, 0, fmt.Errorf("list public designers: %w", err)
	}
	defer rows.Close()
	ids := make([]int64, 0)
	for rows.Next() {
		var userID int64
		if err := rows.Scan(&userID); err != nil {
			return nil, 0, fmt.Errorf("scan public designer: %w", err)
		}
		ids = append(ids, userID)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("read public designers: %w", err)
	}
	return ids, total, nil
}

func publicDesignerSpecialtyOptions(values []string) []string {
	seen := make(map[string]bool)
	options := make([]string, 0)
	for _, raw := range values {
		raw = strings.TrimSpace(raw)
		raw = strings.TrimPrefix(strings.TrimPrefix(raw, "专业领域："), "专业领域:")
		for _, part := range strings.FieldsFunc(raw, func(r rune) bool {
			return strings.ContainsRune(",，;；、/\n\r", r)
		}) {
			value := strings.TrimSpace(part)
			if value == "" || value == "待完善" || value == "未填写" || seen[value] {
				continue
			}
			seen[value] = true
			options = append(options, value)
		}
	}
	sort.Strings(options)
	return options
}

func listPublicDesignerSpecialties(userDBModel *model.UserModel) ([]string, error) {
	if userDBModel == nil || userDBModel.DB == nil {
		return nil, errors.New("服务不可用")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	// 选项来自全体公开设计师，避免当前分页或当前筛选造成可选方向丢失。
	where, args := buildPublicDesignerWhere(publicDesignerFilters{})
	query := "SELECT DISTINCT COALESCE(up.specialty_styles, '') FROM users u LEFT JOIN user_profiles up ON up.user_id = u.id WHERE " + where
	rows, err := userDBModel.DB.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list public designer specialties: %w", err)
	}
	defer rows.Close()
	values := make([]string, 0)
	for rows.Next() {
		var value string
		if err := rows.Scan(&value); err != nil {
			return nil, fmt.Errorf("scan public designer specialty: %w", err)
		}
		values = append(values, value)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read public designer specialties: %w", err)
	}
	return publicDesignerSpecialtyOptions(values), nil
}

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
