package release_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	nethttp "net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/FigoGoo/Dora-Agent/internal/contracts/foundation"
	"github.com/FigoGoo/Dora-Agent/internal/testdb"
	"github.com/FigoGoo/Dora-Agent/internal/testredis"
	agenthttp "github.com/FigoGoo/Dora-Agent/services/agent/internal/api/http"
	"github.com/FigoGoo/Dora-Agent/services/agent/internal/application/workbench"
	runtimestream "github.com/FigoGoo/Dora-Agent/services/agent/internal/events/stream"
	"github.com/FigoGoo/Dora-Agent/services/agent/internal/infra/repository"
	"github.com/FigoGoo/Dora-Agent/services/agent/internal/runtime/creation"
)

func TestReleaseAgentHTTPRedisRuntimeGate(t *testing.T) {
	db := testdb.StartPostgres(t, "dora_release_agent_http_redis")
	migrator := testdb.ApplyMigrations(t, db.URL, "db/migrations/iterations/2026-07-01-agent-runtime-contracts/agent")
	t.Cleanup(func() { testdb.DownMigrations(t, migrator) })
	redisServer := testredis.Start(t)

	sqlDB, err := db.DB.DB()
	if err != nil {
		t.Fatalf("open sql database: %v", err)
	}
	redisBus, err := runtimestream.NewRedisAGUIEventBus(redisServer.Client, 1000)
	if err != nil {
		t.Fatalf("create redis AG-UI event bus: %v", err)
	}
	snapshotCache, err := runtimestream.NewRedisSnapshotCache(redisServer.Client)
	if err != nil {
		t.Fatalf("create redis snapshot cache: %v", err)
	}
	turnLock, err := runtimestream.NewRedisTurnLock(redisServer.Client)
	if err != nil {
		t.Fatalf("create redis turn lock: %v", err)
	}

	repo := repository.New(db.DB)
	now := time.Date(2026, 7, 1, 12, 30, 0, 0, time.UTC)
	runtime := creation.New(func() time.Time { return now })
	creationResult, err := runtime.ExecuteGenericCreation(t.Context(), creation.GenericCreationInput{
		RunID:                "run_release_http_redis_001",
		ProjectID:            "prj_release_http_redis_001",
		SessionID:            "sess_release_http_redis_001",
		SpaceID:              "sp_personal_1001",
		ActorUserID:          "usr_1001",
		TraceID:              "trace-release-http-redis",
		Prompt:               "生成一支 30 秒城市文旅宣传短片，风格明亮、真实、有文化质感",
		RouterDecisionDigest: "sha256:" + strings.Repeat("5", 64),
	})
	if err != nil {
		t.Fatalf("execute generic creation: %v", err)
	}
	if err := repo.SaveGenericCreationState(t.Context(), creationResult.GraphTemplate, creationResult.GraphPlan, creationResult.Board, creationResult.Elements, creationResult.Events); err != nil {
		t.Fatalf("save generic creation state: %v", err)
	}
	for _, event := range creationResult.Events {
		if err := redisBus.PublishAGUI(t.Context(), event); err != nil {
			t.Fatalf("publish initial event to redis: %v", err)
		}
	}
	extraEvent := releaseRedisOnlyEvent(t, creationResult, now.Add(time.Minute), 3)
	if err := redisBus.PublishAGUI(t.Context(), extraEvent); err != nil {
		t.Fatalf("publish redis-only event: %v", err)
	}
	if err := redisBus.PublishAGUI(t.Context(), extraEvent); err != nil {
		t.Fatalf("publish duplicate redis-only event: %v", err)
	}
	if length, err := redisServer.Client.XLen(t.Context(), runtimestream.RunEventsKey(creationResult.GraphPlan.RunID)).Result(); err != nil || length != 3 {
		t.Fatalf("expected Redis stream to dedupe duplicate event, length=%d err=%v", length, err)
	}

	app := workbench.New(repo, workbench.StaticGateway{
		Auth:   workbench.AuthContextDTO{ActorUserID: "usr_1001", LoginIdentityType: "personal", SpaceID: "sp_personal_1001"},
		Space:  workbench.SpaceContextDTO{SpaceID: "sp_personal_1001", SpaceType: "personal", CreditAccountID: "ca_personal_1001"},
		Access: workbench.ProjectAccessDTO{Allowed: true, ProjectStatus: "active", CreativeAllowed: true, AllowedActions: []string{"view", "continue_creation"}},
	}, "local-dev")
	app.SetRuntimePrimitives(redisBus, snapshotCache, turnLock)
	router := agenthttp.NewRouter(agenthttp.RouterOptions{
		App: app,
		Ready: func(ctx context.Context) error {
			if err := sqlDB.PingContext(ctx); err != nil {
				return err
			}
			return redisServer.Client.Ping(ctx).Err()
		},
	})

	assertAgentHTTPStatus(t, router, "/healthz", nethttp.StatusOK)
	assertAgentHTTPStatus(t, router, "/readyz", nethttp.StatusOK)
	replay := agentJSON(t, router, nethttp.MethodGet, "/api/agent/runs/"+creationResult.GraphPlan.RunID+"/events?after_sequence=0&limit=10", nil)
	events := replay["events"].([]any)
	if len(events) != 3 {
		t.Fatalf("expected HTTP replay to read 3 Redis-backed events, got %#v", replay)
	}
	lastEvent := events[2].(map[string]any)
	if lastEvent["type"] != extraEvent.EventType || lastEvent["sequence"] != float64(extraEvent.Seq) {
		t.Fatalf("unexpected Redis-backed replay event: %#v", lastEvent)
	}

	snapshotKey := runtimestream.RunSnapshotKey(creationResult.GraphPlan.RunID)
	if err := snapshotCache.Set(t.Context(), snapshotKey, []byte(`{"source":"release"}`), time.Minute); err != nil {
		t.Fatalf("set redis snapshot cache: %v", err)
	}
	if value, ok, err := snapshotCache.Get(t.Context(), snapshotKey); err != nil || !ok || string(value) != `{"source":"release"}` {
		t.Fatalf("get redis snapshot cache value=%s ok=%v err=%v", string(value), ok, err)
	}
	lockKey := runtimestream.TurnLockKey(creationResult.GraphPlan.RunID)
	if acquired, err := turnLock.Acquire(t.Context(), lockKey, "owner_a", time.Minute); err != nil || !acquired {
		t.Fatalf("acquire redis turn lock acquired=%v err=%v", acquired, err)
	}
	if acquired, err := turnLock.Acquire(t.Context(), lockKey, "owner_b", time.Minute); err != nil || acquired {
		t.Fatalf("duplicate redis turn lock acquire should wait acquired=%v err=%v", acquired, err)
	}
	if released, err := turnLock.Release(t.Context(), lockKey, "owner_a"); err != nil || !released {
		t.Fatalf("release redis turn lock released=%v err=%v", released, err)
	}
}

