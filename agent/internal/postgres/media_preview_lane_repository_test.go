package postgres

import (
	"context"
	"errors"
	"testing"

	"github.com/FigoGoo/Dora-Agent/agent/internal/mediapreview"
	"github.com/FigoGoo/Dora-Agent/agent/internal/mediapreviewruntime"
)

// TestMediaRuntimeRepositoryRejectsCrossJobTerminalAsset 锁定 Repository 在任何事务或 Event 投影前拒绝串单资产。
func TestMediaRuntimeRepositoryRejectsCrossJobTerminalAsset(t *testing.T) {
	t.Parallel()
	repository := &MediaRuntimeRepository{}
	claim := mediapreviewruntime.TerminalClaim{
		ToolKey: mediapreview.GenerateMediaToolKey, JobType: mediapreview.JobTypeGeneratePNG,
		AssetID: "019f68e8-0430-7000-8000-000000000430", AssetVersion: 1,
	}
	result := mediapreviewruntime.TerminalResult{
		SchemaVersion: mediapreview.JobResultSchemaVersion, Status: "succeeded",
		AssetRef: &mediapreviewruntime.TerminalAssetRef{
			AssetID: "019f68e8-0431-7000-8000-000000000431", Version: 1, Status: "ready",
			MediaKind: "image", MIMEType: "image/png",
			ContentDigest: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", SizeBytes: 1024,
		},
		FinalizationReceiptID: "019f68e8-0432-7000-8000-000000000432",
	}
	if err := repository.CompleteTerminal(context.Background(), claim, result); !errors.Is(err, mediapreviewruntime.ErrOutputContract) {
		t.Fatalf("CompleteTerminal error = %v, want ErrOutputContract", err)
	}
}
