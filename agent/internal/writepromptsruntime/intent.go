package writepromptsruntime

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/FigoGoo/Dora-Agent/agent/internal/graphtool/writeprompts"
)

// CanonicalIntent 是 Runtime 入队和 Router 共用的严格 Intent 事实。
type CanonicalIntent struct {
	// Value 是严格解码后的模型可控 Intent。
	Value writeprompts.Intent
	// JSON 是字段顺序冻结的 canonical JSON。
	JSON []byte
	// Digest 是 canonical JSON 的小写 SHA-256。
	Digest string
}

// DecodeIntent 复用 Tool Core strict decoder 并冻结 canonical bytes 与摘要。
func DecodeIntent(encoded []byte) (CanonicalIntent, error) {
	intent, err := writeprompts.DecodeIntent(encoded)
	if err != nil {
		return CanonicalIntent{}, fmt.Errorf("decode write prompts runtime intent: %w", err)
	}
	canonical, err := json.Marshal(intent)
	if err != nil {
		return CanonicalIntent{}, fmt.Errorf("encode write prompts runtime intent: %w", err)
	}
	digest, err := writeprompts.IntentDigest(intent)
	if err != nil {
		return CanonicalIntent{}, fmt.Errorf("digest write prompts runtime intent: %w", err)
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
		return CanonicalIntent{}, fmt.Errorf("validate write prompts runtime intent: canonical bytes or digest mismatch")
	}
	return intent, nil
}
