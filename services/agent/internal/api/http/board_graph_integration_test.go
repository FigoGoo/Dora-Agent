package http

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/FigoGoo/Dora-Agent/internal/contracts/boardgraph"
	"github.com/FigoGoo/Dora-Agent/internal/contracts/foundation"
	"github.com/FigoGoo/Dora-Agent/internal/testdb"
	"github.com/FigoGoo/Dora-Agent/services/agent/internal/application/workbench"
	runtimestream "github.com/FigoGoo/Dora-Agent/services/agent/internal/events/stream"
	"github.com/FigoGoo/Dora-Agent/services/agent/internal/infra/repository"
	"github.com/FigoGoo/Dora-Agent/services/agent/internal/runtime/creation"
)

func TestBoardGraphHTTPRoutes(t *testing.T) {
	db := testdb.StartPostgres(t, "dora_agent_board_graph_http")
	migrator := testdb.ApplyMigrations(t, db.URL, "db/migrations/iterations/2026-07-01-agent-runtime-contracts/agent")
	t.Cleanup(func() { testdb.DownMigrations(t, migrator) })

	repo := repository.New(db.DB)
	now := time.Date(2026, 7, 1, 11, 0, 0, 0, time.UTC)
	runtime := creation.New(func() time.Time { return now })
	creationResult, err := runtime.ExecuteGenericCreation(t.Context(), creation.GenericCreationInput{
		RunID:                "run_city_tourism_001",
		ProjectID:            "prj_active_1001",
		SessionID:            "sess_city_001",
		SpaceID:              "sp_personal_1001",
		ActorUserID:          "usr_1001",
		TraceID:              "trace-board-graph-http",
		Prompt:               "生成一支 30 秒城市文旅宣传短片，风格明亮、真实、有文化质感",
		RouterDecisionDigest: "sha256:" + strings.Repeat("2", 64),
	})
	if err != nil {
		t.Fatalf("execute generic creation: %v", err)
	}
	if err := repo.SaveGenericCreationState(t.Context(), creationResult.GraphTemplate, creationResult.GraphPlan, creationResult.Board, creationResult.Elements, creationResult.Events); err != nil {
		t.Fatalf("save generic creation state: %v", err)
	}

	eventBus := runtimestream.NewMemoryAGUIEventBus()
	app := workbench.New(repo, workbench.StaticGateway{
		Auth:   workbench.AuthContextDTO{ActorUserID: "usr_1001", LoginIdentityType: "personal", SpaceID: "sp_personal_1001"},
		Space:  workbench.SpaceContextDTO{SpaceID: "sp_personal_1001", SpaceType: "personal", CreditAccountID: "ca_personal_1001"},
		Access: workbench.ProjectAccessDTO{Allowed: true, ProjectStatus: "active", CreativeAllowed: true, AllowedActions: []string{"view", "continue_creation"}},
	}, "local-dev")
	app.SetRuntimePrimitives(eventBus, nil, nil)
	router := NewRouter(RouterOptions{App: app})

	boardResp := agentJSON(t, router, http.MethodGet, "/api/agent/boards/"+creationResult.Board.BoardID, "", nil)
	board := boardResp["board"].(map[string]any)
	snapshot := boardResp["snapshot"].(map[string]any)
	if board["status"] != "ready" || snapshot["status"] != "ready" || snapshot["version"] != float64(1) {
		t.Fatalf("unexpected board response: %#v", boardResp)
	}

	graphResp := agentJSON(t, router, http.MethodGet, "/api/agent/graphs/"+creationResult.GraphPlan.GraphPlanID, "", nil)
	graphPlan := graphResp["graph_plan"].(map[string]any)
	if graphPlan["graph_plan_id"] != creationResult.GraphPlan.GraphPlanID || graphPlan["status"] != "compiled" {
		t.Fatalf("unexpected graph response: %#v", graphResp)
	}
	initialReplay := agentJSON(t, router, http.MethodGet, "/api/agent/runs/"+creationResult.GraphPlan.RunID+"/events?after_sequence=0&limit=10", "", nil)
	initialEvents := initialReplay["events"].([]any)
	if len(initialEvents) != 2 || initialEvents[0].(map[string]any)["type"] != "graph.plan.created" || initialEvents[1].(map[string]any)["type"] != "board.snapshot.updated" {
		t.Fatalf("unexpected initial board graph event replay: %#v", initialReplay)
	}

	afterBoard := creationResult.Board
	afterBoard.Version = 2
	afterBoard.ElementsCount = len(creationResult.Elements) + 1
	afterBoard.UpdatedAt = now.Add(30 * time.Second)
	patchedElement := boardgraph.CreativeElement{
		SchemaVersion: boardgraph.SchemaVersionCreativeElement,
		ElementID:     "elem_patch_note_001",
		BoardID:       creationResult.Board.BoardID,
		ElementType:   boardgraph.BoardElementTypeTextNote,
		Source:        boardgraph.BoardElementSourceUser,
		Status:        boardgraph.BoardElementStatusReady,
		Position:      boardgraph.ElementPosition{X: 0, Y: 760, Width: 640, Height: 160, Order: 3},
		Content: map[string]any{
			"text": "补充城市夜景转场，保持明亮真实风格",
		},
		LinkedAssetIDs: []string{},
		CreatedAt:      afterBoard.UpdatedAt,
		UpdatedAt:      afterBoard.UpdatedAt,
	}
	patchedElement.ContentDigest = testDigest(t, patchedElement.Content)
	afterElements := append(append([]boardgraph.CreativeElement{}, creationResult.Elements...), patchedElement)
	afterBoard.BoardDigest = testDigest(t, map[string]any{"board_id": afterBoard.BoardID, "version": afterBoard.Version, "elements": afterElements})
	patchPayload := map[string]any{
		"board_after":         afterBoard,
		"elements_after":      afterElements,
		"changed_element_ids": []string{patchedElement.ElementID},
	}
	patch := boardgraph.BoardPatch{
		SchemaVersion:  boardgraph.SchemaVersionBoardPatch,
		PatchID:        "patch_add_note_001",
		BoardID:        creationResult.Board.BoardID,
		BaseVersion:    1,
		TargetVersion:  2,
		Operation:      boardgraph.BoardPatchOperationAddElement,
		Actor:          boardgraph.BoardPatchActorUser,
		IdempotencyKey: "idem-board-patch",
		Payload:        patchPayload,
		PatchDigest:    testDigest(t, patchPayload),
		CreatedAt:      afterBoard.UpdatedAt,
	}
	patchResp := agentJSON(t, router, http.MethodPost, "/api/agent/boards/"+creationResult.Board.BoardID+"/patches", "idem-board-patch", map[string]any{"patch": patch})
	patchedBoard := patchResp["board"].(map[string]any)
	if patchedBoard["version"] != float64(2) || patchedBoard["elements_count"] != float64(3) {
		t.Fatalf("unexpected patch response: %#v", patchResp)
	}
	replayAfterPatch := agentJSON(t, router, http.MethodGet, "/api/agent/runs/"+creationResult.GraphPlan.RunID+"/events?after_sequence=0&limit=10", "", nil)
	eventsAfterPatch := replayAfterPatch["events"].([]any)
	if len(eventsAfterPatch) != 4 || eventsAfterPatch[2].(map[string]any)["type"] != "board.patch.applied" || eventsAfterPatch[3].(map[string]any)["type"] != "board.snapshot.updated" {
		t.Fatalf("unexpected board graph event replay after patch: %#v", replayAfterPatch)
	}
	busEventsAfterPatch, err := eventBus.ReplayAGUI(t.Context(), creationResult.GraphPlan.RunID, 2, 10)
	if err != nil {
		t.Fatalf("replay runtime bus after patch: %v", err)
	}
	if len(busEventsAfterPatch) != 2 || busEventsAfterPatch[0].EventType != boardgraph.EventTypeBoardPatchApplied || busEventsAfterPatch[1].EventType != boardgraph.EventTypeBoardSnapshotUpdated {
		t.Fatalf("unexpected runtime bus events after patch: %#v", busEventsAfterPatch)
	}

	approveResp := agentJSON(t, router, http.MethodPost, "/api/agent/boards/"+creationResult.Board.BoardID+"/approve", "idem-board-approve", map[string]any{
		"approved_by":   "usr_1001",
		"board_version": 2,
	})
	approvedBoard := approveResp["board"].(map[string]any)
	if approvedBoard["status"] != "approved" || approvedBoard["version"] != float64(3) || approvedBoard["tool_plan_allowed"] != true {
		t.Fatalf("unexpected approve response: %#v", approveResp)
	}
	approvePatch := approveResp["patch"].(map[string]any)
	if approvePatch["operation"] != "approve_board" {
		t.Fatalf("approve response missing approve patch: %#v", approveResp)
	}

	retryResp := agentJSON(t, router, http.MethodPost, "/api/agent/boards/"+creationResult.Board.BoardID+"/approve", "idem-board-approve", map[string]any{
		"approved_by":   "usr_1001",
		"board_version": 2,
	})
	retryBoard := retryResp["board"].(map[string]any)
	if retryBoard["status"] != "approved" || retryBoard["version"] != float64(3) {
		t.Fatalf("unexpected idempotent approve response: %#v", retryResp)
	}
	run, err := repo.GetAgentRunV1(t.Context(), creationResult.GraphPlan.RunID)
	if err != nil {
		t.Fatalf("get run after HTTP approve: %v", err)
	}
	if run.Status != foundation.RunStatusPlanning {
		t.Fatalf("expected run planning after HTTP approve, got %#v", run)
	}
	replayAfterApproval := agentJSON(t, router, http.MethodGet, "/api/agent/runs/"+creationResult.GraphPlan.RunID+"/events?after_sequence=0&limit=10", "", nil)
	eventsAfterApproval := replayAfterApproval["events"].([]any)
	if len(eventsAfterApproval) != 6 || eventsAfterApproval[4].(map[string]any)["type"] != "board.patch.applied" || eventsAfterApproval[5].(map[string]any)["type"] != "board.snapshot.updated" {
		t.Fatalf("unexpected board graph event replay after approval: %#v", replayAfterApproval)
	}
	busEventsAfterApproval, err := eventBus.ReplayAGUI(t.Context(), creationResult.GraphPlan.RunID, 4, 10)
	if err != nil {
		t.Fatalf("replay runtime bus after approval: %v", err)
	}
	if len(busEventsAfterApproval) != 2 || busEventsAfterApproval[0].EventType != boardgraph.EventTypeBoardPatchApplied || busEventsAfterApproval[1].EventType != boardgraph.EventTypeBoardSnapshotUpdated {
		t.Fatalf("unexpected runtime bus events after approval: %#v", busEventsAfterApproval)
	}

	archivedRouter := NewRouter(RouterOptions{App: workbench.New(repo, workbench.StaticGateway{
		Auth:   workbench.AuthContextDTO{ActorUserID: "usr_1001", LoginIdentityType: "personal", SpaceID: "sp_personal_1001"},
		Space:  workbench.SpaceContextDTO{SpaceID: "sp_personal_1001", SpaceType: "personal", CreditAccountID: "ca_personal_1001"},
		Access: workbench.ProjectAccessDTO{Allowed: true, ProjectStatus: "archived", CreativeAllowed: false, AllowedActions: []string{"view"}, UserMessage: "project archived"},
	}, "local-dev")})
	archivedRead := agentJSON(t, archivedRouter, http.MethodGet, "/api/agent/boards/"+creationResult.Board.BoardID, "", nil)
	if archivedRead["board"].(map[string]any)["status"] != "approved" {
		t.Fatalf("archived project should still allow board read: %#v", archivedRead)
	}
	archivedApprove := agentRaw(t, archivedRouter, http.MethodPost, "/api/agent/boards/"+creationResult.Board.BoardID+"/approve", "idem-board-approve-archived", map[string]any{
		"approved_by":   "usr_1001",
		"board_version": 2,
	})
	if archivedApprove.Code != http.StatusConflict || archivedApprove.ErrorCode() != "PROJECT_ARCHIVED" {
		t.Fatalf("expected archived approve block, status=%d body=%#v", archivedApprove.Code, archivedApprove.Body)
	}
}

func testDigest(t *testing.T, value any) string {
	t.Helper()
	digest, err := foundation.CanonicalDigest(value)
	if err != nil {
		t.Fatalf("digest: %v", err)
	}
	return digest
}
