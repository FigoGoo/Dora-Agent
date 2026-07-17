//go:build localsmoke

// Package main 为 Analyze Materials canonical smoke 提供只读 PostgreSQL 权威断言与 Redis 直连探针。
package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"
	redisclient "github.com/redis/go-redis/v9"
)

const authoritySchemaVersion = "analyze_materials_runtime.authority.v1"

type probeOutput struct {
	SchemaVersion    string `json:"schema_version"`
	Mode             string `json:"mode"`
	PostgreSQLDirect bool   `json:"postgresql_direct"`
	RedisDirect      bool   `json:"redis_direct"`
}

type authorityCounts struct {
	InputCount               int64 `json:"input_count"`
	MessageCount             int64 `json:"message_count"`
	RunCount                 int64 `json:"run_count"`
	ContextCount             int64 `json:"context_count"`
	ContextPinsValid         bool  `json:"context_pins_valid"`
	ContextDigestsValid      bool  `json:"context_digests_valid"`
	ModelReceiptCount        int64 `json:"model_receipt_count"`
	RouterModelReceiptCount  int64 `json:"router_model_receipt_count"`
	GraphModelReceiptCount   int64 `json:"graph_model_receipt_count"`
	ToolReceiptCount         int64 `json:"tool_receipt_count"`
	ProjectionCount          int64 `json:"projection_count"`
	AcceptedEventCount       int64 `json:"accepted_event_count"`
	TerminalEventCount       int64 `json:"terminal_event_count"`
	EventHighWatermark       int64 `json:"event_high_watermark"`
	CreationSpecPreviewCount int64 `json:"creation_spec_preview_count"`
	UserMessageTurnCount     int64 `json:"user_message_turn_count"`
	UnsafeEventPayloadCount  int64 `json:"unsafe_event_payload_count"`
}

type authorityOutput struct {
	SchemaVersion string          `json:"schema_version"`
	Mode          string          `json:"mode"`
	Counts        authorityCounts `json:"counts"`
}

type authorityIdentity struct {
	SessionID  string
	InputID    string
	TurnID     string
	RunID      string
	ToolCallID string
}

func main() {
	if err := run(os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "本地 Analyze Materials 权威探针失败")
		os.Exit(1)
	}
}

