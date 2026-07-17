package chatmodel

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/FigoGoo/Dora-Agent/agent/internal/turncontext"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

var (
	errReceiptTestConflict = errors.New("模型回执请求摘要冲突")
	errReceiptTestStore    = errors.New("模型回执存储失败")
)

// TestReceiptModelFirstWriteWinsAndReplay 验证首次完整响应冻结后，同一请求只重放数据库首写结果而不再次调用底层模型。
func TestReceiptModelFirstWriteWinsAndReplay(t *testing.T) {
	store := newReceiptTestStore()
	base := &receiptTestModel{response: schema.AssistantMessage("first response", nil)}
	wrapped, err := NewReceiptModel(base, store, ReceiptCallRouter)
	if err != nil {
		t.Fatalf("创建模型回执包装器失败: %v", err)
	}
	ctx := receiptTestContext()
	messages := []*schema.Message{schema.UserMessage("生成创作规格")}

	first, err := wrapped.Generate(ctx, messages)
	if err != nil {
		t.Fatalf("首次模型调用失败: %v", err)
	}
	base.response = schema.AssistantMessage("must not escape", nil)
	second, err := wrapped.Generate(ctx, messages)
	if err != nil {
		t.Fatalf("模型回执重放失败: %v", err)
	}
	if first.Content != "first response" || second.Content != first.Content {
		t.Fatalf("first-write-wins 响应漂移: first=%q second=%q", first.Content, second.Content)
	}
	if base.generateCalls != 1 || store.freezeCalls != 1 {
		t.Fatalf("同义重放重复执行模型或冻结: model=%d freeze=%d", base.generateCalls, store.freezeCalls)
	}
}

// TestReceiptModelRejectsRequestDigestConflict 验证相同 ToolCall/call_index 的不同请求摘要失败关闭，且冲突请求不进入底层模型。
func TestReceiptModelRejectsRequestDigestConflict(t *testing.T) {
	store := newReceiptTestStore()
	base := &receiptTestModel{response: schema.AssistantMessage("frozen", nil)}
	wrapped, err := NewReceiptModel(base, store, ReceiptCallRouter)
	if err != nil {
		t.Fatalf("创建模型回执包装器失败: %v", err)
	}
	ctx := receiptTestContext()
	if _, err := wrapped.Generate(ctx, []*schema.Message{schema.UserMessage("request one")}); err != nil {
		t.Fatalf("建立首个模型回执失败: %v", err)
	}
	_, err = wrapped.Generate(ctx, []*schema.Message{schema.UserMessage("request two")})
	if !errors.Is(err, errReceiptTestConflict) {
		t.Fatalf("异义模型请求错误=%v want digest conflict", err)
	}
	if base.generateCalls != 1 {
		t.Fatalf("摘要冲突后仍调用底层模型: calls=%d", base.generateCalls)
	}
}

// TestReceiptModelPropagatesStoreFailures 验证占位和冻结存储错误均停止执行，且不会把未冻结响应冒充成功返回。
func TestReceiptModelPropagatesStoreFailures(t *testing.T) {
	t.Run("reserve failure skips model", func(t *testing.T) {
		store := newReceiptTestStore()
		store.replayErr = errReceiptTestStore
		base := &receiptTestModel{response: schema.AssistantMessage("unused", nil)}
		wrapped, err := NewReceiptModel(base, store, ReceiptCallRouter)
		if err != nil {
			t.Fatalf("创建模型回执包装器失败: %v", err)
		}
		_, err = wrapped.Generate(receiptTestContext(), []*schema.Message{schema.UserMessage("request")})
		if !errors.Is(err, errReceiptTestStore) || base.generateCalls != 0 {
			t.Fatalf("占位错误未阻止模型: err=%v calls=%d", err, base.generateCalls)
		}
	})

	t.Run("freeze failure hides response", func(t *testing.T) {
		store := newReceiptTestStore()
		store.freezeErr = errReceiptTestStore
		base := &receiptTestModel{response: schema.AssistantMessage("not durable", nil)}
		wrapped, err := NewReceiptModel(base, store, ReceiptCallRouter)
		if err != nil {
			t.Fatalf("创建模型回执包装器失败: %v", err)
		}
		response, err := wrapped.Generate(receiptTestContext(), []*schema.Message{schema.UserMessage("request")})
		if !errors.Is(err, errReceiptTestStore) || response != nil || base.generateCalls != 1 {
			t.Fatalf("冻结错误泄漏未持久响应: response=%+v err=%v calls=%d", response, err, base.generateCalls)
		}
	})
}

