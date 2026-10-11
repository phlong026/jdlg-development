package report

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const snapshotSchemaVersion = "report-a4-v1"

var fixedMissingInformation = []string{
	"退界与实际可建范围",
	"道路及相邻建筑方位",
	"现场地质与高差",
	"当地用地、层数及审批条件",
}

var allowedFamilyTypes = stringSet("三代同堂", "夫妻自住", "两代同住", "返乡度假")
var allowedStyles = stringSet("新闽派", "新中式", "现代简约", "欧式")
var allowedBudgetScopes = stringSet("主体毛坯", "主体及外装", "含室内装修")
var allowedPriorities = stringSet("老人起居", "家庭团聚", "采光通风", "庭院生活", "外观体面", "预算控制")

type Generator struct {
	textClient TextClient
	now        func() time.Time
}

func NewGenerator(textClient TextClient) *Generator {
	return &Generator{textClient: textClient, now: time.Now}
}

func NormalizeAndValidateInput(input Input) (Input, error) {
	input.Site.Region = collapseInputWhitespace(input.Site.Region)
	input.Site.RoadSide = strings.TrimSpace(input.Site.RoadSide)
	input.Household.FamilyType = strings.TrimSpace(input.Household.FamilyType)
	input.Household.Residents = collapseInputWhitespace(input.Household.Residents)
	input.Preference.Style = strings.TrimSpace(input.Preference.Style)
	input.Preference.Goal = collapseInputWhitespace(input.Preference.Goal)
	input.Budget.Scope = strings.TrimSpace(input.Budget.Scope)
	input.Preference.Priorities = append([]string(nil), input.Preference.Priorities...)
	for index := range input.Preference.Priorities {
		input.Preference.Priorities[index] = strings.TrimSpace(input.Preference.Priorities[index])
	}
	for _, value := range []string{input.Site.Region, input.Site.RoadSide, input.Household.FamilyType, input.Household.Residents, input.Preference.Style, input.Preference.Goal, input.Budget.Scope} {
		if containsControl(value) {
			return Input{}, ErrInvalidInput
		}
	}

	if !boundedText(input.Site.Region, 2, 32) || !boundedNumber(input.Site.WidthM, 4, 50) || !boundedNumber(input.Site.DepthM, 4, 80) {
		return Input{}, ErrInvalidInput
	}
	if input.Site.RoadSide != "" && !boundedText(input.Site.RoadSide, 1, 20) {
		return Input{}, ErrInvalidInput
	}
	if !allowedFamilyTypes[input.Household.FamilyType] || !boundedText(input.Household.Residents, 2, 80) {
		return Input{}, ErrInvalidInput
	}
	if input.Space.Floors < 1 || input.Space.Floors > 3 || input.Space.Bedrooms < 1 || input.Space.Bedrooms > 12 {
		return Input{}, ErrInvalidInput
	}
	if !allowedStyles[input.Preference.Style] || !boundedText(input.Preference.Goal, 2, 80) {
		return Input{}, ErrInvalidInput
	}
	if len(input.Preference.Priorities) != 3 {
		return Input{}, ErrInvalidInput
	}
	seen := make(map[string]struct{}, len(input.Preference.Priorities))
	for _, priority := range input.Preference.Priorities {
		if !allowedPriorities[priority] {
			return Input{}, ErrInvalidInput
		}
		if _, duplicate := seen[priority]; duplicate {
			return Input{}, ErrInvalidInput
		}
		seen[priority] = struct{}{}
	}
	if !boundedNumber(input.Budget.Wan, 10, 1000) || !allowedBudgetScopes[input.Budget.Scope] {
		return Input{}, ErrInvalidInput
	}
	return input, nil
}

func collapseInputWhitespace(value string) string {
	return strings.Join(strings.Fields(value), " ")
}

func containsControl(value string) bool {
	for _, char := range value {
		if unicode.IsControl(char) {
			return true
		}
	}
	return false
}

func MarshalInput(input Input) (json.RawMessage, string, error) {
	normalized, err := NormalizeAndValidateInput(input)
	if err != nil {
		return nil, "", err
	}
	data, err := json.Marshal(normalized)
	if err != nil {
		return nil, "", ErrInvalidInput
	}
	digest := sha256.Sum256(data)
	return data, hex.EncodeToString(digest[:]), nil
}

