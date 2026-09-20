package report

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

const (
	ProtocolDecisionV5      = "report-decision-v5"
	SchemaDecisionV5        = "report-decision-v5"
	DecisionTemplateVersion = "full-10-v1"
	DecisionPreviewVersion  = "preview-4-v1"
	DecisionSourceV5        = "report-orchestrator-v5"

	GoalSpace     = "space"
	GoalBudget    = "budget"
	GoalFamily    = "family"
	GoalReadiness = "readiness"

	BuildableScopeKnown   = "known"
	BuildableScopeUnknown = "unknown"

	FrequencyNone      = "none"
	FrequencyMonthly   = "monthly"
	FrequencyQuarterly = "quarterly"
	FrequencyHolidays  = "holidays"
	FrequencyYearly    = "yearly"
	FrequencyUnknown   = "unknown"

	FlexibleUseYes     = "yes"
	FlexibleUseNo      = "no"
	FlexibleUseUnknown = "unknown"

	ScopeIncluded = "included"
	ScopeExcluded = "excluded"
	ScopeUnknown  = "unknown"

	PreparationExisting = "existing"
	PreparationMissing  = "missing"
	PreparationUnknown  = "unknown"

	StageIdea            = "idea"
	StageInformation     = "information"
	StageDesign          = "design"
	StageApproval        = "approval"
	StageBidding         = "bidding"
	StagePreconstruction = "preconstruction"
	StageUnknown         = "unknown"

	ChoiceOptionA   = "option_a"
	ChoiceOptionB   = "option_b"
	ChoiceConfirm   = "confirm"
	ChoiceUndecided = "undecided"
)

type DecisionInput struct {
	House                 *HouseRequirements  `json:"house,omitempty"`
	PrimaryGoal           string              `json:"primary_goal"`
	Region                string              `json:"region,omitempty"`
	ApproximateDimensions DecisionDimensions  `json:"approximate_dimensions,omitempty"`
	BuildableScope        string              `json:"buildable_scope"`
	BuildableConstraints  string              `json:"buildable_constraints,omitempty"`
	PermanentCount        *int                `json:"permanent_count,omitempty"`
	ReturningCount        *int                `json:"returning_count,omitempty"`
	ReturnFrequency       string              `json:"return_frequency"`
	Floors                *int                `json:"floors,omitempty"`
	Bedrooms              *int                `json:"bedrooms,omitempty"`
	FlexibleUse           string              `json:"flexible_use"`
	Priorities            []string            `json:"priorities"`
	BudgetWan             *float64            `json:"budget_wan,omitempty"`
	BudgetScope           []DecisionScopeItem `json:"budget_scope,omitempty"`
	FamilyPerspectives    []FamilyPerspective `json:"family_perspectives,omitempty"`
	PreparationStage      string              `json:"preparation_stage,omitempty"`
	PreparationItems      []PreparationItem   `json:"preparation_items,omitempty"`
}

type DecisionDimensions struct {
	WidthM *float64 `json:"width_m,omitempty"`
	DepthM *float64 `json:"depth_m,omitempty"`
}

type DecisionScopeItem struct {
	Item   string `json:"item"`
	Status string `json:"status"`
}

type FamilyPerspective struct {
	Role    string `json:"role"`
	Request string `json:"request"`
}

type PreparationItem struct {
	Item   string `json:"item"`
	Status string `json:"status"`
}

type DecisionConfirmation struct {
	DiagnosisID string   `json:"diagnosis_id"`
	InputHash   string   `json:"input_hash"`
	Choice      string   `json:"choice"`
	Priorities  []string `json:"priorities"`
}

type DecisionBrief struct {
	ID           string         `json:"id"`
	Recipe       string         `json:"recipe"`
	Topic        string         `json:"topic"`
	Reason       string         `json:"reason"`
	EvidenceRefs []string       `json:"evidence_refs"`
	Options      []ContentBlock `json:"options"`
	Unknowns     []string       `json:"unknowns"`
	Question     string         `json:"question"`
}

type DecisionSnapshot struct {
	House        *HouseContent         `json:"house,omitempty"`
	Meta         SnapshotMeta          `json:"meta"`
	Facts        map[string]string     `json:"facts"`
	Recipe       string                `json:"recipe"`
	Diagnosis    DecisionBrief         `json:"diagnosis"`
	Confirmation *DecisionConfirmation `json:"confirmation,omitempty"`
	Pages        []SnapshotPage        `json:"pages"`
}

var numericClaimPattern = regexp.MustCompile(`\d+(?:\.\d+)?\s*(?:平方米|㎡|万元|万|米)`)
var chineseMeasurementClaimPattern = regexp.MustCompile(`[零〇一二两三四五六七八九十百千万亿]+(?:点[零〇一二两三四五六七八九]+)?\s*(?:平方米|平米|㎡|万元|元|米)`)

