package model

import (
	"database/sql"
	"errors"
	"strings"
)

const (
	BusinessContactConsentGranted   = "business_contact_consent_granted"
	BusinessContactConsentWithdrawn = "business_contact_consent_withdrawn"
	BusinessContactConsentVersion   = "phone_contact_v1"
	BusinessContactConsentPurpose   = "use_bound_phone_for_business_contact"
)

var (
	ErrBusinessContactConsentRequired  = errors.New("business contact consent required")
	ErrBusinessContactPhoneUnavailable = errors.New("business contact phone unavailable")
)

type TrackingLeadConversionResult struct {
	LeadID   int64
	Existed  bool
	LeadType string
}

type businessContactConsentState struct {
	Accepted bool
	Revision int64
	Purpose  string
	Version  string
}

func validBusinessContactConsent(state businessContactConsentState) bool {
	return state.Accepted &&
		strings.TrimSpace(state.Purpose) == BusinessContactConsentPurpose &&
		strings.TrimSpace(state.Version) == BusinessContactConsentVersion &&
		state.Revision > 0
}

func sameBusinessContactConsentChoice(state businessContactConsentState, accepted bool) bool {
	currentPurpose := strings.TrimSpace(state.Purpose) == BusinessContactConsentPurpose
	currentVersion := strings.TrimSpace(state.Version) == BusinessContactConsentVersion
	validRepeatedGrant := !accepted || state.Revision > 0
	return state.Accepted == accepted && currentPurpose && currentVersion && validRepeatedGrant
}

func ensureBusinessContactConsentRow(tx *sql.Tx, userID int64) error {
	_, err := tx.Exec(`
		INSERT IGNORE INTO business_contact_consents
			(user_id, accepted, revision, purpose, consent_version, updated_at)
		VALUES (?, 0, 0, ?, ?, NOW())
	`, userID, BusinessContactConsentPurpose, BusinessContactConsentVersion)
	return err
}

func lockBusinessContactConsent(tx *sql.Tx, userID int64) (businessContactConsentState, error) {
	if err := ensureBusinessContactConsentRow(tx, userID); err != nil {
		return businessContactConsentState{}, err
	}
	var state businessContactConsentState
	err := tx.QueryRow(`
		SELECT accepted, revision, purpose, consent_version
		FROM business_contact_consents
		WHERE user_id = ?
		FOR UPDATE
	`, userID).Scan(&state.Accepted, &state.Revision, &state.Purpose, &state.Version)
	return state, err
}

// BusinessContactConsent reads only the dedicated purpose/version record.
// Legacy analytics consent, phone verification and malformed rows fail closed.
func (m *CustomerServiceModel) BusinessContactConsent(userID int64) (bool, error) {
	if m == nil || m.DB == nil || userID <= 0 {
		return false, nil
	}
	var state businessContactConsentState
	err := m.DB.QueryRow(`
		SELECT accepted, revision, purpose, consent_version
		FROM business_contact_consents
		WHERE user_id = ?
	`, userID).Scan(&state.Accepted, &state.Revision, &state.Purpose, &state.Version)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return validBusinessContactConsent(state), nil
}

// SetBusinessContactConsent serializes with every account-phone use by locking
// the same per-user row. Whichever transaction gets the lock first defines the
// visible order.
func (m *CustomerServiceModel) SetBusinessContactConsent(userID int64, accepted bool, source string) error {
	if m == nil || m.DB == nil || userID <= 0 {
		return errors.New("business contact consent store unavailable")
	}
	tx, err := m.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	state, err := lockBusinessContactConsent(tx, userID)
	if err != nil {
		return err
	}
	if sameBusinessContactConsentChoice(state, accepted) {
		return tx.Commit()
	}
	revision := state.Revision + 1
	if _, err := tx.Exec(`
		UPDATE business_contact_consents
		SET accepted = ?, revision = ?, purpose = ?, consent_version = ?, updated_at = NOW()
		WHERE user_id = ?
	`, accepted, revision, BusinessContactConsentPurpose, BusinessContactConsentVersion, userID); err != nil {
		return err
	}
	eventType := BusinessContactConsentWithdrawn
	if accepted {
		eventType = BusinessContactConsentGranted
	}
	_, err = m.createEventOn(tx, &CustomerServiceEventCreateInput{
		UserID:    userID,
		EventType: eventType,
		Source:    strings.TrimSpace(source),
		Payload: map[string]interface{}{
			"purpose":  BusinessContactConsentPurpose,
			"version":  BusinessContactConsentVersion,
			"revision": revision,
			"accepted": accepted,
		},
	})
	if err != nil {
		return err
	}
	return tx.Commit()
}

func accountPhoneOn(tx *sql.Tx, userID int64) (string, error) {
	var phone string
	err := tx.QueryRow(`
		SELECT identity_key
		FROM user_identities
		WHERE identity_type = 'phone' AND user_id = ?
		ORDER BY id DESC
		LIMIT 1
	`, userID).Scan(&phone)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return strings.TrimSpace(phone), err
}

// refreshExistingLeadAccountPhoneOn is intentionally private to the
// consent-row-locked transactions in this file. It can upgrade legacy rows or
// refresh a previously authorized revision, but it never replaces a phone the
// user typed into the lead itself.
func refreshExistingLeadAccountPhoneOn(tx *sql.Tx, leadID int64, phone string, revision int64) error {
	phone = strings.TrimSpace(phone)
	if leadID <= 0 || phone == "" || revision <= 0 {
		return nil
	}
	_, err := tx.Exec(`
		UPDATE customer_leads
		SET phone = ?, phone_source = ?, phone_consent_revision = ?, updated_at = NOW()
		WHERE id = ?
		  AND (phone = '' OR phone_source IN ('none', 'account_bound', 'legacy_unknown'))
	`, phone, CustomerLeadPhoneSourceAccountBound, revision, leadID)
	return err
}