func (g *Generator) BuildPreview(ctx context.Context, record *Report) (json.RawMessage, string, error) {
	input, facts, err := inputAndFacts(record)
	if err != nil {
		return nil, "", err
	}
	mode := GenerationRulesOnly
	text := ruleText(facts)
	if g.textClient != nil {
		generated, generateErr := g.textClient.Generate(ctx, TextRequest{Facts: facts})
		if generateErr == nil && validTextResult(generated) {
			text = generated
			mode = GenerationAIAssisted
		} else {
			log.Printf("[report] report_id=%s stage=preview llm_fallback=rules_only", record.ID)
		}
	}
	priorities := make([]ContentBlock, 0, 3)
	for index := 0; index < 3; index++ {
		title := "待确认的次要目标"
		if index < len(input.Preference.Priorities) {
			title = input.Preference.Priorities[index]
		}
		priorities = append(priorities, ContentBlock{
			Title: title,
			Body:  priorityExplanation(index, title),
		})
	}

	snapshot := Snapshot{
		Meta: SnapshotMeta{
			SchemaVersion:   snapshotSchemaVersion,
			ProtocolVersion: record.ProtocolVersion,
			Source:          "report-service-v1",
			ReportID:        record.ID,
			InputHash:       record.InputHash,
			Date:            g.now().Format("2006年01月02日"),
			Demonstration:   false,
			GenerationMode:  mode,
		},
		Facts: facts,
		Pages: []SnapshotPage{
			{Number: 1, Access: "free", Kind: "cover", Title: "我家的建房规划报告", Subtitle: "从家庭需要到三种概念选择，为下一步专业沟通准备一份清晰底稿。"},
			{Number: 2, Access: "free", Kind: "evidence", Title: "报告依据与信息完整度", Subtitle: "先说明已知条件，再说明哪些仍需现场确认。", Blocks: []ContentBlock{
				{Title: "现在可以先讨论什么", Body: "先用家庭目标比较空间取舍，再带着明确问题核实宅基地条件。"},
				{Title: "这份报告的边界", Body: "用于建房前期沟通，不代替测绘、规划审批、建筑施工图、结构计算和工程报价。"},
			}},
			{Number: 3, Access: "free", Kind: "profile", Title: "家庭与宅基地画像", Subtitle: "把平时的日常与团聚时的需求放在同一张图里。", Blocks: []ContentBlock{
				{Title: "家庭生活画像", Body: text.ProfileInsight},
				{Title: "地块与需求画像", Body: fmt.Sprintf("当前输入为 %.1f × %.1f 米、%d 层、%d 间卧室，风格偏好为%s。尺寸和可建范围仍需核实。", facts.WidthM, facts.DepthM, facts.Floors, facts.Bedrooms, facts.Style)},
			}},
			{Number: 4, Access: "free", Kind: "priorities", Title: "核心矛盾与设计目标", Subtitle: "不是把每个愿望同时放大，而是先确定最重要的选择。", Priorities: priorities, Blocks: []ContentBlock{
				{Title: "当前最值得先解决的矛盾", Body: text.CoreTradeoff},
				{Title: "下一步设计目标", Body: text.Recommendation},
			}},
		},
	}
	return marshalSnapshot(snapshot)
}

