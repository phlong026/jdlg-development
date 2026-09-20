package report

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

type houseAgentFixture struct {
	mu          sync.Mutex
	calls       map[string]int
	rejectImage bool
	imageMIMEs  []string
}

func (c *houseAgentFixture) Identity() AgentIdentity {
	return AgentIdentity{ConfigVersion: "house-fixture-v1", Models: map[string]string{"diagnosis": "model", "planner": "model", "writer": "model", "decision": "model", "spatial": "model", "visual": "model", "budget": "model", "editor": "model", "reviewer": "model"}}
}

func (c *houseAgentFixture) Run(_ context.Context, req AgentRequest) (json.RawMessage, error) {
	c.mu.Lock()
	if c.calls == nil {
		c.calls = map[string]int{}
	}
	c.calls[req.Role]++
	reject := c.rejectImage
	for _, image := range req.Images {
		c.imageMIMEs = append(c.imageMIMEs, image.MIMEType)
	}
	c.mu.Unlock()
	var in houseAgentInput
	if err := json.Unmarshal(req.Input, &in); err != nil {
		return nil, err
	}
	var out any
	switch req.PromptVersion {
	case houseDiagnosisVersion:
		out = PlanHouseDecision(in.Input)
	case housePlannerVersion:
		out = HousePlan{SchemeCount: 2, Focus: []string{"父母日常", "返乡团聚", "投入边界"}}
	case houseDecisionVersion:
		out = HouseSchemeDirections{Schemes: validHouseContentFixture(in.Input).Schemes}
	case houseSpatialVersion:
		out = HouseSpatialResult{Schemes: validHouseContentFixture(in.Input).Schemes}
	case houseVisualVersion:
		if len(in.Tasks) == 0 {
			return nil, errors.New("canonical image tasks missing")
		}
		ids := make([]string, 0, len(in.Tasks))
		for _, task := range in.Tasks {
			ids = append(ids, task.ID)
		}
		out = HouseVisualResult{Approved: true, ApprovedTaskIDs: ids}
	case houseBudgetVersion:
		out = HouseBudgetResult{Blocks: validHouseContentFixture(in.Input).Budget}
	case houseEditorVersion:
		fixture := validHouseContentFixture(in.Input)
		out = HouseEditorialResult{Comparison: fixture.Comparison, DesignerBrief: fixture.DesignerBrief, Actions: fixture.Actions}
	case houseImageReviewVersion:
		if len(req.Images) == 0 {
			return nil, errors.New("missing image bytes")
		}
		var audit struct {
			Asset HouseImageAsset `json:"asset"`
		}
		if err := json.Unmarshal(req.Input, &audit); err != nil {
			return nil, err
		}
		out = HouseImageReview{AssetID: audit.Asset.ID, SHA256: audit.Asset.SHA256, Approved: !reject}
		if reject {
			out = HouseImageReview{AssetID: audit.Asset.ID, SHA256: audit.Asset.SHA256, Approved: false, Issues: []string{"房间数量与方案不一致"}}
		}
	case houseFinalReviewVersion:
		out = ReviewResult{Approved: true}
	default:
		return nil, errors.New("unexpected prompt version")
	}
	return json.Marshal(out)
}

func TestValidateHouseVisualRequiresExplicitCompleteApproval(t *testing.T) {
	input := houseDecisionInput(2, 4, FlexibleUseYes)
	tasks := buildHouseImageTasks(input, validHouseContentFixture(input).Schemes)
	ids := make([]string, 0, len(tasks))
	for _, task := range tasks {
		ids = append(ids, task.ID)
	}
	if err := validateHouseVisual(input, validHouseContentFixture(input).Schemes, HouseVisualResult{Approved: true, ApprovedTaskIDs: ids}); err != nil {
		t.Fatal(err)
	}
	if err := validateHouseVisual(input, validHouseContentFixture(input).Schemes, HouseVisualResult{Approved: false, Issues: []string{"层数不一致"}}); !errors.Is(err, ErrAgentRejected) {
		t.Fatalf("rejection=%v", err)
	}
	duplicate := append([]string(nil), ids...)
	duplicate[len(duplicate)-1] = duplicate[0]
	if err := validateHouseVisual(input, validHouseContentFixture(input).Schemes, HouseVisualResult{Approved: true, ApprovedTaskIDs: duplicate}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("duplicate approval=%v", err)
	}
}

