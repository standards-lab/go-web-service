package repo_test

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/standards-lab/go-web-service/tools/slab/env"
	"github.com/standards-lab/go-web-service/tools/slab/repo"
)

// tree builds a fake checkout: the root's go.mod declaring the module, and a
// nested tools/slab module declaring its own path, as the real one does.
func tree(t *testing.T) (root, nested string) {
	t.Helper()
	root = t.TempDir()
	write(t, filepath.Join(root, "go.mod"), "module "+repo.Module+"\n\ngo 1.27\n")
	nested = filepath.Join(root, "tools", "slab", "internal", "demo")
	write(t, filepath.Join(root, "tools", "slab", "go.mod"), "module "+repo.Module+"/tools/slab\n")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	return root, nested
}

func write(t *testing.T, path, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestFind_WalksPastTheNestedModuleToTheRoot(t *testing.T) {
	root, nested := tree(t)
	got, err := repo.Find(nested)
	if err != nil {
		t.Fatalf("Find(%s): %v", nested, err)
	}
	if got != root {
		t.Errorf("Find(%s) = %s; want %s", nested, got, root)
	}
}

func TestFind_FromTheRealCheckout(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	got, err := repo.Find(wd)
	if err != nil {
		t.Fatalf("Find(%s): %v", wd, err)
	}
	if _, err := os.Stat(filepath.Join(got, "domain", "organization", "statements")); err != nil {
		t.Errorf("Find(%s) = %s, which has no domain/organization/statements: %v", wd, got, err)
	}
}

func TestFind_ReportsNotFoundAboveADirectoryWithNoRoot(t *testing.T) {
	dir := t.TempDir()
	_, err := repo.Find(dir)
	if !errors.Is(err, repo.ErrNotFound) {
		t.Fatalf("Find(%s) error = %v; want ErrNotFound", dir, err)
	}
	if !strings.Contains(err.Error(), dir) {
		t.Errorf("error does not name the directory the walk started from: %v", err)
	}
}

func TestRoot_UsesTheOverride(t *testing.T) {
	root, _ := tree(t)
	ctx := env.WithContext(context.Background(), env.Env{Repo: root})
	got, err := repo.Root(ctx)
	if err != nil {
		t.Fatalf("Root with --repo %s: %v", root, err)
	}
	if got != root {
		t.Errorf("Root = %s; want the override %s", got, root)
	}
}

func TestRoot_RejectsAnOverrideThatIsNotTheRoot(t *testing.T) {
	_, nested := tree(t)
	ctx := env.WithContext(context.Background(), env.Env{Repo: nested})
	if _, err := repo.Root(ctx); err == nil {
		t.Errorf("Root accepted --repo %s, which declares no go.mod", nested)
	}
	ctx = env.WithContext(context.Background(), env.Env{Repo: filepath.Join(nested, "..", "..")})
	if _, err := repo.Root(ctx); err == nil {
		t.Error("Root accepted --repo tools/slab, whose go.mod declares another module")
	}
}

func TestFS_ReadsUnderTheRoot(t *testing.T) {
	root, _ := tree(t)
	write(t, filepath.Join(root, "data", "seeds", "default.json"), "{}")
	ctx := env.WithContext(context.Background(), env.Env{Repo: root})
	fsys, err := repo.FS(ctx)
	if err != nil {
		t.Fatalf("FS: %v", err)
	}
	text, err := fs.ReadFile(fsys, "data/seeds/default.json")
	if err != nil {
		t.Fatalf("ReadFile through FS: %v", err)
	}
	if string(text) != "{}" {
		t.Errorf("read %q, want {}", text)
	}
}

func TestFS_FailsWhereRootFails(t *testing.T) {
	ctx := env.WithContext(context.Background(), env.Env{Repo: t.TempDir()})
	if _, err := repo.FS(ctx); err == nil {
		t.Error("FS accepted --repo pointing at a directory with no go.mod")
	}
}

func TestRoot_WalksFromTheWorkingDirectoryWithoutAnOverride(t *testing.T) {
	got, err := repo.Root(context.Background())
	if err != nil {
		t.Fatalf("Root: %v", err)
	}
	if _, err := os.Stat(filepath.Join(got, "data", "patterns")); err != nil {
		t.Errorf("Root = %s, which has no data/patterns: %v", got, err)
	}
}
