package session

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/FigoGoo/Dora-Agent/agent/internal/skill"
)

func userMessageRuntimeTestProfile() UserMessageRuntimeProfile {
	return UserMessageRuntimeProfile{
		Enabled: true, Profile: UserMessageRuntimeProfileV2Preview1,
		ContextSchema: UserMessageContextSchemaV2Preview1,
		PromptRef:     "user_message.direct_response@v1", PromptDigest: strings.Repeat("1", 64),
		ToolRegistryRef: "user_message.empty_tools@v1", ToolRegistryDigest: strings.Repeat("2", 64),
		RuntimePolicyRef: "user_message.runtime_policy@v1", RuntimePolicyDigest: strings.Repeat("3", 64),
		ModelRouteRef: "local.fake.user_message@v1", ModelRouteDigest: strings.Repeat("4", 64),
		BudgetRef: "user_message.single_model_call@v1", BudgetDigest: strings.Repeat("5", 64),
	}
}

func TestEnsureProjectSessionFreezesUserMessageRuntimePlan(t *testing.T) {
	repository := newMemoryRepository()
	promptProtector := &recordingProtector{}
	snapshotProtector := &recordingSkillSnapshotProtector{}
	service, err := NewServiceWithSkillSnapshotAndUserMessageRuntime(
		repository, &sequenceIDGenerator{}, fixedClock{now: time.Date(2026, 7, 17, 3, 0, 0, 0, time.UTC)},
		promptProtector, snapshotProtector, skill.DefaultLimitsProfileV1(), userMessageRuntimeTestProfile(),
	)
	if err != nil {
		t.Fatalf("创建 user_message runtime Session Service 失败: %v", err)
	}
	command := newTestCommand(t, testCommandID, "请帮我规划一个短视频")
	if _, err := service.EnsureProjectSession(context.Background(), command); err != nil {
		t.Fatalf("EnsureProjectSession 失败: %v", err)
	}
	plan := repository.createdPlan()
	if plan.UserMessageRuntime == nil {
		t.Fatal("非空 Prompt 未冻结 user_message runtime Turn/Context")
	}
	if !ValidUserMessageRuntimePlanForRepository(plan) {
		t.Fatal("冻结的 user_message runtime 计划未通过 Repository 复核")
	}
	contextValue := plan.UserMessageRuntime.Context
	if contextValue.MessageContentDigest != plan.Message.ContentDigest ||
		contextValue.SkillSnapshotDigest != plan.SkillSnapshot.Digest ||
		contextValue.ToolRegistryRef != "user_message.empty_tools@v1" ||
		contextValue.AccessScopeDigest != plan.Receipt.RequestDigest {
		t.Fatalf("最小 Context 未绑定原始事实: %+v", contextValue)
	}
	digest, err := DigestUserMessageContext(contextValue)
	if err != nil || digest != contextValue.ContextDigest {
		t.Fatalf("Context digest=%q recomputed=%q err=%v", contextValue.ContextDigest, digest, err)
	}
	turn := plan.UserMessageRuntime.Turn
	if turn.InputID != plan.Input.ID || turn.MessageID != plan.Message.ID || turn.Status != "created" ||
		turn.TurnID == "" || turn.OutputID == "" || turn.ModelCallID == "" ||
		turn.RecoveryEventID == "" || turn.TerminalEventID == "" {
		t.Fatalf("稳定 Turn 身份不完整: %+v", turn)
	}
}

func TestEnsureBlankPromptCreatesNoUserMessageRuntimeFacts(t *testing.T) {
	repository := newMemoryRepository()
	service, err := NewServiceWithSkillSnapshotAndUserMessageRuntime(
		repository, &sequenceIDGenerator{}, fixedClock{now: time.Date(2026, 7, 17, 3, 0, 0, 0, time.UTC)},
		&recordingProtector{}, &recordingSkillSnapshotProtector{}, skill.DefaultLimitsProfileV1(), userMessageRuntimeTestProfile(),
	)
	if err != nil {
		t.Fatalf("创建 user_message runtime Session Service 失败: %v", err)
	}
	command := newTestCommand(t, testCommandID, "   ")
	if _, err := service.EnsureProjectSession(context.Background(), command); err != nil {
		t.Fatalf("EnsureProjectSession 空 Prompt 失败: %v", err)
	}
	plan := repository.createdPlan()
	if plan.Message != nil || plan.Input != nil || plan.UserMessageRuntime != nil {
		t.Fatalf("空 Prompt 产生了运行时事实: %+v", plan.UserMessageRuntime)
	}
}

func TestEnsureProjectSessionWakesSharedRuntimeAfterCommit(t *testing.T) {
	repository := newMemoryRepository()
	service, err := NewServiceWithSkillSnapshotAndUserMessageRuntime(
		repository, &sequenceIDGenerator{}, fixedClock{now: time.Date(2026, 7, 17, 3, 0, 0, 0, time.UTC)},
		&recordingProtector{}, &recordingSkillSnapshotProtector{}, skill.DefaultLimitsProfileV1(), userMessageRuntimeTestProfile(),
	)
	if err != nil {
		t.Fatalf("创建 user_message runtime Session Service 失败: %v", err)
	}
	wakeCount := 0
	service, err = service.WithRuntimeWake(func() { wakeCount++ })
	if err != nil {
		t.Fatalf("注入共享 Runtime Wake 失败: %v", err)
	}
	if _, err := service.EnsureProjectSession(
		context.Background(), newTestCommand(t, testCommandID, "请继续"),
	); err != nil {
		t.Fatalf("EnsureProjectSession 失败: %v", err)
	}
	if wakeCount != 1 {
		t.Fatalf("共享 Runtime Wake 次数=%d，期望 1", wakeCount)
	}
}

func TestEnsureBlankPromptDoesNotWakeSharedRuntime(t *testing.T) {
	repository := newMemoryRepository()
	service, err := NewServiceWithSkillSnapshotAndUserMessageRuntime(
		repository, &sequenceIDGenerator{}, fixedClock{now: time.Date(2026, 7, 17, 3, 0, 0, 0, time.UTC)},
		&recordingProtector{}, &recordingSkillSnapshotProtector{}, skill.DefaultLimitsProfileV1(), userMessageRuntimeTestProfile(),
	)
	if err != nil {
		t.Fatalf("创建 user_message runtime Session Service 失败: %v", err)
	}
	wakeCount := 0
	service, err = service.WithRuntimeWake(func() { wakeCount++ })
	if err != nil {
		t.Fatalf("注入共享 Runtime Wake 失败: %v", err)
	}
	if _, err := service.EnsureProjectSession(
		context.Background(), newTestCommand(t, testCommandID, "   "),
	); err != nil {
		t.Fatalf("EnsureProjectSession 空 Prompt 失败: %v", err)
	}
	if wakeCount != 0 {
		t.Fatalf("空 Prompt 触发共享 Runtime Wake 次数=%d", wakeCount)
	}
}

func TestUserMessageRuntimeProfileRejectsToolRegistryExpansion(t *testing.T) {
	profile := userMessageRuntimeTestProfile()
	profile.ToolRegistryRef = "user_message.tools@v2"
	if err := profile.Validate(); err == nil {
		t.Fatal("Profile 接受了非空 Executable Tool Registry")
	}
}