// CreateLeadWithBusinessContact keeps the authorization decision, account
// phone read and lead write in one transaction under the consent row lock.
func (m *CustomerServiceModel) CreateLeadWithBusinessContact(input *CustomerLeadCreateInput) (int64, bool, error) {
	if m == nil || m.DB == nil || input == nil || input.UserID <= 0 {
		return 0, false, errors.New("business contact lead store unavailable")
	}
	tx, err := m.DB.Begin()
	if err != nil {
		return 0, false, err
	}
	defer tx.Rollback()
	// This operation owns only account-bound phone reuse. User-provided phone
	// values must go through ordinary CreateLead with explicit provenance.
	input.Phone = ""
	input.PhoneSource = CustomerLeadPhoneSourceNone
	input.PhoneConsentRevision = 0
	state, err := lockBusinessContactConsent(tx, input.UserID)
	if err != nil {
		return 0, false, err
	}
	usedBoundPhone := false
	if validBusinessContactConsent(state) {
		phone, phoneErr := accountPhoneOn(tx, input.UserID)
		if phoneErr != nil {
			return 0, false, phoneErr
		}
		if phone != "" {
			input.Phone = phone
			input.PhoneSource = CustomerLeadPhoneSourceAccountBound
			input.PhoneConsentRevision = state.Revision
			usedBoundPhone = true
		}
	}
	leadID, err := m.createLeadOn(tx, input, true)
	if err != nil {
		return 0, false, err
	}
	if err := tx.Commit(); err != nil {
		return 0, false, err
	}
	return leadID, usedBoundPhone, nil
}

// ConvertTrackingLead performs consent, phone read, dedupe, lead write and
// evidence append atomically under the same authorization row lock.
func (m *CustomerServiceModel) ConvertTrackingLead(input *CustomerLeadCreateInput, event *CustomerServiceEventCreateInput, idempotent bool) (TrackingLeadConversionResult, error) {
	if m == nil || m.DB == nil || input == nil || event == nil || input.UserID <= 0 {
		return TrackingLeadConversionResult{}, errors.New("tracking lead store unavailable")
	}
	tx, err := m.DB.Begin()
	if err != nil {
		return TrackingLeadConversionResult{}, err
	}
	defer tx.Rollback()
	state, err := lockBusinessContactConsent(tx, input.UserID)
	if err != nil {
		return TrackingLeadConversionResult{}, err
	}
	if !validBusinessContactConsent(state) {
		return TrackingLeadConversionResult{}, ErrBusinessContactConsentRequired
	}
	phone, err := accountPhoneOn(tx, input.UserID)
	if err != nil {
		return TrackingLeadConversionResult{}, err
	}
	if phone == "" {
		return TrackingLeadConversionResult{}, ErrBusinessContactPhoneUnavailable
	}
	existingID, err := m.findLatestOpenLeadIDOn(tx, input.UserID)
	if err != nil {
		return TrackingLeadConversionResult{}, err
	}
	if existingID > 0 {
		if err := refreshExistingLeadAccountPhoneOn(tx, existingID, phone, state.Revision); err != nil {
			return TrackingLeadConversionResult{}, err
		}
		if !idempotent {
			event.LeadID = existingID
			if _, err := m.createEventOn(tx, event); err != nil {
				return TrackingLeadConversionResult{}, err
			}
		}
		leadType, err := persistedLeadTypeOn(tx, existingID)
		if err != nil {
			return TrackingLeadConversionResult{}, err
		}
		if err := tx.Commit(); err != nil {
			return TrackingLeadConversionResult{}, err
		}
		return TrackingLeadConversionResult{LeadID: existingID, Existed: true, LeadType: leadType}, nil
	}
	input.Phone = phone
	input.PhoneSource = CustomerLeadPhoneSourceAccountBound
	input.PhoneConsentRevision = state.Revision
	leadID, err := m.createLeadOn(tx, input, true)
	if err != nil {
		return TrackingLeadConversionResult{}, err
	}
	event.LeadID = leadID
	if _, err := m.createEventOn(tx, event); err != nil {
		return TrackingLeadConversionResult{}, err
	}
	leadType, err := persistedLeadTypeOn(tx, leadID)
	if err != nil {
		return TrackingLeadConversionResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return TrackingLeadConversionResult{}, err
	}
	return TrackingLeadConversionResult{LeadID: leadID, LeadType: leadType}, nil
}

func persistedLeadTypeOn(tx *sql.Tx, leadID int64) (string, error) {
	var identityType string
	if err := tx.QueryRow(`SELECT COALESCE(identity_type, '') FROM customer_leads WHERE id = ?`, leadID).Scan(&identityType); err != nil {
		return "", err
	}
	identityType = strings.TrimSpace(identityType)
	if identityType == "" {
		return CustomerLeadTypeIdentityPending, nil
	}
	if identityType == "乡墅业主" {
		return CustomerLeadTypeSales, nil
	}
	if IsPartnerIdentity(identityType) {
		return CustomerLeadTypePartner, nil
	}
	return "", errors.New("persisted customer lead identity is invalid")
}
