package input_test

import (
	"fmt"
	"testing"

	"github.com/spf13/cobra"

	"github.com/standards-lab/go-web-service/tools/slab/input"
)

// readQuery binds the read flags on a fresh command, parses args, and
// returns the query pairs they produce.
func readQuery(t *testing.T, args ...string) ([][2]string, error) {
	t.Helper()
	var read input.ReadFlags
	cmd := &cobra.Command{Use: "list"}
	read.Bind(cmd)
	if err := cmd.ParseFlags(args); err != nil {
		t.Fatal(err)
	}
	return read.Query(cmd)
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
