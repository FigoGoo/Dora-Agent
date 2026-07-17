package analyzematerials

import (
	"context"
	"encoding/json"
	"reflect"
	"sort"
	"strings"
	"testing"
)

func TestToolInfoStrictSchemaExcludesTrustedFields(t *testing.T) {
	t.Parallel()
	info, err := (&Tool{}).Info(context.Background())
	if err != nil {
		t.Fatalf("Info() error = %v", err)
	}
	if info.Name != ToolKey || info.Desc == "" || info.ParamsOneOf == nil {
		t.Fatalf("Info() = %+v", info)
	}
	intentSchema, err := info.ParamsOneOf.ToJSONSchema()
	if err != nil {
		t.Fatalf("ToJSONSchema() error = %v", err)
	}
	encoded, err := json.Marshal(intentSchema)
	if err != nil {
		t.Fatalf("marshal schema: %v", err)
	}
	var document map[string]any
	if err := json.Unmarshal(encoded, &document); err != nil {
		t.Fatalf("decode schema: %v", err)
	}
	assertSchemaObject(t, document, []string{
		"analysis_goal", "asset_ids", "expected_assets", "focus_dimensions", "output_language", "schema_version",
	}, []string{"analysis_goal", "asset_ids", "focus_dimensions", "output_language", "schema_version"})

	properties := document["properties"].(map[string]any)
	expectedAssets := properties["expected_assets"].(map[string]any)
	if expectedAssets["type"] != "array" || expectedAssets["minItems"] != float64(1) || expectedAssets["maxItems"] != float64(maxAssets) {
		t.Fatalf("expected_assets schema = %+v", expectedAssets)
	}
	expectedItem := expectedAssets["items"].(map[string]any)
	assertSchemaObject(t, expectedItem, []string{"asset_id", "asset_version"}, []string{"asset_id", "asset_version"})
	itemProperties := expectedItem["properties"].(map[string]any)
	if itemProperties["asset_id"].(map[string]any)["pattern"] != canonicalUUIDv7Pattern ||
		itemProperties["asset_version"].(map[string]any)["minimum"] != float64(1) {
		t.Fatalf("expected_assets item properties = %+v", itemProperties)
	}

	assetIDs := properties["asset_ids"].(map[string]any)
	if assetIDs["uniqueItems"] != true || assetIDs["minItems"] != float64(1) || assetIDs["maxItems"] != float64(maxAssets) ||
		assetIDs["items"].(map[string]any)["pattern"] != canonicalUUIDv7Pattern {
		t.Fatalf("asset_ids schema = %+v", assetIDs)
	}
	focus := properties["focus_dimensions"].(map[string]any)
	if focus["uniqueItems"] != true || focus["minItems"] != float64(1) || focus["maxItems"] != float64(4) {
		t.Fatalf("focus_dimensions schema = %+v", focus)
	}

	schemaText := string(encoded)
	for _, forbidden := range []string{
		"owner", "user_id", "project_id", "session_id", "input_id", "turn_id", "run_id", "tool_call_id",
		"fence_token", "prompt_version", "validator_version", "evidence_policy_version", "evidence_body",
	} {
		if strings.Contains(schemaText, `"`+forbidden+`"`) {
			t.Fatalf("Tool schema exposed trusted/internal field %q: %s", forbidden, schemaText)
		}
	}
}

func assertSchemaObject(t *testing.T, document map[string]any, wantProperties []string, wantRequired []string) {
	t.Helper()
	if document["type"] != "object" || document["additionalProperties"] != false {
		t.Fatalf("schema object is not strict: %+v", document)
	}
	properties, ok := document["properties"].(map[string]any)
	if !ok {
		t.Fatalf("schema properties = %#v", document["properties"])
	}
	propertyKeys := make([]string, 0, len(properties))
	for key := range properties {
		propertyKeys = append(propertyKeys, key)
	}
	sort.Strings(propertyKeys)
	sort.Strings(wantProperties)
	if !reflect.DeepEqual(propertyKeys, wantProperties) {
		t.Fatalf("schema property keys = %v, want %v", propertyKeys, wantProperties)
	}
	requiredValues, ok := document["required"].([]any)
	if !ok {
		t.Fatalf("schema required = %#v", document["required"])
	}
	required := make([]string, 0, len(requiredValues))
	for _, value := range requiredValues {
		required = append(required, value.(string))
	}
	sort.Strings(required)
	sort.Strings(wantRequired)
	if !reflect.DeepEqual(required, wantRequired) {
		t.Fatalf("schema required = %v, want %v", required, wantRequired)
	}
}
