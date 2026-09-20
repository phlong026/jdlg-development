package report

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path"
	"reflect"
	"sort"
	"strconv"
	"strings"
)

const houseConceptDisclaimer = "本报告中的平面、体量和外观均为前期概念表达，不是施工图、审批图或精确工程测量结果；需由具备资质的专业人员结合实测和当地规范复核。"

func PlanHouseDecision(input DecisionInput) DecisionBrief {
	normalized, err := NormalizeDecisionInput(input)
	if err != nil || normalized.House == nil {
		return DecisionBrief{}
	}
	input = normalized
	refs := []string{"household.permanent_count", "household.returning_count", "household.return_frequency", "intent.floors", "intent.bedrooms", "intent.flexible_use", "intent.priorities", "house.style"}
	brief := DecisionBrief{
		Recipe:       GoalSpace,
		Topic:        "先确定整屋的日常居住底线与团聚弹性",
		Reason:       "整屋方案需要同时回应常住家庭、返乡使用、逐层房间分配和预算边界，先把不可交换的生活需求固定下来，再比较有实质差异的组织方向。",
		EvidenceRefs: refs,
		Options: []ContentBlock{
			{Title: "日常舒适方向", Body: "优先减少常住成员的上下楼和相互干扰，再为返乡使用保留明确的启用条件。"},
			{Title: "团聚均衡方向", Body: "在不削弱日常居住底线的前提下，加强节日聚会、待客与多人共处的空间组织。"},
		},
		Unknowns: houseUnknowns(input),
		Question: "请确认这些家庭事实和设计底线是否准确；确认后将据此生成整屋方案、逐层概念平面、体量与外观方向。",
	}
	if input.FlexibleUse == FlexibleUseNo {
		brief.Topic = "先保证独立居住底线，再组织整屋公共空间"
		brief.Reason = "用户已明确拒绝兼用，整屋方案必须按独立房间需求分配各层空间，再比较日常动线、公共活动和团聚接待的不同组织方式。"
		brief.Options[0] = ContentBlock{Title: "独立居住优先", Body: "保留全部独立房间与隐私底线，比较不同楼层分配对日常生活的影响。"}
		brief.Options[1] = ContentBlock{Title: "公共空间优先级", Body: "不减少独立房间，只比较客餐厅、家庭厅和交通空间如何服务不同使用情景。"}
	} else if input.ReturnFrequency == FrequencyNone || input.ReturningCount == nil || *input.ReturningCount == 0 {
		brief.Topic = "先确定长期同住的私密边界与共享关系"
		brief.Reason = "当前输入以长期同住为主，整屋方案应围绕独立居住、卫浴使用、上下楼动线和公共活动的相互影响展开。"
	}
	brief.ID = houseBriefID(input, brief)
	return brief
}

func ValidateHouseBrief(input DecisionInput, brief DecisionBrief) error {
	normalized, err := NormalizeDecisionInput(input)
	if err != nil || normalized.House == nil {
		return ErrInvalidInput
	}
	input = normalized
	base := PlanHouseDecision(input)
	if brief.ID == "" || brief.ID != base.ID || brief.Recipe != base.Recipe || brief.Topic != base.Topic ||
		!sameStrings(brief.EvidenceRefs, base.EvidenceRefs) || !sameStrings(brief.Unknowns, base.Unknowns) || !sameBlockTitles(brief.Options, base.Options) ||
		len(brief.Options) != 2 || !boundedCleanText(brief.Reason, 8, 600) || !boundedCleanText(brief.Question, 8, 240) || !validBlocks(brief.Options, 2, 80, 500) {
		return ErrInvalidInput
	}
	facts := buildHouseFacts(input)
	for _, ref := range brief.EvidenceRefs {
		if _, ok := facts[ref]; !ok {
			return ErrInvalidInput
		}
	}
	if input.FlexibleUse == FlexibleUseNo {
		for _, option := range brief.Options {
			if strings.Contains(option.Title+option.Body, "兼用") || strings.Contains(option.Title+option.Body, "转换房") {
				return ErrInvalidInput
			}
		}
	}
	return nil
}

