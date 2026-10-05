//go:build integration

package integration_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"testing"

	"github.com/standards-lab/go-core/process/processtest"
	"github.com/standards-lab/go-storage"
	"github.com/standards-lab/go-web-sdk/webtest"

	"github.com/standards-lab/go-web-service/integration"
)

// docsOrg is the organization whose documents the document and sweep cases
// work in: one the default state seeds no document tree for, so each case
// starts from an organization without a root. The seeded tree is acme's,
// and TestSeededStorage reads it.
const docsOrg = "finance"

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
	ID      string `json:"id"`
	Name    string `json:"name"`
	Status  string `json:"status"`
	Version int64  `json:"version"`
}

type directoryPage struct {
	Items []directory `json:"items"`
	Total *int        `json:"total"`
}

type filePage struct {
	Items []file `json:"items"`
	Total *int   `json:"total"`
}

// entryPage is either listing's page as the paging case reads it: each
// entry's name and the envelope's paging fields.
type entryPage struct {
	Items []struct {
		Name string `json:"name"`
	} `json:"items"`
	Page  int    `json:"page"`
	Total *int   `json:"total"`
	More  bool   `json:"more"`
	Next  string `json:"next"`
}

// names is the page's entries' names in order.
func (p entryPage) names() []string {
	out := make([]string, len(p.Items))
	for i, e := range p.Items {
		out[i] = e.Name
	}
	return out
}

// total is the page's counted total, or -1 when the page omitted it.
func (p entryPage) total() int {
	if p.Total == nil {
		return -1
	}
	return *p.Total
}

