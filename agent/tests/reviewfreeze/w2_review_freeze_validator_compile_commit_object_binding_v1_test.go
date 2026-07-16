package reviewfreeze_test

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

const (
	reviewFreezeCompileCommitObjectMaxPayloadBytesV1 = 1 << 20
	reviewFreezeCompileCommitObjectMaxFrameBytesV1   = reviewFreezeCompileCommitObjectMaxPayloadBytesV1 + 64
	reviewFreezeCompileCommitHeaderMaxBytesV1        = 256 << 10
	reviewFreezeCompileCommitHeaderLineMaxBytesV1    = 64 << 10
	reviewFreezeCompileCommitHeaderMaxLinesV1        = 4096
	reviewFreezeCompileCommitParentMaxCountV1        = 64
	reviewFreezeCompileCommitListProbeMaxCountV1     = 2

	reviewFreezeCompileCommitBindingVerifiedClaimV1 = "base_commit_object_to_tree_binding_verified"
	reviewFreezeCompileCommitFormalFreezeStatusV1   = "not_established"
	reviewFreezeCompileCommitSourceEqualityGapV1    = "trusted_source_commit_exact_equality_unverified"
	reviewFreezeCompileCommitAncestryGapV1          = "commit_graph_ancestry_unverified_non_authoritative"
	reviewFreezeCompileCommitRemoteAuthorityGapV1   = "github_repository_authority_unverified"
	reviewFreezeCompileCommitRequiredAnchorSchemaV1 = "w2_github_repository_commit_anchor.v1"
)

// reviewFreezeCompileCommitObjectDescriptorV1 是 raw CAS 在 List 阶段返回的不可变描述符。
// BodySHA256 为 payload 的额外强摘要；Git ObjectID 仍必须由 canonical frame 的 SHA-1 重算。
type reviewFreezeCompileCommitObjectDescriptorV1 struct {
	ObjectID      string
	ObjectKind    string
	BodySizeBytes int64
	BodySHA256    string
}

// reviewFreezeCompileCommitObjectOpenedV1 是 loader 按对象 ID 打开的不可变对象句柄。
// Descriptor 必须与 List 结果逐字段相同；Reader 返回 canonical `commit <size>\x00<payload>`，
// Close 必须能中断 Read，以便 verifier 兑现 context 取消和资源关闭语义。
type reviewFreezeCompileCommitObjectOpenedV1 struct {
	Descriptor reviewFreezeCompileCommitObjectDescriptorV1
	Reader     io.ReadCloser
}

// reviewFreezeCompileCommitObjectLoaderV1 是 commit verifier 的最小 CAS 消费边界。
// verifier 先 List 一次完成 exact-set，再按 statement.BaseCommitSHA Open 一次；loader 不得
// 自行解析 ref、HEAD 或工作树。
type reviewFreezeCompileCommitObjectLoaderV1 interface {
	ListCommitObjects(context.Context) ([]reviewFreezeCompileCommitObjectDescriptorV1, error)
	OpenCommitObject(context.Context, string) (reviewFreezeCompileCommitObjectOpenedV1, error)
}

// reviewFreezeCompileCommitAuthorityAnchorV1 描述后续 source equality/remote authority 阶段必须
// 注入的 typed anchor 形状。本 verifier 不接收或构造该值，避免把本地对象存在性冒充为
// GitHub 权威；后续实现还须验证签名 envelope/receipt，并要求 ExpectedSourceCommitSHA 与
// statement.BaseCommitSHA 精确相等。普通 ancestor 关系会放行陈旧源码，不能作为准入条件。
type reviewFreezeCompileCommitAuthorityAnchorV1 struct {
	SchemaVersion            string // SchemaVersion 固定 typed anchor 的解析规则。
	RepositoryID             string // RepositoryID 绑定 statement 中的数值仓库身份。
	GitHubRepositoryNodeID   string // GitHubRepositoryNodeID 绑定 GitHub 不可变仓库节点身份。
	RefFullName              string // RefFullName 固定受信 source ref，而不是调用方任意 ref。
	ExpectedSourceCommitSHA  string // ExpectedSourceCommitSHA 必须与 BaseCommitSHA 精确相等。
	ObservationReceiptSHA256 string // ObservationReceiptSHA256 绑定 GitHub 观测回执原文。
	SignatureEnvelopeSHA256  string // SignatureEnvelopeSHA256 绑定 trust-root 验证的签名 envelope。
}

// reviewFreezeCompileCommitBindingScopeV1 明确本阶段只关闭 commit object 到 tree 字段的
// 内容寻址绑定；source commit exact equality 与 GitHub remote authority 仍需 typed anchor。
type reviewFreezeCompileCommitBindingScopeV1 struct {
	VerifiedClaim             string
	BaseCommitToTreeBound     bool
	TrustedSourceCommitEqual  bool
	CommitAncestryProven      bool
	GitHubAuthorityProven     bool
	FormalFreezeStatus        string
	RequiredTypedAnchorSchema string
	OpenGaps                  []string
}

// reviewFreezeVerifiedCompileCommitBindingV1 保存已验证对象的不可变投影。parents 和 raw
// frame 均不直接暴露内部存储，调用方不能通过修改返回切片污染后续准入判断。
type reviewFreezeVerifiedCompileCommitBindingV1 struct {
	commitSHA string
	treeSHA   string
	parents   []string
	frame     string
}

// CommitSHA 返回已经通过 canonical frame SHA-1 校验的 commit 对象 ID。
func (binding *reviewFreezeVerifiedCompileCommitBindingV1) CommitSHA() string {
	if binding == nil {
		return ""
	}
	return binding.commitSHA
}

// TreeSHA 返回 commit 唯一 tree header，并已与 statement.BaseTreeSHA 精确绑定。
func (binding *reviewFreezeVerifiedCompileCommitBindingV1) TreeSHA() string {
	if binding == nil {
		return ""
	}
	return binding.treeSHA
}

// ParentSHAs 返回 canonical parent 顺序的副本；这里只解析并校验格式，不证明 ancestry。
func (binding *reviewFreezeVerifiedCompileCommitBindingV1) ParentSHAs() []string {
	if binding == nil {
		return nil
	}
	return append([]string(nil), binding.parents...)
}

// FramedObjectBytes 返回已验证 canonical Git object frame 的副本，用于后续纯函数组合。
func (binding *reviewFreezeVerifiedCompileCommitBindingV1) FramedObjectBytes() []byte {
	if binding == nil {
		return nil
	}
	return []byte(binding.frame)
}

// UsedObjectIDs 返回本次 List exact-set 中已经被单次 Open 并完全消费的 OID 副本。
func (binding *reviewFreezeVerifiedCompileCommitBindingV1) UsedObjectIDs() []string {
	if binding == nil {
		return nil
	}
	return []string{binding.commitSHA}
}

// Scope 返回固定证明边界。三个 false 与三个 open gap 是安全结论，不得由调用方覆盖。
func (binding *reviewFreezeVerifiedCompileCommitBindingV1) Scope() reviewFreezeCompileCommitBindingScopeV1 {
	if binding == nil {
		return reviewFreezeCompileCommitBindingScopeV1{}
	}
	return reviewFreezeCompileCommitBindingScopeV1{
		VerifiedClaim:             reviewFreezeCompileCommitBindingVerifiedClaimV1,
		BaseCommitToTreeBound:     true,
		TrustedSourceCommitEqual:  false,
		CommitAncestryProven:      false,
		GitHubAuthorityProven:     false,
		FormalFreezeStatus:        reviewFreezeCompileCommitFormalFreezeStatusV1,
		RequiredTypedAnchorSchema: reviewFreezeCompileCommitRequiredAnchorSchemaV1,
		OpenGaps: []string{
			reviewFreezeCompileCommitSourceEqualityGapV1,
			reviewFreezeCompileCommitAncestryGapV1,
			reviewFreezeCompileCommitRemoteAuthorityGapV1,
		},
	}
}

// reviewFreezeCompileCommitHeaderV1 是物理 header/continuation 归一后的单个逻辑 header。
type reviewFreezeCompileCommitHeaderV1 struct {
	key           string
	value         string
	continuations []string
}

// reviewFreezeCompileCommitParsedV1 只保留本阶段需要的 tree 与 parent 结构投影。
type reviewFreezeCompileCommitParsedV1 struct {
	treeSHA string
	parents []string
}

