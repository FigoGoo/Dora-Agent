package config

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/FigoGoo/Dora-Agent/services/internal/configsource"
	"github.com/FigoGoo/Dora-Agent/services/internal/envconfig"
)

func TestLoadFromBusinessConfig(t *testing.T) {
	unsetBusinessEnv(t)
	dir := t.TempDir()
	example := filepath.Join(dir, ".env.example")
	local := filepath.Join(dir, ".env.local")
	writeEnv(t, example, `
APP_ENV=local
LOG_LEVEL=info
BUSINESS_DATABASE_URL=postgres://business
BUSINESS_SERVICE_NAME=dora.business
BUSINESS_KITEX_PORT=19001
BUSINESS_HTTP_ENABLED=true
BUSINESS_HTTP_ADDR=0.0.0.0:19080
KITEX_REGISTRY=none
KITEX_TIMEOUT_MS=3000
TOS_ENDPOINT=tos-cn-beijing.volces.com
TOS_BUCKET=dora-public
TOS_ACCESS_KEY_ID=public-key
TOS_SECRET_ACCESS_KEY=secret-value
TOS_REGION=cn-beijing
TOS_BASE_URL=https://tos.doraigc.com
TOS_REQUEST_TIMEOUT=30s
TOS_CONNECT_TIMEOUT=10s
VOLC_TLS_ENDPOINT=https://tls.example
VOLC_TLS_REGION=cn-beijing
VOLC_TLS_ACCESS_KEY_ID=tls-key
VOLC_TLS_ACCESS_KEY_SECRET=tls-secret
VOLC_TLS_PROJECT_ID=project
VOLC_TLS_TOPIC_ID=topic
SECRET_ENCRYPTION_KEY_REF=local-ref
CORS_ALLOWED_ORIGINS=http://localhost:3000
`)
	writeEnv(t, local, `
LOG_LEVEL=debug
BUSINESS_HTTP_ADDR=127.0.0.1:29080
`)

	cfg, err := LoadFrom(example, local)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if cfg.KitexAddr != ":19001" || cfg.HTTPAddr != "127.0.0.1:29080" {
		t.Fatalf("unexpected addrs: kitex=%s http=%s", cfg.KitexAddr, cfg.HTTPAddr)
	}
	if cfg.TOS.RequestTimeout != 30*time.Second || cfg.TOS.ConnectTimeout != 10*time.Second {
		t.Fatalf("unexpected tos timeouts: %#v", cfg.TOS)
	}
	if cfg.TOS.SecretAccessKey != "secret-value" || cfg.TLS.AccessKeySecret != "tls-secret" {
		t.Fatalf("expected secret fields to load for runtime use")
	}
}

func TestLoadFromBusinessConfigRequiredAndInvalid(t *testing.T) {
	unsetBusinessEnv(t)
	dir := t.TempDir()
	example := filepath.Join(dir, ".env.example")
	writeEnv(t, example, "BUSINESS_SERVICE_NAME=dora.business\nBUSINESS_HTTP_ENABLED=false\n")
	if _, err := LoadFrom(example); err == nil {
		t.Fatal("expected missing database url error")
	}

	writeEnv(t, example, `
BUSINESS_DATABASE_URL=postgres://business
BUSINESS_SERVICE_NAME=dora.business
BUSINESS_KITEX_PORT=bad
BUSINESS_HTTP_ENABLED=false
`)
	if _, err := LoadFrom(example); err == nil {
		t.Fatal("expected invalid port error")
	}

	writeEnv(t, example, `
BUSINESS_DATABASE_URL=postgres://business
BUSINESS_SERVICE_NAME=dora.business
BUSINESS_KITEX_PORT=19001
BUSINESS_HTTP_ENABLED=maybe
`)
	if _, err := LoadFrom(example); err == nil {
		t.Fatal("expected invalid bool error")
	}
}

