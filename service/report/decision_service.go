package report

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
)

func (s *Service) CreateDecisionPreview(ctx context.Context, userID int64, input DecisionInput) (SafeView, error) {
	if input.House != nil {
		return SafeView{}, ErrInvalidInput
	}
	return s.createDecisionPreview(ctx, userID, input, ProtocolDecisionV5)
}

func (s *Service) CreateHousePreview(ctx context.Context, userID int64, input DecisionInput) (SafeView, error) {
	if !s.config.HouseEnabled {
		return SafeView{}, ErrDisabled
	}
	if input.House == nil {
		return SafeView{}, ErrInvalidInput
	}
	return s.createDecisionPreview(ctx, userID, input, ProtocolHouseV1)
}

func (s *Service) createDecisionPreview(ctx context.Context, userID int64, input DecisionInput, protocol string) (SafeView, error) {
	if !s.config.Enabled || !s.config.DecisionEnabled {
		return SafeView{}, ErrDisabled
	}
	if userID <= 0 {
		return SafeView{}, ErrInvalidInput
	}
	if _, ok := s.store.(DecisionContextStore); !ok {
		return SafeView{}, ErrInvalidInput
	}
	normalizedInput, err := NormalizeDecisionInput(input)
	if err != nil {
		return SafeView{}, err
	}
	if err := ValidateDecisionBrief(normalizedInput, PlanDecision(normalizedInput)); err != nil {
		return SafeView{}, err
	}
	inputJSON, inputHash, err := MarshalDecisionInput(normalizedInput)
	if err != nil {
		return SafeView{}, err
	}
	now := time.Now()
	record := &Report{
		ID: uuid.NewString(), UserID: userID, InputHash: inputHash,
		ProtocolVersion: protocol, InputJSON: inputJSON,
		Status: StatusPreviewQueued, GenerationMode: GenerationRulesOnly,
		CreatedAt: now, UpdatedAt: now,
	}
	stored, _, err := s.store.CreateOrGet(ctx, record)
	if err != nil {
		return SafeView{}, err
	}
	stored, err = s.hydrateDecisionContext(ctx, stored)
	if err != nil {
		return SafeView{}, err
	}
	return s.safeView(stored), nil
}

func (s *Service) ConfirmDecision(ctx context.Context, userID int64, reportID string, confirmation DecisionConfirmation) (SafeView, error) {
	record, err := s.get(ctx, userID, reportID)
	if err != nil {
		return SafeView{}, err
	}
	if !IsDecisionProtocol(record.ProtocolVersion) || record.Status != StatusPreviewReady || len(record.PreviewSnapshot) == 0 || record.Paid || record.PendingOrderNo != "" {
		return SafeView{}, ErrConflict
	}
	var input DecisionInput
	if err := json.Unmarshal(record.InputJSON, &input); err != nil {
		return SafeView{}, ErrInvalidInput
	}
	canonicalInput, inputHash, err := MarshalDecisionInput(input)
	if err != nil || inputHash != record.InputHash || !jsonEqual(canonicalInput, record.InputJSON) {
		return SafeView{}, ErrInvalidInput
	}
	var snapshot DecisionSnapshot
	if err := json.Unmarshal(record.PreviewSnapshot, &snapshot); err != nil || ValidateDecisionSnapshot(snapshot, ModePreview) != nil ||
		snapshot.Meta.ReportID != record.ID || snapshot.Meta.InputHash != record.InputHash || snapshot.Meta.ProtocolVersion != record.ProtocolVersion {
		return SafeView{}, ErrInvalidInput
	}
	if confirmation.DiagnosisID != snapshot.Diagnosis.ID || confirmation.InputHash != record.InputHash {
		return SafeView{}, ErrInvalidInput
	}
	normalized, err := NormalizeDecisionConfirmation(input, confirmation)
	if err != nil {
		return SafeView{}, err
	}
	raw, hash, err := marshalDecisionConfirmation(normalized)
	if err != nil {
		return SafeView{}, err
	}
	decisionStore, ok := s.store.(DecisionContextStore)
	if !ok {
		return SafeView{}, ErrInvalidInput
	}
	decisionContext, err := decisionStore.SaveDecisionConfirmation(ctx, userID, reportID, raw, hash)
	if err != nil {
		return SafeView{}, err
	}
	if !validDecisionContext(decisionContext) || decisionContext.ConfirmationHash != hash {
		return SafeView{}, ErrConflict
	}
	applyDecisionContext(record, decisionContext)
	return s.safeView(record), nil
}

