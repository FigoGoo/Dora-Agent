package session

import (
	"context"
	"fmt"
	"testing"
	"time"

	aigcconfig "github.com/FigoGoo/Dora-Agent/internal/aigc/config"
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
		Role:      "user",
		Content:   "生成一个武侠短片",
	})
	if err != nil {
		t.Fatalf("AppendMessage(first) error = %v", err)
	}
	second, err := store.AppendMessage(ctx, MessageRecord{
		ID:        fmt.Sprintf("message-%d-2", suffix),
		SessionID: sessionID,
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
	if len(messages) != 1 || messages[0].Seq != 2 || messages[0].Content != "我会先规划故事板。" {
		t.Fatalf("unexpected message window: %#v", messages)
	}
}
