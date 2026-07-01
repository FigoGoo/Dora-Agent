package release_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/FigoGoo/Dora-Agent/internal/testdb"
	"github.com/FigoGoo/Dora-Agent/internal/testprocess"
	"github.com/FigoGoo/Dora-Agent/internal/testredis"
)

func TestReleaseFullHTTPServiceE2ESmoke(t *testing.T) {
	businessDB := testdb.StartPostgres(t, "dora_release_full_business_http")
	businessMigrator := testdb.ApplyMigrations(t, businessDB.URL, "db/migrations/iterations/2026-06-27-business-core/business")
	t.Cleanup(func() { testdb.DownMigrations(t, businessMigrator) })
	testdb.ExecSQL(t, businessDB.DB, testdb.MustReadSQL(t, "tests/business/seed/business_core_seed.sql"))

	agentDB := testdb.StartPostgres(t, "dora_release_full_agent_http")
	agentMigrator := testdb.ApplyMigrations(t, agentDB.URL, "db/migrations/iterations/20260627_agent_runtime/agent")
	t.Cleanup(func() { testdb.DownMigrations(t, agentMigrator) })
	redisServer := testredis.Start(t)

	repoRoot := testdb.RepoRoot(t)
	businessBinary := buildReleaseBinary(t, repoRoot, "dora-business-full-http", "./services/business/cmd/business")
	agentBinary := buildReleaseBinary(t, repoRoot, "dora-agent-full-http", "./services/agent/cmd/agent")

	businessHTTPAddr := testprocess.FreeLocalAddr(t)
	businessHTTPPort := testprocess.LocalPort(t, businessHTTPAddr)
	businessKitexPort := testprocess.LocalPort(t, testprocess.FreeLocalAddr(t))
	business := startReleaseProcess(t, repoRoot, businessBinary, map[string]string{
		"DORA_CONFIG_SOURCE":                    "env",
		"APP_ENV":                               "test",
		"LOG_LEVEL":                             "debug",
		"BUSINESS_DATABASE_URL":                 businessDB.URL,
		"BUSINESS_SERVICE_NAME":                 "dora.business",
		"BUSINESS_KITEX_PORT":                   businessKitexPort,
		"KITEX_REGISTRY":                        "none",
		"KITEX_TIMEOUT_MS":                      "1000",
		"BUSINESS_HTTP_ENABLED":                 "true",
		"BUSINESS_HTTP_PORT":                    businessHTTPPort,
		"BUSINESS_HTTP_ADDR":                    businessHTTPAddr,
		"PUBLIC_WEB_BASE_URL":                   "http://127.0.0.1",
		"ADMIN_BOOTSTRAP_ACCOUNT":               "admin@dora.local",
		"ADMIN_BOOTSTRAP_PASSWORD_HASH":         "$argon2id$v=19$m=16384,t=1,p=1$ZG9yYS1sb2NhbC1zYWx0MQ$4jdN85WOR//36CwDBXmQQli7Mu8sYwHd+AM3HYmjPXI",
		"ADMIN_BOOTSTRAP_CREDENTIAL_SECRET_REF": "local/test/bootstrap",
		"TOS_ENDPOINT":                          "",
		"TOS_BUCKET":                            "dora-test",
		"TOS_ACCESS_KEY_ID":                     "",
		"TOS_SECRET_ACCESS_KEY":                 "",
		"TOS_REGION":                            "local",
		"TOS_BASE_URL":                          "http://127.0.0.1/tos",
		"SECRET_ENCRYPTION_KEY_REF":             "test-secret-ref",
		"CORS_ALLOWED_ORIGINS":                  "http://127.0.0.1",
	})
	logReleaseProcessOnFailure(t, "business", business)
	testprocess.AssertEndpointOK(t, "business", business.done, &business.waitErr, &business.output, "http://"+businessHTTPAddr+"/healthz")
	testprocess.AssertEndpointOK(t, "business", business.done, &business.waitErr, &business.output, "http://"+businessHTTPAddr+"/readyz")

	agentHTTPAddr := testprocess.FreeLocalAddr(t)
	agent := startReleaseProcess(t, repoRoot, agentBinary, map[string]string{
		"DORA_CONFIG_SOURCE":                    "env",
		"APP_ENV":                               "test",
		"LOG_LEVEL":                             "debug",
		"AGENT_DATABASE_URL":                    agentDB.URL,
		"AGENT_HTTP_ADDR":                       agentHTTPAddr,
		"AGENT_SERVICE_NAME":                    "dora.agent",
		"BUSINESS_SERVICE_NAME":                 "dora.business",
		"BUSINESS_HOSTPORTS":                    "127.0.0.1:" + businessKitexPort,
		"KITEX_REGISTRY":                        "none",
		"KITEX_TIMEOUT_MS":                      "1000",
		"AGENT_MODEL_ADAPTER":                   "local",
		"AGENT_GENERATION_QUEUE":                "inline",
		"AGENT_RUNTIME_REDIS_MODE":              "redis",
		"AGENT_RUNTIME_REDIS_ADDR":              redisServer.Addr,
		"AGENT_RUNTIME_REDIS_DB":                "0",
		"AGENT_RUNTIME_REDIS_STREAM_MAX_LEN":    "256",
		"AGENT_GENERATION_RECOVERY_STALE_AFTER": "30s",
		"DEEPSEEK_API_KEY":                      "",
	})
	logReleaseProcessOnFailure(t, "agent", agent)
	testprocess.AssertEndpointOK(t, "agent", agent.done, &agent.waitErr, &agent.output, "http://"+agentHTTPAddr+"/healthz")
	testprocess.AssertEndpointOK(t, "agent", agent.done, &agent.waitErr, &agent.output, "http://"+agentHTTPAddr+"/readyz")

	token := loginReleaseUser(t, "http://"+businessHTTPAddr)
	session := postReleaseAgentJSON(t, "http://"+agentHTTPAddr, token, "/api/agent/sessions", "idem-release-full-session", map[string]any{
		"project_id":    "prj_active_1001",
		"initial_title": "release full HTTP smoke",
	})
	sessionID := stringField(t, session, "session_id")

	guideRun := postReleaseAgentJSON(t, "http://"+agentHTTPAddr, token, "/api/agent/runs", "idem-release-full-guide-run", map[string]any{
		"session_id": sessionID,
		"project_id": "prj_active_1001",
		"run_intent": "entry_guide",
		"user_input": map[string]any{
			"client_message_id": "cm_release_guide",
			"content_type":      "text",
			"text":              "",
		},
	})
	if got := guideRun["status"]; got != "completed" {
		t.Fatalf("entry guide run should complete through real Business RPC, got %#v", guideRun)
	}
	guideReplay := getReleaseAgentJSON(t, "http://"+agentHTTPAddr, token, "/api/agent/runs/"+stringField(t, guideRun, "run_id")+"/events?after_sequence=0&limit=50")
	assertReleaseEventTypes(t, guideReplay, "creative.guide.presented", "agent.run.completed")

	normalRun := postReleaseAgentJSON(t, "http://"+agentHTTPAddr, token, "/api/agent/runs", "idem-release-full-normal-run", map[string]any{
		"session_id": sessionID,
		"project_id": "prj_active_1001",
		"run_intent": "normal",
		"user_input": map[string]any{
			"client_message_id": "cm_release_normal",
			"content_type":      "text",
			"text":              "帮我做一个产品宣传片，年轻一点",
		},
	})
	if got := normalRun["status"]; got != "waiting_input" {
		t.Fatalf("ambiguous normal run should stop at router clarify gate, got %#v", normalRun)
	}
	normalReplay := getReleaseAgentJSON(t, "http://"+agentHTTPAddr, token, "/api/agent/runs/"+stringField(t, normalRun, "run_id")+"/events?after_sequence=0&limit=100")
	assertReleaseEventTypes(t, normalReplay, "creative.router.decided", "agent.message.completed")
}

