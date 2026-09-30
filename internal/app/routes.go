package app

import (
	"log/slog"

	"github.com/standards-lab/go-web-sdk"

	"github.com/standards-lab/go-web-service/internal/config"
)

// routes is the list of mounts: the modules the router serves, each built
// by the layer file that owns it. The API mount comes from domain.go, the
// admin mount from admin.go; this file composes and does nothing else.
// logger is the service's, which every group's error writer logs through.
func routes(dom *Domain, adm *Admin, cfg *config.Config, logger *slog.Logger) []*web.Module {
	return []*web.Module{
		web.NewModule(mountAPI(dom, cfg, logger)),
		web.NewModule(mountAdmin(adm, logger)),
	}
}
