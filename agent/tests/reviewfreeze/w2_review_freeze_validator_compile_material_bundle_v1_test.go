package reviewfreeze_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"reflect"
	"sort"
	"strings"
	"testing"
	"unicode/utf8"
)

const (
	reviewFreezeAttestationMaterialBundleSchemaV1        = "w2_attestation_material_bundle.v1"
	reviewFreezeAttestationMaterialBundleMaxJSONBytesV1  = 64 << 10
	reviewFreezeAttestationMaterialBundleMaxTotalBytesV1 = 128 << 20
	reviewFreezeAttestationMaterialBundleEntryCountV1    = 7

	reviewFreezeAttestationMaterialRoleBuildClosureV1 = "build_closure_projection"
	reviewFreezeAttestationMaterialRoleBuildInfoV1    = "build_info_raw"
	reviewFreezeAttestationMaterialRoleArtifactV1     = "compiled_test_binary"
	reviewFreezeAttestationMaterialRoleGoListV1       = "go_list_raw"
	reviewFreezeAttestationMaterialRoleSnapshotV1     = "input_snapshot"
	reviewFreezeAttestationMaterialRoleSBOMBinaryV1   = "sbom_generator_binary"
	reviewFreezeAttestationMaterialRoleSBOMRawV1      = "sbom_raw"
)

// reviewFreezeAttestationMaterialBundleV1 是无 URI 的 direct-material descriptor。它只
// 声明 strict statement 原始字节身份和该 statement 直接携带的 typed content-ref exact-set。
// 它不递归展开 input snapshot 内的 repository/module-cache 叶子材料。
type reviewFreezeAttestationMaterialBundleV1 struct {
	SchemaVersion          string                                         `json:"schema_version"`
	StatementSchemaVersion string                                         `json:"statement_schema_version"`
	StatementSHA256        string                                         `json:"statement_sha256"`
	StatementSizeBytes     int64                                          `json:"statement_size_bytes"`
	TotalMaterialSizeBytes int64                                          `json:"total_material_size_bytes"`
	Entries                []reviewFreezeAttestationMaterialBundleEntryV1 `json:"entries"`
}

// reviewFreezeAttestationMaterialBundleEntryV1 为一个稳定 role 绑定 statement 中的完整
// typed ref；role 不是 locator，loader 只能按不可变 ref 获取内容。
type reviewFreezeAttestationMaterialBundleEntryV1 struct {
	Role string                              `json:"role"`
	Ref  reviewFreezeAttestationContentRefV1 `json:"ref"`
}

// reviewFreezeAttestationMaterialLoaderV1 是 material verifier 的最小消费方接口。
// Open 每个 unique digest 最多调用一次；实现不得把 ref 解释为可变 URI。
type reviewFreezeAttestationMaterialLoaderV1 interface {
	Open(context.Context, reviewFreezeAttestationContentRefV1) (io.ReadCloser, error)
}

// reviewFreezeVerifiedAttestationMaterialV1 保存一次读取并验真的 immutable 内容。
// raw 使用 string 保持内部不可变，任何对外读取都重新复制为 []byte。
type reviewFreezeVerifiedAttestationMaterialV1 struct {
	ref reviewFreezeAttestationContentRefV1
	raw string
}

// reviewFreezeVerifiedAttestationMaterialBundleV1 是一次 verifier 调用的冻结结果。
// 下游只能复用这里的 bytes，不得按 ref 再次调用 loader。
type reviewFreezeVerifiedAttestationMaterialBundleV1 struct {
	statementRaw string
	roles        []string
	materials    map[string]reviewFreezeVerifiedAttestationMaterialV1
}

// StatementRaw 返回已完成 strict decode 且与 descriptor 摘要绑定的 statement 字节副本。
func (bundle *reviewFreezeVerifiedAttestationMaterialBundleV1) StatementRaw() []byte {
	if bundle == nil {
		return nil
	}
	return []byte(bundle.statementRaw)
}

// Roles 返回已验证 material role 的有序副本。
func (bundle *reviewFreezeVerifiedAttestationMaterialBundleV1) Roles() []string {
	if bundle == nil {
		return nil
	}
	return append([]string(nil), bundle.roles...)
}

// Material 返回指定 role 的 ref 值副本和 bytes 副本；它不会再次调用 loader。
func (bundle *reviewFreezeVerifiedAttestationMaterialBundleV1) Material(role string) (reviewFreezeAttestationContentRefV1, []byte, bool) {
	if bundle == nil {
		return reviewFreezeAttestationContentRefV1{}, nil, false
	}
	material, exists := bundle.materials[role]
	if !exists {
		return reviewFreezeAttestationContentRefV1{}, nil, false
	}
	return material.ref, []byte(material.raw), true
}