// reviewFreezeVerifyCompileCommitObjectBindingV1 先完成 statement 校验，再从 CAS List
// exact-set 并单次打开 BaseCommitSHA。它验证 descriptor、canonical Git frame、SHA-1 内容
// 身份、payload SHA-256 和唯一 tree header，最后将 tree 与 BaseTreeSHA 精确绑定；该函数
// 不调用 git、GitHub、ref 或工作树。
func reviewFreezeVerifyCompileCommitObjectBindingV1(
	ctx context.Context,
	statement reviewFreezeValidatorCompileAttestationV1,
	loader reviewFreezeCompileCommitObjectLoaderV1,
) (*reviewFreezeVerifiedCompileCommitBindingV1, error) {
	if ctx == nil {
		return nil, fmt.Errorf("compile commit context 不能为空")
	}
	if loader == nil {
		return nil, fmt.Errorf("compile commit loader 不能为空")
	}
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("compile commit context before validation: %w", err)
	}
	if err := reviewFreezeValidateCompileAttestationStatementV1(statement); err != nil {
		return nil, fmt.Errorf("compile commit statement: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("compile commit context before List: %w", err)
	}
	listed, err := loader.ListCommitObjects(ctx)
	if err != nil {
		return nil, fmt.Errorf("compile commit List: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("compile commit context after List: %w", err)
	}
	// 冻结 loader 返回的 descriptor slice，保证 exact-set 校验与后续 Open 使用同一值投影。
	frozenListed := append([]reviewFreezeCompileCommitObjectDescriptorV1(nil), listed...)
	descriptor, err := reviewFreezeValidateCompileCommitObjectListingV1(frozenListed, statement.Subject.BaseCommitSHA)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("compile commit context before Open: %w", err)
	}

	opened, err := loader.OpenCommitObject(ctx, descriptor.ObjectID)
	if err != nil {
		if opened.Reader != nil {
			if closeErr := opened.Reader.Close(); closeErr != nil {
				return nil, fmt.Errorf("compile commit Open: %w; close=%v", err, closeErr)
			}
		}
		return nil, fmt.Errorf("compile commit Open: %w", err)
	}
	if opened.Reader == nil {
		return nil, fmt.Errorf("compile commit Open 返回 nil reader")
	}
	if opened.Descriptor != descriptor {
		return nil, reviewFreezeCompileCommitCloseWithErrorV1(
			opened.Reader,
			fmt.Errorf("compile commit opened descriptor=%+v want=%+v", opened.Descriptor, descriptor),
		)
	}
	if err := ctx.Err(); err != nil {
		return nil, reviewFreezeCompileCommitCloseWithErrorV1(
			opened.Reader,
			fmt.Errorf("compile commit context after Open: %w", err),
		)
	}

	expectedFrameBytes := int64(len("commit ")) + int64(len(strconv.FormatInt(descriptor.BodySizeBytes, 10))) + 1 + descriptor.BodySizeBytes
	if expectedFrameBytes <= 0 || expectedFrameBytes > reviewFreezeCompileCommitObjectMaxFrameBytesV1 {
		return nil, reviewFreezeCompileCommitCloseWithErrorV1(
			opened.Reader,
			fmt.Errorf("compile commit expected frame bytes=%d limit=%d", expectedFrameBytes, reviewFreezeCompileCommitObjectMaxFrameBytesV1),
		)
	}
	frame, err := reviewFreezeReadCompileCommitObjectV1(ctx, opened.Reader, expectedFrameBytes)
	if err != nil {
		return nil, err
	}
	if actualSHA := reviewFreezeCompileCommitObjectSHAV1(frame); actualSHA != statement.Subject.BaseCommitSHA {
		return nil, fmt.Errorf("compile commit framed SHA-1=%q want=%q", actualSHA, statement.Subject.BaseCommitSHA)
	}
	payload, err := reviewFreezeParseCompileCommitFrameV1(frame)
	if err != nil {
		return nil, err
	}
	if int64(len(payload)) != descriptor.BodySizeBytes || reviewFreezeSHA256V1(payload) != descriptor.BodySHA256 {
		return nil, fmt.Errorf("compile commit payload descriptor drift size=%d/%d sha=%q/%q", len(payload), descriptor.BodySizeBytes, reviewFreezeSHA256V1(payload), descriptor.BodySHA256)
	}
	return reviewFreezeFinalizeCompileCommitObjectBindingV1(statement, frame, payload)
}

// reviewFreezeVerifyCompileCommitObjectBindingFromRawBundleV1 直接消费 resolver 已冻结的
// body-only bundle，不再构造兼容旧接口的 List/Open/ReadCloser view。该边界仍独立重验
// descriptor exact-set、descriptor/object/body 三方一致性、canonical Git frame 内容身份，
// 以及 statement 的 commit/tree 绑定，避免把上游验证结论当作可变内存的永久事实。
func reviewFreezeVerifyCompileCommitObjectBindingFromRawBundleV1(
	ctx context.Context,
	statement reviewFreezeValidatorCompileAttestationV1,
	bundle *reviewFreezeCompileGitRawObjectBundleV1,
) (*reviewFreezeVerifiedCompileCommitBindingV1, error) {
	if ctx == nil {
		return nil, fmt.Errorf("compile commit raw bundle context 不能为空")
	}
	if bundle == nil {
		return nil, fmt.Errorf("compile commit raw bundle 不能为空")
	}
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("compile commit raw bundle context before validation: %w", err)
	}
	if err := reviewFreezeValidateCompileAttestationStatementV1(statement); err != nil {
		return nil, fmt.Errorf("compile commit raw bundle statement: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("compile commit raw bundle context before descriptor snapshot: %w", err)
	}

	descriptors := bundle.Descriptors()
	if err := reviewFreezeValidateCompileGitRawDescriptorsV1(descriptors); err != nil {
		return nil, fmt.Errorf("compile commit raw bundle descriptors: %w", err)
	}
	var listedCommit reviewFreezeCompileGitRawObjectDescriptorV1
	for _, descriptor := range descriptors {
		if descriptor.Kind == "commit" {
			listedCommit = descriptor
			break
		}
	}
	if listedCommit.ObjectID != statement.Subject.BaseCommitSHA {
		return nil, fmt.Errorf("compile commit raw bundle listed commit=%q want BaseCommitSHA=%q", listedCommit.ObjectID, statement.Subject.BaseCommitSHA)
	}
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("compile commit raw bundle context after descriptor snapshot: %w", err)
	}

	objectDescriptor, exists := bundle.Descriptor(statement.Subject.BaseCommitSHA)
	if !exists {
		return nil, fmt.Errorf("compile commit raw bundle missing object=%q", statement.Subject.BaseCommitSHA)
	}
	if objectDescriptor != listedCommit {
		return nil, fmt.Errorf("compile commit raw bundle descriptor drift actual=%+v listed=%+v", objectDescriptor, listedCommit)
	}
	body, exists := bundle.BodyBytes(statement.Subject.BaseCommitSHA)
	if !exists {
		return nil, fmt.Errorf("compile commit raw bundle missing body=%q", statement.Subject.BaseCommitSHA)
	}
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("compile commit raw bundle context after body snapshot: %w", err)
	}
	actualBodySHA256 := reviewFreezeSHA256V1(body)
	if int64(len(body)) != listedCommit.BodySizeBytes || actualBodySHA256 != listedCommit.BodySHA256 {
		return nil, fmt.Errorf(
			"compile commit raw bundle body descriptor drift size=%d/%d sha=%q/%q",
			len(body),
			listedCommit.BodySizeBytes,
			actualBodySHA256,
			listedCommit.BodySHA256,
		)
	}

	frame := reviewFreezeCompileGitRawCanonicalFrameV1(listedCommit.Kind, body)
	return reviewFreezeFinalizeCompileCommitObjectBindingV1(statement, frame, body)
}

// reviewFreezeFinalizeCompileCommitObjectBindingV1 在 legacy loader 和 direct raw bundle
// 两条入口完成各自 descriptor/body 冻结后，共享内容身份、commit header 语义与结果构造。
func reviewFreezeFinalizeCompileCommitObjectBindingV1(
	statement reviewFreezeValidatorCompileAttestationV1,
	frame []byte,
	payload []byte,
) (*reviewFreezeVerifiedCompileCommitBindingV1, error) {
	if actualSHA := reviewFreezeCompileCommitObjectSHAV1(frame); actualSHA != statement.Subject.BaseCommitSHA {
		return nil, fmt.Errorf("compile commit framed SHA-1=%q want=%q", actualSHA, statement.Subject.BaseCommitSHA)
	}
	parsed, err := reviewFreezeParseCompileCommitPayloadV1(payload, statement.Subject.BaseCommitSHA)
	if err != nil {
		return nil, err
	}
	if parsed.treeSHA != statement.Subject.BaseTreeSHA {
		return nil, fmt.Errorf("compile commit tree binding=%q want BaseTreeSHA=%q", parsed.treeSHA, statement.Subject.BaseTreeSHA)
	}
	return &reviewFreezeVerifiedCompileCommitBindingV1{
		commitSHA: statement.Subject.BaseCommitSHA,
		treeSHA:   parsed.treeSHA,
		parents:   append([]string(nil), parsed.parents...),
		frame:     string(append([]byte(nil), frame...)),
	}, nil
}