func BuildHousePreview(record *Report, input DecisionInput, brief DecisionBrief) (DecisionSnapshot, error) {
	if err := validateHouseRecord(record, input); err != nil {
		return DecisionSnapshot{}, err
	}
	if err := ValidateHouseBrief(input, brief); err != nil {
		return DecisionSnapshot{}, err
	}
	facts := buildHouseFacts(input)
	profileRefs := existingHouseRefs(facts, "household.permanent_count", "household.returning_count", "household.return_frequency", "house.member.01", "house.member.02")
	constraintRefs := existingHouseRefs(facts, "site.region", "site.approx_dimensions", "site.buildable_scope", "site.buildable_constraints", "house.road_side", "house.orientation", "house.courtyard", "house.parking")
	goalRefs := existingHouseRefs(facts, "intent.floors", "intent.bedrooms", "intent.flexible_use", "intent.priorities", "house.style", "house.first_floor_elder_room")
	pages := []SnapshotPage{
		{Number: 1, Access: "free", Kind: "house_profile", Title: "我家的居住画像", Subtitle: "先区分日常常住与返乡团聚两种真实使用情景。", Blocks: houseProfileBlocks(input), Sources: profileRefs},
		{Number: 2, Access: "free", Kind: "house_constraints", Title: "宅基地与建造条件", Subtitle: "已知条件用于限定方向，未知条件继续保留为专业核实项。", Blocks: houseConstraintBlocks(input), Sources: constraintRefs, Note: strings.Join(brief.Unknowns, "；")},
		{Number: 3, Access: "free", Kind: "house_goals", Title: "整屋设计目标与取舍", Subtitle: brief.Topic, Blocks: append([]ContentBlock{{Title: "核心判断", Body: brief.Reason}}, cloneBlocks(brief.Options)...), Sources: goalRefs},
		{Number: 4, Access: "free", Kind: "house_confirmation", Title: "确认后生成哪些成果", Subtitle: brief.Question, Blocks: []ContentBlock{{Title: "整屋概念方案", Body: "形成有实质差异的整屋方向，并逐层说明房间、动线、收益、代价和成立条件。"}, {Title: "图文与执行资料", Body: "生成对应的概念平面、体量、外观、方案比较、询价边界、设计任务书和行动清单。"}}, Sources: existingHouseRefs(facts, "intent.priorities", "intent.floors", "intent.bedrooms"), Disclaimer: houseConceptDisclaimer},
	}
	snapshot := DecisionSnapshot{Meta: houseMeta(record), Facts: facts, Recipe: GoalSpace, Diagnosis: brief, Pages: pages}
	if err := ValidateHouseSnapshot(snapshot, ModePreview); err != nil {
		return DecisionSnapshot{}, err
	}
	return snapshot, nil
}

func ValidateHouseSnapshot(snapshot DecisionSnapshot, mode string) error {
	if snapshot.Meta.SchemaVersion != SchemaHouseV1 || snapshot.Meta.ProtocolVersion != ProtocolHouseV1 || snapshot.Meta.Source != HouseSource || snapshot.Meta.ReportID == "" || snapshot.Meta.InputHash == "" || snapshot.Recipe != GoalSpace || len(snapshot.Facts) == 0 {
		return ErrInvalidInput
	}
	if snapshot.Diagnosis.ID == "" || snapshot.Diagnosis.Recipe != GoalSpace {
		return ErrInvalidInput
	}
	for key, value := range snapshot.Facts {
		if !boundedCleanText(key, 2, 120) || !boundedCleanText(value, 1, 500) {
			return ErrInvalidInput
		}
	}
	if mode == ModePreview {
		if len(snapshot.Pages) != 4 || snapshot.Confirmation != nil || snapshot.House != nil {
			return ErrInvalidInput
		}
		want := []string{"house_profile", "house_constraints", "house_goals", "house_confirmation"}
		return validateHousePages(snapshot, want, "free")
	}
	if mode != ModeFull || snapshot.Confirmation == nil || !oneOf(snapshot.Confirmation.Choice, ChoiceConfirm, ChoiceUndecided) || snapshot.Confirmation.DiagnosisID != snapshot.Diagnosis.ID || snapshot.Confirmation.InputHash != snapshot.Meta.InputHash || snapshot.House == nil {
		return ErrInvalidInput
	}
	if err := validateHouseContent(snapshot.Facts, *snapshot.House); err != nil {
		return err
	}
	wantKinds := []string{"house_profile", "house_constraints", "house_goals", "house_confirmation", "house_overview"}
	for _, scheme := range snapshot.House.Schemes {
		for range scheme.Floors {
			wantKinds = append(wantKinds, "house_floor_plan")
		}
		wantKinds = append(wantKinds, "house_exterior")
	}
	wantKinds = append(wantKinds, "house_comparison", "house_budget", "house_designer_brief", "house_actions")
	if err := validateHousePages(snapshot, wantKinds, "paid"); err != nil {
		return err
	}
	return validateHousePageBindings(snapshot)
}

