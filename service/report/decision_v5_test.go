package report

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func intPointer(value int) *int           { return &value }
func floatPointer(value float64) *float64 { return &value }

func seasonalSpaceInput() DecisionInput {
	return DecisionInput{
		PrimaryGoal:           GoalSpace,
		Region:                "泉州南安",
		ApproximateDimensions: DecisionDimensions{WidthM: floatPointer(10), DepthM: floatPointer(15)},
		BuildableScope:        BuildableScopeUnknown,
		PermanentCount:        intPointer(2), ReturningCount: intPointer(4), ReturnFrequency: FrequencyHolidays,
		Floors: intPointer(2), Bedrooms: intPointer(4), FlexibleUse: FlexibleUseYes,
		Priorities:  []string{"父母居住舒适", "节日团聚", "投入可控"},
		BudgetWan:   floatPointer(100),
		BudgetScope: []DecisionScopeItem{{Item: "主体", Status: ScopeIncluded}, {Item: "室内装修", Status: ScopeIncluded}, {Item: "庭院", Status: ScopeUnknown}},
	}
}

func testReportForDecision(t *testing.T, input DecisionInput) *Report {
	t.Helper()
	raw, hash, err := MarshalDecisionInput(input)
	if err != nil {
		t.Fatalf("MarshalDecisionInput: %v", err)
	}
	return &Report{ID: "report-v5-test", InputHash: hash, InputJSON: raw, ProtocolVersion: ProtocolDecisionV5}
}

func TestNormalizeDecisionInputRejectsInvalidFlexibleUseAndPriorities(t *testing.T) {
	input := seasonalSpaceInput()
	input.FlexibleUse = "sometimes"
	if _, err := NormalizeDecisionInput(input); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("invalid flexible_use error = %v", err)
	}
	input = seasonalSpaceInput()
	input.Priorities = []string{"same", "same"}
	if _, err := NormalizeDecisionInput(input); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("duplicate priorities error = %v", err)
	}
}

func TestPlanDecisionChangesWithLivingPattern(t *testing.T) {
	seasonal := seasonalSpaceInput()
	seasonalBrief := PlanDecision(seasonal)
	if !strings.Contains(seasonalBrief.Topic, "团聚") || len(seasonalBrief.Options) != 2 {
		t.Fatalf("seasonal brief = %#v", seasonalBrief)
	}
	permanent := seasonalSpaceInput()
	permanent.PermanentCount, permanent.ReturningCount = intPointer(6), intPointer(0)
	permanent.ReturnFrequency, permanent.FlexibleUse = FrequencyNone, FlexibleUseNo
	permanentBrief := PlanDecision(permanent)
	if seasonalBrief.Topic == permanentBrief.Topic || !strings.Contains(permanentBrief.Topic, "私密") {
		t.Fatalf("permanent brief did not change: %#v", permanentBrief)
	}
	for _, option := range permanentBrief.Options {
		if strings.Contains(option.Body, "兼用客房") {
			t.Fatalf("rejected flexible use was recommended: %#v", option)
		}
	}
}

