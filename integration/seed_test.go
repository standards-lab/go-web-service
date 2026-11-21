//go:build integration

package integration_test

import (
	"bytes"
	"context"
	"maps"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/standards-lab/go-web-sdk/webtest"

	"github.com/standards-lab/go-web-service/integration"
)

var seededCounts = integration.Seeded{"organizations": seededTotal, "logos": seededTotal, "documents": 10}

const acmeLogoKey = "5eed0001-0000-4000-8000-000000000001/5eed0001-0000-4000-8000-000000000001.png"

func TestSeededStorage(t *testing.T) {
	s := integration.Start(t, integration.Options{Seed: integration.Default})
	c := s.Client()

	assertLogosServeFixtures := func(t *testing.T) {
		t.Helper()
		for code, o := range tree(t, c) {
			fixture, err := os.ReadFile(filepath.Join("..", "data", "seeds", "fixtures", code+".png"))
			if err != nil {
				t.Fatal(err)
			}
			r := c.Get(t, organizations+"/"+o.ID+"/logo")
			if r.Status != http.StatusOK || !bytes.Equal(r.Body, fixture) || r.Header.Get("Content-Type") != "image/png" {
				t.Errorf("%s's logo = %d, %d bytes of %q; want its fixture's %d bytes as image/png", code, r.Status, len(r.Body), r.Header.Get("Content-Type"), len(fixture))
			}
		}
	}
	names := func(t *testing.T, path string) []string {
		t.Helper()
		var out []string
		for _, d := range webtest.Decode[directoryPage](t, c.Get(t, path+"/directories"), http.StatusOK).Items {
			out = append(out, d.Name+"/")
		}
		for _, f := range webtest.Decode[filePage](t, c.Get(t, path+"/files"), http.StatusOK).Items {
			if f.Status != "available" {
				t.Errorf("%s is %s; want available", f.Name, f.Status)
			}
			out = append(out, f.Name)
		}
		slices.Sort(out)
		return out
	}
	assertAcmeTreeListsAndDownloads := func(t *testing.T) {
		t.Helper()
		docs := "/api/documents/" + tree(t, c)["acme"].ID
		if got := names(t, docs+"/directories/root"); !slices.Equal(got, []string{"README.txt", "engineering/", "finance/", "operations/"}) {
			t.Errorf("acme's root = %v", got)
		}
		dirs := webtest.Decode[directoryPage](t, c.Get(t, docs+"/directories/root/directories"), http.StatusOK)
		for _, d := range dirs.Items {
			if d.Name != "finance" {
				continue
			}
			files := webtest.Decode[filePage](t, c.Get(t, docs+"/directories/"+d.ID+"/files"), http.StatusOK)
			if len(files.Items) != 1 || files.Items[0].Name != "budget.csv" {
				t.Fatalf("finance/ = %+v; want budget.csv", files.Items)
			}
			r := c.Get(t, docs+"/files/"+files.Items[0].ID+"/content")
			if r.Status != http.StatusOK || !bytes.HasPrefix(r.Body, []byte("unit,quarter,amount\n")) {
				t.Errorf("budget.csv = %d %q; want its seeded content", r.Status, r.Body)
			}
		}
		for path, want := range map[string][]string{
			"/5eed0002-0000-4000-8000-000000000002": {"architecture.txt", "platform/"},
			"/5eed0002-0000-4000-8000-000000000004": {"runbook.txt"},
			"/5eed0002-0000-4000-8000-000000000009": {"carriers.csv"},
		} {
			if got := names(t, docs+"/directories"+path); !slices.Equal(got, want) {
				t.Errorf("directory %s = %v; want %v, under its fixed id", path, got, want)
			}
		}
	}

	tr := integration.Reset(t, c, integration.Default)
	if !maps.Equal(tr.Seeded, seededCounts) {
		t.Fatalf("reset to default seeded %v; want %v", tr.Seeded, seededCounts)
	}
	assertLogosServeFixtures(t)
	assertAcmeTreeListsAndDownloads(t)
	if _, err := integration.Objects(t).Stat(context.Background(), acmeLogoKey); err != nil {
		t.Errorf("acme's logo is not under its fixed key: %v", err)
	}

	t.Run("a seed over the seeded state stores nothing and changes nothing", func(t *testing.T) {
		if n := integration.Seed(t, c, integration.Default); !maps.Equal(n, integration.Seeded{"organizations": 0, "logos": 0, "documents": 0}) {
			t.Errorf("seed over default = %v; want every contribution at zero", n)
		}
		assertLogosServeFixtures(t)
		assertAcmeTreeListsAndDownloads(t)
	})

	t.Run("a reset rewrites each file under its fixed key over the object the last reset left", func(t *testing.T) {
		if tr := integration.Reset(t, c, integration.Default); !maps.Equal(tr.Seeded, seededCounts) {
			t.Errorf("a second reset to default seeded %v; want %v", tr.Seeded, seededCounts)
		}
		assertLogosServeFixtures(t)
		assertAcmeTreeListsAndDownloads(t)
	})

	t.Run("the empty state seeds no organization, so no logo and no tree", func(t *testing.T) {
		tr := integration.Reset(t, c, "empty")
		if !maps.Equal(tr.Seeded, integration.Seeded{"organizations": 0, "logos": 0, "documents": 0}) || len(tree(t, c)) != 0 {
			t.Errorf("reset to empty seeded %v, %d organizations; want nothing", tr.Seeded, len(tree(t, c)))
		}
	})

	if code := s.Stop(t); code != 0 {
		t.Fatalf("exit = %d:\n%s", code, s.Output())
	}
}
