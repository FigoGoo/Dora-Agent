package modelreceipt

import (
	"context"
	"fmt"
	"testing"
	"time"

	aigcconfig "github.com/FigoGoo/Dora-Agent/internal/aigc/config"
	aigcstorage "github.com/FigoGoo/Dora-Agent/internal/aigc/storage"
)

func TestPostgresStorePutOnceKeepsFirstOutput(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	db, err := aigcstorage.OpenAgentPostgres(ctx, aigcconfig.LoadFromEnv())
	if err != nil {
		t.Skipf("agent postgres unavailable: %v", err)
	}
	store := NewPostgresStore(db)
	if err := store.AutoMigrate(ctx); err != nil {
		t.Fatalf("AutoMigrate() error = %v", err)
	}
	turnID := fmt.Sprintf("model-receipt-pg-%d", time.Now().UnixNano())
	first, err := store.PutOnce(ctx, receiptForTest(turnID, 1, `{"tool":"A"}`))
	if err != nil {
		t.Fatalf("PutOnce(first) error = %v", err)
	}
	second, err := store.PutOnce(ctx, receiptForTest(turnID, 1, `{"tool":"B"}`))
	if err != nil {
		t.Fatalf("PutOnce(second) error = %v", err)
	}
	if second.OutputDigest != first.OutputDigest || string(second.OutputJSON) != string(first.OutputJSON) {
		t.Fatalf("second writer replaced first: first=%s second=%s", first.OutputJSON, second.OutputJSON)
	}
	if err := db.WithContext(ctx).Where("turn_id = ?", turnID).Delete(&Receipt{}).Error; err != nil {
		t.Fatalf("cleanup receipt: %v", err)
	}
}
