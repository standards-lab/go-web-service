//go:build integration

package integration_test

import (
	"context"
	"net/http"
	"testing"

	bfdata "github.com/standards-lab/blobfs/data"
	blobfspg "github.com/standards-lab/blobfs/postgres"
	"github.com/standards-lab/go-database"
	"github.com/standards-lab/go-database/postgres"
	"github.com/standards-lab/go-web-sdk/webtest"
	"github.com/standards-lab/sqlate"
	pgdialect "github.com/standards-lab/sqlate/postgres"
	"github.com/standards-lab/sqlate/query"

	"github.com/standards-lab/go-web-service/integration"
)

// The document API's directory and file, as it presents them: the fields
// the suite reads.
type directory struct {
	ID      string  `json:"id"`
	Name    string  `json:"name"`
	Path    string  `json:"path"`
	Status  string  `json:"status"`
	Version int64   `json:"version"`
	Parent  *string `json:"parent_id"`
}

type file struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Status string `json:"status"`
}

type directoryPage struct {
	Items []directory `json:"items"`
	Total *int        `json:"total"`
}

type filePage struct {
	Items []file `json:"items"`
	Total *int   `json:"total"`
}

func TestDocument(t *testing.T) {
	s := integration.Start(t, integration.Options{Seed: integration.Default})
	c := s.Client()

	run := func(name string, fn func(t *testing.T, docs string)) {
		t.Run(name, func(t *testing.T) {
			integration.Reset(t, c, integration.Default)
			fn(t, "/api/documents/"+tree(t, c)["acme"].ID)
		})
	}

	run("a directory reads with its status", func(t *testing.T, docs string) {
		reports := webtest.Decode[identity](t, c.Post(t, docs+"/directories", map[string]string{"parent_id": "root", "name": "reports"}), http.StatusCreated)
		d := webtest.Decode[directory](t, c.Get(t, docs+"/directories/"+reports.ID), http.StatusOK)
		if d.Status != "active" || d.Path != "/reports" || d.Version != reports.Version {
			t.Errorf("reports = %+v; want it active at /reports", d)
		}
		root := webtest.Decode[directory](t, c.Get(t, docs+"/directories/root"), http.StatusOK)
		if root.Status != "active" || root.Name != "/" || root.Parent != nil {
			t.Errorf("root = %+v; want it active", root)
		}
		p := webtest.Decode[directoryPage](t, c.Get(t, docs+"/directories/root/directories"), http.StatusOK)
		if len(p.Items) != 1 || p.Items[0].Status != "active" {
			t.Errorf("root's directories = %+v; want reports, active", p.Items)
		}
	})

	run("a deleting branch reads by id and lists as not found", func(t *testing.T, docs string) {
		reports := webtest.Decode[identity](t, c.Post(t, docs+"/directories", map[string]string{"parent_id": "root", "name": "reports"}), http.StatusCreated)
		archive := webtest.Decode[identity](t, c.Post(t, docs+"/directories", map[string]string{"parent_id": reports.ID, "name": "archive"}), http.StatusCreated)
		q3 := webtest.Decode[identity](t, c.Put(t, docs+"/directories/"+reports.ID+"/files/q3.txt", webtest.Raw{ContentType: "text/plain", Body: []byte("report")}), http.StatusCreated)
		_ = webtest.Decode[identity](t, c.Put(t, docs+"/directories/root/files/kept.txt", webtest.Raw{ContentType: "text/plain", Body: []byte("kept")}), http.StatusCreated)
		if f := webtest.Decode[filePage](t, c.Get(t, docs+"/directories/"+reports.ID+"/files"), http.StatusOK); len(f.Items) != 1 || f.Items[0].Status != "available" {
			t.Fatalf("reports' files before the mark = %+v", f.Items)
		}

		markDeleting(t, reports.ID)

		for id, name := range map[string]string{reports.ID: "reports", archive.ID: "archive"} {
			d := webtest.Decode[directory](t, c.Get(t, docs+"/directories/"+id), http.StatusOK)
			if d.Status != "deleting" || d.Name != name {
				t.Errorf("%s = %+v; want it deleting", name, d)
			}
			_ = c.Get(t, docs+"/directories/"+id+"/directories").Problem(t, http.StatusNotFound)
			_ = c.Get(t, docs+"/directories/"+id+"/files").Problem(t, http.StatusNotFound)
		}
		if f := webtest.Decode[file](t, c.Get(t, docs+"/files/"+q3.ID), http.StatusOK); f.Status != "deleting" {
			t.Errorf("q3 = %+v; want it deleting", f)
		}
		// The branch is hidden from its parent's listing, which still
		// lists the rest.
		if p := webtest.Decode[directoryPage](t, c.Get(t, docs+"/directories/root/directories"), http.StatusOK); len(p.Items) != 0 || p.Total == nil || *p.Total != 0 {
			t.Errorf("root's directories = %+v; want the deleting branch hidden", p)
		}
		if f := webtest.Decode[filePage](t, c.Get(t, docs+"/directories/root/files"), http.StatusOK); len(f.Items) != 1 || f.Items[0].Name != "kept.txt" {
			t.Errorf("root's files = %+v; want kept.txt alone", f.Items)
		}
	})

	run("a conflict carries its curated detail", func(t *testing.T, docs string) {
		reports := webtest.Decode[identity](t, c.Post(t, docs+"/directories", map[string]string{"parent_id": "root", "name": "reports"}), http.StatusCreated)
		archive := webtest.Decode[identity](t, c.Post(t, docs+"/directories", map[string]string{"parent_id": reports.ID, "name": "archive"}), http.StatusCreated)
		q3 := webtest.Raw{ContentType: "text/plain", Body: []byte("report")}
		_ = webtest.Decode[identity](t, c.Put(t, docs+"/directories/"+reports.ID+"/files/q3.txt", q3), http.StatusCreated)

		conflict(t, c.Put(t, docs+"/directories/"+reports.ID+"/files/q3.txt", q3), "an entry with that name already exists")
		conflict(t, c.Post(t, docs+"/directories", map[string]string{"parent_id": reports.ID, "name": "archive"}), "an entry with that name already exists")
		conflict(t, c.Delete(t, docs+"/directories/"+reports.ID), "the directory is not empty")

		markDeleting(t, archive.ID)
		conflict(t, c.Post(t, docs+"/directories", map[string]string{"parent_id": archive.ID, "name": "q4"}), "the directory is being deleted")
		conflict(t, c.Put(t, docs+"/directories/"+archive.ID+"/files/q4.txt", q3), "the directory is being deleted")
	})
}

