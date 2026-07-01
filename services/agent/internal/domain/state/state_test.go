package state

import (
	"testing"

	"github.com/FigoGoo/Dora-Agent/internal/contracts/foundation"
)

func TestRunStatusAliasesFoundationContract(t *testing.T) {
	aliases := map[string]string{
		RunStatusCreated:             foundation.RunStatusCreated,
		RunStatusRouting:             foundation.RunStatusRouting,
		RunStatusPlanning:            foundation.RunStatusPlanning,
		RunStatusWaitingInput:        foundation.RunStatusWaitingInput,
		RunStatusWaitingConfirmation: foundation.RunStatusWaitingConfirmation,
		RunStatusFreezing:            foundation.RunStatusFreezing,
		RunStatusQueued:              foundation.RunStatusQueued,
		RunStatusRunning:             foundation.RunStatusRunning,
		RunStatusCompleted:           foundation.RunStatusCompleted,
		RunStatusFailed:              foundation.RunStatusFailed,
		RunStatusCancelled:           foundation.RunStatusCancelled,
	}
	for domainStatus, contractStatus := range aliases {
		if domainStatus != contractStatus {
			t.Fatalf("domain run status %q does not match foundation %q", domainStatus, contractStatus)
		}
		if !foundation.IsValidState(foundation.StateRunStatus, domainStatus) {
			t.Fatalf("domain run status %q must be valid foundation RunStatus", domainStatus)
		}
	}
}

func TestLocalRunStatusExtensionsStayOutOfFoundationContract(t *testing.T) {
	for _, status := range []string{RunStatusPending, RunStatusResuming} {
		if foundation.IsValidState(foundation.StateRunStatus, status) {
			t.Fatalf("local runtime extension %q must not drift into foundation RunStatus without contract update", status)
		}
	}
}
