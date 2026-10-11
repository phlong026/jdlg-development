package route

import (
	"errors"
	"net/http"
	"strings"
	"testing"
)

func TestBuildAIVideoRemoteHTTPErrorMarksPermanentFailures(t *testing.T) {
	tests := []struct {
		name       string
		stage      string
		statusCode int
		body       string
		wantFail   bool
	}{
		{
			name:       "task not exist",
			stage:      "status query failed",
			statusCode: http.StatusBadRequest,
			body:       `{"message":"task_not_exist","type":"task_error","code":"task_not_exist"}`,
			wantFail:   true,
		},
		{
			name:       "content not found",
			stage:      "content query failed",
			statusCode: http.StatusNotFound,
			body:       "<html><title>Not Found</title></html>",
			wantFail:   true,
		},
		{
			name:       "status server error remains retryable",
			stage:      "status query failed",
			statusCode: http.StatusInternalServerError,
			body:       `{"message":"temporary error"}`,
			wantFail:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := buildAIVideoRemoteHTTPError(tt.stage, defaultVideoAPIBaseURL, "video_test", tt.statusCode, []byte(tt.body))
			if got := isUnrecoverableAIVideoRemoteError(err); got != tt.wantFail {
				t.Fatalf("isUnrecoverableAIVideoRemoteError() = %v, want %v; err=%v", got, tt.wantFail, err)
			}
		})
	}
}

func TestBuildAIVideoRemoteJSONErrorMarksInvalidResponsePermanent(t *testing.T) {
	err := buildAIVideoRemoteJSONError("content response invalid", defaultVideoAPIBaseURL, "video_test", []byte{0}, errors.New("invalid character '\\x00' looking for beginning of value"))
	if !isUnrecoverableAIVideoRemoteError(err) {
		t.Fatalf("invalid JSON response should be unrecoverable")
	}
	if !strings.Contains(err.Error(), "content response invalid") {
		t.Fatalf("error should include stage, got %q", err.Error())
	}
}
