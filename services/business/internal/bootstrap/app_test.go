package bootstrap

import (
	"testing"
	"time"

	"github.com/FigoGoo/Dora-Agent/services/business/internal/infra/config"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/transport/rpc"
)

func TestNewKitexServerWithNoneRegistry(t *testing.T) {
	cfg := config.BusinessConfig{
		ServiceName:   "dora.business",
		KitexAddr:     "127.0.0.1:0",
		KitexRegistry: "none",
		KitexTimeout:  3 * time.Second,
	}
	svr, err := NewKitexServer(cfg, rpc.NewUnimplementedHandler())
	if err != nil {
		t.Fatalf("new kitex server: %v", err)
	}
	registered := svr.GetServiceInfos()
	expected := []string{
		"AccountSpaceService",
		"EnterpriseService",
		"AdminService",
		"UserAdminService",
		"ProjectService",
		"ProjectAssetService",
		"AssetService",
		"CreditService",
		"AssetCreditCommitService",
		"SkillCatalogService",
		"ToolCapabilityService",
		"ModelConfigService",
		"PlatformDictionaryService",
		"WorkService",
		"WorkShareService",
		"FeaturedWorkAdminService",
		"PublicContentService",
		"NotificationService",
		"BusinessSkillMarketplaceService",
	}
	if got := len(registered); got != len(expected) {
		t.Fatalf("expected %d services, got %d", len(expected), got)
	}
	for _, serviceName := range expected {
		if _, ok := registered[serviceName]; !ok {
			t.Fatalf("expected registered service %s", serviceName)
		}
	}
}
