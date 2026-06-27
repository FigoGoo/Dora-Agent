package state

const (
	SessionStatusActive   = "active"
	SessionStatusArchived = "archived"

	RunStatusPending     = "pending"
	RunStatusRunning     = "running"
	RunStatusInterrupted = "interrupted"
	RunStatusCompleted   = "completed"
	RunStatusFailed      = "failed"
	RunStatusCanceled    = "canceled"

	InterruptStatusPending  = "pending"
	InterruptStatusResolved = "resolved"
	InterruptStatusExpired  = "expired"
	InterruptStatusCanceled = "canceled"
)

func CanTransitionRun(from, to string) bool {
	switch from {
	case RunStatusPending:
		return to == RunStatusRunning || to == RunStatusCanceled || to == RunStatusFailed
	case RunStatusRunning:
		return to == RunStatusInterrupted || to == RunStatusCompleted || to == RunStatusFailed || to == RunStatusCanceled
	case RunStatusInterrupted:
		return to == RunStatusRunning || to == RunStatusCompleted || to == RunStatusFailed || to == RunStatusCanceled
	case RunStatusCompleted, RunStatusFailed, RunStatusCanceled:
		return false
	default:
		return false
	}
}

func CanResolveInterrupt(from, to string) bool {
	if from != InterruptStatusPending {
		return false
	}
	return to == InterruptStatusResolved || to == InterruptStatusExpired || to == InterruptStatusCanceled
}