func (s *Service) hydrateDecisionContext(ctx context.Context, record *Report) (*Report, error) {
	if record == nil || !IsDecisionProtocol(record.ProtocolVersion) {
		return record, nil
	}
	decisionStore, ok := s.store.(DecisionContextStore)
	if !ok {
		return nil, ErrInvalidInput
	}
	decisionContext, err := decisionStore.GetDecisionContext(ctx, record.UserID, record.ID)
	if errors.Is(err, ErrNotFound) {
		record.CurrentStage = DecisionStagePreviewPending
		return record, nil
	}
	if err != nil {
		return nil, err
	}
	if !validDecisionContext(decisionContext) {
		return nil, ErrConflict
	}
	applyDecisionContext(record, decisionContext)
	return record, nil
}

func applyDecisionContext(record *Report, decisionContext DecisionContext) {
	record.ConfirmationJSON = append(json.RawMessage(nil), decisionContext.ConfirmationJSON...)
	record.ConfirmationHash = decisionContext.ConfirmationHash
	record.CurrentStage = decisionContext.Stage
}

func marshalDecisionConfirmation(confirmation DecisionConfirmation) (json.RawMessage, string, error) {
	raw, err := json.Marshal(confirmation)
	if err != nil {
		return nil, "", ErrInvalidInput
	}
	digest := sha256.Sum256(raw)
	return raw, hex.EncodeToString(digest[:]), nil
}

func validateDecisionPayment(record *Report) (DecisionSnapshot, string) {
	if len(record.ConfirmationJSON) == 0 || record.ConfirmationHash == "" {
		return DecisionSnapshot{}, "confirmation_required"
	}
	var input DecisionInput
	if json.Unmarshal(record.InputJSON, &input) != nil {
		return DecisionSnapshot{}, "invalid_frozen_input"
	}
	canonicalInput, inputHash, err := MarshalDecisionInput(input)
	if err != nil || inputHash != record.InputHash || !jsonEqual(canonicalInput, record.InputJSON) {
		return DecisionSnapshot{}, "invalid_frozen_input"
	}
	var snapshot DecisionSnapshot
	if json.Unmarshal(record.PreviewSnapshot, &snapshot) != nil || ValidateDecisionSnapshot(snapshot, ModePreview) != nil || snapshot.Meta.ReportID != record.ID || snapshot.Meta.InputHash != record.InputHash || snapshot.Meta.ProtocolVersion != record.ProtocolVersion {
		return DecisionSnapshot{}, "invalid_preview"
	}
	var confirmation DecisionConfirmation
	if json.Unmarshal(record.ConfirmationJSON, &confirmation) != nil {
		return DecisionSnapshot{}, "invalid_confirmation"
	}
	normalized, err := NormalizeDecisionConfirmation(input, confirmation)
	if err != nil || normalized.DiagnosisID != snapshot.Diagnosis.ID {
		return DecisionSnapshot{}, "invalid_confirmation"
	}
	raw, hash, err := marshalDecisionConfirmation(normalized)
	if err != nil || hash != record.ConfirmationHash || !jsonEqual(raw, record.ConfirmationJSON) {
		return DecisionSnapshot{}, "invalid_confirmation"
	}
	packID := "decision-v5-" + snapshot.Recipe
	if !validIdentifier(packID, 64) {
		return DecisionSnapshot{}, "invalid_preview"
	}
	return snapshot, ""
}

func jsonEqual(first, second json.RawMessage) bool {
	var left any
	var right any
	return json.Unmarshal(first, &left) == nil && json.Unmarshal(second, &right) == nil && deepJSONEqual(left, right)
}

func deepJSONEqual(left, right any) bool {
	leftJSON, leftErr := json.Marshal(left)
	rightJSON, rightErr := json.Marshal(right)
	return leftErr == nil && rightErr == nil && string(leftJSON) == string(rightJSON)
}
