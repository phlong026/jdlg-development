package report

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/google/uuid"
)

type Service struct {
	config  Config
	store   Store
	catalog PackCatalog
	storage PrivateStorage
}

func NewService(config Config, store Store, catalog PackCatalog, storage PrivateStorage) (*Service, error) {
	if store == nil {
		return nil, ErrInvalidInput
	}
	return &Service{config: config, store: store, catalog: catalog, storage: storage}, nil
}

func (s *Service) CreatePreview(ctx context.Context, userID int64, input Input) (SafeView, error) {
	if !s.config.Enabled {
		return SafeView{}, ErrDisabled
	}
	if userID <= 0 {
		return SafeView{}, ErrInvalidInput
	}
	inputJSON, inputHash, err := MarshalInput(input)
	if err != nil {
		return SafeView{}, err
	}
	now := time.Now()
	record := &Report{
		ID: uuid.NewString(), UserID: userID, InputHash: inputHash,
		ProtocolVersion: s.config.ProtocolVersion, InputJSON: inputJSON,
		Status: StatusPreviewQueued, GenerationMode: GenerationRulesOnly,
		CreatedAt: now, UpdatedAt: now,
	}
	stored, _, err := s.store.CreateOrGet(ctx, record)
	if err != nil {
		return SafeView{}, err
	}
	return s.safeView(stored), nil
}

func (s *Service) Get(ctx context.Context, userID int64, reportID string) (SafeView, error) {
	record, err := s.get(ctx, userID, reportID)
	if err != nil {
		return SafeView{}, err
	}
	return s.safeView(record), nil
}

func (s *Service) Latest(ctx context.Context, userID int64) (SafeView, error) {
	if !s.config.Enabled {
		return SafeView{}, ErrDisabled
	}
	if userID <= 0 {
		return SafeView{}, ErrInvalidInput
	}
	record, err := s.store.GetLatestForUser(ctx, userID)
	if err != nil {
		return SafeView{}, err
	}
	record, err = s.hydrateDecisionContext(ctx, record)
	if err != nil {
		return SafeView{}, err
	}
	return s.safeView(record), nil
}

func (s *Service) GetPaymentQuote(ctx context.Context, userID int64, reportID string) (PaymentQuote, error) {
	record, err := s.get(ctx, userID, reportID)
	if err != nil {
		return PaymentQuote{}, err
	}
	return s.paymentQuote(record), nil
}

// GetPaymentReservation is server-only metadata for payment callback
// verification. It is intentionally absent from SafeView.
func (s *Service) GetPaymentReservation(ctx context.Context, userID int64, reportID string) (PaymentReservation, error) {
	record, err := s.get(ctx, userID, reportID)
	if err != nil {
		return PaymentReservation{}, err
	}
	reservation := reservationFromRecord(record, record.PendingOrderNo)
	if !validReservation(reservation) {
		return PaymentReservation{}, ErrConflict
	}
	return reservation, nil
}

func (s *Service) ReservePaymentOrder(ctx context.Context, userID int64, reportID, proposedOrderNo string) (PaymentReservation, error) {
	if !validIdentifier(proposedOrderNo, 128) {
		return PaymentReservation{}, ErrInvalidInput
	}
	quote, err := s.GetPaymentQuote(ctx, userID, reportID)
	if err != nil {
		return PaymentReservation{}, err
	}
	if !quote.CanPay {
		return PaymentReservation{}, ErrPackUnavailable
	}
	return s.store.ReservePaymentOrder(ctx, userID, reportID, PaymentReservation{
		OrderNo: proposedOrderNo, ProductID: quote.ProductID, PriceFen: quote.PriceFen,
		PackID: quote.PackID, PackVersion: quote.PackVersion,
	})
}

// AuthorizeVerifiedPayment is only for a server-side payment callback after
// amount, product and order ownership have been verified by the route layer.
func (s *Service) AuthorizeVerifiedPayment(ctx context.Context, userID int64, reportID, orderNo string) (SafeView, error) {
	if !validIdentifier(orderNo, 128) {
		return SafeView{}, ErrInvalidInput
	}
	if !s.config.Enabled {
		return SafeView{}, ErrDisabled
	}
	if userID <= 0 || !validIdentifier(reportID, 64) {
		return SafeView{}, ErrInvalidInput
	}
	// Persist the verified entitlement before hydrating auxiliary response data.
	// A decision-context read failure must not lose an accepted provider payment.
	stored, err := s.store.AuthorizePaid(ctx, userID, reportID, orderNo)
	if err != nil {
		return SafeView{}, err
	}
	stored, err = s.hydrateDecisionContext(ctx, stored)
	if err != nil {
		return SafeView{}, err
	}
	return s.safeView(stored), nil
}

