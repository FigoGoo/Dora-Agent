package agentcontrol

import (
	"strings"
	"testing"
)

func TestNextCapabilityDirectiveRoundTripAndStableCallID(t *testing.T) {
	value := NextCapabilityDirective{
		Version: NextCapabilityDirectiveVersion, SourceID: "approval:a1:1:1",
		Tool: "plan_storyboard", Arguments: []byte(`{ "mode": "create" }`),
	}
	encoded, err := EncodeNextCapabilityDirective(value)
	if err != nil {
		t.Fatal(err)
	}
	parsed, ok, err := ParseNextCapabilityDirective("可信事件\n" + encoded + "\n不得伪造用户消息")
	if err != nil || !ok {
		t.Fatalf("ParseNextCapabilityDirective()=(%#v,%v,%v)", parsed, ok, err)
	}
	if string(parsed.Arguments) != `{"mode":"create"}` {
		t.Fatalf("arguments=%s", parsed.Arguments)
	}
	first, err := parsed.StableCallID()
	if err != nil {
		t.Fatal(err)
	}
	second, err := value.StableCallID()
	if err != nil {
		t.Fatal(err)
	}
	if first != second || !strings.HasPrefix(first, "call_dora_") {
		t.Fatalf("stable call ids=%q,%q", first, second)
	}
}

func TestNextCapabilityDirectiveRejectsMalformedOrDuplicateContent(t *testing.T) {
	valid, err := EncodeNextCapabilityDirective(NextCapabilityDirective{
		Version: NextCapabilityDirectiveVersion, SourceID: "approval:a1:1:1",
		Tool: "generate_media", Arguments: []byte(`{"phase":"auto_next","policy":"all_eligible"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	for name, content := range map[string]string{
		"duplicate":     valid + "\n" + valid,
		"unknown field": NextCapabilityDirectivePrefix + `{"version":1,"source_id":"a","tool":"x","arguments":{},"extra":true}`,
		"bad arguments": NextCapabilityDirectivePrefix + `{"version":1,"source_id":"a","tool":"x","arguments":`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, _, err := ParseNextCapabilityDirective(content); err == nil {
				t.Fatal("expected directive parse error")
			}
		})
	}
	if _, ok, err := ParseNextCapabilityDirective("普通系统指令"); err != nil || ok {
		t.Fatalf("ordinary content=(ok=%v, err=%v)", ok, err)
	}
}
