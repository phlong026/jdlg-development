package report

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func houseDecisionInput(floors, bedrooms int, flexible string) DecisionInput {
	elder := true
	return DecisionInput{
		PrimaryGoal:           GoalSpace,
		Region:                "泉州南安",
		ApproximateDimensions: DecisionDimensions{WidthM: floatPointer(12), DepthM: floatPointer(16)},
		BuildableScope:        BuildableScopeUnknown,
		PermanentCount:        intPointer(2), ReturningCount: intPointer(4), ReturnFrequency: FrequencyHolidays,
		Floors: intPointer(floors), Bedrooms: intPointer(bedrooms), FlexibleUse: flexible,
		Priorities: []string{"父母居住舒适", "家庭团聚", "投入可控"},
		BudgetWan:  floatPointer(120),
		House: &HouseRequirements{
			Style: "新闽派", ElderRoom: &elder, RoadSide: "south", Orientation: "坐北朝南",
			Courtyard: "yes", Parking: "yes", StartWindow: "一年内",
			Members: []HouseMember{{Role: "父母", Count: 2, Residence: "permanent", Needs: []string{"首层起居"}}, {Role: "子女家庭", Count: 4, Residence: "returning"}},
			Rooms:   []HouseRoomRequirement{{ID: "elder", Name: "老人房", Count: 1, PreferredFloor: 1, Required: true}},
		},
	}
}

func houseRecord(t *testing.T, input DecisionInput) *Report {
	t.Helper()
	raw, hash, err := MarshalDecisionInput(input)
	if err != nil {
		t.Fatal(err)
	}
	return &Report{ID: "house-report-test", InputHash: hash, InputJSON: raw, ProtocolVersion: ProtocolHouseV1, LeaseToken: "lease-house", CreatedAt: time.Date(2026, 9, 20, 0, 0, 0, 0, time.Local)}
}

func TestPlanHouseDecisionChangesWithHouseholdAndRejectsFlexibleUse(t *testing.T) {
	seasonal := houseDecisionInput(2, 4, FlexibleUseYes)
	seasonalBrief := PlanHouseDecision(seasonal)
	if err := ValidateHouseBrief(seasonal, seasonalBrief); err != nil {
		t.Fatal(err)
	}

	permanent := houseDecisionInput(3, 6, FlexibleUseNo)
	permanent.PermanentCount, permanent.ReturningCount = intPointer(6), intPointer(0)
	permanent.ReturnFrequency = FrequencyNone
	permanent.House.Members = []HouseMember{{Role: "长期同住家庭", Count: 6, Residence: "permanent", Needs: []string{"独立居住"}}}
	permanentBrief := PlanHouseDecision(permanent)
	if err := ValidateHouseBrief(permanent, permanentBrief); err != nil {
		t.Fatal(err)
	}
	if seasonalBrief.ID == permanentBrief.ID || seasonalBrief.Topic == permanentBrief.Topic {
		t.Fatalf("different households produced same diagnosis: %#v %#v", seasonalBrief, permanentBrief)
	}
	encoded, _ := json.Marshal(permanentBrief)
	for _, option := range permanentBrief.Options {
		if strings.Contains(option.Title+option.Body, "兼用") || strings.Contains(option.Title+option.Body, "转换房") {
			t.Fatalf("rejected flexible use was recommended: %s", encoded)
		}
	}
}

func TestBuildHousePreviewUsesFourStableFrameworkPages(t *testing.T) {
	input := houseDecisionInput(2, 4, FlexibleUseYes)
	record := houseRecord(t, input)
	brief := PlanHouseDecision(input)
	snapshot, err := BuildHousePreview(record, input, brief)
	if err != nil {
		t.Fatal(err)
	}
	wantKinds := []string{"house_profile", "house_constraints", "house_goals", "house_confirmation"}
	if len(snapshot.Pages) != len(wantKinds) {
		t.Fatalf("pages=%d", len(snapshot.Pages))
	}
	for i, kind := range wantKinds {
		if snapshot.Pages[i].Kind != kind || snapshot.Pages[i].Number != i+1 || snapshot.Pages[i].Access != "free" {
			t.Fatalf("page %d=%#v", i+1, snapshot.Pages[i])
		}
	}
	if snapshot.Meta.SchemaVersion != SchemaHouseV1 || snapshot.Meta.Source != HouseSource || snapshot.House != nil {
		t.Fatalf("snapshot=%#v", snapshot)
	}
	if err := ValidateHouseSnapshot(snapshot, ModePreview); err != nil {
		t.Fatal(err)
	}

	snapshot.Pages = append(snapshot.Pages, SnapshotPage{Number: 5})
	if !errors.Is(ValidateHouseSnapshot(snapshot, ModePreview), ErrInvalidInput) {
		t.Fatal("five-page preview accepted")
	}
}