func TestLoadFromBusinessConfigEtcdOverlayAndEnvFinal(t *testing.T) {
	unsetBusinessEnv(t)
	dir := t.TempDir()
	example := filepath.Join(dir, ".env.example")
	writeEnv(t, example, `
DORA_CONFIG_SOURCE=etcd
APP_ENV=local
LOG_LEVEL=info
BUSINESS_DATABASE_URL=postgres://business
BUSINESS_SERVICE_NAME=dora.business
BUSINESS_HTTP_ENABLED=true
BUSINESS_HTTP_ADDR=0.0.0.0:19080
ETCD_ENDPOINTS=http://127.0.0.1:2379
ETCD_NAMESPACE=/dora/local
`)
	t.Setenv("LOG_LEVEL", "error")

	cfg, err := loadFromWithEtcdLoader([]string{example}, func(_ context.Context, opts configsource.EtcdOptions) (envconfig.Values, error) {
		if opts.ServiceName != "dora.business" {
			t.Fatalf("unexpected service name: %s", opts.ServiceName)
		}
		if contains(opts.AllowedKeys, "TOS_SECRET_ACCESS_KEY") || contains(opts.AllowedKeys, "BUSINESS_DATABASE_URL") {
			t.Fatal("sensitive business config must not be allowed from etcd")
		}
		return envconfig.Values{
			"LOG_LEVEL":           "debug",
			"PUBLIC_WEB_BASE_URL": "https://console.example",
			"TOS_BUCKET":          "dora-public",
		}, nil
	})
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if cfg.LogLevel != "error" {
		t.Fatalf("expected env to override etcd log level, got %s", cfg.LogLevel)
	}
	if cfg.PublicWebBaseURL != "https://console.example" || cfg.TOS.Bucket != "dora-public" {
		t.Fatalf("expected etcd non-sensitive config overlay, got url=%s bucket=%s", cfg.PublicWebBaseURL, cfg.TOS.Bucket)
	}
}

func unsetBusinessEnv(t *testing.T) {
	t.Helper()
	keys := []string{
		"DORA_CONFIG_SOURCE", "DORA_CONFIG_ETCD_TIMEOUT",
		"APP_ENV", "APP_NAME", "LOG_LEVEL", "BUSINESS_DATABASE_URL", "BUSINESS_KITEX_PORT",
		"BUSINESS_HTTP_ENABLED", "BUSINESS_HTTP_ADDR", "PUBLIC_WEB_BASE_URL", "BUSINESS_SERVICE_NAME",
		"KITEX_REGISTRY", "KITEX_TIMEOUT_MS", "ETCD_ENDPOINTS", "ETCD_NAMESPACE",
		"ADMIN_BOOTSTRAP_ACCOUNT", "ADMIN_BOOTSTRAP_PASSWORD_HASH", "ADMIN_BOOTSTRAP_CREDENTIAL_SECRET_REF",
		"TOS_ENDPOINT", "TOS_BUCKET", "TOS_ACCESS_KEY_ID", "TOS_SECRET_ACCESS_KEY", "TOS_REGION", "TOS_BASE_URL",
		"TOS_REQUEST_TIMEOUT", "TOS_CONNECT_TIMEOUT", "VOLC_TLS_ENDPOINT", "VOLC_TLS_REGION",
		"VOLC_TLS_ACCESS_KEY_ID", "VOLC_TLS_ACCESS_KEY_SECRET", "VOLC_TLS_PROJECT_ID", "VOLC_TLS_TOPIC_ID",
		"SECRET_ENCRYPTION_KEY_REF", "CORS_ALLOWED_ORIGINS",
	}
	for _, key := range keys {
		old, ok := os.LookupEnv(key)
		_ = os.Unsetenv(key)
		key := key
		t.Cleanup(func() {
			if ok {
				_ = os.Setenv(key, old)
			} else {
				_ = os.Unsetenv(key)
			}
		})
	}
}

func contains(items []string, needle string) bool {
	for _, item := range items {
		if item == needle {
			return true
		}
	}
	return false
}

func writeEnv(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write env file: %v", err)
	}
}
