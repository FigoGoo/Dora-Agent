//go:build localsmoke

// Package main 为本地冒烟创建一条真实的 user-message-runtime 之前的 V1 Session cohort。
// 它刻意使用未装配 UserMessageRuntime Profile 的正式 Session Service，因此提交的
// Message/Input/Receipt/Event 不包含 user-message Turn 或 Context。
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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

const (
	localSmokeLegacySeederVersion = "local-smoke-user-message-legacy-seeder"
	legacySmokePrompt             = "local smoke legacy user message"
)

// seedOutput 是成功时 stdout 唯一允许的契约，只包含稳定 ID，不包含 prompt、DSN、密钥、
// 密文、摘要、用户身份或项目身份。
type seedOutput struct {
	SessionID string `json:"session_id"`
	InputID   string `json:"input_id"`
	MessageID string `json:"message_id"`
	CommandID string `json:"command_id"`
}

// main 在失败时只输出固定脱敏错误并以非零状态退出。
func main() {
	if err := run(os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "本地冒烟 legacy user_message cohort 创建失败")
		os.Exit(1)
	}
}

// run 先验证仅本地边界，再装配不含 UserMessageRuntime Profile 的正式 Session 写路径，
// 创建一条使用全新身份的 V1 非空 Prompt cohort。
func run(stdout io.Writer) error {
	databaseURL := strings.TrimSpace(os.Getenv("AGENT_DATABASE_URL"))
	if strings.TrimSpace(os.Getenv("DORA_ENV")) != "local" || !isLocalSmokeAgentDSN(databaseURL) {
		return errors.New("local legacy seeder environment is invalid")
	}
	cfg, err := config.Load(localSmokeLegacySeederVersion)
	if err != nil {
		return errors.New("load local Agent configuration")
	}
	if cfg.Service.Environment != "local" || cfg.PostgreSQL.DSN != databaseURL {
		return errors.New("local Agent configuration changed after validation")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	client, err := postgres.Open(ctx, cfg.PostgreSQL)
	if err != nil {
		return errors.New("open local Agent database")
	}
	defer client.Close()
	if err := client.VerifySchema(ctx, cfg.PostgreSQL.PingTimeout); err != nil {
		return errors.New("verify local Agent schema")
	}
	repository, err := postgres.NewSessionRepository(client)
	if err != nil {
		return errors.New("create local Session repository")
	}
	promptProtector, err := contentcrypto.NewAES256GCMProtectorWithPrevious(
		cfg.ContentProtection.Key,
		cfg.ContentProtection.KeyVersion,
		cfg.ContentProtection.PreviousKey,
		cfg.ContentProtection.PreviousKeyVersion,
	)
	if err != nil {
		return errors.New("create local content protector")
	}
	snapshotProtector, err := contentcrypto.NewSkillSnapshotAES256GCMProtectorWithPrevious(
		cfg.ContentProtection.Key,
		cfg.ContentProtection.KeyVersion,
		cfg.ContentProtection.PreviousKey,
		cfg.ContentProtection.PreviousKeyVersion,
	)
	if err != nil {
		return errors.New("create local Snapshot protector")
	}
	if err := skill.ValidateProducerLimitsV1(cfg.SkillSnapshotLimits, cfg.SkillSnapshotLimits); err != nil {
		return errors.New("validate local Snapshot limits")
	}
	// 该构造器就是生产 Composition Root 的 non-runtime 分支，不会附加 UserMessageRuntimePlan。
	service, err := session.NewServiceWithSkillSnapshot(
		repository,
		idgen.UUIDv7{},
		clock.System{},
		promptProtector,
		snapshotProtector,
		cfg.SkillSnapshotLimits,
	)
	if err != nil {
		return errors.New("create local Session service")
	}

	command, err := newLegacyEnsureCommand(idgen.UUIDv7{}, clock.System{}.Now().UTC())
	if err != nil {
		return errors.New("build local legacy Session command")
	}
	result, err := service.EnsureProjectSession(ctx, command)
	if err != nil {
		return errors.New("create local legacy Session cohort")
	}
	output, err := newSeedOutput(result)
	if err != nil {
		return err
	}
	return json.NewEncoder(stdout).Encode(output)
}

// newLegacyEnsureCommand 每次都分配全新的 command/project/user/request 身份，并计算正式
// Session Service 会再次验证的同一份 V1 canonical 摘要。
func newLegacyEnsureCommand(ids idgen.UUIDv7, requestedAt time.Time) (session.EnsureCommand, error) {
	values := make([]string, 4)
	for index := range values {
		value, err := ids.New()
		if err != nil || !isCanonicalUUIDv7(value) {
			return session.EnsureCommand{}, errors.New("generate local legacy UUIDv7")
		}
		values[index] = value
	}
	requestDigest, promptDigest, promptPresent, err := session.CalculateRequestDigest(
		values[2], values[3], legacySmokePrompt, session.SkillSnapshotKindEmpty,
	)
	if err != nil || !promptPresent {
		return session.EnsureCommand{}, errors.New("calculate local legacy request digest")
	}
	return session.EnsureCommand{
		SchemaVersion:     session.EnsureCommandSchemaVersionV1,
		RequestID:         values[0],
		CommandID:         values[1],
		RequestDigest:     requestDigest,
		ProjectID:         values[2],
		OwnerUserID:       values[3],
		CreationSource:    session.CreationSourceQuickCreate,
		InitialPrompt:     legacySmokePrompt,
		PromptDigest:      promptDigest,
		SkillSnapshotMode: session.SkillSnapshotKindEmpty,
		RequestedAt:       requestedAt,
	}, nil
}

// newSeedOutput 在写 stdout 前拒绝重放、空 Prompt 结果和非规范身份。
func newSeedOutput(result session.EnsureResult) (seedOutput, error) {
	if result.Disposition != session.EnsureDispositionCreated || result.InputID == nil || result.MessageID == nil {
		return seedOutput{}, errors.New("local legacy Session result is incomplete")
	}
	output := seedOutput{
		SessionID: result.SessionID,
		InputID:   *result.InputID,
		MessageID: *result.MessageID,
		CommandID: result.CommandID,
	}
	for _, value := range []string{output.SessionID, output.InputID, output.MessageID, output.CommandID} {
		if !isCanonicalUUIDv7(value) {
			return seedOutput{}, errors.New("local legacy Session result identity is invalid")
		}
	}
	return output, nil
}

// isLocalSmokeAgentDSN 只允许 loopback 上 dora_agent_app 专用角色访问 dora_agent 或
// dora_agent_test 专用库，并要求显式禁用本地 TLS；解析后的 DSN 永不进入日志或输出。
func isLocalSmokeAgentDSN(dsn string) bool {
	parsed, err := url.Parse(dsn)
	if err != nil || parsed.User == nil {
		return false
	}
	query := parsed.Query()
	databaseAllowed := parsed.Path == "/dora_agent" || parsed.Path == "/dora_agent_test"
	if (parsed.Scheme != "postgres" && parsed.Scheme != "postgresql") ||
		parsed.User.Username() != "dora_agent_app" || !databaseAllowed || parsed.Fragment != "" ||
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

func isCanonicalUUIDv7(value string) bool {
	parsed, err := uuid.Parse(value)
	return err == nil && parsed.Version() == 7 && parsed.Variant() == uuid.RFC4122 && parsed.String() == value
}
