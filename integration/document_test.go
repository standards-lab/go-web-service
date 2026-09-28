//go:build integration

package integration_test

import (
	"net/http"
	"testing"

	"github.com/standards-lab/go-web-sdk/webtest"

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

	run("the deletes guard on If-Match", func(t *testing.T, docs string) {
		reports := webtest.Decode[identity](t, c.Post(t, docs+"/directories", map[string]string{"parent_id": "root", "name": "reports"}), http.StatusCreated)
		q3 := webtest.Decode[identity](t, c.Put(t, docs+"/directories/"+reports.ID+"/files/q3.txt", webtest.Raw{ContentType: "text/plain", Body: []byte("report")}), http.StatusCreated)

		_ = c.Delete(t, docs+"/files/"+q3.ID).Problem(t, http.StatusPreconditionRequired)
		_ = c.Delete(t, docs+"/files/"+q3.ID, webtest.IfMatch(q3.Version-1)).Problem(t, http.StatusPreconditionFailed)
		_ = c.Delete(t, docs+"/directories/"+reports.ID).Problem(t, http.StatusPreconditionRequired)
		_ = c.Delete(t, docs+"/directories/"+reports.ID+"?recursive=true").Problem(t, http.StatusPreconditionRequired)
		_ = c.Delete(t, docs+"/directories/"+reports.ID+"?recursive=true", webtest.IfMatch(reports.Version+1)).Problem(t, http.StatusPreconditionFailed)
		if d := webtest.Decode[directory](t, c.Get(t, docs+"/directories/"+reports.ID), http.StatusOK); d.Status != "active" {
			t.Errorf("reports after a stale recursive delete = %+v; want it active", d)
		}

		c.Delete(t, docs+"/files/"+q3.ID, webtest.IfMatch(q3.Version)).Expect(t, http.StatusNoContent)
		_ = c.Get(t, docs+"/files/"+q3.ID).Problem(t, http.StatusNotFound)
		_ = c.Delete(t, docs+"/directories/"+reports.ID, webtest.IfMatch(reports.Version+1)).Problem(t, http.StatusPreconditionFailed)
		c.Delete(t, docs+"/directories/"+reports.ID, webtest.IfMatch(reports.Version)).Expect(t, http.StatusNoContent)
		_ = c.Get(t, docs+"/directories/"+reports.ID).Problem(t, http.StatusNotFound)
	})

	run("a recursive delete is accepted and repeats", func(t *testing.T, docs string) {
		reports := webtest.Decode[identity](t, c.Post(t, docs+"/directories", map[string]string{"parent_id": "root", "name": "reports"}), http.StatusCreated)
		deleteBranch(t, c, docs, reports.ID)
		// The client's version is the one before the mark; the retry is
		// accepted at it all the same.
		r := c.Delete(t, docs+"/directories/"+reports.ID+"?recursive=true", webtest.IfMatch(reports.Version)).Expect(t, http.StatusAccepted)
		if loc := r.Header.Get("Location"); loc != docs+"/directories/"+reports.ID {
			t.Errorf("retry Location = %q", loc)
		}
		if d := webtest.Decode[directory](t, c.Get(t, docs+"/directories/"+reports.ID), http.StatusOK); d.Status != "deleting" || d.Version != reports.Version+1 {
			t.Errorf("reports = %+v; want it deleting, a version on", d)
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

		deleteBranch(t, c, docs, reports.ID)

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
		conflict(t, c.Delete(t, docs+"/directories/"+reports.ID, webtest.IfMatch(reports.Version)), "the directory is not empty")

		deleteBranch(t, c, docs, archive.ID)
		conflict(t, c.Post(t, docs+"/directories", map[string]string{"parent_id": archive.ID, "name": "q4"}), "the directory is being deleted")
		conflict(t, c.Put(t, docs+"/directories/"+archive.ID+"/files/q4.txt", q3), "the directory is being deleted")
	})
}

// deleteBranch deletes the directory with id recursively at the version
// it reads at, and asserts the 202 and its Location, the directory's read,
// which reports it deleting: the sweep that removes the branch is not
// wired yet, so the branch stays marked for the rest of the case.
func deleteBranch(t *testing.T, c *webtest.Client, docs, id string) {
	t.Helper()
	d := webtest.Decode[directory](t, c.Get(t, docs+"/directories/"+id), http.StatusOK)
	r := c.Delete(t, docs+"/directories/"+id+"?recursive=true", webtest.IfMatch(d.Version)).Expect(t, http.StatusAccepted)
	loc := r.Header.Get("Location")
	if loc != docs+"/directories/"+id || len(r.Body) != 0 {
		t.Fatalf("202 Location %q, body %q; want the directory's read and no body", loc, r.Body)
	}
	if marked := webtest.Decode[directory](t, c.Get(t, loc), http.StatusOK); marked.Status != "deleting" {
		t.Fatalf("%s after the delete = %+v; want it deleting", loc, marked)
	}
}
