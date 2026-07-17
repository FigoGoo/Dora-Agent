package businessrpc

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/FigoGoo/Dora-Agent/agent/internal/config"
	"github.com/FigoGoo/Dora-Agent/agent/internal/graphtool/analyzematerials"
)

const materialAnalysisRPCIntegrationEnv = "DORA_MATERIAL_ANALYSIS_RPC_INTEGRATION"

// TestMaterialAnalysisPreviewRPCIntegration 通过真实 etcd 发现和 Kitex 调用验证 BIZ-PREVIEW-004 已接入运行中的 Business。
// 测试使用不存在的规范 UUIDv7，只断言统一 NOT_FOUND 映射，不写入任何业务或 Evidence 数据。
func TestMaterialAnalysisPreviewRPCIntegration(t *testing.T) {
	if os.Getenv(materialAnalysisRPCIntegrationEnv) != "1" {
		t.Skipf("set %s=1 to run the local Foundation RPC integration test", materialAnalysisRPCIntegrationEnv)
	}
	endpoint := os.Getenv("DORA_MATERIAL_ANALYSIS_RPC_ETCD_ENDPOINT")
	if endpoint == "" {
		endpoint = "127.0.0.1:12379"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	client, err := NewClient(ctx,
		config.BusinessRPCConfig{
			ConnectTimeout: time.Second, RequestTimeout: 3 * time.Second,
			StartupTimeout: 5 * time.Second, ProbeInterval: 100 * time.Millisecond,
		},
		config.EtcdConfig{Endpoints: []string{endpoint}, DialTimeout: 2 * time.Second, LeaseTTL: 15 * time.Second},
		config.ServiceConfig{Name: "dora-agent-material-analysis-integration", Version: "test", Environment: "local", InstanceID: "material-analysis-integration"},
		false, false, false,
	)
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })

	_, err = client.BatchGetAssetAnalysisInputs(ctx, analyzematerials.EvidenceQuery{
		UserID: "019f6beb-7001-7000-8000-000000000001", ProjectID: "019f6beb-7002-7000-8000-000000000002",
		Targets: []analyzematerials.AssetTarget{{AssetID: "019f6beb-7003-7000-8000-000000000003"}},
	})
	if got := analyzematerials.ErrorResultCode(err); got != analyzematerials.ResultCodeMaterialsNotAvailable {
		t.Fatalf("BatchGetAssetAnalysisInputs() code=%q error=%v want=%q", got, err, analyzematerials.ResultCodeMaterialsNotAvailable)
	}
	if err == nil || err.Error() != analyzematerials.ErrorSummary(err) {
		t.Fatalf("integration error leaked unsafe detail: %v", err)
	}
}
