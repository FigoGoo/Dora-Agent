package server

import (
	"encoding/json"
	"strings"

	"github.com/cloudwego/eino/schema"

	"github.com/FigoGoo/Dora-Agent/internal/aigc/capability"
)

// authoritativeCapabilityProgressTracker correlates Tool results with the
// exact ToolCall that owns the stable system ToolRun. It suppresses a model
// recap only when the turn contains no other public tool whose result may need
// a user-visible explanation.
type authoritativeCapabilityProgressTracker struct {
	calls              map[string]string
	projected          map[string]bool
	targetSucceeded    bool
	sawOtherPublicTool bool
}

func newAuthoritativeCapabilityProgressTracker() *authoritativeCapabilityProgressTracker {
	return &authoritativeCapabilityProgressTracker{
		calls:     map[string]string{},
		projected: map[string]bool{},
	}
}

func (t *authoritativeCapabilityProgressTracker) Observe(message *schema.Message, progressPublished bool) {
	if t == nil || message == nil {
		return
	}
	switch message.Role {
	case schema.Assistant:
		for _, call := range message.ToolCalls {
			callID := strings.TrimSpace(call.ID)
			toolName := strings.TrimSpace(call.Function.Name)
			if isInternalToolProgress(toolName) {
				continue
			}
			if callID == "" || toolName == "" {
				t.sawOtherPublicTool = true
				continue
			}
			t.calls[callID] = toolName
			if isAuthoritativeCapabilityProgressTool(toolName) {
				if progressPublished {
					t.projected[callID] = true
				}
				continue
			}
			t.sawOtherPublicTool = true
		}
	case schema.Tool:
		callID := strings.TrimSpace(message.ToolCallID)
		toolName := strings.TrimSpace(message.ToolName)
		if isInternalToolProgress(toolName) {
			return
		}
		expectedName, known := t.calls[callID]
		if callID == "" || toolName == "" || !known || expectedName != toolName {
			t.sawOtherPublicTool = true
			return
		}
		if !isAuthoritativeCapabilityProgressTool(toolName) {
			t.sawOtherPublicTool = true
			return
		}
		if progressPublished {
			t.projected[callID] = true
		}
		if !t.projected[callID] || !successfulAsyncCapabilityResult(message.Content) {
			return
		}
		t.targetSucceeded = true
	}
}

func (t *authoritativeCapabilityProgressTracker) SuppressModelAssistant() bool {
	return t != nil && t.targetSucceeded && !t.sawOtherPublicTool
}

func isAuthoritativeCapabilityProgressTool(toolName string) bool {
	switch strings.TrimSpace(toolName) {
	case capability.GenerateMediaToolKey, capability.AssembleOutputToolKey:
		return true
	default:
		return false
	}
}

func successfulAsyncCapabilityResult(content string) bool {
	status, ok := capabilityResultStatus(content)
	if !ok {
		return false
	}
	return status == capability.StatusAccepted || status == capability.StatusCompleted
}

func capabilityResultStatus(content string) (string, bool) {
	var result struct {
		Status string `json:"status"`
	}
	content = strings.TrimSpace(content)
	if json.Unmarshal([]byte(content), &result) == nil && strings.TrimSpace(result.Status) != "" {
		return result.Status, true
	}
	preview, ok := persistedOutputFirstPreview(content)
	if !ok {
		return "", false
	}
	decoder := json.NewDecoder(strings.NewReader(preview))
	opening, err := decoder.Token()
	if err != nil || opening != json.Delim('{') {
		return "", false
	}
	for decoder.More() {
		keyToken, err := decoder.Token()
		if err != nil {
			return "", false
		}
		key, ok := keyToken.(string)
		if !ok {
			return "", false
		}
		if key == "status" {
			var status string
			if decoder.Decode(&status) != nil || strings.TrimSpace(status) == "" {
				return "", false
			}
			return status, true
		}
		var skipped json.RawMessage
		if decoder.Decode(&skipped) != nil {
			return "", false
		}
	}
	return "", false
}

func persistedOutputFirstPreview(content string) (string, bool) {
	if !strings.HasPrefix(content, "<persisted-output>") {
		return "", false
	}
	for _, markers := range [][2]string{
		{"Preview (first ", "\n\nPreview (last "},
		{"预览 (前 ", "\n\n预览 (后 "},
	} {
		markerIndex := strings.Index(content, markers[0])
		if markerIndex < 0 {
			continue
		}
		previewStart := strings.Index(content[markerIndex:], "):\n")
		if previewStart < 0 {
			continue
		}
		preview := content[markerIndex+previewStart+3:]
		if previewEnd := strings.Index(preview, markers[1]); previewEnd >= 0 {
			preview = preview[:previewEnd]
		}
		preview = strings.TrimSpace(preview)
		if preview != "" {
			return preview, true
		}
	}
	return "", false
}
