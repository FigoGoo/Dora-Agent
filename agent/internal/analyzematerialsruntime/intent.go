package analyzematerialsruntime

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/FigoGoo/Dora-Agent/agent/internal/graphtool/analyzematerials"
)

// CanonicalIntent 是 Runtime 入队和 Router 共用的严格 Intent 事实。
type CanonicalIntent struct {
	Value  analyzematerials.Intent
	JSON   []byte
	Digest string
}

// DecodeIntent 在 Tool Core 契约之上收紧 expected_assets 为必填 exact-set。
func DecodeIntent(encoded []byte) (CanonicalIntent, error) {
	intent, err := analyzematerials.DecodeIntent(encoded)
	if err != nil {
		return CanonicalIntent{}, fmt.Errorf("decode analyze materials runtime intent: %w", err)
	}
	if intent.ExpectedAssets == nil || len(intent.ExpectedAssets) != len(intent.AssetIDs) {
		return CanonicalIntent{}, fmt.Errorf("decode analyze materials runtime intent: expected_assets exact-set is required")
	}
	canonical, err := json.Marshal(intent)
	if err != nil {
		return CanonicalIntent{}, fmt.Errorf("encode analyze materials runtime intent: %w", err)
	}
	digest, err := analyzematerials.IntentDigest(intent)
	if err != nil {
		return CanonicalIntent{}, fmt.Errorf("digest analyze materials runtime intent: %w", err)
	}
	return CanonicalIntent{Value: intent, JSON: canonical, Digest: digest}, nil
}

// ValidateCanonicalIntent 证明解密后的明文就是已冻结 canonical JSON 和摘要。
func ValidateCanonicalIntent(encoded []byte, expectedDigest string) (CanonicalIntent, error) {
	intent, err := DecodeIntent(encoded)
	if err != nil {
		return CanonicalIntent{}, err
	}
	if !bytes.Equal(intent.JSON, encoded) || intent.Digest != expectedDigest {
		return CanonicalIntent{}, fmt.Errorf("validate analyze materials runtime intent: canonical bytes or digest mismatch")
	}
	return intent, nil
}