// reviewFreezeValidateCompileCommitObjectListingV1 在任何 Open 前验证单对象 exact-set。
// 最多观察两个 descriptor 以区分 duplicate/extra；更大的列表立即按预算失败关闭。
func reviewFreezeValidateCompileCommitObjectListingV1(
	listed []reviewFreezeCompileCommitObjectDescriptorV1,
	expectedCommitSHA string,
) (reviewFreezeCompileCommitObjectDescriptorV1, error) {
	if len(listed) > reviewFreezeCompileCommitListProbeMaxCountV1 {
		return reviewFreezeCompileCommitObjectDescriptorV1{}, fmt.Errorf("compile commit List descriptor budget count=%d limit=%d", len(listed), reviewFreezeCompileCommitListProbeMaxCountV1)
	}
	if len(listed) == 0 {
		return reviewFreezeCompileCommitObjectDescriptorV1{}, fmt.Errorf("compile commit List missing expected object=%q", expectedCommitSHA)
	}
	seen := make(map[string]struct{}, len(listed))
	var expected reviewFreezeCompileCommitObjectDescriptorV1
	for _, descriptor := range listed {
		if !reviewFreezeGitSHA1V1.MatchString(descriptor.ObjectID) || descriptor.ObjectID == strings.Repeat("0", 40) {
			return reviewFreezeCompileCommitObjectDescriptorV1{}, fmt.Errorf("compile commit List object_id 非 lowercase SHA-1=%q", descriptor.ObjectID)
		}
		if _, duplicate := seen[descriptor.ObjectID]; duplicate {
			return reviewFreezeCompileCommitObjectDescriptorV1{}, fmt.Errorf("compile commit List duplicate object_id=%q", descriptor.ObjectID)
		}
		seen[descriptor.ObjectID] = struct{}{}
		if descriptor.ObjectKind != "commit" {
			return reviewFreezeCompileCommitObjectDescriptorV1{}, fmt.Errorf("compile commit List object kind=%q want=commit", descriptor.ObjectKind)
		}
		if descriptor.BodySizeBytes <= 0 || descriptor.BodySizeBytes > reviewFreezeCompileCommitObjectMaxPayloadBytesV1 {
			return reviewFreezeCompileCommitObjectDescriptorV1{}, fmt.Errorf("compile commit List body size=%d limit=%d", descriptor.BodySizeBytes, reviewFreezeCompileCommitObjectMaxPayloadBytesV1)
		}
		if !reviewFreezePrefixedSHA256V1.MatchString(descriptor.BodySHA256) || descriptor.BodySHA256 == reviewFreezeSHA256V1(nil) {
			return reviewFreezeCompileCommitObjectDescriptorV1{}, fmt.Errorf("compile commit List body SHA-256 非法=%q", descriptor.BodySHA256)
		}
		if descriptor.ObjectID != expectedCommitSHA {
			return reviewFreezeCompileCommitObjectDescriptorV1{}, fmt.Errorf("compile commit List extra object_id=%q", descriptor.ObjectID)
		}
		expected = descriptor
	}
	if len(seen) != 1 || expected.ObjectID == "" {
		return reviewFreezeCompileCommitObjectDescriptorV1{}, fmt.Errorf("compile commit List expected object 未全消费=%q", expectedCommitSHA)
	}
	return expected, nil
}

// reviewFreezeCompileCommitCloseWithErrorV1 在 metadata/context 失败路径关闭 loader 句柄，
// 并保留原始错误；Close 失败也必须进入诊断，不能静默泄漏资源。
func reviewFreezeCompileCommitCloseWithErrorV1(reader io.ReadCloser, cause error) error {
	if closeErr := reader.Close(); closeErr != nil {
		return fmt.Errorf("%w; close=%v", cause, closeErr)
	}
	return cause
}

// reviewFreezeReadCompileCommitObjectV1 最多读取 frame 预算加一个探测字节。取消时主动
// Close 以中断 Read；loader 的 Reader 契约负责保证 Close 可中断阻塞读取。
func reviewFreezeReadCompileCommitObjectV1(ctx context.Context, reader io.ReadCloser, expectedFrameBytes int64) ([]byte, error) {
	type readResult struct {
		raw []byte
		err error
	}
	result := make(chan readResult, 1)
	go func() {
		raw, err := io.ReadAll(io.LimitReader(reader, expectedFrameBytes+1))
		result <- readResult{raw: raw, err: err}
	}()

	select {
	case <-ctx.Done():
		if closeErr := reader.Close(); closeErr != nil {
			return nil, fmt.Errorf("compile commit context during read: %w; close=%v", ctx.Err(), closeErr)
		}
		return nil, fmt.Errorf("compile commit context during read: %w", ctx.Err())
	case read := <-result:
		closeErr := reader.Close()
		if read.err != nil {
			return nil, fmt.Errorf("compile commit read: %w", read.err)
		}
		if closeErr != nil {
			return nil, fmt.Errorf("compile commit close: %w", closeErr)
		}
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("compile commit context after read: %w", err)
		}
		if int64(len(read.raw)) < expectedFrameBytes {
			return nil, fmt.Errorf("compile commit frame truncated actual=%d want=%d", len(read.raw), expectedFrameBytes)
		}
		if int64(len(read.raw)) > expectedFrameBytes {
			return nil, fmt.Errorf("compile commit frame oversized actual>%d", expectedFrameBytes)
		}
		return read.raw, nil
	}
}

// reviewFreezeParseCompileCommitFrameV1 只接受 Git SHA-1 commit loose-object 的 canonical
// framing：`commit <无前导零十进制 payload 大小>\x00<payload>`。type、大小和尾随字节
// 统一在这一边界失败关闭。
func reviewFreezeParseCompileCommitFrameV1(frame []byte) ([]byte, error) {
	separator := bytes.IndexByte(frame, 0)
	if separator < 0 {
		return nil, fmt.Errorf("compile commit frame 缺 NUL separator")
	}
	header := string(frame[:separator])
	if !strings.HasPrefix(header, "commit ") {
		return nil, fmt.Errorf("compile commit frame object type/header=%q", header)
	}
	sizeText := strings.TrimPrefix(header, "commit ")
	if sizeText == "" || (len(sizeText) > 1 && sizeText[0] == '0') {
		return nil, fmt.Errorf("compile commit frame size 非 canonical=%q", sizeText)
	}
	for _, character := range sizeText {
		if character < '0' || character > '9' {
			return nil, fmt.Errorf("compile commit frame size 非十进制=%q", sizeText)
		}
	}
	declaredSize, err := strconv.ParseInt(sizeText, 10, 64)
	if err != nil || declaredSize <= 0 || declaredSize > reviewFreezeCompileCommitObjectMaxPayloadBytesV1 {
		return nil, fmt.Errorf("compile commit payload size=%q limit=%d", sizeText, reviewFreezeCompileCommitObjectMaxPayloadBytesV1)
	}
	payload := frame[separator+1:]
	if int64(len(payload)) != declaredSize {
		return nil, fmt.Errorf("compile commit payload size actual=%d declared=%d", len(payload), declaredSize)
	}
	return append([]byte(nil), payload...), nil
}

// reviewFreezeParseCompileCommitPayloadV1 只解析本证明需要的 commit header 结构：tree
// 必须是唯一首行，parent 必须紧随 tree 且合法、唯一、有界。其他合法扩展 header 及其
// generic continuation 仅作为已被对象摘要覆盖的 opaque 值，不在本层制定身份、签名、
// encoding 或 message 内容策略。
func reviewFreezeParseCompileCommitPayloadV1(payload []byte, commitSHA string) (reviewFreezeCompileCommitParsedV1, error) {
	if len(payload) == 0 || len(payload) > reviewFreezeCompileCommitObjectMaxPayloadBytesV1 {
		return reviewFreezeCompileCommitParsedV1{}, fmt.Errorf("compile commit payload bytes=%d", len(payload))
	}
	boundary := bytes.Index(payload, []byte("\n\n"))
	if boundary <= 0 {
		return reviewFreezeCompileCommitParsedV1{}, fmt.Errorf("compile commit 缺 canonical header/message separator")
	}
	headerRaw := payload[:boundary]
	// Message 已由 descriptor SHA-256 与 Git object SHA-1 完整绑定，不属于 tree 证明的
	// 语法输入；因此 CR/NUL 只在 header 区失败关闭，不能额外拒绝合法 message bytes。
	if bytes.IndexByte(headerRaw, 0) >= 0 {
		return reviewFreezeCompileCommitParsedV1{}, fmt.Errorf("compile commit header 禁止 NUL")
	}
	if bytes.IndexByte(headerRaw, '\r') >= 0 {
		return reviewFreezeCompileCommitParsedV1{}, fmt.Errorf("compile commit header 禁止 CR/CRLF")
	}
	if len(headerRaw) > reviewFreezeCompileCommitHeaderMaxBytesV1 {
		return reviewFreezeCompileCommitParsedV1{}, fmt.Errorf("compile commit header bytes=%d limit=%d", len(headerRaw), reviewFreezeCompileCommitHeaderMaxBytesV1)
	}
	headers, err := reviewFreezeParseCompileCommitHeadersV1(headerRaw)
	if err != nil {
		return reviewFreezeCompileCommitParsedV1{}, err
	}
	treeCount := 0
	for _, header := range headers {
		if header.key == "tree" {
			treeCount++
		}
	}
	if treeCount != 1 {
		return reviewFreezeCompileCommitParsedV1{}, fmt.Errorf("compile commit tree header count=%d want=1", treeCount)
	}
	if headers[0].key != "tree" {
		return reviewFreezeCompileCommitParsedV1{}, fmt.Errorf("compile commit tree header 必须唯一且位于首行")
	}
	if !reviewFreezeGitSHA1V1.MatchString(headers[0].value) ||
		headers[0].value == strings.Repeat("0", 40) ||
		headers[0].value == commitSHA {
		return reviewFreezeCompileCommitParsedV1{}, fmt.Errorf("compile commit tree SHA 非法=%q", headers[0].value)
	}
	if len(headers[0].continuations) != 0 {
		return reviewFreezeCompileCommitParsedV1{}, fmt.Errorf("compile commit tree 禁止 continuation")
	}

	parsed := reviewFreezeCompileCommitParsedV1{treeSHA: headers[0].value}
	seenParents := make(map[string]struct{})
	index := 1
	for index < len(headers) && headers[index].key == "parent" {
		parent := headers[index]
		if len(parent.continuations) != 0 ||
			!reviewFreezeGitSHA1V1.MatchString(parent.value) ||
			parent.value == strings.Repeat("0", 40) ||
			parent.value == commitSHA ||
			parent.value == parsed.treeSHA {
			return reviewFreezeCompileCommitParsedV1{}, fmt.Errorf("compile commit parent SHA 非法=%q", parent.value)
		}
		if _, duplicate := seenParents[parent.value]; duplicate {
			return reviewFreezeCompileCommitParsedV1{}, fmt.Errorf("compile commit duplicate parent=%q", parent.value)
		}
		seenParents[parent.value] = struct{}{}
		parsed.parents = append(parsed.parents, parent.value)
		if len(parsed.parents) > reviewFreezeCompileCommitParentMaxCountV1 {
			return reviewFreezeCompileCommitParsedV1{}, fmt.Errorf("compile commit parent count>%d", reviewFreezeCompileCommitParentMaxCountV1)
		}
		index++
	}

	for ; index < len(headers); index++ {
		header := headers[index]
		if header.key == "parent" {
			return reviewFreezeCompileCommitParsedV1{}, fmt.Errorf("compile commit out-of-order parent header")
		}
		// treeCount 已在上方按逻辑 header 统计；continuation 中形似 `tree ...` 的
		// opaque 值不会成为新 header，也不能覆盖首行 tree。
	}
	return parsed, nil
}

