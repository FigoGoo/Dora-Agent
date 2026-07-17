package plancreationspec

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"sort"
	"testing"
	"time"

	"github.com/FigoGoo/Dora-Agent/agent/internal/turncontext"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

const (
	recoveryTestRequestID       = "019f68e8-2001-7000-8000-000000000001"
	recoveryTestUserID          = "019f68e8-2002-7000-8000-000000000002"
	recoveryTestProjectID       = "019f68e8-2003-7000-8000-000000000003"
	recoveryTestSessionID       = "019f68e8-2004-7000-8000-000000000004"
	recoveryTestInputID         = "019f68e8-2005-7000-8000-000000000005"
	recoveryTestTurnID          = "019f68e8-2006-7000-8000-000000000006"
	recoveryTestRunID           = "019f68e8-2007-7000-8000-000000000007"
	recoveryTestToolCallID      = "019f68e8-2008-7000-8000-000000000008"
	recoveryTestCommandID       = "019f68e8-2009-7000-8000-000000000009"
	recoveryTestCreationSpecID  = "019f68e8-2010-7000-8000-000000000010"
	recoveryTestRequestDigest   = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	recoveryTestIntentJSON      = `{"schema_version":"plan_creation_spec.preview.intent.v1","goal":"制作一支品牌发布短片","deliverable_type":"video","audience":"潜在客户","locale":"zh-CN","constraints":["时长不超过六十秒"]}`
	recoveryTestTerminalSummary = "确定失败摘要"
)

// TestGraphNodeKeysExactSetAndCompile 验证开发预览 Graph 只注册已冻结的九个稳定 Node，并能在启动阶段完成 DAG Compile。
func TestGraphNodeKeysExactSetAndCompile(t *testing.T) {
	want := []string{
		"validate_intent", "load_context", "render_prompt", "call_model", "validate_proposal",
		"save_draft", "query_save_receipt", "build_result", "defer_recovery",
	}
	if got := NodeKeys(); !reflect.DeepEqual(got, want) {
		t.Fatalf("Graph Node exact-set=%v want=%v", got, want)
	}
	business := &recoveryTestBusiness{}
	store := &recoveryTestStore{}
	model := &recoveryTestModel{proposal: recoveryTestProposal()}
	graph, err := Compile(context.Background(), model, business, store, recoveryTestClock{
		now: time.Date(2026, 7, 16, 12, 0, 0, 0, time.UTC),
	})
	if err != nil || graph == nil || graph.runnable == nil {
		t.Fatalf("启动阶段 Graph Compile 失败: graph=%+v err=%v", graph, err)
	}
}

// TestGraphPreservesRequiredEmptyConstraintArray 锁定表单允许的空约束必须贯穿 Proposal、Draft 与完成 Card，不能退化为 null。
func TestGraphPreservesRequiredEmptyConstraintArray(t *testing.T) {
	audience := "本地 MVP 验收用户"
	intentJSON := `{"schema_version":"plan_creation_spec.preview.intent.v1","goal":"Dora 基本功能一键验收 1784250000000","deliverable_type":"video","audience":"本地 MVP 验收用户","locale":"zh-CN","constraints":[]}`
	proposal := Proposal{
		SchemaVersion: ProposalSchemaVersion, Title: "视频创作规格", Goal: "Dora 基本功能一键验收 1784250000000",
		DeliverableType: "video", Audience: audience,
		Phases:      []Phase{{Key: "phase_1", Title: "创作规划", Objective: "冻结目标、结构与交付边界", Output: "可执行创作规格"}},
		Constraints: []string{}, AcceptanceCriteria: []string{"交付结果符合已冻结目标、类型和全部硬约束"},
	}
	business := &recoveryTestBusiness{context: DomainContext{
		ProjectID: recoveryTestProjectID, ProjectVersion: 1, ProjectTitle: "Dora 基本功能一键验收项目",
	}}
	business.save = func(command DraftCommand) (SaveDisposition, Resource, error) {
		if command.Content.Constraints == nil || len(command.Content.Constraints) != 0 {
			t.Fatalf("空约束在 Draft Command 中退化: %#v", command.Content.Constraints)
		}
		digest, err := ContentDigest(command.Content)
		if err != nil {
			t.Fatalf("计算空约束 Draft 摘要失败: %v", err)
		}
		return SaveDispositionCreated, recoveryTestResource(command.Content, digest), nil
	}
	store := &recoveryTestStore{}
	tool := newRecoveryTestTool(t, &recoveryTestModel{proposal: proposal}, business, store)
	_, err := tool.InvokableRun(recoveryTestContext(), intentJSON)
	if err != nil {
		t.Fatalf("空约束一键验收 Graph 执行失败: %v", err)
	}
	if store.terminal == nil || store.terminal.Card == nil || store.terminal.Card.Constraints == nil ||
		len(store.terminal.Card.Constraints) != 0 {
		t.Fatalf("完成 Card 未保留必填空数组: %+v", store.terminal)
	}
}

