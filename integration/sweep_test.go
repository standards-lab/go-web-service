//go:build integration

package integration_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/standards-lab/go-core/process/processtest"
	"github.com/standards-lab/go-storage"
	"github.com/standards-lab/go-web-sdk/webtest"

	"github.com/standards-lab/go-web-service/integration"
)

// refusedRecord is the sweep reactor's record of a pass that returned
// refusals, as the JSON log carries it.
const refusedRecord = `"msg":"sweep pass refused"`

// The sweep reactor removes what a recursive delete marks, through the
// real API and the real stores. The service sweeps in passes of two
// records, so one wake runs several passes while each reports More, and
// its object store is relayed through a forwarder a case severs to have
// the sweep refused. Every wake here is the delete's own nudge, or a
// repeated delete's, never the interval.
func TestDocumentSweep(t *testing.T) {
	f := processtest.Forward(t, integration.StorageAddr())
	s := integration.Start(t, integration.Options{
		Seed:    integration.Default,
		Storage: f.Addr(),
		Env:     []string{"APP_SWEEP_BATCH=2"},
	})
	c := s.Client()
	objects := integration.Objects(t)

	run := func(name string, fn func(t *testing.T, docs string)) {
		t.Run(name, func(t *testing.T) {
			integration.Reset(t, c, integration.Default)
			fn(t, "/api/documents/"+tree(t, c)["acme"].ID)
		})
	}
	mkdir := func(t *testing.T, docs, parent, name string) identity {
		t.Helper()
		return webtest.Decode[identity](t, c.Post(t, docs+"/directories", map[string]string{"parent_id": parent, "name": name}), http.StatusCreated)
	}
	// put uploads a file and returns its object key, blobfs's id/name.
	put := func(t *testing.T, docs, dir, name string) string {
		t.Helper()
		id := webtest.Decode[identity](t, c.Put(t, docs+"/directories/"+dir+"/files/"+name, webtest.Raw{ContentType: "text/plain", Body: []byte(name)}), http.StatusCreated)
		return id.ID + "/" + name
	}
	stored := func(t *testing.T, key string) bool {
		t.Helper()
		_, err := objects.Stat(context.Background(), key)
		if err != nil && !errors.Is(err, storage.ErrNotFound) {
			t.Fatalf("stat %s: %v", key, err)
		}
		return err == nil
	}
	// swept waits until every path given reads 404 and asserts that no
	// key given is left in the store.
	swept := func(t *testing.T, paths, keys []string) {
		t.Helper()
		processtest.WaitFor(t, "the branch swept", func() bool {
			for _, p := range paths {
				if c.Get(t, p).Status != http.StatusNotFound {
					return false
				}
			}
			return true
		})
		for _, k := range keys {
			if stored(t, k) {
				t.Errorf("object %s outlived its sweep", k)
			}
		}
	}

	run("a marked branch is swept, rows and objects", func(t *testing.T, docs string) {
		reports := mkdir(t, docs, "root", "reports")
		archive := mkdir(t, docs, reports.ID, "archive")
		old := mkdir(t, docs, archive.ID, "old")
		keys := []string{
			put(t, docs, reports.ID, "q3.txt"), put(t, docs, reports.ID, "q4.txt"),
			put(t, docs, archive.ID, "a.txt"), put(t, docs, old.ID, "b.txt"),
		}
		kept := put(t, docs, "root", "kept.txt")
		for _, k := range append(keys, kept) {
			if !stored(t, k) {
				t.Fatalf("object %s not stored by its upload", k)
			}
		}

		// Seven records in passes of two: one wake finishes them in four.
		markBranch(t, c, docs, reports.ID)
		paths := []string{docs + "/directories/" + reports.ID, docs + "/directories/" + archive.ID, docs + "/directories/" + old.ID}
		for _, k := range keys {
			paths = append(paths, docs+"/files/"+strings.Split(k, "/")[0])
		}
		swept(t, paths, keys)

		if p := webtest.Decode[directoryPage](t, c.Get(t, docs+"/directories/root/directories"), http.StatusOK); len(p.Items) != 0 {
			t.Errorf("root's directories after the sweep = %+v; want none", p.Items)
		}
		if fp := webtest.Decode[filePage](t, c.Get(t, docs+"/directories/root/files"), http.StatusOK); len(fp.Items) != 1 || fp.Items[0].Name != "kept.txt" {
			t.Errorf("root's files after the sweep = %+v; want kept.txt alone", fp.Items)
		}
		if !stored(t, kept) {
			t.Error("the sweep deleted an object outside the branch")
		}
	})

	run("a refused pass leaves the branch, and a later pass converges", func(t *testing.T, docs string) {
		reports := mkdir(t, docs, "root", "reports")
		archive := mkdir(t, docs, reports.ID, "archive")
		key := put(t, docs, reports.ID, "q3.txt")
		refusals := strings.Count(s.Output(), refusedRecord)

		// The store severed, the pass the mark nudges is refused q3's
		// object: the refusal is logged, not a failure, so the service
		// runs on with the branch left deleting behind the file. The empty
		// archive beneath it is removed all the same.
		f.Sever()
		restored := false
		t.Cleanup(func() {
			if !restored {
				f.Restore(t)
			}
		})
		markBranch(t, c, docs, reports.ID)
		s.Await(t, "the sweep's refusal logged", func() bool {
			return strings.Count(s.Output(), refusedRecord) > refusals
		})
		c.Get(t, "/healthz").Expect(t, http.StatusOK)
		if d := webtest.Decode[directory](t, c.Get(t, docs+"/directories/"+reports.ID), http.StatusOK); d.Status != "deleting" {
			t.Errorf("reports after the refused pass = %+v; want it deleting", d)
		}
		_ = c.Get(t, docs+"/directories/"+archive.ID).Problem(t, http.StatusNotFound)

		// The store back, the repeated delete is accepted and nudges the
		// sweep again, which finishes what the refused pass left.
		f.Restore(t)
		restored = true
		c.Delete(t, docs+"/directories/"+reports.ID+"?recursive=true", webtest.IfMatch(reports.Version)).Expect(t, http.StatusAccepted)
		swept(t, []string{docs + "/directories/" + reports.ID}, []string{key})
	})

	run("the root's recursive delete unbinds and removes it", func(t *testing.T, docs string) {
		reports := mkdir(t, docs, "root", "reports")
		keys := []string{put(t, docs, reports.ID, "q3.txt"), put(t, docs, "root", "readme.txt")}
		root := webtest.Decode[directory](t, c.Get(t, docs+"/directories/root"), http.StatusOK)

		// The sweep removes the root last, and its owner row goes with it
		// through the row's cascading foreign key, so the organization
		// reads as one without a root: the alias is not found, and its
		// listing is empty.
		markBranch(t, c, docs, "root")
		swept(t, []string{docs + "/directories/root", docs + "/directories/" + root.ID, docs + "/directories/" + reports.ID}, keys)
		if p := webtest.Decode[directoryPage](t, c.Get(t, docs+"/directories/root/directories"), http.StatusOK); len(p.Items) != 0 || p.Total == nil || *p.Total != 0 {
			t.Errorf("the rootless organization's listing = %+v; want an empty page", p)
		}

		// The next write ensures a new root.
		mkdir(t, docs, "root", "reports")
		if again := webtest.Decode[directory](t, c.Get(t, docs+"/directories/root"), http.StatusOK); again.ID == root.ID || again.Status != "active" {
			t.Errorf("root after the next write = %+v; want a new, active root", again)
		}
	})
}