type releaseProcess struct {
	cmd     *exec.Cmd
	done    chan struct{}
	waitErr error
	output  bytes.Buffer
}

func buildReleaseBinary(t *testing.T, repoRoot, name, packagePath string) string {
	t.Helper()
	binary := filepath.Join(t.TempDir(), name)
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, testprocess.GoBinary(), "build", "-o", binary, packagePath)
	cmd.Dir = repoRoot
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		t.Fatalf("build %s: %v\n%s", packagePath, err, out.String())
	}
	return binary
}

func startReleaseProcess(t *testing.T, repoRoot, binary string, env map[string]string) *releaseProcess {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	cmd := exec.CommandContext(ctx, binary)
	cmd.Dir = repoRoot
	cmd.Env = testprocess.EnvWith(env)
	process := &releaseProcess{cmd: cmd, done: make(chan struct{})}
	cmd.Stdout = &process.output
	cmd.Stderr = &process.output
	if err := cmd.Start(); err != nil {
		cancel()
		t.Fatalf("start %s: %v", filepath.Base(binary), err)
	}
	go func() {
		process.waitErr = cmd.Wait()
		close(process.done)
	}()
	t.Cleanup(func() {
		testprocess.Stop(t, cmd, process.done, cancel)
	})
	return process
}

func logReleaseProcessOnFailure(t *testing.T, service string, process *releaseProcess) {
	t.Helper()
	t.Cleanup(func() {
		if t.Failed() {
			t.Logf("%s process output:\n%s", service, process.output.String())
		}
	})
}

