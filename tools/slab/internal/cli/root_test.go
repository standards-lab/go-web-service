package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func execute(t *testing.T, args ...string) (string, error) {
	t.Helper()
	var out bytes.Buffer
	root := Root()
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs(args)
	err := root.ExecuteContext(context.Background())
	return out.String(), err
}

func TestRoot_PrintsHelpAndTheListing(t *testing.T) {
	out, err := execute(t)
	if err != nil {
		t.Fatalf("root: %v", err)
	}
	for _, want := range []string{"Available Commands:", "list", "demo", "Scenarios:", "  sqlate    ", "  domain    ", "  problems  "} {
		if !strings.Contains(out, want) {
			t.Errorf("root output lacks %q:\n%s", want, out)
		}
	}
}

func TestList_PrintsTheScenariosInPresentationOrder(t *testing.T) {
	out, err := execute(t, "list")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	sqlate, domain, problems := strings.Index(out, "  sqlate    "), strings.Index(out, "  domain    "), strings.Index(out, "  problems  ")
	if sqlate < 0 || domain < sqlate || problems < domain {
		t.Errorf("list does not print sqlate, domain, problems in that order:\n%s", out)
	}
}

func TestDemo_MountsEachScenario(t *testing.T) {
	out, err := execute(t, "demo")
	if err != nil {
		t.Fatalf("demo: %v", err)
	}
	for _, want := range []string{"Available Commands:", "sqlate", "domain", "problems"} {
		if !strings.Contains(out, want) {
			t.Errorf("demo help lacks %q:\n%s", want, out)
		}
	}
	if _, err := execute(t, "demo", "none"); err == nil {
		t.Error("demo none succeeded")
	}
}
