package report

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"reflect"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
	"time"
)

const GenerationMultiAgent = "multi_agent"

const (
	agentProtocolVersion  = "report-multi-agent-v1"
	templateVersion       = "report-a4-v1"
	plannerPromptVersion  = "planner-v1"
	visualPromptVersion   = "visual-v1"
	writerPromptVersion   = "writer-v1"
	reviewerPromptVersion = "reviewer-v1"
)

var (
	ErrAgentUnavailable = errors.New("report: multi-agent service unavailable")
	ErrAgentRejected    = errors.New("report: multi-agent review rejected")
)

type AgentRunner interface {
	Run(ctx context.Context, record *Report, mode string, pack *ReviewedPack) (json.RawMessage, string, error)
}

type StageCheckpointStore interface {
	LoadStage(ctx context.Context, reportID, runKey, stage string) (json.RawMessage, error)
	SaveStage(ctx context.Context, reportID, leaseToken, runKey, stage string, payload json.RawMessage) error
}

type AgentClient interface {
	Run(ctx context.Context, request AgentRequest) (json.RawMessage, error)
	Identity() AgentIdentity
}

type AgentIdentity struct {
	ConfigVersion string            `json:"config_version"`
	Models        map[string]string `json:"models"`
}

type AgentRequest struct {
	Role          string          `json:"role"`
	Model         string          `json:"model"`
	PromptVersion string          `json:"prompt_version"`
	SystemPrompt  string          `json:"system_prompt"`
	Input         json.RawMessage `json:"input"`
}

type MultiAgentRunner struct {
	client      AgentClient
	checkpoints StageCheckpointStore
	identity    AgentIdentity
}

type PlanResult struct {
	Sections []PlanSection `json:"sections"`
}

type PlanSection struct {
	Page         int      `json:"page"`
	Objective    string   `json:"objective"`
	EvidenceRefs []string `json:"evidence_refs"`
}

type VisualResult struct {
	Selections []VisualSelection `json:"selections"`
}

type VisualSelection struct {
	Page     int      `json:"page"`
	AssetIDs []string `json:"asset_ids"`
}

type WriterResult struct {
	Patches []NarrativePatch `json:"patches"`
}

type NarrativePatch struct {
	Page         int      `json:"page"`
	BlockIndex   int      `json:"block_index"`
	Body         string   `json:"body"`
	EvidenceRefs []string `json:"evidence_refs"`
}

type ReviewResult struct {
	Approved bool          `json:"approved"`
	Issues   []ReviewIssue `json:"issues"`
}

