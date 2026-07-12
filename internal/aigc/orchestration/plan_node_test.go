package orchestration

import (
	"encoding/json"
	"testing"

	"github.com/FigoGoo/Dora-Agent/internal/aigc/capability"
)

func TestPlanNodeValidate(t *testing.T) {
	valid := PlanNode{Kind: NodeKindCapability, ToolKey: capability.GenerateMediaToolKey, Arguments: json.RawMessage(`{"phase":"auto_next"}`)}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid capability node: %v", err)
	}

	unknownKind := valid
	unknownKind.Kind = "weird"
	if err := unknownKind.Validate(); err == nil {
		t.Fatal("unknown kind must be rejected")
	}

	unknownCapability := valid
	unknownCapability.ToolKey = "not_a_capability"
	if err := unknownCapability.Validate(); err == nil {
		t.Fatal("capability node must reference a registered capability tool")
	}

	badArguments := valid
	badArguments.Arguments = json.RawMessage(`{`)
	if err := badArguments.Validate(); err == nil {
		t.Fatal("invalid JSON arguments must be rejected")
	}

	// atomic 为预留态：kind 合法、tool_key 非空即可（原子词汇表校验属 L2，
	// v1 无注册表可查；directive 白名单在 agent/next_capability.go 兜底）。
	atomic := PlanNode{Kind: NodeKindAtomic, ToolKey: "image_generate", Arguments: json.RawMessage(`{}`)}
	if err := atomic.Validate(); err != nil {
		t.Fatalf("reserved atomic node must pass schema validation: %v", err)
	}
	emptyAtomic := atomic
	emptyAtomic.ToolKey = " "
	if err := emptyAtomic.Validate(); err == nil {
		t.Fatal("atomic node must still name a tool")
	}
}
