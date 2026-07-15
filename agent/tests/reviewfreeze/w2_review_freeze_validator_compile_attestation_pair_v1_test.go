package reviewfreeze_test

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
)

// reviewFreezeValidateCompileAttestationPairJSONV1 是 pair evaluator 的跨信任边界入口；
// 两份 statement 都必须先经过严格 JSON 解码。content bundle 解引用、逐份 input snapshot
// raw 校验、raw→projection 重算、issuer 签名、runner trust domain 和 envelope 防重放仍须
// 由上层 trust-root verifier 在调用本结构比较器前完成。
func reviewFreezeValidateCompileAttestationPairJSONV1(firstRaw, secondRaw []byte) error {
	first, err := reviewFreezeDecodeCompileAttestationStatementJSONV1(firstRaw)
	if err != nil {
		return fmt.Errorf("compile attestation pair builder_a: %w", err)
	}
	second, err := reviewFreezeDecodeCompileAttestationStatementJSONV1(secondRaw)
	if err != nil {
		return fmt.Errorf("compile attestation pair builder_b: %w", err)
	}
	return reviewFreezeValidateCompileAttestationPairV1(first, second)
}

// reviewFreezeValidateCompileAttestationPairV1 比较两个已严格解码并完成 statement 语义
// 校验的 clean-builder run。它只冻结 claim 的可复现性比较规则，不单独证明原始材料
// 存在、raw→projection 正确、builder 独立或任何 Review Freeze authority。
func reviewFreezeValidateCompileAttestationPairV1(first, second reviewFreezeValidatorCompileAttestationV1) error {
	if err := reviewFreezeValidateCompileAttestationStatementV1(first); err != nil {
		return fmt.Errorf("compile attestation pair builder_a: %w", err)
	}
	if err := reviewFreezeValidateCompileAttestationStatementV1(second); err != nil {
		return fmt.Errorf("compile attestation pair builder_b: %w", err)
	}
	if first.EvaluationRequest.BuilderSlot != "builder_a" || second.EvaluationRequest.BuilderSlot != "builder_b" {
		return fmt.Errorf("compile attestation pair builder slots=%q/%q want=builder_a/builder_b", first.EvaluationRequest.BuilderSlot, second.EvaluationRequest.BuilderSlot)
	}
	if first.EvaluationRequest.EvaluationID != second.EvaluationRequest.EvaluationID ||
		first.EvaluationRequest.PairingChallengeSHA256 != second.EvaluationRequest.PairingChallengeSHA256 ||
		first.EvaluationRequest.PairPolicySHA256 != second.EvaluationRequest.PairPolicySHA256 {
		return fmt.Errorf("compile attestation pair evaluation request 不一致")
	}
	if !reflect.DeepEqual(first.Subject, second.Subject) ||
		!reflect.DeepEqual(first.ExternalModules, second.ExternalModules) ||
		!reflect.DeepEqual(first.Environment, second.Environment) ||
		!reflect.DeepEqual(first.GoList, second.GoList) ||
		!reflect.DeepEqual(first.Toolchain, second.Toolchain) {
		return fmt.Errorf("compile attestation pair shared context 不一致")
	}

	firstRun, secondRun := first.BuilderRun, second.BuilderRun
	for _, identity := range []struct {
		name   string
		first  string
		second string
	}{
		{name: "builder_id", first: firstRun.BuilderID, second: secondRun.BuilderID},
		{name: "workspace_id", first: firstRun.WorkspaceID, second: secondRun.WorkspaceID},
		{name: "module_cache_id", first: firstRun.ModuleCacheID, second: secondRun.ModuleCacheID},
		{name: "build_cache_id", first: firstRun.BuildCacheID, second: secondRun.BuildCacheID},
	} {
		if identity.first == identity.second {
			return fmt.Errorf("compile attestation pair %s 必须不同=%q", identity.name, identity.first)
		}
	}
	if !reflect.DeepEqual(firstRun.InputSnapshotBeforeRef, secondRun.InputSnapshotBeforeRef) ||
		!reflect.DeepEqual(firstRun.InputSnapshotAfterRef, secondRun.InputSnapshotAfterRef) {
		return fmt.Errorf("compile attestation pair input snapshot 不一致")
	}
	if firstRun.GoListProjectionSHA != secondRun.GoListProjectionSHA {
		return fmt.Errorf("compile attestation pair go list projection 不一致")
	}
	if !reviewFreezeCompileAttestationPairEqualGoListInvocationV1(firstRun.GoListInvocation, secondRun.GoListInvocation) {
		return fmt.Errorf("compile attestation pair go list invocation policy 不一致")
	}
	if !reflect.DeepEqual(firstRun.ToolchainVersion, secondRun.ToolchainVersion) {
		return fmt.Errorf("compile attestation pair toolchain version evidence 不一致")
	}
	if !reflect.DeepEqual(firstRun.Compile, secondRun.Compile) {
		return fmt.Errorf("compile attestation pair artifact/buildinfo 不一致")
	}
	if !reflect.DeepEqual(firstRun.Test, secondRun.Test) {
		return fmt.Errorf("compile attestation pair test/sandbox evidence 不一致")
	}
	if !reflect.DeepEqual(firstRun.SBOM, secondRun.SBOM) {
		return fmt.Errorf("compile attestation pair deterministic SBOM 不一致")
	}
	return nil
}

