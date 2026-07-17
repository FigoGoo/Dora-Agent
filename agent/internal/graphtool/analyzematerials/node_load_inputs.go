package analyzematerials

import (
	"context"

	"github.com/cloudwego/eino/compose"
)

// loadAssetInputs 只执行一次有界批读，并在进入 Evidence 管道前校验完整 exact-set。
func (b *graphBuilder) loadAssetInputs(ctx context.Context, input validatedInput) (loadedInputs, error) {
	query := EvidenceQuery{
		UserID:    input.TrustedContext.UserID,
		ProjectID: input.TrustedContext.ProjectID,
		Targets:   cloneAssetTargets(input.Targets),
	}
	snapshot, err := b.loader.BatchGetAssetAnalysisInputs(ctx, query)
	if err != nil {
		return loadedInputs{}, err
	}
	if err := ValidateEvidenceSnapshot(query, snapshot); err != nil {
		return loadedInputs{}, err
	}
	snapshot = cloneEvidenceSnapshot(snapshot)
	if err := compose.ProcessState[*State](ctx, func(_ context.Context, state *State) error {
		state.AssetSnapshot = cloneEvidenceSnapshot(snapshot)
		return nil
	}); err != nil {
		return loadedInputs{}, err
	}
	return loadedInputs{ValidatedInput: cloneValidatedInput(input), Snapshot: snapshot}, nil
}

func cloneValidatedInput(input validatedInput) validatedInput {
	input.Intent = cloneIntent(input.Intent)
	input.Targets = cloneAssetTargets(input.Targets)
	return input
}

func cloneEvidenceSnapshot(input EvidenceSnapshot) EvidenceSnapshot {
	result := input
	result.Assets = make([]AssetAnalysisInput, len(input.Assets))
	for index := range input.Assets {
		result.Assets[index] = input.Assets[index]
		result.Assets[index].Evidence = append([]EvidenceInput(nil), input.Assets[index].Evidence...)
	}
	return result
}