// TestToolRecoveryQueriesOnlyDurableCommand 验证 prepared/business_unknown 两种持久阶段都只查询原命令，不读取 Context、不调模型也不重发 Save。
func TestToolRecoveryQueriesOnlyDurableCommand(t *testing.T) {
	for _, stage := range []string{"business_prepared", "business_unknown"} {
		t.Run(stage, func(t *testing.T) {
			content := recoveryTestContent()
			contentDigest, err := ContentDigest(content)
			if err != nil {
				t.Fatalf("计算恢复 Content 摘要失败: %v", err)
			}
			resource := recoveryTestResource(content, contentDigest)
			business := &recoveryTestBusiness{
				query: func(call int, command DraftCommand) (string, *Resource, error) {
					if call != 1 || command.TrustedContext.BusinessCommandID != recoveryTestCommandID ||
						command.RequestDigest != recoveryTestRequestDigest {
						t.Fatalf("恢复查询未复用原命令: call=%d command=%+v", call, command)
					}
					return "completed", &resource, nil
				},
			}
			store := &recoveryTestStore{stage: stage, recovery: &RecoveryDeferred{
				ToolCallID: recoveryTestToolCallID, BusinessCommandID: recoveryTestCommandID,
				RequestDigest: recoveryTestRequestDigest, ContentDigest: contentDigest,
				Command: recoveryTestDurableCommand(recoveryTestRequestDigest),
			}}
			proposalModel := &recoveryTestModel{proposal: recoveryTestProposal()}
			tool := newRecoveryTestTool(t, proposalModel, business, store)

			encoded, err := tool.InvokableRun(recoveryTestContext(), recoveryTestIntentJSON)
			if err != nil {
				t.Fatalf("恢复 Tool 调用失败: %v", err)
			}
			assertRecoveryTestJSONKeys(t, []byte(encoded), "receipt_ref", "resource_ref", "result_code", "status")
			if business.getCalls != 0 || business.saveCalls != 0 || business.queryCalls != 1 ||
				proposalModel.generateCalls != 0 || store.prepareCalls != 0 || store.markCalls != 0 || store.freezeCalls != 1 {
				t.Fatalf("%s 恢复发生重复副作用: business(get=%d save=%d query=%d) model=%d store(prepare=%d mark=%d freeze=%d)",
					stage, business.getCalls, business.saveCalls, business.queryCalls, proposalModel.generateCalls,
					store.prepareCalls, store.markCalls, store.freezeCalls)
			}
		})
	}
}

// TestToolUnknownSaveRecoversWithoutRepeatingSideEffects 验证首次 Save unknown 后仅用原摘要查询；Project 版本变化也不得重跑 Context、模型或 Save。
func TestToolUnknownSaveRecoversWithoutRepeatingSideEffects(t *testing.T) {
	content := recoveryTestContent()
	contentDigest, err := ContentDigest(content)
	if err != nil {
		t.Fatalf("计算恢复 Content 摘要失败: %v", err)
	}
	resource := recoveryTestResource(content, contentDigest)
	business := &recoveryTestBusiness{
		context: DomainContext{ProjectID: recoveryTestProjectID, ProjectVersion: 1, ProjectTitle: "品牌发布项目"},
		save: func(command DraftCommand) (SaveDisposition, Resource, error) {
			if command.Content.Title != content.Title || command.DomainContext.ProjectVersion != 1 {
				t.Fatalf("首次 Save Command 非预期: %+v", command)
			}
			return "", Resource{}, ErrBusinessUnknownOutcome
		},
		query: func(call int, command DraftCommand) (string, *Resource, error) {
			switch call {
			case 1:
				return "not_found", nil, nil
			case 2:
				if command.RequestDigest == "" || command.TrustedContext.BusinessCommandID != recoveryTestCommandID {
					t.Fatalf("恢复查询丢失原命令身份: %+v", command)
				}
				return "completed", &resource, nil
			default:
				t.Fatalf("意外的 Business Query 次数: %d", call)
				return "", nil, errors.New("unreachable")
			}
		},
	}
	store := &recoveryTestStore{}
	proposalModel := &recoveryTestModel{proposal: recoveryTestProposal()}
	tool := newRecoveryTestTool(t, proposalModel, business, store)

	if _, err := tool.InvokableRun(recoveryTestContext(), recoveryTestIntentJSON); !errors.Is(err, ErrBusinessUnknownOutcome) {
		t.Fatalf("首次 unknown Save 错误=%v want ErrBusinessUnknownOutcome", err)
	}
	if store.recovery == nil || store.terminal != nil {
		t.Fatalf("unknown Save 未保持开放恢复阶段: recovery=%+v terminal=%+v", store.recovery, store.terminal)
	}
	// 若恢复错误地重跑 load_context，这个版本变化会使后续 Save 摘要和业务语义漂移。
	business.context.ProjectVersion = 2
	completedJSON, err := tool.InvokableRun(recoveryTestContext(), recoveryTestIntentJSON)
	if err != nil {
		t.Fatalf("第二次 Query-only 恢复失败: %v", err)
	}
	replayedJSON, err := tool.InvokableRun(recoveryTestContext(), recoveryTestIntentJSON)
	if err != nil || replayedJSON != completedJSON {
		t.Fatalf("冻结终态未原样重放: replay=%s completed=%s err=%v", replayedJSON, completedJSON, err)
	}
	if business.getCalls != 1 || business.saveCalls != 1 || business.queryCalls != 2 || proposalModel.generateCalls != 1 {
		t.Fatalf("unknown 恢复重复执行: get=%d save=%d query=%d model=%d",
			business.getCalls, business.saveCalls, business.queryCalls, proposalModel.generateCalls)
	}
	if store.prepareCalls != 1 || store.markCalls != 1 || store.freezeCalls != 1 {
		t.Fatalf("恢复回执写点次数异常: prepare=%d mark=%d freeze=%d", store.prepareCalls, store.markCalls, store.freezeCalls)
	}
}

