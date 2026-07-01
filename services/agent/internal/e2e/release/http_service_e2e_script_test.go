package release_test

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/FigoGoo/Dora-Agent/internal/testdb"
	"github.com/FigoGoo/Dora-Agent/internal/testprocess"
	"github.com/FigoGoo/Dora-Agent/internal/testredis"
)

func TestReleaseHTTPServiceE2EScript(t *testing.T) {
	businessDB := testdb.StartPostgres(t, "dora_release_script_business_http")
	businessMigrator := testdb.ApplyMigrations(t, businessDB.URL, "db/migrations/iterations/2026-06-27-business-core/business")
	t.Cleanup(func() { testdb.DownMigrations(t, businessMigrator) })
	testdb.ExecSQL(t, businessDB.DB, testdb.MustReadSQL(t, "tests/business/seed/business_core_seed.sql"))

	agentDB := testdb.StartPostgres(t, "dora_release_script_agent_http")
	agentMigrator := testdb.ApplyMigrations(t, agentDB.URL, "db/migrations/iterations/20260627_agent_runtime/agent")
	t.Cleanup(func() { testdb.DownMigrations(t, agentMigrator) })
	redisServer := testredis.Start(t)

	repoRoot := testdb.RepoRoot(t)
	businessBinary := buildReleaseBinary(t, repoRoot, "dora-business-http-service-e2e", "./services/business/cmd/business")
	agentBinary := buildReleaseBinary(t, repoRoot, "dora-agent-http-service-e2e", "./services/agent/cmd/agent")

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

	reportPath := os.Getenv("RELEASE_HTTP_E2E_REPORT_PATH")
	if reportPath == "" {
		reportPath = filepath.Join(t.TempDir(), "release-http-service-e2e-report.md")
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "scripts/validate-release-http-service-e2e.sh")
	cmd.Dir = repoRoot
	cmd.Env = testprocess.EnvWith(map[string]string{
		"RELEASE_BUSINESS_BASE_URL":    "http://" + businessHTTPAddr,
		"RELEASE_AGENT_BASE_URL":       "http://" + agentHTTPAddr,
		"RELEASE_HTTP_E2E_REPORT_PATH": reportPath,
	})
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	if err := cmd.Run(); err != nil {
		t.Fatalf("release HTTP service E2E script failed: %v\n%s", err, output.String())
	}

	reportFile := reportPath
	if !filepath.IsAbs(reportFile) {
		reportFile = filepath.Join(repoRoot, reportFile)
	}
	report, err := os.ReadFile(reportFile)
	if err != nil {
		t.Fatalf("read release HTTP service E2E report: %v", err)
	}
	if !strings.Contains(string(report), "status: passed") {
		t.Fatalf("release HTTP service E2E report must be passed:\n%s", string(report))
	}
}
