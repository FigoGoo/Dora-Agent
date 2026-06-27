package rpc

import (
	"github.com/FigoGoo/Dora-Agent/kitex_gen/dora/api/businessagent/accountspaceservice"
	"github.com/FigoGoo/Dora-Agent/kitex_gen/dora/api/businessagent/adminservice"
	"github.com/FigoGoo/Dora-Agent/kitex_gen/dora/api/businessagent/assetcreditcommitservice"
	"github.com/FigoGoo/Dora-Agent/kitex_gen/dora/api/businessagent/assetservice"
	"github.com/FigoGoo/Dora-Agent/kitex_gen/dora/api/businessagent/creditservice"
	"github.com/FigoGoo/Dora-Agent/kitex_gen/dora/api/businessagent/enterpriseservice"
	"github.com/FigoGoo/Dora-Agent/kitex_gen/dora/api/businessagent/featuredworkadminservice"
	"github.com/FigoGoo/Dora-Agent/kitex_gen/dora/api/businessagent/modelconfigservice"
	"github.com/FigoGoo/Dora-Agent/kitex_gen/dora/api/businessagent/notificationservice"
	"github.com/FigoGoo/Dora-Agent/kitex_gen/dora/api/businessagent/platformdictionaryservice"
	"github.com/FigoGoo/Dora-Agent/kitex_gen/dora/api/businessagent/projectassetservice"
	"github.com/FigoGoo/Dora-Agent/kitex_gen/dora/api/businessagent/projectservice"
	"github.com/FigoGoo/Dora-Agent/kitex_gen/dora/api/businessagent/publiccontentservice"
	"github.com/FigoGoo/Dora-Agent/kitex_gen/dora/api/businessagent/skillcatalogservice"
	"github.com/FigoGoo/Dora-Agent/kitex_gen/dora/api/businessagent/toolcapabilityservice"
	"github.com/FigoGoo/Dora-Agent/kitex_gen/dora/api/businessagent/useradminservice"
	"github.com/FigoGoo/Dora-Agent/kitex_gen/dora/api/businessagent/workservice"
	"github.com/FigoGoo/Dora-Agent/kitex_gen/dora/api/businessagent/workshareservice"
	"github.com/cloudwego/kitex/server"
)

func RegisterAll(svr server.Server, handler *Handler) error {
	registrars := []func(server.Server, *Handler) error{
		func(s server.Server, h *Handler) error { return accountspaceservice.RegisterService(s, h) },
		func(s server.Server, h *Handler) error { return enterpriseservice.RegisterService(s, h) },
		func(s server.Server, h *Handler) error { return adminservice.RegisterService(s, h) },
		func(s server.Server, h *Handler) error { return useradminservice.RegisterService(s, h) },
		func(s server.Server, h *Handler) error { return projectservice.RegisterService(s, h) },
		func(s server.Server, h *Handler) error { return projectassetservice.RegisterService(s, h) },
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
		func(s server.Server, h *Handler) error { return workservice.RegisterService(s, h) },
		func(s server.Server, h *Handler) error { return workshareservice.RegisterService(s, h) },
		func(s server.Server, h *Handler) error {
			return featuredworkadminservice.RegisterService(s, h)
		},
		func(s server.Server, h *Handler) error { return publiccontentservice.RegisterService(s, h) },
		func(s server.Server, h *Handler) error { return notificationservice.RegisterService(s, h) },
	}
	for _, register := range registrars {
		if err := register(svr, handler); err != nil {
			return err
		}
	}
	return nil
}
