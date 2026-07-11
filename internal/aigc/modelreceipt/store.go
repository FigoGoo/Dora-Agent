// Package modelreceipt persists the authoritative ChatModel output for each
// durable Agent turn/model-call slot. A receipt is immutable: the first
// successfully stored output wins and every retry reuses it.
package modelreceipt

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

var ErrNotFound = errors.New("model output receipt not found")

// Receipt freezes one ChatModel output. TurnID and Ordinal form the durable
// slot identity; InputDigest is audit-only and intentionally does not reject a
// replay whose reconstructed model input differs.
type Receipt struct {
	TurnID       string          `json:"turn_id" gorm:"column:turn_id;primaryKey;size:191;not null"`
	Ordinal      int             `json:"ordinal" gorm:"column:ordinal;primaryKey;not null"`
	OutputJSON   json.RawMessage `json:"output_json" gorm:"column:output_json;type:jsonb;not null"`
	OutputDigest string          `json:"output_digest" gorm:"column:output_digest;size:64;not null"`
	InputDigest  string          `json:"input_digest,omitempty" gorm:"column:input_digest;size:64;not null;default:''"`
	CreatedAt    time.Time       `json:"created_at" gorm:"column:created_at;not null"`
}

func (Receipt) TableName() string { return "aigc_model_output_receipts" }

// Store implements immutable, first-write-wins receipt persistence.
type Store interface {
	Get(ctx context.Context, turnID string, ordinal int) (Receipt, error)
	PutOnce(ctx context.Context, receipt Receipt) (Receipt, error)
}

// Digest returns the lowercase SHA-256 digest used by receipt audit fields.
func Digest(value []byte) string {
	sum := sha256.Sum256(value)
	return hex.EncodeToString(sum[:])
}

func normalize(receipt Receipt) (Receipt, error) {
	receipt.TurnID = strings.TrimSpace(receipt.TurnID)
	receipt.OutputDigest = strings.TrimSpace(receipt.OutputDigest)
	receipt.InputDigest = strings.TrimSpace(receipt.InputDigest)
	receipt.OutputJSON = cloneJSON(receipt.OutputJSON)
	if receipt.TurnID == "" {
		return Receipt{}, fmt.Errorf("model receipt turn id is required")
	}
	if receipt.Ordinal <= 0 {
		return Receipt{}, fmt.Errorf("model receipt ordinal must be positive")
	}
	if len(receipt.OutputJSON) == 0 || !json.Valid(receipt.OutputJSON) {
		return Receipt{}, fmt.Errorf("model receipt output_json must be valid JSON")
	}
	originalDigest := Digest(receipt.OutputJSON)
	canonical, err := canonicalJSON(receipt.OutputJSON)
	if err != nil {
		return Receipt{}, fmt.Errorf("canonicalize model receipt output_json: %w", err)
	}
	receipt.OutputJSON = canonical
	actualDigest := Digest(receipt.OutputJSON)
	if receipt.OutputDigest != "" && receipt.OutputDigest != originalDigest && receipt.OutputDigest != actualDigest {
		return Receipt{}, fmt.Errorf("model receipt output digest mismatch")
	}
	// Persist the digest of the canonical JSONB-compatible representation.
	receipt.OutputDigest = actualDigest
	return receipt, nil
}

// PostgreSQL jsonb is semantic JSON storage and may reformat object bytes on a
// read. Digesting a canonical encoding keeps the stored digest stable across
// that round trip.
func canonicalJSON(value json.RawMessage) (json.RawMessage, error) {
	var decoded any
	decoder := json.NewDecoder(bytes.NewReader(value))
	decoder.UseNumber()
	if err := decoder.Decode(&decoded); err != nil {
		return nil, err
	}
	canonical, err := json.Marshal(decoded)
	if err != nil {
		return nil, err
	}
	return canonical, nil
}

func clone(receipt Receipt) Receipt {
	receipt.OutputJSON = cloneJSON(receipt.OutputJSON)
	return receipt
}

func cloneJSON(value json.RawMessage) json.RawMessage {
	if value == nil {
		return nil
	}
	return append(json.RawMessage(nil), value...)
}