// A branch a process marked but never swept is swept at the next
// process's start, not an interval later: the sweep's wake is nudged at
// construction. The first process marks the branch with its object store
// severed, so its sweep is refused and leaves the branch deleting, and
// stops. The second starts with the store back and an interval of an
// hour, so only the startup wake can sweep the branch within the wait.
func TestSweepAtStartup(t *testing.T) {
	f := processtest.Forward(t, integration.StorageAddr())
	opts := integration.Options{
		Seed:    integration.Default,
		Storage: f.Addr(),
		Env:     []string{"APP_SWEEP_INTERVAL=1h"},
	}
	first := integration.Start(t, opts)
	c := first.Client()
	integration.Reset(t, c, integration.Default)
	docs := "/api/documents/" + tree(t, c)["acme"].ID
	reports := webtest.Decode[identity](t, c.Post(t, docs+"/directories", map[string]string{"parent_id": "root", "name": "reports"}), http.StatusCreated)
	file := webtest.Decode[identity](t, c.Put(t, docs+"/directories/"+reports.ID+"/files/q3.txt", webtest.Raw{ContentType: "text/plain", Body: []byte("q3")}), http.StatusCreated)
	key := file.ID + "/q3.txt"
	refusals := strings.Count(first.Output(), refusedRecord)

	f.Sever()
	restored := false
	t.Cleanup(func() {
		if !restored {
			f.Restore(t)
		}
	})
	markBranch(t, c, docs, reports.ID)
	first.Await(t, "the sweep's refusal logged", func() bool {
		return strings.Count(first.Output(), refusedRecord) > refusals
	})
	if code := first.Stop(t); code != 0 {
		t.Fatalf("first process exit = %d:\n%s", code, first.Output())
	}

	f.Restore(t)
	restored = true
	second := integration.Start(t, opts)
	c = second.Client()
	processtest.WaitFor(t, "the branch swept at startup", func() bool {
		return c.Get(t, docs+"/directories/"+reports.ID).Status == http.StatusNotFound
	})
	if _, err := integration.Objects(t).Stat(context.Background(), key); !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("stat %s after the startup sweep = %v; want not found", key, err)
	}
}