func NormalizeDecisionInput(input DecisionInput) (DecisionInput, error) {
	input.PrimaryGoal = strings.TrimSpace(input.PrimaryGoal)
	input.Region = collapseInputWhitespace(input.Region)
	input.BuildableScope = strings.TrimSpace(input.BuildableScope)
	input.BuildableConstraints = collapseInputWhitespace(input.BuildableConstraints)
	input.ReturnFrequency = strings.TrimSpace(input.ReturnFrequency)
	input.FlexibleUse = strings.TrimSpace(input.FlexibleUse)
	input.PreparationStage = strings.TrimSpace(input.PreparationStage)

	if !oneOf(input.PrimaryGoal, GoalSpace, GoalBudget, GoalFamily, GoalReadiness) ||
		!oneOf(input.BuildableScope, BuildableScopeKnown, BuildableScopeUnknown) ||
		!oneOf(input.ReturnFrequency, FrequencyNone, FrequencyMonthly, FrequencyQuarterly, FrequencyHolidays, FrequencyYearly, FrequencyUnknown) ||
		!oneOf(input.FlexibleUse, FlexibleUseYes, FlexibleUseNo, FlexibleUseUnknown) {
		return DecisionInput{}, ErrInvalidInput
	}
	if input.Region != "" && !boundedCleanText(input.Region, 1, 40) {
		return DecisionInput{}, ErrInvalidInput
	}
	if input.BuildableConstraints != "" && !boundedCleanText(input.BuildableConstraints, 1, 240) {
		return DecisionInput{}, ErrInvalidInput
	}
	if input.BuildableScope == BuildableScopeKnown && input.BuildableConstraints == "" {
		return DecisionInput{}, ErrInvalidInput
	}
	if !validOptionalFloat(input.ApproximateDimensions.WidthM, 1, 100) || !validOptionalFloat(input.ApproximateDimensions.DepthM, 1, 150) ||
		!validOptionalInt(input.PermanentCount, 0, 30) || !validOptionalInt(input.ReturningCount, 0, 50) ||
		!validOptionalInt(input.Floors, 1, 6) || !validOptionalInt(input.Bedrooms, 1, 20) || !validOptionalFloat(input.BudgetWan, 1, 10000) {
		return DecisionInput{}, ErrInvalidInput
	}
	if (input.ApproximateDimensions.WidthM == nil) != (input.ApproximateDimensions.DepthM == nil) {
		return DecisionInput{}, ErrInvalidInput
	}
	if input.ReturnFrequency == FrequencyNone && input.ReturningCount != nil && *input.ReturningCount != 0 {
		return DecisionInput{}, ErrInvalidInput
	}

	var err error
	input.Priorities, err = normalizeUniqueTexts(input.Priorities, 1, 3, 2, 40)
	if err != nil {
		return DecisionInput{}, err
	}
	input.BudgetScope, err = normalizeScopeItems(input.BudgetScope)
	if err != nil {
		return DecisionInput{}, err
	}
	input.FamilyPerspectives, err = normalizePerspectives(input.FamilyPerspectives)
	if err != nil {
		return DecisionInput{}, err
	}
	input.PreparationItems, err = normalizePreparationItems(input.PreparationItems)
	if err != nil {
		return DecisionInput{}, err
	}

	switch input.PrimaryGoal {
	case GoalSpace:
		if input.PermanentCount == nil || input.ReturningCount == nil {
			return DecisionInput{}, ErrInvalidInput
		}
	case GoalBudget:
		if len(input.BudgetScope) == 0 {
			return DecisionInput{}, ErrInvalidInput
		}
	case GoalFamily:
		if len(input.FamilyPerspectives) == 0 {
			return DecisionInput{}, ErrInvalidInput
		}
	case GoalReadiness:
		if !oneOf(input.PreparationStage, StageIdea, StageInformation, StageDesign, StageApproval, StageBidding, StagePreconstruction, StageUnknown) || len(input.PreparationItems) == 0 {
			return DecisionInput{}, ErrInvalidInput
		}
	}
	unknowns := decisionUnknowns(input)
	if len(unknowns) > 0 && !boundedCleanText(strings.Join(unknowns, "；"), 1, 500) {
		return DecisionInput{}, ErrInvalidInput
	}
	if input.House != nil {
		input.House, err = normalizeHouseRequirements(input)
		if err != nil {
			return DecisionInput{}, err
		}
	}
	return input, nil
}