type ReviewIssue struct {
	Page    int    `json:"page"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

type checkpointEnvelope struct {
	PromptVersion string          `json:"prompt_version"`
	Model         string          `json:"model,omitempty"`
	DurationMS    int64           `json:"duration_ms,omitempty"`
	Payload       json.RawMessage `json:"payload"`
}

type finalCheckpoint struct {
	RunKey         string            `json:"run_key"`
	GenerationMode string            `json:"generation_mode"`
	Protocol       string            `json:"agent_protocol"`
	Models         map[string]string `json:"models"`
	Snapshot       json.RawMessage   `json:"snapshot"`
}

func NewMultiAgentRunner(client AgentClient, checkpoints StageCheckpointStore) (*MultiAgentRunner, error) {
	if client == nil || checkpoints == nil {
		return nil, ErrInvalidInput
	}
	identity := client.Identity()
	if !validAgentIdentity(identity) {
		return nil, ErrAgentUnavailable
	}
	identity.Models = cloneStringMap(identity.Models)
	return &MultiAgentRunner{client: client, checkpoints: checkpoints, identity: identity}, nil
}

func (r *MultiAgentRunner) Run(ctx context.Context, record *Report, mode string, pack *ReviewedPack) (json.RawMessage, string, error) {
	if record == nil || record.ID == "" || record.LeaseToken == "" || (mode != ModePreview && mode != ModeFull) {
		return nil, "", ErrInvalidInput
	}
	if mode == ModeFull && pack == nil {
		return nil, "", ErrPackUnavailable
	}
	runKey, err := r.runKey(record, mode, pack)
	if err != nil {
		return nil, "", err
	}
	base, err := r.loadOrBuildBase(ctx, record, mode, pack, runKey)
	if err != nil {
		return nil, "", err
	}
	var canonical Snapshot
	if err := strictDecode(base, &canonical); err != nil {
		return nil, "", ErrInvalidInput
	}
	if final, found, err := r.loadFinal(ctx, record.ID, runKey, mode, canonical); err != nil {
		return nil, "", err
	} else if found {
		return final, GenerationMultiAgent, nil
	}
	agentSnapshot := sanitizeSnapshotForAgent(canonical)

	planInput := struct {
		Mode     string   `json:"mode"`
		Snapshot Snapshot `json:"snapshot"`
	}{mode, agentSnapshot}
	plan, err := runTypedStage[PlanResult](ctx, r, record, runKey, "plan", "planner", plannerPromptVersion, plannerPrompt, planInput, func(value PlanResult) error {
		return validatePlan(value, mode)
	})
	if err != nil {
		return nil, "", err
	}

	visualInput := struct {
		Mode     string           `json:"mode"`
		Plan     PlanResult       `json:"plan"`
		AssetIDs []string         `json:"approved_asset_ids"`
		Schemes  []SnapshotScheme `json:"approved_schemes"`
	}{Mode: mode, Plan: plan, AssetIDs: sortedAssetIDs(canonical.Assets), Schemes: canonical.Schemes}
	visual, err := runTypedStage[VisualResult](ctx, r, record, runKey, "visual", "visual", visualPromptVersion, visualPrompt, visualInput, func(value VisualResult) error {
		return validateVisual(value, mode, canonical)
	})
	if err != nil {
		return nil, "", err
	}

	writerInput := struct {
		Mode     string       `json:"mode"`
		Plan     PlanResult   `json:"plan"`
		Visual   VisualResult `json:"visual"`
		Snapshot Snapshot     `json:"canonical_snapshot"`
	}{mode, plan, visual, agentSnapshot}
	draft, err := runTypedStage[WriterResult](ctx, r, record, runKey, "draft", "writer", writerPromptVersion, writerPrompt, writerInput, func(value WriterResult) error {
		return validateWriter(value, mode, plan)
	})
	if err != nil {
		return nil, "", err
	}
	candidate, err := applyWriter(canonical, mode, draft)
	if err != nil {
		return nil, "", err
	}

	review, err := r.review(ctx, record, runKey, "review", candidate, plan, visual)
	if err != nil {
		return nil, "", err
	}
	if !review.Approved {
		repairInput := struct {
			Mode     string        `json:"mode"`
			Plan     PlanResult    `json:"plan"`
			Visual   VisualResult  `json:"visual"`
			Snapshot Snapshot      `json:"rejected_snapshot"`
			Issues   []ReviewIssue `json:"issues"`
		}{mode, plan, visual, sanitizeSnapshotForAgent(candidate), review.Issues}
		repair, repairErr := runTypedStage[WriterResult](ctx, r, record, runKey, "repair", "writer", writerPromptVersion, writerRepairPrompt, repairInput, func(value WriterResult) error {
			return validateWriter(value, mode, plan)
		})
		if repairErr != nil {
			return nil, "", repairErr
		}
		candidate, err = applyWriter(canonical, mode, repair)
		if err != nil {
			return nil, "", err
		}
		review, err = r.review(ctx, record, runKey, "review_recheck", candidate, plan, visual)
		if err != nil {
			return nil, "", err
		}
		if !review.Approved {
			return nil, "", ErrAgentRejected
		}
	}
	candidate.Meta.GenerationMode = GenerationMultiAgent

	finalBytes, err := json.Marshal(candidate)
	if err != nil {
		return nil, "", ErrInvalidInput
	}
	final := finalCheckpoint{RunKey: runKey, GenerationMode: GenerationMultiAgent, Protocol: agentProtocolVersion, Models: cloneStringMap(r.identity.Models), Snapshot: finalBytes}
	if err := r.saveEnvelope(ctx, record, runKey, "final", agentProtocolVersion, "", final); err != nil {
		return nil, "", err
	}
	return finalBytes, GenerationMultiAgent, nil
}

func (r *MultiAgentRunner) review(ctx context.Context, record *Report, runKey, stage string, candidate Snapshot, plan PlanResult, visual VisualResult) (ReviewResult, error) {
	input := struct {
		Plan     PlanResult   `json:"plan"`
		Visual   VisualResult `json:"visual"`
		Snapshot Snapshot     `json:"snapshot"`
	}{plan, visual, sanitizeSnapshotForAgent(candidate)}
	return runTypedStage[ReviewResult](ctx, r, record, runKey, stage, "reviewer", reviewerPromptVersion, reviewerPrompt, input, func(value ReviewResult) error {
		return validateReview(value, len(candidate.Pages))
	})
}

func (r *MultiAgentRunner) loadOrBuildBase(ctx context.Context, record *Report, mode string, pack *ReviewedPack, runKey string) (json.RawMessage, error) {
	var stored json.RawMessage
	if found, err := r.loadEnvelope(ctx, record.ID, runKey, "base", templateVersion, "", &stored); err != nil {
		return nil, err
	} else if found {
		return stored, nil
	}
	var base json.RawMessage
	var err error
	if mode == ModePreview {
		base, _, err = NewGenerator(nil).BuildPreview(ctx, record)
	} else {
		var preview Snapshot
		_, facts, factsErr := inputAndFacts(record)
		if factsErr != nil || strictDecode(record.PreviewSnapshot, &preview) != nil || preview.Meta.ReportID != record.ID || preview.Meta.InputHash != record.InputHash || preview.Meta.ProtocolVersion != record.ProtocolVersion || !reflect.DeepEqual(preview.Facts, facts) {
			return nil, ErrInvalidInput
		}
		base, err = NewGenerator(nil).BuildFull(record.PreviewSnapshot, *pack)
	}
	if err != nil {
		return nil, err
	}
	if err := r.saveEnvelope(ctx, record, runKey, "base", templateVersion, "", base); err != nil {
		return nil, err
	}
	return base, nil
}

func (r *MultiAgentRunner) loadFinal(ctx context.Context, reportID, runKey, mode string, canonical Snapshot) (json.RawMessage, bool, error) {
	var final finalCheckpoint
	found, err := r.loadEnvelope(ctx, reportID, runKey, "final", agentProtocolVersion, "", &final)
	if err != nil || !found {
		return nil, found, err
	}
	if final.RunKey != runKey || final.GenerationMode != GenerationMultiAgent || final.Protocol != agentProtocolVersion || !reflect.DeepEqual(final.Models, r.identity.Models) {
		return nil, false, ErrConflict
	}
	var snapshot Snapshot
	if len(final.Snapshot) == 0 || strictDecode(final.Snapshot, &snapshot) != nil || validateFinalAgainstCanonical(snapshot, canonical, mode) != nil {
		return nil, false, ErrConflict
	}
	return append(json.RawMessage(nil), final.Snapshot...), true, nil
}

func runTypedStage[T any, I any](ctx context.Context, runner *MultiAgentRunner, record *Report, runKey, stage, role, promptVersion, prompt string, input I, validate func(T) error) (T, error) {
	var zero T
	model := runner.identity.Models[role]
	var result T
	if found, err := runner.loadEnvelope(ctx, record.ID, runKey, stage, promptVersion, model, &result); err != nil {
		return zero, err
	} else if found {
		log.Printf("[report-agent] report_id=%s stage=%s role=%s cache_hit=true", record.ID, stage, role)
		if err := validate(result); err != nil {
			return zero, ErrConflict
		}
		return result, nil
	}
	inputBytes, err := json.Marshal(input)
	if err != nil {
		return zero, ErrInvalidInput
	}
	started := time.Now()
	raw, err := runner.client.Run(ctx, AgentRequest{Role: role, Model: model, PromptVersion: promptVersion, SystemPrompt: prompt, Input: inputBytes})
	duration := time.Since(started)
	if err != nil {
		log.Printf("[report-agent] report_id=%s stage=%s role=%s cache_hit=false duration_ms=%d result=failed", record.ID, stage, role, duration.Milliseconds())
		return zero, ErrAgentUnavailable
	}
	if err := strictDecode(raw, &result); err != nil || validate(result) != nil {
		log.Printf("[report-agent] report_id=%s stage=%s role=%s cache_hit=false duration_ms=%d result=invalid", record.ID, stage, role, duration.Milliseconds())
		return zero, ErrAgentUnavailable
	}
	if err := runner.saveEnvelopeWithDuration(ctx, record, runKey, stage, promptVersion, model, duration.Milliseconds(), result); err != nil {
		return zero, err
	}
	log.Printf("[report-agent] report_id=%s stage=%s role=%s cache_hit=false duration_ms=%d result=ok", record.ID, stage, role, duration.Milliseconds())
	return result, nil
}

func (r *MultiAgentRunner) loadEnvelope(ctx context.Context, reportID, runKey, stage, promptVersion, model string, target any) (bool, error) {
	data, err := r.checkpoints.LoadStage(ctx, reportID, runKey, stage)
	if errors.Is(err, ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	var envelope checkpointEnvelope
	if strictDecode(data, &envelope) != nil || envelope.PromptVersion != promptVersion || envelope.Model != model || len(envelope.Payload) == 0 || strictDecode(envelope.Payload, target) != nil {
		return false, ErrConflict
	}
	return true, nil
}

func (r *MultiAgentRunner) saveEnvelope(ctx context.Context, record *Report, runKey, stage, promptVersion, model string, payload any) error {
	return r.saveEnvelopeWithDuration(ctx, record, runKey, stage, promptVersion, model, 0, payload)
}

func (r *MultiAgentRunner) saveEnvelopeWithDuration(ctx context.Context, record *Report, runKey, stage, promptVersion, model string, durationMS int64, payload any) error {
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return ErrInvalidInput
	}
	envelope, err := json.Marshal(checkpointEnvelope{PromptVersion: promptVersion, Model: model, DurationMS: durationMS, Payload: payloadBytes})
	if err != nil {
		return ErrInvalidInput
	}
	return r.checkpoints.SaveStage(ctx, record.ID, record.LeaseToken, runKey, stage, envelope)
}

func (r *MultiAgentRunner) runKey(record *Report, mode string, pack *ReviewedPack) (string, error) {
	identityJSON, err := json.Marshal(r.identity)
	if err != nil {
		return "", ErrInvalidInput
	}
	hash := sha256.New()
	for _, value := range [][]byte{[]byte(agentProtocolVersion), []byte(templateVersion), []byte(plannerPromptVersion), []byte(visualPromptVersion), []byte(writerPromptVersion), []byte(reviewerPromptVersion), identityJSON, []byte(mode), []byte(record.InputHash), []byte(record.ProtocolVersion), record.InputJSON, record.PreviewSnapshot} {
		_, _ = hash.Write(value)
		_, _ = hash.Write([]byte{0})
	}
	if pack != nil {
		packJSON, marshalErr := json.Marshal(pack)
		if marshalErr != nil {
			return "", ErrInvalidInput
		}
		_, _ = hash.Write(packJSON)
	}
	return "ma-" + hex.EncodeToString(hash.Sum(nil)), nil
}

func validAgentIdentity(identity AgentIdentity) bool {
	if !validIdentifier(identity.ConfigVersion, 64) {
		return false
	}
	for _, role := range []string{"planner", "visual", "writer", "reviewer"} {
		if !validIdentifier(identity.Models[role], 100) {
			return false
		}
	}
	return true
}

func validatePlan(plan PlanResult, mode string) error {
	want := 4
	if mode == ModeFull {
		want = 14
	}
	if len(plan.Sections) != want {
		return ErrInvalidInput
	}
	seen := make(map[int]bool, want)
	for _, section := range plan.Sections {
		if section.Page < 1 || section.Page > want || seen[section.Page] || !boundedAgentText(section.Objective, 2, 160) || len(section.EvidenceRefs) < 1 || len(section.EvidenceRefs) > 8 {
			return ErrInvalidInput
		}
		seen[section.Page] = true
		for _, ref := range section.EvidenceRefs {
			if !validEvidenceRef(ref, mode) {
				return ErrInvalidInput
			}
		}
	}
	return nil
}

func validEvidenceRef(value, mode string) bool {
	if len(value) < 3 || len(value) > 100 || strings.ContainsAny(value, " \t\r\n:/\\") {
		return false
	}
	allowed := []string{"facts.", "preview."}
	if mode == ModeFull {
		allowed = append(allowed, "pack.", "assets.", "budget.")
	}
	for _, prefix := range allowed {
		if strings.HasPrefix(value, prefix) {
			return true
		}
	}
	return false
}

func validateVisual(result VisualResult, mode string, snapshot Snapshot) error {
	if mode == ModePreview {
		if len(result.Selections) != 0 {
			return ErrInvalidInput
		}
		return nil
	}
	expected := make(map[int][]string, 6)
	for index, scheme := range snapshot.Schemes {
		planPage := 6 + index*2
		for _, floor := range scheme.Floors {
			expected[planPage] = append(expected[planPage], floor.AssetID)
		}
		expected[planPage+1] = []string{scheme.FacadeAsset, scheme.VolumeAsset}
	}
	if len(result.Selections) != len(expected) {
		return ErrInvalidInput
	}
	seen := make(map[int]bool, len(expected))
	for _, selection := range result.Selections {
		want, ok := expected[selection.Page]
		if !ok || seen[selection.Page] || !sameStringSet(selection.AssetIDs, want) {
			return ErrInvalidInput
		}
		seen[selection.Page] = true
		for _, id := range selection.AssetIDs {
			if _, approved := snapshot.Assets[id]; !approved {
				return ErrInvalidInput
			}
		}
	}
	return nil
}

func validateWriter(result WriterResult, mode string, plan PlanResult) error {
	if len(result.Patches) < 1 || len(result.Patches) > 8 {
		return ErrInvalidInput
	}
	seen := make(map[string]bool, len(result.Patches))
	for _, patch := range result.Patches {
		key := fmt.Sprintf("%d/%d", patch.Page, patch.BlockIndex)
		if seen[key] || !writerTargetAllowed(mode, patch.Page, patch.BlockIndex) || !safeAgentNarrative(patch.Body) || len(patch.EvidenceRefs) < 1 || len(patch.EvidenceRefs) > 6 {
			return ErrInvalidInput
		}
		allowedEvidence := planEvidence(plan, patch.Page)
		for _, ref := range patch.EvidenceRefs {
			if !allowedEvidence[ref] {
				return ErrInvalidInput
			}
		}
		seen[key] = true
	}
	return nil
}

func writerTargetAllowed(mode string, page, block int) bool {
	allowed := map[string]bool{
		ModePreview + "/3/0": true, ModePreview + "/4/0": true, ModePreview + "/4/1": true,
		ModeFull + "/5/0": true, ModeFull + "/12/0": true, ModeFull + "/13/0": true,
		ModeFull + "/13/1": true, ModeFull + "/14/0": true,
	}
	return allowed[fmt.Sprintf("%s/%d/%d", mode, page, block)]
}

func applyWriter(canonical Snapshot, mode string, result WriterResult) (Snapshot, error) {
	data, err := json.Marshal(canonical)
	if err != nil {
		return Snapshot{}, ErrInvalidInput
	}
	var candidate Snapshot
	if strictDecode(data, &candidate) != nil {
		return Snapshot{}, ErrInvalidInput
	}
	for _, patch := range result.Patches {
		page := &candidate.Pages[patch.Page-1]
		if patch.BlockIndex < 0 || patch.BlockIndex >= len(page.Blocks) {
			return Snapshot{}, ErrInvalidInput
		}
		page.Blocks[patch.BlockIndex].Body = patch.Body
	}
	if mode == ModeFull && !reflect.DeepEqual(candidate.Pages[:4], canonical.Pages[:4]) {
		return Snapshot{}, ErrInvalidInput
	}
	if !reflect.DeepEqual(candidate.Facts, canonical.Facts) || !reflect.DeepEqual(candidate.Assets, canonical.Assets) || !reflect.DeepEqual(candidate.Budget, canonical.Budget) || !reflect.DeepEqual(candidate.Schemes, canonical.Schemes) || candidate.Meta != canonical.Meta {
		return Snapshot{}, ErrInvalidInput
	}
	return candidate, nil
}

func validateFinalAgainstCanonical(final, canonical Snapshot, mode string) error {
	if len(final.Pages) != len(canonical.Pages) || final.Meta.GenerationMode != GenerationMultiAgent {
		return ErrConflict
	}
	data, err := json.Marshal(final)
	if err != nil {
		return ErrConflict
	}
	var normalized Snapshot
	if strictDecode(data, &normalized) != nil {
		return ErrConflict
	}
	for pageIndex := range normalized.Pages {
		if len(normalized.Pages[pageIndex].Blocks) != len(canonical.Pages[pageIndex].Blocks) {
			return ErrConflict
		}
		for blockIndex := range normalized.Pages[pageIndex].Blocks {
			if writerTargetAllowed(mode, pageIndex+1, blockIndex) {
				normalized.Pages[pageIndex].Blocks[blockIndex].Body = canonical.Pages[pageIndex].Blocks[blockIndex].Body
			}
		}
	}
	normalized.Meta.GenerationMode = canonical.Meta.GenerationMode
	if !reflect.DeepEqual(normalized, canonical) {
		return ErrConflict
	}
	return nil
}

func validateReview(review ReviewResult, pages int) error {
	if review.Approved {
		if len(review.Issues) != 0 {
			return ErrInvalidInput
		}
		return nil
	}
	if len(review.Issues) < 1 || len(review.Issues) > 8 {
		return ErrInvalidInput
	}
	for _, issue := range review.Issues {
		if issue.Page < 1 || issue.Page > pages || !validIdentifier(issue.Code, 40) || !boundedAgentText(issue.Message, 2, 180) {
			return ErrInvalidInput
		}
	}
	return nil
}

func safeAgentNarrative(value string) bool {
	if !boundedAgentText(value, 2, 220) || strings.Contains(value, "://") {
		return false
	}
	for _, char := range value {
		if unicode.IsNumber(char) {
			return false
		}
	}
	for _, claim := range []string{"保证", "一定", "确定可建", "符合审批", "准确造价", "施工图已完成", "正式报价"} {
		if strings.Contains(value, claim) {
			return false
		}
	}
	return true
}

func boundedAgentText(value string, minimum, maximum int) bool {
	if strings.TrimSpace(value) != value || strings.ContainsAny(value, "\r\n") {
		return false
	}
	count := utf8.RuneCountInString(value)
	if count < minimum || count > maximum {
		return false
	}
	for _, char := range value {
		if unicode.IsControl(char) {
			return false
		}
	}
	return true
}

func sameStringSet(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	a := append([]string(nil), left...)
	b := append([]string(nil), right...)
	sort.Strings(a)
	sort.Strings(b)
	return reflect.DeepEqual(a, b)
}

func planEvidence(plan PlanResult, page int) map[string]bool {
	allowed := make(map[string]bool)
	for _, section := range plan.Sections {
		if section.Page == page {
			for _, ref := range section.EvidenceRefs {
				allowed[ref] = true
			}
		}
	}
	return allowed
}

func sanitizeSnapshotForAgent(snapshot Snapshot) Snapshot {
	data, _ := json.Marshal(snapshot)
	var sanitized Snapshot
	_ = json.Unmarshal(data, &sanitized)
	for id := range sanitized.Assets {
		sanitized.Assets[id] = "approved"
	}
	return sanitized
}

func sortedAssetIDs(assets map[string]string) []string {
	ids := make([]string, 0, len(assets))
	for id := range assets {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

func strictDecode(data []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return ErrInvalidInput
		}
		return err
	}
	return nil
}

const plannerPrompt = `你是建房规划报告的规划Agent。只能在输入的固定四页预览或十四页完整报告合同内规划，不得增删页，不得创造事实、数字或专业结论。逐页返回objective和evidence_refs；证据只能引用facts、preview，完整报告还可引用pack、assets、budget。只返回严格JSON。`

const visualPrompt = `你是视觉素材Agent。你不生成CAD、施工图或任意网址，只能从approved_asset_ids和approved_schemes中选择并核对成套素材ID。预览模式返回空selections；完整模式必须为第六至十一页选择各自方案的全部平面素材及成套立面和体量素材。只返回严格JSON。`

const writerPrompt = `你是报告内容Agent。按照plan和visual写个性化解释，只能返回允许正文位置的patches，每个patch必须引用该页plan已有的evidence_refs。不得修改事实、数字、预算、素材、方案、标题、表格或免责声明，不得写数字、保证性结论、审批结论、施工图结论或正式报价。完整报告前四页已经冻结，不得修改。只返回严格JSON。`

const writerRepairPrompt = `你是报告内容Agent，负责一次且仅一次的定向修订。根据issues重写允许位置的正文，不得修改事实、数字、预算、素材、方案、标题、表格、免责声明或完整报告前四页。不得写数字、保证性结论、审批结论、施工图结论或正式报价。只返回严格JSON。`

const reviewerPrompt = `你是独立报告审核Agent。检查文案是否忠于事实与证据、图文是否使用审核素材、是否越过概念规划边界、是否有保证性承诺、是否前后矛盾。通过时approved为true且issues为空；不通过时approved为false并给出明确页码、代码和问题。不得代写内容。只返回严格JSON。`