// markDeleting marks the branch rooted at the directory with id deleting
// through blobfs's store, over a pool of its own to the database the
// service runs against, since the API has no mark path yet: the recursive
// delete that marks a branch arrives with the asynchronous delete. The
// pool is go-database's, configured as the service's is: config.json's
// name and user, the compose password, and the APP_DATABASE_* overrides
// the harness passes the service.
func markDeleting(t *testing.T, id string) {
	t.Helper()
	ctx := context.Background()
	cfg := database.Config{Host: "127.0.0.1", Name: "app", User: "app", Password: "app", Options: map[string]string{"sslmode": "disable"}}
	if err := cfg.Finalize("app"); err != nil {
		t.Fatal(err)
	}
	pool, err := postgres.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = pool.Shutdown(ctx) }()
	db := sqlate.Wrap(pool.Conn(), pgdialect.Dialect{})
	catalog := query.MustCatalog(query.Patterns(), bfdata.Patterns())
	fs, err := bfdata.New(catalog, db.Dialect(), bfdata.WithEngine(blobfspg.Engine))
	if err != nil {
		t.Fatal(err)
	}
	tx, err := db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fs.Directories.MarkDeleting(ctx, tx, id); err != nil {
		_ = tx.Rollback()
		t.Fatalf("mark %s: %v", id, err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
}
