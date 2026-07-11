package agentcontrol

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

const (
	NextCapabilityDirectiveVersion = 1
	NextCapabilityDirectivePrefix  = "DORA_INTERNAL_NEXT_CAPABILITY "
)

// NextCapabilityDirective is a trusted, machine-readable System instruction
// emitted by the durable runtime after an approval command has committed.
// User-authored messages are never inspected for this directive.
type NextCapabilityDirective struct {
	Version   int             `json:"version"`
	SourceID  string          `json:"source_id"`
	Tool      string          `json:"tool"`
	Arguments json.RawMessage `json:"arguments"`
}

func EncodeNextCapabilityDirective(value NextCapabilityDirective) (string, error) {
	normalized, err := normalizeNextCapabilityDirective(value)
	if err != nil {
		return "", err
	}
	raw, err := json.Marshal(normalized)
	if err != nil {
		return "", fmt.Errorf("marshal next-capability directive: %w", err)
	}
	return NextCapabilityDirectivePrefix + string(raw), nil
}

// ParseNextCapabilityDirective reads at most one directive line from trusted
// System content. Unknown fields, duplicate lines and trailing JSON fail closed.
func ParseNextCapabilityDirective(content string) (NextCapabilityDirective, bool, error) {
	var found *NextCapabilityDirective
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, NextCapabilityDirectivePrefix) {
			continue
		}
		if found != nil {
			return NextCapabilityDirective{}, false, fmt.Errorf("multiple next-capability directives")
		}
		raw := strings.TrimSpace(strings.TrimPrefix(line, NextCapabilityDirectivePrefix))
		decoder := json.NewDecoder(strings.NewReader(raw))
		decoder.DisallowUnknownFields()
		var value NextCapabilityDirective
		if err := decoder.Decode(&value); err != nil {
			return NextCapabilityDirective{}, false, fmt.Errorf("decode next-capability directive: %w", err)
		}
		var trailing any
		if err := decoder.Decode(&trailing); err == nil {
			return NextCapabilityDirective{}, false, fmt.Errorf("next-capability directive contains multiple JSON values")
		} else if !errors.Is(err, io.EOF) {
			return NextCapabilityDirective{}, false, fmt.Errorf("next-capability directive contains trailing data: %w", err)
		}
		normalized, err := normalizeNextCapabilityDirective(value)
		if err != nil {
			return NextCapabilityDirective{}, false, err
		}
		found = &normalized
	}
	if found == nil {
		return NextCapabilityDirective{}, false, nil
	}
	return *found, true, nil
}

func (value NextCapabilityDirective) StableCallID() (string, error) {
	encoded, err := EncodeNextCapabilityDirective(value)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256([]byte(encoded))
	return "call_dora_" + hex.EncodeToString(digest[:16]), nil
}

func normalizeNextCapabilityDirective(value NextCapabilityDirective) (NextCapabilityDirective, error) {
	if value.Version != NextCapabilityDirectiveVersion {
		return NextCapabilityDirective{}, fmt.Errorf("next-capability directive version must be %d", NextCapabilityDirectiveVersion)
	}
	value.SourceID = strings.TrimSpace(value.SourceID)
	value.Tool = strings.TrimSpace(value.Tool)
	if value.SourceID == "" || value.Tool == "" {
		return NextCapabilityDirective{}, fmt.Errorf("next-capability directive source_id and tool are required")
	}
	if len(value.Arguments) == 0 || !json.Valid(value.Arguments) {
		return NextCapabilityDirective{}, fmt.Errorf("next-capability directive arguments must be valid JSON")
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, value.Arguments); err != nil {
		return NextCapabilityDirective{}, fmt.Errorf("compact next-capability arguments: %w", err)
	}
	value.Arguments = append(json.RawMessage(nil), compact.Bytes()...)
	return value, nil
}
