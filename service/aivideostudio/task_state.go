package aivideostudio

const (
	TaskQueued     = "queued"
	TaskSubmitting = "submitting"
	TaskUncertain  = "uncertain"
	TaskSubmitted  = "submitted"
	TaskRunning    = "running"
	TaskCompleted  = "completed"
	TaskFailed     = "failed"
	TaskCancelled  = "cancelled"
)

// CanTransition is the durable worker's state contract. In particular, an
// uncertain submission may only advance from provider query evidence; it never
// returns to queued/submitting where another paid request could be created.
func CanTransition(from, to string) bool {
	allowed := map[string]map[string]bool{
		TaskQueued:     {TaskSubmitting: true, TaskCancelled: true},
		TaskSubmitting: {TaskSubmitted: true, TaskUncertain: true, TaskFailed: true},
		TaskUncertain:  {TaskSubmitted: true, TaskRunning: true, TaskCompleted: true, TaskFailed: true, TaskCancelled: true},
		TaskSubmitted:  {TaskRunning: true, TaskCompleted: true, TaskFailed: true, TaskCancelled: true},
		TaskRunning:    {TaskRunning: true, TaskCompleted: true, TaskFailed: true, TaskCancelled: true},
	}
	return allowed[from][to]
}

func Recoverable(status string) bool {
	return status == TaskUncertain || status == TaskSubmitted || status == TaskRunning
}
func MayCreateProviderRequest(status string) bool { return status == TaskSubmitting }
