package event

import "time"

const (
	TypeAgentRunStarted      = "agent.run.started"
	TypeAgentSkillSelected   = "agent.skill.selected"
	TypeAgentSkillMissing    = "agent.skill.missing"
	TypePlatformTagsUpdated  = "platform.tags.updated"
	TypeSafetyEvaluating     = "safety.prompt.evaluating"
	TypeSafetyEvaluated      = "safety.prompt.evaluated"
	TypeSafetyBlocked        = "safety.prompt.blocked"
	TypeSafetyFailed         = "safety.prompt.failed"
	TypeConfirmationRequired = "confirmation.required"
	TypeGenerationProgress   = "generation.progress"
	TypeToolCallStarted      = "tool.call.started"
	TypeToolCallFailed       = "tool.call.failed"
)

type Envelope struct {
	EventID     string         `json:"event_id"`
	Type        string         `json:"type"`
	SessionID   string         `json:"session_id"`
	RunID       string         `json:"run_id"`
	ProjectID   string         `json:"project_id"`
	SpaceID     string         `json:"space_id"`
	ActorUserID string         `json:"actor_user_id"`
	Sequence    int64          `json:"sequence"`
	Timestamp   time.Time      `json:"timestamp"`
	Component   string         `json:"component"`
	TraceID     string         `json:"trace_id"`
	Payload     map[string]any `json:"payload"`
}

func IsCanonical(eventType string) bool {
	switch eventType {
	case TypeAgentRunStarted, TypeAgentSkillSelected, TypeAgentSkillMissing, TypePlatformTagsUpdated,
		TypeSafetyEvaluating, TypeSafetyEvaluated, TypeSafetyBlocked, TypeSafetyFailed,
		TypeConfirmationRequired, TypeGenerationProgress, TypeToolCallStarted, TypeToolCallFailed:
		return true
	default:
		return false
	}
}