func buildHouseFullSnapshot(record *Report, input DecisionInput, confirmation DecisionConfirmation, preview DecisionSnapshot, content HouseContent) (DecisionSnapshot, error) {
	if err := validateHouseRecord(record, input); err != nil || ValidateHouseSnapshot(preview, ModePreview) != nil || preview.Meta.ReportID != record.ID || preview.Meta.InputHash != record.InputHash || !reflect.DeepEqual(preview.Facts, buildHouseFacts(input)) || !reflect.DeepEqual(preview.Diagnosis, PlanHouseDecision(input)) {
		return DecisionSnapshot{}, ErrInvalidInput
	}
	if confirmation.DiagnosisID != preview.Diagnosis.ID || confirmation.InputHash != record.InputHash || !oneOf(confirmation.Choice, ChoiceConfirm, ChoiceUndecided) || !sameStringSet(confirmation.Priorities, input.Priorities) {
		return DecisionSnapshot{}, ErrConflict
	}
	if err := validateHouseContent(preview.Facts, content); err != nil {
		return DecisionSnapshot{}, err
	}
	pages := cloneHousePages(preview.Pages, "paid")
	overview := SnapshotPage{Access: "paid", Kind: "house_overview", Title: "整屋概念方案总览", Subtitle: "各方向使用相同家庭事实，但在空间组织、使用收益和需要接受的代价上保持实质差异。", Disclaimer: houseConceptDisclaimer}
	overview.Sources = existingHouseRefs(preview.Facts, "household.permanent_count", "household.returning_count", "intent.floors", "intent.bedrooms", "intent.priorities")
	for _, scheme := range content.Schemes {
		overview.Blocks = append(overview.Blocks, ContentBlock{Title: scheme.Name, Body: scheme.Strategy + "；主要收益：" + scheme.Gain + "；需要接受：" + scheme.Tradeoff})
	}
	pages = appendHousePage(pages, overview)
	assets := houseAssetsByKey(content.Images)
	for _, scheme := range content.Schemes {
		for _, floor := range scheme.Floors {
			asset := assets[houseAssetKey(scheme.ID, "floor_plan", floor.Number)]
			pages = appendHousePage(pages, SnapshotPage{Access: "paid", Kind: "house_floor_plan", Scheme: scheme.ID, Title: scheme.Name + "｜" + floor.Name + "概念平面", Subtitle: floor.Summary, Blocks: []ContentBlock{{Title: "空间配置", Body: houseRoomSummary(floor.Rooms)}, {Title: "动线组织", Body: floor.Circulation}, {Title: "楼梯位置", Body: floor.StairPosition}}, Sources: existingHouseRefs(preview.Facts, "intent.floors", "intent.bedrooms", "house.first_floor_elder_room", "intent.flexible_use"), Illustrations: []ReportIllustration{{Asset: asset.Asset, Title: floor.Name + "概念平面", Caption: "按结构化房间清单生成，用于前期讨论，实际尺寸与结构需专业复核。"}}, Disclaimer: houseConceptDisclaimer})
		}
		volume := assets[houseAssetKey(scheme.ID, "massing", 0)]
		facade := assets[houseAssetKey(scheme.ID, "exterior", 0)]
		pages = appendHousePage(pages, SnapshotPage{Access: "paid", Kind: "house_exterior", Scheme: scheme.ID, Title: scheme.Name + "｜体量与外观方向", Subtitle: scheme.Facade, Blocks: []ContentBlock{{Title: "方案收益", Body: scheme.Gain}, {Title: "方案代价", Body: scheme.Tradeoff}, {Title: "成本敏感项", Body: strings.Join(scheme.CostSensitive, "、")}}, Sources: existingHouseRefs(preview.Facts, "intent.floors", "house.style", "house.orientation", "house.courtyard", "house.parking"), Illustrations: []ReportIllustration{{Asset: volume.Asset, Title: "体量关系", Caption: "体量图与本方案逐层空间数据关联。"}, {Asset: facade.Asset, Title: "外观方向", Caption: "外观图引用同方案体量，仅表达风格和构成方向。"}}, Disclaimer: houseConceptDisclaimer})
	}
	pages = appendHousePage(pages,
		SnapshotPage{Access: "paid", Kind: "house_comparison", Title: "整屋方案比较与建议", Subtitle: "从家庭使用、空间组织、外观与成本敏感项比较，不强迫用户在证据不足时定案。", Sources: existingHouseRefs(preview.Facts, "intent.priorities", "intent.floors", "intent.bedrooms"), Table: content.Comparison, Disclaimer: houseConceptDisclaimer},
		SnapshotPage{Access: "paid", Kind: "house_budget", Title: "预算边界与统一询价清单", Subtitle: "预算内容随方案成本敏感项展开，不将未知范围写成正式报价。", Sources: houseBudgetSources(preview.Facts), Blocks: cloneBlocks(content.Budget)},
		SnapshotPage{Access: "paid", Kind: "house_designer_brief", Title: "交给设计师的需求任务书", Subtitle: "区分必须满足、优先满足、可协商和仍待核实的条件。", Sources: existingHouseRefs(preview.Facts, "intent.priorities", "intent.floors", "intent.bedrooms", "intent.flexible_use", "house.first_floor_elder_room"), Blocks: cloneBlocks(content.DesignerBrief), Disclaimer: houseConceptDisclaimer},
		SnapshotPage{Access: "paid", Kind: "house_actions", Title: "下一步行动计划", Subtitle: "每项任务写清负责角色、前置条件和完成依据。", Sources: existingHouseRefs(preview.Facts, "site.buildable_scope", "house.start_window", "budget.wan"), Actions: houseActionBlocks(content.Actions)},
	)
	full := DecisionSnapshot{Meta: houseMeta(record), Facts: cloneStringMap(preview.Facts), Recipe: GoalSpace, Diagnosis: preview.Diagnosis, Confirmation: &confirmation, Pages: pages, House: &content}
	if err := ValidateHouseSnapshot(full, ModeFull); err != nil {
		return DecisionSnapshot{}, err
	}
	return full, nil
}