func MarshalDecisionInput(input DecisionInput) (json.RawMessage, string, error) {
	normalized, err := NormalizeDecisionInput(input)
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

func NormalizeDecisionConfirmation(input DecisionInput, confirmation DecisionConfirmation) (DecisionConfirmation, error) {
	normalized, err := NormalizeDecisionInput(input)
	if err != nil {
		return DecisionConfirmation{}, err
	}
	_, inputHash, err := MarshalDecisionInput(normalized)
	if err != nil {
		return DecisionConfirmation{}, err
	}
	confirmation.DiagnosisID = strings.TrimSpace(confirmation.DiagnosisID)
	confirmation.InputHash = strings.TrimSpace(confirmation.InputHash)
	confirmation.Choice = strings.TrimSpace(confirmation.Choice)
	choiceValid := oneOf(confirmation.Choice, ChoiceConfirm, ChoiceUndecided)
	if normalized.PrimaryGoal == GoalSpace && normalized.House == nil {
		choiceValid = oneOf(confirmation.Choice, ChoiceOptionA, ChoiceOptionB, ChoiceUndecided)
	}
	if confirmation.DiagnosisID != PlanDecision(normalized).ID || confirmation.InputHash != inputHash || !choiceValid {
		return DecisionConfirmation{}, ErrInvalidInput
	}
	if len(confirmation.Priorities) == 0 {
		confirmation.Priorities = append([]string(nil), normalized.Priorities...)
	}
	confirmation.Priorities, err = normalizeUniqueTexts(confirmation.Priorities, 1, 3, 2, 40)
	if err != nil {
		return DecisionConfirmation{}, err
	}
	if !sameStringSet(confirmation.Priorities, normalized.Priorities) {
		return DecisionConfirmation{}, ErrInvalidInput
	}
	return confirmation, nil
}

func BuildDecisionFacts(input DecisionInput) map[string]string {
	facts := map[string]string{
		"input.primary_goal":         input.PrimaryGoal,
		"site.region":                valueOrUnknown(input.Region),
		"site.buildable_scope":       input.BuildableScope,
		"site.buildable_constraints": valueOrUnknown(input.BuildableConstraints),
		"site.approx_dimensions":     "unknown",
		"household.permanent_count":  formatOptionalInt(input.PermanentCount),
		"household.returning_count":  formatOptionalInt(input.ReturningCount),
		"household.return_frequency": input.ReturnFrequency,
		"intent.floors":              formatOptionalInt(input.Floors),
		"intent.bedrooms":            formatOptionalInt(input.Bedrooms),
		"intent.flexible_use":        input.FlexibleUse,
		"intent.priorities":          strings.Join(input.Priorities, " / "),
		"budget.wan":                 formatOptionalFloat(input.BudgetWan),
		"readiness.stage":            valueOrUnknown(input.PreparationStage),
	}
	if input.ApproximateDimensions.WidthM != nil && input.ApproximateDimensions.DepthM != nil {
		facts["site.approx_dimensions"] = formatFloat(*input.ApproximateDimensions.WidthM) + "m x " + formatFloat(*input.ApproximateDimensions.DepthM) + "m (user estimate)"
	}
	for _, item := range input.BudgetScope {
		facts["budget.scope."+stableFactKey(item.Item)] = item.Status + ": " + item.Item
	}
	for index, perspective := range input.FamilyPerspectives {
		facts[fmt.Sprintf("family.perspective.%02d", index+1)] = perspective.Role + ": " + perspective.Request
	}
	for index, item := range input.PreparationItems {
		facts[fmt.Sprintf("readiness.item.%02d", index+1)] = item.Status + ": " + item.Item
	}
	addHouseFacts(input, facts)
	return facts
}

func PlanDecision(input DecisionInput) DecisionBrief {
	if input.House != nil {
		return PlanHouseDecision(input)
	}
	normalized, err := NormalizeDecisionInput(input)
	if err != nil {
		return DecisionBrief{}
	}
	var brief DecisionBrief
	switch normalized.PrimaryGoal {
	case GoalSpace:
		brief = planSpaceDecision(normalized)
	case GoalBudget:
		brief = planBudgetDecision(normalized)
	case GoalFamily:
		brief = planFamilyDecision(normalized)
	case GoalReadiness:
		brief = planReadinessDecision(normalized)
	}
	brief.Recipe = normalized.PrimaryGoal
	brief.Unknowns = decisionUnknowns(normalized)
	brief.ID = decisionBriefID(normalized, brief)
	return brief
}

func ValidateDecisionBrief(input DecisionInput, brief DecisionBrief) error {
	if input.House != nil {
		return ValidateHouseBrief(input, brief)
	}
	normalized, err := NormalizeDecisionInput(input)
	if err != nil {
		return err
	}
	baseline := PlanDecision(normalized)
	if brief.ID == "" || brief.ID != baseline.ID || brief.Recipe != normalized.PrimaryGoal || brief.Topic != baseline.Topic ||
		!sameStrings(brief.EvidenceRefs, baseline.EvidenceRefs) || !sameStrings(brief.Unknowns, baseline.Unknowns) || !sameBlockTitles(brief.Options, baseline.Options) ||
		!boundedCleanText(brief.Topic, 4, 120) || !boundedCleanText(brief.Reason, 8, 500) || !boundedCleanText(brief.Question, 4, 180) ||
		len(brief.EvidenceRefs) == 0 || len(brief.EvidenceRefs) > 12 || len(brief.Options) == 0 || len(brief.Options) > 4 {
		return ErrInvalidInput
	}
	facts := BuildDecisionFacts(normalized)
	for _, ref := range brief.EvidenceRefs {
		if _, ok := facts[ref]; !ok {
			return ErrInvalidInput
		}
	}
	if !validBlocks(brief.Options, 4, 80, 500) {
		return ErrInvalidInput
	}
	for _, unknown := range brief.Unknowns {
		if !boundedCleanText(unknown, 2, 100) {
			return ErrInvalidInput
		}
	}
	if inventsUnknownNumericClaim(normalized, briefText(brief)) {
		return ErrInvalidInput
	}
	return nil
}

func BuildDecisionPreview(record *Report, input DecisionInput, brief DecisionBrief) (DecisionSnapshot, error) {
	if record != nil && record.ProtocolVersion == ProtocolHouseV1 {
		return BuildHousePreview(record, input, brief)
	}
	normalized, err := validateDecisionRecord(record, input)
	if err != nil {
		return DecisionSnapshot{}, err
	}
	if err := ValidateDecisionBrief(normalized, brief); err != nil {
		return DecisionSnapshot{}, err
	}
	brief.Unknowns = append([]string(nil), decisionUnknowns(normalized)...)
	facts := BuildDecisionFacts(normalized)
	pages := []SnapshotPage{
		{Number: 1, Access: "free", Kind: "decision", Title: "我家值得先决定什么", Subtitle: brief.Topic, Blocks: []ContentBlock{{Title: "核心判断", Body: brief.Reason}}, Sources: cloneStrings(brief.EvidenceRefs)},
		{Number: 2, Access: "free", Kind: "evidence", Title: "为什么提出这个判断", Subtitle: "依据用户已填写的事实，同时保留未知边界。", Blocks: factBlocks(facts, brief.EvidenceRefs), Sources: cloneStrings(brief.EvidenceRefs), Note: strings.Join(brief.Unknowns, "；")},
		{Number: 3, Access: "free", Kind: "directions", Title: previewDirectionTitle(normalized.PrimaryGoal), Subtitle: previewDirectionSubtitle(normalized.PrimaryGoal), Blocks: cloneBlocks(brief.Options), Sources: cloneStrings(brief.EvidenceRefs)},
		{Number: 4, Access: "free", Kind: "confirmation", Title: "确认后可以继续解决什么", Subtitle: brief.Question, Blocks: []ContentBlock{{Title: "个性化深化", Body: paidValueForRecipe(normalized.PrimaryGoal)}, {Title: "结论边界", Body: "未核实的工程与审批条件会作为待确认事项，不会被写成已知结论。"}}, Sources: []string{"intent.priorities"}},
	}
	snapshot := DecisionSnapshot{Meta: decisionMeta(record, DecisionPreviewVersion), Facts: facts, Recipe: normalized.PrimaryGoal, Diagnosis: brief, Pages: pages}
	if err := ValidateDecisionSnapshot(snapshot, ModePreview); err != nil {
		return DecisionSnapshot{}, err
	}
	return snapshot, nil
}

func BuildDecisionFull(record *Report, input DecisionInput, confirmation DecisionConfirmation, brief DecisionBrief, analysis []ContentBlock, budget []ContentBlock) (DecisionSnapshot, error) {
	normalized, err := validateDecisionRecord(record, input)
	if err != nil {
		return DecisionSnapshot{}, err
	}
	if err := ValidateDecisionBrief(normalized, brief); err != nil {
		return DecisionSnapshot{}, err
	}
	brief.Unknowns = append([]string(nil), decisionUnknowns(normalized)...)
	confirmation, err = NormalizeDecisionConfirmation(normalized, confirmation)
	if err != nil || confirmation.DiagnosisID != brief.ID {
		return DecisionSnapshot{}, ErrInvalidInput
	}
	if len(analysis) < 2 || len(budget) < 1 || !validBlocks(analysis, 8, 80, 700) || !validBlocks(budget, 8, 80, 700) ||
		inventsUnknownNumericClaim(normalized, blocksText(analysis, budget)) {
		return DecisionSnapshot{}, ErrInvalidInput
	}
	facts := BuildDecisionFacts(normalized)
	p04, p05, p06 := fullDecisionPageTitles(normalized.PrimaryGoal)
	first, second := splitBlocks(analysis)
	factKeys := reportFactKeys(facts)
	pages := []SnapshotPage{
		{Number: 1, Access: "paid", Kind: "recommendation", Title: "本次报告的核心建议", Subtitle: brief.Topic, Blocks: []ContentBlock{{Title: "建议", Body: confirmedRecommendation(brief, confirmation)}, {Title: "阅读边界", Body: "这是建房前期决策底稿，需要测绘、审批、设计和正式询价核实的内容已单独标出。"}}, Sources: cloneStrings(brief.EvidenceRefs)},
		{Number: 2, Access: "paid", Kind: "facts", Title: "我家的事实与待核实项", Subtitle: "将用户自述、计算信息与未知项分开。", Blocks: factBlocks(facts, factKeys), Sources: factKeys, Note: strings.Join(brief.Unknowns, "；")},
		{Number: 3, Access: "paid", Kind: "criteria", Title: "决策优先级与评价标准", Subtitle: "以用户确认后的顺序比较。", Priorities: priorityBlocks(confirmation.Priorities), Sources: []string{"intent.priorities"}},
		{Number: 4, Access: "paid", Kind: "analysis_primary", Title: p04, Subtitle: "说明收益、影响、成立条件和待验证内容。", Blocks: first, Sources: cloneStrings(brief.EvidenceRefs)},
		{Number: 5, Access: "paid", Kind: "analysis_secondary", Title: p05, Subtitle: "使用同一组家庭事实作可比较的分析。", Blocks: second, Sources: cloneStrings(brief.EvidenceRefs)},
		{Number: 6, Access: "paid", Kind: "decision_table", Title: p06, Subtitle: "将判断条件与可能改变结论的新信息列清。", Blocks: cloneBlocks(brief.Options), Sources: cloneStrings(brief.EvidenceRefs)},
		{Number: 7, Access: "paid", Kind: "budget_scope", Title: "预算范围与询价清单", Subtitle: "只使用用户填写的计划投入和范围状态。", Blocks: cloneBlocks(budget), Sources: budgetFactKeys(facts)},
		{Number: 8, Access: "paid", Kind: "designer_brief", Title: "给设计师的需求任务书", Subtitle: "将已确认需求、可协商项和待回答问题放在同一页。", Blocks: designerBriefBlocks(confirmation, brief), Sources: cloneStrings(brief.EvidenceRefs)},
		{Number: 9, Access: "paid", Kind: "action_plan", Title: "下一步行动计划", Subtitle: "每项行动说明前置条件、完成凭证和建议责任方。", Actions: actionBlocks(normalized, brief), Sources: cloneStrings(brief.EvidenceRefs)},
		{Number: 10, Access: "paid", Kind: "confirmation_sheet", Title: "沟通确认单", Subtitle: "下次家庭与设计师沟通时可直接使用。", Blocks: communicationChecklist(confirmation, brief), Sources: cloneStrings(brief.EvidenceRefs), Note: "报告模板：" + DecisionTemplateVersion},
	}
	snapshot := DecisionSnapshot{Meta: decisionMeta(record, DecisionTemplateVersion), Facts: facts, Recipe: normalized.PrimaryGoal, Diagnosis: brief, Confirmation: &confirmation, Pages: pages}
	if err := ValidateDecisionSnapshot(snapshot, ModeFull); err != nil {
		return DecisionSnapshot{}, err
	}
	return snapshot, nil
}

func ValidateDecisionSnapshot(snapshot DecisionSnapshot, mode string) error {
	if snapshot.Meta.ProtocolVersion == ProtocolHouseV1 {
		return ValidateHouseSnapshot(snapshot, mode)
	}
	wantPages := 0
	switch mode {
	case ModePreview:
		wantPages = 4
	case ModeFull:
		wantPages = 10
	default:
		return ErrInvalidInput
	}
	if snapshot.Meta.SchemaVersion != SchemaDecisionV5 || snapshot.Meta.ProtocolVersion != ProtocolDecisionV5 || snapshot.Meta.Source != DecisionSourceV5 ||
		snapshot.Meta.ReportID == "" || snapshot.Meta.InputHash == "" || !oneOf(snapshot.Recipe, GoalSpace, GoalBudget, GoalFamily, GoalReadiness) || len(snapshot.Pages) != wantPages || len(snapshot.Facts) == 0 || len(snapshot.Facts) > 64 {
		return ErrInvalidInput
	}
	for key, value := range snapshot.Facts {
		if !boundedCleanText(key, 2, 100) || !boundedCleanText(value, 1, 300) {
			return ErrInvalidInput
		}
	}
	if snapshot.Diagnosis.ID == "" || snapshot.Diagnosis.Recipe != snapshot.Recipe {
		return ErrInvalidInput
	}
	if mode == ModePreview && snapshot.Confirmation != nil {
		return ErrInvalidInput
	}
	if mode == ModeFull && (snapshot.Confirmation == nil || snapshot.Confirmation.DiagnosisID != snapshot.Diagnosis.ID || snapshot.Confirmation.InputHash != snapshot.Meta.InputHash) {
		return ErrInvalidInput
	}
	for index, page := range snapshot.Pages {
		if page.Number != index+1 || !oneOf(page.Access, "free", "paid") || !boundedCleanText(page.Title, 2, 100) || !boundedCleanText(page.Subtitle, 2, 240) ||
			!validBlocks(page.Blocks, 10, 100, 900) || !validBlocks(page.Priorities, 6, 100, 500) || !validBlocks(page.Actions, 8, 100, 500) ||
			!validTable(page.Table) || !optionalBoundedText(page.Note, 500) || !optionalBoundedText(page.Disclaimer, 500) || len(page.Sources) > 16 {
			return ErrInvalidInput
		}
		if mode == ModePreview && page.Access != "free" || mode == ModeFull && page.Access != "paid" {
			return ErrInvalidInput
		}
		for _, source := range page.Sources {
			if _, ok := snapshot.Facts[source]; !ok {
				return ErrInvalidInput
			}
		}
	}
	visible := snapshotVisibleText(snapshot)
	if snapshot.Facts["site.approx_dimensions"] == "unknown" && containsGeometryClaim(visible) ||
		snapshot.Facts["budget.wan"] == "unknown" && containsBudgetClaim(visible) {
		return ErrInvalidInput
	}
	return nil
}

func planSpaceDecision(input DecisionInput) DecisionBrief {
	evidence := []string{"household.permanent_count", "household.returning_count", "household.return_frequency", "intent.flexible_use", "intent.priorities"}
	if input.ReturningCount != nil && *input.ReturningCount > 0 && input.ReturnFrequency != FrequencyNone {
		options := []ContentBlock{{Title: "保留固定客房", Body: "团聚时准备更直接，但要一并比较长期空置房间对日常空间的影响。"}, {Title: "使用可转换房间", Body: "日常可服务阅读、收纳或活动，团聚前需明确转换方式与隐私边界。"}}
		if input.FlexibleUse == FlexibleUseNo {
			options = []ContentBlock{{Title: "独立居住底线", Body: "按明确的独立居住需求整理房间任务，不把兼用作为默认前提。"}, {Title: "团聚时的公共空间组织", Body: "在保留独立居住底线的同时，让设计师重点验证团聚活动与日常动线。"}}
		}
		return DecisionBrief{Topic: "日常居住与节日团聚空间的取舍", Reason: "常住人数与返乡人数形成两种使用情景，需要先确认房间是否长期固定，再让设计师验证空间条件。", EvidenceRefs: evidence, Options: options, Question: "你更在意随时可住的独立房间，还是日常空间利用率？拿不准也可以保留两个方向。"}
	}
	return DecisionBrief{Topic: "长期同住中私密性与公共活动的取舍", Reason: "当前输入以长期居住为主，应先保留已表达的独立居住底线，再比较公共活动与相互干扰。", EvidenceRefs: evidence, Options: []ContentBlock{{Title: "独立居住底线", Body: "明确哪些居住需求必须独立，并让设计师验证隔音、动线和日常使用。"}, {Title: "共享活动组织", Body: "在不改变独立居住底线的前提下，比较公共活动的位置与时段对家人的影响。"}}, Question: "哪些私密与独立居住需求是不能交换的底线？"}
}

func planBudgetDecision(input DecisionInput) DecisionBrief {
	refs := []string{"budget.wan", "intent.priorities"}
	for _, item := range input.BudgetScope {
		refs = append(refs, "budget.scope."+stableFactKey(item.Item))
	}
	reason := "计划投入在主体、装修、庭院与设备等范围不同时不能直接比较。先锁定包含项、排除项和待确认项。"
	if input.BudgetWan == nil {
		reason = "当前计划投入金额尚未确认，但可以先分清主体、装修、庭院与设备等范围中的包含项、排除项和待确认项。"
	}
	return DecisionBrief{Topic: "先统一计划投入的覆盖范围", Reason: reason, EvidenceRefs: refs, Options: []ContentBlock{{Title: "必要需求与明确包含项", Body: "保留已表达的生活底线，并用同一询价口径确认覆盖范围。"}, {Title: "可调整需求与待确认范围", Body: "对尚未决定是否包含的项目单独询价，不预先假设单价或节省金额。"}}, Question: "哪些需求必须包含在当前计划投入内，哪些可以分期或单独询价？"}
}

func planFamilyDecision(input DecisionInput) DecisionBrief {
	refs := []string{"intent.priorities"}
	for index := range input.FamilyPerspectives {
		refs = append(refs, fmt.Sprintf("family.perspective.%02d", index+1))
	}
	if len(input.FamilyPerspectives) == 1 {
		return DecisionBrief{Topic: "先补齐家人视角，再确认共同底线", Reason: "当前只有单一视角的明确表达，可以先保留这条需求，但不能据此声称已经存在家庭分歧或已达成全家共识。", EvidenceRefs: refs, Options: []ContentBlock{{Title: "已表达需求", Body: input.FamilyPerspectives[0].Role + "已表达：" + input.FamilyPerspectives[0].Request}, {Title: "待补充视角", Body: "请其他参与决策的家人各自说明一条底线和一条可协商项。"}}, Question: "还需要哪些家人表达自己的底线与可协商项？"}
	}
	return DecisionBrief{Topic: "先分清共同底线与已表达的不同需求", Reason: "已收集多个家庭视角，应先把共同底线、个人偏好和可协商事项分开，不把不同表达自动定性为冲突。", EvidenceRefs: refs, Options: []ContentBlock{{Title: "共同底线清单", Body: "找出所有人都认可且不宜交换的生活需求。"}, {Title: "不同需求与可协商项", Body: "把各方已表达的需求放在同一组使用情景中比较，记录需要共同确认的条件。"}}, Question: "请确认哪些是全家共同底线，哪些仍需要家庭讨论？"}
}

func planReadinessDecision(input DecisionInput) DecisionBrief {
	refs := []string{"readiness.stage", "intent.priorities"}
	for index := range input.PreparationItems {
		refs = append(refs, fmt.Sprintf("readiness.item.%02d", index+1))
	}
	return DecisionBrief{Topic: "先识别真正阻断下一步的条件", Reason: "当前阶段的资料可分为已具备、缺失和尚未确认。先处理会阻断后续决策的项目，其余准备工作可以并行推进。", EvidenceRefs: refs, Options: []ContentBlock{{Title: "阻断项与前置条件", Body: "将标记为缺失或未知、且会影响下一阶段的资料放在首位。"}, {Title: "可并行准备的事项", Body: "在不依赖未核实结论的范围内，同步整理家庭需求、沟通清单和询价口径。"}}, Question: "请确认哪些资料已有可核对的凭证，哪些仍需要联系专业人员核实？"}
}

func decisionUnknowns(input DecisionInput) []string {
	unknowns := make([]string, 0, 5)
	if input.ApproximateDimensions.WidthM == nil {
		unknowns = append(unknowns, "宅基地估算尺寸未提供")
	}
	if input.BuildableScope == BuildableScopeUnknown {
		unknowns = append(unknowns, "实际可建范围未核实")
	}
	if input.BudgetWan == nil {
		unknowns = append(unknowns, "计划投入金额未提供")
	}
	for _, item := range input.BudgetScope {
		if item.Status == ScopeUnknown {
			unknowns = append(unknowns, item.Item+"是否包含在计划投入内待确认")
		}
	}
	if input.ReturnFrequency == FrequencyUnknown {
		unknowns = append(unknowns, "返乡使用频率未确认")
	}
	return unknowns
}

func validateDecisionRecord(record *Report, input DecisionInput) (DecisionInput, error) {
	if record == nil || record.ID == "" || !IsDecisionProtocol(record.ProtocolVersion) || record.InputHash == "" || (record.ProtocolVersion == ProtocolHouseV1) != (input.House != nil) {
		return DecisionInput{}, ErrInvalidInput
	}
	normalized, err := NormalizeDecisionInput(input)
	if err != nil {
		return DecisionInput{}, err
	}
	_, hash, err := MarshalDecisionInput(normalized)
	if err != nil || hash != record.InputHash {
		return DecisionInput{}, ErrInvalidInput
	}
	if len(record.InputJSON) > 0 {
		var stored DecisionInput
		if err := json.Unmarshal(record.InputJSON, &stored); err != nil {
			return DecisionInput{}, ErrInvalidInput
		}
		_, storedHash, err := MarshalDecisionInput(stored)
		if err != nil || storedHash != record.InputHash {
			return DecisionInput{}, ErrInvalidInput
		}
	}
	return normalized, nil
}

func decisionMeta(record *Report, _ string) SnapshotMeta {
	date := ""
	if !record.CreatedAt.IsZero() {
		date = record.CreatedAt.Format("2006年01月02日")
	}
	meta := SnapshotMeta{SchemaVersion: SchemaDecisionV5, ProtocolVersion: ProtocolDecisionV5, Source: DecisionSourceV5, ReportID: record.ID, InputHash: record.InputHash, Date: date, Demonstration: false, GenerationMode: record.GenerationMode}
	if record.ProtocolVersion == ProtocolHouseV1 {
		meta.SchemaVersion = SchemaHouseV1
		meta.ProtocolVersion = ProtocolHouseV1
		meta.Source = HouseSource
	}
	return meta
}

func decisionBriefID(input DecisionInput, brief DecisionBrief) string {
	identity := struct {
		Recipe       string   `json:"recipe"`
		Topic        string   `json:"topic"`
		EvidenceRefs []string `json:"evidence_refs"`
		OptionTitles []string `json:"option_titles"`
	}{Recipe: brief.Recipe, Topic: brief.Topic, EvidenceRefs: cloneStrings(brief.EvidenceRefs)}
	for _, option := range brief.Options {
		identity.OptionTitles = append(identity.OptionTitles, option.Title)
	}
	inputJSON, _, _ := MarshalDecisionInput(input)
	identityJSON, _ := json.Marshal(identity)
	digest := sha256.Sum256(append(append([]byte(nil), inputJSON...), identityJSON...))
	return "diagnosis-" + hex.EncodeToString(digest[:8])
}

func sameStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func sameBlockTitles(left, right []ContentBlock) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index].Title != right[index].Title {
			return false
		}
	}
	return true
}

