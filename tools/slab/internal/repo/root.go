// Package repo resolves the go-web-service repository root, the directory a
// scenario reads the service's own sources from.
package repo

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/standards-lab/go-web-service/tools/slab/internal/env"
)

// Module is the module path the repository root's go.mod declares. slab is
// its own module under tools/, so the walk up from it passes tools/slab's
// go.mod, whose module line is a different path, and stops at the root's.
const Module = "github.com/standards-lab/go-web-service"

// ErrNotFound reports that no directory on the walk declares Module.
var ErrNotFound = errors.New("repo: no go.mod declaring " + Module)

// Root returns the repository root: the --repo override carried by ctx
// when one was given, checked to be a root, or else the nearest ancestor of
// the working directory whose go.mod declares Module.
func Root(ctx context.Context) (string, error) {
	if override := env.FromContext(ctx).Repo; override != "" {
		dir, err := filepath.Abs(override)
		if err != nil {
			return "", fmt.Errorf("repo: --repo %q: %w", override, err)
		}
		ok, err := declares(filepath.Join(dir, "go.mod"))
		if err != nil {
			return "", fmt.Errorf("repo: --repo %s: %w", dir, err)
		}
		if !ok {
			return "", fmt.Errorf("repo: --repo %s: its go.mod does not declare module %s", dir, Module)
		}
		return dir, nil
	}
	wd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("repo: %w", err)
	}
	return Find(wd)
}

// FS returns the repository root Root resolves as a filesystem, the form a
// scenario reads the service's sources and seeds through.
func FS(ctx context.Context) (fs.FS, error) {
	root, err := Root(ctx)
	if err != nil {
		return nil, err
	}
	return os.DirFS(root), nil
}

// Find walks up from dir to the filesystem root and returns the first
// directory whose go.mod declares Module, or ErrNotFound naming dir when the
// walk reaches the top without one.
func Find(dir string) (string, error) {
	start, err := filepath.Abs(dir)
	if err != nil {
		return "", fmt.Errorf("repo: %w", err)
	}
	for dir := start; ; dir = filepath.Dir(dir) {
		ok, err := declares(filepath.Join(dir, "go.mod"))
		if err != nil {
			return "", fmt.Errorf("repo: %w", err)
		}
		if ok {
			return dir, nil
		}
		if filepath.Dir(dir) == dir {
			return "", fmt.Errorf("%w above %s", ErrNotFound, start)
		}
	}
}

// declares reports whether the go.mod at path exists and its module
// directive names Module. A missing file is not an error: it is the common
// case on the walk.
func declares(path string) (bool, error) {
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	defer func() { _ = f.Close() }() // read-only; nothing to act on
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if !strings.HasPrefix(line, "module ") && !strings.HasPrefix(line, "module\t") {
			continue
		}
		path := strings.TrimSpace(strings.TrimPrefix(line, "module"))
		return strings.Trim(path, `"`) == Module, nil
	}
	return false, sc.Err()
}
