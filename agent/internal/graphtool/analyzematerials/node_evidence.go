package analyzematerials

import (
	"context"

	"github.com/cloudwego/eino/compose"
)

// normalizeEvidence 校验 Evidence 身份、定位与正文摘要，并产生 ready/missing exact-set。
func (*graphBuilder) normalizeEvidence(ctx context.Context, input loadedInputs) (normalizedEvidence, error) {
	assets, ready, missing, err := NormalizeEvidence(input.ValidatedInput.Intent, input.Snapshot)
	if err != nil {
		return normalizedEvidence{}, err
	}
	result := normalizedEvidence{
		ValidatedInput: cloneValidatedInput(input.ValidatedInput),
		Assets:         cloneAssetInputs(assets),
		Ready:          cloneEvidenceUnits(ready),
		Missing:        cloneMissingRequirements(missing),
	}
	if err := compose.ProcessState[*State](ctx, func(_ context.Context, state *State) error {
		state.ReadyEvidence = cloneEvidenceUnits(ready)
		state.MissingRequirements = cloneMissingRequirements(missing)
		return nil
	}); err != nil {
		return normalizedEvidence{}, err
	}
	return result, nil
}

// selectPromptEvidence 按稳定顺序选择完整 Evidence 单元，并把预算排除转成 missing requirement。
func (*graphBuilder) selectPromptEvidence(ctx context.Context, input normalizedEvidence) (selectedEvidence, error) {
	included, missing, err := SelectPromptEvidence(
		input.ValidatedInput.Intent,
		input.Assets,
		input.Ready,
		input.Missing,
	)
	if err != nil {
		return selectedEvidence{}, err
	}
	result := selectedEvidence{
		ValidatedInput: cloneValidatedInput(input.ValidatedInput),
		Assets:         cloneAssetInputs(input.Assets),
		Included:       cloneEvidenceUnits(included),
		Missing:        cloneMissingRequirements(missing),
	}
	if err := compose.ProcessState[*State](ctx, func(_ context.Context, state *State) error {
		state.IncludedEvidence = cloneEvidenceUnits(included)
		state.MissingRequirements = cloneMissingRequirements(missing)
		return nil
	}); err != nil {
		return selectedEvidence{}, err
	}
	return result, nil
}

// evaluateEvidenceGate 冻结 deterministic coverage；零 included Evidence 永不进入模型节点。
func (*graphBuilder) evaluateEvidenceGate(ctx context.Context, input selectedEvidence) (analysisRoute, error) {
	coverage, err := EvaluateCoverage(input.ValidatedInput.Intent, input.Assets, input.Included, input.Missing)
	if err != nil {
		return analysisRoute{}, err
	}
	result := analysisRoute{Selected: cloneSelectedEvidence(input), Coverage: cloneCoverage(coverage)}
	switch coverage.Status {
	case "completed", "partial":
		result.Route = routeAnalyze
	case "failed":
		result.Route = routeDependencyNotReady
		result.Failure = &failure{
			Code:    ResultCodeDependencyNotReady,
			Summary: safeSummaryForCode(ResultCodeDependencyNotReady),
		}
	default:
		return analysisRoute{}, contractErrorf(ResultCodeInternal, "evaluate evidence gate: invalid coverage status")
	}
	if err := compose.ProcessState[*State](ctx, func(_ context.Context, state *State) error {
		state.Coverage = cloneCoverage(coverage)
		state.Failure = cloneFailure(result.Failure)
		return nil
	}); err != nil {
		return analysisRoute{}, err
	}
	return result, nil
}

func cloneAssetInputs(input []AssetAnalysisInput) []AssetAnalysisInput {
	result := make([]AssetAnalysisInput, len(input))
	for index := range input {
		result[index] = input[index]
		result[index].Evidence = append([]EvidenceInput(nil), input[index].Evidence...)
	}
	return result
}

func cloneEvidenceUnits(input []evidenceUnit) []evidenceUnit {
	if input == nil {
		return nil
	}
	result := make([]evidenceUnit, len(input))
	copy(result, input)
	return result
}

func cloneMissingRequirements(input []MissingRequirement) []MissingRequirement {
	if input == nil {
		return nil
	}
	result := make([]MissingRequirement, len(input))
	copy(result, input)
	return result
}

func cloneCoverage(input Coverage) Coverage {
	result := input
	result.TargetAssetIDs = cloneStrings(input.TargetAssetIDs)
	result.AnalyzableAssetIDs = cloneStrings(input.AnalyzableAssetIDs)
	result.IncludedEvidenceIDs = cloneStrings(input.IncludedEvidenceIDs)
	result.MissingRequirements = cloneMissingRequirements(input.MissingRequirements)
	return result
}

func cloneSelectedEvidence(input selectedEvidence) selectedEvidence {
	input.ValidatedInput = cloneValidatedInput(input.ValidatedInput)
	input.Assets = cloneAssetInputs(input.Assets)
	input.Included = cloneEvidenceUnits(input.Included)
	input.Missing = cloneMissingRequirements(input.Missing)
	return input
}

func cloneFailure(input *failure) *failure {
	if input == nil {
		return nil
	}
	result := *input
	return &result
}
