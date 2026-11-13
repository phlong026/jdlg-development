package aivideostudio

import "testing"

func TestUncertainSubmissionNeverReturnsToSubmission(t *testing.T) {
	for _, to := range []string{TaskQueued, TaskSubmitting} {
		if CanTransition(TaskUncertain, to) {
			t.Fatalf("uncertain must not transition to %s", to)
		}
	}
	if MayCreateProviderRequest(TaskUncertain) {
		t.Fatal("uncertain task must not create another provider request")
	}
	if !Recoverable(TaskUncertain) {
		t.Fatal("uncertain task must remain query-recoverable")
	}
}

func TestTerminalTaskCannotBeOverwrittenByLateResponse(t *testing.T) {
	for _, from := range []string{TaskCompleted, TaskFailed, TaskCancelled} {
		for _, to := range []string{TaskRunning, TaskCompleted, TaskFailed} {
			if CanTransition(from, to) {
				t.Fatalf("terminal %s must not transition to %s", from, to)
			}
		}
	}
}

func TestSubmittedTaskRecoversByQuery(t *testing.T) {
	for _, to := range []string{TaskRunning, TaskCompleted, TaskFailed, TaskCancelled} {
		if !CanTransition(TaskSubmitted, to) {
			t.Fatalf("submitted task should transition to %s", to)
		}
	}
}