func normalizeScopeItems(items []DecisionScopeItem) ([]DecisionScopeItem, error) {
	if len(items) > 8 {
		return nil, ErrInvalidInput
	}
	out := append([]DecisionScopeItem(nil), items...)
	seen := map[string]bool{}
	for index := range out {
		out[index].Item = collapseInputWhitespace(out[index].Item)
		out[index].Status = strings.TrimSpace(out[index].Status)
		if !boundedCleanText(out[index].Item, 1, 40) || !oneOf(out[index].Status, ScopeIncluded, ScopeExcluded, ScopeUnknown) || seen[out[index].Item] {
			return nil, ErrInvalidInput
		}
		seen[out[index].Item] = true
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Item < out[j].Item })
	return out, nil
}

func normalizePerspectives(items []FamilyPerspective) ([]FamilyPerspective, error) {
	if len(items) > 8 {
		return nil, ErrInvalidInput
	}
	out := append([]FamilyPerspective(nil), items...)
	seen := map[string]bool{}
	for index := range out {
		out[index].Role = collapseInputWhitespace(out[index].Role)
		out[index].Request = collapseInputWhitespace(out[index].Request)
		if !boundedCleanText(out[index].Role, 1, 30) || !boundedCleanText(out[index].Request, 2, 160) || seen[out[index].Role] {
			return nil, ErrInvalidInput
		}
		seen[out[index].Role] = true
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Role < out[j].Role })
	return out, nil
}

