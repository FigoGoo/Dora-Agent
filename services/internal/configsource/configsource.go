package configsource

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/FigoGoo/Dora-Agent/services/internal/envconfig"
	clientv3 "go.etcd.io/etcd/client/v3"
)

const (
	sourceKey          = "DORA_CONFIG_SOURCE"
	sourceEnv          = "env"
	sourceEtcd         = "etcd"
	defaultEtcdTimeout = 2 * time.Second
)

type EtcdOptions struct {
	Values      envconfig.Values
	ServiceName string
	AllowedKeys []string
}

type EtcdLoader func(context.Context, EtcdOptions) (envconfig.Values, error)

type LayeredOptions struct {
	ServiceNameKey     string
	DefaultServiceName string
	AllowedKeys        []string
	EtcdLoader         EtcdLoader
}

func LoadLayered(ctx context.Context, paths []string, opts LayeredOptions) (envconfig.Values, error) {
	values, err := envconfig.LoadFiles(paths...)
	if err != nil {
		return nil, err
	}

	probe := values.Clone()
	probe.OverlayEnv()
	serviceName := strings.TrimSpace(probe.String(opts.ServiceNameKey, opts.DefaultServiceName))

	loader := opts.EtcdLoader
	if loader == nil {
		loader = LoadEtcd
	}
	etcdValues, err := loader(ctx, EtcdOptions{
		Values:      probe,
		ServiceName: serviceName,
		AllowedKeys: opts.AllowedKeys,
	})
	if err != nil {
		return nil, err
	}
	values.Overlay(etcdValues)
	values.OverlayEnv()
	return values, nil
}

func LoadEtcd(ctx context.Context, opts EtcdOptions) (envconfig.Values, error) {
	return loadEtcdWithFactory(ctx, opts, newEtcdClient)
}

type etcdKVClient interface {
	Get(ctx context.Context, key string, opts ...clientv3.OpOption) (*clientv3.GetResponse, error)
	Close() error
}

type etcdClientFactory func([]string, time.Duration) (etcdKVClient, error)

func loadEtcdWithFactory(ctx context.Context, opts EtcdOptions, factory etcdClientFactory) (envconfig.Values, error) {
	mode := strings.ToLower(strings.TrimSpace(opts.Values.String(sourceKey, sourceEnv)))
	if mode == "" || mode == sourceEnv || mode == "none" {
		return envconfig.Values{}, nil
	}
	if mode != sourceEtcd {
		return nil, fmt.Errorf("%s must be env or etcd", sourceKey)
	}

	endpoints := opts.Values.CSV("ETCD_ENDPOINTS")
	if len(endpoints) == 0 {
		return nil, fmt.Errorf("ETCD_ENDPOINTS is required when %s=etcd", sourceKey)
	}
	if strings.TrimSpace(opts.ServiceName) == "" {
		return nil, fmt.Errorf("service name is required when %s=etcd", sourceKey)
	}

	timeout, err := opts.Values.Duration("DORA_CONFIG_ETCD_TIMEOUT", defaultEtcdTimeout)
	if err != nil {
		return nil, err
	}
	client, err := factory(endpoints, timeout)
	if err != nil {
		return nil, fmt.Errorf("create etcd config client: %w", err)
	}
	defer client.Close()

	prefix := servicePrefix(opts.Values, opts.ServiceName)
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	resp, err := client.Get(ctx, prefix+"/", clientv3.WithPrefix())
	if err != nil {
		return nil, fmt.Errorf("load etcd config %s/: %w", prefix, err)
	}
	return valuesFromKVs(resp, prefix, opts.AllowedKeys)
}

func newEtcdClient(endpoints []string, timeout time.Duration) (etcdKVClient, error) {
	return clientv3.New(clientv3.Config{
		Endpoints:   endpoints,
		DialTimeout: timeout,
	})
}

func servicePrefix(values envconfig.Values, serviceName string) string {
	namespace := strings.TrimSpace(values.String("ETCD_NAMESPACE", ""))
	if namespace == "" {
		namespace = "/dora/" + strings.Trim(strings.TrimSpace(values.String("APP_ENV", "local")), "/")
	}
	return "/" + strings.Trim(strings.TrimSpace(namespace), "/") + "/" + strings.Trim(strings.TrimSpace(serviceName), "/")
}

func valuesFromKVs(resp *clientv3.GetResponse, prefix string, allowedKeys []string) (envconfig.Values, error) {
	allowed := make(map[string]struct{}, len(allowedKeys))
	for _, key := range allowedKeys {
		allowed[key] = struct{}{}
	}

	searchPrefix := prefix + "/"
	values := envconfig.Values{}
	for _, kv := range resp.Kvs {
		key := strings.TrimPrefix(string(kv.Key), searchPrefix)
		if key == "" || strings.Contains(key, "/") {
			return nil, fmt.Errorf("etcd config key %q must be a direct child of %s", string(kv.Key), prefix)
		}
		if _, ok := allowed[key]; !ok {
			return nil, fmt.Errorf("etcd config key %s is not allowed for non-sensitive config", key)
		}
		values[key] = strings.TrimSpace(string(kv.Value))
	}
	return values, nil
}
