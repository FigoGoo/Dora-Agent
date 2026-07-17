package textmaterial

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

type serviceRepositoryStub struct {
	created TextMaterial
	result  CreateResult
	items   []TextMaterial
	err     error
}

func (stub *serviceRepositoryStub) CreateOrReplay(_ context.Context, material TextMaterial) (CreateResult, error) {
	stub.created = material
	if stub.err != nil {
		return CreateResult{}, stub.err
	}
	if stub.result.Material.AssetID == "" {
		return CreateResult{Material: material}, nil
	}
	return stub.result, nil
}

func (stub *serviceRepositoryStub) ListOwned(_ context.Context, _ ListQuery) ([]TextMaterial, error) {
	return stub.items, stub.err
}

type serviceClockStub struct{ now time.Time }

func (clock serviceClockStub) Now() time.Time { return clock.now }

type serviceIDStub struct{ id string }

func (generator serviceIDStub) New() (string, error) { return generator.id, nil }

func testUUIDv7(t *testing.T) string {
	t.Helper()
	id, err := uuid.NewV7()
	if err != nil {
		t.Fatal(err)
	}
	return id.String()
}

func TestServiceCreateUsesIdempotencyKeyAsAssetID(t *testing.T) {
	repository := &serviceRepositoryStub{}
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.FixedZone("offset", 8*60*60))
	evidenceID := testUUIDv7(t)
	service, err := NewService(repository, serviceClockStub{now: now}, serviceIDStub{id: evidenceID})
	if err != nil {
		t.Fatal(err)
	}
	key := testUUIDv7(t)
	result, err := service.Create(context.Background(), CreateCommand{
		OwnerUserID: testUUIDv7(t), ProjectID: testUUIDv7(t), IdempotencyKey: key, Content: "完整文本素材",
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if result.Material.AssetID != key || repository.created.EvidenceID != evidenceID ||
		repository.created.AssetVersion != 1 || repository.created.ContentDigest != ContentDigest("完整文本素材") ||
		!repository.created.CreatedAt.Equal(now.UTC()) || repository.created.CreatedAt.Location() != time.UTC {
		t.Fatalf("unexpected material: %+v", repository.created)
	}
}

func TestServiceCreateRejectsInvalidNFCAndBoundsBeforeRepository(t *testing.T) {
	repository := &serviceRepositoryStub{}
	service, err := NewService(repository, serviceClockStub{now: time.Now()}, serviceIDStub{id: testUUIDv7(t)})
	if err != nil {
		t.Fatal(err)
	}
	base := CreateCommand{OwnerUserID: testUUIDv7(t), ProjectID: testUUIDv7(t), IdempotencyKey: testUUIDv7(t)}
	for _, content := range []string{"", "   ", "e\u0301", strings.Repeat("文", MaxContentCharacters+1)} {
		_, err := service.Create(context.Background(), CreateCommand{
			OwnerUserID: base.OwnerUserID, ProjectID: base.ProjectID, IdempotencyKey: base.IdempotencyKey, Content: content,
		})
		if !errors.Is(err, ErrInvalidArgument) {
			t.Fatalf("content %q error = %v", content, err)
		}
	}
	if repository.created.AssetID != "" {
		t.Fatalf("invalid content reached repository: %+v", repository.created)
	}
}

func TestServiceListOwnedUsesFixedBound(t *testing.T) {
	repository := &serviceRepositoryStub{items: []TextMaterial{}}
	service, err := NewService(repository, serviceClockStub{now: time.Now()}, serviceIDStub{id: testUUIDv7(t)})
	if err != nil {
		t.Fatal(err)
	}
	items, err := service.ListOwned(context.Background(), testUUIDv7(t), testUUIDv7(t))
	if err != nil || len(items) != 0 {
		t.Fatalf("ListOwned() items=%v err=%v", items, err)
	}
}
