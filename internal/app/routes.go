package app

import (
	"github.com/standards-lab/go-core/graph"
	"github.com/standards-lab/go-web-sdk"
)

func routes(s *graph.Scope, n *Nodes) []*web.Module {
	return []*web.Module{
		web.NewModule(mountAPI(s, n)),
		web.NewModule(mountAdmin(s, n)),
	}
}
