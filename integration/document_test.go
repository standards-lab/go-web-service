//go:build integration

package integration_test

import (
	"net/http"
	"testing"

	"github.com/standards-lab/go-core/process/processtest"
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

// The document API's contract, the deleting state included. The object
// store is relayed through a forwarder, so a case that asserts a marked
// branch holds it: with the store severed, the sweep the delete nudges is
// refused each file's object, and a directory holding a file stays
// deleting (the sweep's own cases are TestDocumentSweep's).
func TestDocument(t *testing.T) {
	f := processtest.Forward(t, integration.StorageAddr())
	s := integration.Start(t, integration.Options{Seed: integration.Default, Storage: f.Addr()})
	c := s.Client()
	hold := func(t *testing.T) {
		f.Sever()
		t.Cleanup(func() { f.Restore(t) })
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
