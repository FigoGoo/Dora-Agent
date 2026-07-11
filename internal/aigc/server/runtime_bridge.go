package server

import (
	"context"
	"fmt"
	"sync"

	"github.com/FigoGoo/Dora-Agent/internal/aigc/sessionruntime"
)

// RuntimeBridge breaks the startup dependency cycle between ApprovalRuntime,
// DurableAgentProcessor and SessionRuntime.Manager.
type RuntimeBridge struct {
	mu     sync.RWMutex
	target SessionRuntime
}

func NewRuntimeBridge() *RuntimeBridge { return &RuntimeBridge{} }

func (b *RuntimeBridge) Set(target SessionRuntime) { b.mu.Lock(); b.target = target; b.mu.Unlock() }
func (b *RuntimeBridge) get() (SessionRuntime, error) {
	b.mu.RLock()
	target := b.target
	b.mu.RUnlock()
	if target == nil {
		return nil, fmt.Errorf("session runtime is not ready")
	}
	return target, nil
}
func (b *RuntimeBridge) Enqueue(ctx context.Context, sessionID string, input sessionruntime.SessionInput) (sessionruntime.EnqueueResult, error) {
	target, err := b.get()
	if err != nil {
		return sessionruntime.EnqueueResult{}, err
	}
	return target.Enqueue(ctx, sessionID, input)
}
func (b *RuntimeBridge) EnsureSession(ctx context.Context, sessionID string) (bool, error) {
	target, err := b.get()
	if err != nil {
		return false, err
	}
	return target.EnsureSession(ctx, sessionID)
}
func (b *RuntimeBridge) Wake(sessionID string) {
	if target, err := b.get(); err == nil {
		target.Wake(sessionID)
	}
}

var _ SessionRuntime = (*RuntimeBridge)(nil)