func validateHouseRequirements(input DecisionInput) error {
	if input.House == nil || input.Floors == nil || input.Bedrooms == nil || *input.Floors < 1 || *input.Floors > 6 || *input.Bedrooms < 1 || *input.Bedrooms > 20 || !boundedCleanText(input.House.Style, 1, 80) {
		return ErrInvalidInput
	}
	if input.FlexibleUse == FlexibleUseNo {
		for _, room := range input.House.Rooms {
			if strings.Contains(room.Name, "兼用") || strings.Contains(room.Name, "转换") {
				return ErrInvalidInput
			}
		}
	}
	ids := map[string]bool{}
	for _, room := range input.House.Rooms {
		if !validIdentifier(room.ID, 64) || !boundedCleanText(room.Name, 1, 80) || room.Count < 1 || room.Count > 20 || room.PreferredFloor < 0 || room.PreferredFloor > *input.Floors || ids[room.ID] {
			return ErrInvalidInput
		}
		ids[room.ID] = true
	}
	return nil
}

func validateHouseRecord(record *Report, input DecisionInput) error {
	if record == nil || record.ID == "" || record.ProtocolVersion != ProtocolHouseV1 || record.InputHash == "" || validateHouseRequirements(input) != nil {
		return ErrInvalidInput
	}
	_, hash, err := MarshalDecisionInput(input)
	if err != nil || hash != record.InputHash {
		return ErrInvalidInput
	}
	return nil
}