// reviewFreezeParseCompileCommitHeadersV1 把物理行解析为逻辑 header。任意 header 都可
// 使用 Git 的单空格 continuation；continuation 内容保持 opaque，不重新解释为 tree/parent。
func reviewFreezeParseCompileCommitHeadersV1(headerRaw []byte) ([]reviewFreezeCompileCommitHeaderV1, error) {
	lines := bytes.Split(headerRaw, []byte{'\n'})
	if len(lines) == 0 || len(lines) > reviewFreezeCompileCommitHeaderMaxLinesV1 {
		return nil, fmt.Errorf("compile commit physical header lines=%d limit=%d", len(lines), reviewFreezeCompileCommitHeaderMaxLinesV1)
	}
	headers := make([]reviewFreezeCompileCommitHeaderV1, 0, len(lines))
	for lineNumber, rawLine := range lines {
		if len(rawLine) == 0 || len(rawLine) > reviewFreezeCompileCommitHeaderLineMaxBytesV1 {
			return nil, fmt.Errorf("compile commit header line=%d bytes=%d", lineNumber+1, len(rawLine))
		}
		if rawLine[0] == ' ' {
			if len(headers) == 0 {
				return nil, fmt.Errorf("compile commit orphan continuation line=%d", lineNumber+1)
			}
			previous := &headers[len(headers)-1]
			previous.continuations = append(previous.continuations, string(rawLine[1:]))
			continue
		}
		if rawLine[0] == '\t' {
			return nil, fmt.Errorf("compile commit tab continuation 禁止 line=%d", lineNumber+1)
		}
		separator := bytes.IndexByte(rawLine, ' ')
		if separator <= 0 {
			return nil, fmt.Errorf("compile commit header line=%d 缺 key/value", lineNumber+1)
		}
		key := string(rawLine[:separator])
		for index, character := range key {
			if (index == 0 && (character < 'a' || character > 'z')) ||
				(index > 0 && (character < 'a' || character > 'z') && (character < '0' || character > '9') && character != '-') {
				return nil, fmt.Errorf("compile commit header key 非 canonical=%q", key)
			}
		}
		value := string(rawLine[separator+1:])
		headers = append(headers, reviewFreezeCompileCommitHeaderV1{key: key, value: value})
	}
	return headers, nil
}

func reviewFreezeCompileCommitObjectSHAV1(frame []byte) string {
	digest := sha1.Sum(frame)
	return hex.EncodeToString(digest[:])
}

