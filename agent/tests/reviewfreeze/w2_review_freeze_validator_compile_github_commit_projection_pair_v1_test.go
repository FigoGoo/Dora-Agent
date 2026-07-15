package reviewfreeze_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"unicode/utf8"
)

const (
	reviewFreezeGitHubCommitProjectionPairSchemaV1 = "w2_github_reported_commit_projection_pair.v1"

	reviewFreezeGitHubCommitProjectionEndpointV1       = "github_rest_get_git_commit_by_sha"
	reviewFreezeGitHubCommitProjectionRESTAPIVersionV1 = "2022-11-28"
	reviewFreezeGitHubCommitProjectionAcceptV1         = "application/vnd.github+json"
	reviewFreezeGitHubCommitProjectionAcceptEncodingV1 = "identity"
	reviewFreezeGitHubCommitProjectionHTTPMediaTypeV1  = "application/json; charset=utf-8"

	reviewFreezeGitHubCommitProjectionPairMaxJSONBytesV1   = 16 << 10
	reviewFreezeGitHubCommitProjectionMaxParentCountV1     = 64
	reviewFreezeGitHubCommitProjectionMaxEntityBodyBytesV1 = int64(4 << 20)
	reviewFreezeGitHubCommitProjectionMaxTotalBodyBytesV1  = int64(8 << 20)

	reviewFreezeGitHubCommitProjectionExpectedSourceNoneV1      = "none"
	reviewFreezeGitHubCommitProjectionExpectedSourceUntrustedV1 = "compare_untrusted_test_value"

	reviewFreezeGitHubCommitProjectionAssuranceV1 = "github_reported_commit_oid_stable_double_read_test_only"
	reviewFreezeGitHubCommitProjectionClosedGapV1 = "github_reported_commit_projection_stable_double_read_verified"

	reviewFreezeGitHubCommitProjectionOpenTrustedExpectedSourceV1 = "trusted_expected_source_commit_unverified"
	reviewFreezeGitHubCommitProjectionOpenRepositoryAuthorityV1   = "github_repository_identity_authority_unverified"
	reviewFreezeGitHubCommitProjectionOpenObservationAuthorityV1  = "github_observation_authority_unverified"
	reviewFreezeGitHubCommitProjectionOpenCompleteTreeV1          = "github_complete_tree_projection_unverified"
	reviewFreezeGitHubCommitProjectionOpenSignatureTrustRootV1    = "github_response_signature_trust_root_unverified"
	reviewFreezeGitHubCommitProjectionOpenFormalFreezeV1          = "formal_review_freeze_unverified"
)

// reviewFreezeGitHubCommitProjectionPairPolicyV1 是调用方预先冻结的 test-only GitHub
// REST 投影策略。所有字段都是失败关闭参数；调用方不能借由扩大预算或切换媒体类型来改变验证语义。
type reviewFreezeGitHubCommitProjectionPairPolicyV1 struct {
	// RepositoryID 是调用方期望的 GitHub 数字仓库 ID；本验证器只比较值，不证明其权威来源。
	RepositoryID string
	// EndpointKind 固定 Git Database Get a commit 端点（/git/commits/{commit_sha}）语义。
	EndpointKind string
	// RequestedCommitSHA 是两次请求共同使用的 lowercase、non-zero Git commit SHA。
	RequestedCommitSHA string
	// RESTAPIVersion 固定请求使用的 GitHub REST API 版本。
	RESTAPIVersion string
	// Accept 固定请求的 GitHub JSON 表示层。
	Accept string
	// AcceptEncoding 固定为 identity，避免两次实体摘要跨压缩表示层比较。
	AcceptEncoding string
	// HTTPMediaType 固定两次成功响应的媒体类型。
	HTTPMediaType string
	// MaxPairJSONBytes 固定 pair projection 的最大 canonical JSON 字节数。
	MaxPairJSONBytes int
	// MaxParentCount 固定单次投影可携带的 ordered parent SHA 数量。
	MaxParentCount int
	// MaxEntityBodyBytes 固定单次原始 HTTP entity body 的最大字节数。
	MaxEntityBodyBytes int64
	// MaxTotalEntityBodyBytes 固定两次原始 HTTP entity body 的总字节预算。
	MaxTotalEntityBodyBytes int64
	// ExpectedSourceCheckMode 只允许不比较，或比较一个明确标注为不可信的测试值。
	ExpectedSourceCheckMode string
	// UntrustedExpectedSourceCommitSHA 仅在 compare_untrusted_test_value 模式下参与相等比较。
	UntrustedExpectedSourceCommitSHA string
}

// reviewFreezeGitHubCommitProjectionPairV1 是两次 GitHub REST commit 响应的 canonical
// 投影。它不携带响应正文、Header 全集、签名或 GitHub 身份凭据，因此不能单独形成 authority。
type reviewFreezeGitHubCommitProjectionPairV1 struct {
	// SchemaVersion 固定本投影 pair 的结构与语义版本。
	SchemaVersion string `json:"schema_version"`
	// RepositoryID 是请求上下文中的 GitHub 数字仓库 ID。
	RepositoryID string `json:"repository_id"`
	// EndpointKind 标识 Git Database Get a commit（/git/commits/{commit_sha}）端点种类。
	EndpointKind string `json:"endpoint_kind"`
	// RequestedCommitSHA 是两次读取共同请求的 lowercase、non-zero commit SHA。
	RequestedCommitSHA string `json:"requested_commit_sha"`
	// RESTAPIVersion 是两次读取共同使用的 GitHub REST API 版本。
	RESTAPIVersion string `json:"rest_api_version"`
	// Accept 是两次读取共同使用的 HTTP Accept 值。
	Accept string `json:"accept"`
	// AcceptEncoding 是两次读取共同使用的 HTTP Accept-Encoding 值。
	AcceptEncoding string `json:"accept_encoding"`
	// FirstRead 是第一次 GitHub REST 响应的有界投影。
	FirstRead reviewFreezeGitHubCommitProjectionReadV1 `json:"first_read"`
	// SecondRead 是第二次 GitHub REST 响应的有界投影。
	SecondRead reviewFreezeGitHubCommitProjectionReadV1 `json:"second_read"`
}

// reviewFreezeGitHubCommitProjectionReadV1 是一次成功响应的最小投影。Parent 顺序保留
// Git commit 的 first-parent 语义，同时数组内部必须是无重复的 SHA exact-set。
type reviewFreezeGitHubCommitProjectionReadV1 struct {
	// HTTPStatus 必须为 200。
	HTTPStatus int `json:"http_status"`
	// HTTPMediaType 必须与冻结策略完全一致。
	HTTPMediaType string `json:"http_media_type"`
	// EntityBodySizeBytes 是解码任何内容前观测到的原始 HTTP entity body 字节数。
	EntityBodySizeBytes int64 `json:"entity_body_size_bytes"`
	// EntityBodySHA256 是同一原始 entity body 的 lowercase、非空内容 SHA-256。
	EntityBodySHA256 string `json:"entity_body_sha256"`
	// ReportedCommitSHA 是 GitHub 响应报告的 commit SHA。
	ReportedCommitSHA string `json:"reported_commit_sha"`
	// ReportedTreeSHA 是 GitHub 响应报告的根 tree SHA。
	ReportedTreeSHA string `json:"reported_tree_sha"`
	// OrderedParentSHAs 按响应顺序保存全部 parent SHA，禁止重复、缺项或重排。
	OrderedParentSHAs []string `json:"ordered_parent_shas"`
}

