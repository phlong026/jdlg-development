package tracking

import (
	"context"
	"errors"
	"net/http"
	"testing"
)

func trackingErrorStatus(err error) int {
	var target *Error
	if errors.As(err, &target) {
		return target.Status
	}
	return 0
}

func TestAcceptForAccountRejectsForeignAndAnonymousEntryTokens(t *testing.T) {
	service, _ := fixture(t)
	tests := []struct {
		name         string
		entryAccount int64
		actor        int64
		wantStatus   int
	}{
		{name: "anonymous token", entryAccount: 0, actor: 42, wantStatus: http.StatusForbidden},
		{name: "another account token", entryAccount: 41, actor: 42, wantStatus: http.StatusForbidden},
		{name: "missing current account", entryAccount: 41, actor: 0, wantStatus: http.StatusUnauthorized},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			entry := enter(t, service, test.entryAccount)
			event := ev(service, entry, "account-bound-event-"+test.name, "page_view", service.Now())
			if _, err := service.AcceptForAccount(context.Background(), entry.Token, []Event{event}, test.actor); trackingErrorStatus(err) != test.wantStatus {
				t.Fatalf("status = %d err=%v", trackingErrorStatus(err), err)
			}
		})
	}
}

func TestAcceptForAccountAllowsTheEntryOwner(t *testing.T) {
	service, _ := fixture(t)
	entry := enter(t, service, 42)
	event := ev(service, entry, "account-bound-event-owner", "page_view", service.Now())
	results, err := service.AcceptForAccount(context.Background(), entry.Token, []Event{event}, 42)
	if err != nil || len(results) != 1 || results[0].Status != "accepted" {
		t.Fatalf("results=%+v err=%v", results, err)
	}
}
