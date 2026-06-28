package rpc

import (
	stderrors "errors"
	"testing"

	"github.com/FigoGoo/Dora-Agent/kitex_gen/dora/api/businessagent"
	bizerrors "github.com/FigoGoo/Dora-Agent/services/business/internal/pkg/errors"
	"github.com/cloudwego/kitex/server"
)

func TestRegisterAllBusinessServices(t *testing.T) {
	svr := server.NewServer()
	if err := RegisterAll(svr, NewUnimplementedHandler()); err != nil {
		t.Fatalf("register services: %v", err)
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

func TestUnimplementedHandlerReturnsBusinessError(t *testing.T) {
	handler := NewUnimplementedHandler()
	_, err := handler.CheckProjectAccess(t.Context(), &businessagent.CheckProjectAccessRequest{})
	var businessErr *bizerrors.BusinessError
	if !stderrors.As(err, &businessErr) {
		t.Fatalf("expected business error, got %T %v", err, err)
	}
	if businessErr.Code != bizerrors.CodeNotImplemented {
		t.Fatalf("expected NOT_IMPLEMENTED, got %s", businessErr.Code)
	}
}
