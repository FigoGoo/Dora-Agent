package modelreceipt

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
)

func TestMemoryStorePutOnceFirstWriterWins(t *testing.T) {
	store := NewMemoryStore()
	first := receiptForTest("turn-first", 1, `{"tool":"A"}`)
	winner, err := store.PutOnce(context.Background(), first)
	if err != nil {
		t.Fatalf("PutOnce(first) error = %v", err)
	}
	second := receiptForTest("turn-first", 1, `{"tool":"B"}`)
	replayed, err := store.PutOnce(context.Background(), second)
	if err != nil {
		t.Fatalf("PutOnce(second) error = %v", err)
	}
	if string(replayed.OutputJSON) != string(winner.OutputJSON) {
		t.Fatalf("replayed output = %s, want first %s", replayed.OutputJSON, winner.OutputJSON)
	}
	if replayed.InputDigest != winner.InputDigest {
		t.Fatalf("input digest was overwritten: got %q want %q", replayed.InputDigest, winner.InputDigest)
	}
}

func TestMemoryStoreConcurrentPutOnceReturnsOneAuthoritativeOutput(t *testing.T) {
	store := NewMemoryStore()
	const writers = 32
	start := make(chan struct{})
	results := make(chan Receipt, writers)
	errs := make(chan error, writers)
	var wg sync.WaitGroup
	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			<-start
			raw, _ := json.Marshal(map[string]any{"writer": index})
			result, err := store.PutOnce(context.Background(), Receipt{
				TurnID: "turn-race", Ordinal: 1, OutputJSON: raw,
				OutputDigest: Digest(raw), InputDigest: "audit-input",
			})
			if err != nil {
				errs <- err
				return
			}
			results <- result
		}(i)
	}
	close(start)
	wg.Wait()
	close(results)
	close(errs)
	for err := range errs {
		t.Fatalf("concurrent PutOnce error = %v", err)
	}
	var digest string
	for result := range results {
		if digest == "" {
			digest = result.OutputDigest
		}
		if result.OutputDigest != digest {
			t.Fatalf("concurrent writers observed different winners: %q and %q", digest, result.OutputDigest)
		}
	}
	stored, err := store.Get(context.Background(), "turn-race", 1)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if stored.OutputDigest != digest {
		t.Fatalf("stored digest = %q, callers observed %q", stored.OutputDigest, digest)
	}
}

func TestMemoryStoreGetReturnsJSONClone(t *testing.T) {
	store := NewMemoryStore()
	_, err := store.PutOnce(context.Background(), receiptForTest("turn-clone", 1, `{"content":"safe"}`))
	if err != nil {
		t.Fatalf("PutOnce() error = %v", err)
	}
	got, err := store.Get(context.Background(), "turn-clone", 1)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	got.OutputJSON[0] = '['
	again, err := store.Get(context.Background(), "turn-clone", 1)
	if err != nil {
		t.Fatalf("Get() again error = %v", err)
	}
	if !json.Valid(again.OutputJSON) || string(again.OutputJSON) != `{"content":"safe"}` {
		t.Fatalf("stored JSON was mutated: %s", again.OutputJSON)
	}
}

func TestMemoryStoreNotFound(t *testing.T) {
	_, err := NewMemoryStore().Get(context.Background(), "missing", 1)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get() error = %v, want ErrNotFound", err)
	}
}

func receiptForTest(turnID string, ordinal int, output string) Receipt {
	raw := json.RawMessage(output)
	return Receipt{
		TurnID: turnID, Ordinal: ordinal, OutputJSON: raw,
		OutputDigest: Digest(raw), InputDigest: "input-" + output,
	}
}
