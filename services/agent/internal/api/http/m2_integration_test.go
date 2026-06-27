package http

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/FigoGoo/Dora-Agent/internal/testdb"
	"github.com/FigoGoo/Dora-Agent/services/agent/internal/application/workbench"
	"github.com/FigoGoo/Dora-Agent/services/agent/internal/domain/state"
	"github.com/FigoGoo/Dora-Agent/services/agent/internal/infra/repository"
)

func TestM2AgentSessionRunProjectGate(t *testing.T) {
	db := testdb.StartPostgres(t, "dora_agent_m2")
	migrator := testdb.ApplyMigrations(t, db.URL, "db/migrations/iterations/20260627_agent_runtime/agent")
	t.Cleanup(func() { testdb.DownMigrations(t, migrator) })

	repo := repository.New(db.DB)
	app := workbench.New(repo, workbench.StaticGateway{
		Space:  workbench.SpaceContextDTO{SpaceID: "sp_personal_1001", SpaceType: "personal", CreditAccountID: "ca_personal_1001"},
		Access: workbench.ProjectAccessDTO{Allowed: true, ProjectStatus: "active", CreativeAllowed: true, AllowedActions: []string{"view", "continue_creation"}},
	}, "local-dev")
	router := NewRouter(RouterOptions{App: app})

	sessionResp := agentJSON(t, router, http.MethodPost, "/api/agent/sessions", "idem-agent-session", map[string]any{"project_id": "prj_active_1001", "initial_title": "M2"})
	sessionID := sessionResp["session_id"].(string)
	if sessionID == "" {
		t.Fatalf("session response missing session_id: %#v", sessionResp)
	}

	runResp := agentJSON(t, router, http.MethodPost, "/api/agent/runs", "idem-agent-run", map[string]any{
		"session_id": sessionID,
		"project_id": "prj_active_1001",
		"user_input": map[string]any{"client_message_id": "cm_1", "content_type": "text", "text": "hello"},
		"referenced_assets": []map[string]any{{
			"asset_id": "ast_generated_1001", "project_id": "prj_active_1001", "source": "project_asset", "purpose": "style_reference", "metadata_digest": "sha256:asset",
		}},
		"control_inputs": []map[string]any{{
			"control_id": "aspect_ratio", "type": "string", "value": "1:1", "display_label": "Aspect ratio", "required": true, "validation_digest": "sha256:control",
		}},
	})
	runID := runResp["run_id"].(string)
	if runResp["status"] != "waiting_confirmation" {
		t.Fatalf("expected waiting confirmation run after M4 credit estimate, got %#v", runResp)
	}
	createdRun, err := repo.GetRun(t.Context(), runID)
	if err != nil {
		t.Fatalf("get created run: %v", err)
	}
	var inputSummary map[string]any
	if err := json.Unmarshal(createdRun.InputSummary, &inputSummary); err != nil {
		t.Fatalf("decode input summary: %v", err)
	}
	if inputSummary["referenced_asset_count"] != float64(1) || inputSummary["control_input_count"] != float64(1) {
		t.Fatalf("run input summary dropped referenced assets or controls: %#v", inputSummary)
	}
	stream := httptest.NewRecorder()
	router.ServeHTTP(stream, agentRequest(http.MethodGet, "/api/agent/runs/"+runID+"/stream", "", nil))
	if stream.Code != http.StatusOK || !strings.Contains(stream.Body.String(), "agent.run.started") {
		t.Fatalf("stream route did not replay run event, status=%d body=%s", stream.Code, stream.Body.String())
	}
	appended := agentJSON(t, router, http.MethodPost, "/api/agent/runs/"+runID+"/messages", "idem-append", map[string]any{
		"user_input": map[string]any{"client_message_id": "cm_append", "content_type": "text", "text": "more context"},
	})
	if appended["run_id"] != runID {
		t.Fatalf("append input returned unexpected run: %#v", appended)
	}
	replayAfterAppend := agentJSON(t, router, http.MethodGet, "/api/agent/runs/"+runID+"/events?after_sequence=0&limit=100", "", nil)
	events := replayAfterAppend["events"].([]any)
	assertCanonicalAgentEvents(t, events)
	assertNoProviderRuntimeRef(t, events)
	if countEvent(events, "safety.prompt.evaluating") < 2 || countEvent(events, "safety.prompt.evaluated") < 2 {
		t.Fatalf("expected start and append safety evaluation events: %#v", events)
	}
	if !hasAssetElementHints(events) {
		t.Fatalf("platform dictionary event dropped schema/render hints: %#v", events)
	}

	conflict := httptest.NewRecorder()
	req := agentRequest(http.MethodPost, "/api/agent/runs", "idem-agent-run-2", map[string]any{
		"session_id": sessionID,
		"project_id": "prj_active_1001",
		"user_input": map[string]any{"client_message_id": "cm_2", "content_type": "text", "text": "again"},
	})
	router.ServeHTTP(conflict, req)
	if conflict.Code != http.StatusConflict {
		t.Fatalf("expected active run conflict, status=%d body=%s", conflict.Code, conflict.Body.String())
	}

	cancelled := agentJSON(t, router, http.MethodPost, "/api/agent/runs/"+runID+"/cancel", "idem-cancel", map[string]any{"cancel_reason": "user_cancel"})
	if cancelled["status"] != "cancelled" {
		t.Fatalf("cancel response = %#v", cancelled)
	}
	replay := agentJSON(t, router, http.MethodGet, "/api/agent/runs/"+runID+"/events?after_sequence=0", "", nil)
	if len(replay["events"].([]any)) == 0 {
		t.Fatalf("expected replay events: %#v", replay)
	}

	archivedApp := workbench.New(repo, workbench.StaticGateway{
		Space:  workbench.SpaceContextDTO{SpaceID: "sp_personal_1001", SpaceType: "personal", CreditAccountID: "ca_personal_1001"},
		Access: workbench.ProjectAccessDTO{Allowed: true, ProjectStatus: "archived", CreativeAllowed: false, AllowedActions: []string{"view"}},
	}, "local-dev")
	archivedRouter := NewRouter(RouterOptions{App: archivedApp})
	snapshot := agentJSON(t, archivedRouter, http.MethodGet, "/api/agent/runs/"+runID+"/snapshot", "", nil)
	if snapshot["readonly_reason"] != "project_archived" {
		t.Fatalf("expected archived readonly snapshot, got %#v", snapshot)
	}
}