func normalizePreparationItems(items []PreparationItem) ([]PreparationItem, error) {
	if len(items) > 10 {
		return nil, ErrInvalidInput
	}
	out := append([]PreparationItem(nil), items...)
	seen := map[string]bool{}
	for index := range out {
		out[index].Item = collapseInputWhitespace(out[index].Item)
		out[index].Status = strings.TrimSpace(out[index].Status)
		if !boundedCleanText(out[index].Item, 1, 60) || !oneOf(out[index].Status, PreparationExisting, PreparationMissing, PreparationUnknown) || seen[out[index].Item] {
			return nil, ErrInvalidInput
		}
		seen[out[index].Item] = true
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Item < out[j].Item })
	return out, nil
}

func normalizeUniqueTexts(values []string, minItems, maxItems, minLength, maxLength int) ([]string, error) {
	if len(values) < minItems || len(values) > maxItems {
		return nil, ErrInvalidInput
	}
	out := append([]string(nil), values...)
	seen := map[string]bool{}
	for index := range out {
		out[index] = collapseInputWhitespace(out[index])
		if !boundedCleanText(out[index], minLength, maxLength) || seen[out[index]] {
			return nil, ErrInvalidInput
		}
		seen[out[index]] = true
	}
	return out, nil
}

func oneOf(value string, allowed ...string) bool {
	for _, item := range allowed {
		if value == item {
			return true
		}
	}
	return false
}
func boundedCleanText(value string, min, max int) bool {
	return boundedText(value, min, max) && !containsControl(value)
}
func validOptionalInt(value *int, min, max int) bool {
	return value == nil || *value >= min && *value <= max
}
func validOptionalFloat(value *float64, min, max float64) bool {
	return value == nil || boundedNumber(*value, min, max)
}
func valueOrUnknown(value string) string {
	if value == "" {
		return "unknown"
	}
	return value
}
func formatOptionalInt(value *int) string {
	if value == nil {
		return "unknown"
	}
	return strconv.Itoa(*value)
}
func formatOptionalFloat(value *float64) string {
	if value == nil {
		return "unknown"
	}
	return formatFloat(*value)
}
func formatFloat(value float64) string { return strconv.FormatFloat(value, 'f', -1, 64) }
func stableFactKey(value string) string {
	digest := sha256.Sum256([]byte(value))
	return hex.EncodeToString(digest[:6])
}
func cloneStrings(values []string) []string            { return append([]string(nil), values...) }
func cloneBlocks(values []ContentBlock) []ContentBlock { return append([]ContentBlock(nil), values...) }

