package app

import (
	"github.com/spf13/cobra"

	"github.com/standards-lab/go-web-service/tools/slab/output"
)

// commands is the list of mounts: the subtrees the legacy cobra root serves,
// each built by the layer file that owns it. The admin mount comes from
// admin.go, the demo mount and list from demo.go; this file composes and
// does nothing else. The admin commands render through out; the demo
// narrates through its own reporter, over cfg.
func commands(adm *Admin, out *output.Output, cfg *legacyConfig) []*cobra.Command {
	return []*cobra.Command{mountAdmin(adm, out), mountDemo(cfg), listCommand()}
}
