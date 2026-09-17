package app

import (
	"github.com/spf13/cobra"
)

// commands is the list of mounts: the subtrees the root serves, each built
// by the layer file that owns it. The domain commands come from domain.go,
// the admin mount from admin.go, the demo mount and list from demo.go; this
// file composes and does nothing else.
func commands(dom *Domain, adm *Admin, cfg *Config) []*cobra.Command {
	var all []*cobra.Command
	all = append(all, mountDomain(dom)...)
	all = append(all, mountAdmin(adm), mountDemo(cfg), listCommand())
	return all
}