func TestPlanDecisionUsesFourDifferentRecipes(t *testing.T) {
	tests := []struct {
		name  string
		input DecisionInput
		want  string
	}{
		{name: "space", input: seasonalSpaceInput(), want: "团聚"},
		{name: "budget", input: DecisionInput{PrimaryGoal: GoalBudget, Priorities: []string{"投入可控"}, FlexibleUse: FlexibleUseUnknown, BuildableScope: BuildableScopeUnknown, ReturnFrequency: FrequencyUnknown, BudgetWan: floatPointer(100), BudgetScope: []DecisionScopeItem{{Item: "主体", Status: ScopeIncluded}, {Item: "庭院", Status: ScopeUnknown}}}, want: "覆盖范围"},
		{name: "family", input: DecisionInput{PrimaryGoal: GoalFamily, Priorities: []string{"全家可以共同确认"}, FlexibleUse: FlexibleUseUnknown, BuildableScope: BuildableScopeUnknown, ReturnFrequency: FrequencyUnknown, FamilyPerspectives: []FamilyPerspective{{Role: "父母", Request: "首层日常起居"}, {Role: "子女", Request: "节日团聚空间"}}}, want: "共同底线"},
		{name: "readiness", input: DecisionInput{PrimaryGoal: GoalReadiness, Priorities: []string{"顺利推进"}, FlexibleUse: FlexibleUseUnknown, BuildableScope: BuildableScopeUnknown, ReturnFrequency: FrequencyUnknown, PreparationStage: StageInformation, PreparationItems: []PreparationItem{{Item: "宅基地边界核实", Status: PreparationMissing}, {Item: "家庭需求清单", Status: PreparationExisting}}}, want: "阻断"},
	}
	seen := map[string]bool{}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			normalized, err := NormalizeDecisionInput(test.input)
			if err != nil {
				t.Fatalf("NormalizeDecisionInput: %v", err)
			}
			brief := PlanDecision(normalized)
			if !strings.Contains(brief.Topic, test.want) {
				t.Fatalf("topic %q does not contain %q", brief.Topic, test.want)
			}
			if seen[brief.Topic] {
				t.Fatalf("duplicate recipe topic %q", brief.Topic)
			}
			seen[brief.Topic] = true
			if err := ValidateDecisionBrief(normalized, brief); err != nil {
				t.Fatalf("ValidateDecisionBrief: %v", err)
			}
		})
	}
}

func TestFamilyRecipeDoesNotInventConflictFromOnePerspective(t *testing.T) {
	input := DecisionInput{PrimaryGoal: GoalFamily, Priorities: []string{"先确认需求"}, FlexibleUse: FlexibleUseUnknown, BuildableScope: BuildableScopeUnknown, ReturnFrequency: FrequencyUnknown, FamilyPerspectives: []FamilyPerspective{{Role: "填写人", Request: "老人房放首层"}}}
	normalized, err := NormalizeDecisionInput(input)
	if err != nil {
		t.Fatal(err)
	}
	brief := PlanDecision(normalized)
	joined := brief.Topic + brief.Reason + brief.Question
	for _, block := range brief.Options {
		joined += block.Title + block.Body
	}
	if strings.Contains(joined, "家庭冲突") || strings.Contains(joined, "已经产生分歧") {
		t.Fatalf("invented family conflict: %s", joined)
	}
	if !strings.Contains(joined, "单一视角") {
		t.Fatalf("single perspective boundary missing: %s", joined)
	}
}

func TestUnknownDimensionsAndBudgetRemainUnknown(t *testing.T) {
	input := seasonalSpaceInput()
	input.ApproximateDimensions = DecisionDimensions{}
	input.BudgetWan = nil
	input.BudgetScope = nil
	normalized, err := NormalizeDecisionInput(input)
	if err != nil {
		t.Fatal(err)
	}
	facts := BuildDecisionFacts(normalized)
	if facts["site.approx_dimensions"] != "unknown" || facts["budget.wan"] != "unknown" {
		t.Fatalf("unknown facts changed: %#v", facts)
	}
	brief := PlanDecision(normalized)
	if err := ValidateDecisionBrief(normalized, brief); err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(brief)
	if strings.Contains(string(encoded), "150平方米") || strings.Contains(string(encoded), "100万元") {
		t.Fatalf("unknown value was fabricated: %s", encoded)
	}
}

