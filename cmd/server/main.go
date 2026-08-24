package main

import (
	"io"
	"os"

	"github.com/standards-lab/go-core/process"

	"github.com/standards-lab/go-web-service/internal/app"
	"github.com/standards-lab/go-web-service/internal/config"
)

func main() {
	os.Exit(run(os.Stdout, os.Stderr))
}

func run(stdout, stderr io.Writer) int {
	ctx, stop := process.SignalContext()
	defer stop()

	cfg, err := config.Load()
	if err != nil {
		return process.Fail(stderr, "config load failed", err)
	}

	a, err := app.New(cfg, stdout)
	if err != nil {
		return process.Fail(stderr, "app init failed", err)
	}

	return a.Run(ctx)
}
