package process

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
)

const (
	ExitOK      = 0
	ExitFailure = 1
	ExitUsage   = 2
)

func Fail(w io.Writer, msg string, err error) int {
	_, _ = fmt.Fprintf(w, "%s: %v\n", msg, err)
	return ExitFailure
}

func Usage(w io.Writer, text string) int {
	_, _ = fmt.Fprintln(w, text)
	return ExitUsage
}

func SignalContext() (context.Context, context.CancelFunc) {
	return signal.NotifyContext(
		context.Background(),
		os.Interrupt,
		syscall.SIGTERM,
	)
}
