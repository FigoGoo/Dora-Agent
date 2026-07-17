package analyzematerials

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
	"golang.org/x/text/unicode/norm"
)

const (
	missingReasonMissing         = "EVIDENCE_MISSING"
	missingReasonFailed          = "EVIDENCE_FAILED"
	missingReasonRedacted        = "EVIDENCE_REDACTED"
	missingReasonUnsupported     = "EVIDENCE_UNSUPPORTED"
	missingReasonBudgetTruncated = "EVIDENCE_BUDGET_TRUNCATED"

	maxCandidateItems       = 16
	maxEvidenceIDsPerItem   = 32
	maxCandidateTextRunes   = 2_000
	maxCandidateShortRunes  = 200
	maxCandidateDetailRunes = 1_000
)

var (
	localIDPattern    = regexp.MustCompile(`^[a-z][a-z0-9]*(?:_[a-z0-9]+)*$`)
	reasonCodePattern = regexp.MustCompile(`^[A-Z][A-Z0-9_]{0,63}$`)
)

// DecodeIntent 对原始 Tool JSON 执行大小、UTF-8、重复键、null、未知字段、尾随值与领域边界校验。
// 返回值中的集合已经规范排序，供后续 Query、Prompt 和摘要直接复用。
func DecodeIntent(encoded []byte) (Intent, error) {
	var intent Intent
	if err := decodeStrictJSON(encoded, maxIntentJSONBytes, &intent); err != nil {
		return Intent{}, newContractError(ResultCodeInvalidArgument, err)
	}
	if err := ValidateIntent(intent); err != nil {
		return Intent{}, err
	}
	return canonicalIntent(intent), nil
}

// ValidateIntent 校验冻结版本、UUIDv7、NFC 文本、枚举、非 null 数组、去重和 expected exact-set。
func ValidateIntent(intent Intent) error {
	if intent.SchemaVersion != IntentSchemaVersion || !validText(intent.AnalysisGoal, 1, 1_000, false) {
		return contractErrorf(ResultCodeInvalidArgument, "validate intent: invalid schema or analysis goal")
	}
	if len(intent.AssetIDs) < 1 || len(intent.AssetIDs) > maxAssets || intent.AssetIDs == nil {
		return contractErrorf(ResultCodeInvalidArgument, "validate intent: invalid asset count")
	}
	assetSet := make(map[string]struct{}, len(intent.AssetIDs))
	for _, assetID := range intent.AssetIDs {
		if !canonicalUUIDv7(assetID) {
			return contractErrorf(ResultCodeInvalidArgument, "validate intent: invalid asset id")
		}
		if _, duplicated := assetSet[assetID]; duplicated {
			return contractErrorf(ResultCodeInvalidArgument, "validate intent: duplicate asset id")
		}
		assetSet[assetID] = struct{}{}
	}
	if len(intent.FocusDimensions) < 1 || len(intent.FocusDimensions) > 4 || intent.FocusDimensions == nil {
		return contractErrorf(ResultCodeInvalidArgument, "validate intent: invalid focus count")
	}
	focusSet := make(map[string]struct{}, len(intent.FocusDimensions))
	for _, focus := range intent.FocusDimensions {
		if !validFocusDimension(focus) {
			return contractErrorf(ResultCodeInvalidArgument, "validate intent: invalid focus dimension")
		}
		if _, duplicated := focusSet[focus]; duplicated {
			return contractErrorf(ResultCodeInvalidArgument, "validate intent: duplicate focus dimension")
		}
		focusSet[focus] = struct{}{}
	}
	if !validOutputLanguage(intent.OutputLanguage) {
		return contractErrorf(ResultCodeInvalidArgument, "validate intent: invalid output language")
	}
	if intent.ExpectedAssets != nil {
		if len(intent.ExpectedAssets) != len(intent.AssetIDs) {
			return contractErrorf(ResultCodeInvalidArgument, "validate intent: expected asset exact-set mismatch")
		}
		expectedSet := make(map[string]struct{}, len(intent.ExpectedAssets))
		for _, expected := range intent.ExpectedAssets {
			if !canonicalUUIDv7(expected.AssetID) || expected.AssetVersion < 1 {
				return contractErrorf(ResultCodeInvalidArgument, "validate intent: invalid expected asset")
			}
			if _, target := assetSet[expected.AssetID]; !target {
				return contractErrorf(ResultCodeInvalidArgument, "validate intent: extra expected asset")
			}
			if _, duplicated := expectedSet[expected.AssetID]; duplicated {
				return contractErrorf(ResultCodeInvalidArgument, "validate intent: duplicate expected asset")
			}
			expectedSet[expected.AssetID] = struct{}{}
		}
	}
	return nil
}

// IntentDigest 对具名 wire DTO 计算排序无关的 Intent 小写 SHA-256。
func IntentDigest(intent Intent) (string, error) {
	if err := ValidateIntent(intent); err != nil {
		return "", err
	}
	return digestNamedWire(canonicalIntent(intent))
}

// ValidateTrustedContext 校验 Runtime 注入身份、Fence 与 Prompt/Validator/Policy pin。
func ValidateTrustedContext(trusted TrustedContext) error {
	if !validStructuredText(trusted.Owner, 1, 128, false) || trusted.FenceToken < 1 ||
		!canonicalUUIDv7(trusted.UserID) || !canonicalUUIDv7(trusted.ProjectID) ||
		!canonicalUUIDv7(trusted.SessionID) || !canonicalUUIDv7(trusted.InputID) ||
		!canonicalUUIDv7(trusted.TurnID) || !canonicalUUIDv7(trusted.RunID) ||
		!canonicalUUIDv7(trusted.ToolCallID) || trusted.PromptVersion != PromptVersion ||
		trusted.ValidatorVersion != ValidatorVersion || trusted.EvidencePolicyVersion != EvidencePolicyVersion {
		return contractErrorf(ResultCodeInvalidArgument, "validate trusted context: invalid identity, fence, or version pin")
	}
	return nil
}

// BuildEvidenceQuery 只把可信用户/Project 与规范排序后的 Asset Target 交给 Loader。
func BuildEvidenceQuery(trusted TrustedContext, intent Intent) (EvidenceQuery, error) {
	if err := ValidateTrustedContext(trusted); err != nil {
		return EvidenceQuery{}, err
	}
	if err := ValidateIntent(intent); err != nil {
		return EvidenceQuery{}, err
	}
	canonical := canonicalIntent(intent)
	expectedVersions := make(map[string]int64, len(canonical.ExpectedAssets))
	for _, expected := range canonical.ExpectedAssets {
		expectedVersions[expected.AssetID] = expected.AssetVersion
	}
	targets := make([]AssetTarget, 0, len(canonical.AssetIDs))
	for _, assetID := range canonical.AssetIDs {
		targets = append(targets, AssetTarget{AssetID: assetID, ExpectedVersion: expectedVersions[assetID]})
	}
	return EvidenceQuery{UserID: trusted.UserID, ProjectID: trusted.ProjectID, Targets: targets}, nil
}

// ValidateEvidenceSnapshot 校验 Loader 响应完整性、Asset exact-set、版本及全部 text/image Evidence。
func ValidateEvidenceSnapshot(query EvidenceQuery, snapshot EvidenceSnapshot) error {
	if !canonicalUUIDv7(query.UserID) || !canonicalUUIDv7(query.ProjectID) || len(query.Targets) < 1 || len(query.Targets) > maxAssets {
		return contractErrorf(ResultCodeSnapshotInvalid, "validate snapshot: invalid query identity or targets")
	}
	targets := make(map[string]int64, len(query.Targets))
	for _, target := range query.Targets {
		if !canonicalUUIDv7(target.AssetID) || target.ExpectedVersion < 0 {
			return contractErrorf(ResultCodeSnapshotInvalid, "validate snapshot: invalid query target")
		}
		if _, duplicated := targets[target.AssetID]; duplicated {
			return contractErrorf(ResultCodeSnapshotInvalid, "validate snapshot: duplicate query target")
		}
		targets[target.AssetID] = target.ExpectedVersion
	}
	return validateEvidenceSnapshotTargets(targets, snapshot)
}

