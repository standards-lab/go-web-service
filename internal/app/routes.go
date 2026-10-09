package app

import (
	"github.com/standards-lab/go-core/graph"
	"github.com/standards-lab/go-web-sdk"
)

// routes is the list of mounts: the modules the router serves, each built
// by the layer file that owns it. The API mount comes from domain.go, the
// admin mount from admin.go; this file composes and does nothing else.
// Each mount reads the nodes it needs with s.Use from n, and every group's
// error writer logs through the logger node, the service's logger.
func routes(s *graph.Scope, n *Nodes) []*web.Module {
	return []*web.Module{
		web.NewModule(mountAPI(s, n)),
		web.NewModule(mountAdmin(s, n)),
	}
}