// TestToolAuthoritativeNotFoundResendsSameDurableCommand 验证 Save 请求未送达时只在持久化预算内重发同键同摘要命令。
func TestToolAuthoritativeNotFoundResendsSameDurableCommand(t *testing.T) {
	content := recoveryTestContent()
	contentDigest, err := ContentDigest(content)
	if err != nil {
		t.Fatalf("计算测试 Content 摘要失败: %v", err)
	}
	resource := recoveryTestResource(content, contentDigest)
	var firstCommand DraftCommand
	saveAttempt := 0
	business := &recoveryTestBusiness{
		context: DomainContext{ProjectID: recoveryTestProjectID, ProjectVersion: 1, ProjectTitle: "品牌发布项目"},
		save: func(command DraftCommand) (SaveDisposition, Resource, error) {
			saveAttempt++
			if saveAttempt == 1 {
				firstCommand = command
				return "", Resource{}, ErrBusinessUnknownOutcome
			}
			if command.TrustedContext.BusinessCommandID != firstCommand.TrustedContext.BusinessCommandID ||
				command.RequestDigest != firstCommand.RequestDigest ||
				command.TrustedContext.ToolCallID != firstCommand.TrustedContext.ToolCallID {
				t.Fatalf("同键重发发生身份漂移: first=%+v resent=%+v", firstCommand, command)
			}
			return SaveDispositionCreated, resource, nil
		},
		query: func(call int, command DraftCommand) (string, *Resource, error) {
			if call != 1 || command.TrustedContext.BusinessCommandID != firstCommand.TrustedContext.BusinessCommandID ||
				command.RequestDigest != firstCommand.RequestDigest {
				t.Fatalf("not_found 查询未绑定原命令: call=%d command=%+v", call, command)
			}
			return "not_found", nil, nil
		},
	}
	store := &recoveryTestStore{reserve: func(recovery RecoveryDeferred) (RecoveryDeferred, bool, error) {
		if recovery.Command.TrustedContext.BusinessCommandID != firstCommand.TrustedContext.BusinessCommandID ||
			recovery.Command.RequestDigest != firstCommand.RequestDigest {
			t.Fatalf("预算预留未绑定完整原命令: %+v", recovery)
		}
		recovery.ResendAttempts = 1
		recovery.ResendLimit = 3
		return recovery, true, nil
	}}
	tool := newRecoveryTestTool(t, &recoveryTestModel{proposal: recoveryTestProposal()}, business, store)

	encoded, err := tool.InvokableRun(recoveryTestContext(), recoveryTestIntentJSON)
	if err != nil || encoded == "" {
		t.Fatalf("同键有界重发未收敛: encoded=%s err=%v", encoded, err)
	}
	if business.saveCalls != 2 || business.queryCalls != 1 || store.reserveCalls != 1 ||
		store.prepareCalls != 1 || store.freezeCalls != 1 || store.markCalls != 0 {
		t.Fatalf("重发调用计数异常: business=%+v store=%+v", business, store)
	}
}

