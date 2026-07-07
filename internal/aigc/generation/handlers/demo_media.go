package handlers

import (
	"context"
	"fmt"
	"hash/fnv"
	"strings"

	"github.com/FigoGoo/Dora-Agent/internal/aigc/asset"
	"github.com/FigoGoo/Dora-Agent/internal/aigc/generation"
)

// AssetSaver is the minimal asset persistence capability the demo handlers need.
// asset.PostgresStore satisfies it.
type AssetSaver interface {
	Save(ctx context.Context, a asset.Asset) (asset.Asset, error)
}

// DemoMediaJobHandlerConfig configures a fixed-asset media handler used when real
// provider keys are absent, so the whole flow (reference images → shot videos →
// narration/music → assembly) can be walked end-to-end without calling providers.
type DemoMediaJobHandlerConfig struct {
	Assets   AssetSaver
	Provider string   // generation.ProviderImage2 / ProviderSeedance / ProviderAudio
	Kind     string   // asset.KindImage / KindVideo / KindAudio
	MIMEType string   // e.g. image/png, video/mp4, audio/mpeg
	URLs     []string // fixed asset URLs to rotate across targets (must be non-empty)
}

// DemoMediaJobHandler persists a fixed asset (pointing at a bundled demo file) and
// returns its id so the worker binds it onto the storyboard target like a real one.
type DemoMediaJobHandler struct {
	cfg DemoMediaJobHandlerConfig
}

func NewDemoMediaJobHandler(cfg DemoMediaJobHandlerConfig) DemoMediaJobHandler {
	return DemoMediaJobHandler{cfg: cfg}
}

func (h DemoMediaJobHandler) Handle(ctx context.Context, job generation.GenerationJob) (generation.HandlerResult, error) {
	if h.cfg.Assets == nil {
		return generation.HandlerResult{}, fmt.Errorf("demo media handler requires an asset store")
	}
	if len(h.cfg.URLs) == 0 {
		return generation.HandlerResult{}, fmt.Errorf("demo media handler requires at least one url")
	}
	url := h.pickURL(job.TargetID)
	assetID := "demo-asset:" + job.ID
	saved, err := h.cfg.Assets.Save(ctx, asset.Asset{
		ID:              assetID,
		SessionID:       job.SessionID,
		Kind:            h.cfg.Kind,
		Source:          "demo",
		MIMEType:        h.cfg.MIMEType,
		Filename:        demoFilename(url),
		StorageProvider: "demo",
		URL:             url,
		Metadata: map[string]any{
			"demo_placeholder": true,
			"provider":         h.cfg.Provider,
			"target_type":      strings.TrimSpace(job.TargetType),
			"target_id":        strings.TrimSpace(job.TargetID),
			"media_kind":       jobPayloadValue(job.Payload, "media_kind"),
			"prompt":           jobPayloadValue(job.Payload, "prompt"),
		},
	})
	if err != nil {
		return generation.HandlerResult{}, fmt.Errorf("save demo %s asset: %w", h.cfg.Kind, err)
	}
	return generation.HandlerResult{
		AssetIDs: []string{saved.ID},
		Result: map[string]any{
			"demo_placeholder": true,
			"provider":         h.cfg.Provider,
			"asset_id":         saved.ID,
			"url":              url,
			"kind":             h.cfg.Kind,
		},
	}, nil
}

// pickURL deterministically maps a target to one of the fixed URLs so repeated runs
// are stable and different targets get some visual variety.
func (h DemoMediaJobHandler) pickURL(targetID string) string {
	if len(h.cfg.URLs) == 1 {
		return h.cfg.URLs[0]
	}
	hsh := fnv.New32a()
	_, _ = hsh.Write([]byte(strings.TrimSpace(targetID)))
	return h.cfg.URLs[int(hsh.Sum32())%len(h.cfg.URLs)]
}

func demoFilename(url string) string {
	trimmed := strings.TrimSpace(url)
	if idx := strings.LastIndex(trimmed, "/"); idx >= 0 && idx+1 < len(trimmed) {
		return trimmed[idx+1:]
	}
	return trimmed
}

var _ generation.JobHandler = DemoMediaJobHandler{}
