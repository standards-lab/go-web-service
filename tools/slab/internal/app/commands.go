package app

import (
	"github.com/spf13/cobra"
)

// commands is the list of mounts: the subtrees the legacy cobra root serves,
// each built by the layer file that owns it: the demo mount and list from
// demo.go; this file composes and does nothing else. The demo narrates
// through its own reporter, over cfg.
func commands(cfg *legacyConfig) []*cobra.Command {
	return []*cobra.Command{mountDemo(cfg), listCommand()}
}
