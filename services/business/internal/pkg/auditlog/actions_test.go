package auditlog

import (
	"regexp"
	"testing"
)

func TestBusinessActionValuesAreCentralAndStable(t *testing.T) {
	actions := BusinessActionValues()
	if len(actions) == 0 {
		t.Fatal("expected business actions")
	}
	pattern := regexp.MustCompile(`^[a-z]+(\.[a-z_]+)+$`)
	seen := map[string]bool{}
	for _, action := range actions {
		if !pattern.MatchString(action) {
			t.Fatalf("business action %q must use module.action naming", action)
		}
		if seen[action] {
			t.Fatalf("duplicate business action %q", action)
		}
		seen[action] = true
		if !KnownBusinessAction(action) {
			t.Fatalf("KnownBusinessAction(%q) = false", action)
		}
	}

	expected := []string{
		ActionAuthRegister,
		ActionAdminBootstrap,
		ActionProjectCreate,
		ActionProjectArchive,
		ActionWorkPublicTakeDown,
		ActionEnterpriseMemberRemove,
	}
	for _, action := range expected {
		if !seen[action] {
			t.Fatalf("expected action %q in BusinessActionValues", action)
		}
	}
	if KnownBusinessAction("work.publish") {
		t.Fatal("unexpected unknown action accepted")
	}
}