func TestM2AgentInterruptRoutes(t *testing.T) {
	db := testdb.StartPostgres(t, "dora_agent_m2_interrupt")
	migrator := testdb.ApplyMigrations(t, db.URL, "db/migrations/iterations/20260627_agent_runtime/agent")
	t.Cleanup(func() { testdb.DownMigrations(t, migrator) })

	repo := repository.New(db.DB)
	app := workbench.New(repo, workbench.StaticGateway{
		Space:  workbench.SpaceContextDTO{SpaceID: "sp_personal_1001", SpaceType: "personal", CreditAccountID: "ca_personal_1001"},
		Access: workbench.ProjectAccessDTO{Allowed: true, ProjectStatus: "active", CreativeAllowed: true, AllowedActions: []string{"view", "continue_creation"}},
	}, "local-dev")
	router := NewRouter(RouterOptions{App: app})

	acceptRunID, acceptInterruptID := createWaitingInterruptRun(t, router, repo, "accept")
	accepted := agentJSON(t, router, http.MethodPost, "/api/agent/runs/"+acceptRunID+"/interrupts/"+acceptInterruptID+"/accept", "idem-accept", map[string]any{
		"run_id": acceptRunID, "interrupt_id": acceptInterruptID, "action": "confirm", "confirmed_payload_digest": "sha256:payload",
	})
	if accepted["status"] != "completed" {
		t.Fatalf("accept interrupt response = %#v", accepted)
	}

	rejectRunID, rejectInterruptID := createWaitingInterruptRun(t, router, repo, "reject")
	rejected := agentJSON(t, router, http.MethodPost, "/api/agent/runs/"+rejectRunID+"/interrupts/"+rejectInterruptID+"/reject", "idem-reject", map[string]any{
		"run_id": rejectRunID, "interrupt_id": rejectInterruptID, "reason_code": "user_rejected",
	})
	if rejected["status"] != "failed" || rejected["error_code"] != "INTERRUPT_REJECTED" {
		t.Fatalf("reject interrupt response = %#v", rejected)
	}
}