// TestToolExhaustedBudgetQueryFailureRemainsQueryOnly 验证预算已用尽但 Query 技术失败时不得误标 exhausted 或重发；后续 completed 仍可收敛。
func TestToolExhaustedBudgetQueryFailureRemainsQueryOnly(t *testing.T) {
	content := recoveryTestContent()
	contentDigest, err := ContentDigest(content)
	if err != nil {
		t.Fatalf("计算恢复 Content 摘要失败: %v", err)
	}
	resource := recoveryTestResource(content, contentDigest)
	recovery := &RecoveryDeferred{
		ToolCallID: recoveryTestToolCallID, BusinessCommandID: recoveryTestCommandID,
		RequestDigest: recoveryTestRequestDigest, ContentDigest: contentDigest,
		Command: recoveryTestDurableCommand(recoveryTestRequestDigest), ResendAttempts: 3, ResendLimit: 3,
	}
	store := &recoveryTestStore{stage: "business_unknown", recovery: recovery}
	business := &recoveryTestBusiness{query: func(call int, _ DraftCommand) (string, *Resource, error) {
		if call == 1 {
			return "", nil, ErrBusinessTechnical
		}
		return "completed", &resource, nil
	}}
	model := &recoveryTestModel{proposal: recoveryTestProposal()}
	tool := newRecoveryTestTool(t, model, business, store)

	if _, err := tool.InvokableRun(recoveryTestContext(), recoveryTestIntentJSON); !errors.Is(err, ErrBusinessUnknownOutcome) {
		t.Fatalf("Query 技术失败未保留恢复: %v", err)
	}
	if store.recovery == nil || store.recovery.ResendExhausted || store.reserveCalls != 0 ||
		business.saveCalls != 0 || store.markCalls != 1 {
		t.Fatalf("Query 技术失败错误消耗/耗尽重发预算: store=%+v business=%+v", store, business)
	}
	if _, err := tool.InvokableRun(recoveryTestContext(), recoveryTestIntentJSON); err != nil {
		t.Fatalf("后续权威 completed 未收敛: %v", err)
	}
	if business.queryCalls != 2 || business.saveCalls != 0 || model.generateCalls != 0 || store.freezeCalls != 1 {
		t.Fatalf("Query-only 恢复发生额外副作用: business=%+v model=%+v store=%+v", business, model, store)
	}
}

// TestToolLastResendNotFoundMarksObservableExhaustion 验证最后一次同键重发后的权威 not_found 经 finishOutcome 持久化耗尽标记。
func TestToolLastResendNotFoundMarksObservableExhaustion(t *testing.T) {
	content := recoveryTestContent()
	contentDigest, err := ContentDigest(content)
	if err != nil {
		t.Fatalf("计算恢复 Content 摘要失败: %v", err)
	}
	recovery := &RecoveryDeferred{
		ToolCallID: recoveryTestToolCallID, BusinessCommandID: recoveryTestCommandID,
		RequestDigest: recoveryTestRequestDigest, ContentDigest: contentDigest,
		Command: recoveryTestDurableCommand(recoveryTestRequestDigest), ResendAttempts: 0, ResendLimit: 1,
	}
	store := &recoveryTestStore{stage: "business_unknown", recovery: recovery, reserve: func(value RecoveryDeferred) (RecoveryDeferred, bool, error) {
		value.ResendAttempts = 1
		value.ResendLimit = 1
		return value, true, nil
	}}
	business := &recoveryTestBusiness{
		save: func(DraftCommand) (SaveDisposition, Resource, error) {
			return "", Resource{}, ErrBusinessUnknownOutcome
		},
		query: func(int, DraftCommand) (string, *Resource, error) { return "not_found", nil, nil },
	}
	tool := newRecoveryTestTool(t, &recoveryTestModel{proposal: recoveryTestProposal()}, business, store)

	if _, err := tool.InvokableRun(recoveryTestContext(), recoveryTestIntentJSON); !errors.Is(err, ErrBusinessUnknownOutcome) {
		t.Fatalf("最后重发 not_found 未保留恢复: %v", err)
	}
	if store.recovery == nil || !store.recovery.ResendExhausted || store.recovery.ResendAttempts != 1 ||
		store.reserveCalls != 1 || store.markCalls != 1 || business.saveCalls != 1 || business.queryCalls != 2 {
		t.Fatalf("重发耗尽未被全链持久化: store=%+v business=%+v", store, business)
	}
}