// reviewFreezeVerifyAttestationMaterialBundleV1 先完成 descriptor/statement 的纯校验，
// 再按 statement 派生顺序逐项单次有界读取。返回成功只证明七个 direct material 的
// bytes 与 ref size/SHA 一致；各 content schema 语义须由下游 validator 另行验证。
// 特别是 input snapshot 必须先完成语义校验，后续 two-phase resolver 才能派生并读取其
// 14 项 repository 与 15 项 module-cache 叶子 material。
func reviewFreezeVerifyAttestationMaterialBundleV1(
	ctx context.Context,
	descriptorRaw []byte,
	statementRaw []byte,
	loader reviewFreezeAttestationMaterialLoaderV1,
) (*reviewFreezeVerifiedAttestationMaterialBundleV1, error) {
	if ctx == nil {
		return nil, fmt.Errorf("material bundle context 不能为空")
	}
	if loader == nil {
		return nil, fmt.Errorf("material bundle loader 不能为空")
	}
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("material bundle context: %w", err)
	}

	statement, err := reviewFreezeDecodeCompileAttestationStatementJSONV1(statementRaw)
	if err != nil {
		return nil, fmt.Errorf("material bundle strict statement: %w", err)
	}
	descriptor, err := reviewFreezeDecodeAttestationMaterialBundleJSONV1(descriptorRaw)
	if err != nil {
		return nil, err
	}
	if err := reviewFreezeValidateAttestationMaterialBundleStatementBindingV1(descriptor, statementRaw, statement); err != nil {
		return nil, err
	}
	expectedEntries, err := reviewFreezeCompileAttestationMaterialEntriesV1(statement)
	if err != nil {
		return nil, err
	}
	if err := reviewFreezeValidateAttestationMaterialBundleEntriesV1(descriptor, expectedEntries); err != nil {
		return nil, err
	}

	verified := &reviewFreezeVerifiedAttestationMaterialBundleV1{
		statementRaw: string(statementRaw),
		roles:        make([]string, 0, len(expectedEntries)),
		materials:    make(map[string]reviewFreezeVerifiedAttestationMaterialV1, len(expectedEntries)),
	}
	loadedBytes := int64(0)
	for _, entry := range expectedEntries {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("material bundle context before role=%s: %w", entry.Role, err)
		}
		raw, loadErr := reviewFreezeLoadAttestationMaterialOnceV1(ctx, loader, entry)
		if loadErr != nil {
			return nil, loadErr
		}
		if int64(len(raw)) > reviewFreezeAttestationMaterialBundleMaxTotalBytesV1-loadedBytes {
			return nil, fmt.Errorf("material bundle loaded bytes 超出预算 role=%s", entry.Role)
		}
		loadedBytes += int64(len(raw))
		verified.roles = append(verified.roles, entry.Role)
		verified.materials[entry.Role] = reviewFreezeVerifiedAttestationMaterialV1{
			ref: entry.Ref,
			raw: string(raw),
		}
	}
	if loadedBytes != descriptor.TotalMaterialSizeBytes {
		return nil, fmt.Errorf("material bundle loaded total=%d want=%d", loadedBytes, descriptor.TotalMaterialSizeBytes)
	}
	return verified, nil
}

// reviewFreezeDecodeAttestationMaterialBundleJSONV1 拒绝超预算、非法 UTF-8、重复键、
// null、缺失、大小写 alias、未知字段、尾随值和非 canonical JSON。
func reviewFreezeDecodeAttestationMaterialBundleJSONV1(raw []byte) (reviewFreezeAttestationMaterialBundleV1, error) {
	var zero reviewFreezeAttestationMaterialBundleV1
	if len(raw) == 0 || len(raw) > reviewFreezeAttestationMaterialBundleMaxJSONBytesV1 {
		return zero, fmt.Errorf("material bundle descriptor size=%d limit=%d", len(raw), reviewFreezeAttestationMaterialBundleMaxJSONBytesV1)
	}
	if !utf8.Valid(raw) {
		return zero, fmt.Errorf("material bundle descriptor 不是合法 UTF-8")
	}
	if err := reviewFreezeInspectCompileAttestationJSONV1(raw); err != nil {
		return zero, fmt.Errorf("material bundle descriptor JSON: %w", err)
	}
	var generic any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&generic); err != nil {
		return zero, err
	}
	if err := reviewFreezeRejectCompileAttestationNullV1(generic, "$material_bundle"); err != nil {
		return zero, err
	}
	if err := reviewFreezeRequireCompileAttestationFieldsV1(generic, reflect.TypeOf(reviewFreezeAttestationMaterialBundleV1{}), "$material_bundle"); err != nil {
		return zero, err
	}

	var descriptor reviewFreezeAttestationMaterialBundleV1
	strictDecoder := json.NewDecoder(bytes.NewReader(raw))
	strictDecoder.DisallowUnknownFields()
	if err := strictDecoder.Decode(&descriptor); err != nil {
		return zero, err
	}
	var trailing any
	if err := strictDecoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return zero, fmt.Errorf("material bundle descriptor trailing JSON")
	}
	canonicalRaw, err := json.Marshal(descriptor)
	if err != nil {
		return zero, fmt.Errorf("编码 material bundle canonical JSON: %w", err)
	}
	if !bytes.Equal(raw, canonicalRaw) {
		return zero, fmt.Errorf("material bundle descriptor 必须使用 canonical JSON")
	}
	if descriptor.SchemaVersion != reviewFreezeAttestationMaterialBundleSchemaV1 {
		return zero, fmt.Errorf("material bundle schema_version=%q", descriptor.SchemaVersion)
	}
	return descriptor, nil
}

// reviewFreezeValidateAttestationMaterialBundleStatementBindingV1 将 descriptor 绑定到已
// strict decode 的 statement 原始字节，而不是重新编码后的 Go DTO。
func reviewFreezeValidateAttestationMaterialBundleStatementBindingV1(
	descriptor reviewFreezeAttestationMaterialBundleV1,
	statementRaw []byte,
	statement reviewFreezeValidatorCompileAttestationV1,
) error {
	if descriptor.StatementSchemaVersion != statement.SchemaVersion ||
		descriptor.StatementSchemaVersion != reviewFreezeValidatorCompileAttestationSchemaV1 {
		return fmt.Errorf("material bundle statement_schema_version=%q want=%q", descriptor.StatementSchemaVersion, statement.SchemaVersion)
	}
	wantSHA := reviewFreezeSHA256V1(statementRaw)
	wantSize := int64(len(statementRaw))
	if descriptor.StatementSHA256 != wantSHA || descriptor.StatementSizeBytes != wantSize {
		return fmt.Errorf("material bundle statement raw digest/size=%q/%d want=%q/%d", descriptor.StatementSHA256, descriptor.StatementSizeBytes, wantSHA, wantSize)
	}
	return nil
}