// TestReceiptModelUsesStableCallIndexes 验证 Router 首轮/收尾与 Proposal 分别固定使用 call_index 1/3/2。
func TestReceiptModelUsesStableCallIndexes(t *testing.T) {
	tests := []struct {
		name     string
		kind     ReceiptCallKind
		messages []*schema.Message
		want     int
	}{
		{name: "router first turn", kind: ReceiptCallRouter, messages: []*schema.Message{schema.UserMessage("intent")}, want: 1},
		{name: "proposal", kind: ReceiptCallProposal, messages: []*schema.Message{schema.SystemMessage("prompt")}, want: 2},
		{name: "router after tool", kind: ReceiptCallRouter, messages: []*schema.Message{schema.UserMessage("intent"), schema.ToolMessage("result", "tool-call")}, want: 3},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store := newReceiptTestStore()
			base := &receiptTestModel{response: schema.AssistantMessage("response", nil)}
			wrapped, err := NewReceiptModel(base, store, test.kind)
			if err != nil {
				t.Fatalf("创建模型回执包装器失败: %v", err)
			}
			if _, err := wrapped.Generate(receiptTestContext(), test.messages); err != nil {
				t.Fatalf("执行模型回执包装器失败: %v", err)
			}
			if len(store.callIndexes) == 0 || store.callIndexes[0] != test.want {
				t.Fatalf("首个 call_index=%v want=%d", store.callIndexes, test.want)
			}
			for _, callIndex := range store.callIndexes {
				if callIndex != test.want {
					t.Fatalf("同一次冻结/重放 call_index 漂移: %v", store.callIndexes)
				}
			}
		})
	}
}

type receiptTestModel struct {
	response      *schema.Message
	err           error
	generateCalls int
}

func (m *receiptTestModel) Generate(context.Context, []*schema.Message, ...model.Option) (*schema.Message, error) {
	m.generateCalls++
	if m.err != nil {
		return nil, m.err
	}
	return cloneReceiptTestMessage(m.response), nil
}

func (m *receiptTestModel) Stream(ctx context.Context, messages []*schema.Message, options ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	response, err := m.Generate(ctx, messages, options...)
	if err != nil {
		return nil, err
	}
	return schema.StreamReaderFromArray([]*schema.Message{response}), nil
}

type receiptTestEntry struct {
	digest   string
	response *schema.Message
}

type receiptTestStore struct {
	mu          sync.Mutex
	entries     map[string]receiptTestEntry
	replayErr   error
	freezeErr   error
	freezeCalls int
	callIndexes []int
}

func newReceiptTestStore() *receiptTestStore {
	return &receiptTestStore{entries: make(map[string]receiptTestEntry)}
}

func (s *receiptTestStore) ReplayOrReserveModel(
	_ context.Context,
	identity ReceiptIdentity,
	callIndex int,
	requestDigest string,
) (*schema.Message, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.callIndexes = append(s.callIndexes, callIndex)
	if s.replayErr != nil {
		return nil, false, s.replayErr
	}
	key := receiptTestKey(identity, callIndex)
	entry, ok := s.entries[key]
	if ok {
		if entry.digest != requestDigest {
			return nil, false, errReceiptTestConflict
		}
		if entry.response != nil {
			return cloneReceiptTestMessage(entry.response), false, nil
		}
		return nil, true, nil
	}
	s.entries[key] = receiptTestEntry{digest: requestDigest}
	return nil, true, nil
}

func (s *receiptTestStore) FreezeModel(
	_ context.Context,
	identity ReceiptIdentity,
	callIndex int,
	requestDigest string,
	response *schema.Message,
) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.freezeCalls++
	if s.freezeErr != nil {
		return s.freezeErr
	}
	key := receiptTestKey(identity, callIndex)
	entry, ok := s.entries[key]
	if !ok || entry.digest != requestDigest {
		return errReceiptTestConflict
	}
	if entry.response == nil {
		entry.response = cloneReceiptTestMessage(response)
		s.entries[key] = entry
	}
	return nil
}

func receiptTestContext() context.Context {
	return turncontext.WithPreview(context.Background(), turncontext.Preview{
		Owner: "processor-test", SessionID: "019f68e8-1001-7000-8000-000000000001",
		InputID: "019f68e8-1002-7000-8000-000000000002", ToolCallID: "019f68e8-1003-7000-8000-000000000003",
		FenceToken: 7,
	})
}

func receiptTestKey(identity ReceiptIdentity, callIndex int) string {
	return fmt.Sprintf("%s/%s/%s/%d", identity.SessionID, identity.InputID, identity.ToolCallID, callIndex)
}

func cloneReceiptTestMessage(message *schema.Message) *schema.Message {
	if message == nil {
		return nil
	}
	copy := *message
	copy.ToolCalls = append([]schema.ToolCall(nil), message.ToolCalls...)
	return &copy
}

var _ model.BaseChatModel = (*receiptTestModel)(nil)
var _ ReceiptStore = (*receiptTestStore)(nil)
