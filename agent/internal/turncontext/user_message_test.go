package turncontext

import (
	"context"
	"testing"
)

// TestUserMessageRuntimeContextUsesValueCopy 验证可信上下文不保留调用方可变指针。
func TestUserMessageRuntimeContextUsesValueCopy(t *testing.T) {
	value := UserMessageRuntime{
		Profile: "user_message.runtime.v2preview1", Owner: "processor-a", RunID: "run-a",
		ModelCallID: "model-a", OutputID: "output-a", FenceToken: 7,
		Context: UserMessageTurnContext{
			SchemaVersion: UserMessageTurnContextSchemaVersion,
			TurnID:        "turn-a", SessionID: "session-a", InputID: "input-a", MessageID: "message-a", ContextDigest: "digest-a",
		},
	}
	ctx := WithUserMessageRuntime(context.Background(), value)
	value.Owner = "mutated"
	value.Context.SessionID = "mutated"

	loaded, ok := UserMessageRuntimeFrom(ctx)
	if !ok || loaded.Owner != "processor-a" || loaded.Context.SessionID != "session-a" {
		t.Fatalf("可信 User Message 上下文发生别名漂移: ok=%v value=%+v", ok, loaded)
	}
	if _, ok := UserMessageRuntimeFrom(context.Background()); ok {
		t.Fatal("空 Context 意外返回可信 User Message 上下文")
	}
}
