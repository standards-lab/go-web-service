package app

import (
	"context"
	"fmt"
	"runtime/debug"

	"github.com/standards-lab/go-cli-sdk"
)

// versionCommand returns the version command, which prints the main
// module's version. It declares no nodes, so it builds nothing and reads no
// configuration.
func versionCommand() *cli.Command {
	return &cli.Command{
		Name:    "version",
		Summary: "Print slab's version",
		Args:    cli.NoArgs,
		Run: func(_ context.Context, inv *cli.Invocation) error {
			_, err := fmt.Fprintln(inv.Stdout, moduleVersion())
			return err
		},
	}
}

// moduleVersion returns the main module's version from the build info, or
// "(devel)" when the binary carries none, as under go test; go run reports
// "(devel)" itself.
func moduleVersion() string {
	info, ok := debug.ReadBuildInfo()
	if !ok || info.Main.Version == "" {
		return "(devel)"
	}
	return info.Main.Version
}