func TestBuildDecisionPreviewAndFullPageContracts(t *testing.T) {
	input, err := NormalizeDecisionInput(seasonalSpaceInput())
	if err != nil {
		t.Fatal(err)
	}
	record := testReportForDecision(t, input)
	brief := PlanDecision(input)
	preview, err := BuildDecisionPreview(record, input, brief)
	if err != nil {
		t.Fatalf("BuildDecisionPreview: %v", err)
	}
	if len(preview.Pages) != 4 || preview.Pages[0].Number != 1 || preview.Pages[3].Number != 4 {
		t.Fatalf("preview pages = %#v", preview.Pages)
	}
	if preview.Meta.SchemaVersion != SchemaDecisionV5 || preview.Meta.ProtocolVersion != ProtocolDecisionV5 {
		t.Fatalf("preview meta = %#v", preview.Meta)
	}
	if err := ValidateDecisionSnapshot(preview, ModePreview); err != nil {
		t.Fatalf("ValidateDecisionSnapshot(preview): %v", err)
	}
	confirmation := DecisionConfirmation{DiagnosisID: brief.ID, InputHash: record.InputHash, Choice: ChoiceUndecided, Priorities: input.Priorities}
	confirmation, err = NormalizeDecisionConfirmation(input, confirmation)
	if err != nil {
		t.Fatal(err)
	}
	analysis := []ContentBlock{{Title: "日常使用", Body: "保留常住家庭的日常动线和隐私。"}, {Title: "团聚使用", Body: "节日团聚时再启用额外的居住功能。"}}
	budget := []ContentBlock{{Title: "待统一询价", Body: "庭院是否包含在计划投入内尚未确认。"}}
	full, err := BuildDecisionFull(record, input, confirmation, brief, analysis, budget)
	if err != nil {
		t.Fatalf("BuildDecisionFull: %v", err)
	}
	if len(full.Pages) != 10 || full.Pages[0].Number != 1 || full.Pages[9].Number != 10 {
		t.Fatalf("full page count/order = %d %#v", len(full.Pages), full.Pages)
	}
	if full.Confirmation == nil || full.Meta.Source != DecisionSourceV5 {
		t.Fatalf("full contract metadata = %#v", full)
	}
	if err := ValidateDecisionSnapshot(full, ModeFull); err != nil {
		t.Fatalf("ValidateDecisionSnapshot(full): %v", err)
	}
}

func TestDecisionValidationRejectsUnknownEvidenceAndOutOfBoundsPages(t *testing.T) {
	input, err := NormalizeDecisionInput(seasonalSpaceInput())
	if err != nil {
		t.Fatal(err)
	}
	brief := PlanDecision(input)
	brief.EvidenceRefs = append(brief.EvidenceRefs, "invented.fact")
	if !errors.Is(ValidateDecisionBrief(input, brief), ErrInvalidInput) {
		t.Fatal("unknown evidence ref was accepted")
	}
	record := testReportForDecision(t, input)
	brief = PlanDecision(input)
	snapshot, err := BuildDecisionPreview(record, input, brief)
	if err != nil {
		t.Fatal(err)
	}
	snapshot.Pages[0].Sources = []string{"invented.fact"}
	if !errors.Is(ValidateDecisionSnapshot(snapshot, ModePreview), ErrInvalidInput) {
		t.Fatal("unknown page source was accepted")
	}
	snapshot, err = BuildDecisionPreview(record, input, brief)
	if err != nil {
		t.Fatal(err)
	}
	snapshot.Pages = append(snapshot.Pages, SnapshotPage{Number: 5})
	if !errors.Is(ValidateDecisionSnapshot(snapshot, ModePreview), ErrInvalidInput) {
		t.Fatal("preview with five pages was accepted")
	}
}

func TestDecisionBriefAllowsBoundedEditorialEnhancementButKeepsIdentity(t *testing.T) {
	input, err := NormalizeDecisionInput(seasonalSpaceInput())
	if err != nil {
		t.Fatal(err)
	}
	brief := PlanDecision(input)
	brief.Reason += " 这个判断只用于组织后续比较。"
	brief.Question = "在两种使用情景中，哪一种代价更不能接受？"
	brief.Options[0].Body += " 后续需让设计师验证。"
	if err := ValidateDecisionBrief(input, brief); err != nil {
		t.Fatalf("bounded enhancement rejected: %v", err)
	}
	brief.Options[0].Title = "完全不同的方向"
	if !errors.Is(ValidateDecisionBrief(input, brief), ErrInvalidInput) {
		t.Fatal("changed option identity was accepted")
	}
}

