package analyzematerials

import (
	"encoding/json"
	"testing"
)

// TestEvidenceLocatorMarshalJSONPreservesFrozenZeroFields 固定真实 Trial 的 start=0，
// 并覆盖图片左上角 x=0/y=0，防止 omitempty 再次生成前端无法解析的投影。
func TestEvidenceLocatorMarshalJSONPreservesFrozenZeroFields(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		locator EvidenceLocator
		want    string
	}{
		{
			name:    "trial text range starts at zero",
			locator: EvidenceLocator{Kind: "text_range", Start: 0, End: 21, SourceLength: 21},
			want:    `{"kind":"text_range","start":0,"end":21,"source_length":21}`,
		},
		{
			name:    "image whole has no numeric fields",
			locator: EvidenceLocator{Kind: "image_whole"},
			want:    `{"kind":"image_whole"}`,
		},
		{
			name:    "image region starts at top left",
			locator: EvidenceLocator{Kind: "image_region", X: 0, Y: 0, Width: 6400, Height: 3600},
			want:    `{"kind":"image_region","x":0,"y":0,"width":6400,"height":3600}`,
		},
	}
	for _, testCase := range tests {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			encoded, err := json.Marshal(testCase.locator)
			if err != nil {
				t.Fatalf("json.Marshal() error=%v", err)
			}
			if string(encoded) != testCase.want {
				t.Fatalf("locator JSON=%s want=%s", encoded, testCase.want)
			}
		})
	}
}

// TestEvidenceLocatorMarshalJSONRejectsMixedVariants 确保内部字段误混时失败关闭，
// 避免序列化过程静默丢弃本应被 Validator 识别的错误状态。
func TestEvidenceLocatorMarshalJSONRejectsMixedVariants(t *testing.T) {
	t.Parallel()
	for _, locator := range []EvidenceLocator{
		{Kind: "text_range", Start: 0, End: 1, SourceLength: 1, Width: 1},
		{Kind: "image_whole", End: 1},
		{Kind: "image_region", Start: 1, Width: 1, Height: 1},
		{Kind: "future_locator"},
	} {
		if _, err := json.Marshal(locator); err == nil {
			t.Fatalf("混合或未知定位器应失败关闭: %+v", locator)
		}
	}
}
