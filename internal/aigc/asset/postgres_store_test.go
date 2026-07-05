package asset

import (
	"context"
	"testing"
	"time"

	aigcconfig "github.com/FigoGoo/Dora-Agent/internal/aigc/config"
	aigcstorage "github.com/FigoGoo/Dora-Agent/internal/aigc/storage"
)

func TestPostgresStorePersistsAssets(t *testing.T) {
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

	record := Asset{
		ID:              "asset-store-test",
		SessionID:       "asset-session",
		UserID:          "user-1",
		Kind:            KindImage,
		Source:          SourceUpload,
		MIMEType:        "image/png",
		Filename:        "ref.png",
		SizeBytes:       123,
		StorageProvider: StorageProviderTOS,
		Bucket:          "dora-public",
		ObjectKey:       "aigc/sessions/asset-session/assets/asset-store-test/ref.png",
		URL:             "https://tos.doraigc.com/aigc/sessions/asset-session/assets/asset-store-test/ref.png",
		Metadata:        map[string]any{"element_key": "suji"},
	}
	if _, err := store.Save(ctx, record); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	got, err := store.Get(ctx, record.ID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got.ID != record.ID || got.SessionID != record.SessionID || got.URL != record.URL {
		t.Fatalf("asset = %#v", got)
	}
	if got.Metadata["element_key"] != "suji" {
		t.Fatalf("metadata = %#v", got.Metadata)
	}

	list, err := store.ListBySession(ctx, record.SessionID)
	if err != nil {
		t.Fatalf("ListBySession() error = %v", err)
	}
	if len(list) == 0 || list[0].ID != record.ID {
		t.Fatalf("list = %#v", list)
	}
}