// reviewFreezeGitHubCommitProjectionPairResultV1 是验证后的不可变 test-only 结果。
// 它只关闭双读投影稳定性，所有外部 authority 能力都由只读方法固定为 false。
type reviewFreezeGitHubCommitProjectionPairResultV1 struct {
	assurance                         string
	closedSemanticGaps                []string
	openGaps                          []string
	repositoryID                      string
	requestedCommitSHA                string
	reportedCommitSHA                 string
	reportedTreeSHA                   string
	orderedParentSHAs                 []string
	expectedSourceCheckMode           string
	untrustedExpectedSourceValueEqual bool
}

// Assurance 返回本验证器唯一允许的 test-only assurance。
func (result *reviewFreezeGitHubCommitProjectionPairResultV1) Assurance() string {
	if result == nil {
		return ""
	}
	return result.assurance
}

// ClosedSemanticGaps 返回防御性副本；本阶段只能关闭双读投影稳定性这一项。
func (result *reviewFreezeGitHubCommitProjectionPairResultV1) ClosedSemanticGaps() []string {
	if result == nil {
		return nil
	}
	return append([]string(nil), result.closedSemanticGaps...)
}

// OpenGaps 返回固定缺口的防御性副本；即使不可信 expected-source 测试值相等，
// 可信来源、仓库/观测 authority、完整 tree、签名 trust-root 与 Formal Freeze 仍全部开放。
func (result *reviewFreezeGitHubCommitProjectionPairResultV1) OpenGaps() []string {
	if result == nil {
		return nil
	}
	return append([]string(nil), result.openGaps...)
}

// RepositoryID 返回已与显式策略比较的仓库 ID；该值本身不具有仓库身份 authority。
func (result *reviewFreezeGitHubCommitProjectionPairResultV1) RepositoryID() string {
	if result == nil {
		return ""
	}
	return result.repositoryID
}

// RequestedCommitSHA 返回两次请求共同绑定的 commit SHA。
func (result *reviewFreezeGitHubCommitProjectionPairResultV1) RequestedCommitSHA() string {
	if result == nil {
		return ""
	}
	return result.requestedCommitSHA
}

// ReportedCommitSHA 返回两次响应共同报告、并与 requested SHA 相等的 commit SHA。
func (result *reviewFreezeGitHubCommitProjectionPairResultV1) ReportedCommitSHA() string {
	if result == nil {
		return ""
	}
	return result.reportedCommitSHA
}

// ReportedTreeSHA 返回两次响应共同报告的 tree SHA。
func (result *reviewFreezeGitHubCommitProjectionPairResultV1) ReportedTreeSHA() string {
	if result == nil {
		return ""
	}
	return result.reportedTreeSHA
}

// OrderedParentSHAs 返回 parent exact-sequence 的防御性副本。
func (result *reviewFreezeGitHubCommitProjectionPairResultV1) OrderedParentSHAs() []string {
	if result == nil {
		return nil
	}
	return append([]string(nil), result.orderedParentSHAs...)
}

// ExpectedSourceCheckMode 返回调用方选择的不可信测试值比较模式。
func (result *reviewFreezeGitHubCommitProjectionPairResultV1) ExpectedSourceCheckMode() string {
	if result == nil {
		return ""
	}
	return result.expectedSourceCheckMode
}

// UntrustedExpectedSourceValueEqual 仅表示 compare_untrusted_test_value 的字符串相等，
// 不能被解释为 trusted expected source、分支头、tag、release 或 deployment authority。
func (result *reviewFreezeGitHubCommitProjectionPairResultV1) UntrustedExpectedSourceValueEqual() bool {
	return result != nil && result.untrustedExpectedSourceValueEqual
}

// TrustedExpectedSourceVerified 固定为 false；本投影没有可信 expected-source 输入。
func (result *reviewFreezeGitHubCommitProjectionPairResultV1) TrustedExpectedSourceVerified() bool {
	return false
}

// TrustedRepositoryIdentityVerified 固定为 false；repository_id 只来自调用方比较值。
func (result *reviewFreezeGitHubCommitProjectionPairResultV1) TrustedRepositoryIdentityVerified() bool {
	return false
}

// IndependentObservationVerified 固定为 false；两次读取未证明独立网络路径或故障域。
func (result *reviewFreezeGitHubCommitProjectionPairResultV1) IndependentObservationVerified() bool {
	return false
}

// SignatureVerified 固定为 false；投影不携带或验证 GitHub/issuer 签名。
func (result *reviewFreezeGitHubCommitProjectionPairResultV1) SignatureVerified() bool {
	return false
}

// Authority 固定为 false；稳定双读不能自行授予 Review Freeze authority。
func (result *reviewFreezeGitHubCommitProjectionPairResultV1) Authority() bool {
	return false
}

// FormalFreezeEligible 固定为 false；生产 trust-root、签名与授权观测仍未建立。
func (result *reviewFreezeGitHubCommitProjectionPairResultV1) FormalFreezeEligible() bool {
	return false
}

// reviewFreezeGitHubCommitProjectionExpectedOpenGapsV1 返回本纯投影组件的固定
// open-gap exact-set；后续 adapter/composite 必须另行增加 entity policy 与 attestation gap。
func reviewFreezeGitHubCommitProjectionExpectedOpenGapsV1() []string {
	return []string{
		reviewFreezeGitHubCommitProjectionOpenTrustedExpectedSourceV1,
		reviewFreezeGitHubCommitProjectionOpenRepositoryAuthorityV1,
		reviewFreezeGitHubCommitProjectionOpenObservationAuthorityV1,
		reviewFreezeGitHubCommitProjectionOpenCompleteTreeV1,
		reviewFreezeGitHubCommitProjectionOpenSignatureTrustRootV1,
		reviewFreezeGitHubCommitProjectionOpenFormalFreezeV1,
	}
}

