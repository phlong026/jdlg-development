package report

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"time"
)

const (
	houseAgentVersion       = "house-agent-v2"
	houseDiagnosisVersion   = "house-diagnosis-v1"
	housePlannerVersion     = "house-planner-v1"
	houseDecisionVersion    = "house-decision-v1"
	houseSpatialVersion     = "house-spatial-v1"
	houseVisualVersion      = "house-visual-v2"
	houseBudgetVersion      = "house-budget-v1"
	houseEditorVersion      = "house-editor-v1"
	houseImageReviewVersion = "house-image-review-v1"
	houseFinalReviewVersion = "house-final-review-v1"
)

type HouseOrchestrator struct {
	stages *MultiAgentRunner
	images HouseImageService
}

type HouseSchemeDirections struct {
	Schemes []HouseScheme `json:"schemes"`
}
type HouseSpatialResult struct {
	Schemes []HouseScheme `json:"schemes"`
}
type HouseVisualResult struct {
	Approved        bool     `json:"approved"`
	ApprovedTaskIDs []string `json:"approved_task_ids"`
	Issues          []string `json:"issues"`
}
type HouseBudgetResult struct {
	Blocks []ContentBlock `json:"blocks"`
}
type HouseEditorialResult struct {
	Comparison    [][]string     `json:"comparison"`
	DesignerBrief []ContentBlock `json:"designer_brief"`
	Actions       []HouseAction  `json:"actions"`
}
type HouseImageReview struct {
	AssetID  string   `json:"asset_id"`
	SHA256   string   `json:"sha256"`
	Approved bool     `json:"approved"`
	Issues   []string `json:"issues"`
}

type houseAgentInput struct {
	Input        DecisionInput         `json:"user_input"`
	Facts        map[string]string     `json:"facts"`
	Brief        DecisionBrief         `json:"decision_brief"`
	Confirmation *DecisionConfirmation `json:"user_confirmation,omitempty"`
	Preview      *DecisionSnapshot     `json:"frozen_preview,omitempty"`
	Plan         *HousePlan            `json:"plan,omitempty"`
	Schemes      []HouseScheme         `json:"schemes,omitempty"`
	Tasks        []HouseImageTask      `json:"image_tasks,omitempty"`
	Assets       []HouseImageAsset     `json:"image_assets,omitempty"`
	Snapshot     *DecisionSnapshot     `json:"canonical_snapshot,omitempty"`
}

func NewHouseOrchestrator(client AgentClient, checkpoints StageCheckpointStore, images HouseImageService) (*HouseOrchestrator, error) {
	if images == nil {
		return nil, ErrInvalidInput
	}
	stages, err := NewMultiAgentRunner(client, checkpoints)
	if err != nil {
		return nil, err
	}
	for _, role := range []string{"diagnosis", "planner", "decision", "spatial", "visual", "budget", "editor", "reviewer"} {
		if !validAgentModel(stages.identity.Models[role]) {
			return nil, ErrAgentUnavailable
		}
	}
	return &HouseOrchestrator{stages: stages, images: images}, nil
}

func (o *HouseOrchestrator) Run(ctx context.Context, record *Report, mode string, _ *ReviewedPack) (json.RawMessage, string, error) {
	if record == nil || record.ProtocolVersion != ProtocolHouseV1 || record.ID == "" || record.LeaseToken == "" || !oneOf(mode, ModePreview, ModeFull) {
		return nil, "", ErrInvalidInput
	}
	if mode == ModeFull && !record.Paid {
		return nil, "", ErrPaymentRequired
	}
	var input DecisionInput
	if strictDecode(record.InputJSON, &input) != nil {
		return nil, "", ErrInvalidInput
	}
	normalized, err := NormalizeDecisionInput(input)
	if err != nil || normalized.House == nil {
		return nil, "", ErrInvalidInput
	}
	input = normalized
	_, inputHash, err := MarshalDecisionInput(input)
	if err != nil || inputHash != record.InputHash {
		return nil, "", ErrInvalidInput
	}
	key, err := o.runKey(record, mode)
	if err != nil {
		return nil, "", err
	}
	var cached DecisionSnapshot
	if found, loadErr := o.stages.loadEnvelope(ctx, record.ID, key, "final", houseAgentVersion, "", &cached); loadErr != nil {
		return nil, "", loadErr
	} else if found {
		if ValidateHouseSnapshot(cached, mode) != nil || cached.Meta.ReportID != record.ID || cached.Meta.InputHash != record.InputHash || cached.Meta.GenerationMode != GenerationMultiAgent || !houseCachedRunMatches(record, mode, input, cached) {
			return nil, "", ErrConflict
		}
		raw, marshalErr := json.Marshal(cached)
		return raw, GenerationMultiAgent, marshalErr
	}
	if mode == ModePreview {
		return o.runPreview(ctx, record, key, input)
	}
	return o.runFull(ctx, record, key, input)
}