// NormalizeEvidence 生成规范 Asset、全部合法 ready Evidence 与确定性 missing requirement。
func NormalizeEvidence(intent Intent, snapshot EvidenceSnapshot) ([]AssetAnalysisInput, []evidenceUnit, []MissingRequirement, error) {
	if err := ValidateIntent(intent); err != nil {
		return nil, nil, nil, err
	}
	targets := targetsFromIntent(intent)
	if err := validateEvidenceSnapshotTargets(targets, snapshot); err != nil {
		return nil, nil, nil, err
	}
	assets := cloneAndSortAssets(snapshot.Assets)
	ready := make([]evidenceUnit, 0, maxEvidence)
	missing := make([]MissingRequirement, 0)
	for _, asset := range assets {
		readyKinds := make(map[string]bool)
		unavailableReasons := make(map[string]string)
		for _, evidence := range asset.Evidence {
			if evidence.Availability == "ready" {
				readyKinds[evidence.EvidenceKind] = true
				ready = append(ready, evidenceUnit{Ref: evidenceRefFromInput(evidence), Content: evidence.Content})
				continue
			}
			reason := missingReasonForAvailability(evidence.Availability)
			if previous, exists := unavailableReasons[evidence.EvidenceKind]; !exists || reasonPriority(reason) > reasonPriority(previous) {
				unavailableReasons[evidence.EvidenceKind] = reason
			}
		}
		for _, focus := range canonicalIntent(intent).FocusDimensions {
			kind, supported := requiredEvidenceKind(asset.MediaType, focus)
			if !supported {
				missing = append(missing, newMissingRequirement(asset, focus, kind, missingReasonUnsupported))
				continue
			}
			if readyKinds[kind] {
				continue
			}
			reason := unavailableReasons[kind]
			if reason == "" {
				reason = missingReasonMissing
			}
			missing = append(missing, newMissingRequirement(asset, focus, kind, reason))
		}
	}
	sortEvidenceUnits(ready)
	missing = canonicalMissingRequirements(missing)
	return assets, copyEvidenceUnits(ready), copyMissingRequirements(missing), nil
}

// SelectPromptEvidence 按稳定复合键选择完整 Evidence 单元，并把预算排除写入 missing set。
func SelectPromptEvidence(intent Intent, assets []AssetAnalysisInput, ready []evidenceUnit, missing []MissingRequirement) ([]evidenceUnit, []MissingRequirement, error) {
	if err := ValidateIntent(intent); err != nil {
		return nil, nil, err
	}
	if len(assets) != len(intent.AssetIDs) {
		return nil, nil, contractErrorf(ResultCodeSnapshotInvalid, "select evidence: asset exact-set mismatch")
	}
	targetSet := stringSet(intent.AssetIDs)
	assetByID := make(map[string]AssetAnalysisInput, len(assets))
	for _, asset := range assets {
		if _, target := targetSet[asset.AssetID]; !target || !canonicalUUIDv7(asset.AssetID) ||
			asset.AssetVersion < 1 || !validMediaType(asset.MediaType) {
			return nil, nil, contractErrorf(ResultCodeSnapshotInvalid, "select evidence: invalid asset")
		}
		if _, duplicate := assetByID[asset.AssetID]; duplicate {
			return nil, nil, contractErrorf(ResultCodeSnapshotInvalid, "select evidence: duplicate asset")
		}
		assetByID[asset.AssetID] = asset
	}
	if err := validateMissingSet(canonicalMissingRequirements(missing), intent, assetByID); err != nil {
		return nil, nil, err
	}
	ordered := copyEvidenceUnits(ready)
	sortEvidenceUnits(ordered)
	included := make([]evidenceUnit, 0, len(ordered))
	combinedMissing := copyMissingRequirements(missing)
	missingByID := make(map[string]MissingRequirement, len(combinedMissing))
	for _, requirement := range combinedMissing {
		missingByID[requirement.RequirementID] = requirement
	}
	usedRunes := 0
	seenEvidence := make(map[string]struct{}, len(ordered))
	for _, unit := range ordered {
		if err := validateEvidenceUnit(unit); err != nil {
			return nil, nil, err
		}
		asset, exists := assetByID[unit.Ref.AssetID]
		if !exists || unit.Ref.AssetVersion != asset.AssetVersion || unit.Ref.MediaType != asset.MediaType {
			return nil, nil, contractErrorf(ResultCodeEvidenceConflict, "select evidence: ready evidence asset binding mismatch")
		}
		if _, duplicate := seenEvidence[unit.Ref.EvidenceID]; duplicate {
			return nil, nil, contractErrorf(ResultCodeEvidenceConflict, "select evidence: duplicate ready evidence")
		}
		seenEvidence[unit.Ref.EvidenceID] = struct{}{}
		contentRunes := utf8.RuneCountInString(unit.Content)
		if usedRunes+contentRunes <= maxPromptEvidenceRunes {
			included = append(included, unit)
			usedRunes += contentRunes
			continue
		}
		for _, focus := range canonicalIntent(intent).FocusDimensions {
			kind, supported := requiredEvidenceKind(asset.MediaType, focus)
			if !supported || kind != unit.Ref.EvidenceKind {
				continue
			}
			requirement := newMissingRequirement(asset, focus, kind, missingReasonBudgetTruncated)
			missingByID[requirement.RequirementID] = requirement
		}
	}
	combinedMissing = combinedMissing[:0]
	for _, requirement := range missingByID {
		combinedMissing = append(combinedMissing, requirement)
	}
	combinedMissing = canonicalMissingRequirements(combinedMissing)
	return copyEvidenceUnits(included), copyMissingRequirements(combinedMissing), nil
}

// EvaluateCoverage 冻结目标、可分析 Asset、included Evidence 与 missing 集合摘要，并确定 completed/partial/failed。
func EvaluateCoverage(intent Intent, assets []AssetAnalysisInput, included []evidenceUnit, missing []MissingRequirement) (Coverage, error) {
	if err := ValidateIntent(intent); err != nil {
		return Coverage{}, err
	}
	if len(assets) != len(intent.AssetIDs) {
		return Coverage{}, contractErrorf(ResultCodeSnapshotInvalid, "evaluate coverage: asset exact-set mismatch")
	}
	targetIDs := append([]string(nil), intent.AssetIDs...)
	sort.Strings(targetIDs)
	assetByID := make(map[string]AssetAnalysisInput, len(assets))
	for _, asset := range assets {
		assetByID[asset.AssetID] = asset
	}
	for _, targetID := range targetIDs {
		if _, exists := assetByID[targetID]; !exists {
			return Coverage{}, contractErrorf(ResultCodeSnapshotInvalid, "evaluate coverage: target asset missing")
		}
	}
	refs := make([]EvidenceRef, 0, len(included))
	analyzableSet := make(map[string]struct{})
	for _, unit := range included {
		if err := validateEvidenceUnit(unit); err != nil {
			return Coverage{}, err
		}
		if _, target := assetByID[unit.Ref.AssetID]; !target {
			return Coverage{}, contractErrorf(ResultCodeEvidenceConflict, "evaluate coverage: evidence outside target set")
		}
		refs = append(refs, cloneEvidenceRef(unit.Ref))
		analyzableSet[unit.Ref.AssetID] = struct{}{}
	}
	analyzableIDs := make([]string, 0, len(analyzableSet))
	for assetID := range analyzableSet {
		analyzableIDs = append(analyzableIDs, assetID)
	}
	sort.Strings(analyzableIDs)
	includedIDs := make([]string, 0, len(refs))
	for _, ref := range canonicalEvidenceRefs(refs) {
		includedIDs = append(includedIDs, ref.EvidenceID)
	}
	canonicalMissing := canonicalMissingRequirements(missing)
	if err := validateMissingSet(canonicalMissing, intent, assetByID); err != nil {
		return Coverage{}, err
	}
	targetDigest, err := targetAssetSetDigest(assets)
	if err != nil {
		return Coverage{}, err
	}
	includedDigest, err := evidenceRefSetDigest(refs)
	if err != nil {
		return Coverage{}, err
	}
	missingDigest, err := missingRequirementSetDigest(canonicalMissing)
	if err != nil {
		return Coverage{}, err
	}
	status := "completed"
	if len(includedIDs) == 0 || len(analyzableIDs) == 0 {
		status = "failed"
	} else if len(canonicalMissing) > 0 {
		status = "partial"
	}
	coverage := Coverage{
		Status: status, EvidencePolicyVersion: EvidencePolicyVersion,
		TargetAssetIDs: targetIDs, AnalyzableAssetIDs: analyzableIDs,
		IncludedEvidenceIDs: includedIDs, MissingRequirements: canonicalMissing,
		TargetAssetSetDigest: targetDigest, IncludedEvidenceSetDigest: includedDigest,
		MissingRequirementDigest: missingDigest,
	}
	if err := ValidateCoverage(coverage); err != nil {
		return Coverage{}, err
	}
	return copyCoverage(coverage), nil
}