// reviewFreezeValidateGitHubCommitProjectionPairJSONV1 是本 test-only Schema 的唯一入口。
// 它先锁定 policy，再执行严格 JSON、canonical bytes、单读语义和逐字段双读相等校验。
func reviewFreezeValidateGitHubCommitProjectionPairJSONV1(raw []byte, policy reviewFreezeGitHubCommitProjectionPairPolicyV1) (*reviewFreezeGitHubCommitProjectionPairResultV1, error) {
	if err := reviewFreezeValidateGitHubCommitProjectionPairPolicyV1(policy); err != nil {
		return nil, err
	}
	if len(raw) == 0 || len(raw) > policy.MaxPairJSONBytes {
		return nil, fmt.Errorf("github commit projection pair JSON size=%d limit=%d", len(raw), policy.MaxPairJSONBytes)
	}
	if !utf8.Valid(raw) {
		return nil, fmt.Errorf("github commit projection pair JSON 不是合法 UTF-8")
	}
	// 复用 compile attestation 的 token/depth/duplicate/trailing 扫描底座，确保同一
	// Review Freeze 信任边界不会出现一套较宽松的 JSON 方言。
	if err := reviewFreezeInspectCompileAttestationJSONV1(raw); err != nil {
		return nil, fmt.Errorf("github commit projection pair strict JSON: %w", err)
	}
	var generic any
	genericDecoder := json.NewDecoder(bytes.NewReader(raw))
	genericDecoder.UseNumber()
	if err := genericDecoder.Decode(&generic); err != nil {
		return nil, fmt.Errorf("github commit projection pair generic decode: %w", err)
	}
	if err := reviewFreezeRejectCompileAttestationNullV1(generic, "$github_commit_projection_pair"); err != nil {
		return nil, fmt.Errorf("github commit projection pair strict JSON: %w", err)
	}
	if err := reviewFreezeRequireCompileAttestationFieldsV1(generic, reflect.TypeOf(reviewFreezeGitHubCommitProjectionPairV1{}), "$github_commit_projection_pair"); err != nil {
		return nil, fmt.Errorf("github commit projection pair strict JSON: %w", err)
	}
	var pair reviewFreezeGitHubCommitProjectionPairV1
	if err := messageSetStrictDecodeV1(raw, &pair); err != nil {
		return nil, fmt.Errorf("github commit projection pair strict DTO: %w", err)
	}
	canonical, err := json.Marshal(pair)
	if err != nil {
		return nil, fmt.Errorf("github commit projection pair canonical marshal: %w", err)
	}
	if !bytes.Equal(canonical, raw) {
		return nil, fmt.Errorf("github commit projection pair 必须使用 json.Marshal(dto) canonical bytes")
	}
	if err := reviewFreezeValidateGitHubCommitProjectionPairV1(pair, policy); err != nil {
		return nil, err
	}

	untrustedExpectedEqual := policy.ExpectedSourceCheckMode == reviewFreezeGitHubCommitProjectionExpectedSourceUntrustedV1
	return &reviewFreezeGitHubCommitProjectionPairResultV1{
		assurance:                         reviewFreezeGitHubCommitProjectionAssuranceV1,
		closedSemanticGaps:                []string{reviewFreezeGitHubCommitProjectionClosedGapV1},
		openGaps:                          reviewFreezeGitHubCommitProjectionExpectedOpenGapsV1(),
		repositoryID:                      pair.RepositoryID,
		requestedCommitSHA:                pair.RequestedCommitSHA,
		reportedCommitSHA:                 pair.FirstRead.ReportedCommitSHA,
		reportedTreeSHA:                   pair.FirstRead.ReportedTreeSHA,
		orderedParentSHAs:                 append([]string(nil), pair.FirstRead.OrderedParentSHAs...),
		expectedSourceCheckMode:           policy.ExpectedSourceCheckMode,
		untrustedExpectedSourceValueEqual: untrustedExpectedEqual,
	}, nil
}

// reviewFreezeValidateGitHubCommitProjectionPairPolicyV1 拒绝隐式默认值与预算扩张。
// Repository/commit identity 虽由调用方提供，但仍必须采用唯一、规范化的表示层。
func reviewFreezeValidateGitHubCommitProjectionPairPolicyV1(policy reviewFreezeGitHubCommitProjectionPairPolicyV1) error {
	if err := reviewFreezeValidateGitHubCommitProjectionRepositoryIDV1(policy.RepositoryID); err != nil {
		return fmt.Errorf("github commit projection policy repository_id: %w", err)
	}
	if err := reviewFreezeValidateGitHubCommitProjectionSHA1V1(policy.RequestedCommitSHA, "requested_commit_sha"); err != nil {
		return fmt.Errorf("github commit projection policy: %w", err)
	}
	if policy.EndpointKind != reviewFreezeGitHubCommitProjectionEndpointV1 ||
		policy.RESTAPIVersion != reviewFreezeGitHubCommitProjectionRESTAPIVersionV1 ||
		policy.Accept != reviewFreezeGitHubCommitProjectionAcceptV1 ||
		policy.AcceptEncoding != reviewFreezeGitHubCommitProjectionAcceptEncodingV1 ||
		policy.HTTPMediaType != reviewFreezeGitHubCommitProjectionHTTPMediaTypeV1 {
		return fmt.Errorf("github commit projection policy endpoint/API/media identity 漂移")
	}
	if policy.MaxPairJSONBytes != reviewFreezeGitHubCommitProjectionPairMaxJSONBytesV1 ||
		policy.MaxParentCount != reviewFreezeGitHubCommitProjectionMaxParentCountV1 ||
		policy.MaxEntityBodyBytes != reviewFreezeGitHubCommitProjectionMaxEntityBodyBytesV1 ||
		policy.MaxTotalEntityBodyBytes != reviewFreezeGitHubCommitProjectionMaxTotalBodyBytesV1 {
		return fmt.Errorf("github commit projection policy size budget 漂移")
	}
	switch policy.ExpectedSourceCheckMode {
	case reviewFreezeGitHubCommitProjectionExpectedSourceNoneV1:
		if policy.UntrustedExpectedSourceCommitSHA != "" {
			return fmt.Errorf("github commit projection expected-source none 模式禁止携带比较值")
		}
	case reviewFreezeGitHubCommitProjectionExpectedSourceUntrustedV1:
		if err := reviewFreezeValidateGitHubCommitProjectionSHA1V1(policy.UntrustedExpectedSourceCommitSHA, "untrusted_expected_source_commit_sha"); err != nil {
			return fmt.Errorf("github commit projection policy: %w", err)
		}
	default:
		return fmt.Errorf("github commit projection expected-source mode 非法=%q", policy.ExpectedSourceCheckMode)
	}
	return nil
}

// reviewFreezeValidateGitHubCommitProjectionPairV1 校验 outer policy 绑定、逐字段双读
// 相等、单读预算与可选不可信 expected-source 比较；校验顺序保证任何 drift 都失败关闭。
func reviewFreezeValidateGitHubCommitProjectionPairV1(pair reviewFreezeGitHubCommitProjectionPairV1, policy reviewFreezeGitHubCommitProjectionPairPolicyV1) error {
	if pair.SchemaVersion != reviewFreezeGitHubCommitProjectionPairSchemaV1 {
		return fmt.Errorf("github commit projection schema_version=%q", pair.SchemaVersion)
	}
	if pair.RepositoryID != policy.RepositoryID ||
		pair.EndpointKind != policy.EndpointKind ||
		pair.RequestedCommitSHA != policy.RequestedCommitSHA ||
		pair.RESTAPIVersion != policy.RESTAPIVersion ||
		pair.Accept != policy.Accept ||
		pair.AcceptEncoding != policy.AcceptEncoding {
		return fmt.Errorf("github commit projection outer policy/identity 漂移")
	}
	if !reflect.DeepEqual(pair.FirstRead, pair.SecondRead) {
		return fmt.Errorf("github commit projection first_read/second_read 必须逐字段相等")
	}
	if err := reviewFreezeValidateGitHubCommitProjectionReadV1(pair.FirstRead, pair.RequestedCommitSHA, policy); err != nil {
		return fmt.Errorf("github commit projection first_read: %w", err)
	}
	if err := reviewFreezeValidateGitHubCommitProjectionReadV1(pair.SecondRead, pair.RequestedCommitSHA, policy); err != nil {
		return fmt.Errorf("github commit projection second_read: %w", err)
	}
	if pair.FirstRead.EntityBodySizeBytes+pair.SecondRead.EntityBodySizeBytes > policy.MaxTotalEntityBodyBytes {
		return fmt.Errorf("github commit projection total entity body size=%d limit=%d", pair.FirstRead.EntityBodySizeBytes+pair.SecondRead.EntityBodySizeBytes, policy.MaxTotalEntityBodyBytes)
	}
	if policy.ExpectedSourceCheckMode == reviewFreezeGitHubCommitProjectionExpectedSourceUntrustedV1 &&
		pair.FirstRead.ReportedCommitSHA != policy.UntrustedExpectedSourceCommitSHA {
		return fmt.Errorf("github commit projection untrusted expected-source value 不相等")
	}
	return nil
}