// reviewFreezeCompileAttestationMaterialEntriesV1 从 strict statement 派生唯一 direct typed
// ref。同 role 只允许完全相同的重复引用；不同 role 复用 digest 会因类型混淆而失败关闭。
func reviewFreezeCompileAttestationMaterialEntriesV1(statement reviewFreezeValidatorCompileAttestationV1) ([]reviewFreezeAttestationMaterialBundleEntryV1, error) {
	candidates := []reviewFreezeAttestationMaterialBundleEntryV1{
		{Role: reviewFreezeAttestationMaterialRoleBuildClosureV1, Ref: statement.Subject.BuildClosureProjectionRef},
		{Role: reviewFreezeAttestationMaterialRoleBuildInfoV1, Ref: statement.BuilderRun.Compile.BuildInfoRawRef},
		{Role: reviewFreezeAttestationMaterialRoleArtifactV1, Ref: statement.BuilderRun.Compile.ArtifactRef},
		{Role: reviewFreezeAttestationMaterialRoleGoListV1, Ref: statement.BuilderRun.GoListRawRef},
		{Role: reviewFreezeAttestationMaterialRoleSnapshotV1, Ref: statement.BuilderRun.InputSnapshotBeforeRef},
		{Role: reviewFreezeAttestationMaterialRoleSnapshotV1, Ref: statement.BuilderRun.InputSnapshotAfterRef},
		{Role: reviewFreezeAttestationMaterialRoleSBOMBinaryV1, Ref: statement.BuilderRun.SBOM.GeneratorBinaryRef},
		{Role: reviewFreezeAttestationMaterialRoleSBOMRawV1, Ref: statement.BuilderRun.SBOM.RawRef},
	}
	byRole := make(map[string]reviewFreezeAttestationContentRefV1, reviewFreezeAttestationMaterialBundleEntryCountV1)
	roleByDigest := make(map[string]string, reviewFreezeAttestationMaterialBundleEntryCountV1)
	for _, candidate := range candidates {
		if previous, exists := byRole[candidate.Role]; exists {
			if !reflect.DeepEqual(previous, candidate.Ref) {
				return nil, fmt.Errorf("material bundle statement role=%s 引用不一致", candidate.Role)
			}
			continue
		}
		if previousRole, exists := roleByDigest[candidate.Ref.SHA256]; exists {
			return nil, fmt.Errorf("material bundle statement 同 digest 跨 role=%s/%s digest=%q", previousRole, candidate.Role, candidate.Ref.SHA256)
		}
		byRole[candidate.Role] = candidate.Ref
		roleByDigest[candidate.Ref.SHA256] = candidate.Role
	}
	if len(byRole) != reviewFreezeAttestationMaterialBundleEntryCountV1 {
		return nil, fmt.Errorf("material bundle derived role 数量=%d want=%d", len(byRole), reviewFreezeAttestationMaterialBundleEntryCountV1)
	}
	roles := make([]string, 0, len(byRole))
	for role := range byRole {
		roles = append(roles, role)
	}
	sort.Strings(roles)
	entries := make([]reviewFreezeAttestationMaterialBundleEntryV1, 0, len(roles))
	for _, role := range roles {
		entries = append(entries, reviewFreezeAttestationMaterialBundleEntryV1{Role: role, Ref: byRole[role]})
	}
	return entries, nil
}

// reviewFreezeValidateAttestationMaterialBundleEntriesV1 验证 descriptor entry 排序、唯一、
// exact-set、typed ref 身份和总 material 预算；任何失败都发生在 loader 调用之前。
func reviewFreezeValidateAttestationMaterialBundleEntriesV1(
	descriptor reviewFreezeAttestationMaterialBundleV1,
	expected []reviewFreezeAttestationMaterialBundleEntryV1,
) error {
	if descriptor.Entries == nil {
		return fmt.Errorf("material bundle entries 必须显式声明数组")
	}
	expectedByRole := make(map[string]reviewFreezeAttestationContentRefV1, len(expected))
	for _, entry := range expected {
		expectedByRole[entry.Role] = entry.Ref
	}
	lastRole := ""
	seenDigests := make(map[string]string, len(descriptor.Entries))
	for index, entry := range descriptor.Entries {
		if index > 0 && entry.Role <= lastRole {
			return fmt.Errorf("material bundle entries role 未排序或重复=%q previous=%q", entry.Role, lastRole)
		}
		wantRef, exists := expectedByRole[entry.Role]
		if !exists {
			return fmt.Errorf("material bundle extra role=%q", entry.Role)
		}
		if previousRole, duplicate := seenDigests[entry.Ref.SHA256]; duplicate {
			return fmt.Errorf("material bundle 同 digest 跨 role=%s/%s digest=%q", previousRole, entry.Role, entry.Ref.SHA256)
		}
		seenDigests[entry.Ref.SHA256] = entry.Role
		if !reflect.DeepEqual(entry.Ref, wantRef) {
			return fmt.Errorf("material bundle role=%s typed ref mismatch=%+v want=%+v", entry.Role, entry.Ref, wantRef)
		}
		lastRole = entry.Role
	}
	if len(descriptor.Entries) != len(expected) {
		return fmt.Errorf("material bundle entries exact-set 长度=%d want=%d", len(descriptor.Entries), len(expected))
	}

	total := int64(0)
	for _, entry := range expected {
		if entry.Ref.SizeBytes > math.MaxInt64-total {
			return fmt.Errorf("material bundle total size 溢出")
		}
		total += entry.Ref.SizeBytes
	}
	if descriptor.TotalMaterialSizeBytes != total {
		return fmt.Errorf("material bundle total_material_size_bytes=%d want=%d", descriptor.TotalMaterialSizeBytes, total)
	}
	if total <= 0 || total > reviewFreezeAttestationMaterialBundleMaxTotalBytesV1 {
		return fmt.Errorf("material bundle total material budget=%d limit=%d", total, reviewFreezeAttestationMaterialBundleMaxTotalBytesV1)
	}
	return nil
}

