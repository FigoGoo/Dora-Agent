package generation

import (
	"errors"
	"fmt"
)

var (
	ErrVersionConflict     = errors.New("generation version conflict")
	ErrInvalidTransition   = errors.New("invalid generation state transition")
	ErrDuplicate           = errors.New("generation record already exists")
	ErrIdempotencyConflict = errors.New("generation idempotency key conflict")
)

var jobTransitions = map[string]map[string]struct{}{
	StatusQueued: {
		StatusRunning: {}, StatusCancelled: {}, StatusFailed: {},
	},
	StatusRunning: {
		StatusWaitingProvider: {}, StatusFinalizing: {}, StatusRetryWait: {}, StatusFailed: {}, StatusCancelled: {},
	},
	StatusWaitingProvider: {
		StatusRunning: {}, StatusFinalizing: {}, StatusRetryWait: {}, StatusFailed: {}, StatusCancelled: {},
	},
	StatusFinalizing: {
		StatusRetryWait: {}, StatusSucceeded: {}, StatusFailed: {}, StatusCancelled: {},
	},
	StatusRetryWait: {
		StatusRunning: {}, StatusFinalizing: {}, StatusFailed: {}, StatusCancelled: {},
	},
}

var batchTransitions = map[string]map[string]struct{}{
	BatchStatusWaitingJobs: {
		BatchStatusFinalizing: {}, BatchStatusCancelling: {}, BatchStatusCompleted: {}, BatchStatusPartialFailed: {}, BatchStatusFailed: {}, BatchStatusCancelled: {},
	},
	BatchStatusFinalizing: {
		BatchStatusCancelling: {}, BatchStatusCompleted: {}, BatchStatusPartialFailed: {}, BatchStatusFailed: {}, BatchStatusCancelled: {},
	},
	BatchStatusCancelling: {
		BatchStatusCancelled: {}, BatchStatusCompleted: {}, BatchStatusPartialFailed: {},
	},
}

var operationTransitions = map[string]map[string]struct{}{
	OperationStatusAccepted: {
		OperationStatusWaitingJobs: {}, OperationStatusCompleted: {}, OperationStatusPartialFailed: {}, OperationStatusFailed: {}, OperationStatusCancelled: {},
	},
	OperationStatusWaitingJobs: {
		OperationStatusCompleted: {}, OperationStatusPartialFailed: {}, OperationStatusFailed: {}, OperationStatusCancelled: {},
	},
}

func ValidateJobTransition(from, to string) error {
	if NormalizeStatus(from) == "" || NormalizeStatus(to) == "" {
		return fmt.Errorf("%w: job %q -> %q", ErrInvalidTransition, from, to)
	}
	if from == to {
		return nil
	}
	if _, ok := jobTransitions[from][to]; !ok {
		return fmt.Errorf("%w: job %q -> %q", ErrInvalidTransition, from, to)
	}
	return nil
}

func ValidateBatchTransition(from, to string) error {
	if from == to {
		return nil
	}
	if _, ok := batchTransitions[from][to]; !ok {
		return fmt.Errorf("%w: batch %q -> %q", ErrInvalidTransition, from, to)
	}
	return nil
}

func ValidateOperationTransition(from, to string) error {
	if from == to {
		return nil
	}
	if _, ok := operationTransitions[from][to]; !ok {
		return fmt.Errorf("%w: operation %q -> %q", ErrInvalidTransition, from, to)
	}
	return nil
}
