package output_test

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/standards-lab/go-web-sdk"

	"github.com/standards-lab/go-web-service/tools/slab/httpx"
	"github.com/standards-lab/go-web-service/tools/slab/output"
)

// fixed runs a FixedCommand built over op with args and returns what it
// wrote to stdout and the error it returned.
func fixed(t *testing.T, status int, op func(context.Context) (*httpx.Response, error), args ...string) (string, error) {
	t.Helper()
	cmd := output.FixedCommand("thing", "Read the thing", status, op)
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), err
}

func TestFixedCommand_CallsOpWhenItRunsAndPrintsTheReply(t *testing.T) {
	calls := 0
	op := func(ctx context.Context) (*httpx.Response, error) {
		calls++
		if ctx == nil {
			t.Error("op received a nil context")
		}
		return &httpx.Response{Status: http.StatusOK, Body: []byte(`{"version":2}`)}, nil
	}
	cmd := output.FixedCommand("thing", "Read the thing", http.StatusOK, op)
	if calls != 0 {
		t.Fatalf("FixedCommand called op %d times while building the command", calls)
	}
	if cmd.Use != "thing" || cmd.Short != "Read the thing" {
		t.Errorf("command = %q %q", cmd.Use, cmd.Short)
	}
	out, err := fixed(t, http.StatusOK, op)
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Errorf("op was called %d times, want once at run time", calls)
	}
	if want := "{\n  \"version\": 2\n}\n"; out != want {
		t.Errorf("stdout = %q, want %q", out, want)
	}
}

func TestFixedCommand_RefusesArguments(t *testing.T) {
	calls := 0
	op := func(context.Context) (*httpx.Response, error) {
		calls++
		return &httpx.Response{Status: http.StatusOK}, nil
	}
	_, err := fixed(t, http.StatusOK, op, "extra")
	if err == nil || !strings.Contains(err.Error(), "unknown command") {
		t.Errorf("err = %v, want cobra's no-arguments error", err)
	}
	if calls != 0 {
		t.Errorf("op was called %d times, want none", calls)
	}
}

func TestFixedCommand_ReturnsOpsError(t *testing.T) {
	want := errors.New("connection refused")
	_, err := fixed(t, http.StatusOK, func(context.Context) (*httpx.Response, error) {
		return nil, want
	})
	if !errors.Is(err, want) {
		t.Errorf("err = %v, want %v", err, want)
	}
}

func TestFixedCommand_ExpectsTheStatusItIsGiven(t *testing.T) {
	problem := &httpx.Response{
		Status: http.StatusConflict,
		Header: http.Header{"Content-Type": {web.ProblemMediaType}},
		Body:   []byte(`{"type":"about:blank","title":"Conflict","status":409,"detail":"dirty"}`),
	}
	_, err := fixed(t, http.StatusOK, func(context.Context) (*httpx.Response, error) {
		return problem, nil
	})
	var pe *output.ProblemError
	if !errors.As(err, &pe) {
		t.Fatalf("err = %v (%T), want a *output.ProblemError", err, err)
	}
	if pe.Status != http.StatusConflict || pe.Detail != "dirty" {
		t.Errorf("problem = %+v", pe.Problem)
	}

	accepted := &httpx.Response{Status: http.StatusAccepted, Body: []byte(`{}`)}
	out, err := fixed(t, http.StatusAccepted, func(context.Context) (*httpx.Response, error) {
		return accepted, nil
	})
	if err != nil {
		t.Fatalf("err = %v, want nil when the status matches", err)
	}
	if want := "{}\n"; out != want {
		t.Errorf("stdout = %q, want %q", out, want)
	}
}
