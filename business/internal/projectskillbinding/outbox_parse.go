package projectskillbinding

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"

	"github.com/FigoGoo/Dora-Agent/business/internal/project"
)

// ParseCanonicalOutboxPayloadV2 严格解析 Dispatcher 解密后的 v2 plaintext，并逐层重算 Prompt、Runtime、Snapshot、Request 与 Payload digest。
// 成功结果只能短暂存在于 Dispatcher 调用栈；不得写入日志、Trace、Event、Receipt 或 Business 明文列。
func ParseCanonicalOutboxPayloadV2(plaintext []byte, expected OutboxExpectedV2, limits LimitsV1) (SessionBootstrapOutboxPayloadV2, error) {
	if err := limits.Validate(); err != nil || len(plaintext) == 0 || len(plaintext) > limits.MaxOutboxPlaintextBytes ||
		!isCanonicalUUIDv7(expected.CommandID) || !isCanonicalUUIDv7(expected.ProjectID) || !isCanonicalUUIDv7(expected.OwnerUserID) ||
		isZeroDigest(expected.RequestDigest) || isZeroDigest(expected.SnapshotDigest) || isZeroDigest(expected.PayloadDigest) ||
		expected.SkillCount < 0 || expected.SkillCount > limits.MaxItems {
		return SessionBootstrapOutboxPayloadV2{}, ErrSnapshotInvalid
	}
	decoder := json.NewDecoder(bytes.NewReader(plaintext))
	decoder.DisallowUnknownFields()
	var payload SessionBootstrapOutboxPayloadV2
	if err := decoder.Decode(&payload); err != nil {
		return SessionBootstrapOutboxPayloadV2{}, ErrSnapshotInvalid
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return SessionBootstrapOutboxPayloadV2{}, ErrSnapshotInvalid
	}
	canonical, payloadDigest, err := CanonicalOutboxPayloadV2(payload)
	if err != nil || !bytes.Equal(canonical, plaintext) || payloadDigest != expected.PayloadDigest {
		return SessionBootstrapOutboxPayloadV2{}, ErrSnapshotInvalid
	}
	if payload.CommandID != expected.CommandID || payload.ProjectID != expected.ProjectID || payload.OwnerUserID != expected.OwnerUserID ||
		payload.SkillSnapshot.SkillCount != expected.SkillCount || payload.SkillSnapshot.SnapshotSetDigest != expected.SnapshotDigest.Hex() {
		return SessionBootstrapOutboxPayloadV2{}, ErrSnapshotInvalid
	}
	normalizedPrompt, projectPromptDigest, promptPresent, err := project.NormalizeEnsureSessionPrompt(payload.InitialPrompt)
	if err != nil || normalizedPrompt != payload.InitialPrompt || promptPresent != payload.PromptPresent {
		return SessionBootstrapOutboxPayloadV2{}, ErrSnapshotInvalid
	}
	var promptDigest Digest
	copy(promptDigest[:], projectPromptDigest[:])
	promptDigestHex := ""
	if promptPresent {
		promptDigestHex = promptDigest.Hex()
	}
	if payload.PromptDigest != promptDigestHex {
		return SessionBootstrapOutboxPayloadV2{}, ErrSnapshotInvalid
	}
	totalRuntimeBytes := 0
	for _, item := range payload.SkillSnapshot.Skills {
		runtimeBytes, runtimeDigest, err := CanonicalRuntimeContentV1(item.RuntimeContent)
		if err != nil || runtimeDigest.Hex() != item.RuntimeContentDigest || len(runtimeBytes) > limits.MaxRuntimeContentBytesPerItem ||
			len(item.RuntimeContent.Examples) > limits.MaxExamplesPerItem || len(item.RuntimeContent.StarterPrompts) > limits.MaxStarterPromptsPerItem {
			return SessionBootstrapOutboxPayloadV2{}, ErrSnapshotInvalid
		}
		totalRuntimeBytes += len(runtimeBytes)
		if totalRuntimeBytes > limits.MaxTotalRuntimeContentBytes {
			return SessionBootstrapOutboxPayloadV2{}, ErrSnapshotLimitExceeded
		}
	}
	metadataBytes, snapshotDigest, err := CanonicalSnapshotSetV1(payload.SkillSnapshot.Skills)
	if err != nil || snapshotDigest != expected.SnapshotDigest || len(metadataBytes) > limits.MaxSnapshotMetadataBytes {
		return SessionBootstrapOutboxPayloadV2{}, ErrSnapshotInvalid
	}
	_, requestDigest, err := CanonicalEnsureRequestV2(
		payload.ProjectID, payload.OwnerUserID, payload.PromptPresent, promptDigest, payload.SkillSnapshot,
	)
	if err != nil || requestDigest != expected.RequestDigest || payload.RequestDigest != expected.RequestDigest.Hex() {
		return SessionBootstrapOutboxPayloadV2{}, ErrSnapshotInvalid
	}
	return payload, nil
}