// ValidateCoverage 校验 deterministic coverage 的判别联合、集合边界、排序和摘要形状。
func ValidateCoverage(coverage Coverage) error {
	if coverage.EvidencePolicyVersion != EvidencePolicyVersion || coverage.TargetAssetIDs == nil ||
		coverage.AnalyzableAssetIDs == nil || coverage.IncludedEvidenceIDs == nil || coverage.MissingRequirements == nil ||
		len(coverage.TargetAssetIDs) < 1 || len(coverage.TargetAssetIDs) > maxAssets ||
		!validLowerSHA256(coverage.TargetAssetSetDigest) || !validLowerSHA256(coverage.IncludedEvidenceSetDigest) ||
		!validLowerSHA256(coverage.MissingRequirementDigest) {
		return contractErrorf(ResultCodeInternal, "validate coverage: invalid version, collection, or digest")
	}
	if !validSortedUniqueUUIDs(coverage.TargetAssetIDs) || !validSortedUniqueUUIDs(coverage.AnalyzableAssetIDs) ||
		!validUniqueUUIDList(coverage.IncludedEvidenceIDs, 0, maxEvidence) {
		return contractErrorf(ResultCodeInternal, "validate coverage: invalid or unsorted exact-set")
	}
	targetSet := stringSet(coverage.TargetAssetIDs)
	for _, assetID := range coverage.AnalyzableAssetIDs {
		if _, target := targetSet[assetID]; !target {
			return contractErrorf(ResultCodeInternal, "validate coverage: analyzable asset outside target set")
		}
	}
	if !missingRequirementsAreCanonical(coverage.MissingRequirements) {
		return contractErrorf(ResultCodeInternal, "validate coverage: invalid missing requirement set")
	}
	recomputedMissing, err := missingRequirementSetDigest(coverage.MissingRequirements)
	if err != nil || recomputedMissing != coverage.MissingRequirementDigest {
		return contractErrorf(ResultCodeInternal, "validate coverage: missing requirement digest mismatch")
	}
	switch coverage.Status {
	case "completed":
		if len(coverage.AnalyzableAssetIDs) == 0 || len(coverage.IncludedEvidenceIDs) == 0 || len(coverage.MissingRequirements) != 0 {
			return contractErrorf(ResultCodeInternal, "validate coverage: invalid completed shape")
		}
	case "partial":
		if len(coverage.AnalyzableAssetIDs) == 0 || len(coverage.IncludedEvidenceIDs) == 0 || len(coverage.MissingRequirements) == 0 {
			return contractErrorf(ResultCodeInternal, "validate coverage: invalid partial shape")
		}
	case "failed":
		if len(coverage.AnalyzableAssetIDs) != 0 || len(coverage.IncludedEvidenceIDs) != 0 {
			return contractErrorf(ResultCodeInternal, "validate coverage: invalid failed shape")
		}
	default:
		return contractErrorf(ResultCodeInternal, "validate coverage: invalid status")
	}
	return nil
}

// DecodeAndValidateCandidate 严格解析模型 JSON，并关闭 Asset、Evidence、Observation 与 missing 引用。
func DecodeAndValidateCandidate(encoded []byte, intent Intent, coverage Coverage, included []evidenceUnit, missing []MissingRequirement) (Candidate, error) {
	if err := ValidateIntent(intent); err != nil {
		return Candidate{}, newContractError(ResultCodeModelOutputInvalid, err)
	}
	if err := ValidateCoverage(coverage); err != nil {
		return Candidate{}, newContractError(ResultCodeModelOutputInvalid, err)
	}
	if !sameMissingRequirements(canonicalMissingRequirements(missing), coverage.MissingRequirements) {
		return Candidate{}, contractErrorf(ResultCodeModelOutputInvalid, "validate candidate: missing set mismatch")
	}
	refs := make([]EvidenceRef, 0, len(included))
	for _, unit := range included {
		if err := validateEvidenceUnit(unit); err != nil {
			return Candidate{}, newContractError(ResultCodeModelOutputInvalid, err)
		}
		refs = append(refs, cloneEvidenceRef(unit.Ref))
	}
	if !sameStringSet(evidenceIDs(refs), coverage.IncludedEvidenceIDs) {
		return Candidate{}, contractErrorf(ResultCodeModelOutputInvalid, "validate candidate: included evidence set mismatch")
	}
	var candidate Candidate
	if err := decodeStrictJSON(encoded, maxCandidateJSONBytes, &candidate); err != nil {
		return Candidate{}, newContractError(ResultCodeModelOutputInvalid, err)
	}
	canonical := canonicalCandidate(candidate)
	if err := validateCandidate(canonical, coverage, refs); err != nil {
		return Candidate{}, err
	}
	return cloneCandidate(canonical), nil
}

// CandidateDigest 计算已通过结构边界的 Candidate 具名 JSON 小写 SHA-256。
func CandidateDigest(candidate Candidate) (string, error) {
	canonical := canonicalCandidate(candidate)
	if err := validateCandidateShape(canonical); err != nil {
		return "", err
	}
	return digestNamedWire(canonical)
}

// ValidateResult 校验 completed/partial/failed 判别联合及其 Candidate/Coverage/Evidence 闭合关系。
func ValidateResult(result Result) error {
	return validateResult(result, "")
}

// ValidateResultForContext 额外要求 Result 精确绑定当前可信 ToolCall。
func ValidateResultForContext(result Result, trusted TrustedContext) error {
	if err := ValidateTrustedContext(trusted); err != nil {
		return err
	}
	return validateResult(result, trusted.ToolCallID)
}

// CloneResult 深复制公开 Result，防止调用方修改已通过校验的集合和 Candidate。
func CloneResult(result Result) Result {
	cloned := result
	if result.Analysis != nil {
		candidate := cloneCandidate(*result.Analysis)
		cloned.Analysis = &candidate
	}
	if result.Coverage != nil {
		coverage := copyCoverage(*result.Coverage)
		cloned.Coverage = &coverage
	}
	cloned.EvidenceRefs = cloneEvidenceRefs(result.EvidenceRefs)
	if result.Retryable != nil {
		retryable := *result.Retryable
		cloned.Retryable = &retryable
	}
	return cloned
}

func validateResult(result Result, expectedToolCallID string) error {
	if result.SchemaVersion != ResultSchemaVersion || !canonicalUUIDv7(result.InvocationRef.ToolCallID) ||
		(expectedToolCallID != "" && result.InvocationRef.ToolCallID != expectedToolCallID) {
		return contractErrorf(ResultCodeInternal, "validate result: invalid schema or invocation ref")
	}
	switch result.Status {
	case "completed", "partial":
		expectedCode := ResultCodeCompleted
		if result.Status == "partial" {
			expectedCode = ResultCodePartial
		}
		if result.ResultCode != expectedCode || result.Analysis == nil || result.Coverage == nil ||
			result.Coverage.Status != result.Status || result.EvidenceRefs == nil || len(result.EvidenceRefs) == 0 ||
			result.Summary != "" || result.Retryable != nil {
			return contractErrorf(ResultCodeInternal, "validate result: invalid success shape")
		}
		if err := ValidateCoverage(*result.Coverage); err != nil {
			return err
		}
		refs := canonicalEvidenceRefs(result.EvidenceRefs)
		if len(refs) != len(result.EvidenceRefs) || !sameStringSet(evidenceIDs(refs), result.Coverage.IncludedEvidenceIDs) {
			return contractErrorf(ResultCodeInternal, "validate result: evidence exact-set mismatch")
		}
		for _, ref := range refs {
			if err := validateEvidenceRef(ref); err != nil {
				return newContractError(ResultCodeInternal, err)
			}
		}
		digest, err := evidenceRefSetDigest(refs)
		if err != nil || digest != result.Coverage.IncludedEvidenceSetDigest {
			return contractErrorf(ResultCodeInternal, "validate result: evidence digest mismatch")
		}
		if err := validateCandidate(canonicalCandidate(*result.Analysis), *result.Coverage, refs); err != nil {
			return newContractError(ResultCodeInternal, err)
		}
	case "failed":
		if !validFailureResultCode(result.ResultCode) || result.Analysis != nil || result.Coverage != nil ||
			len(result.EvidenceRefs) != 0 || result.Retryable == nil || *result.Retryable ||
			result.Summary != safeSummaryForCode(result.ResultCode) {
			return contractErrorf(ResultCodeInternal, "validate result: invalid failed shape")
		}
	default:
		return contractErrorf(ResultCodeInternal, "validate result: invalid status")
	}
	return nil
}

