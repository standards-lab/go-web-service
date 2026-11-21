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

func run() int {
	ctx, stop := process.SignalContext()
	defer stop()
	go func() {
		<-ctx.Done()
		stop()
	}()

	return app.New(cli.Streams{Stdin: os.Stdin, Stdout: os.Stdout, Stderr: os.Stderr}).Run(ctx, os.Args[1:])
}
