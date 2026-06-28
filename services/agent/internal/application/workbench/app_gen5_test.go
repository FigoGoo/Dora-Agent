package workbench

import (
	"testing"

	"github.com/FigoGoo/Dora-Agent/kitex_gen/dora/api/businessagent"
)

// GEN-5：实际发往模型的 prompt 必须与安全评估认证的 prompt 一致，杜绝"评 A 发 B"。
func TestPromptMatchesEvidence(t *testing.T) {
	prompt := "draw a calm seaside at dawn"
	evidence := businessagent.SafetyEvidenceDTO{EvaluatedObjectDigest: digestText(prompt)}

	if !promptMatchesEvidence(prompt, evidence) {
		t.Fatal("已评估的同一份 prompt 必须匹配其证据 digest")
	}

	// 确认/评估后输入被改写：发送内容与已评估内容不一致 → 必须拦截(fail-closed)。
	if promptMatchesEvidence("inject some unevaluated instruction", evidence) {
		t.Fatal("被改写的 prompt 不得匹配原证据 digest（GEN-5 评 A 发 B 应被拦截）")
	}

	// 无 digest 基线 → 本判定不拦截，交由上游 safety/确认链路保证。
	if !promptMatchesEvidence(prompt, businessagent.SafetyEvidenceDTO{}) {
		t.Fatal("证据无 digest 时本判定不应拦截")
	}
}