// TestToolPostPrepareUntrustedResponsesRemainRecovery 验证 Save 已越过 prepared 边界后，非权威错误或不可信 Save/Query 响应都不得冻结 failed。
func TestToolPostPrepareUntrustedResponsesRemainRecovery(t *testing.T) {
	content := recoveryTestContent()
	contentDigest, err := ContentDigest(content)
	if err != nil {
		t.Fatalf("计算测试 Content 摘要失败: %v", err)
	}
	validResource := recoveryTestResource(content, contentDigest)
	invalidResource := validResource
	invalidResource.ContentDigest = recoveryTestRequestDigest
	tests := []struct {
		name  string
		save  func(DraftCommand) (SaveDisposition, Resource, error)
		query func(int, DraftCommand) (string, *Resource, error)
	}{
		{
			name: "save technical error",
			save: func(DraftCommand) (SaveDisposition, Resource, error) {
				return "", Resource{}, ErrBusinessTechnical
			},
		},
		{
			name: "save invalid disposition",
			save: func(DraftCommand) (SaveDisposition, Resource, error) {
				return SaveDisposition("future"), validResource, nil
			},
		},
		{
			name: "save invalid resource",
			save: func(DraftCommand) (SaveDisposition, Resource, error) {
				return SaveDispositionCreated, invalidResource, nil
			},
		},
		{
			name: "query technical error",
			save: func(DraftCommand) (SaveDisposition, Resource, error) {
				return "", Resource{}, ErrBusinessUnknownOutcome
			},
			query: func(int, DraftCommand) (string, *Resource, error) {
				return "", nil, ErrBusinessTechnical
			},
		},
		{
			name: "query unknown status",
			save: func(DraftCommand) (SaveDisposition, Resource, error) {
				return "", Resource{}, ErrBusinessUnknownOutcome
			},
			query: func(int, DraftCommand) (string, *Resource, error) {
				return "future", nil, nil
			},
		},
		{
			name: "query completed invalid resource",
			save: func(DraftCommand) (SaveDisposition, Resource, error) {
				return "", Resource{}, ErrBusinessUnknownOutcome
			},
			query: func(int, DraftCommand) (string, *Resource, error) {
				return "completed", &invalidResource, nil
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			query := test.query
			if query == nil {
				query = func(int, DraftCommand) (string, *Resource, error) { return "not_found", nil, nil }
			}
			business := &recoveryTestBusiness{
				context: DomainContext{ProjectID: recoveryTestProjectID, ProjectVersion: 1, ProjectTitle: "品牌发布项目"},
				save:    test.save, query: query,
			}
			store := &recoveryTestStore{}
			proposalModel := &recoveryTestModel{proposal: recoveryTestProposal()}
			tool := newRecoveryTestTool(t, proposalModel, business, store)

			_, err := tool.InvokableRun(recoveryTestContext(), recoveryTestIntentJSON)
			if !errors.Is(err, ErrBusinessUnknownOutcome) {
				t.Fatalf("不可信副作用响应错误=%v want recovery", err)
			}
			if business.saveCalls != 1 || business.queryCalls != 1 || proposalModel.generateCalls != 1 ||
				store.prepareCalls != 1 || store.markCalls != 1 || store.freezeCalls != 0 || store.recovery == nil {
				t.Fatalf("不可信副作用响应被错误冻结: business(save=%d query=%d) model=%d store=%+v",
					business.saveCalls, business.queryCalls, proposalModel.generateCalls, store)
			}
		})
	}
}

// TestSaveAndBuildLocalStateFailuresRemainRecovery 验证 Save 成功后的 State 写入或 Result 本地构造失败都返回原命令恢复标记。
func TestSaveAndBuildLocalStateFailuresRemainRecovery(t *testing.T) {
	command := recoveryTestCommand(t)
	contentDigest, err := ContentDigest(command.Content)
	if err != nil {
		t.Fatalf("计算测试 Content 摘要失败: %v", err)
	}
	resource := recoveryTestResource(command.Content, contentDigest)

	t.Run("save ProcessState failure", func(t *testing.T) {
		business := &recoveryTestBusiness{save: func(DraftCommand) (SaveDisposition, Resource, error) {
			return SaveDispositionCreated, resource, nil
		}}
		store := &recoveryTestStore{}
		builder := &graphBuilder{business: business, journal: store, clock: recoveryTestClock{now: processorIndependentNow()}}
		outcome, err := builder.saveDraft(context.Background(), command)
		if err != nil || outcome.Status != "unknown" || outcome.Command.RequestDigest != command.RequestDigest ||
			business.saveCalls != 1 || store.prepareCalls != 1 {
			t.Fatalf("Save 后 State 失败未转恢复: outcome=%+v err=%v business=%+v store=%+v", outcome, err, business, store)
		}
	})

	for _, test := range []struct {
		name  string
		clock Clock
	}{
		{name: "zero clock", clock: recoveryTestClock{}},
		{name: "ProcessState failure", clock: recoveryTestClock{now: processorIndependentNow()}},
	} {
		t.Run("build result "+test.name, func(t *testing.T) {
			builder := &graphBuilder{clock: test.clock}
			outcome, err := builder.buildResult(context.Background(), SaveOutcome{
				Status: "saved", Resource: &resource, Command: command,
			})
			if err != nil || outcome.Recovery == nil || outcome.Terminal != nil ||
				outcome.Recovery.BusinessCommandID != recoveryTestCommandID {
				t.Fatalf("本地 buildResult 失败未保留恢复: outcome=%+v err=%v", outcome, err)
			}
		})
	}
}

