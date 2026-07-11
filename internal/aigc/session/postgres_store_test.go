package session

import (
	"context"
	"fmt"
	"testing"
	"time"

	aigcconfig "github.com/FigoGoo/Dora-Agent/internal/aigc/config"
	"github.com/FigoGoo/Dora-Agent/internal/aigc/sessionruntime"
	aigcstorage "github.com/FigoGoo/Dora-Agent/internal/aigc/storage"
)

func TestPostgresStorePersistsSessionAndMessages(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	db, err := aigcstorage.OpenAgentPostgres(ctx, aigcconfig.LoadFromEnv())
	if err != nil {
		t.Skipf("local postgres is not available: %v", err)
	}

	store := NewPostgresStore(db)
	if err := store.AutoMigrate(ctx); err != nil {
		t.Fatalf("AutoMigrate() error = %v", err)
	}

	suffix := time.Now().UnixNano()
	sessionID := fmt.Sprintf("session-%d", suffix)
	if err := store.SaveSession(ctx, SessionRecord{
		ID:      sessionID,
		UserID:  "user-1",
		SkillID: "skill-video",
		Title:   "测试会话",
		Status:  "active",
	}); err != nil {
		t.Fatalf("SaveSession() error = %v", err)
	}

	gotSession, err := store.GetSession(ctx, sessionID)
	if err != nil {
		t.Fatalf("GetSession() error = %v", err)
	}
	if gotSession.ID != sessionID || gotSession.SkillID != "skill-video" || gotSession.Status != "active" {
		t.Fatalf("unexpected session: %#v", gotSession)
	}

	first, err := store.AppendMessage(ctx, MessageRecord{
		ID:        fmt.Sprintf("message-%d-1", suffix),
		SessionID: sessionID,
		RunID:     "run-a",
		Role:      "user",
		Content:   "生成一个武侠短片",
	})
	if err != nil {
		t.Fatalf("AppendMessage(first) error = %v", err)
	}
	second, err := store.AppendMessage(ctx, MessageRecord{
		ID:        fmt.Sprintf("message-%d-2", suffix),
		SessionID: sessionID,
		RunID:     "run-a",
		Role:      "assistant",
		Content:   "我会先规划故事板。",
	})
	if err != nil {
		t.Fatalf("AppendMessage(second) error = %v", err)
	}
	if first.Seq != 1 || second.Seq != 2 {
		t.Fatalf("unexpected seq values: first=%d second=%d", first.Seq, second.Seq)
	}

	messages, err := store.ListMessages(ctx, sessionID, MessageWindow{Limit: 1})
	if err != nil {
		t.Fatalf("ListMessages() error = %v", err)
	}
	if len(messages) != 2 || messages[0].Seq != 1 || messages[1].Seq != 2 || messages[1].Content != "我会先规划故事板。" {
		t.Fatalf("unexpected message window: %#v", messages)
	}

	throughFirst := first.Seq
	bounded, err := store.ListMessages(ctx, sessionID, MessageWindow{Limit: 10, ThroughSeq: &throughFirst})
	if err != nil {
		t.Fatalf("ListMessages(bounded) error = %v", err)
	}
	if len(bounded) != 2 || bounded[0].Seq != first.Seq || bounded[0].Content != first.Content || bounded[1].Role != "assistant" {
		t.Fatalf("unexpected bounded message window: %#v", bounded)
	}
	zero := int64(0)
	emptyUserHistory, err := store.ListMessages(ctx, sessionID, MessageWindow{Limit: 10, ThroughSeq: &zero})
	if err != nil || len(emptyUserHistory) != 0 {
		t.Fatalf("explicit zero user boundary = %#v, err=%v", emptyUserHistory, err)
	}

	runtimeStore := sessionruntime.NewPostgresStore(db)
	if err := runtimeStore.AutoMigrate(ctx); err != nil {
		t.Fatalf("migrate runtime store: %v", err)
	}
	third := MessageRecord{
		ID: fmt.Sprintf("message-%d-3", suffix), SessionID: sessionID,
		RunID: "run-b", Role: "user", Content: "只让这个 turn 看到前三条消息",
	}
	input := sessionruntime.NewUserMessage(third.ID, "event:"+third.ID)
	third, enqueued, err := store.AppendMessageAndEnqueue(ctx, runtimeStore, third, input)
	if err != nil {
		t.Fatalf("AppendMessageAndEnqueue() error = %v", err)
	}
	if enqueued.Input.ContextMessageSeq != third.Seq {
		t.Fatalf("runtime boundary=%d, message seq=%d", enqueued.Input.ContextMessageSeq, third.Seq)
	}
	decoded, err := sessionruntime.DecodeInput(enqueued.Input)
	if err != nil {
		t.Fatal(err)
	}
	if got := decoded.(sessionruntime.UserMessage).ContextMessageSeq; got != third.Seq {
		t.Fatalf("typed input boundary=%d, want=%d", got, third.Seq)
	}
	predecessorOutput, err := store.AppendMessage(ctx, MessageRecord{
		ID: fmt.Sprintf("message-%d-predecessor-output", suffix), SessionID: sessionID,
		RunID: "run-a", Role: "assistant", Content: "前序 turn 的迟到输出",
	})
	if err != nil {
		t.Fatal(err)
	}
	laterUser, err := store.AppendMessage(ctx, MessageRecord{
		ID: fmt.Sprintf("message-%d-later-user", suffix), SessionID: sessionID,
		RunID: "run-c", Role: "user", Content: "后排用户输入",
	})
	if err != nil {
		t.Fatal(err)
	}
	logical, err := store.ListMessages(ctx, sessionID, MessageWindow{
		Limit: 10, ThroughSeq: &third.Seq, CurrentMessageID: third.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(logical) != 4 || logical[len(logical)-2].ID != predecessorOutput.ID || logical[len(logical)-1].ID != third.ID {
		t.Fatalf("logical turn order = %#v", logical)
	}
	for _, message := range logical {
		if message.ID == laterUser.ID {
			t.Fatalf("later user leaked into logical window: %#v", logical)
		}
	}

	emptySessionID := fmt.Sprintf("session-empty-%d", suffix)
	if err := store.SaveSession(ctx, SessionRecord{ID: emptySessionID, Status: "active"}); err != nil {
		t.Fatal(err)
	}
	batch := sessionruntime.NewBatchContinuationResult("batch-empty-"+fmt.Sprint(suffix), 1, "event-empty-"+fmt.Sprint(suffix))
	if _, err := runtimeStore.EnqueueInput(ctx, emptySessionID, batch); err != nil {
		t.Fatal(err)
	}
	lease, err := runtimeStore.AcquireLease(ctx, emptySessionID, "owner-empty-"+fmt.Sprint(suffix), time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := runtimeStore.ClaimNext(ctx, sessionruntime.ClaimOptions{Fence: lease.Fence(), ClaimTTL: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	turn, _, err := runtimeStore.GetOrCreateTurn(ctx, lease.Fence(), claimed.InputID, sessionruntime.TurnSpec{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runtimeStore.BeginTurn(ctx, lease.Fence(), turn.TurnID); err != nil {
		t.Fatal(err)
	}
	frozen, err := runtimeStore.FreezeTurnContextFromTerminalUserInputs(ctx, lease.Fence(), turn.TurnID)
	if err != nil || !frozen.ContextSeqFrozen || frozen.ContextMessageSeq != 0 {
		t.Fatalf("empty batch context boundary = %+v, err=%v", frozen, err)
	}
}
