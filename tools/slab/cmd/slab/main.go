package main

import (
	"os"

	"github.com/standards-lab/go-cli-sdk"
	"github.com/standards-lab/go-core/process"

	"github.com/standards-lab/go-web-service/tools/slab/internal/app"
)

func main() {
	os.Exit(run())
}

// run runs app and returns its exit code. It is separate from main so the
// deferred stop releases the signal registration before os.Exit, which
// skips deferred calls.
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

	return app.New(cli.Streams{Stdin: os.Stdin, Stdout: os.Stdout, Stderr: os.Stderr}).Run(ctx, os.Args[1:])
}