// reviewFreezeValidateGitHubCommitProjectionReadV1 校验单次投影的 HTTP、entity、OID 与
// ordered parent exact-set。它只验证报告值的内部结构，不证明响应来自 GitHub。
func reviewFreezeValidateGitHubCommitProjectionReadV1(read reviewFreezeGitHubCommitProjectionReadV1, requestedCommitSHA string, policy reviewFreezeGitHubCommitProjectionPairPolicyV1) error {
	if read.HTTPStatus != 200 {
		return fmt.Errorf("http_status=%d want=200", read.HTTPStatus)
	}
	if read.HTTPMediaType != policy.HTTPMediaType {
		return fmt.Errorf("http_media_type=%q want=%q", read.HTTPMediaType, policy.HTTPMediaType)
	}
	if read.EntityBodySizeBytes <= 0 || read.EntityBodySizeBytes > policy.MaxEntityBodyBytes {
		return fmt.Errorf("entity body size=%d limit=%d", read.EntityBodySizeBytes, policy.MaxEntityBodyBytes)
	}
	if !reviewFreezePrefixedSHA256V1.MatchString(read.EntityBodySHA256) || read.EntityBodySHA256 == reviewFreezeSHA256V1(nil) {
		return fmt.Errorf("entity body SHA256 非法或为空内容摘要=%q", read.EntityBodySHA256)
	}
	if err := reviewFreezeValidateGitHubCommitProjectionSHA1V1(read.ReportedCommitSHA, "reported_commit_sha"); err != nil {
		return err
	}
	if read.ReportedCommitSHA != requestedCommitSHA {
		return fmt.Errorf("reported_commit_sha=%q 与 requested_commit_sha=%q 不一致", read.ReportedCommitSHA, requestedCommitSHA)
	}
	if err := reviewFreezeValidateGitHubCommitProjectionSHA1V1(read.ReportedTreeSHA, "reported_tree_sha"); err != nil {
		return err
	}
	if len(read.OrderedParentSHAs) > policy.MaxParentCount {
		return fmt.Errorf("ordered parent count=%d limit=%d", len(read.OrderedParentSHAs), policy.MaxParentCount)
	}
	seenParents := make(map[string]struct{}, len(read.OrderedParentSHAs))
	for index, parentSHA := range read.OrderedParentSHAs {
		if err := reviewFreezeValidateGitHubCommitProjectionSHA1V1(parentSHA, fmt.Sprintf("ordered_parent_shas[%d]", index)); err != nil {
			return err
		}
		if _, exists := seenParents[parentSHA]; exists {
			return fmt.Errorf("ordered parent SHA 重复=%q", parentSHA)
		}
		seenParents[parentSHA] = struct{}{}
	}
	return nil
}

// reviewFreezeValidateGitHubCommitProjectionRepositoryIDV1 要求无前导零的 uint64 数字 ID。
func reviewFreezeValidateGitHubCommitProjectionRepositoryIDV1(repositoryID string) error {
	if len(repositoryID) == 0 || len(repositoryID) > 20 || !reviewFreezeNumericIDV1.MatchString(repositoryID) {
		return fmt.Errorf("必须是无前导零的正十进制数字=%q", repositoryID)
	}
	if _, err := strconv.ParseUint(repositoryID, 10, 64); err != nil {
		return fmt.Errorf("超出 uint64=%q", repositoryID)
	}
	return nil
}

// reviewFreezeValidateGitHubCommitProjectionSHA1V1 要求 lowercase、non-zero Git SHA-1。
func reviewFreezeValidateGitHubCommitProjectionSHA1V1(sha, field string) error {
	if !reviewFreezeGitSHA1V1.MatchString(sha) || sha == strings.Repeat("0", 40) {
		return fmt.Errorf("%s 必须是 lowercase non-zero Git SHA-1=%q", field, sha)
	}
	return nil
}

// reviewFreezeGitHubCommitProjectionPairPolicyFixtureV1 返回完整显式、不可放宽的策略样例。
func reviewFreezeGitHubCommitProjectionPairPolicyFixtureV1() reviewFreezeGitHubCommitProjectionPairPolicyV1 {
	return reviewFreezeGitHubCommitProjectionPairPolicyV1{
		RepositoryID:                     "123456789",
		EndpointKind:                     reviewFreezeGitHubCommitProjectionEndpointV1,
		RequestedCommitSHA:               strings.Repeat("1", 40),
		RESTAPIVersion:                   reviewFreezeGitHubCommitProjectionRESTAPIVersionV1,
		Accept:                           reviewFreezeGitHubCommitProjectionAcceptV1,
		AcceptEncoding:                   reviewFreezeGitHubCommitProjectionAcceptEncodingV1,
		HTTPMediaType:                    reviewFreezeGitHubCommitProjectionHTTPMediaTypeV1,
		MaxPairJSONBytes:                 reviewFreezeGitHubCommitProjectionPairMaxJSONBytesV1,
		MaxParentCount:                   reviewFreezeGitHubCommitProjectionMaxParentCountV1,
		MaxEntityBodyBytes:               reviewFreezeGitHubCommitProjectionMaxEntityBodyBytesV1,
		MaxTotalEntityBodyBytes:          reviewFreezeGitHubCommitProjectionMaxTotalBodyBytesV1,
		ExpectedSourceCheckMode:          reviewFreezeGitHubCommitProjectionExpectedSourceNoneV1,
		UntrustedExpectedSourceCommitSHA: "",
	}
}

// reviewFreezeGitHubCommitProjectionPairFixtureV1 返回 canonical pair 的有类型测试样例。
func reviewFreezeGitHubCommitProjectionPairFixtureV1() reviewFreezeGitHubCommitProjectionPairV1 {
	policy := reviewFreezeGitHubCommitProjectionPairPolicyFixtureV1()
	read := reviewFreezeGitHubCommitProjectionReadV1{
		HTTPStatus:          200,
		HTTPMediaType:       policy.HTTPMediaType,
		EntityBodySizeBytes: 4096,
		EntityBodySHA256:    reviewFreezeSHA256V1([]byte("github commit response entity")),
		ReportedCommitSHA:   policy.RequestedCommitSHA,
		ReportedTreeSHA:     strings.Repeat("2", 40),
		OrderedParentSHAs: []string{
			strings.Repeat("3", 40),
			strings.Repeat("4", 40),
		},
	}
	return reviewFreezeGitHubCommitProjectionPairV1{
		SchemaVersion:      reviewFreezeGitHubCommitProjectionPairSchemaV1,
		RepositoryID:       policy.RepositoryID,
		EndpointKind:       policy.EndpointKind,
		RequestedCommitSHA: policy.RequestedCommitSHA,
		RESTAPIVersion:     policy.RESTAPIVersion,
		Accept:             policy.Accept,
		AcceptEncoding:     policy.AcceptEncoding,
		FirstRead:          read,
		SecondRead:         reviewFreezeGitHubCommitProjectionCloneReadV1(read),
	}
}

