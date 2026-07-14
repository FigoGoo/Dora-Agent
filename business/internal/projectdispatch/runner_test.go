package projectdispatch

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/FigoGoo/Dora-Agent/business/internal/project"
)

type scriptedDispatcher struct {
	mu       sync.Mutex
	errors   []error
	calls    int
	cancel   context.CancelFunc
	cancelAt int
}

func (dispatcher *scriptedDispatcher) DispatchNext(context.Context) error {
	dispatcher.mu.Lock()
	defer dispatcher.mu.Unlock()
	dispatcher.calls++
	if dispatcher.cancel != nil && dispatcher.calls == dispatcher.cancelAt {
		dispatcher.cancel()
	}
	if dispatcher.calls <= len(dispatcher.errors) {
		return dispatcher.errors[dispatcher.calls-1]
	}
	return project.ErrOutboxEmpty
}

func TestRunnerImmediatelyDrainsSuccessThenStopsOnCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	dispatcher := &scriptedDispatcher{
		errors: []error{nil, project.ErrOutboxEmpty}, cancel: cancel, cancelAt: 2,
	}
	runner, err := New(dispatcher, slog.New(slog.NewTextHandler(io.Discard, nil)), Config{PollInterval: time.Hour})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if err := runner.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if dispatcher.calls != 2 {
		t.Fatalf("DispatchNext calls = %d, want 2", dispatcher.calls)
	}
}

func TestRunnerWaitsAfterFailureAndHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	dispatcher := &scriptedDispatcher{
		errors: []error{errors.New("database detail must not escape")}, cancel: cancel, cancelAt: 1,
	}
	runner, err := New(dispatcher, slog.New(slog.NewTextHandler(io.Discard, nil)), Config{PollInterval: time.Hour})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if err := runner.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if dispatcher.calls != 1 {
		t.Fatalf("DispatchNext calls = %d, want 1", dispatcher.calls)
	}
}

func TestNewRejectsInvalidDependencies(t *testing.T) {
	if _, err := New(nil, slog.Default(), Config{PollInterval: time.Second}); err == nil {
		t.Fatal("New() error = nil, want invalid dependency error")
	}
}