// A reset never meets a sweep pass: the reset holds the process's quiesce
// gate exclusively and each pass holds it shared, so a pass in flight
// finishes before the revert begins and none starts until the reset is
// done. Unguarded, the two deadlock: the revert of organization_directory
// and a pass's cascading directory removal lock the two tables in the
// opposite order, the reset fails with SQLSTATE 40P01, and a pass that
// runs while the tables are dropped is refused on every statement. Each
// round marks a branch of many directories, whose removals are the
// cascading deletes, so the mark's nudge has the sweep working in passes
// of five, and resets at a staggered offset into that work. The rounds run
// in one process, since the startup sweep and a nudged one are the same
// pass; a round costs well under a second.
func TestSweepUnderReset(t *testing.T) {
	s := integration.Start(t, integration.Options{
		Seed: integration.Default,
		Env:  []string{"APP_SWEEP_BATCH=5", "APP_RATE_LIMIT_REQUESTS=100000"},
	})
	c := s.Client()
	refusals := strings.Count(s.Output(), refusedRecord)
	for round := range 15 {
		integration.Reset(t, c, integration.Default)
		docs := "/api/documents/" + tree(t, c)["acme"].ID
		branch := webtest.Decode[identity](t, c.Post(t, docs+"/directories", map[string]string{"parent_id": "root", "name": "branch"}), http.StatusCreated)
		for i := range 40 {
			c.Post(t, docs+"/directories", map[string]string{"parent_id": branch.ID, "name": fmt.Sprint("d", i)}).Expect(t, http.StatusCreated)
		}
		markBranch(t, c, docs, branch.ID)
		time.Sleep(time.Duration(round%5) * 5 * time.Millisecond)
		// The reset under test: Reset fails the round on anything but 200.
		integration.Reset(t, c, integration.Default)
	}
	if n := strings.Count(s.Output(), refusedRecord) - refusals; n != 0 {
		t.Errorf("%d sweep passes were refused across the resets; want none to run under one", n)
	}
}