// reviewFreezeLoadAttestationMaterialOnceV1 对单个 ref 只 Open 一次，并最多读取声明大小加
// 一个字节；这样即使 loader 返回无界流，也能区分 truncated/oversized 而不无限分配内存。
func reviewFreezeLoadAttestationMaterialOnceV1(
	ctx context.Context,
	loader reviewFreezeAttestationMaterialLoaderV1,
	entry reviewFreezeAttestationMaterialBundleEntryV1,
) ([]byte, error) {
	reader, err := loader.Open(ctx, entry.Ref)
	if err != nil {
		return nil, fmt.Errorf("material bundle 打开 role=%s digest=%q: %w", entry.Role, entry.Ref.SHA256, err)
	}
	if reader == nil {
		return nil, fmt.Errorf("material bundle missing role=%s digest=%q", entry.Role, entry.Ref.SHA256)
	}
	limited := io.LimitReader(reader, entry.Ref.SizeBytes+1)
	raw, readErr := io.ReadAll(limited)
	closeErr := reader.Close()
	if readErr != nil {
		return nil, fmt.Errorf("material bundle 读取 role=%s: %w", entry.Role, readErr)
	}
	if closeErr != nil {
		return nil, fmt.Errorf("material bundle 关闭 role=%s: %w", entry.Role, closeErr)
	}
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("material bundle context after role=%s: %w", entry.Role, err)
	}
	actualSize := int64(len(raw))
	switch {
	case actualSize < entry.Ref.SizeBytes:
		return nil, fmt.Errorf("material bundle truncated actual role=%s size=%d want=%d", entry.Role, actualSize, entry.Ref.SizeBytes)
	case actualSize > entry.Ref.SizeBytes:
		return nil, fmt.Errorf("material bundle oversized actual role=%s size>=%d want=%d", entry.Role, actualSize, entry.Ref.SizeBytes)
	}
	actualSHA := reviewFreezeSHA256V1(raw)
	if actualSHA != entry.Ref.SHA256 {
		return nil, fmt.Errorf("material bundle hash mismatch role=%s actual=%q want=%q", entry.Role, actualSHA, entry.Ref.SHA256)
	}
	return raw, nil
}

// reviewFreezeAttestationMaterialBundleFixtureV1 聚合 descriptor、strict statement 和
// content-addressed 原始 material，供 verifier 对抗测试复用。
type reviewFreezeAttestationMaterialBundleFixtureV1 struct {
	Descriptor    reviewFreezeAttestationMaterialBundleV1
	DescriptorRaw []byte
	Statement     reviewFreezeValidatorCompileAttestationV1
	StatementRaw  []byte
	Materials     map[string][]byte
}

// reviewFreezeAttestationMaterialLoaderFixtureV1 是只按 digest 读取的内存 loader，并记录
// 每个 digest 的 Open 次数以证明 verifier 没有二次加载。
type reviewFreezeAttestationMaterialLoaderFixtureV1 struct {
	materials  map[string][]byte
	overrides  map[string][]byte
	openErrors map[string]error
	calls      map[string]int
}

// Open 实现 content-addressed 单次打开；测试注入的 override/error 不改变 verifier 逻辑。
func (loader *reviewFreezeAttestationMaterialLoaderFixtureV1) Open(ctx context.Context, ref reviewFreezeAttestationContentRefV1) (io.ReadCloser, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	loader.calls[ref.SHA256]++
	if openErr := loader.openErrors[ref.SHA256]; openErr != nil {
		return nil, openErr
	}
	if override, exists := loader.overrides[ref.SHA256]; exists {
		return io.NopCloser(bytes.NewReader(override)), nil
	}
	raw, exists := loader.materials[ref.SHA256]
	if !exists {
		return nil, nil
	}
	return io.NopCloser(bytes.NewReader(raw)), nil
}

func reviewFreezeAttestationMaterialBundleFixtureRefV1(ref reviewFreezeAttestationContentRefV1, raw []byte) reviewFreezeAttestationContentRefV1 {
	ref.SHA256 = reviewFreezeSHA256V1(raw)
	ref.SizeBytes = int64(len(raw))
	return ref
}

func reviewFreezeAttestationMaterialBundleFixtureStatementV1(t *testing.T) (reviewFreezeValidatorCompileAttestationV1, map[string][]byte) {
	t.Helper()
	statement := reviewFreezeCompileAttestationFixtureStatementV1(t)
	roleBytes := map[string][]byte{
		reviewFreezeAttestationMaterialRoleBuildClosureV1: []byte("material: build closure projection"),
		reviewFreezeAttestationMaterialRoleBuildInfoV1:    []byte("material: go version -m output"),
		reviewFreezeAttestationMaterialRoleArtifactV1:     []byte("material: compiled test binary"),
		reviewFreezeAttestationMaterialRoleGoListV1:       []byte("material: raw go list JSON stream"),
		reviewFreezeAttestationMaterialRoleSnapshotV1:     []byte("material: canonical input snapshot JSON"),
		reviewFreezeAttestationMaterialRoleSBOMBinaryV1:   []byte("material: deterministic SBOM generator binary"),
		reviewFreezeAttestationMaterialRoleSBOMRawV1:      []byte("material: deterministic CycloneDX JSON"),
	}

	statement.Subject.BuildClosureProjectionRef = reviewFreezeAttestationMaterialBundleFixtureRefV1(
		statement.Subject.BuildClosureProjectionRef,
		roleBytes[reviewFreezeAttestationMaterialRoleBuildClosureV1],
	)
	snapshotRef := reviewFreezeAttestationMaterialBundleFixtureRefV1(
		statement.BuilderRun.InputSnapshotBeforeRef,
		roleBytes[reviewFreezeAttestationMaterialRoleSnapshotV1],
	)
	statement.BuilderRun.InputSnapshotBeforeRef = snapshotRef
	statement.BuilderRun.InputSnapshotAfterRef = snapshotRef

	goListRef := reviewFreezeAttestationMaterialBundleFixtureRefV1(
		statement.BuilderRun.GoListRawRef,
		roleBytes[reviewFreezeAttestationMaterialRoleGoListV1],
	)
	statement.BuilderRun.GoListRawRef = goListRef
	statement.BuilderRun.GoListInvocation.StdoutSHA256 = goListRef.SHA256
	statement.BuilderRun.GoListInvocation.StdoutSizeBytes = goListRef.SizeBytes

	artifactRef := reviewFreezeAttestationMaterialBundleFixtureRefV1(
		statement.BuilderRun.Compile.ArtifactRef,
		roleBytes[reviewFreezeAttestationMaterialRoleArtifactV1],
	)
	statement.BuilderRun.Compile.ArtifactRef = artifactRef
	statement.BuilderRun.Test.PreExecutionArtifactSHA256 = artifactRef.SHA256
	statement.BuilderRun.Test.PostExecutionArtifactSHA256 = artifactRef.SHA256
	statement.BuilderRun.SBOM.SubjectArtifactSHA256 = artifactRef.SHA256

	buildInfoRef := reviewFreezeAttestationMaterialBundleFixtureRefV1(
		statement.BuilderRun.Compile.BuildInfoRawRef,
		roleBytes[reviewFreezeAttestationMaterialRoleBuildInfoV1],
	)
	statement.BuilderRun.Compile.BuildInfoRawRef = buildInfoRef
	statement.BuilderRun.Compile.BuildInfoInvocation.StdoutSHA256 = buildInfoRef.SHA256
	statement.BuilderRun.Compile.BuildInfoInvocation.StdoutSizeBytes = buildInfoRef.SizeBytes

	statement.BuilderRun.SBOM.GeneratorBinaryRef = reviewFreezeAttestationMaterialBundleFixtureRefV1(
		statement.BuilderRun.SBOM.GeneratorBinaryRef,
		roleBytes[reviewFreezeAttestationMaterialRoleSBOMBinaryV1],
	)
	sbomRawRef := reviewFreezeAttestationMaterialBundleFixtureRefV1(
		statement.BuilderRun.SBOM.RawRef,
		roleBytes[reviewFreezeAttestationMaterialRoleSBOMRawV1],
	)
	statement.BuilderRun.SBOM.RawRef = sbomRawRef
	statement.BuilderRun.SBOM.Invocation.StdoutSHA256 = sbomRawRef.SHA256
	statement.BuilderRun.SBOM.Invocation.StdoutSizeBytes = sbomRawRef.SizeBytes
	return statement, roleBytes
}