func TestM2AgentDeniedAccessBodyBlocksResumeActions(t *testing.T) {
	db := testdb.StartPostgres(t, "dora_agent_m2_denied")
	migrator := testdb.ApplyMigrations(t, db.URL, "db/migrations/iterations/20260627_agent_runtime/agent")
	t.Cleanup(func() { testdb.DownMigrations(t, migrator) })

	repo := repository.New(db.DB)
	allowedRouter := NewRouter(RouterOptions{App: workbench.New(repo, workbench.StaticGateway{
		Space:  workbench.SpaceContextDTO{SpaceID: "sp_personal_1001", SpaceType: "personal", CreditAccountID: "ca_personal_1001"},
		Access: workbench.ProjectAccessDTO{Allowed: true, ProjectStatus: "active", CreativeAllowed: true, AllowedActions: []string{"view", "continue_creation"}},
	}, "local-dev")})

	sessionResp := agentJSON(t, allowedRouter, http.MethodPost, "/api/agent/sessions", "idem-denied-session", map[string]any{"project_id": "prj_active_1001", "initial_title": "denied"})
	sessionID := sessionResp["session_id"].(string)
	runResp := agentJSON(t, allowedRouter, http.MethodPost, "/api/agent/runs", "idem-denied-run", map[string]any{
		"session_id": sessionID,
		"project_id": "prj_active_1001",
		"user_input": map[string]any{"client_message_id": "cm_denied", "content_type": "text", "text": "hello"},
	})
	runID := runResp["run_id"].(string)

	archivedRouter := NewRouter(RouterOptions{App: workbench.New(repo, workbench.StaticGateway{
		Space:  workbench.SpaceContextDTO{SpaceID: "sp_personal_1001", SpaceType: "personal", CreditAccountID: "ca_personal_1001"},
		Access: workbench.ProjectAccessDTO{Allowed: true, ProjectStatus: "archived", CreativeAllowed: false, AllowedActions: []string{"view"}, UserMessage: "project archived"},
	}, "local-dev")})
	appendResp := agentRaw(t, archivedRouter, http.MethodPost, "/api/agent/runs/"+runID+"/messages", "idem-denied-append", map[string]any{
		"user_input": map[string]any{"client_message_id": "cm_after_archive", "content_type": "text", "text": "after archive"},
	})
	if appendResp.Code != http.StatusConflict || appendResp.ErrorCode() != "PROJECT_ARCHIVED" {
		t.Fatalf("expected PROJECT_ARCHIVED append block, status=%d body=%#v", appendResp.Code, appendResp.Body)
	}

	interruptRunID, interruptID := createWaitingInterruptRun(t, allowedRouter, repo, "denied-accept")
	deniedRouter := NewRouter(RouterOptions{App: workbench.New(repo, workbench.StaticGateway{
		Space:  workbench.SpaceContextDTO{SpaceID: "sp_personal_1001", SpaceType: "personal", CreditAccountID: "ca_personal_1001"},
		Access: workbench.ProjectAccessDTO{Allowed: false, ProjectStatus: "active", CreativeAllowed: true, UserMessage: "project permission denied"},
	}, "local-dev")})
	for _, path := range []string{
		"/api/agent/runs/" + runID,
		"/api/agent/sessions/" + sessionID + "/messages",
		"/api/agent/runs/" + runID + "/events?after_sequence=0",
		"/api/agent/runs/" + runID + "/snapshot",
	} {
		readResp := agentRaw(t, deniedRouter, http.MethodGet, path, "", nil)
		if readResp.Code != http.StatusForbidden || readResp.ErrorCode() != "PERMISSION_DENIED" {
			t.Fatalf("expected denied read path %s, status=%d body=%#v", path, readResp.Code, readResp.Body)
		}
	}
	acceptResp := agentRaw(t, deniedRouter, http.MethodPost, "/api/agent/runs/"+interruptRunID+"/interrupts/"+interruptID+"/accept", "idem-denied-accept", map[string]any{
		"run_id": interruptRunID, "interrupt_id": interruptID, "action": "confirm", "confirmed_payload_digest": "sha256:payload",
	})
	if acceptResp.Code != http.StatusForbidden || acceptResp.ErrorCode() != "PERMISSION_DENIED" {
		t.Fatalf("expected PERMISSION_DENIED interrupt block, status=%d body=%#v", acceptResp.Code, acceptResp.Body)
	}
	run, err := repo.GetRun(t.Context(), interruptRunID)
	if err != nil {
		t.Fatalf("get denied run: %v", err)
	}
	if run.Status != state.RunStatusWaitingConfirmation {
		t.Fatalf("denied interrupt accept changed run status: %s", run.Status)
	}
}

