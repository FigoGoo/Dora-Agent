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
		{name: "wildcard ipv6", value: `{"service":"dora.agent.session.v1","instance_id":"agent-1","address":"[::]:19082","version":"dev"}`, want: false},
		{name: "loopback", value: `{"service":"dora.agent.session.v1","instance_id":"agent-1","address":"127.0.0.1:19082","version":"dev"}`, want: false},
		{name: "loopback hostname dot", value: `{"service":"dora.agent.session.v1","instance_id":"agent-1","address":"localhost.:19082","version":"dev"}`, want: false},
		{name: "loopback hostname suffix", value: `{"service":"dora.agent.session.v1","instance_id":"agent-1","address":"agent.localhost:19082","version":"dev"}`, want: false},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			instance, ok := parseRegistrationInstance("dora.agent.session.v1", []byte(test.value), false)
			if ok != test.want {
				t.Fatalf("ok=%v want=%v", ok, test.want)
			}
			if test.want && instance.Address().String() != "agent.internal:19082" {
				t.Fatalf("unexpected address: %s", instance.Address())
			}
		})
	}
}

func TestParseRegistrationInstanceAllowsLoopbackOnlyForApprovedLocalProfile(t *testing.T) {
	t.Parallel()

	for _, address := range []string{"127.0.0.1:19082", "[::1]:19082", "localhost:19082", "localhost.:19082", "agent.localhost:19082"} {
		value := []byte(`{"service":"dora.agent.session.v1","instance_id":"agent-local-1","address":"` + address + `","version":"dev"}`)
		instance, ok := parseRegistrationInstance("dora.agent.session.v1", value, true)
		if !ok || instance == nil || instance.Address().String() != address {
			t.Fatalf("local loopback registration %q 未被接受: instance=%v ok=%v", address, instance, ok)
		}
	}
	for _, address := range []string{"0.0.0.0:19082", "[::]:19082"} {
		wildcard := []byte(`{"service":"dora.agent.session.v1","instance_id":"agent-local-1","address":"` + address + `","version":"dev"}`)
		if instance, ok := parseRegistrationInstance("dora.agent.session.v1", wildcard, true); ok || instance != nil {
			t.Fatalf("local Profile 接受 wildcard %q: instance=%v ok=%v", address, instance, ok)
		}
	}
}

func TestClientConfigAllowsLoopbackOnlyForLocalStoryboardProfile(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name   string
		config ClientConfig
		want   bool
	}{
		{name: "local storyboard", config: ClientConfig{Environment: "LOCAL", PlanStoryboardRuntimeEnabled: true}, want: true},
		{name: "local write prompts", config: ClientConfig{Environment: "local", WritePromptsRuntimeEnabled: true}, want: true},
		{name: "local without storyboard", config: ClientConfig{Environment: "local"}, want: false},
		{name: "staging storyboard", config: ClientConfig{Environment: "staging", PlanStoryboardRuntimeEnabled: true}, want: false},
		{name: "production storyboard", config: ClientConfig{Environment: "production", PlanStoryboardRuntimeEnabled: true}, want: false},
		{name: "production write prompts", config: ClientConfig{Environment: "production", WritePromptsRuntimeEnabled: true}, want: false},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := allowLoopbackRegistration(test.config); got != test.want {
				t.Fatalf("allowLoopbackRegistration()=%v want=%v", got, test.want)
			}
		})
	}
}