func validateHouseContent(facts map[string]string, content HouseContent) error {
	floors, err1 := strconv.Atoi(facts["intent.floors"])
	bedrooms, err2 := strconv.Atoi(facts["intent.bedrooms"])
	if err1 != nil || err2 != nil || floors < 1 || floors > 3 || bedrooms < 1 || bedrooms > 12 || content.Plan.SchemeCount < 2 || content.Plan.SchemeCount > 3 || len(content.Schemes) != content.Plan.SchemeCount || len(content.Plan.Focus) < 1 || len(content.Plan.Focus) > 6 || !validBlocks(content.Budget, 12, 100, 900) || !validBlocks(content.DesignerBrief, 12, 100, 900) || len(content.Actions) < 1 || len(content.Actions) > 12 || !validHouseComparison(content.Comparison, len(content.Schemes)) {
		return ErrInvalidInput
	}
	seenSchemes := map[string]bool{}
	requirements, err := houseRequirementsFromFacts(facts)
	if err != nil {
		return err
	}
	for _, scheme := range content.Schemes {
		if !validIdentifier(scheme.ID, 64) || !validIdentifier(scheme.Version, 64) || seenSchemes[scheme.ID] || !boundedCleanText(scheme.Name, 2, 80) || !boundedCleanText(scheme.Strategy, 4, 500) || !boundedCleanText(scheme.Gain, 2, 500) || !boundedCleanText(scheme.Tradeoff, 2, 500) || !boundedCleanText(scheme.Facade, 2, 500) || len(scheme.Floors) != floors || len(scheme.CostSensitive) == 0 {
			return ErrInvalidInput
		}
		if !safeHouseNarrative(scheme.Strategy) || !safeHouseNarrative(scheme.Gain) || !safeHouseNarrative(scheme.Tradeoff) || !safeHouseNarrative(scheme.Facade) {
			return ErrInvalidInput
		}
		seenSchemes[scheme.ID] = true
		bedroomTotal := 0
		for i, floor := range scheme.Floors {
			if floor.Number != i+1 || !boundedCleanText(floor.Name, 2, 40) || !boundedCleanText(floor.Summary, 2, 500) || !boundedCleanText(floor.Circulation, 2, 500) || !boundedCleanText(floor.StairPosition, 2, 200) || len(floor.Rooms) == 0 {
				return ErrInvalidInput
			}
			for _, room := range floor.Rooms {
				if room.Count < 1 || room.Count > 20 || !boundedCleanText(room.Name, 1, 80) || !boundedCleanText(room.Kind, 2, 64) {
					return ErrInvalidInput
				}
				if room.Kind == "bedroom" || room.Kind == "elder_bedroom" {
					bedroomTotal += room.Count
				}
			}
		}
		if bedroomTotal != bedrooms {
			return ErrInvalidInput
		}
		if err := validateSchemeRequirements(facts, scheme, requirements); err != nil {
			return err
		}
	}
	if err := validateHouseImages(content); err != nil {
		return err
	}
	for _, action := range content.Actions {
		if !boundedCleanText(action.Task, 2, 200) || !boundedCleanText(action.Owner, 2, 100) || !boundedCleanText(action.Prerequisite, 2, 300) || !boundedCleanText(action.Evidence, 2, 300) {
			return ErrInvalidInput
		}
	}
	return nil
}

func safeHouseNarrative(value string) bool {
	if !boundedAgentText(value, 2, 500) || strings.Contains(value, "://") || chineseMeasurementClaimPattern.MatchString(value) {
		return false
	}
	for _, char := range value {
		if char >= '0' && char <= '9' {
			return false
		}
	}
	for _, claim := range []string{"确定可建", "符合审批", "准确造价", "施工图已完成", "正式报价"} {
		if strings.Contains(value, claim) {
			return false
		}
	}
	return true
}

