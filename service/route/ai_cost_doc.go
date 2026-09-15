package route

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path"
	"regexp"
	"strings"
	"time"

	"service/component"
	"service/config"
	"service/function"
	"service/model"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/xuri/excelize/v2"
)

const (
	costDocCOSPrefix   = "assets/cost/" // 造价表目录，与用户约定一致
	costDocResultDir   = "cost_result/" // 生成 Excel 存放目录（会再拼 config prefix）
	costDocModel       = "doubao-seed-2-0-pro-260215"
	costDocMaxFiles    = 20
	costDocMaxFileSize = 15 << 20 // 单文件 15MB，Base64 后约 20MB
	costDocArkURL      = "https://ark.cn-beijing.volces.com/api/v3"
)

var costDocSectionTitles = []string{
	"## 建筑基础",
	"## 建筑主体",
	"## 外观装修",
	"## 室内装修",
	"## 花园庭院",
}

// AICostDocRequest 根据造价表生成测算表请求
type AICostDocRequest struct {
	City          string  `json:"city"`            // 别墅所在城市
	Width         float64 `json:"width"`           // 房屋面宽（米）
	Depth         float64 `json:"depth"`           // 进深（米）
	AreaBuild     float64 `json:"area_build"`      // 占地（平方米）
	Floors        int     `json:"floors"`          // 层数
	AreaGarden    float64 `json:"area_garden"`     // 花园面积（平方米）
	AreaCourtyard float64 `json:"area_courtyard"`  // 扣除庭院面积（平方米）
	AreaBalcony   float64 `json:"area_balcony"`    // 露台扣除面积（平方米）
	StructureType string  `json:"structure_type"` // 结构形式
	RoofType      string  `json:"roof_type"`      // 屋顶形式
	HasBasement   string  `json:"has_basement"`    // 是否有地下室（无/一层/二层等）
}

// AICostDocResponse 返回 task_no 与 excel 下载链接
type AICostDocResponse struct {
	TaskNo   string `json:"task_no"`
	ExcelURL string `json:"excel_url"`
}

// RegisterAICostDocRoute 注册 AI 造价文档路由（需在 RegisterAICostRoutes 中传入 cfg 并调用）
func RegisterAICostDocRoute(r *gin.RouterGroup, codeSessionModel *model.CodeSessionRedisModel, userModel *model.UserRedisModel, pricingModel *model.AIPricingModel, taskModel *model.AITaskModel, stoneRecordModel *model.StoneRecordModel, userOrderModel *model.UserOrderModel, inviteRelationModel *model.InviteRelationModel, cfg *config.Config) {
	r.POST("/ai/cost/doc", func(c *gin.Context) {
		handleAICostDoc(c, codeSessionModel, userModel, pricingModel, taskModel, stoneRecordModel, userOrderModel, inviteRelationModel, cfg)
	})
}

func handleAICostDoc(c *gin.Context, codeSessionModel *model.CodeSessionRedisModel, userModel *model.UserRedisModel, pricingModel *model.AIPricingModel, taskModel *model.AITaskModel, stoneRecordModel *model.StoneRecordModel, userOrderModel *model.UserOrderModel, inviteRelationModel *model.InviteRelationModel, cfg *config.Config) {
	codeSession := GetTokenCodeSession(c)
	if codeSession == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"code": 401, "msg": "未通过token验证"})
		return
	}

	var req AICostDocRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "参数错误: " + err.Error()})
		return
	}

	// 计费（可选）
	pricing, _ := pricingModel.GetByScene("ai_cost_doc")
	if pricing != nil && pricing.Stones > 0 {
		current, err := userModel.GetStones(codeSession.UserID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "查询余额失败"})
			return
		}
		if current < pricing.Stones {
			c.JSON(http.StatusPaymentRequired, gin.H{"code": 402, "msg": "余额不足", "data": gin.H{"required": pricing.Stones, "current": current}})
			return
		}
		if err := userModel.DeductStones(codeSession.UserID, pricing.Stones); err != nil {
			c.JSON(http.StatusPaymentRequired, gin.H{"code": 402, "msg": "扣费失败"})
			return
		}
	}

	taskNo := "cost_" + uuid.New().String()[:8]
	requestPayload := buildCostDocRequestPayload(req)
	task := &model.AITask{
		TaskNo:         taskNo,
		UserID:         codeSession.UserID,
		Scene:          "ai_cost_doc",
		RequestPayload: requestPayload,
		// processing：避免被通用 AITaskProcessor 当作绘画任务领取
		Status:     "processing",
		StonesUsed: 0,
	}
	if err := taskModel.Create(task); err != nil {
		if pricing != nil && pricing.Stones > 0 {
			_ = userModel.AddStones(codeSession.UserID, pricing.Stones)
		}
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "创建任务失败"})
		return
	}

	// 异步执行：先返回 task_no，结果通过生成历史查看
	go func() {
		ctx := context.Background()
		excelURL, err := runCostDocPipeline(ctx, cfg, taskNo, &req, codeSession.UserID)
		task, _ := taskModel.GetByTaskNo(taskNo)
		if task == nil {
			return
		}
		if err != nil {
			_ = taskModel.UpdateStatusByTaskNo(taskNo, "failed", "", err.Error())
			if pricing != nil && pricing.Stones > 0 {
				_ = userModel.AddStones(codeSession.UserID, pricing.Stones)
			}
			return
		}
		resultPayload := fmt.Sprintf(`{"excel_url":"%s"}`, excelURL)
		_ = taskModel.UpdateStatusByTaskNo(taskNo, "success", resultPayload, "")
	}()

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"msg":  "任务已提交",
		"data": AICostDocResponse{TaskNo: taskNo, ExcelURL: ""},
	})
}

