package pr1

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

const (
	SchemaVersionRouterDecision = "router_decision.v1"
	SchemaVersionAGUIEvent      = "agui.event.v1"
)

var (
	digestPattern = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)
	idPattern     = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_\-:.]*$`)
)

func ValidateDigest(value string) error {
	if !digestPattern.MatchString(strings.TrimSpace(value)) {
		return fmt.Errorf("invalid digest %q", value)
	}
	return nil
}

func IsValidDigest(value string) bool {
	return ValidateDigest(value) == nil
}

func CanonicalDigest(value any) (string, error) {
	body, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(body)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

func ValidateID(value string) error {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return errors.New("id is required")
	}
	if trimmed != value {
		return fmt.Errorf("invalid id %q", value)
	}
	if len(trimmed) > 128 || !idPattern.MatchString(trimmed) {
		return fmt.Errorf("invalid id %q", value)
	}
	return nil
}

func IsValidID(value string) bool {
	return ValidateID(value) == nil
}

func stringPtrValue(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}
