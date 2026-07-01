package stream

import (
	"errors"
	"testing"
	"time"

	"github.com/FigoGoo/Dora-Agent/internal/contracts/pr1"
)

func TestPR2RedisKeys(t *testing.T) {
	if got := RunEventsKey("run_1"); got != "agent:run:run_1:events" {
		t.Fatalf("run events key = %s", got)
	}
	if got := RunSnapshotKey("run_1"); got != "agent:run:run_1:snapshot" {
		t.Fatalf("run snapshot key = %s", got)
	}
	if got := BoardSnapshotKey("board_1", 3); got != "agent:board:board_1:snapshot:3" {
		t.Fatalf("board snapshot key = %s", got)
	}
	if got := TurnLockKey("run_1"); got != "lock:agent:run:run_1:turn" {
		t.Fatalf("turn lock key = %s", got)
	}
}

func TestMemoryAGUIEventBusReplaysBySeqAndDedupes(t *testing.T) {
	bus := NewMemoryAGUIEventBus()
	third := testAGUIEvent(t, "run_1", "board.snapshot.updated", 3)
	first := testAGUIEvent(t, "run_1", "graph.plan.created", 1)
	second := testAGUIEvent(t, "run_1", "board.patch.applied", 2)
	for _, event := range []pr1.AGUIEnvelope{third, first, second, second} {
		if err := bus.PublishAGUI(t.Context(), event); err != nil {
			t.Fatalf("publish event: %v", err)
		}
	}
	replayed, err := bus.ReplayAGUI(t.Context(), "run_1", 1, 10)
	if err != nil {
		t.Fatalf("replay events: %v", err)
	}
	if len(replayed) != 2 || replayed[0].Seq != 2 || replayed[1].Seq != 3 {
		t.Fatalf("unexpected replay order: %#v", replayed)
	}
}

func TestMemorySnapshotCacheTTLAndCopy(t *testing.T) {
	cache := NewMemorySnapshotCache()
	now := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	cache.now = func() time.Time { return now }
	key := BoardSnapshotKey("board_1", 1)
	if err := cache.Set(t.Context(), key, []byte(`{"version":1}`), time.Minute); err != nil {
		t.Fatalf("set cache: %v", err)
	}
	value, ok, err := cache.Get(t.Context(), key)
	if err != nil || !ok || string(value) != `{"version":1}` {
		t.Fatalf("get cache value=%s ok=%v err=%v", string(value), ok, err)
	}
	value[0] = '['
	again, ok, err := cache.Get(t.Context(), key)
	if err != nil || !ok || string(again) != `{"version":1}` {
		t.Fatalf("cache did not copy value: value=%s ok=%v err=%v", string(again), ok, err)
	}
	now = now.Add(2 * time.Minute)
	if value, ok, err := cache.Get(t.Context(), key); err != nil || ok || value != nil {
		t.Fatalf("expired cache value=%s ok=%v err=%v", string(value), ok, err)
	}
}

func TestMemoryTurnLockOwnerAndExpiry(t *testing.T) {
	locker := NewMemoryTurnLock()
	now := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	locker.now = func() time.Time { return now }
	key := TurnLockKey("run_1")
	acquired, err := locker.Acquire(t.Context(), key, "owner_a", time.Minute)
	if err != nil || !acquired {
		t.Fatalf("acquire lock acquired=%v err=%v", acquired, err)
	}
	acquired, err = locker.Acquire(t.Context(), key, "owner_b", time.Minute)
	if err != nil || acquired {
		t.Fatalf("second acquire should wait acquired=%v err=%v", acquired, err)
	}
	if released, err := locker.Release(t.Context(), key, "owner_b"); !errors.Is(err, ErrLockNotOwned) || released {
		t.Fatalf("expected owner mismatch released=%v err=%v", released, err)
	}
	if released, err := locker.Release(t.Context(), key, "owner_a"); err != nil || !released {
		t.Fatalf("release owner_a released=%v err=%v", released, err)
	}
	now = now.Add(2 * time.Minute)
	acquired, err = locker.Acquire(t.Context(), key, "owner_b", time.Minute)
	if err != nil || !acquired {
		t.Fatalf("acquire after release acquired=%v err=%v", acquired, err)
	}
}

func testAGUIEvent(t *testing.T, runID string, eventType string, seq int64) pr1.AGUIEnvelope {
	t.Helper()
	payload := map[string]any{"seq": seq}
	digest, err := pr1.CanonicalDigest(payload)
	if err != nil {
		t.Fatalf("digest: %v", err)
	}
	event, err := pr1.BuildAGUIEnvelope(pr1.AGUIInput{
		EventID:       "evt_test_" + eventType + "_" + time.Unix(seq, 0).UTC().Format("150405"),
		EventType:     eventType,
		ProjectID:     "proj_1",
		SessionID:     "sess_1",
		RunID:         runID,
		Seq:           seq,
		CreatedAt:     time.Date(2026, 7, 1, 12, 0, int(seq), 0, time.UTC),
		PayloadDigest: digest,
		Payload:       payload,
	})
	if err != nil {
		t.Fatalf("build event: %v", err)
	}
	return event
}
