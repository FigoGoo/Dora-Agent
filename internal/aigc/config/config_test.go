package config

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestLoadFromEnvUsesDefaultsAndTrimsSecrets(t *testing.T) {
	t.Setenv(EnvDeepSeekAPIKey, "  test-deepseek-key  ")
	t.Setenv(EnvImage2APIKey, "  test-image-key  ")
	t.Setenv(EnvSeedanceAPIKey, "  test-seedance-key  ")
	t.Setenv(EnvBusinessDatabaseURL, "")
	t.Setenv(EnvAgentDatabaseURL, "")
	t.Setenv(EnvAgentGenerationRedisAddr, "")
	t.Setenv(EnvAgentGenerationRedisDB, "")
	t.Setenv(EnvAgentGenerationRedisListKey, "")
	t.Setenv(EnvAgentRuntimeRedisMode, "")
	t.Setenv(EnvAgentRuntimeRedisAddr, "")
	t.Setenv(EnvAgentRuntimeRedisDB, "")
	t.Setenv(EnvAgentRuntimeRedisStreamMaxLen, "")
	t.Setenv(EnvTOSEndpoint, "  tos-cn-beijing.volces.com  ")
	t.Setenv(EnvTOSBucket, " dora-public ")
	t.Setenv(EnvTOSAccessKeyID, "  tos-ak  ")
	t.Setenv(EnvTOSSecretAccessKey, "  tos-sk  ")
	t.Setenv(EnvTOSRegion, " cn-beijing ")
	t.Setenv(EnvTOSBaseURL, " https://tos.doraigc.com ")
	t.Setenv(EnvTOSRequestTimeout, "45s")
	t.Setenv(EnvTOSConnectTimeout, "7s")
	t.Setenv(EnvAgentHTTPAddr, "")

	cfg := LoadFromEnv()
	if cfg.DeepSeek.APIKey != "test-deepseek-key" {
		t.Fatalf("DeepSeek API key was not trimmed")
	}
	if cfg.DeepSeek.Model != DefaultDeepSeekModel {
		t.Fatalf("DeepSeek model = %q, want %q", cfg.DeepSeek.Model, DefaultDeepSeekModel)
	}
	if cfg.DeepSeek.BaseURL != DefaultDeepSeekBaseURL {
		t.Fatalf("DeepSeek base url = %q, want %q", cfg.DeepSeek.BaseURL, DefaultDeepSeekBaseURL)
	}
	if cfg.Image2.APIKey != "test-image-key" || cfg.Seedance.APIKey != "test-seedance-key" {
		t.Fatalf("provider keys were not loaded correctly: %#v", cfg)
	}
	if cfg.Storage.AgentDatabaseURL != DefaultAgentDatabaseURL {
		t.Fatalf("agent database url = %q, want %q", cfg.Storage.AgentDatabaseURL, DefaultAgentDatabaseURL)
	}
	if cfg.Storage.BusinessDatabaseURL != DefaultBusinessDatabaseURL {
		t.Fatalf("business database url = %q, want %q", cfg.Storage.BusinessDatabaseURL, DefaultBusinessDatabaseURL)
	}
	if cfg.Storage.GenerationRedis.Addr != DefaultAgentGenerationRedisAddr || cfg.Storage.GenerationRedis.ListKey != DefaultAgentGenerationRedisListKey {
		t.Fatalf("generation redis defaults not loaded: %#v", cfg.Storage.GenerationRedis)
	}
	if cfg.Storage.RuntimeRedis.Mode != DefaultAgentRuntimeRedisMode || cfg.Storage.RuntimeRedis.StreamMaxLen != DefaultAgentRuntimeRedisStreamMaxLen {
		t.Fatalf("runtime redis defaults not loaded: %#v", cfg.Storage.RuntimeRedis)
	}
	if cfg.Storage.TOS.Endpoint != "tos-cn-beijing.volces.com" || cfg.Storage.TOS.Bucket != "dora-public" {
		t.Fatalf("tos endpoint/bucket not loaded: %#v", cfg.Storage.TOS)
	}
	if cfg.Storage.TOS.AccessKeyID != "tos-ak" || cfg.Storage.TOS.SecretAccessKey != "tos-sk" {
		t.Fatalf("tos credentials not loaded: %#v", cfg.Storage.TOS)
	}
	if cfg.Storage.TOS.Region != "cn-beijing" || cfg.Storage.TOS.BaseURL != "https://tos.doraigc.com" {
		t.Fatalf("tos region/base url not loaded: %#v", cfg.Storage.TOS)
	}
	if cfg.Storage.TOS.RequestTimeout != 45*time.Second || cfg.Storage.TOS.ConnectTimeout != 7*time.Second {
		t.Fatalf("tos timeouts not loaded: %#v", cfg.Storage.TOS)
	}
	if cfg.Runtime.HTTPAddr != DefaultAgentHTTPAddr {
		t.Fatalf("http addr = %q, want %q", cfg.Runtime.HTTPAddr, DefaultAgentHTTPAddr)
	}
}

func TestValidateDeepSeekRequiresAPIKey(t *testing.T) {
	err := (Config{}).ValidateDeepSeek()
	if !errors.Is(err, ErrMissingDeepSeekAPIKey) {
		t.Fatalf("ValidateDeepSeek() error = %v, want ErrMissingDeepSeekAPIKey", err)
	}
}

