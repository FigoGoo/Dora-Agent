package skilltest

type Case struct {
	CaseID           string
	ExpectedElements []string
}

type Input struct {
	Cases          []Case
	SafetyResult   string
	ActualElements []string
}

type Result struct {
	Status  string
	Reason  string
	Missing []string
}

type Runner struct{}

func NewRunner() Runner {
	return Runner{}
}

func (Runner) Run(in Input) Result {
	if len(in.Cases) < 3 {
		return Result{Status: "rejected", Reason: "less_than_three_cases"}
	}
	if in.SafetyResult == "blocked" {
		return Result{Status: "blocked", Reason: "safety_blocked"}
	}
	actual := map[string]bool{}
	for _, element := range in.ActualElements {
		actual[element] = true
	}
	missing := []string{}
	for _, testCase := range in.Cases {
		for _, expected := range testCase.ExpectedElements {
			if !actual[expected] {
				missing = append(missing, expected)
			}
		}
	}
	if len(missing) > 0 {
		return Result{Status: "failed", Reason: "missing_required_elements", Missing: missing}
	}
	return Result{Status: "passed"}
}