// BindVerifiedEnterpriseContact is only for a verified enterprise WeChat
// contact callback. Phone verification is not sufficient for this method.
func (s *Service) BindVerifiedEnterpriseContact(ctx context.Context, userID int64, reportID, externalUserID string) error {
	if !validIdentifier(externalUserID, 128) {
		return ErrInvalidInput
	}
	if _, err := s.get(ctx, userID, reportID); err != nil {
		return err
	}
	return s.store.BindEnterpriseContact(ctx, userID, reportID, externalUserID)
}

func (s *Service) Retry(ctx context.Context, userID int64, reportID string) (SafeView, error) {
	previous, err := s.get(ctx, userID, reportID)
	if err != nil {
		return SafeView{}, err
	}
	if previous.ErrorCode == "image_submission_uncertain" {
		return SafeView{}, ErrConflict
	}
	record, err := s.store.RetryFailed(ctx, userID, reportID)
	if err != nil {
		return SafeView{}, err
	}
	record, err = s.hydrateDecisionContext(ctx, record)
	if err != nil {
		return SafeView{}, err
	}
	return s.safeView(record), nil
}

func (s *Service) GetDownloadURL(ctx context.Context, userID int64, reportID, mode string) (string, error) {
	if s.storage == nil {
		return "", ErrNotReady
	}
	record, err := s.get(ctx, userID, reportID)
	if err != nil {
		return "", err
	}
	var key string
	switch mode {
	case ModePreview:
		key = record.PreviewObjectKey
	case ModeFull:
		if !record.Paid {
			return "", ErrPaymentRequired
		}
		if !IsDecisionProtocol(record.ProtocolVersion) && !record.Bound {
			return "", ErrBindingRequired
		}
		if record.Status != StatusReady {
			return "", ErrNotReady
		}
		key = record.FullObjectKey
	default:
		return "", ErrInvalidInput
	}
	if key == "" {
		return "", ErrNotReady
	}
	return s.storage.SignDownload(ctx, key, s.config.DownloadTTL)
}

// EnsurePreviewPDF adds a private PDF to an already frozen preview without
// changing its content or generation state.
func (s *Service) EnsurePreviewPDF(ctx context.Context, userID int64, reportID string, renderer Renderer) error {
	if renderer == nil || s.storage == nil {
		return ErrNotReady
	}
	record, err := s.get(ctx, userID, reportID)
	if err != nil {
		return err
	}
	if IsDecisionProtocol(record.ProtocolVersion) {
		return ErrNotReady
	}
	if record.PreviewObjectKey != "" {
		return nil
	}
	if len(record.PreviewSnapshot) == 0 {
		return ErrNotReady
	}
	pdf, err := renderer.Render(ctx, record.PreviewSnapshot, ModePreview)
	if err != nil {
		return ErrNotReady
	}
	key := reportObjectKey(record, ModePreview, "backfill-"+uuid.NewString())
	if err := s.storage.UploadPrivate(ctx, key, pdf); err != nil {
		return ErrNotReady
	}
	return s.store.SetPreviewObjectKeyIfEmpty(ctx, userID, reportID, key)
}

func (s *Service) get(ctx context.Context, userID int64, reportID string) (*Report, error) {
	if !s.config.Enabled {
		return nil, ErrDisabled
	}
	if userID <= 0 || !validIdentifier(reportID, 64) {
		return nil, ErrInvalidInput
	}
	record, err := s.store.GetForUser(ctx, userID, reportID)
	if err != nil {
		return nil, err
	}
	return s.hydrateDecisionContext(ctx, record)
}

func (s *Service) paymentQuote(record *Report) PaymentQuote {
	quote := PaymentQuote{ProductID: s.config.ProductID, PriceFen: s.config.PriceFen}
	if !s.config.PaymentEnabled {
		quote.BlockReason = "payment_disabled"
		return quote
	}
	if record == nil || record.Status != StatusPreviewReady || len(record.PreviewSnapshot) == 0 {
		quote.BlockReason = "preview_not_ready"
		return quote
	}
	if record.Paid {
		quote.BlockReason = "already_paid"
		return quote
	}
	if IsDecisionProtocol(record.ProtocolVersion) {
		if !s.config.DecisionEnabled || !validIdentifier(s.config.ProductID, 64) || s.config.PriceFen <= 0 {
			quote.BlockReason = "payment_disabled"
			return quote
		}
		snapshot, blockReason := validateDecisionPayment(record)
		if blockReason != "" {
			quote.BlockReason = blockReason
			return quote
		}
		quote.CanPay = true
		quote.PackID = "decision-v5-" + snapshot.Recipe
		quote.PackVersion = DecisionTemplateVersion
		if record.ProtocolVersion == ProtocolHouseV1 {
			if !s.config.HouseEnabled {
				quote.CanPay = false
				quote.BlockReason = "payment_disabled"
				return quote
			}
			quote.PackID = "whole-house-v1"
			quote.PackVersion = HouseTemplateVersion
		}
		return quote
	}
	if s.config.LegacyPaymentBlocked {
		quote.BlockReason = "delivery_unavailable"
		return quote
	}
	if s.catalog == nil {
		quote.BlockReason = "reviewed_pack_unavailable"
		return quote
	}
	var input Input
	if json.Unmarshal(record.InputJSON, &input) != nil {
		quote.BlockReason = "invalid_frozen_input"
		return quote
	}
	pack, err := s.catalog.Match(input)
	if err != nil {
		quote.BlockReason = "reviewed_pack_unavailable"
		return quote
	}
	quote.CanPay = true
	quote.PackID = pack.ID
	quote.PackVersion = pack.Version
	return quote
}