func loginReleaseUser(t *testing.T, businessBaseURL string) string {
	t.Helper()
	resp := postReleaseJSON(t, businessBaseURL+"/api/auth/login", "", "idem-release-full-login", map[string]any{
		"login_type": "personal",
		"account":    "user1001@dora.local",
		"password":   "local-user-change-me",
	})
	data, ok := resp["data"].(map[string]any)
	if !ok {
		t.Fatalf("login response missing data: %#v", resp)
	}
	token, _ := data["access_token"].(string)
	if token == "" {
		t.Fatalf("login response missing access_token: %#v", resp)
	}
	return token
}

func postReleaseAgentJSON(t *testing.T, agentBaseURL, token, path, idempotencyKey string, body any) map[string]any {
	t.Helper()
	return postReleaseJSON(t, agentBaseURL+path, token, idempotencyKey, body)
}

func getReleaseAgentJSON(t *testing.T, agentBaseURL, token, path string) map[string]any {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, agentBaseURL+path, nil)
	if err != nil {
		t.Fatalf("build GET %s: %v", path, err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-Trace-Id", "trace-release-full-http")
	req.Header.Set("X-Space-Id", "sp_personal_1001")
	return doReleaseJSON(t, req)
}

func postReleaseJSON(t *testing.T, url, token, idempotencyKey string, body any) map[string]any {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatalf("encode request body: %v", err)
		}
	}
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, url, &buf)
	if err != nil {
		t.Fatalf("build POST %s: %v", url, err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Trace-Id", "trace-release-full-http")
	req.Header.Set("X-Space-Id", "sp_personal_1001")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if idempotencyKey != "" {
		req.Header.Set("Idempotency-Key", idempotencyKey)
	}
	return doReleaseJSON(t, req)
}

func doReleaseJSON(t *testing.T, req *http.Request) map[string]any {
	t.Helper()
	client := http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", req.Method, req.URL, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("%s %s status=%d body=%s", req.Method, req.URL, resp.StatusCode, string(body))
	}
	var out map[string]any
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("decode %s %s response: %v body=%s", req.Method, req.URL, err, string(body))
	}
	return out
}

func stringField(t *testing.T, value map[string]any, field string) string {
	t.Helper()
	out, _ := value[field].(string)
	if out == "" {
		t.Fatalf("response missing %s: %#v", field, value)
	}
	return out
}

func assertReleaseEventTypes(t *testing.T, replay map[string]any, required ...string) {
	t.Helper()
	rawEvents, ok := replay["events"].([]any)
	if !ok {
		t.Fatalf("replay response missing events: %#v", replay)
	}
	seen := make(map[string]bool, len(rawEvents))
	for _, raw := range rawEvents {
		event, ok := raw.(map[string]any)
		if !ok {
			t.Fatalf("event is not object: %#v", raw)
		}
		if eventType, _ := event["type"].(string); eventType != "" {
			seen[eventType] = true
		}
	}
	var missing []string
	for _, eventType := range required {
		if !seen[eventType] {
			missing = append(missing, eventType)
		}
	}
	if len(missing) > 0 {
		t.Fatalf("missing event types %s in replay %#v", strings.Join(missing, ","), replay)
	}
}
