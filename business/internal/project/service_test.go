package project

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

type serviceClock struct{ now time.Time }

func (clock serviceClock) Now() time.Time { return clock.now }

type serviceIDGenerator struct {
	values []string
	index  int
}

func (generator *serviceIDGenerator) New() (string, error) {
	if generator.index >= len(generator.values) {
		return "", errors.New("id generator exhausted")
	}
	value := generator.values[generator.index]
	generator.index++
	return value, nil
}

type serviceProtector struct {
	calls       int
	prompt      string
	digest      Digest
	returnedErr error
}

func (protector *serviceProtector) Protect(_ context.Context, normalizedPrompt string, promptDigest Digest) (*EncryptedPayload, error) {
	protector.calls++
	protector.prompt = normalizedPrompt
	protector.digest = promptDigest
	if protector.returnedErr != nil {
		return nil, protector.returnedErr
	}
	return &EncryptedPayload{
		Algorithm: PromptEncryptionAlgorithm, KeyVersion: "key-v1", Nonce: []byte("123456789012"),
		Ciphertext: []byte("ciphertext-with-authentication-tag"), PayloadDigest: promptDigest,
	}, nil
}

type serviceRepository struct {
	createCalls int
	aggregate   QuickCreateAggregate
	result      QuickCreateResult
	bootstrap   BootstrapResult
	err         error
}

func (repository *serviceRepository) CreateQuick(_ context.Context, aggregate QuickCreateAggregate) (QuickCreateResult, error) {
	repository.createCalls++
	repository.aggregate = aggregate
	return repository.result, repository.err
}

func (repository *serviceRepository) FindOwnedByID(_ context.Context, _ string, _ string) (Project, error) {
	return Project{}, repository.err
}

func (repository *serviceRepository) FindBootstrapOwnedByID(_ context.Context, _, _ string) (BootstrapResult, error) {
	return repository.bootstrap, repository.err
}

func newProjectServiceForTest(t *testing.T, repository *serviceRepository, protector *serviceProtector) *Service {
	t.Helper()
	values := make([]string, 4)
	for index := range values {
		value, err := uuid.NewV7()
		if err != nil {
			t.Fatalf("generate UUIDv7: %v", err)
		}
		values[index] = value.String()
	}
	service, err := NewService(repository, serviceClock{now: time.Date(2026, 7, 14, 1, 2, 3, 0, time.UTC)}, &serviceIDGenerator{values: values}, protector, 5)
	if err != nil {
		t.Fatalf("create project service: %v", err)
	}
	return service
}

func TestServiceQuickCreateProtectsNormalizedPromptAndPersistsAggregate(t *testing.T) {
	repository := &serviceRepository{result: QuickCreateResult{ProjectID: "result"}}
	protector := &serviceProtector{}
	service := newProjectServiceForTest(t, repository, protector)
	owner, err := uuid.NewV7()
	if err != nil {
		t.Fatal(err)
	}

	result, err := service.QuickCreate(context.Background(), QuickCreateCommand{
		OwnerUserID: owner.String(), IdempotencyKey: "intent-019f", InitialPrompt: " e\u0301 ",
	})
	if err != nil {
		t.Fatalf("quick create: %v", err)
	}
	if result.ProjectID != "result" || repository.createCalls != 1 || protector.calls != 1 || protector.prompt != " é " {
		t.Fatalf("unexpected quick create flow: result=%+v calls=%d protector=%+v", result, repository.createCalls, protector)
	}
	if repository.aggregate.Outbox.EncryptedPayload == nil || repository.aggregate.Outbox.EncryptedPayload.PayloadDigest != protector.digest {
		t.Fatalf("prompt protection metadata drifted: %+v", repository.aggregate.Outbox)
	}
}

func TestServiceQuickCreateBlankPromptSkipsProtector(t *testing.T) {
	repository := &serviceRepository{}
	protector := &serviceProtector{returnedErr: errors.New("must not be called")}
	service := newProjectServiceForTest(t, repository, protector)
	owner, _ := uuid.NewV7()

	if _, err := service.QuickCreate(context.Background(), QuickCreateCommand{
		OwnerUserID: owner.String(), IdempotencyKey: "intent-empty", InitialPrompt: "\t\u3000\n",
	}); err != nil {
		t.Fatalf("quick create blank prompt: %v", err)
	}
	if protector.calls != 0 || repository.aggregate.Outbox.HasInitialPrompt || repository.aggregate.Outbox.EncryptedPayload != nil {
		t.Fatalf("blank prompt created protected payload: calls=%d outbox=%+v", protector.calls, repository.aggregate.Outbox)
	}
}

func TestServiceQuickCreateRejectsInvalidKeyBeforeSensitiveWork(t *testing.T) {
	repository := &serviceRepository{}
	protector := &serviceProtector{}
	service := newProjectServiceForTest(t, repository, protector)
	owner, _ := uuid.NewV7()

	for _, key := range []string{"", " leading", "contains space", "换行"} {
		if _, err := service.QuickCreate(context.Background(), QuickCreateCommand{OwnerUserID: owner.String(), IdempotencyKey: key, InitialPrompt: "secret"}); !errors.Is(err, ErrInvalidIdempotencyKey) {
			t.Fatalf("key %q: expected invalid key, got %v", key, err)
		}
	}
	if protector.calls != 0 || repository.createCalls != 0 {
		t.Fatalf("invalid key reached sensitive work: protector=%d repository=%d", protector.calls, repository.createCalls)
	}
}

func TestServiceQuickCreateMapsProtectorErrorsWithoutLeakingCause(t *testing.T) {
	repository := &serviceRepository{}
	protector := &serviceProtector{returnedErr: errors.New("kms https://secret.local key=raw-secret")}
	service := newProjectServiceForTest(t, repository, protector)
	owner, _ := uuid.NewV7()

	_, err := service.QuickCreate(context.Background(), QuickCreateCommand{
		OwnerUserID: owner.String(), IdempotencyKey: "intent-secret", InitialPrompt: "secret prompt",
	})
	if !errors.Is(err, ErrPromptProtection) || err.Error() != ErrPromptProtection.Error() {
		t.Fatalf("expected stable protection error, got %v", err)
	}
	if repository.createCalls != 0 {
		t.Fatal("protection failure reached repository")
	}
}

func TestBootstrapCreationStatus(t *testing.T) {
	for status, expected := range map[ProvisioningStatus]string{
		ProvisioningStatusPending: "provisioning", ProvisioningStatusReconciling: "provisioning",
		ProvisioningStatusReady: "ready", ProvisioningStatusBlocked: "failed",
	} {
		if actual := (BootstrapResult{ProvisioningStatus: status}).CreationStatus(); actual != expected {
			t.Fatalf("status %q: expected %q, got %q", status, expected, actual)
		}
	}
}
