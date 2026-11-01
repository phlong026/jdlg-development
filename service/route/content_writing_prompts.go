package route

import (
	"context"
	"database/sql"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

// writingPrompt is one operable AI-writing template served to the miniprogram.
// The anti-fabrication clause is intentionally NOT part of this registry: it
// stays hardcoded in the client so no configuration can ever disable it.
type writingPrompt struct {
	Key         string `json:"key"`
	Label       string `json:"label"`
	Heading     string `json:"heading"`
	Example     string `json:"example"`
	Instruction string `json:"instruction"`
}

// defaultWritingPrompts mirror the shipped client wording so behavior is
// unchanged before the migration is applied or while the table is empty.
func defaultWritingPrompts() []writingPrompt {
	return []writingPrompt{
		{Key: "design", Label: "设计说明", Heading: "方案设计说明", Example: "本方案为三层框架结构自建房，占地约120平方米。一层布置客厅、餐厨与老人房，二层为主卧套房与书房。立面采用新闽派风格，坡屋顶配合浅色真石漆，整体与村落肌理协调。", Instruction: "请撰写方案设计说明。"},
		{Key: "project", Label: "项目介绍", Heading: "项目内容介绍", Example: "这是位于镇郊的一栋自建房项目，前后院采光充足。设计保留了场地原有大树，入户朝南，室内动线围绕家庭聚餐与老人起居展开，屋顶露台可远眺后山。", Instruction: "请撰写项目内容介绍。"},
		{Key: "promotion", Label: "推广文案", Heading: "作品推广文案", Example: "老宅翻新，不拆承重、不动感情。这次改造保留了三十年前的门头石雕，把新生活装进旧骨架。回乡建房的朋友，欢迎来聊聊你家的老宅故事。", Instruction: "请撰写作品推广文案。"},
	}
}

type writingPromptCache struct {
	mu        sync.Mutex
	loadedAt  time.Time
	prompts   []writingPrompt
	lastError error
}

var writingPromptsCache writingPromptCache

const writingPromptCacheTTL = 60 * time.Second

// loadWritingPrompts returns enabled prompts ordered by sort_order. Missing
// table, empty rows, or a nil DB all fall back to code defaults; failures are
// cached briefly so a broken table cannot turn into a query storm.
func loadWritingPrompts(ctx context.Context, db *sql.DB) []writingPrompt {
	writingPromptsCache.mu.Lock()
	defer writingPromptsCache.mu.Unlock()
	if time.Since(writingPromptsCache.loadedAt) < writingPromptCacheTTL && writingPromptsCache.prompts != nil {
		return writingPromptsCache.prompts
	}
	prompts := defaultWritingPrompts()
	if db != nil {
		rows, err := db.QueryContext(ctx, `SELECT prompt_key,label,heading,example,instruction FROM content_writing_prompts WHERE enabled=1 ORDER BY sort_order,prompt_key`)
		if err == nil {
			defer rows.Close()
			loaded := make([]writingPrompt, 0, 3)
			for rows.Next() {
				var item writingPrompt
				if scanErr := rows.Scan(&item.Key, &item.Label, &item.Heading, &item.Example, &item.Instruction); scanErr != nil {
					loaded = nil
					break
				}
				if item.Key != "" && item.Heading != "" && item.Instruction != "" {
					loaded = append(loaded, item)
				}
			}
			// Query error (e.g. table missing) or malformed rows keep defaults.
			if rows.Err() == nil && len(loaded) > 0 {
				prompts = loaded
			}
		}
	}
	writingPromptsCache.prompts = prompts
	writingPromptsCache.loadedAt = time.Now()
	return prompts
}

// resetWritingPromptCache is used by tests to force a reload.
func resetWritingPromptCache() {
	writingPromptsCache.mu.Lock()
	defer writingPromptsCache.mu.Unlock()
	writingPromptsCache.prompts = nil
	writingPromptsCache.loadedAt = time.Time{}
}

// registerWritingPromptsRoute serves the writing prompt registry. db may be
// nil (unit fixtures): the endpoint then returns code defaults.
func registerWritingPromptsRoute(g *gin.RouterGroup, db *sql.DB) {
	g.GET("/writing-prompts", func(c *gin.Context) {
		prompts := loadWritingPrompts(c.Request.Context(), db)
		trackingResponse(c, gin.H{"prompts": prompts}, nil)
	})
}
