package sessionruntime

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"

	"github.com/FigoGoo/Dora-Agent/internal/aigc/a2ui"
	"github.com/FigoGoo/Dora-Agent/internal/aigc/events"
	"gorm.io/gorm"
)

// terminalFailureEvent is deliberately independent of the internal failure
// string: provider/database details remain in the Turn record, while the
// public event is stable, safe to replay, and cannot change identity between
// competing dead-letter paths.
func terminalFailureEvent(input SessionInputRecord, turnID string) events.SessionEvent {
	turnID = strings.TrimSpace(turnID)
	identity := "input:" + strings.TrimSpace(input.InputID) + ":dead"
	if turnID != "" {
		identity = "turn:" + turnID + ":dead"
	}
	sum := sha256.Sum256([]byte(identity))
	payload, _ := json.Marshal(map[string]any{
		"code":       "session_turn_failed",
		"message":    "创作处理未能完成，请重新提交或稍后重试。",
		"input_id":   input.InputID,
		"turn_id":    turnID,
		"input_type": input.InputType,
		"retryable":  false,
	})
	return events.SessionEvent{
		SessionID: input.SessionID, EventID: "runtime_dead_" + hex.EncodeToString(sum[:12]),
		EventType: a2ui.EventError, ProducerKind: events.ProducerSessionRuntime,
		SourceKey: identity, Payload: payload,
	}
}

func appendTerminalFailureEventTx(ctx context.Context, tx *gorm.DB, input SessionInputRecord, turnID string) error {
	_, err := events.NewPostgresStore(tx).AppendSessionEventOnceTx(ctx, tx, terminalFailureEvent(input, turnID))
	return err
}
