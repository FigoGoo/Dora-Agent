package localseed

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/FigoGoo/Dora-Agent/business/internal/user"
)

type seedRepository struct {
	record      user.AuthenticationRecord
	findErr     error
	createErr   error
	createCalls int
}

func (repository *seedRepository) FindAuthenticationRecord(context.Context, user.IdentityType, string) (user.AuthenticationRecord, error) {
	return repository.record, repository.findErr
}
func (repository *seedRepository) CreateAuthenticationRecord(_ context.Context, record user.AuthenticationRecord) error {
	repository.createCalls++
	repository.record = record
	return repository.createErr
}

type seedIDs struct{ values []string }

func (ids *seedIDs) New() (string, error) {
	value := ids.values[0]
	ids.values = ids.values[1:]
	return value, nil
}

type seedClock struct{ now time.Time }

func (clock seedClock) Now() time.Time { return clock.now }

type seedHasher struct{}

func (seedHasher) Hash(string, io.Reader) ([]byte, []byte, error) {
	return []byte("1234567890123456"), []byte("12345678901234567890123456789012"), nil
}

type seedVerifier struct{ matched bool }

func (verifier seedVerifier) Verify(string, user.PasswordCredential) (bool, error) {
	return verifier.matched, nil
}

func newSeederForTest(t *testing.T, repository *seedRepository, verifier seedVerifier) *Seeder {
	t.Helper()
	seeder, err := New(
		repository,
		&seedIDs{values: []string{
			"019f0000-0000-7000-8000-000000000011",
			"019f0000-0000-7000-8000-000000000012",
			"019f0000-0000-7000-8000-000000000013",
		}},
		seedClock{now: time.Date(2026, 7, 14, 7, 0, 0, 0, time.UTC)},
		bytes.NewReader(make([]byte, 64)), seedHasher{}, verifier,
		Config{Environment: "local", Email: " Smoke.User@Example.test ", Password: "local-password-123", DisplayName: "本地冒烟用户"},
	)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return seeder
}

func TestSeederCreatesNormalArgon2idAuthenticationRecord(t *testing.T) {
	repository := &seedRepository{findErr: user.ErrUserNotFound}
	seeder := newSeederForTest(t, repository, seedVerifier{matched: true})
	result, err := seeder.Ensure(context.Background())
	if err != nil {
		t.Fatalf("Ensure() error = %v", err)
	}
	if !result.Created || repository.createCalls != 1 || repository.record.Identity.NormalizedIdentifier != "smoke.user@example.test" ||
		repository.record.Credential.Algorithm != "argon2id" || repository.record.Credential.MemoryKiB != argonMemoryKiB {
		t.Fatalf("created fixture drifted: result=%+v record=%+v", result, repository.record)
	}
}

func TestSeederReplaysMatchingAccountAndRejectsPasswordConflict(t *testing.T) {
	repository := &seedRepository{}
	created := newSeederForTest(t, &seedRepository{findErr: user.ErrUserNotFound}, seedVerifier{matched: true})
	record, err := created.newRecord()
	if err != nil {
		t.Fatal(err)
	}
	repository.record = record
	result, err := newSeederForTest(t, repository, seedVerifier{matched: true}).Ensure(context.Background())
	if err != nil || result.Created || repository.createCalls != 0 {
		t.Fatalf("matching replay failed: result=%+v err=%v", result, err)
	}
	if _, err := newSeederForTest(t, repository, seedVerifier{matched: false}).Ensure(context.Background()); !errors.Is(err, ErrFixtureConflict) {
		t.Fatalf("password conflict error = %v", err)
	}
}

func TestSeederRejectsNonLocalEnvironmentBeforeDependencies(t *testing.T) {
	if _, err := New(nil, nil, nil, nil, nil, nil, Config{
		Environment: "production", Email: "smoke@example.test", Password: "local-password-123", DisplayName: "Smoke",
	}); !errors.Is(err, ErrLocalEnvironmentRequired) {
		t.Fatalf("New() error = %v", err)
	}
}
