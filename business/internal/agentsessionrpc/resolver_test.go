package agentsessionrpc

import "testing"

func TestParseRegistrationInstance(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		value string
		want  bool
	}{
		{name: "valid", value: `{"service":"dora.agent.session.v1","instance_id":"agent-1","address":"agent.internal:19082","version":"dev"}`, want: true},
		{name: "malformed", value: `{`, want: false},
		{name: "wrong service", value: `{"service":"other","instance_id":"agent-1","address":"agent.internal:19082","version":"dev"}`, want: false},
		{name: "missing identity", value: `{"service":"dora.agent.session.v1","address":"agent.internal:19082","version":"dev"}`, want: false},
		{name: "wildcard", value: `{"service":"dora.agent.session.v1","instance_id":"agent-1","address":"0.0.0.0:19082","version":"dev"}`, want: false},
		{name: "loopback", value: `{"service":"dora.agent.session.v1","instance_id":"agent-1","address":"127.0.0.1:19082","version":"dev"}`, want: false},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			instance, ok := parseRegistrationInstance("dora.agent.session.v1", []byte(test.value))
			if ok != test.want {
				t.Fatalf("ok=%v want=%v", ok, test.want)
			}
			if test.want && instance.Address().String() != "agent.internal:19082" {
				t.Fatalf("unexpected address: %s", instance.Address())
			}
		})
	}
}