func TestFullReportRejectsInventedUnknownPrice(t *testing.T) {
	input := seasonalSpaceInput()
	input.BudgetWan = nil
	input.BudgetScope = nil
	normalized, err := NormalizeDecisionInput(input)
	if err != nil {
		t.Fatal(err)
	}
	record := testReportForDecision(t, normalized)
	brief := PlanDecision(normalized)
	confirmation, err := NormalizeDecisionConfirmation(normalized, DecisionConfirmation{
		DiagnosisID: brief.ID, InputHash: record.InputHash, Choice: ChoiceUndecided,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = BuildDecisionFull(record, normalized, confirmation, brief,
		[]ContentBlock{{Title: "日常", Body: "保留日常使用。"}, {Title: "团聚", Body: "保留团聚使用。"}},
		[]ContentBlock{{Title: "预算", Body: "预算是100万元。"}})
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("invented price error = %v", err)
	}
}

func TestDecisionFactBlocksUseCustomerFacingChinese(t *testing.T) {
	input, err := NormalizeDecisionInput(seasonalSpaceInput())
	if err != nil {
		t.Fatal(err)
	}
	record := testReportForDecision(t, input)
	brief := PlanDecision(input)
	preview, err := BuildDecisionPreview(record, input, brief)
	if err != nil {
		t.Fatal(err)
	}
	for _, block := range preview.Pages[1].Blocks {
		visible := block.Title + " " + block.Body
		for _, internal := range []string{"household.", "site.", "intent.", "budget.", "unknown", "holidays", "space"} {
			if strings.Contains(visible, internal) {
				t.Fatalf("customer-facing fact leaked %q in %q", internal, visible)
			}
		}
	}
	if got := preview.Pages[1].Blocks[0].Title; got != "常住人数" {
		t.Fatalf("first evidence title = %q", got)
	}
	joined, _ := json.Marshal(preview.Pages[1].Blocks)
	if !strings.Contains(string(joined), "节假日返乡") || !strings.Contains(string(joined), "可接受兼用") {
		t.Fatalf("enum values were not translated: %s", joined)
	}
}

func TestDecisionConfirmationChoiceRulesAndPaidRecommendation(t *testing.T) {
	space, err := NormalizeDecisionInput(seasonalSpaceInput())
	if err != nil {
		t.Fatal(err)
	}
	record := testReportForDecision(t, space)
	brief := PlanDecision(space)
	build := func(choice string) DecisionSnapshot {
		confirmation, normalizeErr := NormalizeDecisionConfirmation(space, DecisionConfirmation{
			DiagnosisID: brief.ID, InputHash: record.InputHash, Choice: choice,
			Priorities: []string{"节日团聚", "父母居住舒适", "投入可控"},
		})
		if normalizeErr != nil {
			t.Fatalf("normalize %s: %v", choice, normalizeErr)
		}
		snapshot, buildErr := BuildDecisionFull(record, space, confirmation, brief,
			[]ContentBlock{{Title: "A", Body: "方向A的具体分析。"}, {Title: "A条件", Body: "方向A的成立条件。"}, {Title: "B", Body: "方向B的具体分析。"}, {Title: "B条件", Body: "方向B的成立条件。"}},
			[]ContentBlock{{Title: "询价范围", Body: "先确认庭院是否纳入同一口径。"}})
		if buildErr != nil {
			t.Fatalf("build %s: %v", choice, buildErr)
		}
		return snapshot
	}
	a := build(ChoiceOptionA)
	b := build(ChoiceOptionB)
	if a.Pages[0].Blocks[0].Body == b.Pages[0].Blocks[0].Body || !strings.Contains(a.Pages[0].Blocks[0].Body, brief.Options[0].Title) || !strings.Contains(b.Pages[0].Blocks[0].Body, brief.Options[1].Title) {
		t.Fatalf("choice did not affect recommendation: A=%q B=%q", a.Pages[0].Blocks[0].Body, b.Pages[0].Blocks[0].Body)
	}
	wantPriorities := "节日团聚 / 父母居住舒适 / 投入可控"
	if a.Pages[7].Blocks[0].Body != wantPriorities || a.Pages[9].Blocks[1].Body != wantPriorities {
		t.Fatalf("confirmed priorities not propagated: p8=%q p10=%q", a.Pages[7].Blocks[0].Body, a.Pages[9].Blocks[1].Body)
	}

	budget := DecisionInput{PrimaryGoal: GoalBudget, Priorities: []string{"投入可控"}, FlexibleUse: FlexibleUseUnknown, BuildableScope: BuildableScopeUnknown, ReturnFrequency: FrequencyUnknown, BudgetScope: []DecisionScopeItem{{Item: "主体", Status: ScopeIncluded}}}
	budget, err = NormalizeDecisionInput(budget)
	if err != nil {
		t.Fatalf("unknown budget should be accepted: %v", err)
	}
	_, budgetHash, _ := MarshalDecisionInput(budget)
	budgetBrief := PlanDecision(budget)
	if err = ValidateDecisionBrief(budget, budgetBrief); err != nil {
		t.Fatalf("unknown budget recipe invalid: %v", err)
	}
	budgetRecord := testReportForDecision(t, budget)
	if _, err = BuildDecisionPreview(budgetRecord, budget, budgetBrief); err != nil {
		t.Fatalf("unknown budget preview failed: %v", err)
	}
	if _, err = NormalizeDecisionConfirmation(budget, DecisionConfirmation{DiagnosisID: budgetBrief.ID, InputHash: budgetHash, Choice: ChoiceOptionA}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("non-space option choice error = %v", err)
	}
	if _, err = NormalizeDecisionConfirmation(budget, DecisionConfirmation{DiagnosisID: budgetBrief.ID, InputHash: budgetHash, Choice: ChoiceConfirm}); err != nil {
		t.Fatalf("budget confirm rejected: %v", err)
	}
}

func TestDecisionActionsDifferByRecipeAndUseInput(t *testing.T) {
	inputs := []DecisionInput{
		seasonalSpaceInput(),
		{PrimaryGoal: GoalBudget, Priorities: []string{"投入可控"}, FlexibleUse: FlexibleUseUnknown, BuildableScope: BuildableScopeUnknown, ReturnFrequency: FrequencyUnknown, BudgetScope: []DecisionScopeItem{{Item: "庭院", Status: ScopeUnknown}}},
		{PrimaryGoal: GoalFamily, Priorities: []string{"全家共同确认"}, FlexibleUse: FlexibleUseUnknown, BuildableScope: BuildableScopeUnknown, ReturnFrequency: FrequencyUnknown, FamilyPerspectives: []FamilyPerspective{{Role: "父母", Request: "首层日常起居"}, {Role: "子女", Request: "节日团聚"}}},
		{PrimaryGoal: GoalReadiness, Priorities: []string{"顺利推进"}, FlexibleUse: FlexibleUseUnknown, BuildableScope: BuildableScopeUnknown, ReturnFrequency: FrequencyUnknown, PreparationStage: StageInformation, PreparationItems: []PreparationItem{{Item: "宅基地边界核实", Status: PreparationMissing}, {Item: "家庭需求清单", Status: PreparationExisting}}},
	}
	seen := map[string]bool{}
	for _, input := range inputs {
		normalized, err := NormalizeDecisionInput(input)
		if err != nil {
			t.Fatal(err)
		}
		actions := actionBlocks(normalized, PlanDecision(normalized))
		encoded, _ := json.Marshal(actions)
		value := string(encoded)
		if seen[value] {
			t.Fatalf("recipe reused identical actions: %s", value)
		}
		seen[value] = true
		switch normalized.PrimaryGoal {
		case GoalBudget:
			if !strings.Contains(value, "庭院") || !strings.Contains(value, "询价") {
				t.Fatalf("budget actions not specific: %s", value)
			}
		case GoalFamily:
			if !strings.Contains(value, "父母") || !strings.Contains(value, "家庭讨论") {
				t.Fatalf("family actions not specific: %s", value)
			}
		case GoalReadiness:
			if !strings.Contains(value, "宅基地边界核实") || !strings.Contains(value, "已具备") {
				t.Fatalf("readiness actions not specific: %s", value)
			}
		}
	}
}

func TestDecisionInputCapacityMatchesReportContract(t *testing.T) {
	budget := DecisionInput{PrimaryGoal: GoalBudget, Priorities: []string{"投入可控"}, FlexibleUse: FlexibleUseUnknown, BuildableScope: BuildableScopeUnknown, ReturnFrequency: FrequencyUnknown}
	for index := 0; index < 8; index++ {
		budget.BudgetScope = append(budget.BudgetScope, DecisionScopeItem{Item: "范围" + string(rune('甲'+index)), Status: ScopeUnknown})
	}
	if _, err := NormalizeDecisionInput(budget); err != nil {
		t.Fatalf("eight budget items rejected: %v", err)
	}
	budget.BudgetScope = append(budget.BudgetScope, DecisionScopeItem{Item: "第九项", Status: ScopeUnknown})
	if _, err := NormalizeDecisionInput(budget); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("nine budget items error = %v", err)
	}

	readiness := DecisionInput{PrimaryGoal: GoalReadiness, Priorities: []string{"顺利推进"}, FlexibleUse: FlexibleUseUnknown, BuildableScope: BuildableScopeUnknown, ReturnFrequency: FrequencyUnknown, PreparationStage: StageInformation}
	for index := 0; index < 10; index++ {
		readiness.PreparationItems = append(readiness.PreparationItems, PreparationItem{Item: "资料" + string(rune('甲'+index)), Status: PreparationUnknown})
	}
	if _, err := NormalizeDecisionInput(readiness); err != nil {
		t.Fatalf("ten preparation items rejected: %v", err)
	}
	readiness.PreparationItems = append(readiness.PreparationItems, PreparationItem{Item: "第十一项", Status: PreparationUnknown})
	if _, err := NormalizeDecisionInput(readiness); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("eleven preparation items error = %v", err)
	}

	longUnknown := strings.Repeat("范围", 19)
	budget.BudgetScope = budget.BudgetScope[:8]
	for index := range budget.BudgetScope {
		budget.BudgetScope[index].Item = longUnknown + string(rune('甲'+index))
	}
	normalized, err := NormalizeDecisionInput(budget)
	if err != nil {
		t.Fatalf("maximum valid unknown items rejected: %v", err)
	}
	if count := len([]rune(strings.Join(decisionUnknowns(normalized), "；"))); count > 500 {
		t.Fatalf("combined unknown note exceeds page contract: %d", count)
	}
}