// reviewFreezeGitHubCommitProjectionCloneReadV1 复制 slice，避免 fixture 意外共享可变 backing array。
func reviewFreezeGitHubCommitProjectionCloneReadV1(read reviewFreezeGitHubCommitProjectionReadV1) reviewFreezeGitHubCommitProjectionReadV1 {
	read.OrderedParentSHAs = append([]string(nil), read.OrderedParentSHAs...)
	return read
}

// reviewFreezeGitHubCommitProjectionMarshalV1 生成验证器唯一接受的 canonical bytes。
func reviewFreezeGitHubCommitProjectionMarshalV1(t *testing.T, pair reviewFreezeGitHubCommitProjectionPairV1) []byte {
	t.Helper()
	raw, err := json.Marshal(pair)
	if err != nil {
		t.Fatalf("marshal github commit projection fixture: %v", err)
	}
	return raw
}

func TestW2ReviewFreezeGitHubCommitProjectionPairV1(t *testing.T) {
	policy := reviewFreezeGitHubCommitProjectionPairPolicyFixtureV1()
	pair := reviewFreezeGitHubCommitProjectionPairFixtureV1()
	result, err := reviewFreezeValidateGitHubCommitProjectionPairJSONV1(reviewFreezeGitHubCommitProjectionMarshalV1(t, pair), policy)
	if err != nil {
		t.Fatalf("valid github commit projection pair rejected: %v", err)
	}
	if result.Assurance() != reviewFreezeGitHubCommitProjectionAssuranceV1 ||
		!reflect.DeepEqual(result.ClosedSemanticGaps(), []string{reviewFreezeGitHubCommitProjectionClosedGapV1}) {
		t.Fatalf("result assurance/closed=%q/%v", result.Assurance(), result.ClosedSemanticGaps())
	}
	if !reflect.DeepEqual(result.OpenGaps(), reviewFreezeGitHubCommitProjectionExpectedOpenGapsV1()) {
		t.Fatalf("result open gaps=%v", result.OpenGaps())
	}
	if result.RepositoryID() != pair.RepositoryID || result.RequestedCommitSHA() != pair.RequestedCommitSHA ||
		result.ReportedCommitSHA() != pair.FirstRead.ReportedCommitSHA || result.ReportedTreeSHA() != pair.FirstRead.ReportedTreeSHA ||
		!reflect.DeepEqual(result.OrderedParentSHAs(), pair.FirstRead.OrderedParentSHAs) {
		t.Fatalf("result identity projection 漂移")
	}
	if result.ExpectedSourceCheckMode() != reviewFreezeGitHubCommitProjectionExpectedSourceNoneV1 || result.UntrustedExpectedSourceValueEqual() {
		t.Fatalf("none expected-source result=%q/%v", result.ExpectedSourceCheckMode(), result.UntrustedExpectedSourceValueEqual())
	}
	if result.TrustedExpectedSourceVerified() || result.TrustedRepositoryIdentityVerified() ||
		result.IndependentObservationVerified() || result.SignatureVerified() || result.Authority() || result.FormalFreezeEligible() {
		t.Fatalf("test-only projection pair 提权")
	}
}

func TestW2ReviewFreezeGitHubCommitProjectionPairStrictJSONV1(t *testing.T) {
	policy := reviewFreezeGitHubCommitProjectionPairPolicyFixtureV1()
	validRaw := reviewFreezeGitHubCommitProjectionMarshalV1(t, reviewFreezeGitHubCommitProjectionPairFixtureV1())

	tests := []struct {
		name string
		raw  func() []byte
		want string
	}{
		{name: "empty", raw: func() []byte { return nil }, want: "JSON size"},
		{name: "oversize", raw: func() []byte {
			return append(append([]byte(nil), validRaw...), bytes.Repeat([]byte(" "), policy.MaxPairJSONBytes-len(validRaw)+1)...)
		}, want: "JSON size"},
		{name: "invalid UTF-8", raw: func() []byte { return []byte{0xff} }, want: "UTF-8"},
		{name: "duplicate field", raw: func() []byte {
			return append([]byte(`{"schema_version":"shadow",`), validRaw[1:]...)
		}, want: "duplicate field"},
		{name: "nested duplicate field", raw: func() []byte {
			return bytes.Replace(validRaw, []byte(`"http_status":200`), []byte(`"http_status":200,"http_status":200`), 1)
		}, want: "duplicate field"},
		{name: "null", raw: func() []byte {
			var object map[string]any
			if err := json.Unmarshal(validRaw, &object); err != nil {
				t.Fatal(err)
			}
			object["first_read"] = nil
			raw, _ := json.Marshal(object)
			return raw
		}, want: "禁止 null"},
		{name: "missing", raw: func() []byte {
			var object map[string]any
			if err := json.Unmarshal(validRaw, &object); err != nil {
				t.Fatal(err)
			}
			delete(object, "accept")
			raw, _ := json.Marshal(object)
			return raw
		}, want: "缺必填字段"},
		{name: "unknown", raw: func() []byte {
			var object map[string]any
			if err := json.Unmarshal(validRaw, &object); err != nil {
				t.Fatal(err)
			}
			object["authority"] = true
			raw, _ := json.Marshal(object)
			return raw
		}, want: "unknown field"},
		{name: "nested null", raw: func() []byte {
			var object map[string]any
			if err := json.Unmarshal(validRaw, &object); err != nil {
				t.Fatal(err)
			}
			object["first_read"].(map[string]any)["ordered_parent_shas"] = nil
			raw, _ := json.Marshal(object)
			return raw
		}, want: "禁止 null"},
		{name: "nested missing", raw: func() []byte {
			var object map[string]any
			if err := json.Unmarshal(validRaw, &object); err != nil {
				t.Fatal(err)
			}
			delete(object["first_read"].(map[string]any), "reported_tree_sha")
			raw, _ := json.Marshal(object)
			return raw
		}, want: "缺必填字段"},
		{name: "nested unknown", raw: func() []byte {
			var object map[string]any
			if err := json.Unmarshal(validRaw, &object); err != nil {
				t.Fatal(err)
			}
			object["first_read"].(map[string]any)["authority"] = true
			raw, _ := json.Marshal(object)
			return raw
		}, want: "unknown field"},
		{name: "trailing", raw: func() []byte { return append(append([]byte(nil), validRaw...), []byte(` {}`)...) }, want: "trailing JSON"},
		{name: "non-canonical whitespace", raw: func() []byte {
			var pretty bytes.Buffer
			if err := json.Indent(&pretty, validRaw, "", "  "); err != nil {
				t.Fatal(err)
			}
			return pretty.Bytes()
		}, want: "canonical bytes"},
		{name: "non-canonical field order", raw: func() []byte {
			var object map[string]any
			if err := json.Unmarshal(validRaw, &object); err != nil {
				t.Fatal(err)
			}
			raw, _ := json.Marshal(object)
			return raw
		}, want: "canonical bytes"},
		{name: "non-canonical equivalent unicode escape", raw: func() []byte {
			return bytes.Replace(validRaw, []byte("github_rest_get_git_commit_by_sha"), []byte(`github_rest_get_git_commit_by_sh\u0061`), 1)
		}, want: "canonical bytes"},
		{name: "depth budget", raw: func() []byte {
			nested := `0`
			for index := 0; index < reviewFreezeCompileAttestationMaxJSONDepthV1+2; index++ {
				nested = `{"x":` + nested + `}`
			}
			return []byte(`{"schema_version":"` + reviewFreezeGitHubCommitProjectionPairSchemaV1 + `","deep":` + nested + `}`)
		}, want: "JSON depth"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := reviewFreezeValidateGitHubCommitProjectionPairJSONV1(test.raw(), policy)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error=%v want contains %q", err, test.want)
			}
		})
	}
}