func (o *HouseOrchestrator) runPreview(ctx context.Context, record *Report, key string, input DecisionInput) (json.RawMessage, string, error) {
	in := houseAgentInput{Input: input, Facts: buildHouseFacts(input), Brief: PlanHouseDecision(input)}
	brief, err := runTypedStage[DecisionBrief](ctx, o.stages, record, key, "diagnosis", "diagnosis", houseDiagnosisVersion, houseDiagnosisPrompt, in, func(v DecisionBrief) error { return ValidateHouseBrief(input, v) })
	if err != nil {
		return nil, "", err
	}
	snapshot, err := BuildHousePreview(record, input, brief)
	if err != nil {
		return nil, "", err
	}
	snapshot.Meta.GenerationMode = GenerationMultiAgent
	in.Brief = brief
	in.Snapshot = &snapshot
	review, err := runTypedStage[ReviewResult](ctx, o.stages, record, key, "preview_review", "reviewer", houseFinalReviewVersion, houseReviewPrompt, in, func(v ReviewResult) error { return validateReview(v, len(snapshot.Pages)) })
	if err != nil {
		return nil, "", err
	}
	if !review.Approved {
		return nil, "", ErrAgentRejected
	}
	if err = o.stages.saveEnvelope(ctx, record, key, "final", houseAgentVersion, "", snapshot); err != nil {
		return nil, "", err
	}
	raw, err := json.Marshal(snapshot)
	return raw, GenerationMultiAgent, err
}

