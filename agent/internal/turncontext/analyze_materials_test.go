package turncontext

import (
	"context"
	"testing"
)

func TestMaterialAnalysisRuntimeContextRoundTrip(t *testing.T) {
	value := MaterialAnalysisRuntime{Owner: "owner", FenceToken: 3, IntentJSON: `{}`,
		Context: MaterialAnalysisTurnContext{SchemaVersion: MaterialAnalysisTurnContextSchemaVersion, Profile: MaterialAnalysisRuntimeProfile}}
	ctx := WithMaterialAnalysisRuntime(context.Background(), value)
	got, ok := MaterialAnalysisRuntimeFrom(ctx)
	if !ok || got.Owner != "owner" || got.FenceToken != 3 || got.Context.SchemaVersion != "analyze_materials.turn_context.v2preview1" {
		t.Fatalf("素材分析 Runtime Context 往返异常: %+v ok=%v", got, ok)
	}
}