func validBlocks(blocks []ContentBlock, maxItems, maxTitle, maxBody int) bool {
	if len(blocks) > maxItems {
		return false
	}
	for _, block := range blocks {
		if !boundedCleanText(block.Title, 1, maxTitle) || !boundedCleanText(block.Body, 1, maxBody) {
			return false
		}
	}
	return true
}

func validTable(table [][]string) bool {
	if len(table) > 6 {
		return false
	}
	for _, row := range table {
		if len(row) == 0 || len(row) > 3 {
			return false
		}
		for _, cell := range row {
			if !boundedCleanText(cell, 1, 300) {
				return false
			}
		}
	}
	return true
}

func optionalBoundedText(value string, max int) bool {
	return value == "" || boundedCleanText(value, 1, max)
}

func briefText(brief DecisionBrief) string {
	parts := []string{brief.Topic, brief.Reason, brief.Question}
	for _, option := range brief.Options {
		parts = append(parts, option.Title, option.Body)
	}
	return strings.Join(parts, " ")
}

func blocksText(groups ...[]ContentBlock) string {
	parts := make([]string, 0)
	for _, blocks := range groups {
		for _, block := range blocks {
			parts = append(parts, block.Title, block.Body)
		}
	}
	return strings.Join(parts, " ")
}

func inventsUnknownNumericClaim(input DecisionInput, text string) bool {
	return input.ApproximateDimensions.WidthM == nil && containsGeometryClaim(text) || input.BudgetWan == nil && containsBudgetClaim(text)
}

func containsGeometryClaim(text string) bool {
	claims := append(numericClaimPattern.FindAllString(text, -1), chineseMeasurementClaimPattern.FindAllString(text, -1)...)
	for _, claim := range claims {
		if strings.Contains(claim, "米") || strings.Contains(claim, "㎡") {
			return true
		}
	}
	return false
}