func (o *HouseOrchestrator) runFull(ctx context.Context, record *Report, key string, input DecisionInput) (json.RawMessage, string, error) {
	var preview DecisionSnapshot
	if strictDecode(record.PreviewSnapshot, &preview) != nil || ValidateHouseSnapshot(preview, ModePreview) != nil || preview.Meta.ReportID != record.ID || preview.Meta.InputHash != record.InputHash || !reflect.DeepEqual(preview.Facts, buildHouseFacts(input)) || !reflect.DeepEqual(preview.Diagnosis, PlanHouseDecision(input)) {
		return nil, "", ErrInvalidInput
	}
	if ValidateHouseBrief(input, preview.Diagnosis) != nil {
		return nil, "", ErrConflict
	}
	var confirmation DecisionConfirmation
	if strictDecode(record.ConfirmationJSON, &confirmation) != nil || record.ConfirmationHash == "" {
		return nil, "", ErrConflict
	}
	confirmation, err := NormalizeDecisionConfirmation(input, confirmation)
	if err != nil || !oneOf(confirmation.Choice, ChoiceConfirm, ChoiceUndecided) {
		return nil, "", ErrConflict
	}
	_, confirmationHash, err := marshalDecisionConfirmation(confirmation)
	if err != nil || confirmationHash != record.ConfirmationHash {
		return nil, "", ErrConflict
	}
	in := houseAgentInput{Input: input, Facts: buildHouseFacts(input), Brief: preview.Diagnosis, Confirmation: &confirmation, Preview: &preview}

	plan, err := runTypedStage[HousePlan](ctx, o.stages, record, key, "plan", "planner", housePlannerVersion, housePlannerPrompt, in, validateHousePlan)
	if err != nil {
		return nil, "", err
	}
	in.Plan = &plan
	directions, err := runTypedStage[HouseSchemeDirections](ctx, o.stages, record, key, "directions", "decision", houseDecisionVersion, houseDecisionPrompt, in, func(v HouseSchemeDirections) error { return validateHouseDirections(in.Facts, plan, v) })
	if err != nil {
		return nil, "", err
	}
	in.Schemes = directions.Schemes
	spatial, err := runTypedStage[HouseSpatialResult](ctx, o.stages, record, key, "spatial", "spatial", houseSpatialVersion, houseSpatialPrompt, in, func(v HouseSpatialResult) error { return validateHouseSpatial(input, directions, v) })
	if err != nil {
		return nil, "", err
	}
	in.Schemes = spatial.Schemes
	in.Tasks = buildHouseImageTasks(input, spatial.Schemes)
	_, err = runTypedStage[HouseVisualResult](ctx, o.stages, record, key, "visual", "visual", houseVisualVersion, houseVisualPrompt, in, func(v HouseVisualResult) error { return validateHouseVisual(input, spatial.Schemes, v) })
	if err != nil {
		return nil, "", err
	}
	assets, err := o.ensureAndReviewImages(ctx, record, key, in.Tasks)
	if err != nil {
		return nil, "", err
	}
	in.Assets = assets
	budget, err := runTypedStage[HouseBudgetResult](ctx, o.stages, record, key, "budget", "budget", houseBudgetVersion, houseBudgetPrompt, in, validateHouseBudget)
	if err != nil {
		return nil, "", err
	}
	editorial, err := runTypedStage[HouseEditorialResult](ctx, o.stages, record, key, "editor", "editor", houseEditorVersion, houseEditorPrompt, in, func(v HouseEditorialResult) error { return validateHouseEditorial(spatial.Schemes, v) })
	if err != nil {
		return nil, "", err
	}
	content := HouseContent{Plan: plan, Schemes: spatial.Schemes, Comparison: editorial.Comparison, Budget: budget.Blocks, DesignerBrief: editorial.DesignerBrief, Actions: editorial.Actions, Images: assets}
	full, err := buildHouseFullSnapshot(record, input, confirmation, preview, content)
	if err != nil {
		return nil, "", err
	}
	full.Meta.GenerationMode = GenerationMultiAgent
	if err = ValidateHouseSnapshot(full, ModeFull); err != nil {
		return nil, "", err
	}
	in.Snapshot = &full
	review, err := runTypedStage[ReviewResult](ctx, o.stages, record, key, "final_review", "reviewer", houseFinalReviewVersion, houseReviewPrompt, in, func(v ReviewResult) error { return validateReview(v, len(full.Pages)) })
	if err != nil {
		return nil, "", err
	}
	if !review.Approved {
		return nil, "", ErrAgentRejected
	}
	if err = o.stages.saveEnvelope(ctx, record, key, "final", houseAgentVersion, "", full); err != nil {
		return nil, "", err
	}
	raw, err := json.Marshal(full)
	return raw, GenerationMultiAgent, err
}

func (o *HouseOrchestrator) ensureAndReviewImages(ctx context.Context, record *Report, key string, tasks []HouseImageTask) ([]HouseImageAsset, error) {
	assets := make([]HouseImageAsset, 0, len(tasks))
	byID := map[string]HouseImageAsset{}
	for _, task := range tasks {
		if deadline, ok := ctx.Deadline(); ok && time.Until(deadline) < 190*time.Second {
			return nil, ErrHousePending
		}
		asset, created, err := o.images.Ensure(ctx, record, task)
		if err != nil {
			return nil, err
		}
		if created {
			return nil, ErrHousePending
		}
		if err = validateHouseAssetForTask(asset, task); err != nil {
			return nil, err
		}
		data, err := o.images.Read(ctx, record, asset)
		if err != nil {
			return nil, err
		}
		var reference *HouseImageAsset
		var referenceData []byte
		if task.ReferenceID != "" {
			ref, ok := byID[task.ReferenceID]
			if !ok {
				return nil, ErrConflict
			}
			reference = &ref
			referenceData, err = o.images.Read(ctx, record, ref)
			if err != nil {
				return nil, err
			}
		}
		review, err := o.reviewHouseImage(ctx, record, key, task, asset, data, reference, referenceData)
		if err != nil {
			return nil, err
		}
		if !review.Approved {
			return nil, ErrAgentRejected
		}
		assets = append(assets, asset)
		byID[asset.ID] = asset
	}
	return assets, nil
}

