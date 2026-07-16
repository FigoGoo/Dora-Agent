// Package contract 验证 Business 消费端生成代码与 Agent-owned W0 Session IDL 保持一致。
package contract

import (
	"reflect"
	"testing"

	"github.com/FigoGoo/Dora-Agent/business/kitex_gen/sessionv1"
)

// TestAgentSessionGeneratedContract 验证服务名、Schema、枚举编号和可选 Receipt 语义没有生成漂移。
func TestAgentSessionGeneratedContract(t *testing.T) {
	if sessionv1.AGENT_SESSION_SERVICE_NAME != "dora.agent.session.v1" ||
		sessionv1.ENSURE_PROJECT_SESSION_SCHEMA_VERSION != "ensure_project_session.v1" ||
		sessionv1.QUERY_PROJECT_SESSION_COMMAND_SCHEMA_VERSION != "query_project_session_command.v1" {
		t.Fatalf("Session RPC 常量漂移")
	}
	if sessionv1.CreationSourceV1_QUICK_CREATE != 1 || sessionv1.SkillSnapshotModeV1_EMPTY != 1 ||
		sessionv1.EnsureDispositionV1_CREATED != 1 || sessionv1.EnsureDispositionV1_REPLAYED != 2 ||
		sessionv1.QueryProjectSessionCommandStatusV1_NOT_FOUND != 1 ||
		sessionv1.QueryProjectSessionCommandStatusV1_COMPLETED != 2 ||
		sessionv1.QueryProjectSessionCommandStatusV1_CONFLICT != 3 {
		t.Fatalf("Session RPC 枚举编号漂移")
	}
	response := sessionv1.NewQueryProjectSessionCommandResponseV1()
	if response.IsSetReceipt() {
		t.Fatalf("Query Receipt 必须保持可选，not_found/conflict 不携带结果")
	}
	requestType := reflect.TypeOf(sessionv1.EnsureProjectSessionRequestV1{})
	wantTags := map[string]string{
		"SchemaVersion": "schema_version,1,required", "RequestId": "request_id,2,required",
		"CommandId": "command_id,3,required", "RequestDigest": "request_digest,4,required",
		"ProjectId": "project_id,5,required", "OwnerUserId": "owner_user_id,6,required",
		"CreationSource": "creation_source,7,required", "InitialPrompt": "initial_prompt,8,optional",
		"PromptDigest": "prompt_digest,9,required", "SkillSnapshotMode": "skill_snapshot_mode,10,required",
		"RequestedAtUnixMs": "requested_at_unix_ms,11,required",
	}
	for fieldName, wantTag := range wantTags {
		field, exists := requestType.FieldByName(fieldName)
		if !exists || field.Tag.Get("thrift") != wantTag {
			t.Fatalf("Ensure 字段 %s 编号/required 漂移: got=%q want=%q", fieldName, field.Tag.Get("thrift"), wantTag)
		}
	}
}