func reviewFreezeAttestationMaterialBundleDescriptorV1(
	statementRaw []byte,
	statement reviewFreezeValidatorCompileAttestationV1,
) (reviewFreezeAttestationMaterialBundleV1, error) {
	entries, err := reviewFreezeCompileAttestationMaterialEntriesV1(statement)
	if err != nil {
		return reviewFreezeAttestationMaterialBundleV1{}, err
	}
	total := int64(0)
	for _, entry := range entries {
		total += entry.Ref.SizeBytes
	}
	return reviewFreezeAttestationMaterialBundleV1{
		SchemaVersion:          reviewFreezeAttestationMaterialBundleSchemaV1,
		StatementSchemaVersion: statement.SchemaVersion,
		StatementSHA256:        reviewFreezeSHA256V1(statementRaw),
		StatementSizeBytes:     int64(len(statementRaw)),
		TotalMaterialSizeBytes: total,
		Entries:                entries,
	}, nil
}

func reviewFreezeAttestationMaterialBundleMarshalFixtureV1(t *testing.T, value any) []byte {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal material bundle fixture: %v", err)
	}
	return raw
}

func reviewFreezeAttestationMaterialBundleFixtureNewV1(t *testing.T) reviewFreezeAttestationMaterialBundleFixtureV1 {
	t.Helper()
	statement, roleBytes := reviewFreezeAttestationMaterialBundleFixtureStatementV1(t)
	statementRaw := reviewFreezeAttestationMaterialBundleMarshalFixtureV1(t, statement)
	if _, err := reviewFreezeDecodeCompileAttestationStatementJSONV1(statementRaw); err != nil {
		t.Fatalf("material bundle fixture statement invalid: %v", err)
	}
	descriptor, err := reviewFreezeAttestationMaterialBundleDescriptorV1(statementRaw, statement)
	if err != nil {
		t.Fatalf("build material bundle descriptor: %v", err)
	}
	materials := make(map[string][]byte, len(descriptor.Entries))
	for _, entry := range descriptor.Entries {
		materials[entry.Ref.SHA256] = append([]byte(nil), roleBytes[entry.Role]...)
	}
	return reviewFreezeAttestationMaterialBundleFixtureV1{
		Descriptor:    descriptor,
		DescriptorRaw: reviewFreezeAttestationMaterialBundleMarshalFixtureV1(t, descriptor),
		Statement:     statement,
		StatementRaw:  statementRaw,
		Materials:     materials,
	}
}

func reviewFreezeAttestationMaterialLoaderFixtureNewV1(materials map[string][]byte) *reviewFreezeAttestationMaterialLoaderFixtureV1 {
	cloned := make(map[string][]byte, len(materials))
	for digest, raw := range materials {
		cloned[digest] = append([]byte(nil), raw...)
	}
	return &reviewFreezeAttestationMaterialLoaderFixtureV1{
		materials:  cloned,
		overrides:  make(map[string][]byte),
		openErrors: make(map[string]error),
		calls:      make(map[string]int),
	}
}

func reviewFreezeAttestationMaterialLoaderCallCountV1(loader *reviewFreezeAttestationMaterialLoaderFixtureV1) int {
	total := 0
	for _, calls := range loader.calls {
		total += calls
	}
	return total
}