func (s *Service) safeView(record *Report) SafeView {
	quote := s.paymentQuote(record)
	confirmed := false
	if IsDecisionProtocol(record.ProtocolVersion) {
		_, blockReason := validateDecisionPayment(record)
		confirmed = blockReason == ""
	}
	return SafeView{
		ID: record.ID, ProtocolVersion: record.ProtocolVersion, InputHash: record.InputHash,
		Status: record.Status, Stage: reportStage(record, confirmed), Confirmed: confirmed, NextAction: nextReportAction(record, confirmed),
		PreviewSnapshot: publicPreview(record),
		GenerationMode:  record.GenerationMode, CanPay: quote.CanPay,
		PaymentBlockReason: quote.BlockReason, Paid: record.Paid, Bound: record.Bound,
		ErrorCode: record.ErrorCode, CreatedAt: record.CreatedAt, UpdatedAt: record.UpdatedAt,
	}
}

func reportStage(record *Report, confirmed bool) string {
	if !IsDecisionProtocol(record.ProtocolVersion) {
		return record.Status
	}
	switch record.Status {
	case StatusPreviewQueued:
		return DecisionStagePreviewPending
	case StatusFullQueued:
		return "full_pending"
	case StatusPreviewGenerating, StatusFullGenerating:
		if record.CurrentStage != "" && record.CurrentStage != DecisionStagePreviewPending && record.CurrentStage != DecisionStageConfirmed {
			return record.CurrentStage
		}
		return record.Status
	case StatusPreviewReady:
		if confirmed {
			return DecisionStageConfirmed
		}
		return "awaiting_confirmation"
	case StatusReady:
		return StatusReady
	case StatusFailed:
		return StatusFailed
	default:
		return record.Status
	}
}

func publicPreview(record *Report) json.RawMessage {
	if record == nil || len(record.PreviewSnapshot) == 0 {
		return nil
	}
	if IsDecisionProtocol(record.ProtocolVersion) {
		var snapshot DecisionSnapshot
		if json.Unmarshal(record.PreviewSnapshot, &snapshot) != nil || ValidateDecisionSnapshot(snapshot, ModePreview) != nil ||
			snapshot.Meta.ReportID != record.ID || snapshot.Meta.InputHash != record.InputHash || snapshot.Meta.ProtocolVersion != record.ProtocolVersion {
			return nil
		}
		snapshot.Confirmation = nil
		data, err := json.Marshal(snapshot)
		if err != nil {
			return nil
		}
		return data
	}
	var snapshot Snapshot
	if json.Unmarshal(record.PreviewSnapshot, &snapshot) != nil || len(snapshot.Pages) != 4 {
		return nil
	}
	snapshot.Assets = nil
	snapshot.Budget = nil
	snapshot.Schemes = nil
	data, err := json.Marshal(snapshot)
	if err != nil {
		return nil
	}
	return data
}

func nextReportAction(record *Report, confirmed bool) string {
	switch record.Status {
	case StatusPreviewQueued, StatusPreviewGenerating:
		return "wait_preview"
	case StatusPreviewReady:
		if IsDecisionProtocol(record.ProtocolVersion) && !confirmed {
			return "confirm_decision"
		}
		if record.Paid {
			return "wait_report"
		}
		return "pay"
	case StatusFullQueued, StatusFullGenerating:
		return "wait_report"
	case StatusReady:
		return "download"
	case StatusFailed:
		if record.ErrorCode == "image_submission_uncertain" {
			return "contact_support"
		}
		return "retry"
	default:
		return "wait"
	}
}

func validIdentifier(value string, max int) bool {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > max {
		return false
	}
	for _, char := range value {
		if !(char == '-' || char == '_' || char == ':' || char == '.' || char >= '0' && char <= '9' || char >= 'A' && char <= 'Z' || char >= 'a' && char <= 'z') {
			return false
		}
	}
	return true
}
