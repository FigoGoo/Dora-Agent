package billing

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
)

func TestMemoryStoreChargeAndRefundAreIdempotent(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	if _, err := store.EnsureAccount(ctx, "u1", 100); err != nil {
		t.Fatal(err)
	}
	charge := MutationRequest{TransactionID: "charge-1", UserID: "u1", IdempotencyKey: "generation:charge:j1", Points: 40, JobID: "j1"}
	first, err := store.Charge(ctx, charge)
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.Charge(ctx, charge)
	if err != nil || !second.Duplicate || second.Transaction.ID != first.Transaction.ID {
		t.Fatalf("duplicate charge = %#v, %v", second, err)
	}
	refund := MutationRequest{TransactionID: "refund-1", UserID: "u1", IdempotencyKey: "generation:refund:j1:charge-1", ReferenceID: "charge-1", Points: 40}
	if _, err := store.Refund(ctx, refund); err != nil {
		t.Fatal(err)
	}
	account, _ := store.GetAccount(ctx, "u1")
	if account.Balance != 100 {
		t.Fatalf("balance = %d", account.Balance)
	}
}

func TestMemoryStoreRejectsInsufficientAndExcessRefund(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	_, _ = store.EnsureAccount(ctx, "u1", 10)
	if _, err := store.Charge(ctx, MutationRequest{TransactionID: "c0", UserID: "u1", IdempotencyKey: "c0", Points: 11}); !errors.Is(err, ErrInsufficientPoints) {
		t.Fatalf("insufficient error = %v", err)
	}
	_, _ = store.Credit(ctx, MutationRequest{TransactionID: "credit", UserID: "u1", IdempotencyKey: "credit", Points: 20})
	_, _ = store.Charge(ctx, MutationRequest{TransactionID: "charge", UserID: "u1", IdempotencyKey: "charge", Points: 20})
	if _, err := store.Refund(ctx, MutationRequest{TransactionID: "refund", UserID: "u1", IdempotencyKey: "refund", ReferenceID: "charge", Points: 21}); !errors.Is(err, ErrRefundExceedsCharge) {
		t.Fatalf("refund error = %v", err)
	}
}

func TestMemoryStoreRejectsIdempotencyKeyReuseWithDifferentMutation(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(MutationRequest) MutationRequest
		invoke func(context.Context, *MemoryStore, MutationRequest) (Result, error)
	}{
		{name: "kind", invoke: func(ctx context.Context, store *MemoryStore, request MutationRequest) (Result, error) {
			return store.Charge(ctx, request)
		}},
		{name: "user", mutate: func(request MutationRequest) MutationRequest { request.UserID = "u2"; return request }},
		{name: "points", mutate: func(request MutationRequest) MutationRequest { request.Points++; return request }},
		{name: "reference", mutate: func(request MutationRequest) MutationRequest { request.ReferenceID = "charge-2"; return request }},
		{name: "operation", mutate: func(request MutationRequest) MutationRequest { request.OperationID = "operation-2"; return request }},
		{name: "batch", mutate: func(request MutationRequest) MutationRequest { request.BatchID = "batch-2"; return request }},
		{name: "job", mutate: func(request MutationRequest) MutationRequest { request.JobID = "job-2"; return request }},
		{name: "breakdown", mutate: func(request MutationRequest) MutationRequest {
			request.Breakdown = map[string]int64{"image": 9, "video": 1}
			return request
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			store := NewMemoryStore()
			_, _ = store.EnsureAccount(ctx, "u1", 100)
			_, _ = store.EnsureAccount(ctx, "u2", 100)
			base := MutationRequest{
				TransactionID: "credit-1", UserID: "u1", IdempotencyKey: "credit-key",
				ReferenceID: "charge-1", OperationID: "operation-1", BatchID: "batch-1", JobID: "job-1",
				Points: 10, Breakdown: map[string]int64{"image": 7, "video": 3},
			}
			if _, err := store.Credit(ctx, base); err != nil {
				t.Fatal(err)
			}
			conflict := base
			conflict.TransactionID = "retry-transaction-id"
			if test.mutate != nil {
				conflict = test.mutate(conflict)
			}
			invoke := test.invoke
			if invoke == nil {
				invoke = func(ctx context.Context, store *MemoryStore, request MutationRequest) (Result, error) {
					return store.Credit(ctx, request)
				}
			}
			if _, err := invoke(ctx, store, conflict); !errors.Is(err, ErrIdempotencyConflict) {
				t.Fatalf("error = %v, want ErrIdempotencyConflict", err)
			}
			account, err := store.GetAccount(ctx, "u1")
			if err != nil || account.Balance != 110 {
				t.Fatalf("account after conflict = %#v, %v", account, err)
			}
		})
	}
}

func TestMemoryStoreNormalizesIdempotentReplay(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	_, _ = store.EnsureAccount(ctx, "u1", 100)
	first, err := store.Credit(ctx, MutationRequest{
		TransactionID: " credit-1 ", UserID: " u1 ", IdempotencyKey: " credit-key ",
		ReferenceID: " charge-1 ", OperationID: " operation-1 ", BatchID: " batch-1 ", JobID: " job-1 ",
		Points: 10, Breakdown: map[string]int64{"video": 3, "image": 7}, Metadata: map[string]any{"attempt": 1},
	})
	if err != nil {
		t.Fatal(err)
	}
	replay, err := store.Credit(ctx, MutationRequest{
		TransactionID: "a-different-retry-id", UserID: "u1", IdempotencyKey: "credit-key",
		ReferenceID: "charge-1", OperationID: "operation-1", BatchID: "batch-1", JobID: "job-1",
		Points: 10, Breakdown: map[string]int64{"image": 7, "video": 3}, Metadata: map[string]any{"attempt": 2},
	})
	if err != nil || !replay.Duplicate || replay.Transaction.ID != first.Transaction.ID {
		t.Fatalf("normalized replay = %#v, %v", replay, err)
	}
}

func TestMemoryStoreConcurrentIdempotentMutationCreditsOnce(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	_, _ = store.EnsureAccount(ctx, "u1", 100)

	const workers = 16
	start := make(chan struct{})
	errorsByWorker := make(chan error, workers)
	var wg sync.WaitGroup
	for index := 0; index < workers; index++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			<-start
			_, err := store.Credit(ctx, MutationRequest{
				TransactionID: fmt.Sprintf("credit-%d", index), UserID: "u1", IdempotencyKey: "concurrent-credit",
				OperationID: "operation-1", BatchID: "batch-1", JobID: "job-1", Points: 10,
				Breakdown: map[string]int64{"image": 10},
			})
			errorsByWorker <- err
		}(index)
	}
	close(start)
	wg.Wait()
	close(errorsByWorker)
	for err := range errorsByWorker {
		if err != nil {
			t.Fatalf("concurrent credit error = %v", err)
		}
	}
	account, err := store.GetAccount(ctx, "u1")
	if err != nil || account.Balance != 110 {
		t.Fatalf("account after concurrent credits = %#v, %v", account, err)
	}
}
