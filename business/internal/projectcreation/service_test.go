package projectcreation

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/FigoGoo/Dora-Agent/business/internal/projectskillbinding"
)

type repositoryStub struct {
	command projectskillbinding.QuickCreateV2Command
	calls   int
	result  projectskillbinding.QuickCreateV2Result
	err     error
}

func (repository *repositoryStub) CreateQuickV2(
	_ context.Context,
	command projectskillbinding.QuickCreateV2Command,
	_ projectskillbinding.LimitsV1,
	_ projectskillbinding.OutboxPayloadProtectorV2,
) (projectskillbinding.QuickCreateV2Result, error) {
	repository.calls++
	repository.command = command
	return repository.result, repository.err
}

type clockStub struct{ now time.Time }

func (clock clockStub) Now() time.Time { return clock.now }

type idStub struct {
	values []string
	index  int
}

func (generator *idStub) New() (string, error) {
	if generator.index >= len(generator.values) {
		return "", errors.New("ID exhausted")
	}
	value := generator.values[generator.index]
	generator.index++
	return value, nil
}

type protectorStub struct{}

func (protectorStub) Protect(_ context.Context, _, _ []byte) (projectskillbinding.EncryptedEnvelopeV2, error) {
	return projectskillbinding.EncryptedEnvelopeV2{}, nil
}

func TestQuickCreateV2FreezesIDsAndSortsSkillSelection(t *testing.T) {
	ids := []string{
		"019f0000-0000-7000-8000-000000000001", "019f0000-0000-7000-8000-000000000002",
		"019f0000-0000-7000-8000-000000000003", "019f0000-0000-7000-8000-000000000004",
		"019f0000-0000-7000-8000-000000000005", "019f0000-0000-7000-8000-000000000006",
		"019f0000-0000-7000-8000-000000000007", "019f0000-0000-7000-8000-000000000008",
		"019f0000-0000-7000-8000-000000000009",
	}
	repository := &repositoryStub{result: projectskillbinding.QuickCreateV2Result{ProjectID: ids[0]}}
	service, err := NewService(
		repository, clockStub{now: time.Date(2026, 7, 14, 10, 0, 0, 0, time.UTC)},
		&idStub{values: ids}, protectorStub{}, projectskillbinding.DefaultLimitsV1(), 5, true,
	)
	if err != nil {
		t.Fatalf("创建服务失败: %v", err)
	}
	result, err := service.QuickCreateV2(context.Background(), QuickCreateV2Command{
		OwnerUserID: "019f0000-0000-7000-8000-0000000000aa", IdempotencyKey: "create-v2-1",
		InitialPrompt: " prompt ", EnabledSkillIDs: []string{
			"019f0000-0000-7000-8000-000000000102", "019f0000-0000-7000-8000-000000000101",
		},
	})
	if err != nil {
		t.Fatalf("QuickCreate v2 失败: %v", err)
	}
	if result.ProjectID != ids[0] || repository.calls != 1 || len(repository.command.Bindings) != 2 ||
		repository.command.Bindings[0].SkillID != "019f0000-0000-7000-8000-000000000101" ||
		repository.command.Bindings[1].SkillID != "019f0000-0000-7000-8000-000000000102" {
		t.Fatalf("v2 冻结结果漂移: result=%+v command=%+v", result, repository.command)
	}
}

func TestQuickCreateV2DisabledAndNilSelectionFailWithoutRepository(t *testing.T) {
	repository := &repositoryStub{}
	service, err := NewService(
		repository, clockStub{now: time.Now()}, &idStub{}, protectorStub{},
		projectskillbinding.DefaultLimitsV1(), 5, false,
	)
	if err != nil {
		t.Fatalf("创建关闭态服务失败: %v", err)
	}
	if _, err := service.QuickCreateV2(context.Background(), QuickCreateV2Command{EnabledSkillIDs: []string{}}); !errors.Is(err, ErrV2Disabled) {
		t.Fatalf("关闭态未失败关闭: %v", err)
	}
	if repository.calls != 0 {
		t.Fatal("关闭态调用了 Repository")
	}
	enabledService, err := NewService(
		repository, clockStub{now: time.Now()}, &idStub{}, protectorStub{},
		projectskillbinding.DefaultLimitsV1(), 5, true,
	)
	if err != nil {
		t.Fatalf("创建开启态服务失败: %v", err)
	}
	if _, err := enabledService.QuickCreateV2(context.Background(), QuickCreateV2Command{
		OwnerUserID: "019f0000-0000-7000-8000-0000000000aa", IdempotencyKey: "create-v2-2", EnabledSkillIDs: nil,
	}); !errors.Is(err, projectskillbinding.ErrInvalidBinding) {
		t.Fatalf("nil enabled_skill_ids 未失败关闭: %v", err)
	}
	if repository.calls != 0 {
		t.Fatal("nil enabled_skill_ids 调用了 Repository")
	}
}
