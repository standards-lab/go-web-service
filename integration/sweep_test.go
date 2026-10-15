//go:build integration

package integration_test

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/standards-lab/go-core/process/processtest"
	"github.com/standards-lab/go-database/sweep"
	"github.com/standards-lab/go-storage"
	"github.com/standards-lab/go-web-sdk/webtest"
	"github.com/standards-lab/sqlate"
	pgdialect "github.com/standards-lab/sqlate/postgres"

	"github.com/standards-lab/go-web-service/data"
	"github.com/standards-lab/go-web-service/integration"
)

// refusedRecord is the sweeper's record of a files pass that returned
// refusals, as the JSON log carries it: go-database's default report of a
// pass's error, at warn.
var refusedRecord = regexp.MustCompile(`"msg":"sweep pass failed".*"reclaimer":"files"`)

// refused counts the refused files passes in a process's output.
func refused(output string) int { return len(refusedRecord.FindAllStringIndex(output, -1)) }

// The sweeper's files reclaimer removes what a recursive delete marks,
// through the real API and the real stores. The service sweeps in passes of
// two records, so one cycle runs several passes while each reports More,
// and its object store is relayed through a forwarder a case severs to have
// the sweep refused. Every cycle here is the delete's own nudge, or a
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
			fn(t, "/api/documents/"+tree(t, c)[docsOrg].ID)
		})
	}
	mkdir := func(t *testing.T, docs, parent, name string) identity {
		t.Helper()
		return webtest.Decode[identity](t, c.Post(t, docs+"/directories", map[string]string{"parent_id": parent, "name": name}), http.StatusCreated)
	}
	// put uploads a file and returns its object key, blobfs's id/name.
	put := func(t *testing.T, docs, dir, name string) string {
		t.Helper()
		id := webtest.Decode[identity](t, c.Post(t, docs+"/directories/"+dir+"/files?name="+name, webtest.Raw{ContentType: "text/plain", Body: []byte(name)}), http.StatusCreated)
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

		// Seven records in passes of two: one cycle finishes them in four.
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
		refusals := refused(s.Output())

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
			return refused(s.Output()) > refusals
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
// process's start, not an interval later: every reclaimer is due at the
// sweeper's start. The first process marks the branch with its object store
// severed, so its sweep is refused and leaves the branch deleting, and
// stops. The second starts with the store back and an interval of an
// hour, so only the startup cycle can sweep the branch within the wait.
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
	docs := "/api/documents/" + tree(t, c)[docsOrg].ID
	reports := webtest.Decode[identity](t, c.Post(t, docs+"/directories", map[string]string{"parent_id": "root", "name": "reports"}), http.StatusCreated)
	file := webtest.Decode[identity](t, c.Post(t, docs+"/directories/"+reports.ID+"/files?name=q3.txt", webtest.Raw{ContentType: "text/plain", Body: []byte("q3")}), http.StatusCreated)
	key := file.ID + "/q3.txt"
	refusals := refused(first.Output())

	f.Sever()
	restored := false
	t.Cleanup(func() {
		if !restored {
			f.Restore(t)
		}
	})
	markBranch(t, c, docs, reports.ID)
	first.Await(t, "the sweep's refusal logged", func() bool {
		return refused(first.Output()) > refusals
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
	refusals := refused(s.Output())
	for round := range 15 {
		integration.Reset(t, c, integration.Default)
		docs := "/api/documents/" + tree(t, c)[docsOrg].ID
		branch := webtest.Decode[identity](t, c.Post(t, docs+"/directories", map[string]string{"parent_id": "root", "name": "branch"}), http.StatusCreated)
		for i := range 40 {
			c.Post(t, docs+"/directories", map[string]string{"parent_id": branch.ID, "name": fmt.Sprint("d", i)}).Expect(t, http.StatusCreated)
		}
		markBranch(t, c, docs, branch.ID)
		time.Sleep(time.Duration(round%5) * 5 * time.Millisecond)
		// The reset under test: Reset fails the round on anything but 200.
		integration.Reset(t, c, integration.Default)
	}
	if n := refused(s.Output()) - refusals; n != 0 {
		t.Errorf("%d sweep passes were refused across the resets; want none to run under one", n)
	}
}

// holding is a reclaimer that records its passes: each pass counts itself,
// waits until hold is closed, and notes any pass of another holding
// reclaimer on the same active count that ran at the same time.
type holding struct {
	hold    <-chan struct{}
	active  *atomic.Int32
	overlap *atomic.Bool
	passes  atomic.Int32
}

func (r *holding) Pass(ctx context.Context, _ int) (bool, error) {
	if r.active.Add(1) > 1 {
		r.overlap.Store(true)
	}
	defer r.active.Add(-1)
	r.passes.Add(1)
	select {
	case <-r.hold:
	case <-ctx.Done():
		return false, ctx.Err()
	}
	// Long enough for a pass of the other replica's, were it unclaimed,
	// to land inside this one.
	time.Sleep(5 * time.Millisecond)
	return false, nil
}

// Two replicas on one database never run the files reclaimer's cycle at
// once: each sweeper, as the service builds it on Postgres, claims
// "sweep.files" (the lock registry's data.LockSweepPrefix) for its cycle,
// and a replica that finds the claim held runs no pass. The first
// sweeper's pass holds until the test releases it; while it does, the
// second, nudged every tick, runs none, and a third session is refused the
// lock. Released, the second takes its turn, and the two then cycle on a
// short interval with no pass of one inside a pass of the other. Each
// sweeper has its own pool, as each replica would.
func TestSweepClaimAcrossReplicas(t *testing.T) {
	const interval = 50 * time.Millisecond
	var active atomic.Int32
	var overlap atomic.Bool
	holdFirst := make(chan struct{})
	released := make(chan struct{})
	close(released)
	first := &holding{hold: holdFirst, active: &active, overlap: &overlap}
	second := &holding{hold: released, active: &active, overlap: &overlap}

	// Each shutdown releases the first's pass first, so a failed assertion
	// leaves no pass holding the drain.
	releaseFirst := sync.OnceFunc(func() { close(holdFirst) })
	start := func(r sweep.Reclaimer) *sweep.Sweeper {
		t.Helper()
		sw, err := sweep.New(integration.Database(t), []sweep.Registration{
			{Name: "files", Reclaimer: r, Policy: sweep.Policy{Interval: interval, Budget: 1}},
		}, 10*time.Second, slog.New(slog.DiscardHandler))
		if err != nil {
			t.Fatalf("sweep.New: %v", err)
		}
		if err := sw.Start(context.Background()); err != nil {
			t.Fatalf("Start: %v", err)
		}
		t.Cleanup(func() {
			releaseFirst()
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if err := sw.Shutdown(ctx); err != nil {
				t.Errorf("Shutdown: %v", err)
			}
		})
		return sw
	}
	start(first)
	processtest.WaitFor(t, "the first replica's pass", func() bool { return first.passes.Load() > 0 })

	other := start(second)
	for range 10 {
		other.Nudge("files")
		time.Sleep(interval)
	}
	if n := second.passes.Load(); n != 0 {
		t.Fatalf("the second replica ran %d passes while the first held the claim; want none", n)
	}
	ctx := context.Background()
	conn, err := integration.Database(t).Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	took, err := pgdialect.Dialect{}.TryLock(ctx, conn, data.LockSweepPrefix+"files")
	_ = conn.Close()
	if err != nil || took {
		t.Fatalf("a third session's TryLock of the claim = (%v, %v); want it refused while the first holds it", took, err)
	}

	releaseFirst()
	processtest.WaitFor(t, "the second replica's pass", func() bool { return second.passes.Load() > 0 })
	for range 10 {
		other.Nudge("files")
		time.Sleep(interval)
	}
	if overlap.Load() {
		t.Error("a pass of one replica ran inside a pass of the other")
	}
}

// exec runs one statement on db beneath the API, failing the test on its
// error.
func exec(t *testing.T, db *sqlate.DB, query string, args ...any) {
	t.Helper()
	if _, err := db.ExecContext(context.Background(), query, args...); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
}

// holds reports whether db holds a row the query, a SELECT 1 over one
// table, finds.
func holds(t *testing.T, db *sqlate.DB, query string, args ...any) bool {
	t.Helper()
	rows, err := db.QueryContext(context.Background(), query, args...)
	if err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	defer func() { _ = rows.Close() }()
	held := rows.Next()
	if err := rows.Err(); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	return held
}

// The sweeper's outbox and inbox reclaimers delete the messaging rows past
// their ages on the running service, and no other. The service runs with
// both ages at an hour and an interval of 100ms, so a row the test
// backdates two hours with SQL is reclaimed within a few intervals while
// every row it leaves younger, or never published, outlives many. Rows are
// told apart by event id, or, in the inbox, by a consumer of the test's
// own. Each case waits for its aged row to go, so the pass that removed it
// ran with the rows it must keep in place; one statement weighs them all.
func TestSweepRetention(t *testing.T) {
	f := processtest.Forward(t, integration.NATSAddr())
	s := integration.Start(t, integration.Options{
		Seed: integration.Default,
		NATS: f.Addr(),
		Env: []string{
			"APP_SWEEP_INTERVAL=100ms", "APP_SWEEP_OUTBOX_AGE=1h", "APP_SWEEP_INBOX_AGE=1h",
			// A publish to the severed broker gives up within a second.
			"APP_MESSAGING_RELAY_TIMEOUT=1s",
		},
	})
	db := integration.Database(t)
	drained(t, db)

	t.Run("handled inbox rows past the inbox age are deleted", func(t *testing.T) {
		const consumer = "integration-sweep-retention"
		t.Cleanup(func() { exec(t, db, "DELETE FROM messaging_inbox WHERE consumer = $1", consumer) })
		held := func(id string) bool {
			return holds(t, db, "SELECT 1 FROM messaging_inbox WHERE consumer = $1 AND id = $2", consumer, id)
		}
		insert := "INSERT INTO messaging_inbox (consumer, source, id, handled_at) VALUES ($1, 'web-service', $2, now() - $3::interval)"
		exec(t, db, insert, consumer, "recent", "1 minute")
		exec(t, db, insert, consumer, "aged", "2 hours")

		processtest.WaitFor(t, "the aged inbox row deleted", func() bool { return !held("aged") })
		if !held("recent") {
			t.Error("the inbox row handled a minute ago was deleted; want it kept until the inbox age")
		}
	})

	t.Run("published outbox rows past the outbox age are deleted", func(t *testing.T) {
		// Published: the relay publishes both, then the test backdates
		// one's publication past the age.
		aged, recent := emitProbe(t, db), emitProbe(t, db)
		drained(t, db)

		// Unpublished: NATS severed, the relay cannot publish the probe,
		// whose row is created two hours ago.
		f.Sever()
		restored := false
		t.Cleanup(func() {
			if !restored {
				f.Restore(t)
			}
		})
		unpublished := emitProbe(t, db)
		exec(t, db, "UPDATE messaging_outbox SET created_at = now() - interval '2 hours' WHERE id = $1", unpublished)

		// Quarantined: a row the relay set aside two hours ago, written
		// directly, as no route can make one.
		quarantined := fmt.Sprintf("quarantined-%d", time.Now().UnixNano())
		exec(t, db, `INSERT INTO messaging_outbox (source, id, header, created_at, quarantined_at, quarantine_reason)
			VALUES ('web-service', $1, '{}', now() - interval '2 hours', now() - interval '2 hours', 'integration: quarantined')`, quarantined)
		t.Cleanup(func() {
			exec(t, db, "DELETE FROM messaging_outbox WHERE id IN ($1, $2)", unpublished, quarantined)
		})

		exec(t, db, "UPDATE messaging_outbox SET published_at = now() - interval '2 hours' WHERE id = $1", aged)
		processtest.WaitFor(t, "the aged published outbox row deleted", func() bool { return !outboxHolds(t, db, aged) })

		if !outboxHolds(t, db, recent) {
			t.Error("the outbox row published just now was deleted; want it kept until the outbox age")
		}
		if !holds(t, db, "SELECT 1 FROM messaging_outbox WHERE id = $1 AND published_at IS NULL", unpublished) {
			t.Error("the unpublished outbox row is gone or published; want it kept, awaiting publication")
		}
		if !outboxHolds(t, db, quarantined) {
			t.Error("the quarantined outbox row was deleted; want it kept")
		}
		f.Restore(t)
		restored = true
	})

	if code := s.Stop(t); code != 0 {
		t.Fatalf("exit = %d:\n%s", code, s.Output())
	}
}
