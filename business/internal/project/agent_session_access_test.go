package project

import (
	"context"
	"errors"
	"testing"
)

type agentSessionAccessRepositoryStub struct {
	result AgentSessionAccess
	err    error
	calls  int
}

func (repository *agentSessionAccessRepositoryStub) FindReadyAgentSessionAccess(_ context.Context, _, _ string) (AgentSessionAccess, error) {
	repository.calls++
	return repository.result, repository.err
}

func TestAgentSessionAccessServiceResolve(t *testing.T) {
	repository := &agentSessionAccessRepositoryStub{result: AgentSessionAccess{
		ProjectID:      "019f0000-0000-7000-8000-000000000004",
		AgentSessionID: "019f0000-0000-7000-8000-000000000005",
	}}
	service, err := NewAgentSessionAccessService(repository)
	if err != nil {
		t.Fatalf("NewAgentSessionAccessService() error = %v", err)
	}
	result, err := service.Resolve(context.Background(), "019f0000-0000-7000-8000-000000000002", repository.result.AgentSessionID)
	if err != nil || result != repository.result || repository.calls != 1 {
		t.Fatalf("Resolve() = %+v, %v, calls=%d", result, err, repository.calls)
	}
}

func TestAgentSessionAccessServiceHidesInvalidAndMismatchedSession(t *testing.T) {
	repository := &agentSessionAccessRepositoryStub{result: AgentSessionAccess{
		ProjectID:      "019f0000-0000-7000-8000-000000000004",
		AgentSessionID: "019f0000-0000-7000-8000-000000000006",
	}}
	service, _ := NewAgentSessionAccessService(repository)
	if _, err := service.Resolve(context.Background(), "not-a-user", "not-a-session"); !errors.Is(err, ErrAgentSessionNotFound) || repository.calls != 0 {
		t.Fatalf("invalid IDs error=%v calls=%d", err, repository.calls)
	}
	if _, err := service.Resolve(context.Background(), "019f0000-0000-7000-8000-000000000002", "019f0000-0000-7000-8000-000000000005"); !errors.Is(err, ErrAgentSessionNotFound) {
		t.Fatalf("mismatched session error=%v", err)
	}
}