func TestValidateHouseSpatialPreservesDirectionContract(t *testing.T) {
	input := houseDecisionInput(2, 4, FlexibleUseYes)
	directions := HouseSchemeDirections{Schemes: validHouseContentFixture(input).Schemes}
	result := HouseSpatialResult{Schemes: validHouseContentFixture(input).Schemes}
	if err := validateHouseSpatial(input, directions, result); err != nil {
		t.Fatal(err)
	}
	result.Schemes[0].Facade = "被空间Agent擅自改写"
	if err := validateHouseSpatial(input, directions, result); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("changed direction accepted: %v", err)
	}
}

type houseImageFixture struct {
	mu      sync.Mutex
	assets  map[string]HouseImageAsset
	creates int
	reads   int
}

func (s *houseImageFixture) Ensure(_ context.Context, _ *Report, task HouseImageTask) (HouseImageAsset, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.assets == nil {
		s.assets = map[string]HouseImageAsset{}
	}
	if asset, ok := s.assets[task.ID]; ok {
		return asset, false, nil
	}
	s.creates++
	digest := sha256.Sum256([]byte(task.ID))
	asset := HouseImageAsset{ID: task.ID, Asset: "house/fixture/" + task.ID + ".png", SchemeID: task.SchemeID, SchemeVersion: task.SchemeVersion, Kind: task.Kind, Floor: task.Floor, Width: 2048, Height: 1152, Model: "fixture", SHA256: hex.EncodeToString(digest[:]), ReferenceID: task.ReferenceID}
	s.assets[task.ID] = asset
	return asset, true, nil
}
func (s *houseImageFixture) Read(_ context.Context, _ *Report, asset HouseImageAsset) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reads++
	return append([]byte("\x89PNG\r\n\x1a\n"), []byte(asset.ID)...), nil
}

func TestHouseImageReviewUsesActualJPEGMIME(t *testing.T) {
	client := &houseAgentFixture{}
	store := &decisionCheckpoints{}
	runner, err := NewHouseOrchestrator(client, store, &houseImageFixture{})
	if err != nil {
		t.Fatal(err)
	}
	record := houseRecord(t, houseDecisionInput(2, 4, FlexibleUseYes))
	asset := HouseImageAsset{ID: "daily-floor-1", SHA256: strings.Repeat("a", 64)}
	task := HouseImageTask{ID: asset.ID, SchemeID: "daily", SchemeVersion: "v1", Kind: "floor_plan", Floor: 1, Prompt: "完整的结构化概念平面审核任务"}
	jpeg := append([]byte("\xff\xd8\xff\xe0\x00\x10JFIF\x00"), make([]byte, 32)...)
	if _, err = runner.reviewHouseImage(context.Background(), record, "house-test", task, asset, jpeg, nil, nil); err != nil {
		t.Fatal(err)
	}
	if len(client.imageMIMEs) != 1 || client.imageMIMEs[0] != "image/jpeg" {
		t.Fatalf("mimes=%v", client.imageMIMEs)
	}
}

func confirmHouseRecord(t *testing.T, record *Report, input DecisionInput, preview []byte) {
	t.Helper()
	record.PreviewSnapshot = preview
	record.Paid = true
	confirmation := DecisionConfirmation{DiagnosisID: PlanHouseDecision(input).ID, InputHash: record.InputHash, Choice: ChoiceUndecided, Priorities: input.Priorities}
	raw, err := json.Marshal(confirmation)
	if err != nil {
		t.Fatal(err)
	}
	record.ConfirmationJSON = raw
	record.ConfirmationHash = decisionDigest(raw)
}