func (g *Generator) BuildFull(preview json.RawMessage, pack ReviewedPack) (json.RawMessage, error) {
	var snapshot Snapshot
	if err := json.Unmarshal(preview, &snapshot); err != nil || len(snapshot.Pages) != 4 || snapshot.Meta.Source != "report-service-v1" {
		return nil, ErrInvalidInput
	}
	if err := ValidateReviewedPack(pack); err != nil {
		return nil, err
	}
	if !packSupportsFacts(pack, snapshot.Facts) {
		return nil, ErrPackUnavailable
	}
	snapshot.Meta.PackID = pack.ID
	snapshot.Meta.PackVersion = pack.Version
	snapshot.Assets = cloneStringMap(pack.Assets)
	snapshot.Budget = &SnapshotBudget{Allocations: append([]BudgetAllocation(nil), pack.BudgetTemplate...)}
	snapshot.Schemes = make([]SnapshotScheme, 0, 3)
	for _, scheme := range pack.Schemes {
		converted := SnapshotScheme{
			ID: scheme.ID, Name: scheme.Name, Tagline: scheme.Tagline,
			Tradeoff: scheme.Tradeoff, Gain: scheme.Gain, Facade: scheme.Facade,
			FacadeCaution: scheme.FacadeCaution, FacadeAsset: scheme.FacadeAssetID,
			VolumeAsset: scheme.VolumeAssetID, ReviewStatus: pack.ReviewStatus,
			Bedrooms: scheme.Bedrooms, FirstFloorElderRoom: scheme.FirstFloorElderRoom,
			PriorityFit: append([]string(nil), scheme.PriorityFit...), CostSensitive: append([]string(nil), scheme.CostSensitive...),
		}
		for _, plan := range scheme.Plans {
			converted.PlanAssets = append(converted.PlanAssets, plan.AssetID)
			converted.Floors = append(converted.Floors, SnapshotFloor{Number: plan.Floor, Name: plan.Name, Summary: plan.Summary, AssetID: plan.AssetID})
		}
		snapshot.Schemes = append(snapshot.Schemes, converted)
	}
	snapshot.Pages = append(snapshot.Pages, paidPages(snapshot.Facts, pack)...)
	data, _, err := marshalSnapshot(snapshot)
	return data, err
}

func packSupportsFacts(pack ReviewedPack, facts SnapshotFacts) bool {
	if pack.Floors != facts.Floors || pack.BudgetScope != facts.BudgetScope ||
		facts.WidthM < pack.MinWidthM || facts.WidthM > pack.MaxWidthM ||
		facts.DepthM < pack.MinDepthM || facts.DepthM > pack.MaxDepthM {
		return false
	}
	styleMatched := false
	for _, style := range pack.Styles {
		if style == facts.Style {
			styleMatched = true
			break
		}
	}
	if !styleMatched {
		return false
	}
	for _, scheme := range pack.Schemes {
		if scheme.Bedrooms != facts.Bedrooms || facts.FirstFloorElderRoom && !scheme.FirstFloorElderRoom {
			return false
		}
	}
	return true
}

func inputAndFacts(record *Report) (Input, SnapshotFacts, error) {
	if record == nil || record.ID == "" || record.ProtocolVersion == "" || record.InputHash == "" {
		return Input{}, SnapshotFacts{}, ErrInvalidInput
	}
	var input Input
	if err := json.Unmarshal(record.InputJSON, &input); err != nil {
		return Input{}, SnapshotFacts{}, ErrInvalidInput
	}
	normalized, err := NormalizeAndValidateInput(input)
	if err != nil {
		return Input{}, SnapshotFacts{}, ErrInvalidInput
	}
	_, hash, err := MarshalInput(normalized)
	if err != nil || hash != record.InputHash {
		return Input{}, SnapshotFacts{}, ErrInvalidInput
	}
	return normalized, factsFromInput(normalized), nil
}

func factsFromInput(input Input) SnapshotFacts {
	return SnapshotFacts{
		Region: input.Site.Region, Family: input.Household.FamilyType, Residents: input.Household.Residents,
		WidthM: input.Site.WidthM, DepthM: input.Site.DepthM, Floors: input.Space.Floors,
		Bedrooms: input.Space.Bedrooms, FirstFloorElderRoom: input.Space.FirstFloorElderRoom,
		Style: input.Preference.Style, BudgetWan: input.Budget.Wan,
		BudgetScope: input.Budget.Scope, Priorities: append([]string(nil), input.Preference.Priorities...),
		Goal: input.Preference.Goal, Missing: append([]string(nil), fixedMissingInformation...),
	}
}