// reviewFreezeCompileAttestationPairEqualGoListInvocationV1 在上层已分别复核两份 raw stream
// 与 canonical projection 的前提下，允许宿主字段导致的 raw 摘要差异；其余执行声明相同。
func reviewFreezeCompileAttestationPairEqualGoListInvocationV1(first, second reviewFreezeCompileAttestationInvocationV1) bool {
	first.StdoutSHA256, second.StdoutSHA256 = "", ""
	first.StdoutSizeBytes, second.StdoutSizeBytes = 0, 0
	return reflect.DeepEqual(first, second)
}

func reviewFreezeCompileAttestationPairFixtureV1(t *testing.T) (reviewFreezeValidatorCompileAttestationV1, reviewFreezeValidatorCompileAttestationV1) {
	t.Helper()
	first := reviewFreezeCompileAttestationFixtureStatementV1(t)
	second := reviewFreezeCompileAttestationFixtureDeepCopyV1(t, first)
	runs, _, _ := reviewFreezeValidateCompileAttestationBuilderFixtureV1()
	first.EvaluationRequest.BuilderSlot = "builder_a"
	first.BuilderRun = runs[0]
	first.BuilderRun.GoListProjectionSHA = first.GoList.ProjectionSHA256
	second.EvaluationRequest.BuilderSlot = "builder_b"
	second.BuilderRun = runs[1]
	second.BuilderRun.GoListProjectionSHA = second.GoList.ProjectionSHA256
	return first, second
}

func TestW2ReviewFreezeCompileAttestationPairV1(t *testing.T) {
	first, second := reviewFreezeCompileAttestationPairFixtureV1(t)
	if first.BuilderRun.GoListRawRef.SHA256 == second.BuilderRun.GoListRawRef.SHA256 {
		t.Fatal("pair fixture 必须覆盖两份不同 raw content identity 的结构比较")
	}
	if err := reviewFreezeValidateCompileAttestationPairJSONV1(
		reviewFreezeCompileAttestationFixtureMarshalV1(t, first),
		reviewFreezeCompileAttestationFixtureMarshalV1(t, second),
	); err != nil {
		t.Fatalf("valid compile attestation pair rejected: %v", err)
	}
}