func validateEvidenceSnapshotTargets(targets map[string]int64, snapshot EvidenceSnapshot) error {
	if snapshot.SchemaVersion != EvidenceSnapshotSchemaVersion || !snapshot.ResponseComplete ||
		len(snapshot.Assets) != len(targets) || len(snapshot.Assets) > maxAssets ||
		(snapshot.SnapshotToken != "" && !validStructuredText(snapshot.SnapshotToken, 1, 256, false)) {
		return contractErrorf(ResultCodeSnapshotInvalid, "validate snapshot: incomplete or invalid envelope")
	}
	seenAssets := make(map[string]struct{}, len(snapshot.Assets))
	seenEvidence := make(map[string]struct{})
	locatorDigests := make(map[string]string)
	evidenceCount := 0
	for _, asset := range snapshot.Assets {
		expectedVersion, target := targets[asset.AssetID]
		if !target || !canonicalUUIDv7(asset.AssetID) || asset.AssetVersion < 1 || !validMediaType(asset.MediaType) ||
			(expectedVersion > 0 && asset.AssetVersion != expectedVersion) {
			return contractErrorf(ResultCodeSnapshotInvalid, "validate snapshot: asset identity, version, or media mismatch")
		}
		if _, duplicated := seenAssets[asset.AssetID]; duplicated {
			return contractErrorf(ResultCodeSnapshotInvalid, "validate snapshot: duplicate asset")
		}
		seenAssets[asset.AssetID] = struct{}{}
		evidenceCount += len(asset.Evidence)
		if evidenceCount > maxEvidence {
			return contractErrorf(ResultCodeSnapshotInvalid, "validate snapshot: evidence limit exceeded")
		}
		for _, evidence := range asset.Evidence {
			if err := validateEvidenceInput(asset, evidence); err != nil {
				return err
			}
			if _, duplicated := seenEvidence[evidence.EvidenceID]; duplicated {
				return contractErrorf(ResultCodeEvidenceConflict, "validate snapshot: duplicate evidence id")
			}
			seenEvidence[evidence.EvidenceID] = struct{}{}
			if evidence.Availability != "ready" {
				continue
			}
			locatorKey, err := locatorConflictKey(evidence)
			if err != nil {
				return newContractError(ResultCodeEvidenceConflict, err)
			}
			if previousDigest, exists := locatorDigests[locatorKey]; exists && previousDigest != evidence.ContentDigest {
				return contractErrorf(ResultCodeEvidenceConflict, "validate snapshot: same locator has conflicting content")
			}
			locatorDigests[locatorKey] = evidence.ContentDigest
		}
	}
	if len(seenAssets) != len(targets) {
		return contractErrorf(ResultCodeSnapshotInvalid, "validate snapshot: target exact-set is incomplete")
	}
	return nil
}

func validateEvidenceInput(asset AssetAnalysisInput, evidence EvidenceInput) error {
	if !canonicalUUIDv7(evidence.EvidenceID) || evidence.AssetID != asset.AssetID ||
		evidence.AssetVersion != asset.AssetVersion || evidence.MediaType != asset.MediaType ||
		!validEvidenceKind(evidence.MediaType, evidence.EvidenceKind) || !validAvailability(evidence.Availability) {
		return contractErrorf(ResultCodeEvidenceConflict, "validate evidence: invalid identity, ownership, kind, or availability")
	}
	if evidence.Availability == "ready" {
		if !validLowerSHA256(evidence.ContentDigest) ||
			!validStructuredText(evidence.ExtractorSchemaVersion, 1, 128, false) ||
			!validStructuredText(evidence.ExtractorVersion, 1, 128, false) ||
			!validText(evidence.Content, 1, maxEvidenceRunes, false) || evidence.ReasonCode != "" ||
			!validLocator(evidence.MediaType, evidence.EvidenceKind, evidence.Locator) ||
			sha256Hex([]byte(evidence.Content)) != evidence.ContentDigest {
			return contractErrorf(ResultCodeEvidenceConflict, "validate evidence: invalid ready evidence")
		}
		// content_digest 是后续引用闭合的权威边界；只校验格式会允许 Loader 把正文与摘要错配。
		actualDigest := sha256.Sum256([]byte(evidence.Content))
		if hex.EncodeToString(actualDigest[:]) != evidence.ContentDigest {
			return contractErrorf(ResultCodeEvidenceConflict, "validate evidence: content digest mismatch")
		}
		return nil
	}
	if !reasonCodePattern.MatchString(evidence.ReasonCode) || evidence.Content != "" ||
		(evidence.ContentDigest != "" && !validLowerSHA256(evidence.ContentDigest)) ||
		(evidence.ExtractorSchemaVersion != "" && !validStructuredText(evidence.ExtractorSchemaVersion, 1, 128, false)) ||
		(evidence.ExtractorVersion != "" && !validStructuredText(evidence.ExtractorVersion, 1, 128, false)) {
		return contractErrorf(ResultCodeEvidenceConflict, "validate evidence: invalid unavailable evidence")
	}
	if !emptyLocator(evidence.Locator) && !validLocator(evidence.MediaType, evidence.EvidenceKind, evidence.Locator) {
		return contractErrorf(ResultCodeEvidenceConflict, "validate evidence: invalid unavailable locator")
	}
	return nil
}

func validateEvidenceUnit(unit evidenceUnit) error {
	if err := validateEvidenceRef(unit.Ref); err != nil {
		return newContractError(ResultCodeEvidenceConflict, err)
	}
	if !validText(unit.Content, 1, maxEvidenceRunes, false) {
		return contractErrorf(ResultCodeEvidenceConflict, "validate evidence unit: invalid content")
	}
	return nil
}

func validateEvidenceRef(ref EvidenceRef) error {
	if !canonicalUUIDv7(ref.EvidenceID) || !canonicalUUIDv7(ref.AssetID) || ref.AssetVersion < 1 ||
		!validMediaType(ref.MediaType) || !validEvidenceKind(ref.MediaType, ref.EvidenceKind) ||
		!validLowerSHA256(ref.ContentDigest) || !validLocator(ref.MediaType, ref.EvidenceKind, ref.Locator) {
		return fmt.Errorf("validate evidence ref: invalid identity, digest, kind, or locator")
	}
	return nil
}

func validateCandidate(candidate Candidate, coverage Coverage, refs []EvidenceRef) error {
	if err := validateCandidateShape(candidate); err != nil {
		return err
	}
	if candidate.SchemaVersion != CandidateSchemaVersion || !sameStringSet(assetSummaryIDs(candidate.AssetSummaries), coverage.AnalyzableAssetIDs) {
		return contractErrorf(ResultCodeModelOutputInvalid, "validate candidate: analyzable asset exact-set mismatch")
	}
	targetSet := stringSet(coverage.TargetAssetIDs)
	refByID := make(map[string]EvidenceRef, len(refs))
	for _, ref := range refs {
		if err := validateEvidenceRef(ref); err != nil {
			return newContractError(ResultCodeModelOutputInvalid, err)
		}
		if _, duplicate := refByID[ref.EvidenceID]; duplicate {
			return contractErrorf(ResultCodeModelOutputInvalid, "validate candidate: duplicate included evidence")
		}
		refByID[ref.EvidenceID] = ref
	}
	referenced := make(map[string]struct{}, len(refByID))
	observationIDs := make(map[string]struct{})
	inferenceIDs := make(map[string]struct{})
	for _, summary := range candidate.AssetSummaries {
		localObservations := make(map[string]struct{}, len(summary.Observations))
		for _, observation := range summary.Observations {
			if _, duplicated := observationIDs[observation.ObservationID]; duplicated {
				return contractErrorf(ResultCodeModelOutputInvalid, "validate candidate: duplicate observation id")
			}
			observationIDs[observation.ObservationID] = struct{}{}
			localObservations[observation.ObservationID] = struct{}{}
			for _, evidenceID := range observation.EvidenceIDs {
				ref, exists := refByID[evidenceID]
				if !exists || ref.AssetID != summary.AssetID {
					return contractErrorf(ResultCodeModelOutputInvalid, "validate candidate: observation evidence is not from the same asset")
				}
				referenced[evidenceID] = struct{}{}
			}
		}
		for _, inference := range summary.Inferences {
			if _, duplicated := inferenceIDs[inference.InferenceID]; duplicated {
				return contractErrorf(ResultCodeModelOutputInvalid, "validate candidate: duplicate inference id")
			}
			inferenceIDs[inference.InferenceID] = struct{}{}
			for _, observationID := range inference.BasedOnObservationIDs {
				if _, local := localObservations[observationID]; !local {
					return contractErrorf(ResultCodeModelOutputInvalid, "validate candidate: inference references another asset observation")
				}
			}
		}
	}
	for _, finding := range candidate.CrossAssetFindings {
		findingAssets := stringSet(finding.AssetIDs)
		coveredAssets := make(map[string]struct{}, len(findingAssets))
		for assetID := range findingAssets {
			if _, analyzable := stringSet(coverage.AnalyzableAssetIDs)[assetID]; !analyzable {
				return contractErrorf(ResultCodeModelOutputInvalid, "validate candidate: finding references non-analyzable asset")
			}
		}
		for _, evidenceID := range finding.EvidenceIDs {
			ref, exists := refByID[evidenceID]
			if !exists {
				return contractErrorf(ResultCodeModelOutputInvalid, "validate candidate: finding references unknown evidence")
			}
			if _, declared := findingAssets[ref.AssetID]; !declared {
				return contractErrorf(ResultCodeModelOutputInvalid, "validate candidate: finding evidence is outside declared assets")
			}
			coveredAssets[ref.AssetID] = struct{}{}
			referenced[evidenceID] = struct{}{}
		}
		if len(coveredAssets) != len(findingAssets) {
			return contractErrorf(ResultCodeModelOutputInvalid, "validate candidate: finding evidence does not cover every asset")
		}
	}
	for _, element := range candidate.UsableElements {
		for _, evidenceID := range element.EvidenceIDs {
			if _, exists := refByID[evidenceID]; !exists {
				return contractErrorf(ResultCodeModelOutputInvalid, "validate candidate: usable element references unknown evidence")
			}
			referenced[evidenceID] = struct{}{}
		}
	}
	for _, risk := range candidate.Risks {
		for _, evidenceID := range risk.EvidenceIDs {
			if _, exists := refByID[evidenceID]; !exists {
				return contractErrorf(ResultCodeModelOutputInvalid, "validate candidate: risk references unknown evidence")
			}
			referenced[evidenceID] = struct{}{}
		}
	}
	missingByID := make(map[string]MissingRequirement, len(coverage.MissingRequirements))
	for _, requirement := range coverage.MissingRequirements {
		missingByID[requirement.RequirementID] = requirement
	}
	for _, question := range candidate.OpenQuestions {
		questionAssets := stringSet(question.AssetIDs)
		for assetID := range questionAssets {
			if _, target := targetSet[assetID]; !target {
				return contractErrorf(ResultCodeModelOutputInvalid, "validate candidate: question references non-target asset")
			}
		}
		for _, requirementID := range question.MissingRequirementIDs {
			requirement, exists := missingByID[requirementID]
			if !exists {
				return contractErrorf(ResultCodeModelOutputInvalid, "validate candidate: question references unknown missing requirement")
			}
			if _, declared := questionAssets[requirement.AssetID]; !declared {
				return contractErrorf(ResultCodeModelOutputInvalid, "validate candidate: question requirement asset is not declared")
			}
		}
	}
	unused := stringSet(candidate.UnusedEvidenceIDs)
	for evidenceID := range unused {
		if _, exists := refByID[evidenceID]; !exists {
			return contractErrorf(ResultCodeModelOutputInvalid, "validate candidate: unused evidence is not included")
		}
		if _, used := referenced[evidenceID]; used {
			return contractErrorf(ResultCodeModelOutputInvalid, "validate candidate: evidence is both referenced and unused")
		}
	}
	if len(referenced)+len(unused) != len(refByID) {
		return contractErrorf(ResultCodeModelOutputInvalid, "validate candidate: evidence complement is not exact")
	}
	return nil
}