func buildCostDocRequestPayload(req AICostDocRequest) string {
	b, _ := json.Marshal(req)
	return string(b)
}

// runCostDocPipeline 拉取 COS 造价表 → 调多模态 → 解析表格 → 转 Excel 上传 COS，返回 excel 链接
func runCostDocPipeline(ctx context.Context, cfg *config.Config, taskNo string, req *AICostDocRequest, userID int64) (excelURL string, err error) {
	client := component.GetCOSClient()
	if client == nil {
		return "", fmt.Errorf("COS 未初始化")
	}

	keys, err := function.ListCOSKeys(ctx, client, costDocCOSPrefix, costDocMaxFiles)
	if err != nil {
		return "", fmt.Errorf("列举造价表失败: %w", err)
	}
	if len(keys) == 0 {
		return "", fmt.Errorf("未找到造价表文件，请检查COS路径")
	}

	// 多模态文档理解接口仅支持 PDF，只收集 .pdf 文件
	var fileContents []struct {
		data     []byte
		filename string
	}
	for _, key := range keys {
		name := path.Base(key)
		if !strings.HasSuffix(strings.ToLower(name), ".pdf") {
			log.Printf("[CostDoc] 跳过非 PDF 文件: %s（多模态接口仅支持 PDF）", name)
			continue
		}
		data, err := function.GetCOSObject(ctx, client, key, costDocMaxFileSize)
		if err != nil {
			log.Printf("[CostDoc] 下载 %s 失败: %v", key, err)
			continue
		}
		fileContents = append(fileContents, struct {
			data     []byte
			filename string
		}{data, name})
	}
	if len(fileContents) == 0 {
		return "", fmt.Errorf("未找到 PDF 造价表文件，请将 PDF 上传至 COS 路径 assets/cost/")
	}

	apiKey := cfg.AI.ArkAPIKey
	if apiKey == "" {
		apiKey = os.Getenv("ARK_API_KEY")
	}
	if apiKey == "" {
		return "", fmt.Errorf("未配置 ARK_API_KEY")
	}

	city := req.City
	if city == "" {
		city = "未填写"
	}
	structureType := req.StructureType
	if structureType == "" {
		structureType = "未填写"
	}
	roofType := req.RoofType
	if roofType == "" {
		roofType = "未填写"
	}
	hasBasement := req.HasBasement
	if hasBasement == "" {
		hasBasement = "无"
	}
	prompt := fmt.Sprintf(`别墅所在城市为%s，房屋面宽为%.1f米，进深为%.1f米，占地%.0f平方米，共%d层楼，花园面积为%.0f平方米，扣除庭院面积为%.0f平方米，露台扣除面积为%.0f平方米，结构形式为%s，屋顶形式为%s，是否有地下室为%s。请根据上传的公司造价表格，生成一份详细的别墅施工造价测算详表，要求：

1. 严格按照以下5个部分拆分，每个部分作为独立章节：
- 建筑基础
- 建筑主体
- 外观装修
- 室内装修
- 花园庭院
2. 每个部分内部，需穷尽造价表中对应类别的所有子项，不得遗漏任何条目。
3. 每个子项必须包含：项目名称、单位、工程量、单价（元）、合价（元）、备注 6列，所有数据均来自上传的造价表，并结合上述所有建房参数进行精准测算。
4. 每个部分最后需增加一行"小计"，汇总该部分所有子项的合价。
5. 最终输出仅为标准Markdown表格，不生成任何多余的解释性文字、说明或总结。`,
		city, req.Width, req.Depth, req.AreaBuild, req.Floors, req.AreaGarden, req.AreaCourtyard, req.AreaBalcony, structureType, roofType, hasBasement)
	systemHint := "你只能输出标准的 Markdown 表格。每个部分一个二级标题（## 建筑基础、## 建筑主体、## 外观装修、## 室内装修、## 花园庭院），标题下紧跟一个 Markdown 表格，表头为：项目名称、单位、工程量、单价（元）、合价（元）、备注；每个部分最后一行為小计。不要输出任何解释、说明或其它文字。"

	contentList := make([]map[string]interface{}, 0)
	for _, f := range fileContents {
		contentList = append(contentList, map[string]interface{}{
			"type":      "input_file",
			"file_data": "data:application/pdf;base64," + base64.StdEncoding.EncodeToString(f.data),
			"filename":  f.filename,
		})
	}
	contentList = append(contentList, map[string]interface{}{
		"type": "input_text",
		"text":  systemHint + "\n\n" + prompt,
	})

	body := map[string]interface{}{
		"model": costDocModel,
		"input": []map[string]interface{}{
			{"role": "user", "content": contentList},
		},
	}
	reqBody, _ := json.Marshal(body)
	httpReq, err := http.NewRequestWithContext(ctx, "POST", costDocArkURL+"/responses", bytes.NewReader(reqBody))
	if err != nil {
		return "", err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+apiKey)
	httpClient := &http.Client{Timeout: 300 * time.Second}
	resp, err := httpClient.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("调用多模态 API 失败: %w", err)
	}
	defer resp.Body.Close()
	respBytes, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("多模态 API 返回错误: %d, %s", resp.StatusCode, string(respBytes))
	}

	outputText := extractOutputTextFromResponses(respBytes)
	if strings.TrimSpace(outputText) == "" {
		return "", fmt.Errorf("生成的表格格式不正确，请重试")
	}

	sections := parseMarkdownSections(outputText)
	if len(sections) == 0 {
		return "", fmt.Errorf("生成的表格格式不正确，请重试")
	}

	f := excelize.NewFile()
	defer f.Close()
	sheetIndex := 0
	for _, title := range costDocSectionTitles {
		name := strings.TrimPrefix(title, "## ")
		table := sections[name]
		if len(table) == 0 {
			continue
		}
		if sheetIndex == 0 {
			_ = f.SetSheetName("Sheet1", name)
		} else {
			_, _ = f.NewSheet(name)
		}
		for rowIdx, row := range table {
			for colIdx, cell := range row {
				cellRef, _ := excelize.CoordinatesToCellName(colIdx+1, rowIdx+1)
				_ = f.SetCellValue(name, cellRef, cell)
			}
		}
		sheetIndex++
	}
	if sheetIndex == 0 {
		return "", fmt.Errorf("生成的表格格式不正确，请重试")
	}

	buf := new(bytes.Buffer)
	if _, err := f.WriteTo(buf); err != nil {
		return "", err
	}
	excelURL, err = function.UploadBytes(ctx, nil, cfg, costDocResultDir+taskNo+".xlsx", buf.Bytes(), "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	if err != nil {
		return "", fmt.Errorf("上传 Excel 失败: %w", err)
	}
	return excelURL, nil
}