func TestW2ReviewFreezeCompileAttestationPairAdversarialV1(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*reviewFreezeValidatorCompileAttestationV1, *reviewFreezeValidatorCompileAttestationV1)
		want   string
	}{
		{name: "same builder slot", mutate: func(first, second *reviewFreezeValidatorCompileAttestationV1) {
			second.EvaluationRequest.BuilderSlot = first.EvaluationRequest.BuilderSlot
		}, want: "builder slots"},
		{name: "evaluation replay", mutate: func(_ *reviewFreezeValidatorCompileAttestationV1, second *reviewFreezeValidatorCompileAttestationV1) {
			second.EvaluationRequest.PairingChallengeSHA256 = reviewFreezeSHA256V1([]byte("other challenge"))
		}, want: "evaluation request"},
		{name: "subject substitution", mutate: func(_ *reviewFreezeValidatorCompileAttestationV1, second *reviewFreezeValidatorCompileAttestationV1) {
			second.Subject.ContractManifestSHA256 = reviewFreezeSHA256V1([]byte("other valid manifest"))
		}, want: "shared context"},
		{name: "same builder", mutate: func(first, second *reviewFreezeValidatorCompileAttestationV1) {
			second.BuilderRun.BuilderID = first.BuilderRun.BuilderID
		}, want: "builder_id 必须不同"},
		{name: "same workspace", mutate: func(first, second *reviewFreezeValidatorCompileAttestationV1) {
			second.BuilderRun.WorkspaceID = first.BuilderRun.WorkspaceID
		}, want: "workspace_id 必须不同"},
		{name: "same module cache", mutate: func(first, second *reviewFreezeValidatorCompileAttestationV1) {
			second.BuilderRun.ModuleCacheID = first.BuilderRun.ModuleCacheID
		}, want: "module_cache_id 必须不同"},
		{name: "same build cache", mutate: func(first, second *reviewFreezeValidatorCompileAttestationV1) {
			second.BuilderRun.BuildCacheID = first.BuilderRun.BuildCacheID
		}, want: "build_cache_id 必须不同"},
		{name: "input snapshot differs", mutate: func(_ *reviewFreezeValidatorCompileAttestationV1, second *reviewFreezeValidatorCompileAttestationV1) {
			other := reviewFreezeCompileAttestationBuilderContentRefFixtureV1(
				reviewFreezeCompileAttestationInputSnapshotArtifactKindV1,
				reviewFreezeCompileAttestationInputSnapshotContentSchemaV1,
				reviewFreezeCompileAttestationInputSnapshotMediaTypeV1,
				[]byte("other valid snapshot"),
			)
			second.BuilderRun.InputSnapshotBeforeRef = other
			second.BuilderRun.InputSnapshotAfterRef = other
		}, want: "input snapshot"},
		{name: "artifact differs", mutate: func(_ *reviewFreezeValidatorCompileAttestationV1, second *reviewFreezeValidatorCompileAttestationV1) {
			other := reviewFreezeCompileAttestationBuilderContentRefFixtureV1(
				reviewFreezeCompileAttestationArtifactKindV1,
				reviewFreezeCompileAttestationArtifactContentSchemaV1,
				reviewFreezeCompileAttestationArtifactMediaTypeV1,
				[]byte("other valid artifact"),
			)
			second.BuilderRun.Compile.ArtifactRef = other
			second.BuilderRun.Test.PreExecutionArtifactSHA256 = other.SHA256
			second.BuilderRun.Test.PostExecutionArtifactSHA256 = other.SHA256
			second.BuilderRun.SBOM.SubjectArtifactSHA256 = other.SHA256
		}, want: "artifact/buildinfo"},
		{name: "sandbox differs", mutate: func(_ *reviewFreezeValidatorCompileAttestationV1, second *reviewFreezeValidatorCompileAttestationV1) {
			second.BuilderRun.Test.SandboxPolicySHA256 = reviewFreezeSHA256V1([]byte("other valid sandbox"))
		}, want: "test/sandbox"},
		{name: "deterministic SBOM differs", mutate: func(_ *reviewFreezeValidatorCompileAttestationV1, second *reviewFreezeValidatorCompileAttestationV1) {
			other := reviewFreezeCompileAttestationBuilderContentRefFixtureV1(
				reviewFreezeCompileAttestationSBOMRawArtifactKindV1,
				reviewFreezeCompileAttestationSBOMRawContentSchemaV1,
				reviewFreezeCompileAttestationSBOMRawMediaTypeV1,
				[]byte(`{"bomFormat":"CycloneDX","specVersion":"1.6","version":2}`),
			)
			second.BuilderRun.SBOM.RawRef = other
			second.BuilderRun.SBOM.Invocation.StdoutSHA256 = other.SHA256
			second.BuilderRun.SBOM.Invocation.StdoutSizeBytes = other.SizeBytes
		}, want: "deterministic SBOM"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			first, second := reviewFreezeCompileAttestationPairFixtureV1(t)
			test.mutate(&first, &second)
			err := reviewFreezeValidateCompileAttestationPairJSONV1(
				reviewFreezeCompileAttestationFixtureMarshalV1(t, first),
				reviewFreezeCompileAttestationFixtureMarshalV1(t, second),
			)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error=%v want contains %q", err, test.want)
			}
		})
	}
}

func TestW2ReviewFreezeCompileAttestationPairStrictJSONV1(t *testing.T) {
	first, second := reviewFreezeCompileAttestationPairFixtureV1(t)
	firstRaw := reviewFreezeCompileAttestationFixtureMarshalV1(t, first)
	secondRaw := reviewFreezeCompileAttestationFixtureMarshalV1(t, second)

	t.Run("unknown field", func(t *testing.T) {
		mutated := reviewFreezeCompileAttestationFixtureMutateJSONObjectV1(t, firstRaw, func(object map[string]any) {
			object["trusted"] = true
		})
		err := reviewFreezeValidateCompileAttestationPairJSONV1(mutated, secondRaw)
		if err == nil || !strings.Contains(err.Error(), "unknown field") {
			t.Fatalf("error=%v want unknown field", err)
		}
	})

	t.Run("duplicate field", func(t *testing.T) {
		mutated := append([]byte(`{"schema_version":"shadow",`), firstRaw[1:]...)
		err := reviewFreezeValidateCompileAttestationPairJSONV1(mutated, secondRaw)
		if err == nil || !strings.Contains(err.Error(), "duplicate field") {
			t.Fatalf("error=%v want duplicate field", err)
		}
	})
}
