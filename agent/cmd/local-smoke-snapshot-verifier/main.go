//go:build localsmoke

// Package main 提供仅供 local 冒烟使用的 Session Skill Snapshot 完整性验证器。
// 它调用正式 Session Service Load 路径完成数据库读取、AEAD 解密、canonical 与摘要复核，
// 只输出跨模块核对所需的摘要和 ID，不输出 Runtime Content、密文、密钥或数据库地址。
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/FigoGoo/Dora-Agent/agent/internal/clock"
	"github.com/FigoGoo/Dora-Agent/agent/internal/config"
	"github.com/FigoGoo/Dora-Agent/agent/internal/contentcrypto"
	"github.com/FigoGoo/Dora-Agent/agent/internal/idgen"
	"github.com/FigoGoo/Dora-Agent/agent/internal/postgres"
	"github.com/FigoGoo/Dora-Agent/agent/internal/session"
	"github.com/FigoGoo/Dora-Agent/agent/internal/skill"
	"github.com/google/uuid"
)

const localSmokeVerifierVersion = "local-smoke-snapshot-verifier"

// localSmokeInput 是建立数据库连接前已通过本地环境、专用角色/库和 UUIDv7 校验的输入。
type localSmokeInput struct {
	// databaseURL 是只允许 loopback dora_agent_app@dora_agent 的 Agent DSN。
	databaseURL string
	// sessionID 是待验证冻结快照所属的规范 UUIDv7。
	sessionID string
}

// verificationSkillOutput 是单个冻结 Skill 的脱敏核对投影，只包含顺序、ID 与摘要。
type verificationSkillOutput struct {
	// LoadOrder 是从 1 开始的冻结加载顺序。
	LoadOrder int32 `json:"load_order"`
	// SkillID 是 Business Skill UUIDv7 逻辑引用。
	SkillID string `json:"skill_id"`
	// PublishedSnapshotID 是冻结 Published Snapshot UUIDv7 逻辑引用。
	PublishedSnapshotID string `json:"published_snapshot_id"`
	// RuntimeContentDigest 是已解密并重算通过的 Runtime Content 摘要。
	RuntimeContentDigest string `json:"runtime_content_digest"`
	// ContentDigest 是完整 Published Definition 的不透明审计摘要。
	ContentDigest string `json:"content_digest"`
}

// verificationOutput 是本地 Smoke 唯一允许写入 stdout 的脱敏 JSON 契约。
type verificationOutput struct {
	// Status 固定 verified，表示正式 Load 路径已完成全部完整性校验。
	Status string `json:"status"`
	// SessionID 是已验证的 Agent Session UUIDv7。
	SessionID string `json:"session_id"`
	// SnapshotDigest 是正式 Service 重算通过的 Snapshot set digest。
	SnapshotDigest string `json:"snapshot_digest"`
	// SkillCount 是冻结 Skill 数量。
	SkillCount int32 `json:"skill_count"`
	// Skills 是按 load_order 排列的脱敏 ID/摘要投影。
	Skills []verificationSkillOutput `json:"skills"`
}

// main 执行一次有界本地校验；失败只输出稳定中文消息，禁止把 DSN、密钥或解密错误扩散到终端。
func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "本地冒烟 Session Skill Snapshot 验证失败")
		os.Exit(1)
	}
}

