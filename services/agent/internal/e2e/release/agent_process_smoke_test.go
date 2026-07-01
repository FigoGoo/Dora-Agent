package release_test

import (
	"bytes"
	"context"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/FigoGoo/Dora-Agent/internal/testdb"
	"github.com/FigoGoo/Dora-Agent/internal/testprocess"
	"github.com/FigoGoo/Dora-Agent/internal/testredis"
)

func TestReleaseAgentIndependentProcessHTTPSmoke(t *testing.T) {
	db := testdb.StartPostgres(t, "dora_release_agent_process")
	migrator := testdb.ApplyMigrations(t, db.URL, "db/migrations/iterations/2026-07-01-agent-runtime-contracts/agent")
	t.Cleanup(func() { testdb.DownMigrations(t, migrator) })
	redisServer := testredis.Start(t)

	repoRoot := testdb.RepoRoot(t)
	binary := filepath.Join(t.TempDir(), "dora-agent-smoke")
	buildCtx, buildCancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer buildCancel()
	buildCmd := exec.CommandContext(buildCtx, testprocess.GoBinary(), "build", "-o", binary, "./services/agent/cmd/agent")
	buildCmd.Dir = repoRoot
	var buildOut bytes.Buffer
	buildCmd.Stdout = &buildOut
	buildCmd.Stderr = &buildOut
	if err := buildCmd.Run(); err != nil {
		t.Fatalf("build agent binary: %v\n%s", err, buildOut.String())
	}

	addr := testprocess.FreeLocalAddr(t)
	runCtx, runCancel := context.WithCancel(t.Context())
	defer runCancel()
	cmd := exec.CommandContext(runCtx, binary)
	cmd.Dir = repoRoot
	cmd.Env = testprocess.EnvWith(map[string]string{
		"DORA_CONFIG_SOURCE":                    "env",
		"APP_ENV":                               "test",
		"LOG_LEVEL":                             "debug",
		"AGENT_DATABASE_URL":                    db.URL,
		"AGENT_HTTP_ADDR":                       addr,
		"AGENT_SERVICE_NAME":                    "dora.agent",
		"BUSINESS_SERVICE_NAME":                 "dora.business",
		"BUSINESS_HOSTPORTS":                    "127.0.0.1:1",
		"KITEX_REGISTRY":                        "none",
		"KITEX_TIMEOUT_MS":                      "300",
		"AGENT_MODEL_ADAPTER":                   "local",
		"AGENT_GENERATION_QUEUE":                "inline",
		"AGENT_RUNTIME_REDIS_MODE":              "redis",
		"AGENT_RUNTIME_REDIS_ADDR":              redisServer.Addr,
		"AGENT_RUNTIME_REDIS_DB":                "0",
		"AGENT_RUNTIME_REDIS_STREAM_MAX_LEN":    "256",
		"AGENT_GENERATION_RECOVERY_STALE_AFTER": "30s",
		"DEEPSEEK_API_KEY":                      "",
	})
	var processOut bytes.Buffer
	cmd.Stdout = &processOut
	cmd.Stderr = &processOut
	if err := cmd.Start(); err != nil {
		t.Fatalf("start agent process: %v", err)
	}
	var waitErr error
	done := make(chan struct{})
	go func() {
		waitErr = cmd.Wait()
		close(done)
	}()
	t.Cleanup(func() {
		testprocess.Stop(t, cmd, done, runCancel)
	})

	testprocess.AssertEndpointOK(t, "agent", done, &waitErr, &processOut, "http://"+addr+"/healthz")
	testprocess.AssertEndpointOK(t, "agent", done, &waitErr, &processOut, "http://"+addr+"/readyz")
}