func TestW2ReviewFreezeGitHubCommitProjectionPairPolicyV1(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*reviewFreezeGitHubCommitProjectionPairPolicyV1)
		want   string
	}{
		{name: "repository leading zero", mutate: func(policy *reviewFreezeGitHubCommitProjectionPairPolicyV1) { policy.RepositoryID = "01" }, want: "repository_id"},
		{name: "repository overflow", mutate: func(policy *reviewFreezeGitHubCommitProjectionPairPolicyV1) {
			policy.RepositoryID = "18446744073709551616"
		}, want: "repository_id"},
		{name: "requested uppercase", mutate: func(policy *reviewFreezeGitHubCommitProjectionPairPolicyV1) {
			policy.RequestedCommitSHA = strings.Repeat("A", 40)
		}, want: "lowercase non-zero"},
		{name: "requested zero", mutate: func(policy *reviewFreezeGitHubCommitProjectionPairPolicyV1) {
			policy.RequestedCommitSHA = strings.Repeat("0", 40)
		}, want: "lowercase non-zero"},
		{name: "endpoint", mutate: func(policy *reviewFreezeGitHubCommitProjectionPairPolicyV1) { policy.EndpointKind += "_drift" }, want: "endpoint/API/media"},
		{name: "api version", mutate: func(policy *reviewFreezeGitHubCommitProjectionPairPolicyV1) { policy.RESTAPIVersion = "latest" }, want: "endpoint/API/media"},
		{name: "accept", mutate: func(policy *reviewFreezeGitHubCommitProjectionPairPolicyV1) { policy.Accept = "application/json" }, want: "endpoint/API/media"},
		{name: "accept encoding", mutate: func(policy *reviewFreezeGitHubCommitProjectionPairPolicyV1) { policy.AcceptEncoding = "gzip" }, want: "endpoint/API/media"},
		{name: "media", mutate: func(policy *reviewFreezeGitHubCommitProjectionPairPolicyV1) {
			policy.HTTPMediaType = "application/json"
		}, want: "endpoint/API/media"},
		{name: "pair budget", mutate: func(policy *reviewFreezeGitHubCommitProjectionPairPolicyV1) { policy.MaxPairJSONBytes++ }, want: "size budget"},
		{name: "parent budget", mutate: func(policy *reviewFreezeGitHubCommitProjectionPairPolicyV1) { policy.MaxParentCount-- }, want: "size budget"},
		{name: "entity budget", mutate: func(policy *reviewFreezeGitHubCommitProjectionPairPolicyV1) { policy.MaxEntityBodyBytes++ }, want: "size budget"},
		{name: "total budget", mutate: func(policy *reviewFreezeGitHubCommitProjectionPairPolicyV1) { policy.MaxTotalEntityBodyBytes++ }, want: "size budget"},
		{name: "unknown expected mode", mutate: func(policy *reviewFreezeGitHubCommitProjectionPairPolicyV1) {
			policy.ExpectedSourceCheckMode = "compare_trusted_value"
		}, want: "mode 非法"},
		{name: "none carries value", mutate: func(policy *reviewFreezeGitHubCommitProjectionPairPolicyV1) {
			policy.UntrustedExpectedSourceCommitSHA = policy.RequestedCommitSHA
		}, want: "禁止携带"},
	}
	validRaw := reviewFreezeGitHubCommitProjectionMarshalV1(t, reviewFreezeGitHubCommitProjectionPairFixtureV1())
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			policy := reviewFreezeGitHubCommitProjectionPairPolicyFixtureV1()
			test.mutate(&policy)
			_, err := reviewFreezeValidateGitHubCommitProjectionPairJSONV1(validRaw, policy)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error=%v want contains %q", err, test.want)
			}
		})
	}
}

func TestW2ReviewFreezeGitHubCommitProjectionPairDriftV1(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*reviewFreezeGitHubCommitProjectionPairV1)
		want   string
	}{
		{name: "schema", mutate: func(pair *reviewFreezeGitHubCommitProjectionPairV1) { pair.SchemaVersion += ".drift" }, want: "schema_version"},
		{name: "repository", mutate: func(pair *reviewFreezeGitHubCommitProjectionPairV1) { pair.RepositoryID = "987654321" }, want: "outer policy/identity"},
		{name: "endpoint", mutate: func(pair *reviewFreezeGitHubCommitProjectionPairV1) { pair.EndpointKind += "_drift" }, want: "outer policy/identity"},
		{name: "requested commit", mutate: func(pair *reviewFreezeGitHubCommitProjectionPairV1) {
			pair.RequestedCommitSHA = strings.Repeat("5", 40)
		}, want: "outer policy/identity"},
		{name: "api version", mutate: func(pair *reviewFreezeGitHubCommitProjectionPairV1) { pair.RESTAPIVersion = "latest" }, want: "outer policy/identity"},
		{name: "accept", mutate: func(pair *reviewFreezeGitHubCommitProjectionPairV1) { pair.Accept = "application/json" }, want: "outer policy/identity"},
		{name: "accept encoding", mutate: func(pair *reviewFreezeGitHubCommitProjectionPairV1) { pair.AcceptEncoding = "gzip" }, want: "outer policy/identity"},
		{name: "status", mutate: func(pair *reviewFreezeGitHubCommitProjectionPairV1) { pair.SecondRead.HTTPStatus = 201 }, want: "逐字段相等"},
		{name: "media", mutate: func(pair *reviewFreezeGitHubCommitProjectionPairV1) {
			pair.SecondRead.HTTPMediaType = "application/json"
		}, want: "逐字段相等"},
		{name: "entity size", mutate: func(pair *reviewFreezeGitHubCommitProjectionPairV1) { pair.SecondRead.EntityBodySizeBytes++ }, want: "逐字段相等"},
		{name: "entity digest", mutate: func(pair *reviewFreezeGitHubCommitProjectionPairV1) {
			pair.SecondRead.EntityBodySHA256 = reviewFreezeSHA256V1([]byte("other body"))
		}, want: "逐字段相等"},
		{name: "commit", mutate: func(pair *reviewFreezeGitHubCommitProjectionPairV1) {
			pair.SecondRead.ReportedCommitSHA = strings.Repeat("5", 40)
		}, want: "逐字段相等"},
		{name: "tree", mutate: func(pair *reviewFreezeGitHubCommitProjectionPairV1) {
			pair.SecondRead.ReportedTreeSHA = strings.Repeat("5", 40)
		}, want: "逐字段相等"},
		{name: "parent order", mutate: func(pair *reviewFreezeGitHubCommitProjectionPairV1) {
			pair.SecondRead.OrderedParentSHAs[0], pair.SecondRead.OrderedParentSHAs[1] = pair.SecondRead.OrderedParentSHAs[1], pair.SecondRead.OrderedParentSHAs[0]
		}, want: "逐字段相等"},
		{name: "both reads wrong requested commit", mutate: func(pair *reviewFreezeGitHubCommitProjectionPairV1) {
			pair.FirstRead.ReportedCommitSHA = strings.Repeat("5", 40)
			pair.SecondRead.ReportedCommitSHA = strings.Repeat("5", 40)
		}, want: "requested_commit_sha"},
	}
	policy := reviewFreezeGitHubCommitProjectionPairPolicyFixtureV1()
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			pair := reviewFreezeGitHubCommitProjectionPairFixtureV1()
			test.mutate(&pair)
			_, err := reviewFreezeValidateGitHubCommitProjectionPairJSONV1(reviewFreezeGitHubCommitProjectionMarshalV1(t, pair), policy)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error=%v want contains %q", err, test.want)
			}
		})
	}
}