func validHouseComparison(table [][]string, schemes int) bool {
	if len(table) < 3 || len(table) > 8 {
		return false
	}
	for _, row := range table {
		if len(row) != schemes+1 {
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

func validateHouseImages(content HouseContent) error {
	assets := map[string]HouseImageAsset{}
	for _, image := range content.Images {
		if image.ID == "" || assets[image.ID].ID != "" || !validIdentifier(image.ID, 100) || !validIdentifier(image.SchemeID, 64) || !validIdentifier(image.SchemeVersion, 64) || !oneOf(image.Kind, "floor_plan", "massing", "exterior") || image.Width < 512 || image.Height < 512 || image.Width > 8192 || image.Height > 8192 || len(image.SHA256) != 64 || !safeHouseAsset(image.Asset) {
			return ErrInvalidInput
		}
		assets[image.ID] = image
	}
	for _, scheme := range content.Schemes {
		for _, floor := range scheme.Floors {
			asset, ok := findHouseImage(content.Images, scheme.ID, "floor_plan", floor.Number)
			if !ok || asset.SchemeVersion != scheme.Version || asset.ReferenceID != "" {
				return ErrInvalidInput
			}
		}
		first, _ := findHouseImage(content.Images, scheme.ID, "floor_plan", 1)
		volume, ok := findHouseImage(content.Images, scheme.ID, "massing", 0)
		if !ok || volume.SchemeVersion != scheme.Version || volume.ReferenceID != first.ID {
			return ErrInvalidInput
		}
		facade, ok := findHouseImage(content.Images, scheme.ID, "exterior", 0)
		if !ok || facade.SchemeVersion != scheme.Version || facade.ReferenceID != volume.ID {
			return ErrInvalidInput
		}
	}
	return nil
}

func validateHousePages(snapshot DecisionSnapshot, kinds []string, access string) error {
	if len(snapshot.Pages) != len(kinds) {
		return ErrInvalidInput
	}
	for i, page := range snapshot.Pages {
		tableValid := validTable(page.Table)
		if page.Kind == "house_comparison" && snapshot.House != nil {
			tableValid = validHouseComparison(page.Table, len(snapshot.House.Schemes))
		}
		if page.Number != i+1 || page.Kind != kinds[i] || page.Access != access || !boundedCleanText(page.Title, 2, 120) || !boundedCleanText(page.Subtitle, 2, 500) || !validBlocks(page.Blocks, 16, 120, 1200) || !validBlocks(page.Actions, 16, 120, 1200) || !tableValid || !optionalBoundedText(page.Note, 800) || !optionalBoundedText(page.Disclaimer, 800) {
			return ErrInvalidInput
		}
		for _, illustration := range page.Illustrations {
			if !safeHouseAsset(illustration.Asset) || !boundedCleanText(illustration.Title, 1, 120) || !boundedCleanText(illustration.Caption, 2, 500) {
				return ErrInvalidInput
			}
		}
		if len(page.Sources) == 0 {
			return ErrInvalidInput
		}
		for _, source := range page.Sources {
			if _, ok := snapshot.Facts[source]; !ok {
				return ErrInvalidInput
			}
		}
	}
	return nil
}

func validateHousePageBindings(snapshot DecisionSnapshot) error {
	if snapshot.House == nil {
		return ErrInvalidInput
	}
	assets := houseAssetsByKey(snapshot.House.Images)
	pageIndex := 5
	for _, scheme := range snapshot.House.Schemes {
		for _, floor := range scheme.Floors {
			if pageIndex >= len(snapshot.Pages) {
				return ErrInvalidInput
			}
			page := snapshot.Pages[pageIndex]
			asset := assets[houseAssetKey(scheme.ID, "floor_plan", floor.Number)]
			if page.Scheme != scheme.ID || len(page.Illustrations) != 1 || page.Illustrations[0].Asset != asset.Asset {
				return ErrInvalidInput
			}
			pageIndex++
		}
		if pageIndex >= len(snapshot.Pages) {
			return ErrInvalidInput
		}
		page := snapshot.Pages[pageIndex]
		massing := assets[houseAssetKey(scheme.ID, "massing", 0)]
		exterior := assets[houseAssetKey(scheme.ID, "exterior", 0)]
		if page.Scheme != scheme.ID || len(page.Illustrations) != 2 || page.Illustrations[0].Asset != massing.Asset || page.Illustrations[1].Asset != exterior.Asset {
			return ErrInvalidInput
		}
		pageIndex++
	}
	return nil
}

func buildHouseFacts(input DecisionInput) map[string]string {
	return BuildDecisionFacts(input)
}

func houseProfileBlocks(input DecisionInput) []ContentBlock {
	blocks := []ContentBlock{{Title: "常住家庭", Body: formatOptionalInt(input.PermanentCount) + "人"}, {Title: "返乡使用", Body: formatOptionalInt(input.ReturningCount) + "人，频率：" + decisionFactValue("household.return_frequency", input.ReturnFrequency)}}
	for _, member := range input.House.Members {
		body := fmt.Sprintf("%d人，%s", member.Count, mapHouseResidence(member.Residence))
		if len(member.Needs) > 0 {
			body += "；关注：" + strings.Join(member.Needs, "、")
		}
		blocks = append(blocks, ContentBlock{Title: member.Role, Body: body})
	}
	return blocks
}

func houseConstraintBlocks(input DecisionInput) []ContentBlock {
	blocks := []ContentBlock{{Title: "所在地区", Body: valueOrUnknown(input.Region)}, {Title: "宅基地尺寸", Body: "尚未提供"}, {Title: "可建范围", Body: decisionFactValue("site.buildable_scope", input.BuildableScope)}}
	if input.ApproximateDimensions.WidthM != nil && input.ApproximateDimensions.DepthM != nil {
		blocks[1].Body = formatFloat(*input.ApproximateDimensions.WidthM) + "m × " + formatFloat(*input.ApproximateDimensions.DepthM) + "m（用户估算）"
	}
	if input.House.RoadSide != "" {
		blocks = append(blocks, ContentBlock{Title: "道路与入口", Body: input.House.RoadSide})
	}
	if input.House.Orientation != "" {
		blocks = append(blocks, ContentBlock{Title: "朝向", Body: input.House.Orientation})
	}
	blocks = append(blocks, ContentBlock{Title: "庭院与停车", Body: "庭院：" + houseYesNo(input.House.Courtyard) + "；停车：" + houseYesNo(input.House.Parking)})
	return blocks
}

func mapHouseResidence(value string) string {
	if value == "permanent" {
		return "长期居住"
	}
	if value == "returning" {
		return "返乡居住"
	}
	return "待确认"
}
func houseYesNo(value string) string {
	if value == "yes" {
		return "需要"
	}
	if value == "no" {
		return "不需要"
	}
	return "待确认"
}

func houseUnknowns(input DecisionInput) []string {
	unknowns := decisionUnknowns(input)
	if input.House.RoadSide == "" {
		unknowns = append(unknowns, "道路与主要入口方向待确认")
	}
	if input.House.Orientation == "" {
		unknowns = append(unknowns, "准确朝向待确认")
	}
	if input.House.NeighborNotes == "" {
		unknowns = append(unknowns, "相邻建筑与采光遮挡条件待核实")
	}
	return unknowns
}

func houseBriefID(input DecisionInput, brief DecisionBrief) string {
	raw, _, _ := MarshalDecisionInput(input)
	identity, _ := json.Marshal(struct {
		Topic   string
		Options []ContentBlock
	}{brief.Topic, brief.Options})
	sum := sha256.Sum256(append(raw, identity...))
	return "house-diagnosis-" + hex.EncodeToString(sum[:8])
}

func houseMeta(record *Report) SnapshotMeta {
	date := ""
	if !record.CreatedAt.IsZero() {
		date = record.CreatedAt.Format("2006年01月02日")
	}
	return SnapshotMeta{SchemaVersion: SchemaHouseV1, ProtocolVersion: ProtocolHouseV1, Source: HouseSource, ReportID: record.ID, InputHash: record.InputHash, Date: date, GenerationMode: record.GenerationMode}
}

func existingHouseRefs(facts map[string]string, refs ...string) []string {
	out := make([]string, 0, len(refs))
	for _, ref := range refs {
		if _, ok := facts[ref]; ok {
			out = append(out, ref)
		}
	}
	return out
}

func houseRequirementsFromFacts(facts map[string]string) ([]HouseRoomRequirement, error) {
	var requirements []HouseRoomRequirement
	for key, value := range facts {
		if !strings.HasPrefix(key, "house.room.") {
			continue
		}
		var requirement HouseRoomRequirement
		if json.Unmarshal([]byte(value), &requirement) != nil || requirement.ID == "" {
			return nil, ErrInvalidInput
		}
		requirements = append(requirements, requirement)
	}
	sort.Slice(requirements, func(i, j int) bool { return requirements[i].ID < requirements[j].ID })
	return requirements, nil
}

func validateSchemeRequirements(facts map[string]string, scheme HouseScheme, requirements []HouseRoomRequirement) error {
	counts := map[string]int{}
	floors := map[string][]int{}
	for _, floor := range scheme.Floors {
		for _, room := range floor.Rooms {
			if room.RequirementID != "" {
				counts[room.RequirementID] += room.Count
				floors[room.RequirementID] = append(floors[room.RequirementID], floor.Number)
			}
		}
	}
	for _, requirement := range requirements {
		if requirement.Required && counts[requirement.ID] < requirement.Count {
			return ErrInvalidInput
		}
		if requirement.PreferredFloor > 0 && requirement.Required {
			found := false
			for _, floor := range floors[requirement.ID] {
				if floor == requirement.PreferredFloor {
					found = true
				}
			}
			if !found {
				return ErrInvalidInput
			}
		}
	}
	if facts["house.first_floor_elder_room"] == "yes" {
		found := false
		for _, room := range scheme.Floors[0].Rooms {
			if room.Kind == "elder_bedroom" && room.Count > 0 {
				found = true
			}
		}
		if !found {
			return ErrInvalidInput
		}
	}
	return nil
}

func cloneHousePages(pages []SnapshotPage, access string) []SnapshotPage {
	raw, _ := json.Marshal(pages)
	var cloned []SnapshotPage
	_ = json.Unmarshal(raw, &cloned)
	for i := range cloned {
		cloned[i].Access = access
	}
	return cloned
}

func appendHousePage(pages []SnapshotPage, additions ...SnapshotPage) []SnapshotPage {
	for _, page := range additions {
		page.Number = len(pages) + 1
		pages = append(pages, page)
	}
	return pages
}

func houseRoomSummary(rooms []HouseRoom) string {
	parts := make([]string, 0, len(rooms))
	for _, room := range rooms {
		parts = append(parts, fmt.Sprintf("%s×%d", room.Name, room.Count))
	}
	return strings.Join(parts, "、")
}

func houseActionBlocks(actions []HouseAction) []ContentBlock {
	blocks := make([]ContentBlock, 0, len(actions))
	for _, action := range actions {
		blocks = append(blocks, ContentBlock{Title: action.Task, Body: "负责：" + action.Owner + "；前置：" + action.Prerequisite + "；完成依据：" + action.Evidence})
	}
	return blocks
}

func houseBudgetSources(facts map[string]string) []string {
	refs := existingHouseRefs(facts, "budget.wan", "intent.priorities")
	keys := make([]string, 0)
	for key := range facts {
		if strings.HasPrefix(key, "budget.scope.") {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	return append(refs, keys...)
}

func houseAssetsByKey(images []HouseImageAsset) map[string]HouseImageAsset {
	result := make(map[string]HouseImageAsset, len(images))
	for _, image := range images {
		result[houseAssetKey(image.SchemeID, image.Kind, image.Floor)] = image
	}
	return result
}

func houseAssetKey(scheme, kind string, floor int) string {
	return fmt.Sprintf("%s/%s/%d", scheme, kind, floor)
}

func findHouseImage(images []HouseImageAsset, scheme, kind string, floor int) (HouseImageAsset, bool) {
	var found HouseImageAsset
	count := 0
	for _, image := range images {
		if image.SchemeID == scheme && image.Kind == kind && image.Floor == floor {
			found = image
			count++
		}
	}
	return found, count == 1
}

func safeHouseAsset(asset string) bool {
	if asset == "" || strings.HasPrefix(asset, "/") || strings.Contains(asset, "\\") || path.Clean(asset) != asset || strings.HasPrefix(asset, "../") || asset == "." {
		return false
	}
	return true
}