func TestChineseMeasurementClaimsAreRejectedWithoutBlockingOrdinaryWords(t *testing.T) {
	for _, value := range []string{"预算大约一百万元", "建筑边长二十米", "面积约三百平方米"} {
		if safeAgentNarrative(value) {
			t.Fatalf("Chinese measurement claim accepted: %q", value)
		}
	}
	for _, value := range []string{"统一询价口径", "一样的家庭需求", "万一条件发生变化就重新确认"} {
		if !safeAgentNarrative(value) {
			t.Fatalf("ordinary wording rejected: %q", value)
		}
	}

	input := seasonalSpaceInput()
	input.ApproximateDimensions = DecisionDimensions{}
	input.BudgetWan = nil
	input.BudgetScope = nil
	normalized, err := NormalizeDecisionInput(input)
	if err != nil {
		t.Fatal(err)
	}
	brief := PlanDecision(normalized)
	brief.Reason = "可建边长二十米，预算一百万元。"
	if !errors.Is(ValidateDecisionBrief(normalized, brief), ErrInvalidInput) {
		t.Fatal("Chinese fabricated values passed decision validation")
	}
}

func TestDecisionConfirmationOnlyReordersFrozenPriorities(t *testing.T) {
	input, err := NormalizeDecisionInput(seasonalSpaceInput())
	if err != nil {
		t.Fatal(err)
	}
	_, inputHash, err := MarshalDecisionInput(input)
	if err != nil {
		t.Fatal(err)
	}
	brief := PlanDecision(input)
	base := DecisionConfirmation{DiagnosisID: brief.ID, InputHash: inputHash, Choice: ChoiceUndecided}

	reversed := base
	reversed.Priorities = []string{input.Priorities[2], input.Priorities[1], input.Priorities[0]}
	normalized, err := NormalizeDecisionConfirmation(input, reversed)
	if err != nil {
		t.Fatalf("reordered priorities rejected: %v", err)
	}
	if strings.Join(normalized.Priorities, "|") != strings.Join(reversed.Priorities, "|") {
		t.Fatalf("reordered priorities not preserved: %#v", normalized.Priorities)
	}

	added := base
	added.Priorities = append([]string(nil), input.Priorities...)
	added.Priorities[2] = "新增优先级"
	if _, err = NormalizeDecisionConfirmation(input, added); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("added priority error = %v", err)
	}

	removed := base
	removed.Priorities = append([]string(nil), input.Priorities[:2]...)
	if _, err = NormalizeDecisionConfirmation(input, removed); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("removed priority error = %v", err)
	}
}