func validateCandidateShape(candidate Candidate) error {
	if candidate.SchemaVersion != CandidateSchemaVersion || candidate.AssetSummaries == nil ||
		candidate.CrossAssetFindings == nil || candidate.UsableElements == nil || candidate.Risks == nil ||
		candidate.OpenQuestions == nil || candidate.UnusedEvidenceIDs == nil ||
		len(candidate.AssetSummaries) < 1 || len(candidate.AssetSummaries) > maxAssets ||
		len(candidate.CrossAssetFindings) > maxCandidateItems || len(candidate.UsableElements) > maxCandidateItems ||
		len(candidate.Risks) > maxCandidateItems || len(candidate.OpenQuestions) > maxCandidateItems ||
		len(candidate.UnusedEvidenceIDs) > maxEvidence {
		return contractErrorf(ResultCodeModelOutputInvalid, "validate candidate: invalid root shape")
	}
	summaryAssets := make(map[string]struct{}, len(candidate.AssetSummaries))
	observationIDs := make(map[string]struct{})
	inferenceIDs := make(map[string]struct{})
	for _, summary := range candidate.AssetSummaries {
		if !canonicalUUIDv7(summary.AssetID) || !validText(summary.Summary, 1, maxCandidateTextRunes, false) ||
			summary.Observations == nil || len(summary.Observations) < 1 || len(summary.Observations) > maxCandidateItems ||
			summary.Inferences == nil || len(summary.Inferences) > maxCandidateItems {
			return contractErrorf(ResultCodeModelOutputInvalid, "validate candidate: invalid asset summary")
		}
		if _, duplicated := summaryAssets[summary.AssetID]; duplicated {
			return contractErrorf(ResultCodeModelOutputInvalid, "validate candidate: duplicate asset summary")
		}
		summaryAssets[summary.AssetID] = struct{}{}
		for _, observation := range summary.Observations {
			if !validLocalID(observation.ObservationID) || !validText(observation.Text, 1, maxCandidateTextRunes, false) ||
				!validUniqueUUIDList(observation.EvidenceIDs, 1, maxEvidenceIDsPerItem) {
				return contractErrorf(ResultCodeModelOutputInvalid, "validate candidate: invalid observation")
			}
			if _, duplicate := observationIDs[observation.ObservationID]; duplicate {
				return contractErrorf(ResultCodeModelOutputInvalid, "validate candidate: duplicate observation id")
			}
			observationIDs[observation.ObservationID] = struct{}{}
		}
		for _, inference := range summary.Inferences {
			if !validLocalID(inference.InferenceID) || !validText(inference.Text, 1, maxCandidateTextRunes, false) ||
				!validUniqueLocalIDList(inference.BasedOnObservationIDs, 1, maxCandidateItems) ||
				!validConfidence(inference.Confidence) || !validText(inference.Uncertainty, 1, maxCandidateDetailRunes, false) {
				return contractErrorf(ResultCodeModelOutputInvalid, "validate candidate: invalid inference")
			}
			if _, duplicate := inferenceIDs[inference.InferenceID]; duplicate {
				return contractErrorf(ResultCodeModelOutputInvalid, "validate candidate: duplicate inference id")
			}
			inferenceIDs[inference.InferenceID] = struct{}{}
		}
	}
	if err := validateFindings(candidate.CrossAssetFindings); err != nil {
		return err
	}
	if err := validateUsableElements(candidate.UsableElements); err != nil {
		return err
	}
	if err := validateRisks(candidate.Risks); err != nil {
		return err
	}
	if err := validateOpenQuestions(candidate.OpenQuestions); err != nil {
		return err
	}
	if !validUniqueUUIDList(candidate.UnusedEvidenceIDs, 0, maxEvidence) {
		return contractErrorf(ResultCodeModelOutputInvalid, "validate candidate: invalid unused evidence ids")
	}
	return nil
}

func validateFindings(findings []CrossAssetFinding) error {
	seen := make(map[string]struct{}, len(findings))
	for _, finding := range findings {
		if !validLocalID(finding.FindingID) || !validLocalID(finding.FindingType) ||
			!validText(finding.Text, 1, maxCandidateTextRunes, false) || !validUniqueUUIDList(finding.AssetIDs, 2, maxAssets) ||
			!validUniqueUUIDList(finding.EvidenceIDs, 2, maxEvidenceIDsPerItem) || !validConfidence(finding.Confidence) ||
			!validText(finding.Uncertainty, 1, maxCandidateDetailRunes, false) {
			return contractErrorf(ResultCodeModelOutputInvalid, "validate candidate: invalid cross-asset finding")
		}
		if _, duplicate := seen[finding.FindingID]; duplicate {
			return contractErrorf(ResultCodeModelOutputInvalid, "validate candidate: duplicate finding id")
		}
		seen[finding.FindingID] = struct{}{}
	}
	return nil
}

func validateUsableElements(elements []UsableElement) error {
	seen := make(map[string]struct{}, len(elements))
	for _, element := range elements {
		if !validLocalID(element.ElementID) || !validText(element.Label, 1, maxCandidateShortRunes, false) ||
			!validText(element.Description, 1, maxCandidateDetailRunes, false) ||
			!validUniqueUUIDList(element.EvidenceIDs, 1, maxEvidenceIDsPerItem) || element.Constraints == nil ||
			len(element.Constraints) > 8 || !validUniqueTexts(element.Constraints, 1, maxCandidateShortRunes) {
			return contractErrorf(ResultCodeModelOutputInvalid, "validate candidate: invalid usable element")
		}
		if _, duplicate := seen[element.ElementID]; duplicate {
			return contractErrorf(ResultCodeModelOutputInvalid, "validate candidate: duplicate element id")
		}
		seen[element.ElementID] = struct{}{}
	}
	return nil
}

func validateRisks(risks []Risk) error {
	seen := make(map[string]struct{}, len(risks))
	for _, risk := range risks {
		if !validLocalID(risk.RiskID) || !validRiskCategory(risk.Category) ||
			!validText(risk.Statement, 1, maxCandidateTextRunes, false) ||
			!validUniqueUUIDList(risk.EvidenceIDs, 1, maxEvidenceIDsPerItem) || !validSeverity(risk.Severity) ||
			!validText(risk.Uncertainty, 1, maxCandidateDetailRunes, false) {
			return contractErrorf(ResultCodeModelOutputInvalid, "validate candidate: invalid risk")
		}
		if _, duplicate := seen[risk.RiskID]; duplicate {
			return contractErrorf(ResultCodeModelOutputInvalid, "validate candidate: duplicate risk id")
		}
		seen[risk.RiskID] = struct{}{}
	}
	return nil
}

