package pr5e2e_test

import (
	"bytes"
	"context"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/FigoGoo/Dora-Agent/internal/testdb"
	"github.com/FigoGoo/Dora-Agent/internal/testprocess"
)

func TestPR5BusinessIndependentProcessHTTPSmoke(t *testing.T) {
	db := testdb.StartPostgres(t, "dora_pr5_business_process")
	migrator := testdb.ApplyMigrations(t, db.URL, "db/migrations/iterations/2026-06-27-business-core/business")
	t.Cleanup(func() { testdb.DownMigrations(t, migrator) })

	repoRoot := testdb.RepoRoot(t)
	binary := filepath.Join(t.TempDir(), "dora-business-smoke")
	buildCtx, buildCancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer buildCancel()
	buildCmd := exec.CommandContext(buildCtx, testprocess.GoBinary(), "build", "-o", binary, "./services/business/cmd/business")
	buildCmd.Dir = repoRoot
	var buildOut bytes.Buffer
	buildCmd.Stdout = &buildOut
	buildCmd.Stderr = &buildOut
	if err := buildCmd.Run(); err != nil {
		t.Fatalf("build business binary: %v\n%s", err, buildOut.String())
	}

	httpAddr := testprocess.FreeLocalAddr(t)
	httpPort := testprocess.LocalPort(t, httpAddr)
	kitexPort := testprocess.LocalPort(t, testprocess.FreeLocalAddr(t))
	runCtx, runCancel := context.WithCancel(t.Context())
	defer runCancel()
	cmd := exec.CommandContext(runCtx, binary)
	cmd.Dir = repoRoot
	cmd.Env = testprocess.EnvWith(map[string]string{
		"DORA_CONFIG_SOURCE":                    "env",
		"APP_ENV":                               "test",
		"LOG_LEVEL":                             "debug",
		"BUSINESS_DATABASE_URL":                 db.URL,
		"BUSINESS_SERVICE_NAME":                 "dora.business",
		"BUSINESS_KITEX_PORT":                   kitexPort,
		"KITEX_REGISTRY":                        "none",
		"KITEX_TIMEOUT_MS":                      "300",
		"BUSINESS_HTTP_ENABLED":                 "true",
		"BUSINESS_HTTP_PORT":                    httpPort,
		"BUSINESS_HTTP_ADDR":                    httpAddr,
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
	var processOut bytes.Buffer
	cmd.Stdout = &processOut
	cmd.Stderr = &processOut
	if err := cmd.Start(); err != nil {
		t.Fatalf("start business process: %v", err)
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

	testprocess.AssertEndpointOK(t, "business", done, &waitErr, &processOut, "http://"+httpAddr+"/healthz")
	testprocess.AssertEndpointOK(t, "business", done, &waitErr, &processOut, "http://"+httpAddr+"/readyz")
}
