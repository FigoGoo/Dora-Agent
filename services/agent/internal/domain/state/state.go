package state

import "github.com/FigoGoo/Dora-Agent/internal/contracts/foundation"

const (
	SessionStatusActive   = "active"
	SessionStatusArchived = "archived"
	SessionStatusExpired  = "expired"

	RunStatusCreated             = foundation.RunStatusCreated
	RunStatusRouting             = foundation.RunStatusRouting
	RunStatusPending             = "pending"
	RunStatusPlanning            = foundation.RunStatusPlanning
	RunStatusWaitingInput        = foundation.RunStatusWaitingInput
	RunStatusRunning             = foundation.RunStatusRunning
	RunStatusWaitingConfirmation = foundation.RunStatusWaitingConfirmation
	RunStatusFreezing            = foundation.RunStatusFreezing
	RunStatusQueued              = foundation.RunStatusQueued
	RunStatusResuming            = "resuming"
	RunStatusCompleted           = foundation.RunStatusCompleted
	RunStatusFailed              = foundation.RunStatusFailed
	RunStatusCancelled           = foundation.RunStatusCancelled

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
	case RunStatusCreated:
		return to == RunStatusRouting || to == RunStatusCancelled || to == RunStatusFailed
	case RunStatusPending:
		return to == RunStatusRouting || to == RunStatusRunning || to == RunStatusCancelled || to == RunStatusFailed
	case RunStatusRouting:
		return to == RunStatusPlanning || to == RunStatusWaitingInput || to == RunStatusWaitingConfirmation || to == RunStatusCompleted || to == RunStatusFailed || to == RunStatusCancelled
	case RunStatusPlanning:
		return to == RunStatusWaitingInput || to == RunStatusWaitingConfirmation || to == RunStatusCompleted || to == RunStatusFailed || to == RunStatusCancelled
	case RunStatusWaitingInput:
		return to == RunStatusRouting || to == RunStatusCancelled || to == RunStatusFailed
	case RunStatusFreezing:
		return to == RunStatusQueued || to == RunStatusRunning || to == RunStatusFailed || to == RunStatusCancelled
	case RunStatusQueued:
		return to == RunStatusRunning || to == RunStatusFailed || to == RunStatusCancelled
	case RunStatusRunning:
		return to == RunStatusWaitingConfirmation || to == RunStatusCompleted || to == RunStatusFailed || to == RunStatusCancelled
	case RunStatusWaitingConfirmation:
		return to == RunStatusFreezing || to == RunStatusResuming || to == RunStatusCancelled || to == RunStatusFailed
	case RunStatusResuming:
		return to == RunStatusRouting || to == RunStatusRunning || to == RunStatusFailed || to == RunStatusCancelled
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