// TestRecoverUntrustedQueryResponseRemainsRecovery 验证恢复查询的错误、未知状态、不可信资源或本地构造失败都继续同一 durable Recovery。
func TestRecoverUntrustedQueryResponseRemainsRecovery(t *testing.T) {
	content := recoveryTestContent()
	contentDigest, err := ContentDigest(content)
	if err != nil {
		t.Fatalf("计算恢复 Content 摘要失败: %v", err)
	}
	validResource := recoveryTestResource(content, contentDigest)
	otherContent := content
	otherContent.Title = "另一份自洽但非原命令草稿"
	otherDigest, err := ContentDigest(otherContent)
	if err != nil {
		t.Fatalf("计算异义资源摘要失败: %v", err)
	}
	digestMismatchResource := recoveryTestResource(otherContent, otherDigest)
	tests := []struct {
		name  string
		clock Clock
		query func(int, DraftCommand) (string, *Resource, error)
	}{
		{
			name: "technical error", clock: recoveryTestClock{now: processorIndependentNow()},
			query: func(int, DraftCommand) (string, *Resource, error) { return "", nil, ErrBusinessTechnical },
		},
		{
			name: "unknown status", clock: recoveryTestClock{now: processorIndependentNow()},
			query: func(int, DraftCommand) (string, *Resource, error) { return "future", nil, nil },
		},
		{
			name: "completed nil resource", clock: recoveryTestClock{now: processorIndependentNow()},
			query: func(int, DraftCommand) (string, *Resource, error) { return "completed", nil, nil },
		},
		{
			name: "completed digest mismatch", clock: recoveryTestClock{now: processorIndependentNow()},
			query: func(int, DraftCommand) (string, *Resource, error) {
				return "completed", &digestMismatchResource, nil
			},
		},
		{
			name: "completed zero clock", clock: recoveryTestClock{},
			query: func(int, DraftCommand) (string, *Resource, error) { return "completed", &validResource, nil },
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			business := &recoveryTestBusiness{query: test.query}
			store := &recoveryTestStore{stage: "business_unknown", recovery: &RecoveryDeferred{
				ToolCallID: recoveryTestToolCallID, BusinessCommandID: recoveryTestCommandID,
				RequestDigest: recoveryTestRequestDigest, ContentDigest: contentDigest,
				Command: recoveryTestDurableCommand(recoveryTestRequestDigest),
			}}
			proposalModel := &recoveryTestModel{proposal: recoveryTestProposal()}
			tool := newRecoveryTestToolWithClock(t, proposalModel, business, store, test.clock)

			_, err := tool.InvokableRun(recoveryTestContext(), recoveryTestIntentJSON)
			if !errors.Is(err, ErrBusinessUnknownOutcome) || business.queryCalls != 1 ||
				business.getCalls != 0 || business.saveCalls != 0 || proposalModel.generateCalls != 0 ||
				store.freezeCalls != 0 || store.markCalls != 1 || store.recovery == nil {
				t.Fatalf("不可信恢复查询被错误收口: err=%v business=%+v model=%+v store=%+v",
					err, business, proposalModel, store)
			}
		})
	}
}

// TestResultMarshalJSONExactUnion 验证 completed/failed 终态按 status 输出精确判别联合，内部投影字段不能泄漏。
func TestResultMarshalJSONExactUnion(t *testing.T) {
	completed := Result{
		Status: "completed", ResultCode: ResultCodeCreated,
		ResourceRef: &ResourceRef{ID: recoveryTestCreationSpecID, Version: 1, Digest: recoveryTestRequestDigest, Status: "draft"},
		ReceiptRef:  ReceiptRef{ToolCallID: recoveryTestToolCallID, BusinessCommandID: recoveryTestCommandID},
		Summary:     "must be omitted", Retryable: true,
		Card: &Card{SchemaVersion: CardSchemaVersion}, BusinessRequestDigest: recoveryTestRequestDigest,
	}
	encoded, err := json.Marshal(completed)
	if err != nil {
		t.Fatalf("编码 completed Result 失败: %v", err)
	}
	assertRecoveryTestJSONKeys(t, encoded, "receipt_ref", "resource_ref", "result_code", "status")

	failed := Result{
		Status: "failed", ResultCode: "CREATION_SPEC_PREVIEW_INVALID",
		ResourceRef: &ResourceRef{ID: recoveryTestCreationSpecID},
		ReceiptRef:  ReceiptRef{ToolCallID: recoveryTestToolCallID, BusinessCommandID: recoveryTestCommandID},
		Summary:     recoveryTestTerminalSummary, Retryable: false,
		Card: &Card{SchemaVersion: CardSchemaVersion}, BusinessRequestDigest: recoveryTestRequestDigest,
	}
	encoded, err = json.Marshal(failed)
	if err != nil {
		t.Fatalf("编码 failed Result 失败: %v", err)
	}
	assertRecoveryTestJSONKeys(t, encoded, "receipt_ref", "result_code", "retryable", "status", "summary")

	if _, err := json.Marshal(Result{Status: "recovery_pending"}); err == nil {
		t.Fatal("recovery_pending 被错误编码为可冻结 Tool Result")
	}
}

