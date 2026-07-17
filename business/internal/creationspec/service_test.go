package creationspec

import (
	"context"
	"errors"
	"testing"
	"time"
)

type creationSpecRepositoryStub struct {
	contextResult ProjectContext
	contextErr    error
	saveResult    SaveResult
	saveErr       error
	queryResult   QueryResult
	queryErr      error
	saved         SaveAggregate
	queried       QueryCommand
}

func (stub *creationSpecRepositoryStub) FindOwnedProject(_ context.Context, _, _ string) (ProjectContext, error) {
	return stub.contextResult, stub.contextErr
}

func (stub *creationSpecRepositoryStub) SaveDraft(_ context.Context, aggregate SaveAggregate) (SaveResult, error) {
	stub.saved = aggregate
	if stub.saveResult.Draft.ID == "" {
		stub.saveResult = SaveResult{Disposition: CommandDispositionCreated, Draft: aggregate.Draft}
	}
	return stub.saveResult, stub.saveErr
}

func (stub *creationSpecRepositoryStub) QueryCommand(_ context.Context, query QueryCommand) (QueryResult, error) {
	stub.queried = query
	return stub.queryResult, stub.queryErr
}

type creationSpecClockStub struct{ now time.Time }

func (stub creationSpecClockStub) Now() time.Time { return stub.now }

type creationSpecIDStub struct {
	values []string
	index  int
}

func (stub *creationSpecIDStub) New() (string, error) {
	if stub.index >= len(stub.values) {
		return "", errors.New("id exhausted")
	}
	value := stub.values[stub.index]
	stub.index++
	return value, nil
}

// TestServiceSaveDraftBuildsAtomicAggregate 验证 Service 重算摘要并只把完整 Draft/Receipt 聚合交给 Repository。
func TestServiceSaveDraftBuildsAtomicAggregate(t *testing.T) {
	repository := &creationSpecRepositoryStub{}
	clock := creationSpecClockStub{now: time.Date(2026, 7, 16, 1, 2, 3, 0, time.FixedZone("CST", 8*60*60))}
	ids := &creationSpecIDStub{values: []string{testDraftID, testReceiptID}}
	service, err := NewService(repository, clock, ids)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	result, err := service.SaveDraft(context.Background(), testSaveCommand(t))
	if err != nil {
		t.Fatalf("SaveDraft() error = %v", err)
	}
	if result.Disposition != CommandDispositionCreated || result.Draft.ID != testDraftID ||
		repository.saved.Receipt.CommandID != testCommandID || repository.saved.Receipt.RequestDigest.Hex() != testSaveDigest ||
		repository.saved.Draft.CreatedAt.Location() != time.UTC {
		t.Fatalf("save result/aggregate mismatch: result=%+v aggregate=%+v", result, repository.saved)
	}
}

// TestServiceRejectsDigestTamperingBeforeRepository 验证 Agent 提供的摘要不匹配时不会产生 ID 或数据库写入。
func TestServiceRejectsDigestTamperingBeforeRepository(t *testing.T) {
	repository := &creationSpecRepositoryStub{}
	ids := &creationSpecIDStub{values: []string{testDraftID, testReceiptID}}
	service, _ := NewService(repository, creationSpecClockStub{now: time.Now()}, ids)
	command := testSaveCommand(t)
	command.RequestDigestHex = "0000000000000000000000000000000000000000000000000000000000000000"
	if _, err := service.SaveDraft(context.Background(), command); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("tampered digest error=%v", err)
	}
	if ids.index != 0 || repository.saved.Draft.ID != "" {
		t.Fatalf("invalid command reached side effects: ids=%d aggregate=%+v", ids.index, repository.saved)
	}
}

// TestServiceContextAndQueryFailClosed 验证 Owner 上下文与 Query Repository 返回值仍由应用边界复核。
func TestServiceContextAndQueryFailClosed(t *testing.T) {
	repository := &creationSpecRepositoryStub{contextResult: ProjectContext{ProjectID: testProjectID, Version: 1, Title: "安全项目"}}
	service, _ := NewService(repository, creationSpecClockStub{now: time.Now()}, &creationSpecIDStub{})
	if _, err := service.GetContext(context.Background(), ContextQuery{UserID: testUserID, ProjectID: testProjectID}); err != nil {
		t.Fatalf("GetContext() error = %v", err)
	}
	repository.contextResult.ProjectID = testDraftID
	if _, err := service.GetContext(context.Background(), ContextQuery{UserID: testUserID, ProjectID: testProjectID}); !errors.Is(err, ErrPersistence) {
		t.Fatalf("mismatched context error=%v", err)
	}
	repository.queryResult = QueryResult{Status: QueryStatusCompleted}
	if _, err := service.QueryCommand(context.Background(), testCommandID, testSaveDigest, testUserID, testProjectID); !errors.Is(err, ErrPersistence) {
		t.Fatalf("completed query without Draft error=%v", err)
	}
}

func testSaveCommand(t *testing.T) SaveCommand {
	t.Helper()
	digest, err := SaveRequestDigest(testUserID, testProjectID, 1, testToolCallID, testPrompt, testValidator, testCreationSpecContent())
	if err != nil {
		t.Fatalf("SaveRequestDigest() error = %v", err)
	}
	return SaveCommand{
		CommandID: testCommandID, RequestDigestHex: digest.Hex(), UserID: testUserID, ProjectID: testProjectID,
		ExpectedProjectVersion: 1, ToolCallID: testToolCallID, PromptVersion: testPrompt,
		ValidatorVersion: testValidator, Content: testCreationSpecContent(),
	}
}