func TestValidateHouseSnapshotRejectsWrongBedroomTotalAndSchemeVersion(t *testing.T) {
	input := houseDecisionInput(2, 4, FlexibleUseYes)
	record := houseRecord(t, input)
	preview, err := BuildHousePreview(record, input, PlanHouseDecision(input))
	if err != nil {
		t.Fatal(err)
	}
	content := validHouseContentFixture(input)
	if err := validateHouseContent(buildHouseFacts(input), content); err != nil {
		for _, scheme := range content.Schemes {
			t.Logf("scheme %s safe=%v/%v/%v/%v", scheme.ID, safeAgentNarrative(scheme.Strategy), safeAgentNarrative(scheme.Gain), safeAgentNarrative(scheme.Tradeoff), safeAgentNarrative(scheme.Facade))
		}
		t.Fatalf("fixture content invalid: %v", err)
	}
	full := buildHouseFullSnapshotFixture(t, record, input, preview, content)
	if err := ValidateHouseSnapshot(full, ModeFull); err != nil {
		t.Fatalf("valid full: %v", err)
	}

	full.House.Schemes[0].Floors[0].Rooms[0].Count++
	if !errors.Is(ValidateHouseSnapshot(full, ModeFull), ErrInvalidInput) {
		t.Fatal("wrong bedroom total accepted")
	}
	full = buildHouseFullSnapshotFixture(t, record, input, preview, validHouseContentFixture(input))
	full.House.Images[0].SchemeVersion = "wrong-version"
	if !errors.Is(ValidateHouseSnapshot(full, ModeFull), ErrInvalidInput) {
		t.Fatal("cross-version image accepted")
	}
}

func TestHouseFullSupportsThreeDistinctSchemesAndRejectsWrongComparisonWidth(t *testing.T) {
	input := houseDecisionInput(1, 3, FlexibleUseYes)
	record := houseRecord(t, input)
	preview, err := BuildHousePreview(record, input, PlanHouseDecision(input))
	if err != nil {
		t.Fatal(err)
	}
	content := validHouseContentFixture(input)
	third := content.Schemes[1]
	third.ID = "privacy"
	third.Version = "privacy-v1"
	third.Name = "私密分区型"
	third.Strategy = "强化长期同住的安静分区"
	third.Gain = "不同成员的作息干扰更少"
	third.Tradeoff = "公共空间联系需要更谨慎"
	content.Schemes = append(content.Schemes, third)
	content.Plan.SchemeCount = 3
	for i := range content.Images {
		if content.Images[i].SchemeID != "gathering" {
			continue
		}
		asset := content.Images[i]
		asset.SchemeID = third.ID
		asset.SchemeVersion = third.Version
		asset.ID = strings.Replace(asset.ID, "gathering", third.ID, 1)
		asset.Asset = strings.Replace(asset.Asset, "gathering", third.ID, 1)
		if asset.ReferenceID != "" {
			asset.ReferenceID = strings.Replace(asset.ReferenceID, "gathering", third.ID, 1)
		}
		content.Images = append(content.Images, asset)
	}
	content.Comparison = [][]string{{"维度", "日常舒适型", "团聚均衡型", "私密分区型"}, {"日常使用", "优先", "均衡", "安静分区"}, {"团聚使用", "需准备", "优先", "保持边界"}}
	if err = validateHouseContent(buildHouseFacts(input), content); err != nil {
		for _, asset := range content.Images {
			t.Logf("asset=%s scheme=%s kind=%s ref=%s", asset.ID, asset.SchemeID, asset.Kind, asset.ReferenceID)
		}
		t.Fatalf("three-scheme content: %v", err)
	}
	full := buildHouseFullSnapshotFixture(t, record, input, preview, content)
	if len(full.Pages) != 15 {
		t.Fatalf("pages=%d", len(full.Pages))
	}
	if err = ValidateHouseSnapshot(full, ModeFull); err != nil {
		t.Fatal(err)
	}
	full.House.Comparison[0] = full.House.Comparison[0][:3]
	if !errors.Is(ValidateHouseSnapshot(full, ModeFull), ErrInvalidInput) {
		t.Fatal("wrong comparison width accepted")
	}
}

