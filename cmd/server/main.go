package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/standards-lab/go-web-service/internal/app"
	"github.com/standards-lab/go-web-service/internal/config"
)

func main() {
	os.Exit(run(os.Stdout, os.Stderr))
}

func run(stdout, stderr io.Writer) int {
	ctx, stop := signal.NotifyContext(
		context.Background(),
		os.Interrupt,
		syscall.SIGTERM,
	)
	defer stop()

	cfg, err := config.Load()
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "config load failed:", err)
		return 1
	}

	a, err := app.New(cfg, stdout)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "app init failed:", err)
		return 1
	}

	return a.Run(ctx)
}
