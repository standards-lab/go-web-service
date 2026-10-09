package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A configuration that fails to load ends the process before anything is
// composed: run returns a nonzero exit and writes "config load failed"
// with the cause to stderr, nothing to stdout.
func TestRun_ConfigLoadFailure(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(`{"shutdown_timeout": "soon"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
	t.Setenv("APP_ENV", "")

	var stdout, stderr bytes.Buffer
	if code := run(&stdout, &stderr); code == 0 {
		t.Errorf("run = 0, want a nonzero exit")
	}
	if !strings.HasPrefix(stderr.String(), "config load failed: ") {
		t.Errorf("stderr = %q, want it to start with config load failed", stderr.String())
	}
	if stdout.Len() != 0 {
		t.Errorf("stdout = %q, want nothing", stdout.String())
	}
}
