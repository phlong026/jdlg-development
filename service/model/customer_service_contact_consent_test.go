package model

import (
	"strings"
	"testing"
)

func TestCustomerLeadPhoneVisibilityRequiresCurrentPurposeVersionAndRevision(t *testing.T) {
	valid := businessContactConsentState{
		Accepted: true,
		Revision: 7,
		Purpose:  BusinessContactConsentPurpose,
		Version:  BusinessContactConsentVersion,
	}
	for _, tc := range []struct {
		name  string
		lead  CustomerLead
		state businessContactConsentState
		want  bool
	}{
		{name: "user provided survives contact withdrawal", lead: CustomerLead{Phone: "13900000000", PhoneSource: CustomerLeadPhoneSourceUserProvided}, want: true},
		{name: "legacy unknown is preserved but hidden", lead: CustomerLead{Phone: "13800000000", PhoneSource: CustomerLeadPhoneSourceLegacyUnknown}},
		{name: "current account phone is visible", lead: CustomerLead{Phone: "13800000000", PhoneSource: CustomerLeadPhoneSourceAccountBound, PhoneConsentRevision: 7}, state: valid, want: true},
		{name: "withdrawn account phone is hidden", lead: CustomerLead{Phone: "13800000000", PhoneSource: CustomerLeadPhoneSourceAccountBound, PhoneConsentRevision: 7}, state: businessContactConsentState{Revision: 8, Purpose: BusinessContactConsentPurpose, Version: BusinessContactConsentVersion}},
		{name: "regrant does not revive old revision", lead: CustomerLead{Phone: "13800000000", PhoneSource: CustomerLeadPhoneSourceAccountBound, PhoneConsentRevision: 7}, state: businessContactConsentState{Accepted: true, Revision: 9, Purpose: BusinessContactConsentPurpose, Version: BusinessContactConsentVersion}},
		{name: "wrong purpose fails closed", lead: CustomerLead{Phone: "13800000000", PhoneSource: CustomerLeadPhoneSourceAccountBound, PhoneConsentRevision: 7}, state: businessContactConsentState{Accepted: true, Revision: 7, Purpose: "analytics", Version: BusinessContactConsentVersion}},
		{name: "old version fails closed", lead: CustomerLead{Phone: "13800000000", PhoneSource: CustomerLeadPhoneSourceAccountBound, PhoneConsentRevision: 7}, state: businessContactConsentState{Accepted: true, Revision: 7, Purpose: BusinessContactConsentPurpose, Version: "phone_contact_v0"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := customerLeadPhoneVisible(&tc.lead, tc.state); got != tc.want {
				t.Fatalf("visible=%v, want %v", got, tc.want)
			}
		})
	}
}

func TestSameBusinessContactConsentChoice(t *testing.T) {
	current := businessContactConsentState{Revision: 4, Purpose: BusinessContactConsentPurpose, Version: BusinessContactConsentVersion}
	for _, tc := range []struct {
		name     string
		state    businessContactConsentState
		accepted bool
		want     bool
	}{
		{name: "repeated withdrawal", state: current, want: true},
		{name: "repeated grant", state: businessContactConsentState{Accepted: true, Revision: 4, Purpose: BusinessContactConsentPurpose, Version: BusinessContactConsentVersion}, accepted: true, want: true},
		{name: "withdraw after grant", state: businessContactConsentState{Accepted: true, Revision: 4, Purpose: BusinessContactConsentPurpose, Version: BusinessContactConsentVersion}},
		{name: "grant after withdrawal", state: current, accepted: true},
		{name: "zero revision grant is repaired", state: businessContactConsentState{Accepted: true, Purpose: BusinessContactConsentPurpose, Version: BusinessContactConsentVersion}, accepted: true},
		{name: "old purpose is repaired", state: businessContactConsentState{Accepted: true, Revision: 4, Purpose: "analytics", Version: BusinessContactConsentVersion}, accepted: true},
		{name: "old version is repaired", state: businessContactConsentState{Accepted: true, Revision: 4, Purpose: BusinessContactConsentPurpose, Version: "phone_contact_v0"}, accepted: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := sameBusinessContactConsentChoice(tc.state, tc.accepted); got != tc.want {
				t.Fatalf("same choice=%v, want %v", got, tc.want)
			}
		})
	}
}

func TestCustomerLeadVisiblePhoneSQLMatchesFailClosedPolicy(t *testing.T) {
	sql := CustomerLeadVisiblePhoneSQL("l")
	for _, want := range []string{
		"phone_source = 'user_provided'",
		"phone_source = 'account_bound'",
		"accepted = 1",
		"purpose = 'use_bound_phone_for_business_contact'",
		"consent_version = 'phone_contact_v1'",
		"revision = l.phone_consent_revision",
		"ELSE ''",
	} {
		if !strings.Contains(sql, want) {
			t.Fatalf("visible phone SQL missing %q", want)
		}
	}
}

func TestCustomerLeadExportablePhoneSQLExcludesAccountBoundPhone(t *testing.T) {
	sql := CustomerLeadExportablePhoneSQL("l")
	if !strings.Contains(sql, "phone_source = 'user_provided'") || strings.Contains(sql, "account_bound") {
		t.Fatalf("unsafe CRM export expression: %s", sql)
	}
}

func TestOrdinaryLeadCannotClaimAccountBoundPhoneSource(t *testing.T) {
	input := &CustomerLeadCreateInput{Phone: "13800000000", PhoneSource: CustomerLeadPhoneSourceAccountBound, PhoneConsentRevision: 9}
	normalizeCustomerLeadPhoneMetadata(input, false)
	if input.PhoneSource != CustomerLeadPhoneSourceUserProvided || input.PhoneConsentRevision != 0 {
		t.Fatalf("ordinary phone metadata=%s revision=%d", input.PhoneSource, input.PhoneConsentRevision)
	}
	trusted := &CustomerLeadCreateInput{Phone: "13800000000", PhoneSource: CustomerLeadPhoneSourceAccountBound, PhoneConsentRevision: 9}
	normalizeCustomerLeadPhoneMetadata(trusted, true)
	if trusted.PhoneSource != CustomerLeadPhoneSourceAccountBound || trusted.PhoneConsentRevision != 9 {
		t.Fatalf("trusted phone metadata=%s revision=%d", trusted.PhoneSource, trusted.PhoneConsentRevision)
	}
}
