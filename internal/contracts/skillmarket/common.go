package skillmarket

import (
	"fmt"
	"strings"

	"github.com/FigoGoo/Dora-Agent/internal/contracts/foundation"
)

const (
	AccountScopePersonal   = "personal"
	AccountScopeEnterprise = "enterprise"
)

func validatePrefixID(value, prefix string) error {
	if err := foundation.ValidateID(value); err != nil {
		return err
	}
	if !strings.HasPrefix(value, prefix) {
		return fmt.Errorf("must start with %s", prefix)
	}
	return nil
}

func isAllowed(value string, allowed []string) bool {
	for _, candidate := range allowed {
		if candidate == value {
			return true
		}
	}
	return false
}
