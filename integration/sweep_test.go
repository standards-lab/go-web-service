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

var refusedRecord = regexp.MustCompile(`"msg":"sweep pass failed".*"reclaimer":"files"`)

func refused(output string) int { return len(refusedRecord.FindAllStringIndex(output, -1)) }

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

		f.Restore(t)
		restored = true
		c.Delete(t, docs+"/directories/"+reports.ID+"?recursive=true", webtest.IfMatch(reports.Version)).Expect(t, http.StatusAccepted)
		swept(t, []string{docs + "/directories/" + reports.ID}, []string{key})
	})

	run("the root's recursive delete unbinds and removes it", func(t *testing.T, docs string) {
		reports := mkdir(t, docs, "root", "reports")
		keys := []string{put(t, docs, reports.ID, "q3.txt"), put(t, docs, "root", "readme.txt")}
		root := webtest.Decode[directory](t, c.Get(t, docs+"/directories/root"), http.StatusOK)

		markBranch(t, c, docs, "root")
		swept(t, []string{docs + "/directories/root", docs + "/directories/" + root.ID, docs + "/directories/" + reports.ID}, keys)
		if p := webtest.Decode[directoryPage](t, c.Get(t, docs+"/directories/root/directories"), http.StatusOK); len(p.Items) != 0 || p.Total == nil || *p.Total != 0 {
			t.Errorf("the rootless organization's listing = %+v; want an empty page", p)
		}

		mkdir(t, docs, "root", "reports")
		if again := webtest.Decode[directory](t, c.Get(t, docs+"/directories/root"), http.StatusOK); again.ID == root.ID || again.Status != "active" {
			t.Errorf("root after the next write = %+v; want a new, active root", again)
		}
	})
}

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
		integration.Reset(t, c, integration.Default)
	}
	if n := refused(s.Output()) - refusals; n != 0 {
		t.Errorf("%d sweep passes were refused across the resets; want none to run under one", n)
	}
}

const unclaimedPassWindow = 5 * time.Millisecond

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
	time.Sleep(unclaimedPassWindow)
	return false, nil
}

type counting struct{ passes atomic.Int32 }

func (r *counting) Pass(context.Context, int) (bool, error) {
	r.passes.Add(1)
	return false, nil
}

func TestSweepClaimAcrossReplicas(t *testing.T) {
	const interval = 50 * time.Millisecond
	var active atomic.Int32
	var overlap atomic.Bool
	holdFirst := make(chan struct{})
	released := make(chan struct{})
	close(released)
	first := &holding{hold: holdFirst, active: &active, overlap: &overlap}
	second := &holding{hold: released, active: &active, overlap: &overlap}
	probe := new(counting)

	releaseFirst := sync.OnceFunc(func() { close(holdFirst) })
	policy := sweep.Policy{Interval: interval, Budget: 1}
	start := func(regs ...sweep.Registration) *sweep.Sweeper {
		t.Helper()
		sw, err := sweep.New(integration.Database(t), regs, 10*time.Second, slog.New(slog.DiscardHandler), sweep.Options{})
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
	attemptFilesCycles := func(other *sweep.Sweeper, n int) {
		t.Helper()
		for range n {
			before := probe.passes.Load()
			other.Nudge("files")
			other.Nudge("probe")
			processtest.WaitFor(t, "the second replica's delivery", func() bool { return probe.passes.Load() > before })
		}
	}
	mine := start(sweep.Registration{Name: "files", Reclaimer: first, Policy: policy})
	processtest.WaitFor(t, "the first replica's pass", func() bool { return first.passes.Load() > 0 })

	other := start(
		sweep.Registration{Name: "files", Reclaimer: second, Policy: policy},
		sweep.Registration{Name: "probe", Reclaimer: probe, Policy: policy},
	)
	attemptFilesCycles(other, 5)
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
		mine.Nudge("files")
		attemptFilesCycles(other, 1)
	}
	if overlap.Load() {
		t.Error("a pass of one replica ran inside a pass of the other")
	}
}

func exec(t *testing.T, db *sqlate.DB, query string, args ...any) {
	t.Helper()
	if _, err := db.ExecContext(context.Background(), query, args...); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
}

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

func TestSweepRetention(t *testing.T) {
	f := processtest.Forward(t, integration.NATSAddr())
	s := integration.Start(t, integration.Options{
		Seed: integration.Default,
		NATS: f.Addr(),
		Env: []string{
			"APP_SWEEP_INTERVAL=100ms", "APP_SWEEP_OUTBOX_AGE=1h", "APP_SWEEP_INBOX_AGE=1h",
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
		aged, recent := emitProbe(t, db), emitProbe(t, db)
		drained(t, db)

		f.Sever()
		restored := false
		t.Cleanup(func() {
			if !restored {
				f.Restore(t)
			}
		})
		unpublished := emitProbe(t, db)
		exec(t, db, "UPDATE messaging_outbox SET created_at = now() - interval '2 hours' WHERE id = $1", unpublished)

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