func TestW2ReviewFreezeCompileMaterialBundleV1(t *testing.T) {
	fixture := reviewFreezeAttestationMaterialBundleFixtureNewV1(t)
	loader := reviewFreezeAttestationMaterialLoaderFixtureNewV1(fixture.Materials)
	verified, err := reviewFreezeVerifyAttestationMaterialBundleV1(context.Background(), fixture.DescriptorRaw, fixture.StatementRaw, loader)
	if err != nil {
		t.Fatalf("valid material bundle rejected: %v", err)
	}
	if got := reviewFreezeAttestationMaterialLoaderCallCountV1(loader); got != reviewFreezeAttestationMaterialBundleEntryCountV1 {
		t.Fatalf("loader call count=%d want=%d", got, reviewFreezeAttestationMaterialBundleEntryCountV1)
	}
	if !reflect.DeepEqual(verified.Roles(), func() []string {
		roles := make([]string, 0, len(fixture.Descriptor.Entries))
		for _, entry := range fixture.Descriptor.Entries {
			roles = append(roles, entry.Role)
		}
		return roles
	}()) {
		t.Fatalf("verified roles=%v", verified.Roles())
	}
	for _, entry := range fixture.Descriptor.Entries {
		if loader.calls[entry.Ref.SHA256] != 1 {
			t.Fatalf("role=%s loader calls=%d want=1", entry.Role, loader.calls[entry.Ref.SHA256])
		}
		want := append([]byte(nil), fixture.Materials[entry.Ref.SHA256]...)
		ref, first, exists := verified.Material(entry.Role)
		if !exists || !reflect.DeepEqual(ref, entry.Ref) || !bytes.Equal(first, want) {
			t.Fatalf("verified role=%s ref/raw mismatch", entry.Role)
		}
		first[0] ^= 0xff
		loader.materials[entry.Ref.SHA256][0] ^= 0xff
		_, second, exists := verified.Material(entry.Role)
		if !exists || !bytes.Equal(second, want) {
			t.Fatalf("verified role=%s bytes 不是 immutable copy", entry.Role)
		}
	}
	statementFirst := verified.StatementRaw()
	statementFirst[0] ^= 0xff
	if !bytes.Equal(verified.StatementRaw(), fixture.StatementRaw) {
		t.Fatal("verified statement raw 不是 immutable copy")
	}
	if got := reviewFreezeAttestationMaterialLoaderCallCountV1(loader); got != reviewFreezeAttestationMaterialBundleEntryCountV1 {
		t.Fatalf("读取 verified bytes 后 loader 被再次调用=%d", got)
	}
}

func TestW2ReviewFreezeCompileMaterialBundleDescriptorAdversarialV1(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*reviewFreezeAttestationMaterialBundleV1)
		want   string
	}{
		{name: "missing entry", mutate: func(descriptor *reviewFreezeAttestationMaterialBundleV1) {
			descriptor.Entries = descriptor.Entries[:len(descriptor.Entries)-1]
		}, want: "exact-set 长度"},
		{name: "duplicate entry", mutate: func(descriptor *reviewFreezeAttestationMaterialBundleV1) {
			descriptor.Entries = append(descriptor.Entries, descriptor.Entries[len(descriptor.Entries)-1])
		}, want: "重复"},
		{name: "extra entry", mutate: func(descriptor *reviewFreezeAttestationMaterialBundleV1) {
			descriptor.Entries = append(descriptor.Entries, reviewFreezeAttestationMaterialBundleEntryV1{Role: "zz_extra", Ref: descriptor.Entries[0].Ref})
		}, want: "extra role"},
		{name: "role confusion", mutate: func(descriptor *reviewFreezeAttestationMaterialBundleV1) {
			descriptor.Entries[0].Role = "build_closure_projection_alias"
		}, want: "extra role"},
		{name: "content schema confusion", mutate: func(descriptor *reviewFreezeAttestationMaterialBundleV1) {
			descriptor.Entries[0].Ref.ContentSchemaVersion = "other.v1"
		}, want: "typed ref mismatch"},
		{name: "media confusion", mutate: func(descriptor *reviewFreezeAttestationMaterialBundleV1) {
			descriptor.Entries[0].Ref.MediaType = "application/octet-stream"
		}, want: "typed ref mismatch"},
		{name: "declared size mismatch", mutate: func(descriptor *reviewFreezeAttestationMaterialBundleV1) {
			descriptor.Entries[0].Ref.SizeBytes++
		}, want: "typed ref mismatch"},
		{name: "descriptor cross role digest", mutate: func(descriptor *reviewFreezeAttestationMaterialBundleV1) {
			descriptor.Entries[1].Ref.SHA256 = descriptor.Entries[0].Ref.SHA256
		}, want: "同 digest 跨 role"},
		{name: "total size mismatch", mutate: func(descriptor *reviewFreezeAttestationMaterialBundleV1) {
			descriptor.TotalMaterialSizeBytes++
		}, want: "total_material_size_bytes"},
		{name: "statement digest mismatch", mutate: func(descriptor *reviewFreezeAttestationMaterialBundleV1) {
			descriptor.StatementSHA256 = reviewFreezeSHA256V1([]byte("other statement"))
		}, want: "statement raw digest/size"},
		{name: "statement size mismatch", mutate: func(descriptor *reviewFreezeAttestationMaterialBundleV1) {
			descriptor.StatementSizeBytes++
		}, want: "statement raw digest/size"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := reviewFreezeAttestationMaterialBundleFixtureNewV1(t)
			descriptor := fixture.Descriptor
			descriptor.Entries = append([]reviewFreezeAttestationMaterialBundleEntryV1(nil), descriptor.Entries...)
			test.mutate(&descriptor)
			descriptorRaw := reviewFreezeAttestationMaterialBundleMarshalFixtureV1(t, descriptor)
			loader := reviewFreezeAttestationMaterialLoaderFixtureNewV1(fixture.Materials)
			_, err := reviewFreezeVerifyAttestationMaterialBundleV1(context.Background(), descriptorRaw, fixture.StatementRaw, loader)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error=%v want contains %q", err, test.want)
			}
			if calls := reviewFreezeAttestationMaterialLoaderCallCountV1(loader); calls != 0 {
				t.Fatalf("descriptor 非法时 loader calls=%d want=0", calls)
			}
		})
	}
}