func containsBudgetClaim(text string) bool {
	claims := append(numericClaimPattern.FindAllString(text, -1), chineseMeasurementClaimPattern.FindAllString(text, -1)...)
	for _, claim := range claims {
		if strings.Contains(claim, "万") || strings.Contains(claim, "元") {
			return true
		}
	}
	return false
}

func snapshotVisibleText(snapshot DecisionSnapshot) string {
	parts := []string{snapshot.Diagnosis.Topic, snapshot.Diagnosis.Reason, snapshot.Diagnosis.Question}
	for _, page := range snapshot.Pages {
		parts = append(parts, page.Title, page.Subtitle, page.Fixed, page.Dynamic, page.AI, page.Note, page.Disclaimer)
		parts = append(parts, blocksText(page.Blocks, page.Priorities, page.Actions))
		for _, row := range page.Table {
			parts = append(parts, row...)
		}
	}
	return strings.Join(parts, " ")
}

func factBlocks(facts map[string]string, refs []string) []ContentBlock {
	out := make([]ContentBlock, 0, len(refs))
	for _, ref := range refs {
		out = append(out, ContentBlock{Title: decisionFactLabel(ref, facts[ref]), Body: decisionFactValue(ref, facts[ref])})
	}
	return out
}

func decisionFactLabel(key, value string) string {
	labels := map[string]string{
		"input.primary_goal":         "当前最想解决的问题",
		"site.region":                "建房地区",
		"site.approx_dimensions":     "宅基地估算尺寸",
		"site.buildable_scope":       "可建范围核实状态",
		"site.buildable_constraints": "已知可建条件",
		"household.permanent_count":  "常住人数",
		"household.returning_count":  "额外返乡人数",
		"household.return_frequency": "返乡频率",
		"intent.floors":              "意向层数",
		"intent.bedrooms":            "意向卧室数",
		"intent.flexible_use":        "房间兼用接受度",
		"intent.priorities":          "当前优先级",
		"budget.wan":                 "计划投入",
		"readiness.stage":            "当前准备阶段",
	}
	if label := labels[key]; label != "" {
		return label
	}
	if strings.HasPrefix(key, "budget.scope.") {
		return "投入范围｜" + factItemName(value)
	}
	if strings.HasPrefix(key, "family.perspective.") {
		return "家庭视角 " + strings.TrimLeft(strings.TrimPrefix(key, "family.perspective."), "0")
	}
	if strings.HasPrefix(key, "readiness.item.") {
		return "准备事项｜" + factItemName(value)
	}
	return "已填写信息"
}

func decisionFactValue(key, value string) string {
	if value == "unknown" {
		return "待确认"
	}
	switch key {
	case "input.primary_goal":
		return map[string]string{GoalSpace: "空间怎么分配", GoalBudget: "预算怎么安排", GoalFamily: "家人意见怎么统一", GoalReadiness: "开工前还缺什么"}[value]
	case "site.buildable_scope":
		return map[string]string{BuildableScopeKnown: "已核实（用户自述）", BuildableScopeUnknown: "尚未核实"}[value]
	case "household.permanent_count", "household.returning_count":
		return value + " 人"
	case "household.return_frequency":
		return map[string]string{FrequencyNone: "无额外返乡居住", FrequencyMonthly: "每月", FrequencyQuarterly: "每季度", FrequencyHolidays: "节假日返乡", FrequencyYearly: "每年", FrequencyUnknown: "待确认"}[value]
	case "intent.floors":
		return value + " 层"
	case "intent.bedrooms":
		return value + " 间"
	case "intent.flexible_use":
		return map[string]string{FlexibleUseYes: "可接受兼用", FlexibleUseNo: "不接受兼用", FlexibleUseUnknown: "尚未决定"}[value]
	case "budget.wan":
		return value + " 万元"
	case "readiness.stage":
		return map[string]string{StageIdea: "意向阶段", StageInformation: "资料整理阶段", StageDesign: "设计阶段", StageApproval: "审批阶段", StageBidding: "询价比较阶段", StagePreconstruction: "开工准备阶段", StageUnknown: "待确认"}[value]
	}
	if strings.HasPrefix(key, "budget.scope.") {
		return translateFactStatus(value, map[string]string{ScopeIncluded: "已包含", ScopeExcluded: "明确不含", ScopeUnknown: "待确认"})
	}
	if strings.HasPrefix(key, "readiness.item.") {
		return translateFactStatus(value, map[string]string{PreparationExisting: "已具备", PreparationMissing: "尚缺失", PreparationUnknown: "待确认"})
	}
	return strings.ReplaceAll(strings.ReplaceAll(value, "m x ", " 米 × "), "m (user estimate)", " 米（用户估算）")
}

func factItemName(value string) string {
	parts := strings.SplitN(value, ": ", 2)
	if len(parts) == 2 {
		return parts[1]
	}
	return value
}

