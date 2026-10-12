package input_test

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/standards-lab/go-cli-sdk"

	"github.com/standards-lab/go-web-service/tools/slab/input"
)

// readQuery binds the read flags on a fresh command, dispatches it over
// args, and returns the query pairs they produce.
func readQuery(t *testing.T, args ...string) (query [][2]string, err error) {
	t.Helper()
	var read input.ReadFlags
	cmd := &cli.Command{Name: "list", Run: func(_ context.Context, inv *cli.Invocation) error {
		query, err = read.Query(inv)
		return nil
	}}
	read.Bind(cmd)
	var stderr bytes.Buffer
	if code := cli.Run(context.Background(), cmd, args, cli.Streams{Stdout: &stderr, Stderr: &stderr}); code != 0 {
		t.Fatalf("dispatch of %q exited %d: %s", args, code, stderr.String())
	}
	return query, err
}

// Only the flags given are sent, in flag order, each filter as written.
func TestReadFlags_SendsOnlyTheFlagsGiven(t *testing.T) {
	got, err := readQuery(t)
	if err != nil || len(got) != 0 {
		t.Fatalf("no flags = %v, %v; want nothing", got, err)
	}
	got, err = readQuery(t, "--filter", "code[like]=%a%", "--size", "5", "--sort", "-name", "--page", "2")
	want := [][2]string{{"page", "2"}, {"size", "5"}, {"sort", "-name"}, {"code[like]", "%a%"}}
	if err != nil || fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("query = %v, %v; want %v", got, err, want)
	}
}

func TestReadFlags_RefusesAFilterWithoutAName(t *testing.T) {
	if _, err := readQuery(t, "--filter", "=x"); err == nil {
		t.Error("a nameless filter was accepted")
	}
}

// --page and --cursor are an exclusive group, refused before the command
// runs.
func TestReadFlags_RefusesPageWithCursor(t *testing.T) {
	var read input.ReadFlags
	cmd := &cli.Command{Name: "list", Run: func(context.Context, *cli.Invocation) error {
		t.Error("the command ran")
		return nil
	}}
	read.Bind(cmd)
	var stderr bytes.Buffer
	code := cli.Run(context.Background(), cmd, []string{"--page", "2", "--cursor", "c"}, cli.Streams{Stdout: &stderr, Stderr: &stderr})
	if code != 2 || !strings.Contains(stderr.String(), "flags --page, --cursor cannot be used together") {
		t.Errorf("code = %d, stderr = %q, want the exclusive group refused", code, stderr.String())
	}
}