func validateOpenQuestions(questions []OpenQuestion) error {
	seen := make(map[string]struct{}, len(questions))
	for _, question := range questions {
		if !validLocalID(question.QuestionID) || !validText(question.Question, 1, maxCandidateDetailRunes, false) ||
			!validUniqueUUIDList(question.AssetIDs, 1, maxAssets) ||
			!validUniqueSHA256List(question.MissingRequirementIDs, 1, maxEvidence) {
			return contractErrorf(ResultCodeModelOutputInvalid, "validate candidate: invalid open question")
		}
		if _, duplicate := seen[question.QuestionID]; duplicate {
			return contractErrorf(ResultCodeModelOutputInvalid, "validate candidate: duplicate question id")
		}
		seen[question.QuestionID] = struct{}{}
	}
	return nil
}

func validateMissingSet(missing []MissingRequirement, intent Intent, assets map[string]AssetAnalysisInput) error {
	focusSet := stringSet(intent.FocusDimensions)
	seen := make(map[string]struct{}, len(missing))
	for _, requirement := range missing {
		asset, target := assets[requirement.AssetID]
		if !target || requirement.AssetVersion != asset.AssetVersion {
			return contractErrorf(ResultCodeEvidenceConflict, "validate missing requirement: asset mismatch")
		}
		if _, allowed := focusSet[requirement.FocusDimension]; !allowed {
			return contractErrorf(ResultCodeEvidenceConflict, "validate missing requirement: focus mismatch")
		}
		kind, _ := requiredEvidenceKind(asset.MediaType, requirement.FocusDimension)
		if requirement.EvidenceKind != kind || !validMissingRequirement(requirement) {
			return contractErrorf(ResultCodeEvidenceConflict, "validate missing requirement: invalid kind or digest")
		}
		if _, duplicate := seen[requirement.RequirementID]; duplicate {
			return contractErrorf(ResultCodeEvidenceConflict, "validate missing requirement: duplicate requirement")
		}
		seen[requirement.RequirementID] = struct{}{}
	}
	return nil
}

func validMissingRequirement(requirement MissingRequirement) bool {
	if !canonicalUUIDv7(requirement.AssetID) || requirement.AssetVersion < 1 ||
		!validFocusDimension(requirement.FocusDimension) || requirement.EvidenceKind == "" ||
		!reasonCodePattern.MatchString(requirement.ReasonCode) || !validLowerSHA256(requirement.RequirementID) {
		return false
	}
	expected, err := requirementID(requirement.AssetID, requirement.AssetVersion, requirement.FocusDimension, requirement.EvidenceKind)
	return err == nil && expected == requirement.RequirementID
}

func requiredEvidenceKind(mediaType, focus string) (string, bool) {
	switch mediaType {
	case "text":
		if focus == "visual" {
			return "visual_description", false
		}
		return "text_segment", focus == "content" || focus == "narrative" || focus == "risk"
	case "image":
		if focus == "risk" {
			return "safety_label", true
		}
		return "visual_description", focus == "content" || focus == "visual" || focus == "narrative"
	default:
		return "unsupported", false
	}
}

func newMissingRequirement(asset AssetAnalysisInput, focus, kind, reason string) MissingRequirement {
	id, _ := requirementID(asset.AssetID, asset.AssetVersion, focus, kind)
	return MissingRequirement{RequirementID: id, AssetID: asset.AssetID, AssetVersion: asset.AssetVersion, FocusDimension: focus, EvidenceKind: kind, ReasonCode: reason}
}

func requirementID(assetID string, assetVersion int64, focus, kind string) (string, error) {
	wire := struct {
		AssetID        string `json:"asset_id"`
		AssetVersion   int64  `json:"asset_version"`
		FocusDimension string `json:"focus_dimension"`
		EvidenceKind   string `json:"evidence_kind"`
	}{assetID, assetVersion, focus, kind}
	return digestNamedWire(wire)
}

func targetAssetSetDigest(assets []AssetAnalysisInput) (string, error) {
	type targetWire struct {
		AssetID      string `json:"asset_id"`
		AssetVersion int64  `json:"asset_version"`
		MediaType    string `json:"media_type"`
	}
	ordered := cloneAndSortAssets(assets)
	wire := make([]targetWire, 0, len(ordered))
	for _, asset := range ordered {
		wire = append(wire, targetWire{AssetID: asset.AssetID, AssetVersion: asset.AssetVersion, MediaType: asset.MediaType})
	}
	return digestNamedWire(wire)
}

func evidenceRefSetDigest(refs []EvidenceRef) (string, error) {
	return digestNamedWire(canonicalEvidenceRefs(refs))
}

func missingRequirementSetDigest(missing []MissingRequirement) (string, error) {
	return digestNamedWire(canonicalMissingRequirements(missing))
}

func digestNamedWire(value any) (string, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", contractErrorf(ResultCodeInternal, "canonical digest: encode named wire: %v", err)
	}
	return sha256Hex(encoded), nil
}

func sha256Hex(value []byte) string {
	digest := sha256.Sum256(value)
	return hex.EncodeToString(digest[:])
}

func canonicalIntent(intent Intent) Intent {
	cloned := intent
	cloned.AssetIDs = cloneStrings(intent.AssetIDs)
	sort.Strings(cloned.AssetIDs)
	cloned.FocusDimensions = cloneStrings(intent.FocusDimensions)
	sort.Strings(cloned.FocusDimensions)
	if intent.ExpectedAssets != nil {
		cloned.ExpectedAssets = append([]ExpectedAsset(nil), intent.ExpectedAssets...)
		sort.Slice(cloned.ExpectedAssets, func(left, right int) bool {
			return cloned.ExpectedAssets[left].AssetID < cloned.ExpectedAssets[right].AssetID
		})
	}
	return cloned
}

func canonicalCandidate(candidate Candidate) Candidate {
	cloned := cloneCandidate(candidate)
	sort.Slice(cloned.AssetSummaries, func(left, right int) bool {
		return cloned.AssetSummaries[left].AssetID < cloned.AssetSummaries[right].AssetID
	})
	for summaryIndex := range cloned.AssetSummaries {
		for observationIndex := range cloned.AssetSummaries[summaryIndex].Observations {
			sort.Strings(cloned.AssetSummaries[summaryIndex].Observations[observationIndex].EvidenceIDs)
		}
		for inferenceIndex := range cloned.AssetSummaries[summaryIndex].Inferences {
			sort.Strings(cloned.AssetSummaries[summaryIndex].Inferences[inferenceIndex].BasedOnObservationIDs)
		}
	}
	for index := range cloned.CrossAssetFindings {
		sort.Strings(cloned.CrossAssetFindings[index].AssetIDs)
		sort.Strings(cloned.CrossAssetFindings[index].EvidenceIDs)
	}
	for index := range cloned.UsableElements {
		sort.Strings(cloned.UsableElements[index].EvidenceIDs)
	}
	for index := range cloned.Risks {
		sort.Strings(cloned.Risks[index].EvidenceIDs)
	}
	for index := range cloned.OpenQuestions {
		sort.Strings(cloned.OpenQuestions[index].AssetIDs)
		sort.Strings(cloned.OpenQuestions[index].MissingRequirementIDs)
	}
	sort.Strings(cloned.UnusedEvidenceIDs)
	return cloned
}

func cloneCandidate(candidate Candidate) Candidate {
	cloned := candidate
	cloned.AssetSummaries = make([]AssetSummary, len(candidate.AssetSummaries))
	for index, summary := range candidate.AssetSummaries {
		cloned.AssetSummaries[index] = summary
		cloned.AssetSummaries[index].Observations = make([]Observation, len(summary.Observations))
		for observationIndex, observation := range summary.Observations {
			cloned.AssetSummaries[index].Observations[observationIndex] = observation
			cloned.AssetSummaries[index].Observations[observationIndex].EvidenceIDs = cloneStrings(observation.EvidenceIDs)
		}
		cloned.AssetSummaries[index].Inferences = make([]Inference, len(summary.Inferences))
		for inferenceIndex, inference := range summary.Inferences {
			cloned.AssetSummaries[index].Inferences[inferenceIndex] = inference
			cloned.AssetSummaries[index].Inferences[inferenceIndex].BasedOnObservationIDs = cloneStrings(inference.BasedOnObservationIDs)
		}
	}
	cloned.CrossAssetFindings = make([]CrossAssetFinding, len(candidate.CrossAssetFindings))
	copy(cloned.CrossAssetFindings, candidate.CrossAssetFindings)
	for index := range cloned.CrossAssetFindings {
		cloned.CrossAssetFindings[index].AssetIDs = cloneStrings(candidate.CrossAssetFindings[index].AssetIDs)
		cloned.CrossAssetFindings[index].EvidenceIDs = cloneStrings(candidate.CrossAssetFindings[index].EvidenceIDs)
	}
	cloned.UsableElements = make([]UsableElement, len(candidate.UsableElements))
	copy(cloned.UsableElements, candidate.UsableElements)
	for index := range cloned.UsableElements {
		cloned.UsableElements[index].EvidenceIDs = cloneStrings(candidate.UsableElements[index].EvidenceIDs)
		cloned.UsableElements[index].Constraints = cloneStrings(candidate.UsableElements[index].Constraints)
	}
	cloned.Risks = make([]Risk, len(candidate.Risks))
	copy(cloned.Risks, candidate.Risks)
	for index := range cloned.Risks {
		cloned.Risks[index].EvidenceIDs = cloneStrings(candidate.Risks[index].EvidenceIDs)
	}
	cloned.OpenQuestions = make([]OpenQuestion, len(candidate.OpenQuestions))
	copy(cloned.OpenQuestions, candidate.OpenQuestions)
	for index := range cloned.OpenQuestions {
		cloned.OpenQuestions[index].AssetIDs = cloneStrings(candidate.OpenQuestions[index].AssetIDs)
		cloned.OpenQuestions[index].MissingRequirementIDs = cloneStrings(candidate.OpenQuestions[index].MissingRequirementIDs)
	}
	cloned.UnusedEvidenceIDs = cloneStrings(candidate.UnusedEvidenceIDs)
	return cloned
}