func (o *HouseOrchestrator) reviewHouseImage(ctx context.Context, record *Report, key string, task HouseImageTask, asset HouseImageAsset, data []byte, reference *HouseImageAsset, referenceData []byte) (HouseImageReview, error) {
	stage := "image_review_" + task.ID
	var cached HouseImageReview
	model := o.stages.identity.Models["reviewer"]
	if found, err := o.stages.loadEnvelope(ctx, record.ID, key, stage, houseImageReviewVersion, model, &cached); err != nil {
		return HouseImageReview{}, err
	} else if found {
		if validateHouseImageReview(cached, asset) != nil {
			return HouseImageReview{}, ErrConflict
		}
		return cached, nil
	}
	input, err := json.Marshal(struct {
		Task      HouseImageTask   `json:"task"`
		Asset     HouseImageAsset  `json:"asset"`
		Reference *HouseImageAsset `json:"reference,omitempty"`
	}{task, asset, reference})
	if err != nil {
		return HouseImageReview{}, ErrInvalidInput
	}
	images := []AgentImage{{MIMEType: houseImageMIME(data), Data: data}}
	if images[0].MIMEType == "" {
		return HouseImageReview{}, ErrInvalidInput
	}
	if reference != nil {
		mime := houseImageMIME(referenceData)
		if mime == "" {
			return HouseImageReview{}, ErrInvalidInput
		}
		images = append(images, AgentImage{MIMEType: mime, Data: referenceData})
	}
	raw, err := o.stages.client.Run(ctx, AgentRequest{Images: images, Role: "reviewer", Model: model, PromptVersion: houseImageReviewVersion, SystemPrompt: houseImageReviewPrompt, Input: input})
	if err != nil {
		return HouseImageReview{}, ErrAgentUnavailable
	}
	var review HouseImageReview
	if strictDecode(raw, &review) != nil || validateHouseImageReview(review, asset) != nil {
		return HouseImageReview{}, ErrAgentUnavailable
	}
	if err = o.stages.saveEnvelope(ctx, record, key, stage, houseImageReviewVersion, model, review); err != nil {
		return HouseImageReview{}, err
	}
	return review, nil
}

func houseImageMIME(data []byte) string {
	mime := http.DetectContentType(data)
	if mime == "image/png" || mime == "image/jpeg" {
		return mime
	}
	return ""
}

func houseCachedRunMatches(record *Report, mode string, input DecisionInput, cached DecisionSnapshot) bool {
	if !reflect.DeepEqual(cached.Facts, buildHouseFacts(input)) || !reflect.DeepEqual(cached.Diagnosis, PlanHouseDecision(input)) {
		return false
	}
	if mode == ModePreview {
		return cached.Confirmation == nil
	}
	var confirmation DecisionConfirmation
	if strictDecode(record.ConfirmationJSON, &confirmation) != nil {
		return false
	}
	normalized, err := NormalizeDecisionConfirmation(input, confirmation)
	if err != nil || cached.Confirmation == nil || !reflect.DeepEqual(*cached.Confirmation, normalized) {
		return false
	}
	var preview DecisionSnapshot
	if strictDecode(record.PreviewSnapshot, &preview) != nil || len(cached.Pages) < 4 || !reflect.DeepEqual(cached.Pages[:4], cloneHousePages(preview.Pages, "paid")) {
		return false
	}
	return true
}

func validateHousePlan(plan HousePlan) error {
	if plan.SchemeCount < 2 || plan.SchemeCount > 3 || len(plan.Focus) < 2 || len(plan.Focus) > 6 {
		return ErrInvalidInput
	}
	for _, focus := range plan.Focus {
		if !boundedCleanText(focus, 2, 100) {
			return ErrInvalidInput
		}
	}
	return nil
}

