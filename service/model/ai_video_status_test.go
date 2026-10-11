package model

import (
	"reflect"
	"testing"
)

func TestNormalizeAIVideoStatus(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		want   string
	}{
		{name: "queued", input: "queued", want: AIVideoStatusQueued},
		{name: "pending", input: "pending", want: AIVideoStatusQueued},
		{name: "running", input: "running", want: AIVideoStatusProcessing},
		{name: "in progress", input: "in_progress", want: AIVideoStatusProcessing},
		{name: "success", input: "success", want: AIVideoStatusCompleted},
		{name: "succeeded", input: "succeeded", want: AIVideoStatusCompleted},
		{name: "cancelled", input: "cancelled", want: AIVideoStatusFailed},
		{name: "unknown", input: "hold", want: "hold"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := NormalizeAIVideoStatus(tt.input); got != tt.want {
				t.Fatalf("NormalizeAIVideoStatus(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestAIVideoStatusDisplayMappings(t *testing.T) {
	userTests := []struct {
		input string
		want  string
	}{
		{input: AIVideoStatusQueued, want: AIVideoUserStatusPending},
		{input: "in_progress", want: AIVideoUserStatusProcessing},
		{input: AIVideoStatusCompleted, want: AIVideoUserStatusSuccess},
		{input: AIVideoStatusFailed, want: AIVideoStatusFailed},
	}
	for _, tt := range userTests {
		if got := AIVideoStatusForUser(tt.input); got != tt.want {
			t.Fatalf("AIVideoStatusForUser(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}

	managementTests := []struct {
		input string
		want  string
	}{
		{input: AIVideoStatusQueued, want: AIVideoUserStatusPending},
		{input: "in_progress", want: AIVideoManagementStatusRunning},
		{input: AIVideoStatusCompleted, want: AIVideoUserStatusSuccess},
		{input: AIVideoStatusFailed, want: AIVideoStatusFailed},
	}
	for _, tt := range managementTests {
		if got := AIVideoStatusForManagement(tt.input); got != tt.want {
			t.Fatalf("AIVideoStatusForManagement(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestEffectiveAIVideoStatusWithoutResult(t *testing.T) {
	if got := EffectiveAIVideoStatus(AIVideoStatusCompleted, false); got != AIVideoStatusProcessing {
		t.Fatalf("EffectiveAIVideoStatus(completed, false) = %q, want %q", got, AIVideoStatusProcessing)
	}
	if got := AIVideoStatusForUserWithResult(AIVideoStatusCompleted, false); got != AIVideoUserStatusProcessing {
		t.Fatalf("AIVideoStatusForUserWithResult(completed, false) = %q, want %q", got, AIVideoUserStatusProcessing)
	}
	if got := AIVideoStatusForManagementWithResult(AIVideoStatusCompleted, false); got != AIVideoManagementStatusRunning {
		t.Fatalf("AIVideoStatusForManagementWithResult(completed, false) = %q, want %q", got, AIVideoManagementStatusRunning)
	}
	if got := EffectiveAIVideoStatus(AIVideoStatusCompleted, true); got != AIVideoStatusCompleted {
		t.Fatalf("EffectiveAIVideoStatus(completed, true) = %q, want %q", got, AIVideoStatusCompleted)
	}
}

func TestExpandAIVideoStatusFilter(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []string
	}{
		{name: "pending", input: AIVideoUserStatusPending, want: []string{AIVideoStatusQueued}},
		{name: "running", input: AIVideoManagementStatusRunning, want: []string{AIVideoStatusProcessing, "in_progress"}},
		{name: "processing", input: AIVideoStatusProcessing, want: []string{AIVideoStatusProcessing, "in_progress"}},
		{name: "success", input: AIVideoUserStatusSuccess, want: []string{AIVideoStatusCompleted}},
		{name: "failed", input: AIVideoStatusFailed, want: []string{AIVideoStatusFailed}},
		{name: "all", input: "all", want: nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExpandAIVideoStatusFilter(tt.input)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("ExpandAIVideoStatusFilter(%q) = %#v, want %#v", tt.input, got, tt.want)
			}
		})
	}
}