func TestDecisionBriefCannotRemoveCanonicalUnknowns(t *testing.T) {
	input, err := NormalizeDecisionInput(seasonalSpaceInput())
	if err != nil {
		t.Fatal(err)
	}
	brief := PlanDecision(input)
	if len(brief.Unknowns) == 0 {
		t.Fatal("fixture must contain unknowns")
	}
	brief.Unknowns = brief.Unknowns[:len(brief.Unknowns)-1]
	if !errors.Is(ValidateDecisionBrief(input, brief), ErrInvalidInput) {
		t.Fatal("brief with removed unknown was accepted")
	}
}

func TestFullyKnownDecisionBuildsFourAndTenPages(t *testing.T) {
	input := seasonalSpaceInput()
	input.BuildableScope = BuildableScopeKnown
	input.BuildableConstraints = "用户已核对宅基地边界资料"
	input.BudgetScope = []DecisionScopeItem{{Item: "主体", Status: ScopeIncluded}, {Item: "室内装修", Status: ScopeIncluded}, {Item: "庭院", Status: ScopeExcluded}}
	normalized, err := NormalizeDecisionInput(input)
	if err != nil {
		t.Fatal(err)
	}
	brief := PlanDecision(normalized)
	if len(brief.Unknowns) != 0 {
		t.Fatalf("fully known fixture has unknowns: %#v", brief.Unknowns)
	}
	record := testReportForDecision(t, normalized)
	preview, err := BuildDecisionPreview(record, normalized, brief)
	if err != nil || len(preview.Pages) != 4 {
		t.Fatalf("fully known preview: pages=%d err=%v", len(preview.Pages), err)
	}
	confirmation, err := NormalizeDecisionConfirmation(normalized, DecisionConfirmation{DiagnosisID: brief.ID, InputHash: record.InputHash, Choice: ChoiceUndecided})
	if err != nil {
		t.Fatal(err)
	}
	full, err := BuildDecisionFull(record, normalized, confirmation, brief,
		[]ContentBlock{{Title: "方向A", Body: "核对日常使用取舍。"}, {Title: "方向A条件", Body: "说明适用条件。"}, {Title: "方向B", Body: "核对团聚使用取舍。"}, {Title: "方向B条件", Body: "说明适用条件。"}},
		[]ContentBlock{{Title: "询价口径", Body: "按已确认范围向各方询价。"}})
	if err != nil || len(full.Pages) != 10 {
		t.Fatalf("fully known full report: pages=%d err=%v", len(full.Pages), err)
	}
	boundary := "当前已填写关键事项，仍需以测绘、审批和正式询价凭证核验"
	if full.Pages[7].Blocks[2].Body != boundary || full.Pages[9].Blocks[2].Body != boundary {
		t.Fatalf("known-case boundary missing: p8=%q p10=%q", full.Pages[7].Blocks[2].Body, full.Pages[9].Blocks[2].Body)
	}
}