// extractOutputTextFromResponses 从火山引擎 Responses API 的 JSON 中提取输出文本
func extractOutputTextFromResponses(respBytes []byte) string {
	var raw map[string]json.RawMessage
	if json.Unmarshal(respBytes, &raw) != nil {
		return ""
	}
	out, ok := raw["output"]
	if !ok {
		if t, ok := raw["text"]; ok {
			var s string
			if json.Unmarshal(t, &s) == nil {
				return s
			}
		}
		return ""
	}
	// output 可能是数组: [{"type":"message","content":[{"type":"output_text","text":"..."}]}]
	var list []struct {
		Type    string `json:"type"`
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if json.Unmarshal(out, &list) == nil && len(list) > 0 {
		var sb strings.Builder
		for i := range list {
			for _, c := range list[i].Content {
				if c.Type == "output_text" || c.Text != "" {
					sb.WriteString(c.Text)
				}
			}
		}
		return sb.String()
	}
	// output 可能是对象: {"text": "..."}
	var obj struct {
		Text string `json:"text"`
	}
	if json.Unmarshal(out, &obj) == nil && obj.Text != "" {
		return obj.Text
	}
	return ""
}

// parseMarkdownSections 按 ## 标题切分，并解析每个标题下的 Markdown 表格为二维数组（标题名 -> 行数据）
func parseMarkdownSections(md string) map[string][][]string {
	out := make(map[string][][]string)
	// 统一换行
	md = strings.ReplaceAll(md, "\r\n", "\n")
	blocks := regexp.MustCompile(`(?m)^##\s+(.+)$`).Split(md, -1)
	if len(blocks) < 2 {
		return out
	}
	// 第一个 block 是标题前的文字，从第二个起每个 block 对应一个标题后的内容；需要和标题对应
	titles := regexp.MustCompile(`(?m)^##\s+(.+)$`).FindAllStringSubmatch(md, -1)
	for i, sub := range titles {
		if len(sub) < 2 {
			continue
		}
		title := strings.TrimSpace(sub[1])
		var content string
		if i+1 < len(blocks) {
			content = blocks[i+1]
		} else {
			content = ""
		}
		table := parseMarkdownTable(content)
		if len(table) > 0 {
			out[title] = table
		}
	}
	return out
}

func parseMarkdownTable(block string) [][]string {
	var rows [][]string
	lines := strings.Split(block, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if !strings.HasPrefix(line, "|") {
			continue
		}
		// 跳过分隔行 |---|---|
		if regexp.MustCompile(`^\|[\s\-:]+\|`).MatchString(line) {
			continue
		}
		cells := strings.Split(line, "|")
		// 去掉首尾空元素
		var row []string
		for _, c := range cells {
			c = strings.TrimSpace(c)
			row = append(row, c)
		}
		if len(row) > 0 && (row[0] != "" || len(row) > 1) {
			rows = append(rows, row)
		}
	}
	return rows
}