func validHouseContentFixture(input DecisionInput) HouseContent {
	schemes := []HouseScheme{
		{ID: "daily", Version: "daily-v1", Name: "日常舒适型", Strategy: "优先保证父母日常生活", Gain: "首层日常使用完整", Tradeoff: "团聚空间需要预先安排", Facade: "新闽派简洁体量", CostSensitive: []string{"屋面", "门廊"}, Assumptions: []string{"可建边界待核实"}, EvidenceRefs: []string{"intent.floors", "intent.bedrooms"}},
		{ID: "gathering", Version: "gathering-v1", Name: "团聚均衡型", Strategy: "兼顾日常与节日团聚", Gain: "团聚时公共空间更完整", Tradeoff: "日常需承担更多维护", Facade: "新闽派院落感", CostSensitive: []string{"大开间", "外廊"}, Assumptions: []string{"可建边界待核实"}, EvidenceRefs: []string{"household.returning_count", "intent.priorities"}},
	}
	for si := range schemes {
		for floor := 1; floor <= *input.Floors; floor++ {
			count := *input.Bedrooms / *input.Floors
			if floor <= *input.Bedrooms%*input.Floors {
				count++
			}
			rooms := []HouseRoom{}
			if floor == 1 {
				count--
				rooms = append(rooms, HouseRoom{Name: "老人房", Kind: "elder_bedroom", Count: 1, RequirementID: "elder"})
			}
			if count > 0 {
				rooms = append(rooms, HouseRoom{Name: "卧室", Kind: "bedroom", Count: count, RequirementID: "bedrooms"})
			}
			schemes[si].Floors = append(schemes[si].Floors, HouseFloor{Number: floor, Name: "第" + string(rune('0'+floor)) + "层", Summary: "按家庭使用情景组织空间", Rooms: rooms, Circulation: "减少卧室与公共活动相互穿行", StairPosition: "靠近公共交通核"})
		}
	}
	var images []HouseImageAsset
	for _, scheme := range schemes {
		for _, floor := range scheme.Floors {
			images = append(images, HouseImageAsset{ID: scheme.ID + "-floor-" + string(rune('0'+floor.Number)), Asset: "house/test/" + scheme.ID + "-floor.png", SchemeID: scheme.ID, SchemeVersion: scheme.Version, Kind: "floor_plan", Floor: floor.Number, Width: 2048, Height: 1152, Model: "fixture", SHA256: strings.Repeat("a", 64)})
		}
		images = append(images, HouseImageAsset{ID: scheme.ID + "-massing", Asset: "house/test/" + scheme.ID + "-massing.png", SchemeID: scheme.ID, SchemeVersion: scheme.Version, Kind: "massing", Width: 2048, Height: 1152, Model: "fixture", SHA256: strings.Repeat("b", 64), ReferenceID: scheme.ID + "-floor-1"})
		images = append(images, HouseImageAsset{ID: scheme.ID + "-exterior", Asset: "house/test/" + scheme.ID + "-exterior.png", SchemeID: scheme.ID, SchemeVersion: scheme.Version, Kind: "exterior", Width: 2048, Height: 1152, Model: "fixture", SHA256: strings.Repeat("c", 64), ReferenceID: scheme.ID + "-massing"})
	}
	return HouseContent{Plan: HousePlan{SchemeCount: 2, Focus: []string{"日常居住", "节日团聚"}}, Schemes: schemes, Comparison: [][]string{{"方案", "日常", "团聚"}, {"日常舒适型", "优先", "需准备"}, {"团聚均衡型", "均衡", "优先"}}, Budget: []ContentBlock{{Title: "统一询价口径", Body: "分别核对主体、装修与庭院范围。"}, {Title: "成本敏感项", Body: "分别核对屋面、门廊与大开间的影响。"}}, DesignerBrief: []ContentBlock{{Title: "必须满足", Body: "卧室总数与首层老人房不得减少。"}, {Title: "仍待核实", Body: "可建边界和相邻条件需要专业复核。"}}, Actions: []HouseAction{{Task: "核实可建边界", Owner: "业主与专业人员", Prerequisite: "宅基地资料", Evidence: "形成可核对的边界资料"}, {Task: "统一询价范围", Owner: "业主", Prerequisite: "方案成本敏感项", Evidence: "形成同口径询价清单"}}, Images: images}
}

func buildHouseFullSnapshotFixture(t *testing.T, record *Report, input DecisionInput, preview DecisionSnapshot, content HouseContent) DecisionSnapshot {
	t.Helper()
	confirmation := DecisionConfirmation{DiagnosisID: preview.Diagnosis.ID, InputHash: record.InputHash, Choice: ChoiceUndecided, Priorities: input.Priorities}
	full, err := buildHouseFullSnapshot(record, input, confirmation, preview, content)
	if err != nil {
		t.Fatal(err)
	}
	return full
}
