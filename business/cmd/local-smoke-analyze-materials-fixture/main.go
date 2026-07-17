//go:build localsmoke

// Package main 提供只允许 local 环境编译执行的 Analyze Materials 权威输入夹具命令。
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/FigoGoo/Dora-Agent/business/internal/assetanalysis"
	"github.com/FigoGoo/Dora-Agent/business/internal/localseed"
	"github.com/google/uuid"
)

const (
	fixtureModeSeed      = "seed"
	fixtureModeAuthority = "authority"
	fixtureAssetVersion  = int64(1)
	fixtureSchemaVersion = "analyze_materials.local_smoke.fixture.v1"

	// fixtureEvidenceContent 是仅写入 Business Evidence 权威表的固定预览正文，任何成功或失败输出都不得回显它。
	fixtureEvidenceContent = "蓝色旅行水杯置于浅色背景中央，主体清晰，无人物。"
)

var (
	// errInvalidFixtureInput 表示环境参数、UUIDv7 或闭集模式不符合本地夹具契约。
	errInvalidFixtureInput = errors.New("invalid analyze materials local smoke fixture input")
	// errFixtureConflict 表示目标项目、素材、Evidence 或负向权威计数不符合固定夹具不变量。
	errFixtureConflict = errors.New("analyze materials local smoke fixture conflict")
)

// fixtureConfig 保存命令从环境读取并完成规范化后的非 Secret 输入。
type fixtureConfig struct {
	// Mode 是 seed 或 authority 闭集值。
	Mode string
	// ProjectID 是待准备或核对的 Business Project UUIDv7。
	ProjectID string
	// OwnerUserID 是 Project 的可信所有者 UUIDv7。
	OwnerUserID string
	// AssetID 是该 Project 下固定的图片素材 UUIDv7。
	AssetID string
	// EvidenceID 是素材下固定的不可变 Evidence UUIDv7。
	EvidenceID string
}

// fixtureSeed 是 Repository 幂等写入素材与 Evidence 所需的完整固定事实。
type fixtureSeed struct {
	// ProjectID 是已存在且 active 的 Business Project UUIDv7。
	ProjectID string
	// OwnerUserID 是 Project 的权威所有者 UUIDv7。
	OwnerUserID string
	// AssetID 是待写入的图片素材 UUIDv7。
	AssetID string
	// EvidenceID 是待写入的视觉描述 Evidence UUIDv7。
	EvidenceID string
	// AssetVersion 是固定为 1 的不可变素材版本。
	AssetVersion int64
	// Content 是只进入权威 Evidence 表的固定视觉描述正文。
	Content string
	// ContentDigest 是 Content UTF-8 字节的小写 SHA-256。
	ContentDigest string
}

// fixtureCounts 是本地 smoke 比较 Business 权威状态的固定计数集合。
type fixtureCounts struct {
	// Assets 是指定 Project 与 Owner 下就绪素材总数，固定为 1。
	Assets int64 `json:"asset_count"`
	// Evidence 是上述素材集合下的 Evidence 总数，固定为 1。
	Evidence int64 `json:"evidence_count"`
	// CreationSpecs 是指定 Project 与 Owner 的 CreationSpec 数量，必须为 0。
	CreationSpecs int64 `json:"creation_spec_count"`
	// CreationSpecCommandReceipts 是指定 Project 与 Owner 的 CreationSpec 命令回执数量，必须为 0。
	CreationSpecCommandReceipts int64 `json:"creation_spec_receipt_count"`
}

// fixtureDigests 是 stdout 允许暴露的固定摘要集合，不包含正文、DSN 或 Secret。
type fixtureDigests struct {
	// ContentSHA256 是 Evidence 正文的 SHA-256，只用于跨阶段校验正文未漂移。
	ContentSHA256 string `json:"content_sha256"`
	// AuthoritySHA256 冻结标识、版本、计数和内容摘要，用于比较 seed/authority 结果。
	AuthoritySHA256 string `json:"authority_sha256"`
}

// fixtureOutput 是 stdout 唯一允许的 exact-set 安全 JSON 契约。
type fixtureOutput struct {
	// SchemaVersion 是本地夹具安全输出的冻结契约版本。
	SchemaVersion string `json:"schema_version"`
	// Mode 回显已校验的 seed 或 authority 闭集模式，便于脚本拒绝串用阶段结果。
	Mode string `json:"mode"`
	// AssetID 是已准备或核对的图片素材 UUIDv7。
	AssetID string `json:"asset_id"`
	// EvidenceID 是已准备或核对的视觉描述 Evidence UUIDv7。
	EvidenceID string `json:"evidence_id"`
	// AssetVersion 是固定为 1 的素材版本。
	AssetVersion int64 `json:"asset_version"`
	// Counts 是素材、Evidence 与禁止副作用表的权威计数。
	Counts fixtureCounts `json:"counts"`
	// Digests 是正文与完整权威结果的安全 SHA-256。
	Digests fixtureDigests `json:"digests"`
}