func reviewFreezeCompileCommitEqualStringsV1(left, right []string) bool {
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

func reviewFreezeCompileCommitFrameV1(payload []byte) []byte {
	frame := make([]byte, 0, len(payload)+64)
	frame = append(frame, "commit "...)
	frame = strconv.AppendInt(frame, int64(len(payload)), 10)
	frame = append(frame, 0)
	frame = append(frame, payload...)
	return frame
}

func reviewFreezeCompileCommitPayloadV1(treeSHA string, parents ...string) []byte {
	var builder strings.Builder
	builder.WriteString("tree ")
	builder.WriteString(treeSHA)
	builder.WriteByte('\n')
	for _, parent := range parents {
		builder.WriteString("parent ")
		builder.WriteString(parent)
		builder.WriteByte('\n')
	}
	builder.WriteString("author Review Freeze <review-freeze@example.invalid> 1784114416 +0800\n")
	builder.WriteString("committer Review Freeze <review-freeze@example.invalid> 1784114416 +0800\n\n")
	builder.WriteString("compile commit fixture\n")
	return []byte(builder.String())
}

type reviewFreezeCompileCommitTrackingReaderV1 struct {
	reader   io.Reader
	closeErr error
	closed   int
}

func (reader *reviewFreezeCompileCommitTrackingReaderV1) Read(buffer []byte) (int, error) {
	return reader.reader.Read(buffer)
}

func (reader *reviewFreezeCompileCommitTrackingReaderV1) Close() error {
	reader.closed++
	return reader.closeErr
}

type reviewFreezeCompileCommitBlockingReaderV1 struct {
	started chan struct{}
	closed  chan struct{}
	once    sync.Once
}

func reviewFreezeCompileCommitBlockingReaderNewV1() *reviewFreezeCompileCommitBlockingReaderV1 {
	return &reviewFreezeCompileCommitBlockingReaderV1{started: make(chan struct{}), closed: make(chan struct{})}
}

func (reader *reviewFreezeCompileCommitBlockingReaderV1) Read([]byte) (int, error) {
	reader.once.Do(func() { close(reader.started) })
	<-reader.closed
	return 0, errors.New("read interrupted by close")
}

func (reader *reviewFreezeCompileCommitBlockingReaderV1) Close() error {
	select {
	case <-reader.closed:
	default:
		close(reader.closed)
	}
	return nil
}

type reviewFreezeCompileCommitErrorReaderV1 struct {
	err    error
	closed int
}

func (reader *reviewFreezeCompileCommitErrorReaderV1) Read([]byte) (int, error) {
	return 0, reader.err
}

func (reader *reviewFreezeCompileCommitErrorReaderV1) Close() error {
	reader.closed++
	return nil
}

type reviewFreezeCompileCommitLoaderFixtureV1 struct {
	descriptor             reviewFreezeCompileCommitObjectDescriptorV1
	listingOverride        []reviewFreezeCompileCommitObjectDescriptorV1
	openDescriptorOverride *reviewFreezeCompileCommitObjectDescriptorV1
	frame                  []byte
	listErr                error
	openErr                error
	openErrReader          io.ReadCloser
	readerOverride         io.ReadCloser
	afterList              func()
	afterOpen              func()
	listCalls              int
	openCalls              int
	requested              []string
	lastReader             *reviewFreezeCompileCommitTrackingReaderV1
}

func (loader *reviewFreezeCompileCommitLoaderFixtureV1) ListCommitObjects(ctx context.Context) ([]reviewFreezeCompileCommitObjectDescriptorV1, error) {
	loader.listCalls++
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if loader.listErr != nil {
		return nil, loader.listErr
	}
	listed := []reviewFreezeCompileCommitObjectDescriptorV1{loader.descriptor}
	if loader.listingOverride != nil {
		listed = loader.listingOverride
	}
	if loader.afterList != nil {
		loader.afterList()
	}
	return append([]reviewFreezeCompileCommitObjectDescriptorV1(nil), listed...), nil
}

func (loader *reviewFreezeCompileCommitLoaderFixtureV1) OpenCommitObject(ctx context.Context, objectID string) (reviewFreezeCompileCommitObjectOpenedV1, error) {
	loader.openCalls++
	loader.requested = append(loader.requested, objectID)
	if err := ctx.Err(); err != nil {
		return reviewFreezeCompileCommitObjectOpenedV1{}, err
	}
	descriptor := loader.descriptor
	if loader.openDescriptorOverride != nil {
		descriptor = *loader.openDescriptorOverride
	}
	if loader.openErr != nil {
		return reviewFreezeCompileCommitObjectOpenedV1{Descriptor: descriptor, Reader: loader.openErrReader}, loader.openErr
	}
	if loader.afterOpen != nil {
		loader.afterOpen()
	}
	if loader.readerOverride != nil {
		return reviewFreezeCompileCommitObjectOpenedV1{Descriptor: descriptor, Reader: loader.readerOverride}, nil
	}
	reader := &reviewFreezeCompileCommitTrackingReaderV1{reader: bytes.NewReader(append([]byte(nil), loader.frame...))}
	loader.lastReader = reader
	return reviewFreezeCompileCommitObjectOpenedV1{Descriptor: descriptor, Reader: reader}, nil
}

func reviewFreezeCompileCommitDescriptorV1(commitSHA string, payload []byte) reviewFreezeCompileCommitObjectDescriptorV1 {
	return reviewFreezeCompileCommitObjectDescriptorV1{
		ObjectID:      commitSHA,
		ObjectKind:    "commit",
		BodySizeBytes: int64(len(payload)),
		BodySHA256:    reviewFreezeSHA256V1(payload),
	}
}

func reviewFreezeCompileCommitFixtureV1(t *testing.T, payload []byte, treeSHA string) (reviewFreezeValidatorCompileAttestationV1, *reviewFreezeCompileCommitLoaderFixtureV1) {
	t.Helper()
	frame := reviewFreezeCompileCommitFrameV1(payload)
	commitSHA := reviewFreezeCompileCommitObjectSHAV1(frame)
	statement := reviewFreezeCompileAttestationFixtureStatementV1(t)
	statement.Subject.BaseCommitSHA = commitSHA
	statement.Subject.BaseTreeSHA = treeSHA
	return statement, &reviewFreezeCompileCommitLoaderFixtureV1{
		descriptor: reviewFreezeCompileCommitDescriptorV1(commitSHA, payload),
		frame:      frame,
	}
}

// reviewFreezeCompileCommitRawBundleCloneV1 只供 direct API 对抗测试复制冻结值；body
// 已使用 immutable string 保存，因此 map value 的值复制不会与源 bundle 共享可变字节。
func reviewFreezeCompileCommitRawBundleCloneV1(source *reviewFreezeCompileGitRawObjectBundleV1) *reviewFreezeCompileGitRawObjectBundleV1 {
	if source == nil {
		return nil
	}
	clone := &reviewFreezeCompileGitRawObjectBundleV1{
		descriptors: append([]reviewFreezeCompileGitRawObjectDescriptorV1(nil), source.descriptors...),
		objects:     make(map[string]reviewFreezeCompileGitRawFrozenObjectV1, len(source.objects)),
	}
	for objectID, object := range source.objects {
		clone.objects[objectID] = object
	}
	return clone
}

// TestW2ReviewFreezeCompileCommitObjectBindingFromRawBundleV1Golden 证明 direct API 只消费
// 已冻结 accessor，不回访外部 CAS，也不改变既有 UsedObjectIDs/Scope 证明边界。
func TestW2ReviewFreezeCompileCommitObjectBindingFromRawBundleV1Golden(t *testing.T) {
	loader, commitSHA, treeSHA := reviewFreezeCompileGitRawValidFixtureV1()
	bundle, err := reviewFreezeResolveCompileGitRawObjectBundleV1(context.Background(), loader)
	if err != nil {
		t.Fatalf("resolve raw bundle: %v", err)
	}
	statement := reviewFreezeCompileAttestationFixtureStatementV1(t)
	statement.Subject.BaseCommitSHA = commitSHA
	statement.Subject.BaseTreeSHA = treeSHA
	openBeforeDirect := loader.openCallSnapshot()

	// 修改 accessor 返回副本不能污染后续 direct 校验。
	descriptorCopies := bundle.Descriptors()
	descriptorCopies[0].Kind = "tag"
	bodyCopy, exists := bundle.BodyBytes(commitSHA)
	if !exists || len(bodyCopy) == 0 {
		t.Fatalf("fixture commit body missing object=%s", commitSHA)
	}
	bodyCopy[0] ^= 0xff

	binding, err := reviewFreezeVerifyCompileCommitObjectBindingFromRawBundleV1(context.Background(), statement, bundle)
	if err != nil {
		t.Fatalf("direct raw bundle binding rejected: %v", err)
	}
	if binding.CommitSHA() != commitSHA || binding.TreeSHA() != treeSHA {
		t.Fatalf("direct binding identity commit=%q/%q tree=%q/%q", binding.CommitSHA(), commitSHA, binding.TreeSHA(), treeSHA)
	}
	if parents := binding.ParentSHAs(); len(parents) != 0 {
		t.Fatalf("direct binding parents=%v want=[]", parents)
	}
	wantBody, exists := bundle.BodyBytes(commitSHA)
	if !exists {
		t.Fatalf("fixture commit body disappeared object=%s", commitSHA)
	}
	wantFrame := reviewFreezeCompileGitRawCanonicalFrameV1("commit", wantBody)
	if !bytes.Equal(binding.FramedObjectBytes(), wantFrame) {
		t.Fatalf("direct binding canonical frame drift")
	}
	if used := binding.UsedObjectIDs(); !reviewFreezeCompileCommitEqualStringsV1(used, []string{commitSHA}) {
		t.Fatalf("direct binding used objects=%v want=[%s]", used, commitSHA)
	}
	scope := binding.Scope()
	wantGaps := []string{
		reviewFreezeCompileCommitSourceEqualityGapV1,
		reviewFreezeCompileCommitAncestryGapV1,
		reviewFreezeCompileCommitRemoteAuthorityGapV1,
	}
	if scope.VerifiedClaim != reviewFreezeCompileCommitBindingVerifiedClaimV1 ||
		!scope.BaseCommitToTreeBound ||
		scope.TrustedSourceCommitEqual ||
		scope.CommitAncestryProven ||
		scope.GitHubAuthorityProven ||
		scope.FormalFreezeStatus != reviewFreezeCompileCommitFormalFreezeStatusV1 ||
		scope.RequiredTypedAnchorSchema != reviewFreezeCompileCommitRequiredAnchorSchemaV1 ||
		!reviewFreezeCompileCommitEqualStringsV1(scope.OpenGaps, wantGaps) {
		t.Fatalf("direct binding scope drift=%+v", scope)
	}
	if openAfterDirect := loader.openCallSnapshot(); !reviewFreezeCompileGitRawEqualStringIntMapV1(openBeforeDirect, openAfterDirect) {
		t.Fatalf("direct binding revisited external CAS before=%v after=%v", openBeforeDirect, openAfterDirect)
	}

	// 返回值中的 slice/bytes 也必须保持副本语义。
	usedCopy := binding.UsedObjectIDs()
	usedCopy[0] = strings.Repeat("f", 40)
	scope.OpenGaps[0] = "mutated"
	frameCopy := binding.FramedObjectBytes()
	frameCopy[0] ^= 0xff
	if binding.UsedObjectIDs()[0] != commitSHA ||
		binding.Scope().OpenGaps[0] != reviewFreezeCompileCommitSourceEqualityGapV1 ||
		!bytes.Equal(binding.FramedObjectBytes(), wantFrame) {
		t.Fatalf("direct binding accessor mutation leaked into frozen result")
	}
}

// TestW2ReviewFreezeCompileCommitObjectBindingFromRawBundleV1RejectsAdversaries
// 覆盖 direct 边界必须自行关闭的 context、kind、OID、body 与 statement 漂移。
func TestW2ReviewFreezeCompileCommitObjectBindingFromRawBundleV1RejectsAdversaries(t *testing.T) {
	loader, commitSHA, treeSHA := reviewFreezeCompileGitRawValidFixtureV1()
	validBundle, err := reviewFreezeResolveCompileGitRawObjectBundleV1(context.Background(), loader)
	if err != nil {
		t.Fatalf("resolve raw bundle: %v", err)
	}
	validStatement := reviewFreezeCompileAttestationFixtureStatementV1(t)
	validStatement.Subject.BaseCommitSHA = commitSHA
	validStatement.Subject.BaseTreeSHA = treeSHA

	canceledContext, cancel := context.WithCancel(context.Background())
	cancel()
	tests := []struct {
		name      string
		ctx       context.Context
		statement reviewFreezeValidatorCompileAttestationV1
		bundle    *reviewFreezeCompileGitRawObjectBundleV1
		mutate    func(*reviewFreezeCompileGitRawObjectBundleV1)
	}{
		{name: "nil context", ctx: nil, statement: validStatement, bundle: validBundle},
		{name: "nil bundle", ctx: context.Background(), statement: validStatement},
		{name: "pre-canceled context", ctx: canceledContext, statement: validStatement, bundle: validBundle},
		{
			name:      "listed kind drift",
			ctx:       context.Background(),
			statement: validStatement,
			bundle:    validBundle,
			mutate: func(bundle *reviewFreezeCompileGitRawObjectBundleV1) {
				for index := range bundle.descriptors {
					if bundle.descriptors[index].ObjectID == commitSHA {
						bundle.descriptors[index].Kind = "tree"
						return
					}
				}
			},
		},
		{
			name:      "object descriptor OID drift",
			ctx:       context.Background(),
			statement: validStatement,
			bundle:    validBundle,
			mutate: func(bundle *reviewFreezeCompileGitRawObjectBundleV1) {
				object := bundle.objects[commitSHA]
				object.descriptor.ObjectID = strings.Repeat("f", 40)
				bundle.objects[commitSHA] = object
			},
		},
		{
			name:      "frozen body drift",
			ctx:       context.Background(),
			statement: validStatement,
			bundle:    validBundle,
			mutate: func(bundle *reviewFreezeCompileGitRawObjectBundleV1) {
				object := bundle.objects[commitSHA]
				body := []byte(object.body)
				body[len(body)-1] ^= 0xff
				object.body = string(body)
				bundle.objects[commitSHA] = object
			},
		},
		{
			name: "statement tree mismatch",
			ctx:  context.Background(),
			statement: func() reviewFreezeValidatorCompileAttestationV1 {
				statement := validStatement
				statement.Subject.BaseTreeSHA = strings.Repeat("e", 40)
				return statement
			}(),
			bundle: validBundle,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			bundle := reviewFreezeCompileCommitRawBundleCloneV1(test.bundle)
			if test.mutate != nil {
				test.mutate(bundle)
			}
			if _, err := reviewFreezeVerifyCompileCommitObjectBindingFromRawBundleV1(test.ctx, test.statement, bundle); err == nil {
				t.Fatal("direct raw bundle adversary unexpectedly accepted")
			}
		})
	}
}

