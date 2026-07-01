package state

import (
	"testing"

	"github.com/FigoGoo/Dora-Agent/internal/contracts/pr1"
)

func TestRunStatusAliasesPR1Contract(t *testing.T) {
	aliases := map[string]string{
		RunStatusCreated:             pr1.RunStatusCreated,
		RunStatusRouting:             pr1.RunStatusRouting,
		RunStatusPlanning:            pr1.RunStatusPlanning,
		RunStatusWaitingInput:        pr1.RunStatusWaitingInput,
		RunStatusWaitingConfirmation: pr1.RunStatusWaitingConfirmation,
		RunStatusFreezing:            pr1.RunStatusFreezing,
		RunStatusQueued:              pr1.RunStatusQueued,
		RunStatusRunning:             pr1.RunStatusRunning,
		RunStatusCompleted:           pr1.RunStatusCompleted,
		RunStatusFailed:              pr1.RunStatusFailed,
		RunStatusCancelled:           pr1.RunStatusCancelled,
	}
	for domainStatus, contractStatus := range aliases {
		if domainStatus != contractStatus {
			t.Fatalf("domain run status %q does not match PR-1 %q", domainStatus, contractStatus)
		}
		if !pr1.IsValidState(pr1.StateRunStatus, domainStatus) {
			t.Fatalf("domain run status %q must be valid PR-1 RunStatus", domainStatus)
		}
	}
}

func TestLocalRunStatusExtensionsStayOutOfPR1Contract(t *testing.T) {
	for _, status := range []string{RunStatusPending, RunStatusResuming} {
		if pr1.IsValidState(pr1.StateRunStatus, status) {
			t.Fatalf("local runtime extension %q must not drift into PR-1 RunStatus without contract update", status)
		}
	}
}