func TestM2AgentAuthRequired(t *testing.T) {
	router := NewRouter(RouterOptions{App: workbench.New(repository.New(nil), workbench.StaticGateway{}, "local-dev")})
	resp := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/agent/sessions", bytes.NewBufferString(`{"project_id":"p"}`))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(resp, req)
	if resp.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for missing auth context, got %d body=%s", resp.Code, resp.Body.String())
	}
}

func TestM3AgentAuthUsesAuthorizationToken(t *testing.T) {
	db := testdb.StartPostgres(t, "dora_agent_m3_auth")
	migrator := testdb.ApplyMigrations(t, db.URL, "db/migrations/iterations/20260627_agent_runtime/agent")
	t.Cleanup(func() { testdb.DownMigrations(t, migrator) })

	router := NewRouter(RouterOptions{App: workbench.New(repository.New(db.DB), workbench.StaticGateway{
		Auth:   workbench.AuthContextDTO{ActorUserID: "usr_1001", LoginIdentityType: "personal", SpaceID: "sp_personal_1001"},
		Space:  workbench.SpaceContextDTO{SpaceID: "sp_personal_1001", SpaceType: "personal", CreditAccountID: "ca_personal_1001"},
		Access: workbench.ProjectAccessDTO{Allowed: true, ProjectStatus: "active", CreativeAllowed: true, AllowedActions: []string{"view", "continue_creation"}},
	}, "local-dev")})
	req := agentRequest(http.MethodPost, "/api/agent/sessions", "idem-m3-auth-token", map[string]any{"project_id": "prj_active_1001", "initial_title": "auth"})
	req.Header.Set("X-Actor-User-Id", "malicious_user")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create session with token auth status=%d body=%s", rec.Code, rec.Body.String())
	}
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode auth response: %v", err)
	}
	session := out["snapshot"].(map[string]any)["session"].(map[string]any)
	if session["user_id"] != "usr_1001" {
		t.Fatalf("agent trusted identity header instead of token RPC: %#v", session)
	}
}

func agentJSON(t *testing.T, router http.Handler, method, path, idem string, body any) map[string]any {
	t.Helper()
	resp := agentRaw(t, router, method, path, idem, body)
	if resp.Code != http.StatusOK {
		t.Fatalf("%s %s status=%d body=%#v", method, path, resp.Code, resp.Body)
	}
	return resp.Body
}

type agentRawResponse struct {
	Code int
	Body map[string]any
}

func (r agentRawResponse) ErrorCode() string {
	if errBody, ok := r.Body["error"].(map[string]any); ok {
		if value, ok := errBody["code"].(string); ok {
			return value
		}
	}
	return ""
}

func assertCanonicalAgentEvents(t *testing.T, events []any) {
	t.Helper()
	allowed := map[string]bool{
		"agent.run.started": true, "agent.run.completed": true, "agent.run.cancelled": true, "agent.run.failed": true,
		"agent.thinking.started": true, "agent.thinking.delta": true, "agent.thinking.completed": true,
		"agent.message.delta": true, "agent.message.completed": true, "agent.skill.selected": true, "agent.skill.missing": true,
		"platform.tags.updated": true, "chat.controls.requested": true, "chat.controls.locked": true,
		"safety.prompt.evaluating": true, "safety.prompt.evaluated": true, "safety.prompt.blocked": true, "safety.prompt.failed": true,
		"credits.estimated": true, "confirmation.required": true, "confirmation.accepted": true, "confirmation.rejected": true,
		"resume.accepted": true, "credits.frozen": true, "credits.charged": true, "credits.released": true, "credits.insufficient": true,
		"tool.call.started": true, "tool.call.progress": true, "tool.call.completed": true, "tool.call.failed": true,
		"generation.progress": true, "generation.artifact.completed": true,
		"asset.save.started": true, "asset.save.completed": true, "asset.save.failed": true,
		"workspace.assets.updated": true, "workspace.blackboard.updated": true, "process.snapshot.saved": true,
		"project.archived.blocked": true,
	}
	requiredTopLevel := []string{"event_id", "type", "session_id", "run_id", "project_id", "space_id", "actor_user_id", "sequence", "timestamp", "component", "trace_id", "payload"}
	for _, raw := range events {
		event, ok := raw.(map[string]any)
		if !ok {
			t.Fatalf("event is not object: %#v", raw)
		}
		for _, field := range requiredTopLevel {
			if _, ok := event[field]; !ok {
				t.Fatalf("event missing AG-UI top-level field %s: %#v", field, event)
			}
		}
		eventType, _ := event["type"].(string)
		if !allowed[eventType] {
			t.Fatalf("non-canonical AG-UI event type %q in %#v", eventType, event)
		}
		if _, ok := event["created_at"]; ok {
			t.Fatalf("AG-UI event leaked non-schema created_at: %#v", event)
		}
	}
}