// TestW2ReviewFreezeCompileCommitObjectBindingV1RealHEADGolden 使用真实仓库 HEAD 的
// `git cat-file commit` content 构造 canonical frame。Git 只属于 fixture；verifier 收到的
// 仍是内存 CAS loader，不能读取 ref、工作树或 remote。
func TestW2ReviewFreezeCompileCommitObjectBindingV1RealHEADGolden(t *testing.T) {
	root := reviewFreezeRepoRootV1(t)
	commitSHA := strings.TrimSpace(reviewFreezeRunTestGitV1(t, root, "rev-parse", "HEAD^{commit}"))
	treeSHA := strings.TrimSpace(reviewFreezeRunTestGitV1(t, root, "rev-parse", "HEAD^{tree}"))
	payload := []byte(reviewFreezeRunTestGitV1(t, root, "cat-file", "commit", "HEAD"))
	frame := reviewFreezeCompileCommitFrameV1(payload)
	if actual := reviewFreezeCompileCommitObjectSHAV1(frame); actual != commitSHA {
		t.Fatalf("real HEAD canonical frame SHA=%q want=%q", actual, commitSHA)
	}
	statement := reviewFreezeCompileAttestationFixtureStatementV1(t)
	statement.Subject.BaseCommitSHA = commitSHA
	statement.Subject.BaseTreeSHA = treeSHA
	loader := &reviewFreezeCompileCommitLoaderFixtureV1{
		descriptor: reviewFreezeCompileCommitDescriptorV1(commitSHA, payload),
		frame:      frame,
	}

	binding, err := reviewFreezeVerifyCompileCommitObjectBindingV1(context.Background(), statement, loader)
	if err != nil {
		t.Fatalf("real HEAD commit binding rejected: %v", err)
	}
	if binding.CommitSHA() != commitSHA || binding.TreeSHA() != treeSHA {
		t.Fatalf("real HEAD binding=%q/%q want=%q/%q", binding.CommitSHA(), binding.TreeSHA(), commitSHA, treeSHA)
	}
	if loader.listCalls != 1 || loader.openCalls != 1 || len(loader.requested) != 1 || loader.requested[0] != commitSHA || loader.lastReader == nil || loader.lastReader.closed != 1 {
		t.Fatalf("real HEAD loader lifecycle list=%d open=%d requested=%v reader=%+v", loader.listCalls, loader.openCalls, loader.requested, loader.lastReader)
	}
	scope := binding.Scope()
	if scope.VerifiedClaim != reviewFreezeCompileCommitBindingVerifiedClaimV1 || !scope.BaseCommitToTreeBound ||
		scope.TrustedSourceCommitEqual || scope.CommitAncestryProven || scope.GitHubAuthorityProven ||
		scope.FormalFreezeStatus != reviewFreezeCompileCommitFormalFreezeStatusV1 ||
		scope.RequiredTypedAnchorSchema != reviewFreezeCompileCommitRequiredAnchorSchemaV1 ||
		!reviewFreezeCompileCommitEqualStringsV1(scope.OpenGaps, []string{reviewFreezeCompileCommitSourceEqualityGapV1, reviewFreezeCompileCommitAncestryGapV1, reviewFreezeCompileCommitRemoteAuthorityGapV1}) {
		t.Fatalf("real HEAD scope 越界=%+v", scope)
	}
	usedOIDs := binding.UsedObjectIDs()
	if !reviewFreezeCompileCommitEqualStringsV1(usedOIDs, []string{commitSHA}) {
		t.Fatalf("used OIDs=%v", usedOIDs)
	}
	usedOIDs[0] = strings.Repeat("f", 40)
	if binding.UsedObjectIDs()[0] != commitSHA {
		t.Fatal("used OID result 暴露可变内部切片")
	}
	parents := binding.ParentSHAs()
	if len(parents) > 0 {
		parents[0] = strings.Repeat("f", 40)
		if binding.ParentSHAs()[0] == parents[0] {
			t.Fatal("parent result 暴露可变内部切片")
		}
	}
	frameCopy := binding.FramedObjectBytes()
	frameCopy[0] = 'x'
	if binding.FramedObjectBytes()[0] != 'c' {
		t.Fatal("framed object result 暴露可变内部切片")
	}
	scope.OpenGaps[0] = "closed"
	if binding.Scope().OpenGaps[0] == "closed" {
		t.Fatal("scope 暴露可变内部切片")
	}
}

func TestW2ReviewFreezeCompileCommitObjectBindingV1CanonicalMultilineHeaders(t *testing.T) {
	treeSHA := strings.Repeat("1", 40)
	parentSHA := strings.Repeat("2", 40)
	payload := strings.Join([]string{
		"tree " + treeSHA,
		"parent " + parentSHA,
		"author Review Freeze <review-freeze@example.invalid> 1784114416 +0800",
		"committer Review Freeze <review-freeze@example.invalid> 1784114416 +0800",
		"encoding UTF-8",
		"gpgsig -----BEGIN PGP SIGNATURE-----",
		" iQEzBAABCAAdFiEEfixture",
		" -----END PGP SIGNATURE-----",
		"mergetag object " + parentSHA,
		" type commit",
		" tag v1.0.0",
		" tagger Review Freeze <review-freeze@example.invalid> 1784114416 +0800",
		" ",
		" signed release tag",
		"",
		"signed merge commit",
		"",
	}, "\n")
	statement, loader := reviewFreezeCompileCommitFixtureV1(t, []byte(payload), treeSHA)
	binding, err := reviewFreezeVerifyCompileCommitObjectBindingV1(context.Background(), statement, loader)
	if err != nil {
		t.Fatalf("canonical multiline headers rejected: %v", err)
	}
	if !reviewFreezeCompileCommitEqualStringsV1(binding.ParentSHAs(), []string{parentSHA}) {
		t.Fatalf("canonical parents=%v", binding.ParentSHAs())
	}
}

func TestW2ReviewFreezeCompileCommitObjectBindingV1AllowsOpaqueExtensionsAndMessageBytes(t *testing.T) {
	treeSHA := strings.Repeat("1", 40)
	parentSHA := strings.Repeat("2", 40)
	hiddenTreeSHA := strings.Repeat("3", 40)
	header := strings.Join([]string{
		"tree " + treeSHA,
		"parent " + parentSHA,
		"author opaque identity intentionally outside this proof",
		" generic author continuation",
		"committer opaque",
		"x-dora-extension opaque value",
		" tree " + hiddenTreeSHA,
		" parent " + hiddenTreeSHA,
		"gpgsig deliberately-not-an-armored-signature",
		" generic signature continuation",
		"encoding experimental value with spaces",
		"encoding repeated extension value",
	}, "\n")
	payload := append([]byte(header+"\n\nmessage with CRLF\r\nthen binary NUL "), 0)
	payload = append(payload, []byte(" tail")...)
	statement, loader := reviewFreezeCompileCommitFixtureV1(t, payload, treeSHA)
	binding, err := reviewFreezeVerifyCompileCommitObjectBindingV1(context.Background(), statement, loader)
	if err != nil {
		t.Fatalf("opaque extensions/message bytes rejected: %v", err)
	}
	if binding.TreeSHA() != treeSHA || !reviewFreezeCompileCommitEqualStringsV1(binding.ParentSHAs(), []string{parentSHA}) {
		t.Fatalf("opaque extension changed tree/parents tree=%q parents=%v", binding.TreeSHA(), binding.ParentSHAs())
	}

	minimalPayload := []byte("tree " + treeSHA + "\nx-minimal opaque\n\nmessage\n")
	minimalStatement, minimalLoader := reviewFreezeCompileCommitFixtureV1(t, minimalPayload, treeSHA)
	if _, err := reviewFreezeVerifyCompileCommitObjectBindingV1(context.Background(), minimalStatement, minimalLoader); err != nil {
		t.Fatalf("proof-only minimal commit headers rejected: %v", err)
	}
}

func TestW2ReviewFreezeCompileCommitObjectBindingV1RejectsHeaderAdversaries(t *testing.T) {
	treeSHA := strings.Repeat("1", 40)
	otherTreeSHA := strings.Repeat("3", 40)
	parentSHA := strings.Repeat("2", 40)
	base := string(reviewFreezeCompileCommitPayloadV1(treeSHA, parentSHA))
	cases := []struct {
		name    string
		payload string
		want    string
	}{
		{name: "missing tree", payload: strings.Replace(base, "tree "+treeSHA+"\n", "", 1), want: "tree header"},
		{name: "duplicate tree", payload: strings.Replace(base, "tree "+treeSHA+"\n", "tree "+treeSHA+"\ntree "+otherTreeSHA+"\n", 1), want: "tree header count"},
		{name: "tree not first", payload: strings.Replace(base, "tree "+treeSHA+"\nparent "+parentSHA, "parent "+parentSHA+"\ntree "+treeSHA, 1), want: "tree header"},
		{name: "uppercase tree", payload: strings.Replace(base, treeSHA, strings.Repeat("A", 40), 1), want: "tree SHA"},
		{name: "zero tree", payload: strings.Replace(base, treeSHA, strings.Repeat("0", 40), 1), want: "tree SHA"},
		{name: "short parent", payload: strings.Replace(base, parentSHA, "abc", 1), want: "parent SHA"},
		{name: "uppercase parent", payload: strings.Replace(base, parentSHA, strings.Repeat("A", 40), 1), want: "parent SHA"},
		{name: "zero parent", payload: strings.Replace(base, parentSHA, strings.Repeat("0", 40), 1), want: "parent SHA"},
		{name: "parent equals tree", payload: strings.Replace(base, parentSHA, treeSHA, 1), want: "parent SHA"},
		{name: "duplicate parent", payload: strings.Replace(base, "parent "+parentSHA+"\n", "parent "+parentSHA+"\nparent "+parentSHA+"\n", 1), want: "duplicate parent"},
		{name: "tree continuation", payload: strings.Replace(base, "tree "+treeSHA+"\n", "tree "+treeSHA+"\n opaque", 1), want: "tree 禁止 continuation"},
		{name: "parent continuation", payload: strings.Replace(base, "parent "+parentSHA+"\n", "parent "+parentSHA+"\n opaque\n", 1), want: "parent SHA"},
		{name: "parent after extension", payload: strings.Replace(base, "author Review", "x-extension value\nparent "+otherTreeSHA+"\nauthor Review", 1), want: "out-of-order parent"},
		{name: "orphan continuation", payload: " hidden\n" + base, want: "orphan continuation"},
		{name: "tab continuation", payload: strings.Replace(base, "committer Review", "\thidden\ncommitter Review", 1), want: "tab continuation"},
		{name: "unindented tree after gpgsig", payload: strings.Replace(base, "\n\ncompile", "\ngpgsig -----BEGIN PGP SIGNATURE-----\n -----END PGP SIGNATURE-----\ntree "+otherTreeSHA+"\n\ncompile", 1), want: "tree header count"},
		{name: "invalid uppercase header key", payload: strings.Replace(base, "author Review", "Author Review", 1), want: "key 非 canonical"},
		{name: "header CRLF", payload: strings.Replace(base, "tree "+treeSHA+"\n", "tree "+treeSHA+"\r\n", 1), want: "header 禁止 CR/CRLF"},
		{name: "header NUL", payload: strings.Replace(base, "tree "+treeSHA+"\n", "tree "+treeSHA+"\x00\n", 1), want: "header 禁止 NUL"},
		{name: "missing separator", payload: strings.Replace(base, "\n\n", "\n", 1), want: "separator"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			statement, loader := reviewFreezeCompileCommitFixtureV1(t, []byte(test.payload), treeSHA)
			_, err := reviewFreezeVerifyCompileCommitObjectBindingV1(context.Background(), statement, loader)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error=%v want contains %q", err, test.want)
			}
		})
	}
}