// fixtureAuthority 是 Repository 集合读取返回的安全权威投影，不承载 Evidence 正文。
type fixtureAuthority struct {
	// AssetID 是查询命中的固定素材 UUIDv7。
	AssetID string
	// EvidenceID 是查询命中的固定 Evidence UUIDv7。
	EvidenceID string
	// AssetVersion 是权威素材版本。
	AssetVersion int64
	// ContentDigest 是权威 Evidence 正文的小写 SHA-256。
	ContentDigest string
	// Counts 是指定 Project 与 Owner 下的固定集合计数。
	Counts fixtureCounts
	// ProjectValid 表示 Project 存在、所有者匹配且状态为 active。
	ProjectValid bool
	// AssetValid 表示固定素材的全部不可变字段与夹具一致。
	AssetValid bool
	// EvidenceValid 表示固定 Evidence 的全部不可变字段、正文与摘要与夹具一致。
	EvidenceValid bool
}

// fixtureRepository 定义本地夹具写入与只读权威核验所需的最小 Repository 边界。
type fixtureRepository interface {
	// Ensure 在单事务内执行冲突安全的幂等写入，并返回提交前核对过的权威投影。
	Ensure(ctx context.Context, seed fixtureSeed) (fixtureAuthority, error)
	// Authority 只读指定 Project/Owner/Asset/Evidence 的权威计数和安全摘要。
	Authority(ctx context.Context, seed fixtureSeed) (fixtureAuthority, error)
}

// main 执行本地 Analyze Materials 夹具，并用固定错误阻止 DSN、SQL 或 Evidence 正文进入 stderr。
func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "本地 Analyze Materials 权威夹具执行失败")
		os.Exit(1)
	}
}

// run 校验 local 与安全 DSN，打开有界 GORM Repository 后执行 seed 或 authority 模式。
func run() error {
	if strings.TrimSpace(os.Getenv("DORA_ENV")) != "local" {
		return errInvalidFixtureInput
	}
	dsn := strings.TrimSpace(os.Getenv("BUSINESS_DATABASE_URL"))
	if !localseed.IsSafeLocalBusinessDSN(dsn) {
		return errInvalidFixtureInput
	}
	config, err := fixtureConfigFromEnvironment(os.Getenv)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	repository, err := openPostgresFixtureRepository(ctx, dsn)
	if err != nil {
		return err
	}
	defer repository.Close()
	return execute(ctx, config, repository, os.Stdout)
}

// fixtureConfigFromEnvironment 解析闭集模式和规范 UUIDv7；省略素材标识时按 Project/Owner 稳定派生以支持重放。
func fixtureConfigFromEnvironment(getenv func(string) string) (fixtureConfig, error) {
	if getenv == nil {
		return fixtureConfig{}, errInvalidFixtureInput
	}
	mode := strings.TrimSpace(getenv("DORA_SMOKE_ANALYZE_MATERIALS_MODE"))
	projectID := strings.TrimSpace(getenv("DORA_SMOKE_PROJECT_ID"))
	ownerUserID := strings.TrimSpace(getenv("DORA_SMOKE_OWNER_USER_ID"))
	if (mode != fixtureModeSeed && mode != fixtureModeAuthority) ||
		!assetanalysis.CanonicalUUIDv7(projectID) || !assetanalysis.CanonicalUUIDv7(ownerUserID) {
		return fixtureConfig{}, errInvalidFixtureInput
	}
	assetID := strings.TrimSpace(getenv("DORA_SMOKE_ANALYZE_MATERIALS_ASSET_ID"))
	if assetID == "" {
		assetID = deterministicFixtureUUIDv7("analyze-materials-asset", projectID, ownerUserID)
	}
	evidenceID := strings.TrimSpace(getenv("DORA_SMOKE_ANALYZE_MATERIALS_EVIDENCE_ID"))
	if evidenceID == "" {
		evidenceID = deterministicFixtureUUIDv7("analyze-materials-evidence", projectID, ownerUserID)
	}
	if !assetanalysis.CanonicalUUIDv7(assetID) || !assetanalysis.CanonicalUUIDv7(evidenceID) || assetID == evidenceID {
		return fixtureConfig{}, errInvalidFixtureInput
	}
	return fixtureConfig{
		Mode: mode, ProjectID: projectID, OwnerUserID: ownerUserID, AssetID: assetID, EvidenceID: evidenceID,
	}, nil
}

