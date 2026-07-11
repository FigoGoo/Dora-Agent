package artifact

import "context"

type Store interface {
	CreateRevision(ctx context.Context, revision Revision) (CreateResult, error)
	Get(ctx context.Context, id string) (Revision, error)
	GetByIdempotencyKey(ctx context.Context, key string) (Revision, error)
	GetLatest(ctx context.Context, sessionID, kind string) (Revision, error)
	ApplyReview(ctx context.Context, command ReviewCommand) (ReviewResult, error)
	GetReviewReceipt(ctx context.Context, idempotencyKey string) (ReviewCommandReceipt, error)
	Activate(ctx context.Context, id string, expectedVersion int) (Revision, error)
	Reject(ctx context.Context, id string, expectedVersion int) (Revision, error)
}
