package billing

import "context"

type Store interface {
	Migrate(ctx context.Context) error
	EnsureAccount(ctx context.Context, userID string, initialBalance int64) (Account, error)
	GetAccount(ctx context.Context, userID string) (Account, error)
	Credit(ctx context.Context, request MutationRequest) (Result, error)
	Charge(ctx context.Context, request MutationRequest) (Result, error)
	Refund(ctx context.Context, request MutationRequest) (Result, error)
}

func CheckAvailablePoints(ctx context.Context, store Store, userID string, points int64) error {
	if store == nil {
		return nil
	}
	account, err := store.GetAccount(ctx, userID)
	if err != nil {
		return err
	}
	if points > account.Balance {
		return ErrInsufficientPoints
	}
	return nil
}
