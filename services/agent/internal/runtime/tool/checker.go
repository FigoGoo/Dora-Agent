package tool

type Policy struct {
	Allowed              bool
	RiskLevel            string
	RequiresConfirmation bool
	TimeoutMS            int32
}

type Decision struct {
	Allowed              bool
	RequiresConfirmation bool
	Reason               string
}

type PolicyChecker struct{}

func NewPolicyChecker() PolicyChecker {
	return PolicyChecker{}
}

func (PolicyChecker) Decide(policy Policy) Decision {
	if !policy.Allowed {
		return Decision{Allowed: false, Reason: "policy_denied"}
	}
	if policy.RequiresConfirmation || policy.RiskLevel == "high" {
		return Decision{Allowed: true, RequiresConfirmation: true, Reason: "confirmation_required"}
	}
	return Decision{Allowed: true}
}