type recoveryTestBusiness struct {
	context    DomainContext
	getCalls   int
	saveCalls  int
	queryCalls int
	save       func(DraftCommand) (SaveDisposition, Resource, error)
	query      func(int, DraftCommand) (string, *Resource, error)
}

func (b *recoveryTestBusiness) GetCreationSpecContext(context.Context, string, string, string) (DomainContext, error) {
	b.getCalls++
	return b.context, nil
}

func (b *recoveryTestBusiness) SaveCreationSpecDraft(_ context.Context, command DraftCommand) (SaveDisposition, Resource, error) {
	b.saveCalls++
	if b.save == nil {
		return "", Resource{}, errors.New("unexpected SaveCreationSpecDraft")
	}
	return b.save(command)
}

func (b *recoveryTestBusiness) QueryCreationSpecDraftCommand(_ context.Context, command DraftCommand) (string, *Resource, error) {
	b.queryCalls++
	if b.query == nil {
		return "", nil, errors.New("unexpected QueryCreationSpecDraftCommand")
	}
	return b.query(b.queryCalls, command)
}

type recoveryTestStore struct {
	stage        string
	terminal     *Result
	recovery     *RecoveryDeferred
	prepareCalls int
	reserveCalls int
	freezeCalls  int
	markCalls    int
	reserve      func(RecoveryDeferred) (RecoveryDeferred, bool, error)
}

func (s *recoveryTestStore) ReplayTerminal(context.Context, TrustedContext) (*Result, error) {
	if s.terminal == nil {
		return nil, nil
	}
	copy := *s.terminal
	return &copy, nil
}

func (s *recoveryTestStore) ReplayRecovery(context.Context, TrustedContext) (*RecoveryDeferred, error) {
	if s.recovery == nil {
		return nil, nil
	}
	copy := *s.recovery
	return &copy, nil
}

func (s *recoveryTestStore) PrepareCommand(_ context.Context, command DraftCommand) error {
	s.prepareCalls++
	if command.TrustedContext.BusinessCommandID != recoveryTestCommandID || command.RequestDigest == "" {
		return errors.New("invalid prepared command")
	}
	return nil
}

func (s *recoveryTestStore) ReserveCommandResend(
	_ context.Context,
	_ TrustedContext,
	recovery RecoveryDeferred,
) (RecoveryDeferred, bool, error) {
	s.reserveCalls++
	if s.reserve != nil {
		return s.reserve(recovery)
	}
	// 旧恢复矩阵默认模拟预算已耗尽；专项用例显式注入真实预留行为。
	recovery.ResendAttempts = 1
	recovery.ResendLimit = 1
	recovery.ResendExhausted = true
	return recovery, false, nil
}

func (s *recoveryTestStore) FreezeTerminal(_ context.Context, _ TrustedContext, result Result) error {
	s.freezeCalls++
	copy := result
	s.terminal = &copy
	s.recovery = nil
	return nil
}

func (s *recoveryTestStore) MarkRecovery(_ context.Context, _ TrustedContext, recovery RecoveryDeferred) error {
	s.markCalls++
	copy := recovery
	s.recovery = &copy
	return nil
}

type recoveryTestModel struct {
	proposal      Proposal
	generateCalls int
}

func (m *recoveryTestModel) Generate(context.Context, []*schema.Message, ...model.Option) (*schema.Message, error) {
	m.generateCalls++
	encoded, err := json.Marshal(m.proposal)
	if err != nil {
		return nil, err
	}
	return schema.AssistantMessage(string(encoded), nil), nil
}

func (m *recoveryTestModel) Stream(ctx context.Context, messages []*schema.Message, options ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	message, err := m.Generate(ctx, messages, options...)
	if err != nil {
		return nil, err
	}
	return schema.StreamReaderFromArray([]*schema.Message{message}), nil
}

type recoveryTestClock struct{ now time.Time }

func (clock recoveryTestClock) Now() time.Time { return clock.now }

func newRecoveryTestTool(t *testing.T, proposalModel *recoveryTestModel, business *recoveryTestBusiness, store *recoveryTestStore) *Tool {
	t.Helper()
	return newRecoveryTestToolWithClock(t, proposalModel, business, store, recoveryTestClock{now: processorIndependentNow()})
}