func validateHouseDirections(facts map[string]string, plan HousePlan, result HouseSchemeDirections) error {
	if len(result.Schemes) != plan.SchemeCount {
		return ErrInvalidInput
	}
	seen := map[string]bool{}
	seenDirections := map[string]bool{}
	for _, scheme := range result.Schemes {
		if !validIdentifier(scheme.ID, 64) || !validIdentifier(scheme.Version, 64) || seen[scheme.ID] || !boundedCleanText(scheme.Name, 2, 80) || !boundedCleanText(scheme.Strategy, 4, 500) || !boundedCleanText(scheme.Gain, 2, 500) || !boundedCleanText(scheme.Tradeoff, 2, 500) || !boundedCleanText(scheme.Facade, 2, 500) || len(scheme.CostSensitive) == 0 || len(scheme.EvidenceRefs) == 0 {
			return ErrInvalidInput
		}
		direction := scheme.Strategy + "\x00" + scheme.Gain + "\x00" + scheme.Tradeoff
		if seenDirections[direction] {
			return ErrInvalidInput
		}
		seenDirections[direction] = true
		for _, ref := range scheme.EvidenceRefs {
			if _, ok := facts[ref]; !ok {
				return ErrInvalidInput
			}
		}
		seen[scheme.ID] = true
	}
	return nil
}

func validateHouseSpatial(input DecisionInput, directions HouseSchemeDirections, result HouseSpatialResult) error {
	if len(result.Schemes) != len(directions.Schemes) {
		return ErrInvalidInput
	}
	content := HouseContent{Plan: HousePlan{SchemeCount: len(result.Schemes), Focus: []string{"空间"}}, Schemes: result.Schemes, Comparison: placeholderHouseComparison(result.Schemes), Budget: []ContentBlock{{Title: "预算", Body: "待统一询价。"}}, DesignerBrief: []ContentBlock{{Title: "任务", Body: "按家庭需求复核。"}}, Actions: []HouseAction{{Task: "复核", Owner: "设计师", Prerequisite: "资料", Evidence: "记录"}}, Images: placeholderHouseImages(result.Schemes)}
	facts := buildHouseFacts(input)
	if err := validateHouseContent(facts, content); err != nil {
		return err
	}
	for i, scheme := range result.Schemes {
		base := directions.Schemes[i]
		base.Floors = nil
		schemeWithNoFloors := scheme
		schemeWithNoFloors.Floors = nil
		if !reflect.DeepEqual(schemeWithNoFloors, base) {
			return ErrInvalidInput
		}
		if input.FlexibleUse == FlexibleUseNo {
			for _, floor := range scheme.Floors {
				for _, room := range floor.Rooms {
					if strings.Contains(room.Name, "兼用") || strings.Contains(room.Name, "转换") {
						return ErrInvalidInput
					}
				}
			}
		}
	}
	return nil
}

func validateHouseVisual(input DecisionInput, schemes []HouseScheme, result HouseVisualResult) error {
	expected := buildHouseImageTasks(input, schemes)
	if !result.Approved {
		if len(result.Issues) < 1 || len(result.Issues) > 6 {
			return ErrInvalidInput
		}
		for _, issue := range result.Issues {
			if !boundedCleanText(issue, 2, 300) {
				return ErrInvalidInput
			}
		}
		return ErrAgentRejected
	}
	if len(result.Issues) != 0 || len(result.ApprovedTaskIDs) != len(expected) {
		return ErrInvalidInput
	}
	wanted := make(map[string]bool, len(expected))
	for _, task := range expected {
		wanted[task.ID] = true
	}
	seen := make(map[string]bool, len(result.ApprovedTaskIDs))
	for _, id := range result.ApprovedTaskIDs {
		if !wanted[id] || seen[id] {
			return ErrInvalidInput
		}
		seen[id] = true
	}
	return nil
}
func validateHouseBudget(v HouseBudgetResult) error {
	if !validBlocks(v.Blocks, 12, 100, 900) || len(v.Blocks) < 2 {
		return ErrInvalidInput
	}
	return nil
}
func validateHouseEditorial(schemes []HouseScheme, v HouseEditorialResult) error {
	if !validHouseComparison(v.Comparison, len(schemes)) || !validBlocks(v.DesignerBrief, 12, 100, 900) || len(v.DesignerBrief) < 2 || len(v.Actions) < 2 || len(v.Actions) > 12 {
		return ErrInvalidInput
	}
	for _, a := range v.Actions {
		if !boundedCleanText(a.Task, 2, 200) || !boundedCleanText(a.Owner, 2, 100) || !boundedCleanText(a.Prerequisite, 2, 300) || !boundedCleanText(a.Evidence, 2, 300) {
			return ErrInvalidInput
		}
	}
	return nil
}
func validateHouseImageReview(v HouseImageReview, asset HouseImageAsset) error {
	if v.AssetID != asset.ID || v.SHA256 != asset.SHA256 || v.Approved != (len(v.Issues) == 0) || len(v.Issues) > 6 {
		return ErrInvalidInput
	}
	for _, issue := range v.Issues {
		if !boundedCleanText(issue, 2, 300) {
			return ErrInvalidInput
		}
	}
	return nil
}
func validateHouseAssetForTask(asset HouseImageAsset, task HouseImageTask) error {
	if asset.ID != task.ID || asset.SchemeID != task.SchemeID || asset.SchemeVersion != task.SchemeVersion || asset.Kind != task.Kind || asset.Floor != task.Floor || asset.ReferenceID != task.ReferenceID {
		return ErrConflict
	}
	return nil
}

