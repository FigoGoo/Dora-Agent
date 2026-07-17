package analyzematerials

import (
	"context"

	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
)

// validateAnalysisPrimary 在模型节点之后独立执行 strict Candidate 校验，非法候选通过 Branch 收口。
func (*graphBuilder) validateAnalysisPrimary(ctx context.Context, message *schema.Message) (analysisRoute, error) {
	if message == nil {
		return analysisRoute{}, contractErrorf(ResultCodeModelFailed, "validate primary analysis: model message is nil")
	}
	var snapshot State
	if err := compose.ProcessState[*State](ctx, func(_ context.Context, state *State) error {
		snapshot = *state
		snapshot.Intent = cloneIntent(state.Intent)
		snapshot.AssetSnapshot = cloneEvidenceSnapshot(state.AssetSnapshot)
		snapshot.IncludedEvidence = cloneEvidenceUnits(state.IncludedEvidence)
		snapshot.MissingRequirements = cloneMissingRequirements(state.MissingRequirements)
		snapshot.Coverage = cloneCoverage(state.Coverage)
		return nil
	}); err != nil {
		return analysisRoute{}, err
	}
	selected := selectedEvidence{
		ValidatedInput: validatedInput{
			TrustedContext: snapshot.TrustedContext,
			Intent:         snapshot.Intent,
			IntentDigest:   snapshot.IntentDigest,
		},
		Assets:   cloneAssetInputs(snapshot.AssetSnapshot.Assets),
		Included: cloneEvidenceUnits(snapshot.IncludedEvidence),
		Missing:  cloneMissingRequirements(snapshot.MissingRequirements),
	}
	candidate, err := DecodeAndValidateCandidate(
		[]byte(message.Content),
		snapshot.Intent,
		snapshot.Coverage,
		snapshot.IncludedEvidence,
		snapshot.MissingRequirements,
	)
	if err != nil {
		failed := &failure{
			Code:    ResultCodeModelOutputInvalid,
			Summary: safeSummaryForCode(ResultCodeModelOutputInvalid),
		}
		result := analysisRoute{
			Route:    routeCandidateInvalid,
			Selected: selected,
			Coverage: cloneCoverage(snapshot.Coverage),
			Failure:  failed,
		}
		if stateErr := compose.ProcessState[*State](ctx, func(_ context.Context, state *State) error {
			state.Candidate = nil
			state.CandidateDigest = ""
			state.Failure = cloneFailure(failed)
			return nil
		}); stateErr != nil {
			return analysisRoute{}, stateErr
		}
		return result, nil
	}
	digest, err := CandidateDigest(candidate)
	if err != nil {
		return analysisRoute{}, newContractError(ResultCodeInternal, err)
	}
	result := analysisRoute{
		Route:           routeCandidateValid,
		Selected:        selected,
		Coverage:        cloneCoverage(snapshot.Coverage),
		Candidate:       &candidate,
		CandidateDigest: digest,
	}
	if err := compose.ProcessState[*State](ctx, func(_ context.Context, state *State) error {
		candidateCopy := candidate
		state.Candidate = &candidateCopy
		state.CandidateDigest = digest
		state.Failure = nil
		return nil
	}); err != nil {
		return analysisRoute{}, err
	}
	return result, nil
}