func newRecoveryTestToolWithClock(
	t *testing.T,
	proposalModel *recoveryTestModel,
	business *recoveryTestBusiness,
	store *recoveryTestStore,
	clock Clock,
) *Tool {
	t.Helper()
	graph, err := Compile(context.Background(), proposalModel, business, store, clock)
	if err != nil {
		t.Fatalf("编译 plan_creation_spec Graph 失败: %v", err)
	}
	tool, err := NewTool(graph, store)
	if err != nil {
		t.Fatalf("创建 plan_creation_spec Tool 失败: %v", err)
	}
	return tool
}

func recoveryTestCommand(t *testing.T) DraftCommand {
	t.Helper()
	command := DraftCommand{
		TrustedContext: recoveryTestTrustedContext(),
		DomainContext:  DomainContext{ProjectID: recoveryTestProjectID, ProjectVersion: 1, ProjectTitle: "品牌发布项目"},
		Content:        recoveryTestContent(),
	}
	digest, err := SaveRequestDigest(command)
	if err != nil {
		t.Fatalf("计算测试 Save Request 摘要失败: %v", err)
	}
	command.RequestDigest = digest
	return command
}

func recoveryTestDurableCommand(requestDigest string) DraftCommand {
	return DraftCommand{
		TrustedContext: recoveryTestTrustedContext(),
		DomainContext:  DomainContext{ProjectID: recoveryTestProjectID, ProjectVersion: 1},
		Content:        recoveryTestContent(), RequestDigest: requestDigest,
	}
}

func processorIndependentNow() time.Time {
	return time.Date(2026, 7, 16, 12, 0, 0, 0, time.UTC)
}

func recoveryTestContext() context.Context {
	trusted := recoveryTestTrustedContext()
	return turncontext.WithPreview(context.Background(), turncontext.Preview{
		Owner: trusted.Owner, RequestID: trusted.RequestID, UserID: trusted.UserID,
		ProjectID: trusted.ProjectID, SessionID: trusted.SessionID, InputID: trusted.InputID,
		TurnID: trusted.TurnID, RunID: trusted.RunID, ToolCallID: trusted.ToolCallID,
		BusinessCommandID: trusted.BusinessCommandID, PromptVersion: trusted.PromptVersion,
		ValidatorVersion: trusted.ValidatorVersion, FenceToken: trusted.FenceToken,
	})
}

func recoveryTestTrustedContext() TrustedContext {
	return TrustedContext{
		Owner: "preview-test-owner", RequestID: recoveryTestRequestID, UserID: recoveryTestUserID,
		ProjectID: recoveryTestProjectID, SessionID: recoveryTestSessionID, InputID: recoveryTestInputID,
		TurnID: recoveryTestTurnID, RunID: recoveryTestRunID, ToolCallID: recoveryTestToolCallID,
		BusinessCommandID: recoveryTestCommandID, PromptVersion: PromptVersion, ValidatorVersion: ValidatorVersion,
		FenceToken: 9,
	}
}

func recoveryTestProposal() Proposal {
	return Proposal{
		SchemaVersion: ProposalSchemaVersion, Title: "品牌发布短片创作规格", Goal: "制作一支品牌发布短片",
		DeliverableType: "video", Audience: "潜在客户",
		Phases:      []Phase{{Key: "phase_1", Title: "创作规划", Objective: "冻结结构与表达边界", Output: "可执行创作规格"}},
		Constraints: []string{"时长不超过六十秒"}, AcceptanceCriteria: []string{"成片时长不超过六十秒"},
	}
}

func recoveryTestContent() Content {
	proposal := recoveryTestProposal()
	return Content{
		Title: proposal.Title, Goal: proposal.Goal, DeliverableType: proposal.DeliverableType,
		Audience: proposal.Audience, Locale: "zh-CN", Phases: append([]Phase(nil), proposal.Phases...),
		Constraints:        append([]string(nil), proposal.Constraints...),
		AcceptanceCriteria: append([]string(nil), proposal.AcceptanceCriteria...),
	}
}

func recoveryTestResource(content Content, contentDigest string) Resource {
	return Resource{
		ID: recoveryTestCreationSpecID, ProjectID: recoveryTestProjectID, Version: 1,
		Status: "draft", ContentDigest: contentDigest, Content: content,
	}
}

func assertRecoveryTestJSONKeys(t *testing.T, encoded []byte, want ...string) {
	t.Helper()
	var object map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &object); err != nil {
		t.Fatalf("解析 Result JSON 失败: %v; json=%s", err, encoded)
	}
	got := make([]string, 0, len(object))
	for key := range object {
		got = append(got, key)
	}
	sort.Strings(got)
	sort.Strings(want)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Result JSON exact-set=%v want=%v; json=%s", got, want, encoded)
	}
}

var _ BusinessClient = (*recoveryTestBusiness)(nil)
var _ ResultStore = (*recoveryTestStore)(nil)
var _ model.BaseChatModel = (*recoveryTestModel)(nil)
