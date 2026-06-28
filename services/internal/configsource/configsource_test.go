package configsource

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/FigoGoo/Dora-Agent/services/internal/envconfig"
	"go.etcd.io/etcd/api/v3/mvccpb"
	clientv3 "go.etcd.io/etcd/client/v3"
)

func TestLoadLayeredAppliesEtcdBeforeEnvironment(t *testing.T) {
	unsetConfigSourceEnv(t)
	dir := t.TempDir()
	envFile := filepath.Join(dir, ".env.example")
	writeEnv(t, envFile, `
DORA_CONFIG_SOURCE=etcd
APP_ENV=local
ETCD_NAMESPACE=/dora/local
ETCD_ENDPOINTS=http://127.0.0.1:2379
BUSINESS_SERVICE_NAME=dora.business
LOG_LEVEL=info
`)
	t.Setenv("LOG_LEVEL", "error")

	values, err := LoadLayered(context.Background(), []string{envFile}, LayeredOptions{
		ServiceNameKey: "BUSINESS_SERVICE_NAME",
		AllowedKeys:    []string{"LOG_LEVEL"},
		EtcdLoader: func(_ context.Context, opts EtcdOptions) (envconfig.Values, error) {
			if opts.ServiceName != "dora.business" {
				t.Fatalf("unexpected service name: %s", opts.ServiceName)
			}
			if got := opts.Values.String("LOG_LEVEL", ""); got != "error" {
				t.Fatalf("expected probe to include environment override, got %s", got)
			}
			return envconfig.Values{"LOG_LEVEL": "debug"}, nil
		},
	})
	if err != nil {
		t.Fatalf("load layered config: %v", err)
	}
	if got := values.String("LOG_LEVEL", ""); got != "error" {
		t.Fatalf("expected env to override etcd, got %s", got)
	}
}

func TestLoadEtcdRejectsDisallowedKeys(t *testing.T) {
	values := envconfig.Values{
		"DORA_CONFIG_SOURCE": "etcd",
		"ETCD_ENDPOINTS":     "http://127.0.0.1:2379",
		"ETCD_NAMESPACE":     "/dora/local",
	}
	client := &fakeEtcdClient{resp: &clientv3.GetResponse{Kvs: []*mvccpb.KeyValue{
		{Key: []byte("/dora/local/dora.business/LOG_LEVEL"), Value: []byte("debug")},
		{Key: []byte("/dora/local/dora.business/TOS_SECRET_ACCESS_KEY"), Value: []byte("secret")},
	}}}

	_, err := loadEtcdWithFactory(context.Background(), EtcdOptions{
		Values:      values,
		ServiceName: "dora.business",
		AllowedKeys: []string{"LOG_LEVEL"},
	}, func([]string, time.Duration) (etcdKVClient, error) {
		return client, nil
	})
	if err == nil {
		t.Fatal("expected disallowed key error")
	}
	if !client.closed {
		t.Fatal("expected etcd client to close")
	}
}

func TestLoadEtcdValidatesRequiredInputs(t *testing.T) {
	_, err := loadEtcdWithFactory(context.Background(), EtcdOptions{
		Values: envconfig.Values{"DORA_CONFIG_SOURCE": "etcd"},
	}, func([]string, time.Duration) (etcdKVClient, error) {
		return nil, errors.New("should not create client")
	})
	if err == nil {
		t.Fatal("expected missing endpoint error")
	}
}

type fakeEtcdClient struct {
	resp   *clientv3.GetResponse
	err    error
	closed bool
}

func (f *fakeEtcdClient) Get(context.Context, string, ...clientv3.OpOption) (*clientv3.GetResponse, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.resp, nil
}

func (f *fakeEtcdClient) Close() error {
	f.closed = true
	return nil
}

func unsetConfigSourceEnv(t *testing.T) {
	t.Helper()
	for _, key := range []string{
		"DORA_CONFIG_SOURCE", "DORA_CONFIG_ETCD_TIMEOUT", "APP_ENV", "ETCD_ENDPOINTS", "ETCD_NAMESPACE",
		"BUSINESS_SERVICE_NAME", "LOG_LEVEL",
	} {
		key := key
		old, ok := os.LookupEnv(key)
		_ = os.Unsetenv(key)
		t.Cleanup(func() {
			if ok {
				_ = os.Setenv(key, old)
			} else {
				_ = os.Unsetenv(key)
			}
		})
	}
}

func writeEnv(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write env file: %v", err)
	}
}