func copyCoverage(coverage Coverage) Coverage {
	cloned := coverage
	cloned.TargetAssetIDs = cloneStrings(coverage.TargetAssetIDs)
	cloned.AnalyzableAssetIDs = cloneStrings(coverage.AnalyzableAssetIDs)
	cloned.IncludedEvidenceIDs = cloneStrings(coverage.IncludedEvidenceIDs)
	cloned.MissingRequirements = copyMissingRequirements(coverage.MissingRequirements)
	return cloned
}

func cloneAndSortAssets(assets []AssetAnalysisInput) []AssetAnalysisInput {
	cloned := make([]AssetAnalysisInput, len(assets))
	for index, asset := range assets {
		cloned[index] = asset
		cloned[index].Evidence = append([]EvidenceInput(nil), asset.Evidence...)
		sort.Slice(cloned[index].Evidence, func(left, right int) bool {
			leftEvidence, rightEvidence := cloned[index].Evidence[left], cloned[index].Evidence[right]
			if leftEvidence.EvidenceKind != rightEvidence.EvidenceKind {
				return leftEvidence.EvidenceKind < rightEvidence.EvidenceKind
			}
			return leftEvidence.EvidenceID < rightEvidence.EvidenceID
		})
	}
	sort.Slice(cloned, func(left, right int) bool { return cloned[left].AssetID < cloned[right].AssetID })
	return cloned
}

func canonicalEvidenceRefs(refs []EvidenceRef) []EvidenceRef {
	cloned := cloneEvidenceRefs(refs)
	sort.Slice(cloned, func(left, right int) bool {
		if cloned[left].AssetID != cloned[right].AssetID {
			return cloned[left].AssetID < cloned[right].AssetID
		}
		if cloned[left].EvidenceKind != cloned[right].EvidenceKind {
			return cloned[left].EvidenceKind < cloned[right].EvidenceKind
		}
		return cloned[left].EvidenceID < cloned[right].EvidenceID
	})
	return cloned
}

func canonicalMissingRequirements(missing []MissingRequirement) []MissingRequirement {
	cloned := copyMissingRequirements(missing)
	sort.Slice(cloned, func(left, right int) bool {
		leftValue, rightValue := cloned[left], cloned[right]
		if leftValue.AssetID != rightValue.AssetID {
			return leftValue.AssetID < rightValue.AssetID
		}
		if leftValue.FocusDimension != rightValue.FocusDimension {
			return leftValue.FocusDimension < rightValue.FocusDimension
		}
		if leftValue.EvidenceKind != rightValue.EvidenceKind {
			return leftValue.EvidenceKind < rightValue.EvidenceKind
		}
		return leftValue.RequirementID < rightValue.RequirementID
	})
	return cloned
}

func sortEvidenceUnits(units []evidenceUnit) {
	sort.Slice(units, func(left, right int) bool {
		if units[left].Ref.AssetID != units[right].Ref.AssetID {
			return units[left].Ref.AssetID < units[right].Ref.AssetID
		}
		if units[left].Ref.EvidenceKind != units[right].Ref.EvidenceKind {
			return units[left].Ref.EvidenceKind < units[right].Ref.EvidenceKind
		}
		return units[left].Ref.EvidenceID < units[right].Ref.EvidenceID
	})
}

func copyEvidenceUnits(units []evidenceUnit) []evidenceUnit {
	cloned := make([]evidenceUnit, len(units))
	copy(cloned, units)
	return cloned
}

func cloneEvidenceRefs(refs []EvidenceRef) []EvidenceRef {
	cloned := make([]EvidenceRef, len(refs))
	copy(cloned, refs)
	return cloned
}

func cloneEvidenceRef(ref EvidenceRef) EvidenceRef { return ref }

func copyMissingRequirements(missing []MissingRequirement) []MissingRequirement {
	cloned := make([]MissingRequirement, len(missing))
	copy(cloned, missing)
	return cloned
}

func cloneStrings(values []string) []string {
	cloned := make([]string, len(values))
	copy(cloned, values)
	return cloned
}

func evidenceRefFromInput(evidence EvidenceInput) EvidenceRef {
	return EvidenceRef{EvidenceID: evidence.EvidenceID, AssetID: evidence.AssetID, AssetVersion: evidence.AssetVersion, MediaType: evidence.MediaType, EvidenceKind: evidence.EvidenceKind, ContentDigest: evidence.ContentDigest, Locator: evidence.Locator}
}

func targetsFromIntent(intent Intent) map[string]int64 {
	targets := make(map[string]int64, len(intent.AssetIDs))
	for _, assetID := range intent.AssetIDs {
		targets[assetID] = 0
	}
	for _, expected := range intent.ExpectedAssets {
		targets[expected.AssetID] = expected.AssetVersion
	}
	return targets
}

func missingReasonForAvailability(availability string) string {
	switch availability {
	case "failed":
		return missingReasonFailed
	case "redacted":
		return missingReasonRedacted
	case "unsupported":
		return missingReasonUnsupported
	default:
		return missingReasonMissing
	}
}

func reasonPriority(reason string) int {
	switch reason {
	case missingReasonRedacted:
		return 4
	case missingReasonFailed:
		return 3
	case missingReasonUnsupported:
		return 2
	default:
		return 1
	}
}

func validLocator(mediaType, evidenceKind string, locator EvidenceLocator) bool {
	switch {
	case mediaType == "text" && evidenceKind == "text_segment":
		return locator.Kind == "text_range" && locator.Start >= 0 && locator.End > locator.Start &&
			locator.SourceLength >= locator.End && locator.X == 0 && locator.Y == 0 && locator.Width == 0 && locator.Height == 0
	case mediaType == "image" && (evidenceKind == "visual_description" || evidenceKind == "safety_label"):
		if locator.Start != 0 || locator.End != 0 || locator.SourceLength != 0 {
			return false
		}
		if locator.Kind == "image_whole" {
			return locator.X == 0 && locator.Y == 0 && locator.Width == 0 && locator.Height == 0
		}
		return locator.Kind == "image_region" && locator.X >= 0 && locator.Y >= 0 && locator.X < 10_000 && locator.Y < 10_000 &&
			locator.Width > 0 && locator.Height > 0 && locator.Width <= 10_000-locator.X && locator.Height <= 10_000-locator.Y
	default:
		return false
	}
}

func emptyLocator(locator EvidenceLocator) bool {
	return locator.Kind == "" && locator.Start == 0 && locator.End == 0 && locator.SourceLength == 0 &&
		locator.X == 0 && locator.Y == 0 && locator.Width == 0 && locator.Height == 0
}

func locatorConflictKey(evidence EvidenceInput) (string, error) {
	wire := struct {
		AssetID      string          `json:"asset_id"`
		AssetVersion int64           `json:"asset_version"`
		EvidenceKind string          `json:"evidence_kind"`
		Locator      EvidenceLocator `json:"locator"`
	}{evidence.AssetID, evidence.AssetVersion, evidence.EvidenceKind, evidence.Locator}
	encoded, err := json.Marshal(wire)
	if err != nil {
		return "", fmt.Errorf("encode locator conflict key: %w", err)
	}
	return string(encoded), nil
}

func decodeStrictJSON(encoded []byte, maximum int, target any) error {
	if len(encoded) == 0 || len(encoded) > maximum || !utf8.Valid(encoded) || !validJSONSurrogateEscapes(encoded) {
		return fmt.Errorf("strict JSON: invalid size or UTF-8")
	}
	if err := rejectDuplicateJSONFields(encoded); err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("strict JSON: %w", err)
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return fmt.Errorf("strict JSON: trailing value")
	}
	return nil
}

