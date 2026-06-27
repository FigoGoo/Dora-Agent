package http

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/FigoGoo/Dora-Agent/internal/testdb"
	"github.com/FigoGoo/Dora-Agent/services/agent/internal/application/workbench"
	"github.com/FigoGoo/Dora-Agent/services/agent/internal/domain/model"
	"github.com/FigoGoo/Dora-Agent/services/agent/internal/domain/state"
	"github.com/FigoGoo/Dora-Agent/services/agent/internal/infra/repository"
	"gorm.io/datatypes"
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
	})
	runID := runResp["run_id"].(string)
	if runResp["status"] != "pending" {
		t.Fatalf("expected pending run, got %#v", runResp)
	}
	stream := httptest.NewRecorder()
	router.ServeHTTP(stream, agentRequest(http.MethodGet, "/api/agent/runs/"+runID+"/stream", "", nil))
	if stream.Code != http.StatusOK || !strings.Contains(stream.Body.String(), "agent.run.created") {
		t.Fatalf("stream route did not replay run event, status=%d body=%s", stream.Code, stream.Body.String())
	}
	appended := agentJSON(t, router, http.MethodPost, "/api/agent/runs/"+runID+"/messages", "idem-append", map[string]any{
		"user_input": map[string]any{"client_message_id": "cm_append", "content_type": "text", "text": "more context"},
	})
	if appended["run_id"] != runID {
		t.Fatalf("append input returned unexpected run: %#v", appended)
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

	acceptRunID := createWaitingInterruptRun(t, router, repo, "accept", "intr_accept_1001")
	accepted := agentJSON(t, router, http.MethodPost, "/api/agent/runs/"+acceptRunID+"/interrupts/intr_accept_1001/accept", "idem-accept", map[string]any{
		"run_id": acceptRunID, "interrupt_id": "intr_accept_1001", "action": "confirm", "confirmed_payload_digest": "sha256:payload",
	})
	if accepted["status"] != "resuming" {
		t.Fatalf("accept interrupt response = %#v", accepted)
	}

	rejectRunID := createWaitingInterruptRun(t, router, repo, "reject", "intr_reject_1001")
	rejected := agentJSON(t, router, http.MethodPost, "/api/agent/runs/"+rejectRunID+"/interrupts/intr_reject_1001/reject", "idem-reject", map[string]any{
		"run_id": rejectRunID, "interrupt_id": "intr_reject_1001", "reason_code": "user_rejected",
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

	interruptRunID := createWaitingInterruptRun(t, allowedRouter, repo, "denied-accept", "intr_denied_accept_1001")
	deniedRouter := NewRouter(RouterOptions{App: workbench.New(repo, workbench.StaticGateway{
		Space:  workbench.SpaceContextDTO{SpaceID: "sp_personal_1001", SpaceType: "personal", CreditAccountID: "ca_personal_1001"},
		Access: workbench.ProjectAccessDTO{Allowed: false, ProjectStatus: "active", CreativeAllowed: true, UserMessage: "project permission denied"},
	}, "local-dev")})
	acceptResp := agentRaw(t, deniedRouter, http.MethodPost, "/api/agent/runs/"+interruptRunID+"/interrupts/intr_denied_accept_1001/accept", "idem-denied-accept", map[string]any{
		"run_id": interruptRunID, "interrupt_id": "intr_denied_accept_1001", "action": "confirm", "confirmed_payload_digest": "sha256:payload",
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
	req.Header.Set("X-Trace-Id", "trace-agent-m2")
	req.Header.Set("X-Actor-User-Id", "usr_1001")
	req.Header.Set("X-Space-Id", "sp_personal_1001")
	req.Header.Set("X-Login-Identity-Type", "personal")
	if idem != "" {
		req.Header.Set("Idempotency-Key", idem)
	}
	return req
}

func createWaitingInterruptRun(t *testing.T, router http.Handler, repo *repository.Repository, suffix string, interruptID string) string {
	t.Helper()
	sessionResp := agentJSON(t, router, http.MethodPost, "/api/agent/sessions", "idem-session-"+suffix, map[string]any{"project_id": "prj_active_1001", "initial_title": suffix})
	sessionID := sessionResp["session_id"].(string)
	runResp := agentJSON(t, router, http.MethodPost, "/api/agent/runs", "idem-run-"+suffix, map[string]any{
		"session_id": sessionID,
		"project_id": "prj_active_1001",
		"user_input": map[string]any{"client_message_id": "cm_" + suffix, "content_type": "text", "text": "needs confirmation"},
	})
	runID := runResp["run_id"].(string)
	if err := repo.UpdateRunStatus(t.Context(), runID, state.RunStatusRunning, "", ""); err != nil {
		t.Fatalf("mark running: %v", err)
	}
	if err := repo.UpdateRunStatus(t.Context(), runID, state.RunStatusWaitingConfirmation, "", ""); err != nil {
		t.Fatalf("mark waiting confirmation: %v", err)
	}
	if err := repo.CreateInterrupt(t.Context(), &model.Interrupt{
		ID: interruptID, RunID: runID, InterruptType: "risk_confirmation", Status: state.InterruptStatusRequired,
		Reason: "manual confirmation", ConfirmationPayload: datatypes.JSON([]byte(`{"confirmation_id":"` + interruptID + `"}`)),
		AllowedActions: datatypes.JSON([]byte(`["confirm","reject"]`)), ExpiresAt: time.Now().UTC().Add(time.Hour), TraceID: "trace-agent-m2",
	}); err != nil {
		t.Fatalf("create interrupt: %v", err)
	}
	return runID
}
