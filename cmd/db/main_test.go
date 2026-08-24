package main

import (
	"context"
	"strings"
	"testing"

	"github.com/standards-lab/go-core/process"
)

// The dispatch and flag-validation paths exit before any configuration or
// database work, so they are testable without either. The verbs themselves
// are proven against the live compose database.

func TestRun_NoArgsIsUsageError(t *testing.T) {
	var stdout, stderr strings.Builder

	code := run(nil, &stdout, &stderr)

	if code != process.ExitUsage {
		t.Errorf("run() = %d, want ExitUsage", code)
	}
	if !strings.Contains(stderr.String(), "usage: db") {
		t.Errorf("stderr = %q, want the usage block", stderr.String())
	}
}

func TestRun_HelpPrintsUsageToStdout(t *testing.T) {
	var stdout, stderr strings.Builder

	code := run([]string{"-help"}, &stdout, &stderr)

	if code != process.ExitUsage {
		t.Errorf("run(-help) = %d, want ExitUsage", code)
	}
	if !strings.Contains(stdout.String(), "usage: db") {
		t.Errorf("stdout = %q, want the usage block", stdout.String())
	}
}

func TestRun_UnknownCommandNamesIt(t *testing.T) {
	var stdout, stderr strings.Builder

	code := run([]string{"launch"}, &stdout, &stderr)

	if code != process.ExitUsage {
		t.Errorf("run(launch) = %d, want ExitUsage", code)
	}
	got := stderr.String()
	if !strings.Contains(got, `unknown command "launch"`) || !strings.Contains(got, "usage: db") {
		t.Errorf("stderr = %q, want the command named and the usage block", got)
	}
}

func TestMigrateCmd_UsagePaths(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		wantStderr string
	}{
		{"no verb", nil, "usage: db migrate"},
		{"unknown verb", []string{"sideways"}, `unknown verb "sideways"`},
		{"down negative n", []string{"down", "-n", "-1"}, "-n must not be negative"},
		{"steps zero n", []string{"steps", "-n", "0"}, "-n must be non-zero"},
		{"steps missing n", []string{"steps"}, "-n must be non-zero"},
		{"force missing version", []string{"force"}, "-version is required"},
		{"down unparsable n", []string{"down", "-n", "many"}, "invalid value"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout, stderr strings.Builder

			code := migrateCmd(tt.args, &stdout, &stderr)

			if code != process.ExitUsage {
				t.Errorf("migrateCmd(%v) = %d, want ExitUsage", tt.args, code)
			}
			if !strings.Contains(stderr.String(), tt.wantStderr) {
				t.Errorf("stderr = %q, want it to contain %q", stderr.String(), tt.wantStderr)
			}
		})
	}
}

func TestMigrateCmd_HelpPrintsVerbsToStdout(t *testing.T) {
	var stdout, stderr strings.Builder

	code := migrateCmd([]string{"help"}, &stdout, &stderr)

	if code != process.ExitUsage {
		t.Errorf("migrateCmd(help) = %d, want ExitUsage", code)
	}
	if !strings.Contains(stdout.String(), "usage: db migrate") {
		t.Errorf("stdout = %q, want the migrate usage block", stdout.String())
	}
}

func TestLoadOrganizations_RequiresParentSeededFirst(t *testing.T) {
	rows := []organization{
		{Parent: "ops", Code: "logistics", Name: "Logistics"},
	}

	err := loadOrganizations(context.Background(), nil, rows)

	if err == nil {
		t.Fatal("loadOrganizations() = nil, want parent-order error")
	}
	if !strings.Contains(err.Error(), `parent "ops" not seeded before it`) {
		t.Errorf("error = %v, want the missing parent named", err)
	}
}
