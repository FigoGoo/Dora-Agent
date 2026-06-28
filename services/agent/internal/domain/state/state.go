package state

const (
	SessionStatusActive   = "active"
	SessionStatusArchived = "archived"
	SessionStatusExpired  = "expired"

	RunStatusPending             = "pending"
	RunStatusRunning             = "running"
	RunStatusWaitingConfirmation = "waiting_confirmation"
	RunStatusResuming            = "resuming"
	RunStatusCompleted           = "completed"
	RunStatusFailed              = "failed"
	RunStatusCancelled           = "cancelled"

	TaskStatusPending   = "pending"
	TaskStatusRunning   = "running"
	TaskStatusCompleted = "completed"
	TaskStatusFailed    = "failed"
	TaskStatusCancelled = "cancelled"

	InterruptStatusRequired = "required"
	InterruptStatusAccepted = "accepted"
	InterruptStatusRejected = "rejected"
	InterruptStatusExpired  = "expired"
	InterruptStatusResolved = "resolved"

	SafetyResultPassed  = "passed"
	SafetyResultBlocked = "blocked"
	SafetyResultFailed  = "failed"
)

func CanTransitionRun(from, to string) bool {
	switch from {
	case RunStatusPending:
		return to == RunStatusRunning || to == RunStatusCancelled || to == RunStatusFailed
	case RunStatusRunning:
		return to == RunStatusWaitingConfirmation || to == RunStatusCompleted || to == RunStatusFailed || to == RunStatusCancelled
	case RunStatusWaitingConfirmation:
		return to == RunStatusResuming || to == RunStatusCancelled || to == RunStatusFailed
	case RunStatusResuming:
		return to == RunStatusRunning || to == RunStatusFailed || to == RunStatusCancelled
	case RunStatusCompleted, RunStatusFailed, RunStatusCancelled:
		return false
	default:
		return false
	}
}

func CanTransitionTask(from, to string) bool {
	switch from {
	case TaskStatusPending:
		return to == TaskStatusRunning || to == TaskStatusCancelled
	case TaskStatusRunning:
		return to == TaskStatusCompleted || to == TaskStatusFailed || to == TaskStatusCancelled
	case TaskStatusCompleted, TaskStatusFailed, TaskStatusCancelled:
		return false
	default:
		return false
	}
}

func CanTransitionInterrupt(from, to string) bool {
	switch from {
	case InterruptStatusRequired:
		return to == InterruptStatusAccepted || to == InterruptStatusRejected || to == InterruptStatusExpired
	case InterruptStatusAccepted:
		return to == InterruptStatusResolved
	case InterruptStatusRejected, InterruptStatusExpired, InterruptStatusResolved:
		return false
	default:
		return false
	}
}

func IsValidSafetyResult(result string) bool {
	return result == SafetyResultPassed || result == SafetyResultBlocked || result == SafetyResultFailed
}