func TestHouseOrchestratorDynamicPagesAndCheckpointResume(t *testing.T) {
	for _, floors := range []int{2, 3} {
		t.Run(string(rune('0'+floors))+"floors", func(t *testing.T) {
			input := houseDecisionInput(floors, floors+2, FlexibleUseYes)
			record := houseRecord(t, input)
			store := &decisionCheckpoints{}
			client := &houseAgentFixture{}
			images := &houseImageFixture{}
			runner, err := NewHouseOrchestrator(client, store, images)
			if err != nil {
				t.Fatal(err)
			}
			previewRaw, _, err := runner.Run(context.Background(), record, ModePreview, nil)
			if err != nil {
				t.Fatal(err)
			}
			confirmHouseRecord(t, record, input, previewRaw)
			var fullRaw json.RawMessage
			for attempts := 0; attempts < 30; attempts++ {
				fullRaw, _, err = runner.Run(context.Background(), record, ModeFull, nil)
				if errors.Is(err, ErrHousePending) {
					continue
				}
				break
			}
			if err != nil {
				t.Fatal(err)
			}
			var full DecisionSnapshot
			if err = json.Unmarshal(fullRaw, &full); err != nil {
				t.Fatal(err)
			}
			wantPages := 4 + 1 + 2*(floors+1) + 4
			if len(full.Pages) != wantPages {
				t.Fatalf("pages=%d want=%d", len(full.Pages), wantPages)
			}
			if err = ValidateHouseSnapshot(full, ModeFull); err != nil {
				t.Fatal(err)
			}
			before := map[string]int{}
			for k, v := range client.calls {
				before[k] = v
			}
			again, _, err := runner.Run(context.Background(), record, ModeFull, nil)
			if err != nil || string(again) != string(fullRaw) {
				t.Fatalf("resume %v", err)
			}
			for k, v := range before {
				if client.calls[k] != v {
					t.Fatalf("stage %s reran", k)
				}
			}
		})
	}
}

func TestHouseOrchestratorYieldsAfterEachNewImage(t *testing.T) {
	input := houseDecisionInput(2, 4, FlexibleUseYes)
	record := houseRecord(t, input)
	store := &decisionCheckpoints{}
	client := &houseAgentFixture{}
	images := &houseImageFixture{}
	runner, _ := NewHouseOrchestrator(client, store, images)
	preview, _, err := runner.Run(context.Background(), record, ModePreview, nil)
	if err != nil {
		t.Fatal(err)
	}
	confirmHouseRecord(t, record, input, preview)
	for expected := 1; expected <= 8; expected++ {
		_, _, err = runner.Run(context.Background(), record, ModeFull, nil)
		if !errors.Is(err, ErrHousePending) {
			t.Fatalf("yield %d: %v", expected, err)
		}
		if images.creates != expected {
			t.Fatalf("creates=%d", images.creates)
		}
	}
	if _, _, err = runner.Run(context.Background(), record, ModeFull, nil); err != nil {
		t.Fatal(err)
	}
}

func TestHouseOrchestratorRejectsMissingConfirmationAndFailedImageReview(t *testing.T) {
	input := houseDecisionInput(2, 4, FlexibleUseYes)
	record := houseRecord(t, input)
	store := &decisionCheckpoints{}
	client := &houseAgentFixture{}
	images := &houseImageFixture{}
	runner, _ := NewHouseOrchestrator(client, store, images)
	preview, _, err := runner.Run(context.Background(), record, ModePreview, nil)
	if err != nil {
		t.Fatal(err)
	}
	record.Paid = true
	record.PreviewSnapshot = preview
	if _, _, err = runner.Run(context.Background(), record, ModeFull, nil); !errors.Is(err, ErrConflict) {
		t.Fatalf("missing confirmation: %v", err)
	}
	confirmHouseRecord(t, record, input, preview)
	client.rejectImage = true
	for attempts := 0; attempts < 20; attempts++ {
		_, _, err = runner.Run(context.Background(), record, ModeFull, nil)
		if errors.Is(err, ErrHousePending) {
			continue
		}
		break
	}
	if !errors.Is(err, ErrAgentRejected) {
		t.Fatalf("image rejection=%v", err)
	}
}