func buildHouseImageTasks(input DecisionInput, schemes []HouseScheme) []HouseImageTask {
	tasks := make([]HouseImageTask, 0)
	for _, scheme := range schemes {
		for _, floor := range scheme.Floors {
			id := fmt.Sprintf("%s-floor-%d", scheme.ID, floor.Number)
			tasks = append(tasks, HouseImageTask{ID: id, SchemeID: scheme.ID, SchemeVersion: scheme.Version, Kind: "floor_plan", Floor: floor.Number, Prompt: houseFloorPrompt(input, scheme, floor)})
		}
		first := fmt.Sprintf("%s-floor-1", scheme.ID)
		massing := scheme.ID + "-massing"
		tasks = append(tasks, HouseImageTask{ID: massing, SchemeID: scheme.ID, SchemeVersion: scheme.Version, Kind: "massing", Prompt: houseMassingPrompt(input, scheme), ReferenceID: first})
		tasks = append(tasks, HouseImageTask{ID: scheme.ID + "-exterior", SchemeID: scheme.ID, SchemeVersion: scheme.Version, Kind: "exterior", Prompt: houseExteriorPrompt(input, scheme), ReferenceID: massing})
	}
	return tasks
}

func placeholderHouseImages(schemes []HouseScheme) []HouseImageAsset {
	tasks := buildHouseImageTasks(DecisionInput{House: &HouseRequirements{Style: "概念"}}, schemes)
	images := make([]HouseImageAsset, 0, len(tasks))
	for _, task := range tasks {
		images = append(images, HouseImageAsset{ID: task.ID, Asset: "house/placeholder/" + task.ID + ".png", SchemeID: task.SchemeID, SchemeVersion: task.SchemeVersion, Kind: task.Kind, Floor: task.Floor, Width: 2048, Height: 1152, Model: "placeholder", SHA256: fmt.Sprintf("%064s", "a"), ReferenceID: task.ReferenceID})
	}
	return images
}
func placeholderHouseComparison(schemes []HouseScheme) [][]string {
	header := []string{"维度"}
	daily := []string{"日常使用"}
	tradeoff := []string{"方案代价"}
	for _, scheme := range schemes {
		header = append(header, scheme.Name)
		daily = append(daily, scheme.Gain)
		tradeoff = append(tradeoff, scheme.Tradeoff)
	}
	return [][]string{header, daily, tradeoff}
}
func houseFloorPrompt(input DecisionInput, scheme HouseScheme, floor HouseFloor) string {
	return fmt.Sprintf("生成方案%s（%s）%s概念平面shape图。场地约束：%s。整屋结构：%s。本层严格按清单：%s；楼梯：%s；动线：%s。楼梯位置需与相邻楼层保持同一交通核。采用清楚的中文标注和统一图例，只表达功能关系；地块尺寸是用户估算时不得当作测量值，不标注未经验证的精确尺寸，不得增加或减少房间。", scheme.ID, scheme.Version, floor.Name, houseSiteContext(input), houseFloorContext(scheme), houseRoomSummary(floor.Rooms), floor.StairPosition, floor.Circulation)
}
func houseMassingPrompt(input DecisionInput, scheme HouseScheme) string {
	return fmt.Sprintf("基于参考平面生成方案%s（%s）的整屋体量概念图。场地约束：%s。必须明确表现总层数%d层，并与完整逐层结构保持一致：%s。风格为%s，表达%s。不得改变楼层、房间数量、楼梯交通核或入口逻辑，不作为施工图。", scheme.ID, scheme.Version, houseSiteContext(input), len(scheme.Floors), houseFloorContext(scheme), input.House.Style, scheme.Facade)
}
func houseExteriorPrompt(input DecisionInput, scheme HouseScheme) string {
	return fmt.Sprintf("基于参考体量生成方案%s（%s）外观方向图。场地约束：%s。建筑总层数固定为%d层，逐层结构为：%s。风格为%s，保留同一体量、楼层、入口和开口逻辑，重点表达%s。不得重新设计空间或宣称施工精度。", scheme.ID, scheme.Version, houseSiteContext(input), len(scheme.Floors), houseFloorContext(scheme), input.House.Style, scheme.Facade)
}