// run 在任何数据库访问前完成环境和 DSN 边界校验，再按正式 Runtime 装配 Repository、保护器与 Session Service。
func run() error {
	input, err := loadLocalSmokeInput()
	if err != nil {
		return err
	}
	cfg, err := config.Load(localSmokeVerifierVersion)
	if err != nil {
		return errors.New("load local Agent configuration")
	}
	// 输入校验和正式 Config 必须观察同一 DSN，禁止配置加载阶段用另一地址替换已审核边界。
	if cfg.Service.Environment != "local" || cfg.PostgreSQL.DSN != input.databaseURL {
		return errors.New("local Agent configuration changed after validation")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	client, err := postgres.Open(ctx, config.PostgreSQLConfig{
		DSN: input.databaseURL, MaxOpenConns: 2, MaxIdleConns: 1,
		ConnMaxLifetime: 5 * time.Minute, ConnMaxIdleTime: time.Minute, PingTimeout: 3 * time.Second,
	})
	if err != nil {
		return errors.New("open local Agent database")
	}
	defer client.Close()
	if err := client.VerifySchema(ctx, 5*time.Second); err != nil {
		return errors.New("verify local Agent schema")
	}
	repository, err := postgres.NewSessionRepository(client)
	if err != nil {
		return errors.New("create local Session repository")
	}
	promptProtector, err := contentcrypto.NewAES256GCMProtectorWithPrevious(
		cfg.ContentProtection.Key, cfg.ContentProtection.KeyVersion,
		cfg.ContentProtection.PreviousKey, cfg.ContentProtection.PreviousKeyVersion,
	)
	if err != nil {
		return errors.New("create local content protector")
	}
	snapshotProtector, err := contentcrypto.NewSkillSnapshotAES256GCMProtectorWithPrevious(
		cfg.ContentProtection.Key, cfg.ContentProtection.KeyVersion,
		cfg.ContentProtection.PreviousKey, cfg.ContentProtection.PreviousKeyVersion,
	)
	if err != nil {
		return errors.New("create local Snapshot protector")
	}
	if err := skill.ValidateProducerLimitsV1(cfg.SkillSnapshotLimits, cfg.SkillSnapshotLimits); err != nil {
		return errors.New("validate local Snapshot limits")
	}
	service, err := session.NewServiceWithSkillSnapshot(
		repository, idgen.UUIDv7{}, clock.System{}, promptProtector, snapshotProtector, cfg.SkillSnapshotLimits,
	)
	if err != nil {
		return errors.New("create local Session service")
	}
	loaded, err := service.LoadSessionSkillSnapshotV1(ctx, input.sessionID)
	if err != nil {
		return errors.New("load local Session Skill Snapshot")
	}
	return json.NewEncoder(os.Stdout).Encode(newVerificationOutput(loaded))
}

// loadLocalSmokeInput 在连接数据库前拒绝非 local 环境、非 loopback/专用角色库 DSN 和非规范 UUIDv7。
func loadLocalSmokeInput() (localSmokeInput, error) {
	if strings.TrimSpace(os.Getenv("DORA_ENV")) != "local" {
		return localSmokeInput{}, errors.New("local environment is required")
	}
	databaseURL := strings.TrimSpace(os.Getenv("AGENT_DATABASE_URL"))
	sessionID := strings.TrimSpace(os.Getenv("DORA_SMOKE_AGENT_SESSION_ID"))
	if !isLocalSmokeAgentDSN(databaseURL) || !isCanonicalUUIDv7(sessionID) {
		return localSmokeInput{}, errors.New("local Snapshot verifier input is invalid")
	}
	return localSmokeInput{databaseURL: databaseURL, sessionID: sessionID}, nil
}

// isLocalSmokeAgentDSN 只接受 loopback 上的 dora_agent_app 专用角色和 dora_agent 专用库，且本地连接必须显式禁用 TLS。
func isLocalSmokeAgentDSN(dsn string) bool {
	parsed, err := url.Parse(dsn)
	if err != nil {
		return false
	}
	query := parsed.Query()
	if (parsed.Scheme != "postgres" && parsed.Scheme != "postgresql") || parsed.User == nil ||
		parsed.User.Username() != "dora_agent_app" || parsed.Path != "/dora_agent" || parsed.Fragment != "" ||
		len(query) != 1 || len(query["sslmode"]) != 1 || query.Get("sslmode") != "disable" {
		return false
	}
	if password, exists := parsed.User.Password(); !exists || password == "" {
		return false
	}
	host := parsed.Hostname()
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// isCanonicalUUIDv7 验证 Session ID 使用唯一小写 RFC 9562 UUIDv7 文本表示，禁止其他版本或别名进入查询。
func isCanonicalUUIDv7(value string) bool {
	parsed, err := uuid.Parse(value)
	return err == nil && parsed.Version() == 7 && parsed.Variant() == uuid.RFC4122 && parsed.String() == value
}

// newVerificationOutput 把已完成真实解密和 canonical 校验的 Snapshot 投影为不含 Runtime Content 的固定 JSON 结构。
func newVerificationOutput(loaded session.LoadedSkillSnapshotV1) verificationOutput {
	items := make([]verificationSkillOutput, len(loaded.Snapshot.Skills))
	for index, item := range loaded.Snapshot.Skills {
		items[index] = verificationSkillOutput{
			LoadOrder: item.LoadOrder, SkillID: item.SkillID, PublishedSnapshotID: item.PublishedSnapshotID,
			RuntimeContentDigest: item.RuntimeContentDigest, ContentDigest: item.ContentDigest,
		}
	}
	return verificationOutput{
		Status: "verified", SessionID: loaded.SessionID, SnapshotDigest: loaded.Snapshot.SnapshotSetDigest,
		SkillCount: loaded.Snapshot.SkillCount, Skills: items,
	}
}
