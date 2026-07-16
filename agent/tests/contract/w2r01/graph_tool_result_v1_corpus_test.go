package w2r01_test

import "testing"

// TestW2R01CorpusManifest 确认 R01 corpus、设计源、validator 源和目标测试的 exact-set 闭包。
func TestW2R01CorpusManifest(t *testing.T) {
	runW2R01CorpusManifestV1(t)
}

// TestGraphToolResultV1Corpus 执行 Graph Tool Result v1 的全部接受与拒绝向量。
func TestGraphToolResultV1Corpus(t *testing.T) {
	runGraphToolResultV1Corpus(t)
}

// TestWarningIntegerPolicySafeBoundaryV1 确认 Warning 整数策略不得越过 JavaScript safe integer 边界。
func TestWarningIntegerPolicySafeBoundaryV1(t *testing.T) {
	runWarningIntegerPolicySafeBoundaryV1(t)
}