func translateFactStatus(value string, labels map[string]string) string {
	parts := strings.SplitN(value, ": ", 2)
	if len(parts) != 2 {
		return value
	}
	if label := labels[parts[0]]; label != "" {
		return label + "：" + parts[1]
	}
	return parts[1]
}
func reportFactKeys(facts map[string]string) []string {
	preferred := []string{"input.primary_goal", "site.region", "site.approx_dimensions", "site.buildable_scope", "household.permanent_count", "household.returning_count", "household.return_frequency", "intent.floors", "intent.bedrooms", "budget.wan"}
	out := make([]string, 0, len(preferred))
	for _, key := range preferred {
		if _, ok := facts[key]; ok {
			out = append(out, key)
		}
	}
	return out
}
func sortedFactKeys(facts map[string]string) []string {
	keys := make([]string, 0, len(facts))
	for key := range facts {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
func budgetFactKeys(facts map[string]string) []string {
	out := []string{"budget.wan"}
	for _, key := range sortedFactKeys(facts) {
		if strings.HasPrefix(key, "budget.scope.") {
			out = append(out, key)
		}
	}
	return out
}
func priorityBlocks(priorities []string) []ContentBlock {
	out := make([]ContentBlock, 0, len(priorities))
	for index, priority := range priorities {
		out = append(out, ContentBlock{Title: fmt.Sprintf("优先级 %d", index+1), Body: priority})
	}
	return out
}

func previewDirectionTitle(recipe string) string {
	if recipe == GoalReadiness {
		return "推进条件与并行事项"
	}
	return "可比较的方向"
}
func previewDirectionSubtitle(recipe string) string {
	if recipe == GoalFamily {
		return "先把共同底线与待讨论项分开。"
	}
	if recipe == GoalReadiness {
		return "分清真正阻断项与可并行的准备。"
	}
	return "展示收益、代价与成立条件，不把概念方向当成已定方案。"
}
func paidValueForRecipe(recipe string) string {
	values := map[string]string{GoalSpace: "形成使用情景比较、空间取舍表、设计师任务书和行动计划。", GoalBudget: "形成范围账本、需求取舍、统一询价清单和行动顺序。", GoalFamily: "形成共识与待讨论表、家庭讨论清单、设计师任务书和行动计划。", GoalReadiness: "形成阻断项、资料依赖、责任人建议和沟通顺序。"}
	return values[recipe]
}

func fullDecisionPageTitles(recipe string) (string, string, string) {
	switch recipe {
	case GoalBudget:
		return "投入边界与必要需求", "可调整需求与待核实代价", "投入取舍决策表"
	case GoalFamily:
		return "共同底线与已表达需求", "已知不同需求与可协商事项", "家庭讨论决策表与待确认项"
	case GoalReadiness:
		return "真正阻断下一步的条件", "可以并行开展的准备事项", "资料与行动依赖决策表"
	default:
		return "方向 A 的具体分析", "方向 B 的具体分析", "方向决策表与条件化建议"
	}
}

func splitBlocks(blocks []ContentBlock) ([]ContentBlock, []ContentBlock) {
	middle := (len(blocks) + 1) / 2
	return cloneBlocks(blocks[:middle]), cloneBlocks(blocks[middle:])
}
func confirmedRecommendation(brief DecisionBrief, confirmation DecisionConfirmation) string {
	if confirmation.Choice == ChoiceUndecided {
		return "用户暂未确定单一方向；报告保留两组条件，并说明哪些新信息会导致改选。"
	}
	if confirmation.Choice == ChoiceOptionA && len(brief.Options) > 0 {
		return "根据用户确认，优先深化「" + brief.Options[0].Title + "」。" + brief.Options[0].Body
	}
	if confirmation.Choice == ChoiceOptionB && len(brief.Options) > 1 {
		return "根据用户确认，优先深化「" + brief.Options[1].Title + "」。" + brief.Options[1].Body
	}
	return brief.Reason
}
func designerBriefBlocks(confirmation DecisionConfirmation, brief DecisionBrief) []ContentBlock {
	return []ContentBlock{{Title: "已确认优先级", Body: strings.Join(confirmation.Priorities, " / ")}, {Title: "需要设计师回答", Body: brief.Question}, {Title: "待核实条件", Body: decisionUnknownSummary(brief.Unknowns)}}
}
func actionBlocks(input DecisionInput, brief DecisionBrief) []ContentBlock {
	switch input.PrimaryGoal {
	case GoalBudget:
		included, uncertain := scopeItemsByStatus(input.BudgetScope, ScopeIncluded), scopeItemsByStatus(input.BudgetScope, ScopeUnknown)
		return []ContentBlock{{Title: "冻结本次询价口径", Body: "前置：确认已包含项" + joinedOrNone(included) + "。凭证：投入范围清单。责任方：家庭决策人。"}, {Title: "单独核实待定范围", Body: "前置：列出待确认项" + joinedOrNone(uncertain) + "。凭证：同口径分项询价回执。责任方：询价对象与家庭。"}, {Title: "根据同口径回执取舍", Body: "前置：各方使用同一范围回复。凭证：可比较的询价表与需求取舍记录。责任方：家庭决策人。"}}
	case GoalFamily:
		roles := make([]string, 0, len(input.FamilyPerspectives))
		for _, perspective := range input.FamilyPerspectives {
			roles = append(roles, perspective.Role)
		}
		return []ContentBlock{{Title: "核对各方已表达需求", Body: "前置：由" + strings.Join(roles, "、") + "分别核对自己的表达。凭证：已确认的家庭视角清单。责任方：各参与决策的家人。"}, {Title: "完成一次家庭讨论", Body: "前置：先分开共同底线与可协商项。凭证：家庭讨论确认单。责任方：家庭决策人。"}, {Title: "将共识交给设计师验证", Body: "前置：待讨论项已标注。凭证：设计师对任务书问题的书面回答。责任方：设计师与家庭。"}}
	case GoalReadiness:
		blocking := preparationItemsByStatus(input.PreparationItems, PreparationMissing, PreparationUnknown)
		existing := preparationItemsByStatus(input.PreparationItems, PreparationExisting)
		return []ContentBlock{{Title: "先补齐阻断下一步的资料", Body: "前置：聚焦" + joinedOrNone(blocking) + "。凭证：专业单位或管理部门可核对的文件。责任方：对应的经办人与专业人员。"}, {Title: "整理已具备资料", Body: "前置：汇总" + joinedOrNone(existing) + "。凭证：按用途分类的资料目录。责任方：家庭经办人。"}, {Title: "确认进入下一阶段的条件", Body: "前置：阻断项有了可核对结果。凭证：下一阶段输入清单。责任方：家庭与对应专业人员。"}}
	default:
		return []ContentBlock{{Title: "确认日常与团聚使用情景", Body: "前置：核对常住和返乡使用方式。凭证：两种生活情景清单。责任方：家庭决策人。"}, {Title: "确认房间兼用边界", Body: "前置：将房间兼用意愿与优先级一并说明。凭证：用户确认记录。责任方：家庭决策人。"}, {Title: "请设计师验证两种方向", Body: "前置：未知边界已标出。凭证：设计师对使用、代价与成立条件的书面回答。责任方：设计师与家庭。"}}
	}
}
func communicationChecklist(confirmation DecisionConfirmation, brief DecisionBrief) []ContentBlock {
	return []ContentBlock{{Title: "确认核心问题", Body: brief.Topic}, {Title: "确认优先顺序", Body: strings.Join(confirmation.Priorities, " / ")}, {Title: "确认仍未知的事项", Body: decisionUnknownSummary(brief.Unknowns)}, {Title: "记录会改变结论的新信息", Body: "新增的测绘、审批、家庭意见或正式询价结果需进入新的修订版本。"}}
}

func decisionUnknownSummary(unknowns []string) string {
	if len(unknowns) == 0 {
		return "当前已填写关键事项，仍需以测绘、审批和正式询价凭证核验"
	}
	return strings.Join(unknowns, "；")
}

func scopeItemsByStatus(items []DecisionScopeItem, status string) []string {
	out := make([]string, 0)
	for _, item := range items {
		if item.Status == status {
			out = append(out, item.Item)
		}
	}
	return out
}

func preparationItemsByStatus(items []PreparationItem, statuses ...string) []string {
	out := make([]string, 0)
	for _, item := range items {
		if oneOf(item.Status, statuses...) {
			out = append(out, item.Item)
		}
	}
	return out
}

func joinedOrNone(items []string) string {
	if len(items) == 0 {
		return "暂无"
	}
	return strings.Join(items, "、")
}
