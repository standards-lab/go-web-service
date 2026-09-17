package app

import (
	"github.com/spf13/cobra"

	"github.com/standards-lab/go-web-service/tools/slab/output"
)

// commands is the list of mounts: the subtrees the root serves, each built
// by the layer file that owns it. The domain commands come from domain.go,
// the admin mount from admin.go, the demo mount and list from demo.go; this
// file composes and does nothing else. The direct command families render
// through out; the demo narrates through its own reporter, over cfg.
func commands(dom *Domain, adm *Admin, out *output.Output, cfg *Config) []*cobra.Command {
	var all []*cobra.Command
	all = append(all, mountDomain(dom, out)...)
	all = append(all, mountAdmin(adm, out), mountDemo(cfg), listCommand())
	return all
}
