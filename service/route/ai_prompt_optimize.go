package route

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"service/config"
	"service/model"

	"github.com/gin-gonic/gin"
)

// PromptOptimizeRequest AI优化提示词请求
type PromptOptimizeRequest struct {
	RawPrompt string `json:"raw_prompt" binding:"required"`
	Scene     string `json:"scene"` // architecture, interior, landscape, video, general
}

// 各场景的优化 system prompt
var sceneOptimizePrompts = map[string]string{
	"architecture": `你是一位专业的建筑设计AI提示词优化专家。用户会输入一段关于建筑设计的描述，请将其优化为结构化的AI绘图提示词。
优化规则：
1. 提取并明确：建筑类型、层数、风格、材质、色彩
2. 补充：视角（鸟瞰/人视/透视）、光影（时段/天气）、环境（植物/道路/天空）
3. 保持用户原始意图不变，只做专业化表达
4. 输出为一段连贯的中文提示词，不要分点，不要解释
5. 控制在200字以内`,

	"interior": `你是一位专业的室内设计AI提示词优化专家。用户会输入一段关于室内设计的描述，请将其优化为结构化的AI绘图提示词。
优化规则：
1. 提取并明确：空间类型、设计风格、主要材质、色调
2. 补充：家具布局、灯光氛围、软装细节、空间比例
3. 保持用户原始意图不变，只做专业化表达
4. 输出为一段连贯的中文提示词，不要分点，不要解释
5. 控制在200字以内`,

	"landscape": `你是一位专业的景观设计AI提示词优化专家。用户会输入一段关于景观设计的描述，请将其优化为结构化的AI绘图提示词。
优化规则：
1. 提取并明确：景观类型、植物种类、铺装材质、水景元素
2. 补充：季节氛围、光影效果、空间层次、人的活动
3. 保持用户原始意图不变，只做专业化表达
4. 输出为一段连贯的中文提示词，不要分点，不要解释
5. 控制在200字以内`,

	"video": `你是一位专业的AI视频提示词优化专家。用户会输入一段关于视频画面的描述，请将其优化为结构化的AI视频生成提示词。
优化规则：
1. 提取并明确：场景空间、镜头运动方式（推/拉/摇/移）、画面主体
2. 补充：光影变化、时间流逝感、空间氛围、材质细节
3. 保持用户原始意图不变，只做专业化表达
4. 输出为一段连贯的中文提示词，不要分点，不要解释
5. 控制在200字以内`,
}

const defaultOptimizePrompt = `你是一位专业的AI绘图提示词优化专家。用户会输入一段描述，请将其优化为结构化的AI生成提示词。
优化规则：
1. 提取用户核心意图，明确主体、风格、氛围
2. 补充专业细节：材质、光影、视角、色调
3. 保持用户原始意图不变，只做专业化表达
4. 输出为一段连贯的中文提示词，不要分点，不要解释
5. 控制在200字以内`

func RegisterPromptOptimizeRoute(r *gin.RouterGroup, codeSessionModel *model.CodeSessionRedisModel, userMembershipModel *model.UserMembershipModel, cfg *config.Config) {
	r.POST("/ai/prompt/optimize", func(c *gin.Context) {
		handlePromptOptimize(c, codeSessionModel, userMembershipModel, cfg)
	})
}

func handlePromptOptimize(c *gin.Context, codeSessionModel *model.CodeSessionRedisModel, userMembershipModel *model.UserMembershipModel, cfg *config.Config) {
	var req PromptOptimizeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "参数错误: " + err.Error()})
		return
	}
	if strings.TrimSpace(req.RawPrompt) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "提示词内容不能为空"})
		return
	}

	codeSession := GetTokenCodeSession(c)
	if codeSession == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"code": 401, "msg": "未通过token验证"})
		return
	}

	// 会员校验
	if userMembershipModel != nil {
		_, err := userMembershipModel.GetActiveByUserID(codeSession.UserID)
		if err != nil {
			c.JSON(http.StatusForbidden, gin.H{"code": 403, "msg": "AI优化提示词为会员专属功能，请开通会员后使用"})
			return
		}
	}

	// 获取 API Key
	apiKey := cfg.AI.LaoZhangAPIKey
	if apiKey == "" {
		apiKey = cfg.AI.ArkAPIKey
	}
	if apiKey == "" {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "AI服务未配置"})
		return
	}

	// 选择场景 system prompt
	systemPrompt := defaultOptimizePrompt
	if sp, ok := sceneOptimizePrompts[req.Scene]; ok {
		systemPrompt = sp
	}

	// 构建 OpenAI 兼容请求（老张 API）
	messages := []map[string]string{
		{"role": "system", "content": systemPrompt},
		{"role": "user", "content": strings.TrimSpace(req.RawPrompt)},
	}
	body := map[string]interface{}{
		"model":    laozhangChatModel,
		"messages": messages,
		"stream":   false,
	}
	bodyJSON, err := json.Marshal(body)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "构建请求失败"})
		return
	}

	httpReq, err := http.NewRequest(http.MethodPost, laozhangChatBaseURL+"/chat/completions", bytes.NewReader(bodyJSON))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "创建请求失败"})
		return
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+apiKey)

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(httpReq)
	if err != nil {
		log.Printf("[PromptOptimize] API请求失败: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "AI服务请求失败"})
		return
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil || resp.StatusCode != http.StatusOK {
		log.Printf("[PromptOptimize] API返回错误: status=%d body=%s", resp.StatusCode, string(respBody))
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": fmt.Sprintf("AI服务返回错误: %d", resp.StatusCode)})
		return
	}

	// 解析 OpenAI 格式响应
	var result struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil || len(result.Choices) == 0 {
		log.Printf("[PromptOptimize] 解析响应失败: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "AI返回内容解析失败"})
		return
	}

	optimized := strings.TrimSpace(result.Choices[0].Message.Content)
	if optimized == "" {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "AI返回内容为空"})
		return
	}

	log.Printf("[PromptOptimize] 用户 %d 优化提示词成功, scene=%s", codeSession.UserID, req.Scene)
	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"msg":  "ok",
		"data": gin.H{"optimized_prompt": optimized},
	})
}
