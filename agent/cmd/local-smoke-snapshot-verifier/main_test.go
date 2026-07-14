//go:build localsmoke

package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/FigoGoo/Dora-Agent/agent/internal/session"
	"github.com/FigoGoo/Dora-Agent/agent/internal/skill"
)

// TestRunRejectsNonLocalEnvironmentBeforeDatabaseAccess 验证非 local 环境在配置解析和数据库连接前即失败关闭。
func TestRunRejectsNonLocalEnvironmentBeforeDatabaseAccess(t *testing.T) {
	t.Setenv("DORA_ENV", "production")
	t.Setenv("AGENT_DATABASE_URL", "postgres://must-not-be-used")
	t.Setenv("DORA_SMOKE_AGENT_SESSION_ID", "019f0000-0000-7000-8000-000000000001")
	if err := run(); err == nil {
		t.Fatal("local Snapshot verifier accepted a non-local environment")
	}
}

// TestLoadLocalSmokeInputRejectsUnsafeInputBeforeDatabaseAccess 验证缺失或不安全 DSN/Session ID 不会进入正式配置与连接阶段。
func TestLoadLocalSmokeInputRejectsUnsafeInputBeforeDatabaseAccess(t *testing.T) {
	t.Setenv("DORA_ENV", "local")
	t.Setenv("AGENT_DATABASE_URL", "postgres://dora_agent_app:secret@example.com:5432/dora_agent?sslmode=disable")
	t.Setenv("DORA_SMOKE_AGENT_SESSION_ID", "not-a-session-id")
	if _, err := loadLocalSmokeInput(); err == nil {
		t.Fatal("local Snapshot verifier accepted unsafe input")
	}
}

// TestLocalSmokeAgentDSNRequiresLoopbackDedicatedRoleAndDatabase 锁定 loopback、专用 Agent 角色/库和本地 sslmode 边界。
func TestLocalSmokeAgentDSNRequiresLoopbackDedicatedRoleAndDatabase(t *testing.T) {
	for _, valid := range []string{
		"postgres://dora_agent_app:secret@127.0.0.1:15432/dora_agent?sslmode=disable",
		"postgresql://dora_agent_app:secret@localhost:15432/dora_agent?sslmode=disable",
		"postgres://dora_agent_app:secret@[::1]:15432/dora_agent?sslmode=disable",
	} {
		if !isLocalSmokeAgentDSN(valid) {
			t.Fatalf("合法 local Agent DSN 被拒绝: %s", valid)
		}
	}
	for _, invalid := range []string{
		"postgres://dora_agent_app:secret@example.com:5432/dora_agent?sslmode=disable",
		"postgres://dora_agent_app:secret@localhost.evil:5432/dora_agent?sslmode=disable",
		"postgres://dora_admin:secret@127.0.0.1:15432/dora_agent?sslmode=disable",
		"postgres://dora_agent_app:secret@127.0.0.1:15432/dora_agent_test?sslmode=disable",
		"postgres://dora_agent_app:secret@127.0.0.1:15432/dora_agent?sslmode=require",
		"postgres://dora_agent_app:secret@127.0.0.1:15432/dora_agent?sslmode=disable&host=example.com",
		"postgres://dora_agent_app:secret@127.0.0.1:15432/dora_agent?sslmode=disable&sslmode=require",
		"postgres://dora_agent_app@127.0.0.1:15432/dora_agent?sslmode=disable",
		"mysql://dora_agent_app:secret@127.0.0.1:15432/dora_agent?sslmode=disable",
		"://invalid",
	} {
		if isLocalSmokeAgentDSN(invalid) {
			t.Fatalf("不安全 local Agent DSN 被接受: %s", invalid)
		}
	}
}

// TestCanonicalUUIDv7Boundary 验证查询只接受唯一小写 UUIDv7 表示。
func TestCanonicalUUIDv7Boundary(t *testing.T) {
	if !isCanonicalUUIDv7("019f0000-0000-7000-8000-000000000001") {
		t.Fatal("规范 UUIDv7 被拒绝")
	}
	for _, invalid := range []string{
		"019F0000-0000-7000-8000-000000000001",
		"019f0000-0000-4000-8000-000000000001",
		"not-a-uuid",
		"",
	} {
		if isCanonicalUUIDv7(invalid) {
			t.Fatalf("非规范 UUIDv7 被接受: %q", invalid)
		}
	}
}

// TestVerificationOutputContainsOnlyApprovedDigestsAndIDs 验证 stdout 投影不会携带已解密 Runtime Content、密文或其他元数据。
func TestVerificationOutputContainsOnlyApprovedDigestsAndIDs(t *testing.T) {
	loaded := session.LoadedSkillSnapshotV1{
		SessionID: "019f0000-0000-7000-8000-000000000001",
		Snapshot: skill.SessionSkillSnapshotV1{
			SnapshotSetDigest: strings.Repeat("a", 64), SkillCount: 1,
			Skills: []skill.PublishedSkillSnapshotRefV1{{
				LoadOrder:            1,
				SkillID:              "019f0000-0000-7000-8000-000000000002",
				PublishedSnapshotID:  "019f0000-0000-7000-8000-000000000003",
				RuntimeContentDigest: strings.Repeat("b", 64), ContentDigest: strings.Repeat("c", 64),
				RuntimeContent: skill.SkillRuntimeContentV1{
					Name: "must-not-leak-runtime-name", InvocationRules: "must-not-leak-guidance",
				},
			}},
		},
	}
	encoded, err := json.Marshal(newVerificationOutput(loaded))
	if err != nil {
		t.Fatalf("编码 verifier 输出失败: %v", err)
	}
	for _, forbidden := range []string{"must-not-leak", "runtime_content\"", "ciphertext", "key_version", "publisher_user_id"} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("verifier 输出泄漏禁止字段/正文 %q: %s", forbidden, encoded)
		}
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &top); err != nil {
		t.Fatalf("解析 verifier 输出失败: %v", err)
	}
	assertExactJSONKeys(t, top, "status", "session_id", "snapshot_digest", "skill_count", "skills")
	var items []map[string]json.RawMessage
	if err := json.Unmarshal(top["skills"], &items); err != nil || len(items) != 1 {
		t.Fatalf("解析 verifier skills=%v err=%v", items, err)
	}
	assertExactJSONKeys(t, items[0], "load_order", "skill_id", "published_snapshot_id", "runtime_content_digest", "content_digest")
}

// assertExactJSONKeys 断言脱敏 JSON 对象不多不少只包含白名单键。
func assertExactJSONKeys(t *testing.T, object map[string]json.RawMessage, expected ...string) {
	t.Helper()
	if len(object) != len(expected) {
		t.Fatalf("JSON 键数量=%d want=%d object=%v", len(object), len(expected), object)
	}
	for _, key := range expected {
		if _, exists := object[key]; !exists {
			t.Fatalf("JSON 缺少白名单键 %q: %v", key, object)
		}
	}
}
