package skilltest

type Case struct {
	CaseID           string
	ExpectedElements []string
}

type Input struct {
	Cases                []Case
	SafetyResult         string
	ActualElements       []string
	ActualElementDetails []ActualElement
	ElementTypes         []ElementTypeSpec
	Stage                string
}

type Result struct {
	Status            string
	Reason            string
	Missing           []string
	InvalidTypes      []string
	StageViolations   []string
	UnrenderableHints []string
}

type ElementTypeSpec struct {
	ElementType  string
	UsageStage   string
	DraftEnabled bool
	FinalEnabled bool
	Editable     bool
	Referable    bool
	RenderHint   string
	SchemaJSON   string
}

type ActualElement struct {
	ElementType string
	UsageStage  string
	RenderHint  string
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
	for _, element := range in.ActualElementDetails {
		actual[element.ElementType] = true
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
	specs := map[string]ElementTypeSpec{}
	for _, spec := range in.ElementTypes {
		if spec.ElementType != "" {
			specs[spec.ElementType] = spec
		}
	}
	if len(specs) > 0 {
		invalidTypes := []string{}
		for element := range actual {
			if _, ok := specs[element]; !ok {
				invalidTypes = append(invalidTypes, element)
			}
		}
		if len(invalidTypes) > 0 {
			return Result{Status: "failed", Reason: "invalid_element_types", InvalidTypes: invalidTypes}
		}
		stage := in.Stage
		if stage == "" {
			stage = "draft"
		}
		stageViolations := []string{}
		unrenderableHints := []string{}
		for _, element := range in.ActualElementDetails {
			spec, ok := specs[element.ElementType]
			if !ok {
				continue
			}
			elementStage := element.UsageStage
			if elementStage == "" {
				elementStage = stage
			}
			if !stageAllowed(spec, elementStage) {
				stageViolations = append(stageViolations, element.ElementType)
			}
			if spec.RenderHint != "" && element.RenderHint != spec.RenderHint {
				unrenderableHints = append(unrenderableHints, element.ElementType)
			}
		}
		if len(stageViolations) > 0 {
			return Result{Status: "failed", Reason: "stage_violations", StageViolations: stageViolations}
		}
		if len(unrenderableHints) > 0 {
			return Result{Status: "failed", Reason: "unrenderable_hints", UnrenderableHints: unrenderableHints}
		}
	}
	return Result{Status: "passed"}
}

func stageAllowed(spec ElementTypeSpec, stage string) bool {
	switch stage {
	case "draft":
		return spec.DraftEnabled && (spec.UsageStage == "" || spec.UsageStage == "draft" || spec.UsageStage == "draft_final")
	case "final":
		return spec.FinalEnabled && (spec.UsageStage == "" || spec.UsageStage == "final" || spec.UsageStage == "draft_final")
	default:
		return spec.DraftEnabled || spec.FinalEnabled
	}
}
