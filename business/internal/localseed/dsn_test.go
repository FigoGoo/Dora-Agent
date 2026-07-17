package localseed

import "testing"

func TestIsSafeLocalBusinessDSNAcceptsOnlyDedicatedLoopbackURL(t *testing.T) {
	for _, valid := range []string{
		"postgres://dora_business_app:local-password@127.0.0.1:5432/dora_business?sslmode=disable",
		"postgres://dora_business_app:local-password@127.0.0.1:5432/dora_business_test?sslmode=disable",
	} {
		if !IsSafeLocalBusinessDSN(valid) {
			t.Fatalf("safe local Business DSN was rejected: %s", valid)
		}
	}
	for _, invalid := range []string{
		"postgres://dora_business_app:local-password@db.internal:5432/dora_business?sslmode=disable",
		"postgres://postgres:local-password@127.0.0.1:5432/dora_business?sslmode=disable",
		"postgres://dora_business_app:local-password@127.0.0.1:5432/production?sslmode=disable",
		"postgres://dora_business_app:local-password@127.0.0.1:5432/dora_business?sslmode=require",
		"postgres://dora_business_app:local-password@127.0.0.1:5432/dora_business?sslmode=disable&host=db.internal",
		"postgres://dora_business_app@127.0.0.1:5432/dora_business?sslmode=disable",
		"postgres://dora_business_app:local-password@127.0.0.1:5432/dora_business?sslmode=disable#fragment",
	} {
		if IsSafeLocalBusinessDSN(invalid) {
			t.Fatalf("unsafe local Business DSN was accepted: %s", invalid)
		}
	}
}
