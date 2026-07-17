package analyzematerials

import "github.com/cloudwego/eino/schema"

// State 是 analyze_materials V2 Tool Core 的单次调用 Local State。
// 它不是业务真源，也不得写入普通日志、Trace 或长期 Checkpoint。
type State struct {
	TrustedContext      TrustedContext
	Intent              Intent
	IntentDigest        string
	AssetSnapshot       EvidenceSnapshot
	ReadyEvidence       []evidenceUnit
	IncludedEvidence    []evidenceUnit
	MissingRequirements []MissingRequirement
	Coverage            Coverage
	PromptMessages      []*schema.Message
	PromptDigest        string
	ModelMessage        *schema.Message
	Candidate           *Candidate
	CandidateDigest     string
	Failure             *failure
	Result              *Result
}
