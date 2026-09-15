// Command slab runs the service's narrated scenarios: one command per
// capability the service shows, each printing what it is about to do and what
// it observed.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/standards-lab/go-web-service/tools/slab/internal/cli"
)

func main() {
	os.Exit(run())
}

func run() int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	// The first signal cancels the context; stop then restores the default
	// handlers so a second signal terminates the process even if a scenario
	// ignores the cancellation.
	go func() {
		<-ctx.Done()
		stop()
	}()

	if err := cli.Root().ExecuteContext(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "slab:", err)
		return 1
	}
	return 0
}