// The document API's contract, the deleting state included. The object
// store is relayed through a forwarder, so a case that asserts a marked
// branch holds it: with the store severed, the sweep the delete nudges is
// refused each file's object, and a directory holding a file stays
// deleting (the sweep's own cases are TestDocumentSweep's).
func TestDocument(t *testing.T) {
	f := processtest.Forward(t, integration.StorageAddr())
	s := integration.Start(t, integration.Options{Seed: integration.Default, Storage: f.Addr()})
	c := s.Client()
	objects := integration.Objects(t)
	// hold severs the store for the rest of the case, or until the case
	// calls the restore it returns.
	hold := func(t *testing.T) (restore func()) {
		f.Sever()
		restored := false
		t.Cleanup(func() {
			if !restored {
				f.Restore(t)
			}
		})
		return func() {
			f.Restore(t)
			restored = true
		}
	}

	run := func(name string, fn func(t *testing.T, docs string)) {
		t.Run(name, func(t *testing.T) {
			integration.Reset(t, c, integration.Default)
			fn(t, "/api/documents/"+tree(t, c)[docsOrg].ID)
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

	// Both listings page by number and by cursor: each walk, in pages of
	// two, reads every entry once in the default sort, by name, so the
	// pages are disjoint and together complete. A cursor continues only
	// the listing, sort, and paging mode that minted it.
	run("the listings page by number and by cursor", func(t *testing.T, docs string) {
		files := []string{"f0.txt", "f1.txt", "f2.txt", "f3.txt", "f4.txt"}
		dirs := []string{"d0", "d1", "d2"}
		for _, name := range files {
			c.Post(t, docs+"/directories/root/files?name="+name, webtest.Raw{ContentType: "text/plain", Body: []byte(name)}).Expect(t, http.StatusCreated)
		}
		for _, name := range dirs {
			c.Post(t, docs+"/directories", map[string]string{"parent_id": "root", "name": name}).Expect(t, http.StatusCreated)
		}

		cursors := map[string]string{}
		for listing, want := range map[string][]string{docs + "/directories/root/files": files, docs + "/directories/root/directories": dirs} {
			var numbered []string
			for n := 1; ; n++ {
				p := webtest.Decode[entryPage](t, c.Get(t, fmt.Sprintf("%s?size=2&page=%d", listing, n)), http.StatusOK)
				if p.Page != n || p.total() != len(want) || len(p.Items) > 2 {
					t.Errorf("%s page %d = %+v; want page %d of %d entries, at most two", listing, n, p, n, len(want))
				}
				numbered = append(numbered, p.names()...)
				if !p.More {
					break
				}
				if n > len(want) {
					t.Fatalf("%s reports more past page %d", listing, n)
				}
			}

			p := webtest.Decode[entryPage](t, c.Get(t, listing+"?size=2"), http.StatusOK)
			first := p
			walked := p.names()
			for p.More {
				if p.Next == "" {
					t.Fatalf("%s: a page with more carried no next: %+v", listing, p)
				}
				p = webtest.Decode[entryPage](t, c.Get(t, listing+"?size=2&cursor="+url.QueryEscape(p.Next)), http.StatusOK)
				if p.Page != 0 || p.total() != len(want) {
					t.Errorf("%s: a continued page = %+v; want no page number and the total", listing, p)
				}
				walked = append(walked, p.names()...)
			}
			if !slices.Equal(numbered, want) || !slices.Equal(walked, want) {
				t.Errorf("%s by number = %v, by cursor = %v; want %v", listing, numbered, walked, want)
			}
			cursors[listing] = first.Next

			// A page past the last is empty, not an error.
			if p := webtest.Decode[entryPage](t, c.Get(t, listing+"?size=2&page=9"), http.StatusOK); len(p.Items) != 0 || p.More {
				t.Errorf("%s page 9 = %+v; want an empty page", listing, p)
			}
			// A cursor with a page, one sent under another sort, and one
			// that is not a cursor at all are each the request's error.
			_ = c.Get(t, listing+"?page=2&cursor="+url.QueryEscape(first.Next)).Problem(t, http.StatusBadRequest)
			_ = c.Get(t, listing+"?size=2&sort=-name&cursor="+url.QueryEscape(first.Next)).Problem(t, http.StatusBadRequest)
			_ = c.Get(t, listing+"?cursor=not-a-cursor").Problem(t, http.StatusBadRequest)
		}
		// Nor does one listing's cursor continue the other's.
		_ = c.Get(t, docs+"/directories/root/directories?size=2&cursor="+url.QueryEscape(cursors[docs+"/directories/root/files"])).Problem(t, http.StatusBadRequest)
	})

	run("the deletes guard on If-Match", func(t *testing.T, docs string) {
		reports := webtest.Decode[identity](t, c.Post(t, docs+"/directories", map[string]string{"parent_id": "root", "name": "reports"}), http.StatusCreated)
		q3 := webtest.Decode[identity](t, c.Post(t, docs+"/directories/"+reports.ID+"/files?name=q3.txt", webtest.Raw{ContentType: "text/plain", Body: []byte("report")}), http.StatusCreated)

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

	run("the empty root's delete unbinds it", func(t *testing.T, docs string) {
		reports := webtest.Decode[identity](t, c.Post(t, docs+"/directories", map[string]string{"parent_id": "root", "name": "reports"}), http.StatusCreated)
		c.Delete(t, docs+"/directories/"+reports.ID, webtest.IfMatch(reports.Version)).Expect(t, http.StatusNoContent)
		root := webtest.Decode[directory](t, c.Get(t, docs+"/directories/root"), http.StatusOK)

		// The owner row goes with the root, so the organization reads as
		// one without a root: the alias is not found, and its listing is
		// an empty page.
		c.Delete(t, docs+"/directories/root", webtest.IfMatch(root.Version)).Expect(t, http.StatusNoContent)
		_ = c.Get(t, docs+"/directories/root").Problem(t, http.StatusNotFound)
		_ = c.Get(t, docs+"/directories/"+root.ID).Problem(t, http.StatusNotFound)
		if p := webtest.Decode[directoryPage](t, c.Get(t, docs+"/directories/root/directories"), http.StatusOK); len(p.Items) != 0 || p.Total == nil || *p.Total != 0 {
			t.Errorf("the rootless organization's listing = %+v; want an empty page", p)
		}

		// The next write ensures a new root.
		_ = webtest.Decode[identity](t, c.Post(t, docs+"/directories", map[string]string{"parent_id": "root", "name": "reports"}), http.StatusCreated)
		if again := webtest.Decode[directory](t, c.Get(t, docs+"/directories/root"), http.StatusOK); again.ID == root.ID || again.Status != "active" {
			t.Errorf("root after the next write = %+v; want a new, active root", again)
		}
	})

	run("a recursive delete is accepted and repeats", func(t *testing.T, docs string) {
		reports := webtest.Decode[identity](t, c.Post(t, docs+"/directories", map[string]string{"parent_id": "root", "name": "reports"}), http.StatusCreated)
		_ = webtest.Decode[identity](t, c.Post(t, docs+"/directories/"+reports.ID+"/files?name=q3.txt", webtest.Raw{ContentType: "text/plain", Body: []byte("report")}), http.StatusCreated)
		hold(t)
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
		q3 := webtest.Decode[identity](t, c.Post(t, docs+"/directories/"+reports.ID+"/files?name=q3.txt", webtest.Raw{ContentType: "text/plain", Body: []byte("report")}), http.StatusCreated)
		_ = webtest.Decode[identity](t, c.Post(t, docs+"/directories/"+archive.ID+"/files?name=q2.txt", webtest.Raw{ContentType: "text/plain", Body: []byte("report")}), http.StatusCreated)
		_ = webtest.Decode[identity](t, c.Post(t, docs+"/directories/root/files?name=kept.txt", webtest.Raw{ContentType: "text/plain", Body: []byte("kept")}), http.StatusCreated)
		if f := webtest.Decode[filePage](t, c.Get(t, docs+"/directories/"+reports.ID+"/files"), http.StatusOK); len(f.Items) != 1 || f.Items[0].Status != "available" {
			t.Fatalf("reports' files before the mark = %+v", f.Items)
		}

		hold(t)
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
		_ = webtest.Decode[identity](t, c.Post(t, docs+"/directories/"+reports.ID+"/files?name=q3.txt", q3), http.StatusCreated)

		conflict(t, c.Post(t, docs+"/directories/"+reports.ID+"/files?name=q3.txt", q3), "an entry with that name already exists")
		conflict(t, c.Post(t, docs+"/directories", map[string]string{"parent_id": reports.ID, "name": "archive"}), "an entry with that name already exists")
		conflict(t, c.Delete(t, docs+"/directories/"+reports.ID, webtest.IfMatch(reports.Version)), "the directory is not empty")

		q2 := webtest.Decode[identity](t, c.Post(t, docs+"/directories/"+archive.ID+"/files?name=q2.txt", q3), http.StatusCreated)
		q5 := webtest.Decode[identity](t, c.Post(t, docs+"/directories/"+reports.ID+"/files?name=q5.txt", q3), http.StatusCreated)
		hold(t)

		// A delete the severed store refuses has begun: the file is
		// deleting in an active directory, its own delete, not the
		// directory's.
		_ = c.Delete(t, docs+"/files/"+q5.ID, webtest.IfMatch(q5.Version)).Problem(t, http.StatusServiceUnavailable)
		if f := webtest.Decode[file](t, c.Get(t, docs+"/files/"+q5.ID), http.StatusOK); f.Status != "deleting" {
			t.Fatalf("q5 after the refused delete = %+v; want it deleting", f)
		}
		conflict(t, c.Post(t, docs+"/files/"+q5.ID+"/move", map[string]string{"directory_id": "root", "name": "q5.txt"}, webtest.IfMatch(q5.Version+1)), "the file is being deleted")

		deleteBranch(t, c, docs, archive.ID)
		conflict(t, c.Post(t, docs+"/directories", map[string]string{"parent_id": archive.ID, "name": "q4"}), "the directory is being deleted")
		conflict(t, c.Post(t, docs+"/directories/"+archive.ID+"/files?name=q4.txt", q3), "the directory is being deleted")
		// A file the branch's mark made deleting is the directory's.
		conflict(t, c.Post(t, docs+"/files/"+q2.ID+"/move", map[string]string{"directory_id": "root", "name": "q2.txt"}, webtest.IfMatch(q2.Version+1)), "the directory is being deleted")
	})

	// A file delete the severed store refuses has begun: the row is
	// deleting, a version on, and hidden from its directory's listing,
	// and the object is still stored. Once the store is back, the retry
	// at the version the client read is the same delete, which finishes:
	// the file reads 404 and its object is gone.
	run("a refused file delete is left deleting, and its retry finishes it", func(t *testing.T, docs string) {
		q3 := webtest.Decode[identity](t, c.Post(t, docs+"/directories/root/files?name=q3.txt", webtest.Raw{ContentType: "text/plain", Body: []byte("report")}), http.StatusCreated)
		key := q3.ID + "/q3.txt"
		restore := hold(t)

		_ = c.Delete(t, docs+"/files/"+q3.ID, webtest.IfMatch(q3.Version)).Problem(t, http.StatusServiceUnavailable)
		if f := webtest.Decode[file](t, c.Get(t, docs+"/files/"+q3.ID), http.StatusOK); f.Status != "deleting" || f.Version != q3.Version+1 {
			t.Fatalf("q3 after the refused delete = %+v; want it deleting, a version on", f)
		}
		if p := webtest.Decode[filePage](t, c.Get(t, docs+"/directories/root/files"), http.StatusOK); len(p.Items) != 0 {
			t.Errorf("root's files = %+v; want the deleting file hidden", p.Items)
		}
		_ = c.Get(t, docs+"/files/"+q3.ID+"/content").Problem(t, http.StatusNotFound)

		restore()
		if _, err := objects.Stat(context.Background(), key); err != nil {
			t.Fatalf("stat %s after the refused delete: %v; want the object still stored", key, err)
		}
		c.Delete(t, docs+"/files/"+q3.ID, webtest.IfMatch(q3.Version)).Expect(t, http.StatusNoContent)
		_ = c.Get(t, docs+"/files/"+q3.ID).Problem(t, http.StatusNotFound)
		if _, err := objects.Stat(context.Background(), key); !errors.Is(err, storage.ErrNotFound) {
			t.Errorf("stat %s after the retried delete = %v; want not found", key, err)
		}
		// The name is free again.
		c.Post(t, docs+"/directories/root/files?name=q3.txt", webtest.Raw{ContentType: "text/plain", Body: []byte("report")}).Expect(t, http.StatusCreated)
	})

	run("an upload is a POST, answered with its metadata's Location", func(t *testing.T, docs string) {
		r := c.Post(t, docs+"/directories/root/files?name=q3.txt", webtest.Raw{ContentType: "text/plain", Body: []byte("report")}).Expect(t, http.StatusCreated)
		created := webtest.Decode[identity](t, r, http.StatusCreated)
		if loc := r.Header.Get("Location"); loc != docs+"/files/"+created.ID {
			t.Errorf("Location = %q; want the file's metadata read", loc)
		}
		if f := webtest.Decode[file](t, c.Get(t, r.Header.Get("Location")), http.StatusOK); f.Name != "q3.txt" || f.Status != "available" {
			t.Errorf("the Location's read = %+v; want q3.txt, available", f)
		}
		_ = c.Post(t, docs+"/directories/root/files", webtest.Raw{ContentType: "text/plain", Body: []byte("report")}).Problem(t, http.StatusBadRequest)
		// The PUT to a name is no route.
		_ = c.Put(t, docs+"/directories/root/files/q4.txt", webtest.Raw{ContentType: "text/plain", Body: []byte("report")}).Problem(t, http.StatusNotFound)
	})

	run("an organization that does not exist lists as not found", func(t *testing.T, docs string) {
		absent := "/api/documents/" + absentID
		_ = c.Get(t, absent+"/directories/root/directories").Problem(t, http.StatusNotFound)
		_ = c.Get(t, absent+"/directories/root/files").Problem(t, http.StatusNotFound)
		// A real organization without a root still lists an empty page.
		if p := webtest.Decode[filePage](t, c.Get(t, docs+"/directories/root/files"), http.StatusOK); len(p.Items) != 0 || p.Total == nil || *p.Total != 0 {
			t.Errorf("the rootless organization's files = %+v; want an empty page", p)
		}
		// Its listing refuses what a real root's refuses.
		for _, bad := range []string{"?sort=nope", "?nope=1", "?cursor=past"} {
			_ = c.Get(t, docs+"/directories/root/files"+bad).Problem(t, http.StatusBadRequest)
			_ = c.Get(t, docs+"/directories/root/directories"+bad).Problem(t, http.StatusBadRequest)
		}
	})

	run("a download round-trips its bytes and revalidates", func(t *testing.T, docs string) {
		const body, contentType = "a,b\n1,2\n", "text/csv; charset=utf-8"
		report := webtest.Decode[identity](t, c.Post(t, docs+"/directories/root/files?name=report.csv", webtest.Raw{ContentType: contentType, Body: []byte(body)}), http.StatusCreated)
		content := docs + "/files/" + report.ID + "/content"

		r := c.Get(t, content).Expect(t, http.StatusOK)
		if string(r.Body) != body || r.Header.Get("Content-Type") != contentType || r.Header.Get("Content-Disposition") != `attachment; filename="report.csv"` {
			t.Errorf("download = %q, Content-Type %q, Content-Disposition %q", r.Body, r.Header.Get("Content-Type"), r.Header.Get("Content-Disposition"))
		}
		etag, modified := r.Header.Get("ETag"), r.Header.Get("Last-Modified")
		if etag == "" || modified == "" || r.Header.Get("Cache-Control") != "private, no-cache" {
			t.Fatalf("download headers = %v; want an ETag, a Last-Modified, and private, no-cache", r.Header)
		}

		for _, h := range []webtest.Header{{Name: "If-None-Match", Value: etag}, {Name: "If-Modified-Since", Value: modified}} {
			r := c.Get(t, content, h)
			if r.Status != http.StatusNotModified || len(r.Body) != 0 || r.Header.Get("ETag") != etag {
				t.Errorf("GET with %s = %d, body %q, ETag %q; want 304, no body, the same ETag", h.Name, r.Status, r.Body, r.Header.Get("ETag"))
			}
		}
		// The download's ETag is the object's, never a version.
		_ = c.Delete(t, docs+"/files/"+report.ID, webtest.Header{Name: "If-Match", Value: etag}).Problem(t, http.StatusBadRequest)
	})

	// Organization B's API reaches none of organization A's documents: an
	// A id at any B route answers exactly as an absent id does, and the
	// root alias resolves to each organization's own root.
	run("an organization reaches none of another's documents", func(t *testing.T, adocs string) {
		bdocs := "/api/documents/" + tree(t, c)["engineering"].ID
		mkdir := func(docs, parent, name string) identity {
			return webtest.Decode[identity](t, c.Post(t, docs+"/directories", map[string]string{"parent_id": parent, "name": name}), http.StatusCreated)
		}
		put := func(docs, dir, name string) identity {
			return webtest.Decode[identity](t, c.Post(t, docs+"/directories/"+dir+"/files?name="+name, webtest.Raw{ContentType: "text/plain", Body: []byte(name)}), http.StatusCreated)
		}
		reports, inbox := mkdir(adocs, "root", "reports"), mkdir(bdocs, "root", "inbox")
		archive := mkdir(adocs, reports.ID, "archive")
		q3, memo := put(adocs, reports.ID, "q3.txt"), put(bdocs, "root", "memo.txt")
		aRoot := webtest.Decode[directory](t, c.Get(t, adocs+"/directories/root"), http.StatusOK)
		bRoot := webtest.Decode[directory](t, c.Get(t, bdocs+"/directories/root"), http.StatusOK)
		if aRoot.ID == bRoot.ID {
			t.Fatalf("both organizations' root alias = %s; want a root each", aRoot.ID)
		}

		// Each attempt is sent with an A id, then with an absent one in
		// its place: both are 404 with the same problem.
		attempts := map[string]func(id string) *webtest.Response{
			"read a directory":     func(id string) *webtest.Response { return c.Get(t, bdocs+"/directories/"+id) },
			"read A's root":        func(id string) *webtest.Response { return c.Get(t, bdocs+"/directories/"+id) },
			"list its directories": func(id string) *webtest.Response { return c.Get(t, bdocs+"/directories/"+id+"/directories") },
			"list its files":       func(id string) *webtest.Response { return c.Get(t, bdocs+"/directories/"+id+"/files") },
			"create in a directory": func(id string) *webtest.Response {
				return c.Post(t, bdocs+"/directories", map[string]string{"parent_id": id, "name": "x"})
			},
			"upload into a directory": func(id string) *webtest.Response {
				return c.Post(t, bdocs+"/directories/"+id+"/files?name=x.txt", webtest.Raw{ContentType: "text/plain", Body: []byte("x")})
			},
			"move a directory": func(id string) *webtest.Response {
				return c.Post(t, bdocs+"/directories/"+id+"/move", map[string]string{"parent_id": "root", "name": "archive"}, webtest.IfMatch(archive.Version))
			},
			"move a directory into one": func(id string) *webtest.Response {
				return c.Post(t, bdocs+"/directories/"+inbox.ID+"/move", map[string]string{"parent_id": id, "name": "inbox"}, webtest.IfMatch(inbox.Version))
			},
			"delete a directory": func(id string) *webtest.Response {
				return c.Delete(t, bdocs+"/directories/"+id, webtest.IfMatch(archive.Version))
			},
			"delete a branch": func(id string) *webtest.Response {
				return c.Delete(t, bdocs+"/directories/"+id+"?recursive=true", webtest.IfMatch(reports.Version))
			},
			"read a file":     func(id string) *webtest.Response { return c.Get(t, bdocs+"/files/"+id) },
			"download a file": func(id string) *webtest.Response { return c.Get(t, bdocs+"/files/"+id+"/content") },
			"delete a file": func(id string) *webtest.Response {
				return c.Delete(t, bdocs+"/files/"+id, webtest.IfMatch(q3.Version))
			},
			"move a file": func(id string) *webtest.Response {
				return c.Post(t, bdocs+"/files/"+id+"/move", map[string]string{"directory_id": "root", "name": "q3.txt"}, webtest.IfMatch(q3.Version))
			},
			"move a file into a directory": func(id string) *webtest.Response {
				return c.Post(t, bdocs+"/files/"+memo.ID+"/move", map[string]string{"directory_id": id, "name": "memo.txt"}, webtest.IfMatch(memo.Version))
			},
		}
		target := map[string]string{
			"read A's root":    aRoot.ID,
			"move a directory": archive.ID, "delete a directory": archive.ID,
			"read a file": q3.ID, "download a file": q3.ID, "delete a file": q3.ID, "move a file": q3.ID,
		}
		for name, attempt := range attempts {
			id := reports.ID
			if tid, ok := target[name]; ok {
				id = tid
			}
			got := attempt(id).Problem(t, http.StatusNotFound)
			absent := attempt(absentID).Problem(t, http.StatusNotFound)
			if got.Title != absent.Title || got.Detail != absent.Detail || got.Type != absent.Type {
				t.Errorf("%s: %+v; want it as an absent id's, %+v", name, got, absent)
			}
		}

		// Nothing of either tree moved: A's reads as it was, and each root
		// lists its own organization's entries alone.
		if d := webtest.Decode[directory](t, c.Get(t, adocs+"/directories/"+archive.ID), http.StatusOK); d.Status != "active" || d.Parent == nil || *d.Parent != reports.ID {
			t.Errorf("A's archive = %+v; want it active under reports", d)
		}
		if r := c.Get(t, adocs+"/files/"+q3.ID+"/content").Expect(t, http.StatusOK); string(r.Body) != "q3.txt" {
			t.Errorf("A's q3 = %q", r.Body)
		}
		for docs, want := range map[string]string{adocs: "reports", bdocs: "inbox"} {
			if p := webtest.Decode[directoryPage](t, c.Get(t, docs+"/directories/root/directories"), http.StatusOK); len(p.Items) != 1 || p.Items[0].Name != want {
				t.Errorf("%s root's directories = %+v; want %s alone", docs, p.Items, want)
			}
		}
		if f := webtest.Decode[filePage](t, c.Get(t, bdocs+"/directories/root/files"), http.StatusOK); len(f.Items) != 1 || f.Items[0].Name != "memo.txt" {
			t.Errorf("B root's files = %+v; want memo.txt alone", f.Items)
		}
	})
}

// deleteBranch deletes the directory with id recursively, as markBranch
// does, and asserts its Location's read reports it deleting. The caller
// holds the sweep, the object store severed with a file in each directory
// the case reads, so the branch stays marked for the rest of the case.
func deleteBranch(t *testing.T, c *webtest.Client, docs, id string) {
	t.Helper()
	loc := markBranch(t, c, docs, id)
	if marked := webtest.Decode[directory](t, c.Get(t, loc), http.StatusOK); marked.Status != "deleting" {
		t.Fatalf("%s after the delete = %+v; want it deleting", loc, marked)
	}
}

// markBranch deletes the directory with id recursively at the version it
// reads at, and asserts the 202 and its Location, the directory's read,
// which it returns; the id may be the root's alias, whose Location names
// the root's id. The sweep the delete nudges may remove the branch at any
// moment after.
func markBranch(t *testing.T, c *webtest.Client, docs, id string) string {
	t.Helper()
	d := webtest.Decode[directory](t, c.Get(t, docs+"/directories/"+id), http.StatusOK)
	r := c.Delete(t, docs+"/directories/"+id+"?recursive=true", webtest.IfMatch(d.Version)).Expect(t, http.StatusAccepted)
	loc := r.Header.Get("Location")
	if loc != docs+"/directories/"+d.ID || len(r.Body) != 0 {
		t.Fatalf("202 Location %q, body %q; want the directory's read and no body", loc, r.Body)
	}
	return loc
}

// A document upload the severed store refuses is abandoned, and its
// abandon is refused too: the put fails, then so does the abandon's
// object delete, so the pending row is left deleting, not removed. The
// deleting row is hidden from the listing, as every listing hides one,
// yet it still holds its name, so the same upload answers 409, until the
// sweep's stale reclaim finishes the row once the store is back. The
// service runs a short interval and a one-second stale age, so the
// reclaim follows within the wait; the retry then stores the file.
func TestDocumentRefusedUpload(t *testing.T) {
	f := processtest.Forward(t, integration.StorageAddr())
	s := integration.Start(t, integration.Options{
		Seed:    integration.Default,
		Storage: f.Addr(),
		Env:     []string{"APP_SWEEP_INTERVAL=200ms", "APP_SWEEP_STALE_AGE=1s"},
	})
	c := s.Client()
	integration.Reset(t, c, integration.Default)
	docs := "/api/documents/" + tree(t, c)[docsOrg].ID
	q3 := webtest.Raw{ContentType: "text/plain", Body: []byte("report")}
	c.Post(t, docs+"/directories/root/files?name=kept.txt", q3).Expect(t, http.StatusCreated)
	objects := integration.Objects(t)
	before, err := objects.List(context.Background(), storage.ListOptions{})
	if err != nil {
		t.Fatalf("list the store: %v", err)
	}

	f.Sever()
	restored := false
	t.Cleanup(func() {
		if !restored {
			f.Restore(t)
		}
	})
	_ = slowPost(t, s, docs+"/directories/root/files?name=q3.txt", q3).Problem(t, http.StatusServiceUnavailable)
	if p := webtest.Decode[filePage](t, c.Get(t, docs+"/directories/root/files"), http.StatusOK); len(p.Items) != 1 || p.Items[0].Name != "kept.txt" {
		t.Errorf("root's files after the refused upload = %+v; want kept.txt alone", p.Items)
	}
	if after, err := objects.List(context.Background(), storage.ListOptions{}); err != nil || len(after.Objects) != len(before.Objects) {
		t.Errorf("the store after the refused upload holds %d objects (%v); want the %d before it", len(after.Objects), err, len(before.Objects))
	}

	// The hidden row holds the name, and blobfs refuses it as the deleting
	// row it is, not as a taken name the client cannot list: the retry is
	// refused in the begin's transaction, before any put, so it answers at
	// once.
	conflict(t, c.Post(t, docs+"/directories/root/files?name=q3.txt", q3), "the file is being deleted")

	f.Restore(t)
	restored = true
	s.Await(t, "the abandoned upload's row reclaimed", func() bool { return reclaimRecord.MatchString(s.Output()) })
	created := webtest.Decode[identity](t, c.Post(t, docs+"/directories/root/files?name=q3.txt", q3), http.StatusCreated)
	if r := c.Get(t, docs+"/files/"+created.ID+"/content").Expect(t, http.StatusOK); string(r.Body) != "report" {
		t.Errorf("the retried upload's download = %q", r.Body)
	}
}

// slowPost uploads body to path on a client of its own: with the store
// severed, the put fails once the provider's retries are spent, and the
// abandon's object delete after it, which together outlast the harness
// client's failsafe.
func slowPost(t *testing.T, s *integration.Service, path string, body webtest.Raw) *webtest.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, s.URL()+path, bytes.NewReader(body.Body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", body.ContentType)
	resp, err := (&http.Client{Timeout: 4 * processtest.Failsafe}).Do(req)
	if err != nil {
		t.Fatalf("POST %s: %v", path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("POST %s: read body: %v", path, err)
	}
	return &webtest.Response{Status: resp.StatusCode, Header: resp.Header, Body: raw}
}