func TestSanitizedConfigDoesNotExposeSecrets(t *testing.T) {
	cfg := Config{
		DeepSeek: DeepSeekConfig{APIKey: "test-deepseek-key"},
		Image2:   ProviderConfig{APIKey: "test-image-key"},
		Seedance: ProviderConfig{APIKey: "test-seedance-key"},
		Storage: StorageConfig{
			BusinessDatabaseURL: "postgres://user:secret@127.0.0.1:5432/business?sslmode=disable",
			AgentDatabaseURL:    "postgres://user:secret@127.0.0.1:5432/agent?sslmode=disable",
			GenerationRedis: GenerationRedisConfig{
				RedisConfig: RedisConfig{Addr: "127.0.0.1:6379", Password: "redis-secret", DB: 0},
				ListKey:     "dora:agent:generation_jobs",
			},
			RuntimeRedis: RuntimeRedisConfig{
				RedisConfig:  RedisConfig{Addr: "127.0.0.1:6379", Password: "runtime-redis-secret", DB: 0},
				Mode:         "redis",
				StreamMaxLen: 1000,
			},
			TOS: TOSConfig{
				Endpoint:        "tos-cn-beijing.volces.com",
				Bucket:          "dora-public",
				AccessKeyID:     "tos-ak",
				SecretAccessKey: "tos-sk",
				Region:          "cn-beijing",
				BaseURL:         "https://tos.doraigc.com",
				RequestTimeout:  30 * time.Second,
				ConnectTimeout:  10 * time.Second,
			},
		},
	}

	out, err := json.Marshal(cfg.Sanitized())
	if err != nil {
		t.Fatalf("Marshal sanitized config: %v", err)
	}
	text := string(out)
	for _, secret := range []string{"test-deepseek-key", "test-image-key", "test-seedance-key", "user:secret@", "redis-secret", "runtime-redis-secret", "tos-ak", "tos-sk"} {
		if strings.Contains(text, secret) {
			t.Fatalf("sanitized config leaked secret %q: %s", secret, text)
		}
	}
	for _, envName := range []string{EnvDeepSeekAPIKey, EnvImage2APIKey, EnvSeedanceAPIKey, EnvAgentDatabaseURL, EnvBusinessDatabaseURL, EnvAgentGenerationRedisPassword, EnvAgentRuntimeRedisPassword, EnvTOSAccessKeyID, EnvTOSSecretAccessKey} {
		if !strings.Contains(text, envName) {
			t.Fatalf("sanitized config did not include env hint %q: %s", envName, text)
		}
	}
	if !strings.Contains(text, "tos-cn-beijing.volces.com") || !strings.Contains(text, "https://tos.doraigc.com") {
		t.Fatalf("sanitized config should include non-secret tos settings: %s", text)
	}
}

func TestLoadFromEnvUsesBranchRedisOverrides(t *testing.T) {
	t.Setenv(EnvAgentGenerationRedisAddr, "127.0.0.1:6380")
	t.Setenv(EnvAgentGenerationRedisPassword, " local-redis ")
	t.Setenv(EnvAgentGenerationRedisDB, "2")
	t.Setenv(EnvAgentGenerationRedisListKey, "dora:test:generation_jobs")
	t.Setenv(EnvAgentRuntimeRedisMode, "REDIS")
	t.Setenv(EnvAgentRuntimeRedisAddr, "127.0.0.1:6381")
	t.Setenv(EnvAgentRuntimeRedisDB, "3")
	t.Setenv(EnvAgentRuntimeRedisStreamMaxLen, "2048")

	cfg := LoadFromEnv().Normalize()
	if cfg.Storage.GenerationRedis.Addr != "127.0.0.1:6380" || cfg.Storage.GenerationRedis.Password != "local-redis" || cfg.Storage.GenerationRedis.DB != 2 {
		t.Fatalf("generation redis override not loaded: %#v", cfg.Storage.GenerationRedis)
	}
	if cfg.Storage.GenerationRedis.ListKey != "dora:test:generation_jobs" {
		t.Fatalf("generation redis list key = %q", cfg.Storage.GenerationRedis.ListKey)
	}
	if cfg.Storage.RuntimeRedis.Mode != "redis" || cfg.Storage.RuntimeRedis.Addr != "127.0.0.1:6381" || cfg.Storage.RuntimeRedis.DB != 3 {
		t.Fatalf("runtime redis override not loaded: %#v", cfg.Storage.RuntimeRedis)
	}
	if cfg.Storage.RuntimeRedis.StreamMaxLen != 2048 {
		t.Fatalf("runtime redis stream max len = %d", cfg.Storage.RuntimeRedis.StreamMaxLen)
	}
}

func TestLoadFromEnvUsesHTTPAddrOverride(t *testing.T) {
	t.Setenv(EnvAgentHTTPAddr, " :19090 ")

	cfg := LoadFromEnv().Normalize()
	if cfg.Runtime.HTTPAddr != ":19090" {
		t.Fatalf("http addr = %q", cfg.Runtime.HTTPAddr)
	}
}