func run(stdout io.Writer) error {
	if strings.TrimSpace(os.Getenv("DORA_ENV")) != "local" {
		return errors.New("local environment is required")
	}
	dsn := strings.TrimSpace(os.Getenv("AGENT_DATABASE_URL"))
	if !isLocalAnalyzeMaterialsAgentDSN(dsn) {
		return errors.New("unsafe Agent test DSN")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	database, err := sql.Open("pgx", dsn)
	if err != nil {
		return errors.New("open Agent test database")
	}
	defer database.Close()
	if err := database.PingContext(ctx); err != nil {
		return errors.New("ping Agent test database")
	}

	switch strings.TrimSpace(os.Getenv("DORA_SMOKE_ANALYZE_MATERIALS_MODE")) {
	case "probe":
		if err := pingLocalRedis(ctx); err != nil {
			return err
		}
		return encodeExactJSON(stdout, probeOutput{
			SchemaVersion: authoritySchemaVersion, Mode: "probe", PostgreSQLDirect: true, RedisDirect: true,
		})
	case "authority":
		identity, err := authorityIdentityFromEnvironment()
		if err != nil {
			return err
		}
		counts, err := queryAuthority(ctx, database, identity)
		if err != nil {
			return err
		}
		return encodeExactJSON(stdout, authorityOutput{SchemaVersion: authoritySchemaVersion, Mode: "authority", Counts: counts})
	default:
		return errors.New("unsupported authority mode")
	}
}

func pingLocalRedis(ctx context.Context) error {
	address := strings.TrimSpace(os.Getenv("AGENT_REDIS_ADDR"))
	if address != "127.0.0.1:16379" {
		return errors.New("unsafe Redis address")
	}
	databaseIndex, err := strconv.Atoi(strings.TrimSpace(os.Getenv("AGENT_REDIS_DB")))
	if err != nil || databaseIndex < 0 {
		return errors.New("invalid Redis database")
	}
	client := redisclient.NewClient(&redisclient.Options{
		Addr: address, Password: os.Getenv("AGENT_REDIS_PASSWORD"), DB: databaseIndex,
	})
	defer client.Close()
	pingCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	if err := client.Ping(pingCtx).Err(); err != nil {
		return errors.New("ping local Redis")
	}
	return nil
}

func authorityIdentityFromEnvironment() (authorityIdentity, error) {
	identity := authorityIdentity{
		SessionID:  strings.TrimSpace(os.Getenv("DORA_SMOKE_SESSION_ID")),
		InputID:    strings.TrimSpace(os.Getenv("DORA_SMOKE_INPUT_ID")),
		TurnID:     strings.TrimSpace(os.Getenv("DORA_SMOKE_TURN_ID")),
		RunID:      strings.TrimSpace(os.Getenv("DORA_SMOKE_RUN_ID")),
		ToolCallID: strings.TrimSpace(os.Getenv("DORA_SMOKE_TOOL_CALL_ID")),
	}
	for _, value := range []string{identity.SessionID, identity.InputID, identity.TurnID, identity.RunID, identity.ToolCallID} {
		parsed, err := uuid.Parse(value)
		if err != nil || parsed.Version() != 7 || parsed.Variant() != uuid.RFC4122 || parsed.String() != value {
			return authorityIdentity{}, errors.New("invalid authority identity")
		}
	}
	return identity, nil
}

func queryAuthority(ctx context.Context, database *sql.DB, identity authorityIdentity) (authorityCounts, error) {
	row := database.QueryRowContext(ctx, `
SELECT
  (SELECT count(*) FROM agent.session_input WHERE id=$1::uuid AND session_id=$2::uuid AND source_type='analyze_materials_preview' AND message_id IS NULL AND status='resolved' AND lease_owner IS NULL AND lease_until IS NULL),
  (SELECT count(*) FROM agent.session_message WHERE session_id=$2::uuid),
  (SELECT count(*) FROM agent.analyze_materials_preview_run WHERE input_id=$1::uuid AND turn_id=$3::uuid AND run_id=$4::uuid AND tool_call_id=$5::uuid AND status='completed' AND owner_fence > 0),
  (SELECT count(*) FROM agent.analyze_materials_preview_turn_context WHERE input_id=$1::uuid AND turn_id=$3::uuid AND run_id=$4::uuid AND tool_call_id=$5::uuid AND profile='analyze_materials.runtime.v2preview1' AND schema_version='analyze_materials.turn_context.v2preview1'),
  COALESCE((SELECT tool_registry_ref='analyze_materials.preview_tools@v1' AND tool_definition_ref='analyze_materials.v2preview1' AND intent_schema_ref='analyze_materials.preview.intent.v1' AND result_schema_ref='analyze_materials.preview.result.v1' AND prompt_ref='graph_tool.analyze_materials.preview.v1' AND validator_ref='analyze_materials.preview.validator.v1' AND evidence_policy_ref='analyze_materials.preview.evidence-policy.v1' AND router_model_route_ref='local.fake.analyze_materials.router@v1' AND analysis_model_route_ref='local.fake.analyze_materials.analysis@v1' AND runtime_policy_ref='analyze_materials.runtime_policy@v1' AND budget_ref='analyze_materials.local_preview_budget@v1' FROM agent.analyze_materials_preview_turn_context WHERE input_id=$1::uuid), false),
  COALESCE((SELECT intent_digest ~ '^[0-9a-f]{64}$' AND context_digest ~ '^[0-9a-f]{64}$' AND octet_length(intent_ciphertext) > 0 FROM agent.analyze_materials_preview_turn_context WHERE input_id=$1::uuid), false),
  (SELECT count(*) FROM agent.analyze_materials_preview_model_receipt WHERE input_id=$1::uuid AND run_id=$4::uuid AND status='completed' AND execution_fence > 0 AND response_digest ~ '^[0-9a-f]{64}$' AND octet_length(response_ciphertext) > 0),
  (SELECT count(*) FROM agent.analyze_materials_preview_model_receipt WHERE input_id=$1::uuid AND run_id=$4::uuid AND call_kind='router' AND status='completed'),
  (SELECT count(*) FROM agent.analyze_materials_preview_model_receipt WHERE input_id=$1::uuid AND run_id=$4::uuid AND call_kind='graph_analysis' AND status='completed'),
  (SELECT count(*) FROM agent.analyze_materials_preview_tool_receipt WHERE input_id=$1::uuid AND run_id=$4::uuid AND tool_call_id=$5::uuid AND status='completed' AND execution_fence > 0 AND result_digest ~ '^[0-9a-f]{64}$' AND octet_length(result_ciphertext) > 0),
  (SELECT count(*) FROM agent.analyze_materials_preview_projection WHERE source_input_id=$1::uuid AND session_id=$2::uuid AND turn_id=$3::uuid AND run_id=$4::uuid AND tool_call_id=$5::uuid AND schema_version='analyze_materials.preview.card.v1' AND outcome_kind='tool_completed' AND status='completed' AND result_digest ~ '^[0-9a-f]{64}$' AND projection_version=1),
  (SELECT count(*) FROM agent.session_event_log WHERE session_id=$2::uuid AND event_type='analyze_materials.preview.accepted' AND source_kind='analyze_materials_preview' AND aggregate_type='session_input' AND aggregate_id=$1::uuid AND aggregate_version=1 AND NOT (payload ? 'message_id') AND payload->>'input_id'=$1::text AND payload->>'source_type'='analyze_materials_preview'),
  (SELECT count(*) FROM agent.session_event_log WHERE session_id=$2::uuid AND event_type='analyze_materials.preview.completed' AND source_kind='analyze_materials_preview' AND aggregate_type='session_turn' AND aggregate_id=$3::uuid AND aggregate_version=1 AND payload->>'schema_version'='analyze_materials.preview.card.v1' AND payload->>'status'='completed'),
  COALESCE((SELECT last_seq FROM agent.session_event_counter WHERE session_id=$2::uuid), 0),
  (SELECT count(*) FROM agent.creation_spec_preview_run WHERE session_id=$2::uuid),
  (SELECT count(*) FROM agent.session_user_message_turn WHERE session_id=$2::uuid),
  (SELECT count(*) FROM agent.session_event_log WHERE session_id=$2::uuid AND source_kind='analyze_materials_preview' AND (payload ?| ARRAY['intent_ciphertext','content','provider_payload','reasoning','secret']))`,
		identity.InputID, identity.SessionID, identity.TurnID, identity.RunID, identity.ToolCallID)
	var counts authorityCounts
	if err := row.Scan(
		&counts.InputCount, &counts.MessageCount, &counts.RunCount, &counts.ContextCount,
		&counts.ContextPinsValid, &counts.ContextDigestsValid, &counts.ModelReceiptCount,
		&counts.RouterModelReceiptCount, &counts.GraphModelReceiptCount, &counts.ToolReceiptCount,
		&counts.ProjectionCount, &counts.AcceptedEventCount, &counts.TerminalEventCount,
		&counts.EventHighWatermark, &counts.CreationSpecPreviewCount, &counts.UserMessageTurnCount,
		&counts.UnsafeEventPayloadCount,
	); err != nil {
		return authorityCounts{}, fmt.Errorf("query Analyze Materials authority: %w", err)
	}
	return counts, nil
}

func encodeExactJSON(output io.Writer, value any) error {
	if err := json.NewEncoder(output).Encode(value); err != nil {
		return errors.New("encode safe authority output")
	}
	return nil
}

func isLocalAnalyzeMaterialsAgentDSN(dsn string) bool {
	parsed, err := url.Parse(dsn)
	if err != nil || parsed.User == nil || parsed.Path != "/dora_agent_test" || parsed.Port() != "15432" || parsed.Fragment != "" {
		return false
	}
	if parsed.Scheme != "postgres" && parsed.Scheme != "postgresql" {
		return false
	}
	if parsed.User.Username() != "dora_agent_app" {
		return false
	}
	if password, exists := parsed.User.Password(); !exists || password == "" {
		return false
	}
	query := parsed.Query()
	if len(query) != 1 || len(query["sslmode"]) != 1 || query.Get("sslmode") != "disable" {
		return false
	}
	ip := net.ParseIP(parsed.Hostname())
	return ip != nil && ip.IsLoopback() && parsed.Hostname() == "127.0.0.1"
}
