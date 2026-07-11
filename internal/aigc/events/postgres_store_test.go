package events

import (
	"context"
	"fmt"
	"testing"
	"time"

	aigcconfig "github.com/FigoGoo/Dora-Agent/internal/aigc/config"
	aigcstorage "github.com/FigoGoo/Dora-Agent/internal/aigc/storage"
)

func TestPostgresStoreAppendOnceAndTail(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	db, err := aigcstorage.OpenAgentPostgres(ctx, aigcconfig.LoadFromEnv())
	if err != nil {
		t.Skipf("agent postgres unavailable: %v", err)
	}
	store := NewPostgresStore(db)
	if err := store.AutoMigrate(ctx); err != nil {
		t.Fatalf("migrate event store: %v", err)
	}
	tx := db.WithContext(ctx).Begin()
	if tx.Error != nil {
		t.Fatalf("begin transaction: %v", tx.Error)
	}
	defer tx.Rollback()
	store = store.WithTx(tx)
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	sessionID, eventID := "event-pg-session-"+suffix, "event-pg-"+suffix
	event := SessionEvent{SessionID: sessionID, EventID: eventID, EventType: "a2ui.action", ProducerKind: ProducerDomainProjector, SourceKey: "domain:" + suffix, Payload: []byte(`{"ok":true}`)}
	first, err := store.AppendSessionEventOnce(ctx, event)
	if err != nil || !first.Appended || first.Event.Seq != 1 {
		t.Fatalf("first append = %#v, %v", first, err)
	}
	retry, err := store.AppendSessionEventOnce(ctx, event)
	if err != nil || retry.Appended || retry.Event.Seq != 1 {
		t.Fatalf("retry append = %#v, %v", retry, err)
	}
	rows, err := store.Tail(ctx, sessionID, TailOptions{AfterSeq: 0})
	if err != nil || len(rows) != 1 || rows[0].EventID != eventID {
		t.Fatalf("tail = %#v, %v", rows, err)
	}
}