func TestW2ReviewFreezeCompileMaterialBundleLoaderAdversarialV1(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*reviewFreezeAttestationMaterialLoaderFixtureV1, reviewFreezeAttestationMaterialBundleEntryV1)
		want   string
	}{
		{name: "missing", mutate: func(loader *reviewFreezeAttestationMaterialLoaderFixtureV1, entry reviewFreezeAttestationMaterialBundleEntryV1) {
			delete(loader.materials, entry.Ref.SHA256)
		}, want: "missing"},
		{name: "loader error", mutate: func(loader *reviewFreezeAttestationMaterialLoaderFixtureV1, entry reviewFreezeAttestationMaterialBundleEntryV1) {
			loader.openErrors[entry.Ref.SHA256] = errors.New("storage unavailable")
		}, want: "storage unavailable"},
		{name: "truncated actual", mutate: func(loader *reviewFreezeAttestationMaterialLoaderFixtureV1, entry reviewFreezeAttestationMaterialBundleEntryV1) {
			raw := loader.materials[entry.Ref.SHA256]
			loader.overrides[entry.Ref.SHA256] = append([]byte(nil), raw[:len(raw)-1]...)
		}, want: "truncated actual"},
		{name: "oversized actual", mutate: func(loader *reviewFreezeAttestationMaterialLoaderFixtureV1, entry reviewFreezeAttestationMaterialBundleEntryV1) {
			loader.overrides[entry.Ref.SHA256] = append(append([]byte(nil), loader.materials[entry.Ref.SHA256]...), 'x')
		}, want: "oversized actual"},
		{name: "hash mismatch", mutate: func(loader *reviewFreezeAttestationMaterialLoaderFixtureV1, entry reviewFreezeAttestationMaterialBundleEntryV1) {
			raw := append([]byte(nil), loader.materials[entry.Ref.SHA256]...)
			raw[0] ^= 0xff
			loader.overrides[entry.Ref.SHA256] = raw
		}, want: "hash mismatch"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := reviewFreezeAttestationMaterialBundleFixtureNewV1(t)
			firstEntry := fixture.Descriptor.Entries[0]
			loader := reviewFreezeAttestationMaterialLoaderFixtureNewV1(fixture.Materials)
			test.mutate(loader, firstEntry)
			_, err := reviewFreezeVerifyAttestationMaterialBundleV1(context.Background(), fixture.DescriptorRaw, fixture.StatementRaw, loader)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error=%v want contains %q", err, test.want)
			}
			if calls := reviewFreezeAttestationMaterialLoaderCallCountV1(loader); calls != 1 || loader.calls[firstEntry.Ref.SHA256] != 1 {
				t.Fatalf("loader calls=%d target=%d want=1/1", calls, loader.calls[firstEntry.Ref.SHA256])
			}
		})
	}
}

func TestW2ReviewFreezeCompileMaterialBundleStrictJSONV1(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*testing.T, []byte) []byte
		want   string
	}{
		{name: "duplicate", mutate: func(t *testing.T, raw []byte) []byte {
			t.Helper()
			prefix := []byte(`{"schema_version":`)
			duplicate := []byte(`{"schema_version":"w2_attestation_material_bundle.v1","schema_version":`)
			return bytes.Replace(raw, prefix, duplicate, 1)
		}, want: "duplicate field"},
		{name: "top alias", mutate: func(t *testing.T, raw []byte) []byte {
			return reviewFreezeAttestationMaterialBundleMutateObjectV1(t, raw, func(object map[string]any) {
				object["SCHEMA_VERSION"] = object["schema_version"]
			})
		}, want: "unknown field"},
		{name: "nested alias", mutate: func(t *testing.T, raw []byte) []byte {
			return reviewFreezeAttestationMaterialBundleMutateObjectV1(t, raw, func(object map[string]any) {
				entries := object["entries"].([]any)
				entry := entries[0].(map[string]any)
				entry["Role"] = entry["role"]
			})
		}, want: "unknown field"},
		{name: "unknown URI", mutate: func(t *testing.T, raw []byte) []byte {
			return reviewFreezeAttestationMaterialBundleMutateObjectV1(t, raw, func(object map[string]any) {
				object["uri"] = "file:///tmp/material-bundle"
			})
		}, want: "unknown field"},
		{name: "nested ref URI", mutate: func(t *testing.T, raw []byte) []byte {
			return reviewFreezeAttestationMaterialBundleMutateObjectV1(t, raw, func(object map[string]any) {
				entries := object["entries"].([]any)
				entry := entries[0].(map[string]any)
				ref := entry["ref"].(map[string]any)
				ref["uri"] = "s3://bucket/key"
			})
		}, want: "unknown field"},
		{name: "null entries", mutate: func(t *testing.T, raw []byte) []byte {
			return reviewFreezeAttestationMaterialBundleMutateObjectV1(t, raw, func(object map[string]any) {
				object["entries"] = nil
			})
		}, want: "禁止 null"},
		{name: "trailing whitespace", mutate: func(_ *testing.T, raw []byte) []byte {
			return append(raw, '\n')
		}, want: "canonical JSON"},
		{name: "reordered", mutate: func(t *testing.T, raw []byte) []byte {
			t.Helper()
			var object map[string]any
			if err := json.Unmarshal(raw, &object); err != nil {
				t.Fatalf("decode descriptor: %v", err)
			}
			return reviewFreezeAttestationMaterialBundleMarshalFixtureV1(t, object)
		}, want: "canonical JSON"},
		{name: "descriptor size budget", mutate: func(_ *testing.T, raw []byte) []byte {
			padding := reviewFreezeAttestationMaterialBundleMaxJSONBytesV1 - len(raw) + 1
			return append(raw, bytes.Repeat([]byte(" "), padding)...)
		}, want: "descriptor size"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := reviewFreezeAttestationMaterialBundleFixtureNewV1(t)
			descriptorRaw := test.mutate(t, append([]byte(nil), fixture.DescriptorRaw...))
			loader := reviewFreezeAttestationMaterialLoaderFixtureNewV1(fixture.Materials)
			_, err := reviewFreezeVerifyAttestationMaterialBundleV1(context.Background(), descriptorRaw, fixture.StatementRaw, loader)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error=%v want contains %q", err, test.want)
			}
			if calls := reviewFreezeAttestationMaterialLoaderCallCountV1(loader); calls != 0 {
				t.Fatalf("strict JSON 非法时 loader calls=%d", calls)
			}
		})
	}
}

