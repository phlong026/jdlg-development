package report

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"
)

type decisionCheckpoints struct {
	mu     sync.Mutex
	data   map[string]json.RawMessage
	stages []string
}

func (s *decisionCheckpoints) LoadStage(_ context.Context, id, key, stage string) (json.RawMessage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	value, ok := s.data[id+key+stage]
	if !ok {
		return nil, ErrNotFound
	}
	return append(json.RawMessage(nil), value...), nil
}
func (s *decisionCheckpoints) SaveStage(_ context.Context, id, lease, key, stage string, raw json.RawMessage) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.data == nil {
		s.data = map[string]json.RawMessage{}
	}
	k := id + key + stage
	if old, ok := s.data[k]; ok && string(old) != string(raw) {
		return ErrConflict
	}
	s.data[k] = append(json.RawMessage(nil), raw...)
	return nil
}
func (s *decisionCheckpoints) SetDecisionStage(_ context.Context, id, lease, stage string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stages = append(s.stages, stage)
	return nil
}

type decisionClientFixture struct {
	mu              sync.Mutex
	calls           map[string]int
	failRole        string
	reject          bool
	decisionEntered chan struct{}
	budgetEntered   chan struct{}
}

func (c *decisionClientFixture) Identity() AgentIdentity {
	return AgentIdentity{ConfigVersion: "test-v5", Models: map[string]string{"planner": "model", "writer": "model", "visual": "model", "reviewer": "model", "diagnosis": "model", "decision": "model", "budget": "model", "editor": "model"}}
}
func (c *decisionClientFixture) Run(ctx context.Context, r AgentRequest) (json.RawMessage, error) {
	c.mu.Lock()
	if c.calls == nil {
		c.calls = map[string]int{}
	}
	c.calls[r.Role]++
	fail := c.failRole == r.Role
	reject := c.reject
	c.mu.Unlock()
	if fail {
		return nil, errors.New("test transient failure")
	}
	var in decisionAgentInput
	if err := json.Unmarshal(r.Input, &in); err != nil {
		return nil, err
	}
	if r.Role == "decision" && c.decisionEntered != nil {
		close(c.decisionEntered)
		select {
		case <-c.budgetEntered:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if r.Role == "budget" && c.budgetEntered != nil {
		close(c.budgetEntered)
		select {
		case <-c.decisionEntered:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	var value any
	switch r.Role {
	case "diagnosis":
		value = PlanDecision(in.Input)
	case "decision":
		blocks := []ContentBlock{{Title: "需要比较的生活影响", Body: "结合已确认的家庭使用方式，核对不同安排的代价与成立条件。"}, {Title: "下一步核实", Body: "把待确认事项交给设计师讨论，再决定推进顺序。"}}
		value = DecisionAnalysisNotes{FirstSection: blocks, SecondSection: blocks, EvidenceRefs: in.Brief.EvidenceRefs}
	case "budget":
		value = DecisionStageNotes{Blocks: []ContentBlock{{Title: "需要比较的生活影响", Body: "结合已确认的家庭使用方式，核对不同安排的代价与成立条件。"}, {Title: "下一步核实", Body: "把待确认事项交给设计师讨论，再决定推进顺序。"}}, EvidenceRefs: append(budgetFactKeys(in.Facts), "intent.priorities")}
	case "editor":
		page, block := 0, 0
		for _, p := range in.Snapshot.Pages {
			if (p.Number == 4 || p.Number == 5) && len(p.Blocks) > 0 {
				page = p.Number
				break
			}
		}
		value = WriterResult{Patches: []NarrativePatch{{Page: page, BlockIndex: block, Body: "建议结合已经确认的生活优先级，逐项核实适用条件后再作选择。", EvidenceRefs: in.Snapshot.Pages[page-1].Sources}}}
	case "reviewer":
		value = ReviewResult{Approved: !reject}
		if reject {
			value = ReviewResult{Approved: false, Issues: []ReviewIssue{{Page: 1, Code: "unsupported", Message: "结论需要进一步核对"}}}
		}
	default:
		return nil, ErrInvalidInput
	}
	return json.Marshal(value)
}

func TestDecisionEditorCannotEraseConfirmedBoundaries(t *testing.T) {
	record, input := decisionRunFixture(t)
	brief := PlanDecision(input)
	confirmation := DecisionConfirmation{DiagnosisID: brief.ID, InputHash: record.InputHash, Choice: ChoiceOptionA, Priorities: input.Priorities}
	blocks := []ContentBlock{{Title: "条件", Body: "核对已有资料后再决定下一步。"}, {Title: "代价", Body: "保留还没有确认的条件。"}}
	base, err := BuildDecisionFull(record, input, confirmation, brief, blocks, blocks)
	if err != nil {
		t.Fatal(err)
	}
	for _, target := range [][2]int{{1, 0}, {1, 1}, {2, 0}, {6, 0}, {8, 0}, {8, 1}, {10, 0}} {
		_, err = applyDecisionEdits(base, WriterResult{Patches: []NarrativePatch{{Page: target[0], BlockIndex: target[1], Body: "可以直接继续推进。", EvidenceRefs: brief.EvidenceRefs}}})
		if err == nil {
			t.Fatalf("editor erased protected page/block %v", target)
		}
	}
}

func TestDiagnosisEnhancementCannotInventNumbersFromKnownBudget(t *testing.T) {
	input := seasonalSpaceInput()
	for _, claim := range []string{"整体造价为999万元，可以直接安排后续工作。", "整体造价为九百万元，可以直接安排后续工作。"} {
		brief := PlanDecision(input)
		brief.Reason = claim
		if validateDecisionAgentBrief(input, brief) == nil {
			t.Fatal("model invented a new numerical claim")
		}
	}
	if err := validateDecisionAgentBrief(input, PlanDecision(input)); err != nil {
		t.Fatal(err)
	}
}

func TestDecisionConfirmationHashSurvivesDatabaseJSONFormatting(t *testing.T) {
	client := &decisionClientFixture{}
	o, _ := NewReportOrchestrator(client, &decisionCheckpoints{})
	record, input := decisionRunFixture(t)
	raw, _, err := o.Run(context.Background(), record, ModePreview, nil)
	if err != nil {
		t.Fatal(err)
	}
	record.PreviewSnapshot = raw
	confirmRunFixture(t, record, input)
	var pretty map[string]any
	if err = json.Unmarshal(record.ConfirmationJSON, &pretty); err != nil {
		t.Fatal(err)
	}
	record.ConfirmationJSON, err = json.MarshalIndent(pretty, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = o.Run(context.Background(), record, ModeFull, nil); err != nil {
		t.Fatalf("MySQL normalized JSON must preserve confirmation hash: %v", err)
	}
}

func decisionRunFixture(t *testing.T) (*Report, DecisionInput) {
	t.Helper()
	input := seasonalSpaceInput()
	record := testReportForDecision(t, input)
	record.UserID = 11
	record.LeaseToken = "lease-v5"
	record.CreatedAt = time.Now()
	return record, input
}
func confirmRunFixture(t *testing.T, record *Report, input DecisionInput) {
	t.Helper()
	record.Paid = true
	record.Status = StatusFullGenerating
	raw, err := json.Marshal(DecisionConfirmation{DiagnosisID: PlanDecision(input).ID, InputHash: record.InputHash, Choice: ChoiceUndecided, Priorities: input.Priorities})
	if err != nil {
		t.Fatal(err)
	}
	record.ConfirmationJSON = raw
	record.ConfirmationHash = decisionDigest(raw)
}

func TestReportOrchestratorRunsFourAndTenPagesWithCheckpointReuse(t *testing.T) {
	ctx := context.Background()
	store := &decisionCheckpoints{}
	client := &decisionClientFixture{}
	runner, err := NewReportOrchestrator(client, store)
	if err != nil {
		t.Fatal(err)
	}
	record, input := decisionRunFixture(t)
	preview, _, err := runner.Run(ctx, record, ModePreview, nil)
	if err != nil {
		t.Fatal(err)
	}
	record.PreviewSnapshot = preview
	var p DecisionSnapshot
	if err = json.Unmarshal(preview, &p); err != nil || len(p.Pages) != 4 {
		t.Fatalf("preview %v", err)
	}
	confirmRunFixture(t, record, input)
	full, _, err := runner.Run(ctx, record, ModeFull, nil)
	if err != nil {
		t.Fatal(err)
	}
	var f DecisionSnapshot
	if err = json.Unmarshal(full, &f); err != nil || len(f.Pages) != 10 {
		t.Fatalf("full %v", err)
	}
	calls := 0
	for _, n := range client.calls {
		calls += n
	}
	again, _, err := runner.Run(ctx, record, ModeFull, nil)
	if err != nil || string(again) != string(full) {
		t.Fatalf("resume %v", err)
	}
	after := 0
	for _, n := range client.calls {
		after += n
	}
	if after != calls {
		t.Fatalf("checkpoint miss calls=%d -> %d", calls, after)
	}
	if string(record.PreviewSnapshot) != string(preview) {
		t.Fatal("full rewrote preview")
	}
}

func TestReportOrchestratorParallelAnalysisAndBoundedReview(t *testing.T) {
	store := &decisionCheckpoints{}
	client := &decisionClientFixture{}
	runner, _ := NewReportOrchestrator(client, store)
	record, input := decisionRunFixture(t)
	preview, _, err := runner.Run(context.Background(), record, ModePreview, nil)
	if err != nil {
		t.Fatal(err)
	}
	record.PreviewSnapshot = preview
	confirmRunFixture(t, record, input)
	client.decisionEntered = make(chan struct{})
	client.budgetEntered = make(chan struct{})
	client.reject = true
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if _, _, err = runner.Run(ctx, record, ModeFull, nil); !errors.Is(err, ErrAgentRejected) {
		t.Fatalf("review failure=%v", err)
	}
	if client.calls["editor"] != 2 {
		t.Fatalf("repair budget not enforced: %v", client.calls)
	}
}

func TestReportOrchestratorRejectsUnpaidOrUnconfirmedAndTamperedInput(t *testing.T) {
	runner, _ := NewReportOrchestrator(&decisionClientFixture{}, &decisionCheckpoints{})
	record, input := decisionRunFixture(t)
	if _, _, err := runner.Run(context.Background(), record, ModeFull, nil); !errors.Is(err, ErrPaymentRequired) {
		t.Fatalf("unpaid=%v", err)
	}
	record.Paid = true
	if _, _, err := runner.Run(context.Background(), record, ModeFull, nil); err == nil {
		t.Fatal("unconfirmed accepted")
	}
	record.InputHash = "bad"
	if _, _, err := runner.Run(context.Background(), record, ModePreview, nil); err == nil {
		t.Fatal("tampered input accepted")
	}
	_ = input
}

func TestReportOrchestratorResumesOnlyMissingAnalysis(t *testing.T) {
	store := &decisionCheckpoints{}
	client := &decisionClientFixture{}
	runner, _ := NewReportOrchestrator(client, store)
	record, input := decisionRunFixture(t)
	preview, _, err := runner.Run(context.Background(), record, ModePreview, nil)
	if err != nil {
		t.Fatal(err)
	}
	record.PreviewSnapshot = preview
	confirmRunFixture(t, record, input)
	client.failRole = "budget"
	if _, _, err := runner.Run(context.Background(), record, ModeFull, nil); err == nil {
		t.Fatal("failed dependency accepted")
	}
	client.failRole = ""
	before := client.calls["decision"]
	if _, _, err := runner.Run(context.Background(), record, ModeFull, nil); err != nil {
		t.Fatal(err)
	}
	if client.calls["decision"] != before || client.calls["budget"] != 2 {
		t.Fatal("retry must rerun only the failed budget stage")
	}
}

func TestDecisionFinalCheckpointRejectsChangedFactsAndIdentity(t *testing.T) {
	for _, field := range []string{"facts", "recipe", "diagnosis", "mode"} {
		t.Run(field, func(t *testing.T) {
			store := &decisionCheckpoints{}
			runner, err := NewReportOrchestrator(&decisionClientFixture{}, store)
			if err != nil {
				t.Fatal(err)
			}
			record, _ := decisionRunFixture(t)
			if _, _, err = runner.Run(context.Background(), record, ModePreview, nil); err != nil {
				t.Fatal(err)
			}
			key, err := runner.runKey(record, ModePreview)
			if err != nil {
				t.Fatal(err)
			}
			cacheKey := record.ID + key + "final"
			var envelope checkpointEnvelope
			if err = json.Unmarshal(store.data[cacheKey], &envelope); err != nil {
				t.Fatal(err)
			}
			var snapshot DecisionSnapshot
			if err = json.Unmarshal(envelope.Payload, &snapshot); err != nil {
				t.Fatal(err)
			}
			switch field {
			case "facts":
				snapshot.Facts["household.permanent_count"] = "9"
			case "recipe":
				snapshot.Recipe = GoalBudget
				snapshot.Diagnosis.Recipe = GoalBudget
			case "diagnosis":
				snapshot.Diagnosis.ID = "diagnosis-other"
			case "mode":
				snapshot.Meta.GenerationMode = GenerationRulesOnly
			}
			envelope.Payload, err = json.Marshal(snapshot)
			if err != nil {
				t.Fatal(err)
			}
			store.data[cacheKey], err = json.Marshal(envelope)
			if err != nil {
				t.Fatal(err)
			}
			if _, _, err = runner.Run(context.Background(), record, ModePreview, nil); !errors.Is(err, ErrConflict) {
				t.Fatalf("poisoned %s cache accepted: %v", field, err)
			}
		})
	}
}
