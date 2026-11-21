package app_test

import (
	"bytes"
	"context"
	"io"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/standards-lab/go-core/graph"

	"github.com/standards-lab/go-web-service/internal/app"
	"github.com/standards-lab/go-web-service/internal/config/configtest"
)

const failsafe = 2 * time.Second

type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func TestRun_FailsStartupWithoutInfrastructure(t *testing.T) {
	buf, stderr := &syncBuffer{}, &syncBuffer{}
	a := app.New(configtest.Config(t), buf)

	done := make(chan int, 1)
	go func() { done <- a.Run(context.Background(), stderr) }()

	select {
	case code := <-done:
		if code != 1 {
			t.Errorf("Run = %d, want 1 on a failed startup", code)
		}
	case <-time.After(failsafe):
		t.Fatal("timed out waiting for Run to fail startup")
	}

	out := buf.String()
	failure := regexp.MustCompile(`msg="service failed" error="[^"]*startup: (database|storage|broker)`)
	if !failure.MatchString(out) {
		t.Errorf("log = %q, want a service failure naming a connection's startup", out)
	}
	if strings.Contains(out, "server ready") {
		t.Error("log carries a ready record despite the failed startup")
	}
	if got := stderr.String(); got != "" {
		t.Errorf("stderr = %q, want nothing: a startup failure is logged", got)
	}
}

func TestRun_TwicePanics(t *testing.T) {
	a := app.New(configtest.Config(t), &syncBuffer{})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if code := a.Run(ctx, io.Discard); code != 0 {
		t.Fatalf("first Run = %d, want 0 on a context ended before startup", code)
	}

	var built []string
	a.Graph().Observe(func(r graph.Ref) { built = append(built, r.Name()) })
	defer func() {
		if recover() == nil {
			t.Error("a second Run did not panic")
		}
		if len(built) != 0 {
			t.Errorf("the second Run built %v before it panicked, want nothing", built)
		}
	}()
	_ = a.Run(ctx, io.Discard)
}