func ruleText(facts SnapshotFacts) TextResult {
	first := facts.Priorities[0]
	return TextResult{
		ProfileInsight: fmt.Sprintf("这房子要同时服务于%s。先根据实际居住频率安排首层日常，再处理团聚时的公共空间。", facts.Residents),
		CoreTradeoff:   fmt.Sprintf("当前第一优先级是%s。在地块和预算尚未核实前，需要在房间数、公共空间和外观投入之间保留取舍余地。", first),
		Recommendation: fmt.Sprintf("先以%s为判断标准，将%d层、%d间卧室和%.0f万元预算口径带入方案比较，再根据现场条件调整。", first, facts.Floors, facts.Bedrooms, facts.BudgetWan),
	}
}

func paidPages(facts SnapshotFacts, pack ReviewedPack) []SnapshotPage {
	names := make(map[string]string, 3)
	for _, scheme := range pack.Schemes {
		names[scheme.ID] = scheme.Name
	}
	pages := []SnapshotPage{{Number: 5, Access: "paid", Kind: "overview", Title: "三种概念方案总览", Subtitle: "在相同家庭需求下，比较三种不同的空间重心。", Blocks: []ContentBlock{{Title: "建议的比较方法", Body: "先比较日常使用和优先级，再比较外观和投入差异；所有方案仍需现场与专业深化。"}}}}
	for index, id := range []string{"A", "B", "C"} {
		base := 6 + index*2
		pages = append(pages,
			SnapshotPage{Number: base, Access: "paid", Kind: "plans", Scheme: id, Title: fmt.Sprintf("方案 %s｜一至%d层概念平面", id, facts.Floors), Subtitle: "平面图来自同一审核方案包，仅用于前期沟通。"},
			SnapshotPage{Number: base + 1, Access: "paid", Kind: "elevation", Scheme: id, Title: fmt.Sprintf("方案 %s｜正立面与体量", id), Subtitle: "立面与本方案平面成套，不是独立风格参考图。"},
		)
	}
	comparison := [][]string{
		{"比较重点", "A " + names["A"], "B " + names["B"], "C " + names["C"]},
		{"卧室配置", bedroomLabel(pack.Schemes[0]), bedroomLabel(pack.Schemes[1]), bedroomLabel(pack.Schemes[2])},
		{"首层长辈房", elderRoomLabel(pack.Schemes[0]), elderRoomLabel(pack.Schemes[1]), elderRoomLabel(pack.Schemes[2])},
		{"优先级适配", strings.Join(pack.Schemes[0].PriorityFit, " / "), strings.Join(pack.Schemes[1].PriorityFit, " / "), strings.Join(pack.Schemes[2].PriorityFit, " / ")},
		{"主要获得", pack.Schemes[0].Gain, pack.Schemes[1].Gain, pack.Schemes[2].Gain},
		{"主要取舍", pack.Schemes[0].Tradeoff, pack.Schemes[1].Tradeoff, pack.Schemes[2].Tradeoff},
		{"成本敏感项", strings.Join(pack.Schemes[0].CostSensitive, " / "), strings.Join(pack.Schemes[1].CostSensitive, " / "), strings.Join(pack.Schemes[2].CostSensitive, " / ")},
	}
	pages = append(pages,
		SnapshotPage{Number: 12, Access: "paid", Kind: "comparison", Title: "三方案比较与推荐结论", Subtitle: "按已确认的优先级比较，不把概念方案当作最终定案。", Table: comparison, Blocks: []ContentBlock{{Title: "推荐方向", Body: rankedRecommendation(facts.Priorities, pack.Schemes)}}, Note: "比较依据为问卷事实与审核方案包，不包含工程报价结论。"},
		SnapshotPage{Number: 13, Access: "paid", Kind: "budget", Title: "预算配置与关键风险", Subtitle: "先分清预算要覆盖什么，再讨论外观投入。", Note: fmt.Sprintf("用户输入预算为 %.0f 万元，口径：%s。配置比例是前期沟通参考，不是报价。", facts.BudgetWan, facts.BudgetScope), Blocks: []ContentBlock{{Title: "成本敏感项", Body: "结构、外墙、门窗、机电和场地条件都可能改变投入，需在深化后重新核算。"}, {Title: "关键风险", Body: "未实测的可建范围、地质和审批条件是当前最需先排除的风险。"}}},
		SnapshotPage{Number: 14, Access: "paid", Kind: "actions", Title: "下一步行动与顾问承接", Subtitle: "带着明确问题补资料，再进入专业深化。", Actions: fixedActions(), Blocks: []ContentBlock{{Title: "与顾问沟通时请一并提供", Body: "报告编号、首选方案及理由、最想保留的空间，以及已补齐的地块资料。"}}, Disclaimer: "本报告为建房前期规划参考，不代替测绘、规划审批、建筑与结构施工图、工程量清单和正式报价。"},
	)
	return pages
}