func releaseRedisOnlyEvent(t *testing.T, result creation.GenericCreationResult, createdAt time.Time, seq int64) foundation.AGUIEnvelope {
	t.Helper()
	payload := map[string]any{
		"board_id": result.Board.BoardID,
		"source":   "release_http_redis_gate",
		"seq":      seq,
	}
	digest, err := foundation.CanonicalDigest(payload)
	if err != nil {
		t.Fatalf("build payload digest: %v", err)
	}
	event, err := foundation.BuildAGUIEnvelope(foundation.AGUIInput{
		EventID:       "evt_release_http_redis_003",
		EventType:     "board.patch.applied",
		ProjectID:     result.Board.ProjectID,
		SpaceID:       "sp_personal_1001",
		ActorUserID:   "usr_1001",
		SessionID:     result.Board.SessionID,
		RunID:         result.Board.RunID,
		Seq:           seq,
		CreatedAt:     createdAt,
		PayloadDigest: digest,
		TraceID:       "trace-release-http-redis",
		Payload:       payload,
	})
	if err != nil {
		t.Fatalf("build Redis-only AG-UI event: %v", err)
	}
	return event
}

func assertAgentHTTPStatus(t *testing.T, router nethttp.Handler, path string, expected int) {
	t.Helper()
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(nethttp.MethodGet, path, nil))
	if rec.Code != expected {
		body, _ := io.ReadAll(rec.Body)
		t.Fatalf("GET %s status=%d body=%s", path, rec.Code, string(body))
	}
}

func agentJSON(t *testing.T, router nethttp.Handler, method string, path string, body any) map[string]any {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		_ = json.NewEncoder(&buf).Encode(body)
	}
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer test-agent-token")
	req.Header.Set("X-Trace-Id", "trace-release-http-redis")
	req.Header.Set("X-Actor-User-Id", "usr_1001")
	req.Header.Set("X-Space-Id", "sp_personal_1001")
	req.Header.Set("X-Login-Identity-Type", "personal")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != nethttp.StatusOK {
		t.Fatalf("%s %s status=%d body=%s", method, path, rec.Code, rec.Body.String())
	}
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode response: %v body=%s", err, rec.Body.String())
	}
	return out
}
