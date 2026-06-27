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
	if got := len(svr.GetServiceInfos()); got != 9 {
		t.Fatalf("expected 9 services, got %d", got)
	}
}