func reviewFreezeAttestationMaterialBundleMutateObjectV1(t *testing.T, raw []byte, mutate func(map[string]any)) []byte {
	t.Helper()
	var object map[string]any
	if err := json.Unmarshal(raw, &object); err != nil {
		t.Fatalf("decode material descriptor object: %v", err)
	}
	mutate(object)
	return reviewFreezeAttestationMaterialBundleMarshalFixtureV1(t, object)
}

func TestW2ReviewFreezeCompileMaterialBundleStatementAndBudgetAdversarialV1(t *testing.T) {
	t.Run("strict statement unknown", func(t *testing.T) {
		fixture := reviewFreezeAttestationMaterialBundleFixtureNewV1(t)
		statementRaw := reviewFreezeAttestationMaterialBundleMutateObjectV1(t, fixture.StatementRaw, func(object map[string]any) {
			object["status"] = "trusted"
		})
		descriptor := fixture.Descriptor
		descriptor.StatementSHA256 = reviewFreezeSHA256V1(statementRaw)
		descriptor.StatementSizeBytes = int64(len(statementRaw))
		loader := reviewFreezeAttestationMaterialLoaderFixtureNewV1(fixture.Materials)
		_, err := reviewFreezeVerifyAttestationMaterialBundleV1(context.Background(), reviewFreezeAttestationMaterialBundleMarshalFixtureV1(t, descriptor), statementRaw, loader)
		if err == nil || !strings.Contains(err.Error(), "unknown field") {
			t.Fatalf("strict statement error=%v", err)
		}
		if calls := reviewFreezeAttestationMaterialLoaderCallCountV1(loader); calls != 0 {
			t.Fatalf("strict statement 非法时 loader calls=%d", calls)
		}
	})

	t.Run("statement raw binding", func(t *testing.T) {
		fixture := reviewFreezeAttestationMaterialBundleFixtureNewV1(t)
		statementRaw := append(append([]byte(nil), fixture.StatementRaw...), '\n')
		loader := reviewFreezeAttestationMaterialLoaderFixtureNewV1(fixture.Materials)
		_, err := reviewFreezeVerifyAttestationMaterialBundleV1(context.Background(), fixture.DescriptorRaw, statementRaw, loader)
		if err == nil || !strings.Contains(err.Error(), "statement raw digest/size") {
			t.Fatalf("statement binding error=%v", err)
		}
		if calls := reviewFreezeAttestationMaterialLoaderCallCountV1(loader); calls != 0 {
			t.Fatalf("statement binding 非法时 loader calls=%d", calls)
		}
	})

	t.Run("same digest across statement roles", func(t *testing.T) {
		fixture := reviewFreezeAttestationMaterialBundleFixtureNewV1(t)
		statement := fixture.Statement
		statement.Subject.BuildClosureProjectionRef.SHA256 = statement.BuilderRun.InputSnapshotBeforeRef.SHA256
		statementRaw := reviewFreezeAttestationMaterialBundleMarshalFixtureV1(t, statement)
		descriptor := fixture.Descriptor
		descriptor.StatementSHA256 = reviewFreezeSHA256V1(statementRaw)
		descriptor.StatementSizeBytes = int64(len(statementRaw))
		loader := reviewFreezeAttestationMaterialLoaderFixtureNewV1(fixture.Materials)
		_, err := reviewFreezeVerifyAttestationMaterialBundleV1(context.Background(), reviewFreezeAttestationMaterialBundleMarshalFixtureV1(t, descriptor), statementRaw, loader)
		if err == nil || !strings.Contains(err.Error(), "同 digest 跨 role") {
			t.Fatalf("cross-role digest error=%v", err)
		}
		if calls := reviewFreezeAttestationMaterialLoaderCallCountV1(loader); calls != 0 {
			t.Fatalf("statement ref 非法时 loader calls=%d", calls)
		}
	})

	t.Run("total material budget", func(t *testing.T) {
		fixture := reviewFreezeAttestationMaterialBundleFixtureNewV1(t)
		statement := fixture.Statement
		statement.BuilderRun.GoListRawRef.SizeBytes = reviewFreezeCompileAttestationGoListRawMaxBytesV1
		statement.BuilderRun.GoListInvocation.StdoutSizeBytes = reviewFreezeCompileAttestationGoListRawMaxBytesV1
		statement.BuilderRun.Compile.ArtifactRef.SizeBytes = reviewFreezeCompileAttestationArtifactMaxBytesV1
		statementRaw := reviewFreezeAttestationMaterialBundleMarshalFixtureV1(t, statement)
		if _, err := reviewFreezeDecodeCompileAttestationStatementJSONV1(statementRaw); err != nil {
			t.Fatalf("budget fixture statement invalid: %v", err)
		}
		descriptor, err := reviewFreezeAttestationMaterialBundleDescriptorV1(statementRaw, statement)
		if err != nil {
			t.Fatalf("budget descriptor: %v", err)
		}
		loader := reviewFreezeAttestationMaterialLoaderFixtureNewV1(fixture.Materials)
		_, err = reviewFreezeVerifyAttestationMaterialBundleV1(context.Background(), reviewFreezeAttestationMaterialBundleMarshalFixtureV1(t, descriptor), statementRaw, loader)
		if err == nil || !strings.Contains(err.Error(), "total material budget") {
			t.Fatalf("total budget error=%v", err)
		}
		if calls := reviewFreezeAttestationMaterialLoaderCallCountV1(loader); calls != 0 {
			t.Fatalf("预算超限时 loader calls=%d", calls)
		}
	})
}

func TestW2ReviewFreezeCompileMaterialBundleContextV1(t *testing.T) {
	fixture := reviewFreezeAttestationMaterialBundleFixtureNewV1(t)
	loader := reviewFreezeAttestationMaterialLoaderFixtureNewV1(fixture.Materials)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := reviewFreezeVerifyAttestationMaterialBundleV1(ctx, fixture.DescriptorRaw, fixture.StatementRaw, loader)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled context error=%v", err)
	}
	if calls := reviewFreezeAttestationMaterialLoaderCallCountV1(loader); calls != 0 {
		t.Fatalf("cancelled context loader calls=%d", calls)
	}
}
