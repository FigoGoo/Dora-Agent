package foundation

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"
)

func TestStateEnumsMatchSchemaRegistry(t *testing.T) {
	var registry struct {
		Defs map[string]struct {
			Enum []string `json:"enum"`
		} `json:"$defs"`
	}
	data, err := os.ReadFile(filepath.Join(repoRoot(t), "tests/fixtures/contracts/common/state-enum-registry.schema.json"))
	if err != nil {
		t.Fatalf("read state schema: %v", err)
	}
	if err := json.Unmarshal(data, &registry); err != nil {
		t.Fatalf("unmarshal state schema: %v", err)
	}
	for enumName, values := range StateEnums {
		definition, ok := registry.Defs[enumName]
		if !ok {
			t.Fatalf("schema missing enum %s", enumName)
		}
		if !reflect.DeepEqual(values, definition.Enum) {
			t.Fatalf("%s mismatch\ncode:   %#v\nschema: %#v", enumName, values, definition.Enum)
		}
	}
	if len(StateEnums) != len(registry.Defs) {
		t.Fatalf("state enum count mismatch code=%d schema=%d", len(StateEnums), len(registry.Defs))
	}
}

func TestIsValidState(t *testing.T) {
	if !IsValidState(StateRunStatus, RunStatusRouting) {
		t.Fatalf("routing should be a valid run status")
	}
	if IsValidState(StateRunStatus, "pending") {
		t.Fatalf("pending is not a PR-1 RunStatus")
	}
}

func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatalf("runtime caller unavailable")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "../../.."))
}