// execute 通过注入的 Repository 执行模式分支，验证全部正负不变量后只编码 exact-set 安全 JSON。
func execute(ctx context.Context, config fixtureConfig, repository fixtureRepository, output io.Writer) error {
	if ctx == nil || repository == nil || output == nil {
		return errInvalidFixtureInput
	}
	contentDigest := sha256.Sum256([]byte(fixtureEvidenceContent))
	seed := fixtureSeed{
		ProjectID: config.ProjectID, OwnerUserID: config.OwnerUserID,
		AssetID: config.AssetID, EvidenceID: config.EvidenceID, AssetVersion: fixtureAssetVersion,
		Content: fixtureEvidenceContent, ContentDigest: hex.EncodeToString(contentDigest[:]),
	}
	var (
		authority fixtureAuthority
		err       error
	)
	switch config.Mode {
	case fixtureModeSeed:
		authority, err = repository.Ensure(ctx, seed)
	case fixtureModeAuthority:
		authority, err = repository.Authority(ctx, seed)
	default:
		return errInvalidFixtureInput
	}
	if err != nil {
		return err
	}
	if err := validateFixtureAuthority(seed, authority); err != nil {
		return err
	}
	authorityDigest := fixtureAuthorityDigest(authority)
	return json.NewEncoder(output).Encode(fixtureOutput{
		SchemaVersion: fixtureSchemaVersion, Mode: config.Mode,
		AssetID: authority.AssetID, EvidenceID: authority.EvidenceID, AssetVersion: authority.AssetVersion,
		Counts:  authority.Counts,
		Digests: fixtureDigests{ContentSHA256: authority.ContentDigest, AuthoritySHA256: authorityDigest},
	})
}

// validateFixtureAuthority 强制一素材、一 Evidence 与两个 CreationSpec 表零写入，防止 smoke 把旧副作用误判为成功。
func validateFixtureAuthority(seed fixtureSeed, authority fixtureAuthority) error {
	if !authority.ProjectValid || !authority.AssetValid || !authority.EvidenceValid ||
		authority.AssetID != seed.AssetID || authority.EvidenceID != seed.EvidenceID ||
		authority.AssetVersion != fixtureAssetVersion || authority.ContentDigest != seed.ContentDigest ||
		authority.Counts.Assets != 1 || authority.Counts.Evidence != 1 ||
		authority.Counts.CreationSpecs != 0 || authority.Counts.CreationSpecCommandReceipts != 0 {
		return errFixtureConflict
	}
	return nil
}

// deterministicFixtureUUIDv7 从稳定域、Project 与 Owner 派生规范 UUIDv7，使未显式传 ID 的 seed 可安全重放。
func deterministicFixtureUUIDv7(domain, projectID, ownerUserID string) string {
	digest := sha256.Sum256([]byte(domain + "\x00" + projectID + "\x00" + ownerUserID))
	var value uuid.UUID
	copy(value[:], digest[:16])
	// 复用 Project UUIDv7 的毫秒时间戳，避免稳定夹具 ID 伪造无意义的未来或历史时间排序。
	if projectUUID, err := uuid.Parse(projectID); err == nil {
		copy(value[:6], projectUUID[:6])
	}
	value[6] = (value[6] & 0x0f) | 0x70
	value[8] = (value[8] & 0x3f) | 0x80
	return value.String()
}

// fixtureAuthorityDigest 按冻结字段顺序计算安全结果摘要，Project、Owner 和正文不会进入 stdout。
func fixtureAuthorityDigest(authority fixtureAuthority) string {
	hash := sha256.New()
	for _, field := range []string{
		"analyze-materials-authority-v1", authority.AssetID, authority.EvidenceID,
		fmt.Sprintf("%d", authority.AssetVersion), authority.ContentDigest,
		fmt.Sprintf("%d", authority.Counts.Assets), fmt.Sprintf("%d", authority.Counts.Evidence),
		fmt.Sprintf("%d", authority.Counts.CreationSpecs),
		fmt.Sprintf("%d", authority.Counts.CreationSpecCommandReceipts),
	} {
		_, _ = hash.Write([]byte(field))
		_, _ = hash.Write([]byte{0})
	}
	return hex.EncodeToString(hash.Sum(nil))
}
