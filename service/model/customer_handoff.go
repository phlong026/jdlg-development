package model

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

// MyCustomerHandoff exposes only this designer's recorded handoff. It never
// returns management lead/customer IDs, phone numbers or another channel's case.
type MyCustomerHandoff struct {
	Converted      bool       `json:"converted"`
	Status         string     `json:"status,omitempty"`
	Assigned       bool       `json:"assigned"`
	NextFollowupAt *time.Time `json:"next_followup_at,omitempty"`
	ContactReady   bool       `json:"contact_ready"`
}

func (m *CustomerServiceModel) MyCustomerHandoff(ctx context.Context, actor, customer int64) (MyCustomerHandoff, error) {
	all, err := m.MyCustomerHandoffs(ctx, actor, []int64{customer})
	if err != nil {
		return MyCustomerHandoff{}, err
	}
	state, ok := all[customer]
	if !ok {
		return state, sql.ErrNoRows
	}
	return state, nil
}

func (m *CustomerServiceModel) MyCustomerHandoffs(ctx context.Context, actor int64, customers []int64) (map[int64]MyCustomerHandoff, error) {
	out := map[int64]MyCustomerHandoff{}
	if m == nil || m.DB == nil || actor <= 0 || len(customers) > 100 {
		return out, errors.New("customer handoff store unavailable")
	}
	if len(customers) == 0 {
		return out, nil
	}
	args := []any{actor}
	slots := []string{}
	seen := map[int64]bool{}
	for _, customer := range customers {
		if customer <= 0 || seen[customer] {
			continue
		}
		seen[customer] = true
		slots = append(slots, "?")
		args = append(args, customer)
	}
	if len(slots) == 0 {
		return out, nil
	}
	rows, err := m.DB.QueryContext(ctx, `SELECT u.id,COALESCE(b.accepted,0),COALESCE(b.revision,0),COALESCE(b.purpose,''),COALESCE(b.consent_version,''),
 EXISTS(SELECT 1 FROM user_identities p WHERE p.user_id=u.id AND p.identity_type='phone' AND TRIM(p.identity_key)<>''),
 COALESCE(l.status,''),COALESCE(l.assigned_to,''),l.next_followup_at
 FROM users u LEFT JOIN business_contact_consents b ON b.user_id=u.id
 LEFT JOIN customer_leads l ON l.id=(SELECT e.lead_id FROM customer_service_events e JOIN customer_leads c ON c.id=e.lead_id AND c.user_id=e.user_id
 WHERE e.user_id=u.id AND e.event_type='share_intent_convert' AND e.source='share_tracking_my'
 AND JSON_UNQUOTE(JSON_EXTRACT(e.payload,'$.converted_by'))=? ORDER BY e.id DESC LIMIT 1)
 WHERE u.id IN (`+strings.Join(slots, ",")+`) AND u.user_type='miniprogram' AND COALESCE(u.status,'active')='active'`, args...)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var customer int64
		var state businessContactConsentState
		var phoneExists bool
		var assigned string
		var next sql.NullTime
		var value MyCustomerHandoff
		if err = rows.Scan(&customer, &state.Accepted, &state.Revision, &state.Purpose, &state.Version, &phoneExists, &value.Status, &assigned, &next); err != nil {
			return nil, err
		}
		value.ContactReady = validBusinessContactConsent(state) && phoneExists
		value.Converted = value.Status != ""
		value.Assigned = value.Converted && assigned != ""
		if value.Converted && next.Valid {
			value.NextFollowupAt = &next.Time
		}
		out[customer] = value
	}
	return out, rows.Err()
}

// Called under the existing per-customer consent row lock: a different
// designer's first touch is retained while retries by the same actor dedupe.
func (m *CustomerServiceModel) appendMyTrackingTouchOn(tx *sql.Tx, event *CustomerServiceEventCreateInput) error {
	actor, ok := event.Payload["converted_by"].(int64)
	if !ok || actor <= 0 || event.Source != "share_tracking_my" || event.EventType != "share_intent_convert" {
		return errors.New("invalid tracking handoff provenance")
	}
	var exists bool
	if err := tx.QueryRow(`SELECT EXISTS(SELECT 1 FROM customer_service_events WHERE user_id=? AND lead_id=? AND event_type=? AND source=? AND JSON_UNQUOTE(JSON_EXTRACT(payload,'$.converted_by'))=?)`, event.UserID, event.LeadID, event.EventType, event.Source, actor).Scan(&exists); err != nil {
		return err
	}
	if exists {
		return nil
	}
	_, err := m.createEventOn(tx, event)
	return err
}