func rejectDuplicateJSONFields(encoded []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.UseNumber()
	if err := consumeUniqueJSONValue(decoder); err != nil {
		return err
	}
	if token, err := decoder.Token(); !errors.Is(err, io.EOF) || token != nil {
		return fmt.Errorf("strict JSON: trailing value")
	}
	return nil
}

func consumeUniqueJSONValue(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	if token == nil {
		return fmt.Errorf("strict JSON: null is not allowed")
	}
	delimiter, isDelimiter := token.(json.Delim)
	if !isDelimiter {
		return nil
	}
	switch delimiter {
	case '{':
		seen := make(map[string]struct{})
		for decoder.More() {
			fieldToken, fieldErr := decoder.Token()
			if fieldErr != nil {
				return fieldErr
			}
			field, ok := fieldToken.(string)
			if !ok {
				return fmt.Errorf("strict JSON: object field is not a string")
			}
			if _, duplicate := seen[field]; duplicate {
				return fmt.Errorf("strict JSON: duplicate field %q", field)
			}
			seen[field] = struct{}{}
			if err := consumeUniqueJSONValue(decoder); err != nil {
				return err
			}
		}
		end, endErr := decoder.Token()
		if endErr != nil || end != json.Delim('}') {
			return fmt.Errorf("strict JSON: object is not closed")
		}
	case '[':
		for decoder.More() {
			if err := consumeUniqueJSONValue(decoder); err != nil {
				return err
			}
		}
		end, endErr := decoder.Token()
		if endErr != nil || end != json.Delim(']') {
			return fmt.Errorf("strict JSON: array is not closed")
		}
	default:
		return fmt.Errorf("strict JSON: invalid delimiter")
	}
	return nil
}

func validJSONSurrogateEscapes(raw []byte) bool {
	inString := false
	for index := 0; index < len(raw); index++ {
		switch raw[index] {
		case '"':
			inString = !inString
		case '\\':
			if !inString || index+1 >= len(raw) {
				continue
			}
			if raw[index+1] != 'u' {
				index++
				continue
			}
			code, ok := parseJSONHexCodeUnit(raw, index+2)
			if !ok {
				return false
			}
			if code >= 0xD800 && code <= 0xDBFF {
				next := index + 6
				if next+6 > len(raw) || raw[next] != '\\' || raw[next+1] != 'u' {
					return false
				}
				low, lowOK := parseJSONHexCodeUnit(raw, next+2)
				if !lowOK || low < 0xDC00 || low > 0xDFFF {
					return false
				}
				index += 11
				continue
			}
			if code >= 0xDC00 && code <= 0xDFFF {
				return false
			}
			index += 5
		}
	}
	return true
}

func parseJSONHexCodeUnit(raw []byte, start int) (uint16, bool) {
	if start < 0 || start+4 > len(raw) {
		return 0, false
	}
	var value uint16
	for _, character := range raw[start : start+4] {
		value <<= 4
		switch {
		case character >= '0' && character <= '9':
			value += uint16(character - '0')
		case character >= 'a' && character <= 'f':
			value += uint16(character-'a') + 10
		case character >= 'A' && character <= 'F':
			value += uint16(character-'A') + 10
		default:
			return 0, false
		}
	}
	return value, true
}

func validText(value string, minimum, maximum int, allowEmpty bool) bool {
	if !utf8.ValidString(value) || !norm.NFC.IsNormalString(value) {
		return false
	}
	length := utf8.RuneCountInString(value)
	if allowEmpty && length == 0 {
		return true
	}
	if length < minimum || length > maximum || strings.TrimSpace(value) == "" {
		return false
	}
	for _, character := range value {
		if unicode.IsControl(character) && character != '\n' && character != '\r' && character != '\t' {
			return false
		}
	}
	return true
}

func validStructuredText(value string, minimum, maximum int, allowEmpty bool) bool {
	if !validText(value, minimum, maximum, allowEmpty) || strings.TrimSpace(value) != value {
		return false
	}
	for _, character := range value {
		if unicode.IsControl(character) || character == '\u2028' || character == '\u2029' {
			return false
		}
	}
	return true
}

func validUniqueTexts(values []string, minimum, maximum int) bool {
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if !validText(value, minimum, maximum, false) {
			return false
		}
		if _, duplicate := seen[value]; duplicate {
			return false
		}
		seen[value] = struct{}{}
	}
	return true
}

func canonicalUUIDv7(value string) bool {
	parsed, err := uuid.Parse(value)
	return err == nil && parsed.Version() == 7 && parsed.String() == value
}

func validLowerSHA256(value string) bool {
	if len(value) != sha256.Size*2 || strings.ToLower(value) != value {
		return false
	}
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == sha256.Size
}

func validFocusDimension(value string) bool {
	return value == "content" || value == "visual" || value == "narrative" || value == "risk"
}

func validOutputLanguage(value string) bool { return value == "zh-CN" || value == "en-US" }
func validMediaType(value string) bool      { return value == "text" || value == "image" }

func validEvidenceKind(mediaType, kind string) bool {
	return (mediaType == "text" && kind == "text_segment") ||
		(mediaType == "image" && (kind == "visual_description" || kind == "safety_label"))
}

func validAvailability(value string) bool {
	return value == "ready" || value == "missing" || value == "failed" || value == "redacted" || value == "unsupported"
}

func validLocalID(value string) bool {
	return len(value) <= 64 && localIDPattern.MatchString(value)
}

func validConfidence(value string) bool {
	return value == "low" || value == "medium" || value == "high"
}
func validSeverity(value string) bool { return validConfidence(value) }

func validRiskCategory(value string) bool {
	return value == "content_safety" || value == "privacy" || value == "copyright" || value == "brand" || value == "quality"
}

func validUniqueUUIDList(values []string, minimum, maximum int) bool {
	if values == nil || len(values) < minimum || len(values) > maximum {
		return false
	}
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if !canonicalUUIDv7(value) {
			return false
		}
		if _, duplicate := seen[value]; duplicate {
			return false
		}
		seen[value] = struct{}{}
	}
	return true
}

func validUniqueLocalIDList(values []string, minimum, maximum int) bool {
	if values == nil || len(values) < minimum || len(values) > maximum {
		return false
	}
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if !validLocalID(value) {
			return false
		}
		if _, duplicate := seen[value]; duplicate {
			return false
		}
		seen[value] = struct{}{}
	}
	return true
}

func validUniqueSHA256List(values []string, minimum, maximum int) bool {
	if values == nil || len(values) < minimum || len(values) > maximum {
		return false
	}
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if !validLowerSHA256(value) {
			return false
		}
		if _, duplicate := seen[value]; duplicate {
			return false
		}
		seen[value] = struct{}{}
	}
	return true
}

func validSortedUniqueUUIDs(values []string) bool {
	if values == nil {
		return false
	}
	for index, value := range values {
		if !canonicalUUIDv7(value) || (index > 0 && values[index-1] >= value) {
			return false
		}
	}
	return true
}

func missingRequirementsAreCanonical(values []MissingRequirement) bool {
	canonical := canonicalMissingRequirements(values)
	if len(canonical) != len(values) {
		return false
	}
	seen := make(map[string]struct{}, len(values))
	for index, value := range values {
		if value != canonical[index] || !validMissingRequirement(value) {
			return false
		}
		if _, duplicate := seen[value.RequirementID]; duplicate {
			return false
		}
		seen[value.RequirementID] = struct{}{}
	}
	return true
}

func sameMissingRequirements(left, right []MissingRequirement) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func sameStringSet(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	leftCopy, rightCopy := cloneStrings(left), cloneStrings(right)
	sort.Strings(leftCopy)
	sort.Strings(rightCopy)
	for index := range leftCopy {
		if leftCopy[index] != rightCopy[index] {
			return false
		}
	}
	return true
}

func stringSet(values []string) map[string]struct{} {
	set := make(map[string]struct{}, len(values))
	for _, value := range values {
		set[value] = struct{}{}
	}
	return set
}

func assetSummaryIDs(summaries []AssetSummary) []string {
	ids := make([]string, 0, len(summaries))
	for _, summary := range summaries {
		ids = append(ids, summary.AssetID)
	}
	return ids
}

func evidenceIDs(refs []EvidenceRef) []string {
	ids := make([]string, 0, len(refs))
	for _, ref := range refs {
		ids = append(ids, ref.EvidenceID)
	}
	return ids
}

func validFailureResultCode(code string) bool {
	switch code {
	case ResultCodeInvalidArgument, ResultCodeMaterialsNotAvailable, ResultCodeSnapshotInvalid,
		ResultCodeEvidenceConflict, ResultCodeDependencyNotReady, ResultCodePromptRenderFailed,
		ResultCodeModelFailed, ResultCodeModelOutputInvalid, ResultCodeInternal:
		return true
	default:
		return false
	}
}