func houseSiteContext(input DecisionInput) string {
	dimensions := "尺寸未知，不得猜测"
	if input.ApproximateDimensions.WidthM != nil && input.ApproximateDimensions.DepthM != nil {
		dimensions = fmt.Sprintf("约%sm×%sm（用户估算，非实测）", formatFloat(*input.ApproximateDimensions.WidthM), formatFloat(*input.ApproximateDimensions.DepthM))
	}
	road := valueOrUnknown(input.House.RoadSide)
	orientation := valueOrUnknown(input.House.Orientation)
	neighbors := valueOrUnknown(input.House.NeighborNotes)
	return fmt.Sprintf("地区%s；地块%s；可建范围%s；可建边界%s；道路/入口%s；朝向%s；相邻条件%s；庭院%s；停车%s", valueOrUnknown(input.Region), dimensions, valueOrUnknown(input.BuildableScope), valueOrUnknown(input.BuildableConstraints), road, orientation, neighbors, houseYesNo(input.House.Courtyard), houseYesNo(input.House.Parking))
}

func houseFloorContext(scheme HouseScheme) string {
	parts := make([]string, 0, len(scheme.Floors))
	for _, floor := range scheme.Floors {
		parts = append(parts, fmt.Sprintf("第%d层[%s；楼梯%s]", floor.Number, houseRoomSummary(floor.Rooms), floor.StairPosition))
	}
	return fmt.Sprintf("总层数%d层；%s", len(scheme.Floors), strings.Join(parts, "；"))
}

func (o *HouseOrchestrator) runKey(record *Report, mode string) (string, error) {
	identity, err := json.Marshal(o.stages.identity)
	if err != nil {
		return "", ErrInvalidInput
	}
	hash := sha256.New()
	for _, part := range [][]byte{[]byte(houseAgentVersion), []byte(SchemaHouseV1), []byte(HouseTemplateVersion), []byte(mode), []byte(record.InputHash), []byte(record.ConfirmationHash), identity, record.InputJSON, record.PreviewSnapshot, record.ConfirmationJSON} {
		_, _ = hash.Write(part)
		_, _ = hash.Write([]byte{0})
	}
	return "house-" + hex.EncodeToString(hash.Sum(nil)), nil
}

var _ AgentRunner = (*HouseOrchestrator)(nil)