func TestW2ReviewFreezeCompileCommitObjectBindingV1RejectsTreeBindingDrift(t *testing.T) {
	statementTree := strings.Repeat("1", 40)
	commitTree := strings.Repeat("2", 40)
	statement, loader := reviewFreezeCompileCommitFixtureV1(t, reviewFreezeCompileCommitPayloadV1(commitTree), statementTree)
	_, err := reviewFreezeVerifyCompileCommitObjectBindingV1(context.Background(), statement, loader)
	if err == nil || !strings.Contains(err.Error(), "tree binding") {
		t.Fatalf("tree binding drift error=%v", err)
	}
}

func TestW2ReviewFreezeCompileCommitObjectBindingV1RejectsFrameAdversaries(t *testing.T) {
	treeSHA := strings.Repeat("1", 40)
	payload := reviewFreezeCompileCommitPayloadV1(treeSHA)
	canonical := reviewFreezeCompileCommitFrameV1(payload)
	separator := bytes.IndexByte(canonical, 0)
	contentAddressDrift := append([]byte(nil), canonical...)
	contentAddressDrift[len(contentAddressDrift)-1] ^= 1
	missingNUL := append([]byte(nil), canonical...)
	missingNUL[separator] = 'x'
	wrongType := append([]byte(nil), canonical...)
	copy(wrongType[:len("commit")], "foobar")
	leadingZero := append([]byte("commit 0"+strconv.Itoa(len(payload))+"\x00"), payload[:len(payload)-1]...)
	signedSize := append([]byte("commit +"+strconv.Itoa(len(payload))+"\x00"), payload[:len(payload)-1]...)
	cases := []struct {
		name   string
		frame  []byte
		want   string
		rebind bool
	}{
		{name: "content address drift", frame: contentAddressDrift, want: "framed SHA-1"},
		{name: "truncated frame", frame: canonical[:len(canonical)-1], want: "frame truncated"},
		{name: "oversized frame", frame: append(append([]byte(nil), canonical...), 'x'), want: "frame oversized"},
		{name: "missing nul", frame: missingNUL, want: "NUL separator", rebind: true},
		{name: "wrong object type", frame: wrongType, want: "object type", rebind: true},
		{name: "leading zero size", frame: leadingZero, want: "非 canonical", rebind: true},
		{name: "signed size", frame: signedSize, want: "非十进制", rebind: true},
		{name: "declared short", frame: append([]byte("commit "+strconv.Itoa(len(payload)-1)+"\x00"), payload...), want: "actual", rebind: true},
		{name: "declared long", frame: append([]byte("commit "+strconv.Itoa(len(payload)+1)+"\x00"), payload...), want: "actual", rebind: true},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			statement, loader := reviewFreezeCompileCommitFixtureV1(t, payload, treeSHA)
			loader.frame = append([]byte(nil), test.frame...)
			if test.rebind {
				statement.Subject.BaseCommitSHA = reviewFreezeCompileCommitObjectSHAV1(loader.frame)
				loader.descriptor = reviewFreezeCompileCommitDescriptorV1(statement.Subject.BaseCommitSHA, payload)
			}
			_, err := reviewFreezeVerifyCompileCommitObjectBindingV1(context.Background(), statement, loader)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error=%v want contains %q", err, test.want)
			}
		})
	}

	t.Run("body SHA-256 descriptor drift", func(t *testing.T) {
		statement, loader := reviewFreezeCompileCommitFixtureV1(t, payload, treeSHA)
		loader.descriptor.BodySHA256 = reviewFreezeSHA256V1([]byte("different non-empty body"))
		_, err := reviewFreezeVerifyCompileCommitObjectBindingV1(context.Background(), statement, loader)
		if err == nil || !strings.Contains(err.Error(), "descriptor drift") {
			t.Fatalf("body SHA descriptor error=%v", err)
		}
	})
}

func TestW2ReviewFreezeCompileCommitObjectBindingV1ParentBudget(t *testing.T) {
	treeSHA := strings.Repeat("1", 40)
	parents := make([]string, 0, reviewFreezeCompileCommitParentMaxCountV1+1)
	for index := 0; index <= reviewFreezeCompileCommitParentMaxCountV1; index++ {
		parents = append(parents, fmt.Sprintf("%040x", index+2))
	}
	statement, loader := reviewFreezeCompileCommitFixtureV1(t, reviewFreezeCompileCommitPayloadV1(treeSHA, parents...), treeSHA)
	_, err := reviewFreezeVerifyCompileCommitObjectBindingV1(context.Background(), statement, loader)
	if err == nil || !strings.Contains(err.Error(), "parent count") {
		t.Fatalf("parent budget error=%v", err)
	}
}

func TestW2ReviewFreezeCompileCommitObjectBindingV1RejectsListingAdversaries(t *testing.T) {
	treeSHA := strings.Repeat("1", 40)
	payload := reviewFreezeCompileCommitPayloadV1(treeSHA)
	statement, baseLoader := reviewFreezeCompileCommitFixtureV1(t, payload, treeSHA)
	valid := baseLoader.descriptor
	extra := valid
	extra.ObjectID = strings.Repeat("f", 40)
	cases := []struct {
		name    string
		listing []reviewFreezeCompileCommitObjectDescriptorV1
		want    string
	}{
		{name: "missing", listing: []reviewFreezeCompileCommitObjectDescriptorV1{}, want: "missing"},
		{name: "duplicate", listing: []reviewFreezeCompileCommitObjectDescriptorV1{valid, valid}, want: "duplicate"},
		{name: "extra", listing: []reviewFreezeCompileCommitObjectDescriptorV1{valid, extra}, want: "extra"},
		{name: "list budget", listing: []reviewFreezeCompileCommitObjectDescriptorV1{valid, extra, extra}, want: "budget"},
		{name: "uppercase oid", listing: []reviewFreezeCompileCommitObjectDescriptorV1{func() reviewFreezeCompileCommitObjectDescriptorV1 {
			value := valid
			value.ObjectID = strings.ToUpper(value.ObjectID)
			return value
		}()}, want: "lowercase"},
		{name: "zero oid", listing: []reviewFreezeCompileCommitObjectDescriptorV1{func() reviewFreezeCompileCommitObjectDescriptorV1 {
			value := valid
			value.ObjectID = strings.Repeat("0", 40)
			return value
		}()}, want: "lowercase"},
		{name: "wrong kind", listing: []reviewFreezeCompileCommitObjectDescriptorV1{func() reviewFreezeCompileCommitObjectDescriptorV1 {
			value := valid
			value.ObjectKind = "tree"
			return value
		}()}, want: "kind"},
		{name: "zero body size", listing: []reviewFreezeCompileCommitObjectDescriptorV1{func() reviewFreezeCompileCommitObjectDescriptorV1 {
			value := valid
			value.BodySizeBytes = 0
			return value
		}()}, want: "body size"},
		{name: "oversized body", listing: []reviewFreezeCompileCommitObjectDescriptorV1{func() reviewFreezeCompileCommitObjectDescriptorV1 {
			value := valid
			value.BodySizeBytes = reviewFreezeCompileCommitObjectMaxPayloadBytesV1 + 1
			return value
		}()}, want: "body size"},
		{name: "invalid body sha", listing: []reviewFreezeCompileCommitObjectDescriptorV1{func() reviewFreezeCompileCommitObjectDescriptorV1 {
			value := valid
			value.BodySHA256 = "sha256:bad"
			return value
		}()}, want: "SHA-256"},
		{name: "empty body sha", listing: []reviewFreezeCompileCommitObjectDescriptorV1{func() reviewFreezeCompileCommitObjectDescriptorV1 {
			value := valid
			value.BodySHA256 = reviewFreezeSHA256V1(nil)
			return value
		}()}, want: "SHA-256"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			listing := make([]reviewFreezeCompileCommitObjectDescriptorV1, len(test.listing))
			copy(listing, test.listing)
			loader := &reviewFreezeCompileCommitLoaderFixtureV1{
				descriptor:      valid,
				listingOverride: listing,
				frame:           append([]byte(nil), baseLoader.frame...),
			}
			_, err := reviewFreezeVerifyCompileCommitObjectBindingV1(context.Background(), statement, loader)
			if err == nil || !strings.Contains(err.Error(), test.want) || loader.listCalls != 1 || loader.openCalls != 0 {
				t.Fatalf("error=%v want=%q list=%d open=%d", err, test.want, loader.listCalls, loader.openCalls)
			}
		})
	}
}

