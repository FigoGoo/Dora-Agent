package rpc

import (
	"github.com/FigoGoo/Dora-Agent/kitex_gen/dora/api/businessagent/accountspaceservice"
	"github.com/FigoGoo/Dora-Agent/kitex_gen/dora/api/businessagent/assetcreditcommitservice"
	"github.com/FigoGoo/Dora-Agent/kitex_gen/dora/api/businessagent/assetservice"
	"github.com/FigoGoo/Dora-Agent/kitex_gen/dora/api/businessagent/creditservice"
	"github.com/FigoGoo/Dora-Agent/kitex_gen/dora/api/businessagent/modelconfigservice"
	"github.com/FigoGoo/Dora-Agent/kitex_gen/dora/api/businessagent/platformdictionaryservice"
	"github.com/FigoGoo/Dora-Agent/kitex_gen/dora/api/businessagent/projectservice"
	"github.com/FigoGoo/Dora-Agent/kitex_gen/dora/api/businessagent/skillcatalogservice"
	"github.com/FigoGoo/Dora-Agent/kitex_gen/dora/api/businessagent/toolcapabilityservice"
	"github.com/cloudwego/kitex/server"
)

func RegisterAll(svr server.Server, handler *Handler) error {
	registrars := []func(server.Server, *Handler) error{
		func(s server.Server, h *Handler) error { return accountspaceservice.RegisterService(s, h) },
		func(s server.Server, h *Handler) error { return projectservice.RegisterService(s, h) },
		func(s server.Server, h *Handler) error { return assetservice.RegisterService(s, h) },
		func(s server.Server, h *Handler) error { return creditservice.RegisterService(s, h) },
		func(s server.Server, h *Handler) error {
			return assetcreditcommitservice.RegisterService(s, h)
		},
		func(s server.Server, h *Handler) error { return skillcatalogservice.RegisterService(s, h) },
		func(s server.Server, h *Handler) error {
			return toolcapabilityservice.RegisterService(s, h)
		},
		func(s server.Server, h *Handler) error { return modelconfigservice.RegisterService(s, h) },
		func(s server.Server, h *Handler) error {
			return platformdictionaryservice.RegisterService(s, h)
		},
	}
	for _, register := range registrars {
		if err := register(svr, handler); err != nil {
			return err
		}
	}
	return nil
}
