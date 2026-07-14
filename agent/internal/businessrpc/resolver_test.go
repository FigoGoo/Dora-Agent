package businessrpc

import "testing"

func TestParseRegistrationInstance(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		value string
		want  bool
	}{
		{name: "valid hostname", value: `{"service":"dora.business.foundation.v1","instance_id":"business-1","address":"business.internal:19081","version":"dev"}`, want: true},
		{name: "malformed json", value: `{`, want: false},
		{name: "wrong service", value: `{"service":"other","instance_id":"business-1","address":"business.internal:19081","version":"dev"}`, want: false},
		{name: "missing identity", value: `{"service":"dora.business.foundation.v1","address":"business.internal:19081","version":"dev"}`, want: false},
		{name: "missing version", value: `{"service":"dora.business.foundation.v1","instance_id":"business-1","address":"business.internal:19081"}`, want: false},
		{name: "invalid address", value: `{"service":"dora.business.foundation.v1","instance_id":"business-1","address":"business.internal","version":"dev"}`, want: false},
		{name: "invalid port", value: `{"service":"dora.business.foundation.v1","instance_id":"business-1","address":"business.internal:not-a-port","version":"dev"}`, want: false},
		{name: "wildcard ipv4", value: `{"service":"dora.business.foundation.v1","instance_id":"business-1","address":"0.0.0.0:19081","version":"dev"}`, want: false},
		{name: "wildcard ipv6", value: `{"service":"dora.business.foundation.v1","instance_id":"business-1","address":"[::]:19081","version":"dev"}`, want: false},
		{name: "loopback hostname", value: `{"service":"dora.business.foundation.v1","instance_id":"business-1","address":"localhost:19081","version":"dev"}`, want: false},
		{name: "loopback ip", value: `{"service":"dora.business.foundation.v1","instance_id":"business-1","address":"127.0.0.1:19081","version":"dev"}`, want: false},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			instance, ok := parseRegistrationInstance("dora.business.foundation.v1", []byte(test.value))
			if ok != test.want {
				t.Fatalf("parseRegistrationInstance() ok = %v, want %v", ok, test.want)
			}
			if !test.want {
				if instance != nil {
					t.Fatal("invalid registration returned an instance")
				}
				return
			}
			if got := instance.Address().String(); got != "business.internal:19081" {
				t.Fatalf("instance address = %q", got)
			}
			if got, exists := instance.Tag("instance_id"); !exists || got != "business-1" {
				t.Fatalf("instance_id tag = %q, exists = %v", got, exists)
			}
		})
	}
}
