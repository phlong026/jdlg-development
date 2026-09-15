package model

import "strings"

const (
	AIVideoStatusQueued     = "queued"
	AIVideoStatusProcessing = "processing"
	AIVideoStatusCompleted  = "completed"
	AIVideoStatusFailed     = "failed"

	aiVideoLegacyStatusInProgress = "in_progress"

	AIVideoUserStatusPending    = "pending"
	AIVideoUserStatusProcessing = "processing"
	AIVideoUserStatusSuccess    = "success"

	AIVideoManagementStatusRunning = "running"
)

func NormalizeAIVideoStatus(status string) string {
	trimmed := strings.TrimSpace(status)
	switch strings.ToLower(trimmed) {
	case AIVideoStatusQueued, AIVideoUserStatusPending:
		return AIVideoStatusQueued
	case AIVideoManagementStatusRunning, AIVideoStatusProcessing, aiVideoLegacyStatusInProgress, "inprogress":
		return AIVideoStatusProcessing
	case AIVideoStatusCompleted, AIVideoUserStatusSuccess, "succeeded":
		return AIVideoStatusCompleted
	case AIVideoStatusFailed, "error", "expired", "cancelled", "canceled":
		return AIVideoStatusFailed
	default:
		return trimmed
	}
}

func EffectiveAIVideoStatus(status string, hasResult bool) string {
	normalized := NormalizeAIVideoStatus(status)
	if normalized == AIVideoStatusCompleted && !hasResult {
		return AIVideoStatusProcessing
	}
	return normalized
}

func AIVideoStatusForUser(status string) string {
	switch NormalizeAIVideoStatus(status) {
	case AIVideoStatusQueued:
		return AIVideoUserStatusPending
	case AIVideoStatusProcessing:
		return AIVideoUserStatusProcessing
	case AIVideoStatusCompleted:
		return AIVideoUserStatusSuccess
	case AIVideoStatusFailed:
		return AIVideoStatusFailed
	default:
		return strings.TrimSpace(status)
	}
}

func AIVideoStatusForUserWithResult(status string, hasResult bool) string {
	return AIVideoStatusForUser(EffectiveAIVideoStatus(status, hasResult))
}

func AIVideoStatusForManagement(status string) string {
	switch NormalizeAIVideoStatus(status) {
	case AIVideoStatusQueued:
		return AIVideoUserStatusPending
	case AIVideoStatusProcessing:
		return AIVideoManagementStatusRunning
	case AIVideoStatusCompleted:
		return AIVideoUserStatusSuccess
	case AIVideoStatusFailed:
		return AIVideoStatusFailed
	default:
		return strings.TrimSpace(status)
	}
}

func AIVideoStatusForManagementWithResult(status string, hasResult bool) string {
	return AIVideoStatusForManagement(EffectiveAIVideoStatus(status, hasResult))
}

func AIVideoSupportTicketPriority(status string) string {
	switch NormalizeAIVideoStatus(status) {
	case AIVideoStatusFailed:
		return "high"
	case AIVideoStatusQueued, AIVideoStatusProcessing:
		return "medium"
	default:
		return "low"
	}
}

func AIVideoActiveMonitoringStatuses() []string {
	return []string{AIVideoStatusQueued, AIVideoStatusProcessing, aiVideoLegacyStatusInProgress, AIVideoStatusCompleted}
}

func AIVideoFailureTransitionSourceStatuses() []string {
	return []string{AIVideoStatusQueued, AIVideoStatusProcessing, aiVideoLegacyStatusInProgress}
}

func ExpandAIVideoStatusFilter(status string) []string {
	return expandAIVideoStatusesForStorage(status)
}

func expandAIVideoStatusesForStorage(status string) []string {
	trimmed := strings.TrimSpace(status)
	switch strings.ToLower(trimmed) {
	case "", "all":
		return nil
	case AIVideoStatusQueued, AIVideoUserStatusPending:
		return []string{AIVideoStatusQueued}
	case AIVideoManagementStatusRunning, AIVideoStatusProcessing, aiVideoLegacyStatusInProgress, "inprogress":
		return []string{AIVideoStatusProcessing, aiVideoLegacyStatusInProgress}
	case AIVideoStatusCompleted, AIVideoUserStatusSuccess, "succeeded":
		return []string{AIVideoStatusCompleted}
	case AIVideoStatusFailed, "error", "expired", "cancelled", "canceled":
		return []string{AIVideoStatusFailed}
	default:
		normalized := NormalizeAIVideoStatus(trimmed)
		if normalized != "" && normalized != trimmed {
			return expandAIVideoStatusesForStorage(normalized)
		}
		return []string{trimmed}
	}
}

func uniqueAIVideoStatuses(statuses []string) []string {
	result := make([]string, 0, len(statuses))
	seen := make(map[string]struct{}, len(statuses))
	for _, status := range statuses {
		trimmed := strings.TrimSpace(status)
		if trimmed == "" {
			continue
		}
		if _, exists := seen[trimmed]; exists {
			continue
		}
		seen[trimmed] = struct{}{}
		result = append(result, trimmed)
	}
	return result
}
