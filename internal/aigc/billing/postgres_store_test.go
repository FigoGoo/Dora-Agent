package billing

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	aigcconfig "github.com/FigoGoo/Dora-Agent/internal/aigc/config"
	aigcstorage "github.com/FigoGoo/Dora-Agent/internal/aigc/storage"
)

func TestPostgresStoreEnforcesIdempotencyIntegrity(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	db, err := aigcstorage.OpenAgentPostgres(ctx, aigcconfig.LoadFromEnv())
	if err != nil {
		t.Skipf("agent postgres unavailable: %v", err)
	}
	store := NewPostgresStore(db)
	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	userID := "billing-idempotency-user-" + suffix
	if _, err := store.EnsureAccount(ctx, userID, 100); err != nil {
		t.Fatal(err)
	}
	request := MutationRequest{
		TransactionID: "credit-" + suffix, UserID: userID, IdempotencyKey: "billing-idempotency-" + suffix,
		OperationID: "operation-1", BatchID: "batch-1", JobID: "job-1", Points: 10,
		Breakdown: map[string]int64{"image": 7, "video": 3},
	}
	first, err := store.Credit(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	retry := request
	retry.TransactionID = "retry-" + suffix
	replayed, err := store.Credit(ctx, retry)
	if err != nil || !replayed.Duplicate || replayed.Transaction.ID != first.Transaction.ID {
		t.Fatalf("replay = %#v, %v", replayed, err)
	}
	conflict := retry
	conflict.Points++
	if _, err := store.Credit(ctx, conflict); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("conflict error = %v", err)
	}
}

func TestPostgresStoreConcurrentIdempotentMutationCreditsOnce(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	db, err := aigcstorage.OpenAgentPostgres(ctx, aigcconfig.LoadFromEnv())
	if err != nil {
		t.Skipf("agent postgres unavailable: %v", err)
	}
	store := NewPostgresStore(db)
	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	userID := "billing-concurrent-user-" + suffix
	if _, err := store.EnsureAccount(ctx, userID, 100); err != nil {
		t.Fatal(err)
	}
	const workers = 8
	start := make(chan struct{})
	errorsByWorker := make(chan error, workers)
	var wg sync.WaitGroup
	for index := 0; index < workers; index++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			<-start
			_, err := store.Credit(ctx, MutationRequest{
				TransactionID: fmt.Sprintf("credit-%s-%d", suffix, index), UserID: userID,
				IdempotencyKey: "billing-concurrent-" + suffix, OperationID: "operation-1",
				BatchID: "batch-1", JobID: "job-1", Points: 10, Breakdown: map[string]int64{"image": 10},
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
	account, err := store.GetAccount(ctx, userID)
	if err != nil || account.Balance != 110 {
		t.Fatalf("account after concurrent credits = %#v, %v", account, err)
	}
}