func bedroomLabel(scheme PackScheme) string {
	return fmt.Sprintf("%d卧室", scheme.Bedrooms)
}

func elderRoomLabel(scheme PackScheme) string {
	if scheme.FirstFloorElderRoom {
		return "已配置"
	}
	return "未配置"
}

func rankedRecommendation(priorities []string, schemes []PackScheme) string {
	bestScore := -1
	best := make([]string, 0, 3)
	for _, scheme := range schemes {
		score := priorityScore(priorities, scheme.PriorityFit)
		if score > bestScore {
			bestScore = score
			best = []string{scheme.ID}
		} else if score == bestScore {
			best = append(best, scheme.ID)
		}
	}
	if len(best) == 1 {
		return fmt.Sprintf("按您的三项优先级加权比较，方案 %s 当前匹配度最高；仍需核实现场条件后再深化。", best[0])
	}
	return fmt.Sprintf("按您的三项优先级加权比较，方案 %s 当前并列；请再根据现场条件和取舍选择。", strings.Join(best, "、"))
}

func priorityScore(priorities, supported []string) int {
	weights := []int{3, 2, 1}
	supportSet := make(map[string]struct{}, len(supported))
	for _, priority := range supported {
		supportSet[priority] = struct{}{}
	}
	score := 0
	for index, priority := range priorities {
		if index >= len(weights) {
			break
		}
		if _, ok := supportSet[priority]; ok {
			score += weights[index]
		}
	}
	return score
}

func fixedActions() []ContentBlock {
	return []ContentBlock{
		{Title: "核实宅基地红线与退界", Body: "取得可用的测量或审批资料。"},
		{Title: "确认道路与相邻条件", Body: "补充出入口、采光和隐私条件。"},
		{Title: "补充场地高差与地质信息", Body: "避免平面与基础判断失真。"},
		{Title: "选定一个主方案", Body: "说明选择理由和必须保留的空间。"},
		{Title: "确认预算包含范围", Body: "把主体、外装、内装与院落分开讨论。"},
		{Title: "进入专业深化", Body: "由合格专业人员完成现场适配与施工图。"},
	}
}

func marshalSnapshot(snapshot Snapshot) (json.RawMessage, string, error) {
	data, err := json.Marshal(snapshot)
	if err != nil {
		return nil, "", err
	}
	return data, snapshot.Meta.GenerationMode, nil
}

func priorityExplanation(index int, title string) string {
	if index == 0 {
		return "作为方案比较的第一判断标准：" + title + "。"
	}
	return "在不破坏第一优先级的前提下，再评估：" + title + "。"
}

func validTextResult(value TextResult) bool {
	return safeAIText(value.ProfileInsight) && safeAIText(value.CoreTradeoff) && safeAIText(value.Recommendation)
}

func safeAIText(value string) bool {
	if !boundedText(value, 2, 120) || strings.ContainsAny(value, "\r\n") {
		return false
	}
	for _, char := range value {
		if unicode.IsControl(char) || unicode.IsNumber(char) {
			return false
		}
	}
	for _, claim := range []string{"保证", "一定", "确定可建", "符合审批", "准确造价"} {
		if strings.Contains(value, claim) {
			return false
		}
	}
	return true
}

func boundedText(value string, minRunes, maxRunes int) bool {
	count := utf8.RuneCountInString(value)
	return count >= minRunes && count <= maxRunes
}

func boundedNumber(value, minimum, maximum float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0) && value >= minimum && value <= maximum
}

func cloneStringMap(source map[string]string) map[string]string {
	target := make(map[string]string, len(source))
	for key, value := range source {
		target[key] = value
	}
	return target
}

func stringSet(values ...string) map[string]bool {
	set := make(map[string]bool, len(values))
	for _, value := range values {
		set[value] = true
	}
	return set
}