func TestW2ReviewFreezeGitHubCommitProjectionPairParentsV1(t *testing.T) {
	policy := reviewFreezeGitHubCommitProjectionPairPolicyFixtureV1()
	makeParents := func(count int) []string {
		parents := make([]string, count)
		for index := range parents {
			parents[index] = fmt.Sprintf("%040x", index+1)
		}
		return parents
	}
	run := func(t *testing.T, pair reviewFreezeGitHubCommitProjectionPairV1) error {
		t.Helper()
		_, err := reviewFreezeValidateGitHubCommitProjectionPairJSONV1(reviewFreezeGitHubCommitProjectionMarshalV1(t, pair), policy)
		return err
	}

	t.Run("root commit empty parents", func(t *testing.T) {
		pair := reviewFreezeGitHubCommitProjectionPairFixtureV1()
		pair.FirstRead.OrderedParentSHAs = []string{}
		pair.SecondRead.OrderedParentSHAs = []string{}
		if err := run(t, pair); err != nil {
			t.Fatalf("root commit rejected: %v", err)
		}
	})
	t.Run("exact max", func(t *testing.T) {
		pair := reviewFreezeGitHubCommitProjectionPairFixtureV1()
		pair.FirstRead.OrderedParentSHAs = makeParents(policy.MaxParentCount)
		pair.SecondRead.OrderedParentSHAs = append([]string(nil), pair.FirstRead.OrderedParentSHAs...)
		if err := run(t, pair); err != nil {
			t.Fatalf("max parents rejected: %v", err)
		}
	})
	for _, test := range []struct {
		name   string
		mutate func(*reviewFreezeGitHubCommitProjectionPairV1)
		want   string
	}{
		{name: "over max", mutate: func(pair *reviewFreezeGitHubCommitProjectionPairV1) {
			parents := makeParents(policy.MaxParentCount + 1)
			pair.FirstRead.OrderedParentSHAs = parents
			pair.SecondRead.OrderedParentSHAs = append([]string(nil), parents...)
		}, want: "parent count"},
		{name: "uppercase", mutate: func(pair *reviewFreezeGitHubCommitProjectionPairV1) {
			pair.FirstRead.OrderedParentSHAs[0] = strings.Repeat("A", 40)
			pair.SecondRead.OrderedParentSHAs[0] = strings.Repeat("A", 40)
		}, want: "lowercase non-zero"},
		{name: "zero", mutate: func(pair *reviewFreezeGitHubCommitProjectionPairV1) {
			pair.FirstRead.OrderedParentSHAs[0] = strings.Repeat("0", 40)
			pair.SecondRead.OrderedParentSHAs[0] = strings.Repeat("0", 40)
		}, want: "lowercase non-zero"},
		{name: "duplicate", mutate: func(pair *reviewFreezeGitHubCommitProjectionPairV1) {
			pair.FirstRead.OrderedParentSHAs[1] = pair.FirstRead.OrderedParentSHAs[0]
			pair.SecondRead.OrderedParentSHAs[1] = pair.SecondRead.OrderedParentSHAs[0]
		}, want: "重复"},
	} {
		t.Run(test.name, func(t *testing.T) {
			pair := reviewFreezeGitHubCommitProjectionPairFixtureV1()
			test.mutate(&pair)
			if err := run(t, pair); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error=%v want contains %q", err, test.want)
			}
		})
	}
}

func TestW2ReviewFreezeGitHubCommitProjectionPairEntityV1(t *testing.T) {
	policy := reviewFreezeGitHubCommitProjectionPairPolicyFixtureV1()
	tests := []struct {
		name   string
		mutate func(*reviewFreezeGitHubCommitProjectionPairV1)
		want   string
	}{
		{name: "status", mutate: func(pair *reviewFreezeGitHubCommitProjectionPairV1) {
			pair.FirstRead.HTTPStatus, pair.SecondRead.HTTPStatus = 204, 204
		}, want: "http_status"},
		{name: "media", mutate: func(pair *reviewFreezeGitHubCommitProjectionPairV1) {
			pair.FirstRead.HTTPMediaType, pair.SecondRead.HTTPMediaType = "application/json", "application/json"
		}, want: "http_media_type"},
		{name: "zero size", mutate: func(pair *reviewFreezeGitHubCommitProjectionPairV1) {
			pair.FirstRead.EntityBodySizeBytes, pair.SecondRead.EntityBodySizeBytes = 0, 0
		}, want: "entity body size"},
		{name: "over per-read size", mutate: func(pair *reviewFreezeGitHubCommitProjectionPairV1) {
			pair.FirstRead.EntityBodySizeBytes = policy.MaxEntityBodyBytes + 1
			pair.SecondRead.EntityBodySizeBytes = policy.MaxEntityBodyBytes + 1
		}, want: "entity body size"},
		{name: "int64 overflow input rejected before total addition", mutate: func(pair *reviewFreezeGitHubCommitProjectionPairV1) {
			pair.FirstRead.EntityBodySizeBytes = int64(1<<63 - 1)
			pair.SecondRead.EntityBodySizeBytes = int64(1<<63 - 1)
		}, want: "entity body size"},
		{name: "digest format", mutate: func(pair *reviewFreezeGitHubCommitProjectionPairV1) {
			pair.FirstRead.EntityBodySHA256, pair.SecondRead.EntityBodySHA256 = "", ""
		}, want: "SHA256"},
		{name: "empty content digest", mutate: func(pair *reviewFreezeGitHubCommitProjectionPairV1) {
			pair.FirstRead.EntityBodySHA256 = reviewFreezeSHA256V1(nil)
			pair.SecondRead.EntityBodySHA256 = reviewFreezeSHA256V1(nil)
		}, want: "为空内容摘要"},
		{name: "commit uppercase", mutate: func(pair *reviewFreezeGitHubCommitProjectionPairV1) {
			pair.FirstRead.ReportedCommitSHA, pair.SecondRead.ReportedCommitSHA = strings.Repeat("A", 40), strings.Repeat("A", 40)
		}, want: "lowercase non-zero"},
		{name: "tree zero", mutate: func(pair *reviewFreezeGitHubCommitProjectionPairV1) {
			pair.FirstRead.ReportedTreeSHA, pair.SecondRead.ReportedTreeSHA = strings.Repeat("0", 40), strings.Repeat("0", 40)
		}, want: "lowercase non-zero"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			pair := reviewFreezeGitHubCommitProjectionPairFixtureV1()
			test.mutate(&pair)
			_, err := reviewFreezeValidateGitHubCommitProjectionPairJSONV1(reviewFreezeGitHubCommitProjectionMarshalV1(t, pair), policy)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error=%v want contains %q", err, test.want)
			}
		})
	}
	t.Run("exact per-read and total budget", func(t *testing.T) {
		pair := reviewFreezeGitHubCommitProjectionPairFixtureV1()
		pair.FirstRead.EntityBodySizeBytes = policy.MaxEntityBodyBytes
		pair.SecondRead.EntityBodySizeBytes = policy.MaxEntityBodyBytes
		if _, err := reviewFreezeValidateGitHubCommitProjectionPairJSONV1(reviewFreezeGitHubCommitProjectionMarshalV1(t, pair), policy); err != nil {
			t.Fatalf("exact entity budgets rejected: %v", err)
		}
	})
}

