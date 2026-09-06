package app

import (
	"github.com/standards-lab/go-web-sdk"

	"github.com/standards-lab/go-web-service/internal/config"
)

// routes is the list of mounts: the modules the router serves, each built
// by the layer file that owns it. The API mount comes from domain.go, the
// admin mount from admin.go; this file composes and does nothing else.
func routes(dom *Domain, adm *Admin, cfg *config.Config) []*web.Module {
	return []*web.Module{
		web.NewModule(mountAPI(dom, cfg)),
		web.NewModule(mountAdmin(adm)),
	}
}
