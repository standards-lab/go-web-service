// Command slab calls the service's endpoints, one subcommand per endpoint,
// and runs its narrated scenarios, each printing what it is about to do and
// what it observed.
package main

import (
	"os"

	"github.com/standards-lab/go-core/process"

	"github.com/standards-lab/go-web-service/tools/slab/internal/app"
)

func main() {
	os.Exit(run())
}

func run() int {
	ctx, stop := process.SignalContext()
	defer stop()
	// The first signal cancels the context; stop then restores the default
	// handlers so a second signal terminates the process even if a scenario
	// ignores the cancellation.
	go func() {
		<-ctx.Done()
		stop()
	}()

	return app.New(os.Stdout, os.Stderr).Run(ctx)
}
