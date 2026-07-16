package event

import (
	"bytes"
	"testing"
	"time"
)

// TestSessionEventsDoNotExposePrompt 验证 W0 事件载荷只包含安全投影，不泄漏 Prompt 正文。
func TestSessionEventsDoNotExposePrompt(t *testing.T) {
	now := time.Date(2026, 7, 14, 12, 0, 0, 0, time.FixedZone("CST", 8*60*60))
	created, err := NewSessionCreated("event-1", "session-1", "project-1", "active", "command-1", 1, now)
	if err != nil {
		t.Fatalf("创建 Session Event 失败: %v", err)
	}
	accepted, err := NewSessionInputAccepted("event-2", "session-1", "input-1", "message-1", "command-1", "pending", 1, now)
	if err != nil {
		t.Fatalf("创建 Input Event 失败: %v", err)
	}
	for _, record := range []Record{created, accepted} {
		if bytes.Contains(record.PayloadJSON, []byte("secret prompt")) {
			t.Fatalf("事件载荷泄漏 Prompt: %s", record.PayloadJSON)
		}
		if record.CreatedAt.Location() != time.UTC {
			t.Fatalf("事件时间未转换为 UTC: %v", record.CreatedAt.Location())
		}
	}
	if created.ProjectionIndex != 0 || accepted.ProjectionIndex != 1 {
		t.Fatalf("投影顺序不稳定: created=%d accepted=%d", created.ProjectionIndex, accepted.ProjectionIndex)
	}
}