func TestW2ReviewFreezeCompileCommitObjectBindingV1LoaderLifecycle(t *testing.T) {
	treeSHA := strings.Repeat("1", 40)
	payload := reviewFreezeCompileCommitPayloadV1(treeSHA)
	statement, validLoader := reviewFreezeCompileCommitFixtureV1(t, payload, treeSHA)

	if _, err := reviewFreezeVerifyCompileCommitObjectBindingV1(nil, statement, validLoader); err == nil || !strings.Contains(err.Error(), "context") {
		t.Fatalf("nil context error=%v", err)
	}
	if _, err := reviewFreezeVerifyCompileCommitObjectBindingV1(context.Background(), statement, nil); err == nil || !strings.Contains(err.Error(), "loader") {
		t.Fatalf("nil loader error=%v", err)
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := reviewFreezeVerifyCompileCommitObjectBindingV1(cancelled, statement, validLoader); err == nil || validLoader.listCalls != 0 || validLoader.openCalls != 0 {
		t.Fatalf("pre-cancel error=%v list_calls=%d open_calls=%d", err, validLoader.listCalls, validLoader.openCalls)
	}

	invalid := statement
	invalid.Subject.BaseTreeSHA = "main"
	invalidLoader := &reviewFreezeCompileCommitLoaderFixtureV1{descriptor: validLoader.descriptor, frame: validLoader.frame}
	if _, err := reviewFreezeVerifyCompileCommitObjectBindingV1(context.Background(), invalid, invalidLoader); err == nil || invalidLoader.listCalls != 0 || invalidLoader.openCalls != 0 {
		t.Fatalf("invalid statement error=%v list_calls=%d open_calls=%d", err, invalidLoader.listCalls, invalidLoader.openCalls)
	}

	listErrorLoader := &reviewFreezeCompileCommitLoaderFixtureV1{descriptor: validLoader.descriptor, frame: validLoader.frame, listErr: errors.New("cas list unavailable")}
	if _, err := reviewFreezeVerifyCompileCommitObjectBindingV1(context.Background(), statement, listErrorLoader); err == nil || listErrorLoader.listCalls != 1 || listErrorLoader.openCalls != 0 {
		t.Fatalf("list error lifecycle err=%v list=%d open=%d", err, listErrorLoader.listCalls, listErrorLoader.openCalls)
	}

	afterListContext, afterListCancel := context.WithCancel(context.Background())
	afterListLoader := &reviewFreezeCompileCommitLoaderFixtureV1{
		descriptor: validLoader.descriptor, frame: validLoader.frame, afterList: afterListCancel,
	}
	if _, err := reviewFreezeVerifyCompileCommitObjectBindingV1(afterListContext, statement, afterListLoader); err == nil || afterListLoader.openCalls != 0 {
		t.Fatalf("after-list cancellation err=%v open=%d", err, afterListLoader.openCalls)
	}

	openErrorReader := &reviewFreezeCompileCommitTrackingReaderV1{reader: bytes.NewReader(nil)}
	openErrorLoader := &reviewFreezeCompileCommitLoaderFixtureV1{
		descriptor: validLoader.descriptor, openErr: errors.New("cas unavailable"), openErrReader: openErrorReader,
	}
	if _, err := reviewFreezeVerifyCompileCommitObjectBindingV1(context.Background(), statement, openErrorLoader); err == nil || openErrorReader.closed != 1 {
		t.Fatalf("open error lifecycle err=%v closed=%d", err, openErrorReader.closed)
	}

	openDescriptorMutations := []struct {
		name   string
		mutate func(*reviewFreezeCompileCommitObjectDescriptorV1)
	}{
		{name: "oid", mutate: func(value *reviewFreezeCompileCommitObjectDescriptorV1) { value.ObjectID = strings.Repeat("f", 40) }},
		{name: "kind", mutate: func(value *reviewFreezeCompileCommitObjectDescriptorV1) { value.ObjectKind = "tree" }},
		{name: "size", mutate: func(value *reviewFreezeCompileCommitObjectDescriptorV1) { value.BodySizeBytes++ }},
		{name: "sha256", mutate: func(value *reviewFreezeCompileCommitObjectDescriptorV1) {
			value.BodySHA256 = reviewFreezeSHA256V1([]byte("changed"))
		}},
	}
	for _, test := range openDescriptorMutations {
		t.Run("open descriptor TOCTOU "+test.name, func(t *testing.T) {
			reader := &reviewFreezeCompileCommitTrackingReaderV1{reader: bytes.NewReader(validLoader.frame)}
			wrongDescriptor := validLoader.descriptor
			test.mutate(&wrongDescriptor)
			loader := &reviewFreezeCompileCommitLoaderFixtureV1{
				descriptor: validLoader.descriptor, openDescriptorOverride: &wrongDescriptor, readerOverride: reader,
			}
			_, err := reviewFreezeVerifyCompileCommitObjectBindingV1(context.Background(), statement, loader)
			if err == nil || !strings.Contains(err.Error(), "opened descriptor") || reader.closed != 1 {
				t.Fatalf("descriptor TOCTOU err=%v closed=%d", err, reader.closed)
			}
		})
	}

	// fixture 默认会创建内存 reader，因此用显式 loader 固定 nil-reader 对抗。
	nilLoader := reviewFreezeCompileCommitNilReaderLoaderV1{descriptor: validLoader.descriptor}
	if _, err := reviewFreezeVerifyCompileCommitObjectBindingV1(context.Background(), statement, nilLoader); err == nil || !strings.Contains(err.Error(), "nil reader") {
		t.Fatalf("nil reader error=%v", err)
	}
	afterOpenContext, afterOpenCancel := context.WithCancel(context.Background())
	afterOpenReader := &reviewFreezeCompileCommitTrackingReaderV1{reader: bytes.NewReader(validLoader.frame)}
	afterOpenLoader := &reviewFreezeCompileCommitLoaderFixtureV1{
		descriptor: validLoader.descriptor, readerOverride: afterOpenReader, afterOpen: afterOpenCancel,
	}
	if _, err := reviewFreezeVerifyCompileCommitObjectBindingV1(afterOpenContext, statement, afterOpenLoader); err == nil || afterOpenReader.closed != 1 {
		t.Fatalf("after-open cancellation err=%v closed=%d", err, afterOpenReader.closed)
	}

	readError := &reviewFreezeCompileCommitErrorReaderV1{err: errors.New("read failed")}
	readErrorLoader := &reviewFreezeCompileCommitLoaderFixtureV1{descriptor: validLoader.descriptor, readerOverride: readError}
	if _, err := reviewFreezeVerifyCompileCommitObjectBindingV1(context.Background(), statement, readErrorLoader); err == nil || readError.closed != 1 {
		t.Fatalf("read error lifecycle err=%v closed=%d", err, readError.closed)
	}

	closeErrorReader := &reviewFreezeCompileCommitTrackingReaderV1{reader: bytes.NewReader(validLoader.frame), closeErr: errors.New("close failed")}
	closeErrorLoader := &reviewFreezeCompileCommitLoaderFixtureV1{descriptor: validLoader.descriptor, readerOverride: closeErrorReader}
	if _, err := reviewFreezeVerifyCompileCommitObjectBindingV1(context.Background(), statement, closeErrorLoader); err == nil || !strings.Contains(err.Error(), "close") || closeErrorReader.closed != 1 {
		t.Fatalf("close error lifecycle err=%v closed=%d", err, closeErrorReader.closed)
	}
}

type reviewFreezeCompileCommitNilReaderLoaderV1 struct {
	descriptor reviewFreezeCompileCommitObjectDescriptorV1
}

func (loader reviewFreezeCompileCommitNilReaderLoaderV1) ListCommitObjects(context.Context) ([]reviewFreezeCompileCommitObjectDescriptorV1, error) {
	return []reviewFreezeCompileCommitObjectDescriptorV1{loader.descriptor}, nil
}

func (loader reviewFreezeCompileCommitNilReaderLoaderV1) OpenCommitObject(context.Context, string) (reviewFreezeCompileCommitObjectOpenedV1, error) {
	return reviewFreezeCompileCommitObjectOpenedV1{Descriptor: loader.descriptor}, nil
}

func TestW2ReviewFreezeCompileCommitObjectBindingV1CancellationInterruptsRead(t *testing.T) {
	treeSHA := strings.Repeat("1", 40)
	statement, validLoader := reviewFreezeCompileCommitFixtureV1(t, reviewFreezeCompileCommitPayloadV1(treeSHA), treeSHA)
	blocking := reviewFreezeCompileCommitBlockingReaderNewV1()
	loader := &reviewFreezeCompileCommitLoaderFixtureV1{descriptor: validLoader.descriptor, readerOverride: blocking}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := reviewFreezeVerifyCompileCommitObjectBindingV1(ctx, statement, loader)
		done <- err
	}()
	select {
	case <-blocking.started:
	case <-time.After(2 * time.Second):
		t.Fatal("blocking reader did not start")
	}
	cancel()
	select {
	case err := <-done:
		if err == nil || !errors.Is(err, context.Canceled) {
			t.Fatalf("cancellation error=%v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("context cancellation did not interrupt commit read")
	}
	select {
	case <-blocking.closed:
	default:
		t.Fatal("blocking reader was not closed")
	}
}