func TestHouseOrchestratorDoesNotStartPaidImageNearDeadline(t *testing.T) {
	input := houseDecisionInput(2, 4, FlexibleUseYes)
	record := houseRecord(t, input)
	store := &decisionCheckpoints{}
	client := &houseAgentFixture{}
	images := &houseImageFixture{}
	runner, _ := NewHouseOrchestrator(client, store, images)
	preview, _, err := runner.Run(context.Background(), record, ModePreview, nil)
	if err != nil {
		t.Fatal(err)
	}
	confirmHouseRecord(t, record, input, preview)
	ctx, cancel := context.WithTimeout(context.Background(), 189*time.Second)
	defer cancel()
	if _, _, err = runner.Run(ctx, record, ModeFull, nil); !errors.Is(err, ErrHousePending) {
		t.Fatalf("deadline guard=%v", err)
	}
	if images.creates != 0 {
		t.Fatalf("started %d paid images", images.creates)
	}
}

func TestHouseImagePromptsCarrySiteAndWholeBuildingConstraints(t *testing.T) {
	two := houseDecisionInput(2, 4, FlexibleUseYes)
	two.House.NeighborNotes = "东侧已有两层邻房"
	two.BuildableScope = BuildableScopeKnown
	two.BuildableConstraints = "东侧退界3米，西侧保留排水通道"
	three := houseDecisionInput(3, 5, FlexibleUseYes)
	three.House.NeighborNotes = "东侧已有两层邻房"
	twoTasks := buildHouseImageTasks(two, validHouseContentFixture(two).Schemes)
	threeTasks := buildHouseImageTasks(three, validHouseContentFixture(three).Schemes)
	find := func(tasks []HouseImageTask, kind string) HouseImageTask {
		for _, task := range tasks {
			if task.SchemeID == "daily" && task.Kind == kind {
				return task
			}
		}
		t.Fatalf("missing %s", kind)
		return HouseImageTask{}
	}
	floor := find(twoTasks, "floor_plan")
	massing2 := find(twoTasks, "massing")
	massing3 := find(threeTasks, "massing")
	exterior := find(twoTasks, "exterior")
	for name, prompt := range map[string]string{"floor": floor.Prompt, "massing": massing2.Prompt, "exterior": exterior.Prompt} {
		for _, want := range []string{"泉州南安", "12m×16m", "用户估算", "可建范围known", "东侧退界3米，西侧保留排水通道", "south", "坐北朝南", "东侧已有两层邻房", "庭院需要", "停车需要", "daily", "daily-v1"} {
			if !strings.Contains(prompt, want) {
				t.Fatalf("%s prompt missing %q: %s", name, want, prompt)
			}
		}
	}
	if !strings.Contains(massing2.Prompt, "总层数2层") || !strings.Contains(massing3.Prompt, "总层数3层") || massing2.Prompt == massing3.Prompt {
		t.Fatalf("massing floors not differentiated: %s / %s", massing2.Prompt, massing3.Prompt)
	}
	if !strings.Contains(floor.Prompt, "相邻楼层") {
		t.Fatalf("floor stair relation missing: %s", floor.Prompt)
	}
}

func TestHouseFullRejectsChangedFrozenPreviewAndCachedContext(t *testing.T) {
	input := houseDecisionInput(2, 4, FlexibleUseYes)
	record := houseRecord(t, input)
	preview, err := BuildHousePreview(record, input, PlanHouseDecision(input))
	if err != nil {
		t.Fatal(err)
	}
	preview.Meta.GenerationMode = GenerationMultiAgent
	previewRaw, _ := json.Marshal(preview)
	confirmHouseRecord(t, record, input, previewRaw)
	full := buildHouseFullSnapshotFixture(t, record, input, preview, validHouseContentFixture(input))
	full.Meta.GenerationMode = GenerationMultiAgent
	if !houseCachedRunMatches(record, ModeFull, input, full) {
		t.Fatal("valid cached run rejected")
	}
	full.Facts["intent.bedrooms"] = "9"
	if houseCachedRunMatches(record, ModeFull, input, full) {
		t.Fatal("changed cached facts accepted")
	}
	preview.Facts["intent.bedrooms"] = "9"
	tampered, _ := json.Marshal(preview)
	record.PreviewSnapshot = tampered
	runner, _ := NewHouseOrchestrator(&houseAgentFixture{}, &decisionCheckpoints{}, &houseImageFixture{})
	if _, _, err = runner.Run(context.Background(), record, ModeFull, nil); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("tampered preview=%v", err)
	}
}