const houseDiagnosisPrompt = `你是整屋需求诊断Agent。仅增强程序给出的诊断正文，不改变ID、主题、选项标题、事实引用和未知项。尊重用户拒绝项，不得发明尺寸、价格、审批或施工结论。只返回严格JSON：{"id":"...","recipe":"space","topic":"...","reason":"...","evidence_refs":["事实ID"],"options":[{"title":"...","body":"..."}],"unknowns":["..."],"question":"..."}，不得增加字段。`
const housePlannerPrompt = `你是整屋报告策划Agent。根据家庭事实确定二至三个有实质差异的整屋方向和贯穿比较重点。只返回严格JSON：{"scheme_count":2或3,"focus":["重点一","重点二"]}。focus至少两项，不得增加字段。`
const houseDecisionPrompt = `你是整屋方案Agent。生成与plan数量一致且真正不同的整屋概念方向，此阶段不分配楼层、不编造工程精度。只返回严格JSON：{"schemes":[{"id":"稳定英文或拼音ID","version":"稳定版本ID","name":"方案名","strategy":"空间策略","gain":"主要收益","tradeoff":"需要接受的代价","facade":"外观方向","cost_sensitive":["成本敏感项"],"assumptions":["成立假设"],"evidence_refs":["facts中真实证据ID"],"floors":[]}]}。所有key必填；id/version只能用字母数字下划线或短横线；不得增加字段。`
const houseSpatialPrompt = `你是空间规划Agent。为每个既定方案按user_input真实楼层数分配全部卧室和required房间。必须保留方案的id、version、name、strategy、gain、tradeoff、facade、cost_sensitive、assumptions、evidence_refs原值。只返回严格JSON：{"schemes":[{"id":"...","version":"...","name":"...","strategy":"...","gain":"...","tradeoff":"...","facade":"...","cost_sensitive":["..."],"assumptions":["..."],"evidence_refs":["..."],"floors":[{"number":1,"name":"一层","summary":"...","rooms":[{"name":"老人房","kind":"elder_bedroom","count":1,"requirement_id":"elder"},{"name":"卧室","kind":"bedroom","count":1}],"circulation":"...","stair_position":"..."}]}]}。floor.number从一连续递增；卧室kind只能用bedroom或elder_bedroom，其他空间用稳定英文kind；count为正整数；required房间填写对应requirement_id。用户拒绝兼用时不得生成兼用房，不得增加字段。`
const houseVisualPrompt = `你是视觉任务审核Agent。input.image_tasks是程序从同一scheme结构派生的规范任务：每层一张floor_plan，随后massing引用首层平面，exterior引用massing。逐项核对任务与用户硬约束、方案、楼层和引用关系。通过时返回全部任务ID，不重复且不遗漏；拒绝时说明问题。只返回严格JSON：{"approved":true,"approved_task_ids":["task-id"],"issues":[]}或{"approved":false,"approved_task_ids":[],"issues":["问题"]}，不得增加字段。`
const houseBudgetPrompt = `你是预算与询价Agent。结合方案成本敏感项和用户预算范围，形成可带走使用的统一询价清单，写明范围、询问对象和比较口径；未知成本不得估算。只返回严格JSON：{"blocks":[{"title":"询价项目或口径","body":"范围、向谁询问、如何比较"}]}。blocks为二至十二项，title/body必填，不得增加字段。`
const houseEditorPrompt = `你是整屋报告编辑Agent。根据已冻结的方案、逐层空间和图片资产生成方案比较、设计师任务书和行动计划。只返回严格JSON：{"comparison":[["维度","方案一名称","方案二名称"],["比较维度","方案一内容","方案二内容"]],"designer_brief":[{"title":"必须满足/优先满足/可协商/仍待核实","body":"..."}],"actions":[{"task":"任务","owner":"负责角色","prerequisite":"前置条件","evidence":"完成依据"}]}。comparison每行列数必须等于方案数加一，表头第一格为维度并按输入方案顺序列出每个方案；designer_brief至少两项；actions至少两项且四个key必填；不得增加字段。`
const houseImageReviewPrompt = `你是视觉一致性审核Agent。你必须实际查看随请求提供的图片：第一张是当前待审图，input.reference存在时第二张是同方案参考图。结合input.task中的结构化房间、楼层和硬约束，核对scheme版本、楼层、房间数量、参考关系和概念图边界。不能看清或不一致必须拒绝。只返回严格JSON：{"asset_id":"原asset.id","sha256":"原asset.sha256","approved":true或false,"issues":["明确问题"]}。approved为true时issues必须为空；不得假装已看图或增加字段。`
const houseReviewPrompt = `你是整屋报告独立审核Agent。核查冻结预览事实、确认结果、方案差异、每层房间总数、拒绝项、图文关系、预算边界、任务书和行动计划。概念图不得冒充施工图。只返回严格JSON：通过时{"approved":true,"issues":[]}；不通过时{"approved":false,"issues":[{"page":有效页码,"code":"snake_case","message":"明确可修复的问题"}]}。最多八个问题，不得代写报告或增加字段。`