func countEvent(events []any, eventType string) int {
	count := 0
	for _, raw := range events {
		event, ok := raw.(map[string]any)
		if ok && event["type"] == eventType {
			count++
		}
	}
	return count
}

func hasAssetElementHints(events []any) bool {
	for _, raw := range events {
		event, ok := raw.(map[string]any)
		if !ok || event["type"] != "platform.tags.updated" {
			continue
		}
		payload, ok := event["payload"].(map[string]any)
		if !ok {
			continue
		}
		items, ok := payload["element_types"].([]any)
		if !ok || len(items) == 0 {
			continue
		}
		first, ok := items[0].(map[string]any)
		if ok && first["schema_hint_json"] != "" && first["render_hint_json"] != "" && first["usage_stage"] != "" && first["resource_type"] != "" {
			return true
		}
	}
	return false
}

func assertNoProviderRuntimeRef(t *testing.T, events []any) {
	t.Helper()
	for _, raw := range events {
		event, ok := raw.(map[string]any)
		if !ok || event["type"] != "generation.progress" {
			continue
		}
		payload, _ := event["payload"].(map[string]any)
		if _, exists := payload["provider_runtime_ref"]; exists {
			t.Fatalf("generation.progress leaked provider_runtime_ref: %#v", event)
		}
	}
}

func agentRaw(t *testing.T, router http.Handler, method, path, idem string, body any) agentRawResponse {
	t.Helper()
	req := agentRequest(method, path, idem, body)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode response: %v body=%s", err, rec.Body.String())
	}
	return agentRawResponse{Code: rec.Code, Body: out}
}

func agentRequest(method, path, idem string, body any) *http.Request {
	var buf bytes.Buffer
	if body != nil {
		_ = json.NewEncoder(&buf).Encode(body)
	}
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer test-agent-token")
	req.Header.Set("X-Trace-Id", "trace-agent-m2")
	req.Header.Set("X-Actor-User-Id", "usr_1001")
	req.Header.Set("X-Space-Id", "sp_personal_1001")
	req.Header.Set("X-Login-Identity-Type", "personal")
	if idem != "" {
		req.Header.Set("Idempotency-Key", idem)
	}
	return req
}

func createWaitingInterruptRun(t *testing.T, router http.Handler, repo *repository.Repository, suffix string) (string, string) {
	t.Helper()
	sessionResp := agentJSON(t, router, http.MethodPost, "/api/agent/sessions", "idem-session-"+suffix, map[string]any{"project_id": "prj_active_1001", "initial_title": suffix})
	sessionID := sessionResp["session_id"].(string)
	runResp := agentJSON(t, router, http.MethodPost, "/api/agent/runs", "idem-run-"+suffix, map[string]any{
		"session_id": sessionID,
		"project_id": "prj_active_1001",
		"user_input": map[string]any{"client_message_id": "cm_" + suffix, "content_type": "text", "text": "needs confirmation"},
	})
	runID := runResp["run_id"].(string)
	interrupt, err := repo.GetRequiredInterrupt(t.Context(), runID)
	if err != nil {
		t.Fatalf("get required interrupt: %v", err)
	}
	return runID, interrupt.ID
}
