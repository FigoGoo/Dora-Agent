package analyzematerials

import (
	"context"

	"github.com/cloudwego/eino/compose"
)

// validateIntent 严格解码模型可控 Intent，并冻结 Runtime 注入的可信上下文。
func (*graphBuilder) validateIntent(ctx context.Context, input GraphInput) (validatedInput, error) {
	if err := ValidateTrustedContext(input.TrustedContext); err != nil {
		return validatedInput{}, err
	}
	intent, err := DecodeIntent(input.IntentJSON)
	if err != nil {
		return validatedInput{}, err
	}
	query, err := BuildEvidenceQuery(input.TrustedContext, intent)
	if err != nil {
		return validatedInput{}, err
	}
	digest, err := IntentDigest(intent)
	if err != nil {
		return validatedInput{}, err
	}

	result := validatedInput{
		TrustedContext: input.TrustedContext,
		Intent:         cloneIntent(intent),
		IntentDigest:   digest,
		Targets:        cloneAssetTargets(query.Targets),
	}
	if err := compose.ProcessState[*State](ctx, func(_ context.Context, state *State) error {
		state.TrustedContext = input.TrustedContext
		state.Intent = cloneIntent(intent)
		state.IntentDigest = digest
		return nil
	}); err != nil {
		return validatedInput{}, err
	}
	return result, nil
}

func cloneIntent(input Intent) Intent {
	result := input
	result.AssetIDs = append([]string(nil), input.AssetIDs...)
	result.FocusDimensions = append([]string(nil), input.FocusDimensions...)
	result.ExpectedAssets = append([]ExpectedAsset(nil), input.ExpectedAssets...)
	return result
}

func cloneAssetTargets(input []AssetTarget) []AssetTarget {
	return append([]AssetTarget(nil), input...)
}
