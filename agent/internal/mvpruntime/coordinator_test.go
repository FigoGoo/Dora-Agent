package mvpruntime

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

type coordinatorHandler struct {
	mu      sync.Mutex
	name    string
	work    int
	err     error
	record  *coordinatorCallRecord
	started chan struct{}
	release chan struct{}
}

type coordinatorCallRecord struct {
	mu     sync.Mutex
	values []string
}

func (r *coordinatorCallRecord) append(value string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.values = append(r.values, value)
}

func (r *coordinatorCallRecord) snapshot() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.values...)
}

func (h *coordinatorHandler) ProcessNext(ctx context.Context) (bool, error) {
	h.mu.Lock()
	if h.record != nil {
		h.record.append(h.name)
	}
	if h.started != nil {
		select {
		case <-h.started:
		default:
			close(h.started)
		}
	}
	release := h.release
	err := h.err
	claimed := h.work > 0
	if claimed {
		h.work--
	}
	h.mu.Unlock()
	if release != nil {
		select {
		case <-release:
		case <-ctx.Done():
			return false, ctx.Err()
		}
	}
	return claimed, err
}

// TestCoordinatorRoundRobinDrainsUntilFullEmptyRound 验证固定顺序轮转且命中后继续扫描到完整空轮。
func TestCoordinatorRoundRobinDrainsUntilFullEmptyRound(t *testing.T) {
	record := &coordinatorCallRecord{}
	handlers := make([]Handler, 0, 5)
	for _, name := range []string{"user", "creation", "analyze", "storyboard", "prompts"} {
		handlers = append(handlers, &coordinatorHandler{name: name, work: 1, record: record})
	}
	coordinator, err := NewCoordinator(handlers, Config{Concurrency: 1, PollInterval: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	if err := coordinator.Start(); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for {
		if len(record.snapshot()) >= 10 || time.Now().After(deadline) {
			break
		}
		time.Sleep(time.Millisecond)
	}
	stopCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := coordinator.Stop(stopCtx); err != nil {
		t.Fatal(err)
	}
	want := []string{"user", "creation", "analyze", "storyboard", "prompts", "user", "creation", "analyze", "storyboard", "prompts"}
	got := record.snapshot()
	if len(got) < len(want) {
		t.Fatalf("扫描未完成: %v", got)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("轮转顺序漂移: got=%v want=%v", got[:len(want)], want)
		}
	}
}

// TestCoordinatorWakeAndHandlerErrorRecover 验证单轮错误不会终止 Scanner，后续 Wake 可继续处理。
func TestCoordinatorWakeAndHandlerErrorRecover(t *testing.T) {
	first := &coordinatorHandler{name: "first", err: errors.New("temporary")}
	handlers := []Handler{first}
	for index := 1; index < 5; index++ {
		handlers = append(handlers, &coordinatorHandler{name: "empty"})
	}
	coordinator, err := NewCoordinator(handlers, Config{Concurrency: 1, PollInterval: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	if err := coordinator.Start(); err != nil {
		t.Fatal(err)
	}
	time.Sleep(5 * time.Millisecond)
	first.mu.Lock()
	first.err = nil
	first.work = 1
	first.mu.Unlock()
	coordinator.Wake()
	deadline := time.Now().Add(time.Second)
	for {
		first.mu.Lock()
		remaining := first.work
		first.mu.Unlock()
		if remaining == 0 || time.Now().After(deadline) {
			break
		}
		time.Sleep(time.Millisecond)
	}
	first.mu.Lock()
	remaining := first.work
	first.mu.Unlock()
	if remaining != 0 {
		t.Fatal("后续 Wake 未从瞬时错误恢复")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := coordinator.Stop(ctx); err != nil {
		t.Fatal(err)
	}
}

// TestCoordinatorLifecycleAndDrain 验证重复启动失败，并在 Stop 时等待已进入的 ProcessNext。
func TestCoordinatorLifecycleAndDrain(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	handlers := []Handler{&coordinatorHandler{name: "blocking", work: 1, started: started, release: release}}
	for index := 1; index < 5; index++ {
		handlers = append(handlers, &coordinatorHandler{name: "empty"})
	}
	coordinator, err := NewCoordinator(handlers, Config{Concurrency: 1, PollInterval: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	if err := coordinator.Start(); err != nil {
		t.Fatal(err)
	}
	if err := coordinator.Start(); err == nil {
		t.Fatal("重复启动未失败关闭")
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("阻塞 Handler 未进入")
	}
	stopDone := make(chan error, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		stopDone <- coordinator.Stop(ctx)
	}()
	select {
	case err := <-stopDone:
		t.Fatalf("Coordinator 未等待 Drain: %v", err)
	case <-time.After(10 * time.Millisecond):
	}
	close(release)
	if err := <-stopDone; err != nil {
		t.Fatalf("Drain 后停止失败: %v", err)
	}
}

// TestNewCoordinatorRejectsInvalidHandlers 验证少于五个或空 Handler 不能形成半套能力。
func TestNewCoordinatorRejectsInvalidHandlers(t *testing.T) {
	if _, err := NewCoordinator(nil, Config{Concurrency: 1, PollInterval: time.Second}); err == nil {
		t.Fatal("空 Handler 集合未失败关闭")
	}
	handlers := []Handler{
		&coordinatorHandler{}, &coordinatorHandler{}, nil, &coordinatorHandler{}, &coordinatorHandler{},
	}
	if _, err := NewCoordinator(handlers, Config{Concurrency: 1, PollInterval: time.Second}); err == nil {
		t.Fatal("nil Handler 未失败关闭")
	}
}

var _ Handler = (*coordinatorHandler)(nil)