func TestW2ReviewFreezeGitHubCommitProjectionPairExpectedSourceV1(t *testing.T) {
	pair := reviewFreezeGitHubCommitProjectionPairFixtureV1()
	raw := reviewFreezeGitHubCommitProjectionMarshalV1(t, pair)

	t.Run("untrusted equal", func(t *testing.T) {
		policy := reviewFreezeGitHubCommitProjectionPairPolicyFixtureV1()
		policy.ExpectedSourceCheckMode = reviewFreezeGitHubCommitProjectionExpectedSourceUntrustedV1
		policy.UntrustedExpectedSourceCommitSHA = pair.FirstRead.ReportedCommitSHA
		result, err := reviewFreezeValidateGitHubCommitProjectionPairJSONV1(raw, policy)
		if err != nil {
			t.Fatalf("untrusted equal rejected: %v", err)
		}
		if !result.UntrustedExpectedSourceValueEqual() || result.TrustedExpectedSourceVerified() || result.Authority() || result.FormalFreezeEligible() {
			t.Fatalf("untrusted equal result 提权")
		}
		if !reflect.DeepEqual(result.OpenGaps(), reviewFreezeGitHubCommitProjectionExpectedOpenGapsV1()) {
			t.Fatalf("untrusted equal 不得关闭 authority gaps=%v", result.OpenGaps())
		}
	})
	t.Run("untrusted mismatch", func(t *testing.T) {
		policy := reviewFreezeGitHubCommitProjectionPairPolicyFixtureV1()
		policy.ExpectedSourceCheckMode = reviewFreezeGitHubCommitProjectionExpectedSourceUntrustedV1
		policy.UntrustedExpectedSourceCommitSHA = strings.Repeat("5", 40)
		if _, err := reviewFreezeValidateGitHubCommitProjectionPairJSONV1(raw, policy); err == nil || !strings.Contains(err.Error(), "不相等") {
			t.Fatalf("error=%v want untrusted mismatch", err)
		}
	})
	t.Run("untrusted invalid", func(t *testing.T) {
		policy := reviewFreezeGitHubCommitProjectionPairPolicyFixtureV1()
		policy.ExpectedSourceCheckMode = reviewFreezeGitHubCommitProjectionExpectedSourceUntrustedV1
		policy.UntrustedExpectedSourceCommitSHA = strings.Repeat("0", 40)
		if _, err := reviewFreezeValidateGitHubCommitProjectionPairJSONV1(raw, policy); err == nil || !strings.Contains(err.Error(), "lowercase non-zero") {
			t.Fatalf("error=%v want invalid untrusted source", err)
		}
	})
}

func TestW2ReviewFreezeGitHubCommitProjectionPairPrivilegeBoundaryV1(t *testing.T) {
	policy := reviewFreezeGitHubCommitProjectionPairPolicyFixtureV1()
	pair := reviewFreezeGitHubCommitProjectionPairFixtureV1()
	raw := reviewFreezeGitHubCommitProjectionMarshalV1(t, pair)
	for _, field := range []string{"trusted", "repository_verified", "observation_verified", "signature_verified", "authority", "formal_freeze_eligible"} {
		t.Run(field, func(t *testing.T) {
			var object map[string]any
			if err := json.Unmarshal(raw, &object); err != nil {
				t.Fatal(err)
			}
			object[field] = true
			mutated, _ := json.Marshal(object)
			if _, err := reviewFreezeValidateGitHubCommitProjectionPairJSONV1(mutated, policy); err == nil || !strings.Contains(err.Error(), "unknown field") {
				t.Fatalf("privilege field %q error=%v", field, err)
			}
		})
	}
}

func TestW2ReviewFreezeGitHubCommitProjectionPairImmutableV1(t *testing.T) {
	policy := reviewFreezeGitHubCommitProjectionPairPolicyFixtureV1()
	pair := reviewFreezeGitHubCommitProjectionPairFixtureV1()
	raw := reviewFreezeGitHubCommitProjectionMarshalV1(t, pair)
	result, err := reviewFreezeValidateGitHubCommitProjectionPairJSONV1(raw, policy)
	if err != nil {
		t.Fatal(err)
	}
	wantParents := result.OrderedParentSHAs()
	wantCommit := result.ReportedCommitSHA()

	raw[0] = '['
	policy.RepositoryID = "987654321"
	pair.FirstRead.ReportedCommitSHA = strings.Repeat("5", 40)
	pair.FirstRead.OrderedParentSHAs[0] = strings.Repeat("6", 40)
	returnedParents := result.OrderedParentSHAs()
	returnedParents[0] = strings.Repeat("7", 40)
	closed := result.ClosedSemanticGaps()
	closed[0] = "elevated"
	open := result.OpenGaps()
	open[0] = "closed_without_proof"

	if result.ReportedCommitSHA() != wantCommit || !reflect.DeepEqual(result.OrderedParentSHAs(), wantParents) ||
		!reflect.DeepEqual(result.ClosedSemanticGaps(), []string{reviewFreezeGitHubCommitProjectionClosedGapV1}) ||
		!reflect.DeepEqual(result.OpenGaps(), reviewFreezeGitHubCommitProjectionExpectedOpenGapsV1()) {
		t.Fatalf("result 被输入或 accessor 返回值改写")
	}

	var wait sync.WaitGroup
	for index := 0; index < 32; index++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			if result.Assurance() != reviewFreezeGitHubCommitProjectionAssuranceV1 || result.ReportedCommitSHA() != wantCommit ||
				!reflect.DeepEqual(result.OrderedParentSHAs(), wantParents) || result.Authority() || result.FormalFreezeEligible() {
				t.Errorf("concurrent immutable read 漂移")
			}
		}()
	}
	wait.Wait()
}
