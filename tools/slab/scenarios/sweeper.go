package scenarios

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/standards-lab/go-cli-sdk"
	"github.com/standards-lab/go-cli-sdk/scenario"
	"github.com/standards-lab/go-core/graph"

	"github.com/standards-lab/go-web-service/tools/slab/domain/document"
	"github.com/standards-lab/go-web-service/tools/slab/httpx"
	"github.com/standards-lab/go-web-service/tools/slab/output"
	"github.com/standards-lab/go-web-service/tools/slab/repo"
)

// The sweeper's reclaimers in the order it registers and runs them, and
// the sweep.outcome values of its pass counter, restated from the service's
// internal/app (reactors.go and telemetry.go), which slab does not import.
var (
	reclaimers = []string{"files", "outbox", "inbox"}
	outcomes   = []string{"drained", "more", "error"}
)

// The PromQL the scenario reads the sweeper's metrics with, as the
// collector writes them to Mimir: sweep.passes is the counter
// sweep_passes_total, by sweep_reclaimer and sweep_outcome, and
// sweep.backlogged the gauge sweep_backlogged_ratio, by sweep_reclaimer,
// both under the service's job. passesQuery is also the query the closing
// step points Grafana's Explore at; exportedQuery is the time the files
// series' latest sample was written, which is the time of the export that
// carried it.
const (
	jobMatcher    = `job="` + serviceName + `"`
	passesQuery   = `sum by (sweep_reclaimer, sweep_outcome) (sweep_passes_total{` + jobMatcher + `})`
	backlogQuery  = `max by (sweep_reclaimer) (sweep_backlogged_ratio{` + jobMatcher + `})`
	exportedQuery = `max(timestamp(sweep_passes_total{` + jobMatcher + `,sweep_reclaimer="files"}))`
)

// mimirName is the Mimir datasource's name in Grafana, which the closing
// step tells the reader to pick, and sweeperOrgPath the organization the
// run's branch is created under.
const (
	mimirName      = "Mimir"
	sweeperOrgPath = "/acme"
)

// exportInterval is how often the service's metric reader exports: the
// OpenTelemetry SDK's default for a periodic reader, which the service
// constructs with no interval of its own (internal/app/telemetry.go);
// OTEL_METRIC_EXPORT_INTERVAL in the service's environment would change
// it, and neither the collector nor the configuration sets it. exportLimit
// bounds each wait for an export: two intervals, so the export in flight
// when the wait starts and the one after it both fit, with room for the
// collector's batch and Mimir's ingest. exportPoll is how often a wait
// asks Mimir.
const (
	exportInterval = 60 * time.Second
	exportLimit    = 2*exportInterval + 30*time.Second
	exportPoll     = 2 * time.Second
)

// The configuration files the scenario reads the sweep block from, at the
// repository root: the base and the local overlay mise run serve runs
// with (APP_ENV=local).
const (
	baseConfig  = "config.json"
	localConfig = "config.local.json"
)

// passCounts is sweep_passes_total by reclaimer and then outcome; an
// outcome a reclaimer has never had has no series and counts 0.
type passCounts map[string]map[string]int64

// total returns reclaimer's passes over every outcome.
func (c passCounts) total(reclaimer string) int64 {
	var n int64
	for _, v := range c[reclaimer] {
		n += v
	}
	return n
}

// sweeper is what the sweeper scenario's steps hand forward: the clients,
// the run's stamp, the sweep block as the local stack reads it, the counts
// the first step read with the time of the export that carried them, the
// counts after the sweep, the branch, and when its sweep finished. Each is
// replaced by the step that reads it, the clients by the first step, so
// nothing a run reads is left from a run before it.
type sweeper struct {
	n        Nodes
	client   *httpx.Client
	mimir    *httpx.Mimir
	stamp    string
	policy   sweepBlock
	before   passCounts
	beforeAt time.Time
	after    passCounts
	org      string
	branch   document.Identity
	sweptAt  time.Time
}

// Sweeper returns the sweeper scenario over n: the general sweeper's three
// reclaimers read through their metrics in Mimir, a recursive delete that
// nudges the files reclaimer, the next export showing files moved while
// outbox and inbox ran only on their interval, the outbox's retention, and
// the Grafana query that charts it. It creates one branch under acme, named
// for the run, and its recursive delete removes it, so a run leaves no
// document behind and runs twice in a row. It declares the service node,
// which it calls, grafana up, through whose datasource proxy it reads
// Mimir, the repo node, which locates the configuration files it reads the
// sweep block from, and the no color node.
func Sweeper(n Nodes) scenario.Scenario {
	s := &sweeper{n: n}
	return scenario.Scenario{
		Name:    "sweeper",
		Summary: "The general sweeper's reclaimers through their metrics in Mimir: a recursive delete nudges files, while outbox and inbox run on their interval",
		Nodes:   []graph.Ref{n.Service, n.GrafanaUp, n.Repo, n.NoColor},
		Steps: []scenario.Step{
			{Intent: "Pass Counts", Action: s.passCounts},
			{Intent: "Branch Creation", Action: s.createBranch},
			{Intent: "Recursive Delete", Action: s.deleteRecursively},
			{Intent: "Next Export", Action: s.nextExport},
			{Intent: "Outbox Retention", Action: s.outboxRetention},
			{Intent: "Grafana View", Action: s.grafanaView},
		},
	}
}

func (s *sweeper) passCounts(ctx context.Context, inv *cli.Invocation, r *scenario.Reporter) error {
	s.client = inv.Get(s.n.Service).Client
	s.mimir = inv.Get(s.n.GrafanaUp).Mimir
	s.stamp = strconv.FormatInt(time.Now().UnixNano(), 36)
	st := styleOf(s.n, inv)
	r.Note("The service runs one general sweeper with three reclaimers, in this order: files, blobfs's sweep, which finishes the branches a recursive delete marks and reclaims file rows stuck past stale_age; outbox, which deletes outbox rows the relay published longer ago than outbox_age; and inbox, which deletes handled inbox rows older than inbox_age. Each runs at start and then once per interval, and files also when a recursive delete nudges it. Its sweep block, as the local stack reads it, %s over %s:", localConfig, baseConfig)
	var err error
	if s.policy, err = loadSweepBlock(inv.Get(s.n.Repo)); err != nil {
		return err
	}
	if err := show(r, "Sweep block", output.Table(st, s.policy.rows())); err != nil {
		return err
	}
	r.Note("Every pass counts once in the counter sweep.passes, by sweep.reclaimer and by sweep.outcome: drained when it finished the work it found, more when it stopped at the batch with work left, error when it failed. The gauge sweep.backlogged is 1 for a reclaimer whose last pass reported more. The service exports them every %s, the OpenTelemetry SDK's default interval, through the collector to Mimir, where they are sweep_passes_total and sweep_backlogged_ratio; this reads them through Grafana's proxy to its Mimir datasource, as of the last export.", exportInterval)
	if err := s.awaitFirstExport(ctx); err != nil {
		return err
	}
	if s.beforeAt, s.before, err = s.read(ctx); err != nil {
		return err
	}
	backlog, err := s.backlog(ctx)
	if err != nil {
		return err
	}
	return show(r, "Passes by outcome, exported at "+clock(s.beforeAt), output.Table(st, countRows(s.before, backlog)))
}

// awaitFirstExport waits until Mimir holds the backlog gauge for every
// reclaimer, which the service reports from its start: until then the
// service has not exported since it started, or not since Mimir's data
// was dropped.
func (s *sweeper) awaitFirstExport(ctx context.Context) error {
	_, err := s.mimir.Await(ctx, backlogQuery, exportPoll, exportLimit, func(samples []httpx.Sample) bool {
		return len(byReclaimer(samples)) == len(reclaimers)
	})
	if err != nil {
		return fmt.Errorf("waiting for the service's first export of sweep_backlogged_ratio for %s (is mise run serve streaming to the observability profile?): %w", strings.Join(reclaimers, ", "), err)
	}
	return nil
}

// read returns the time of the latest export of the files series and the
// pass counts as of it or later: the time is read first, so the counts are
// never older than the export it names.
func (s *sweeper) read(ctx context.Context) (time.Time, passCounts, error) {
	at, err := s.exportedAt(ctx)
	if err != nil {
		return time.Time{}, nil, err
	}
	samples, err := s.mimir.Query(ctx, passesQuery)
	if err != nil {
		return time.Time{}, nil, err
	}
	return at, countsOf(samples), nil
}

// exportedAt returns the time of the latest export of the files series,
// the zero time when Mimir holds none.
func (s *sweeper) exportedAt(ctx context.Context) (time.Time, error) {
	samples, err := s.mimir.Query(ctx, exportedQuery)
	if err != nil {
		return time.Time{}, err
	}
	return exportTime(samples), nil
}

// backlog returns the backlog gauge by reclaimer.
func (s *sweeper) backlog(ctx context.Context) (map[string]float64, error) {
	samples, err := s.mimir.Query(ctx, backlogQuery)
	if err != nil {
		return nil, err
	}
	return byReclaimer(samples), nil
}

func (s *sweeper) createBranch(ctx context.Context, inv *cli.Invocation, r *scenario.Reporter) error {
	st := styleOf(s.n, inv)
	acme, err := lookup(ctx, s.client, sweeperOrgPath)
	if err != nil {
		return err
	}
	s.org = acme.ID
	name := "sweeper-" + s.stamp
	r.Note("A branch for the files reclaimer to remove: a directory under acme's document root named for this run, %s, so a second run never collides with the first, and a file in it. Each command records its event in the outbox in its own transaction, directory.created and then file.uploaded, rows the outbox reclaimer comes to below.", name)
	route := documentsRoute(s.org) + "/directories"
	if s.branch, err = createDirectory(ctx, r, st, s.client, route, document.CreateDirectory{ParentID: document.RootAlias, Name: name}); err != nil {
		return err
	}
	filePath := directoryRoute(s.org, s.branch.ID) + "/files?" + url.Values{"name": {"notes.txt"}}.Encode()
	headers := []httpx.Header{{Name: "Content-Type", Value: textPlain}}
	res, err := call(ctx, r, st, s.client, http.MethodPost, filePath, headers, "Notes the files reclaimer removes with their branch.\n")
	if err != nil {
		return err
	}
	return res.Expect(http.StatusCreated)
}

func (s *sweeper) deleteRecursively(ctx context.Context, inv *cli.Invocation, r *scenario.Reporter) error {
	res, err := s.client.Get(ctx, directoryRoute(s.org, s.branch.ID))
	if err != nil {
		return err
	}
	if err := res.Expect(http.StatusOK); err != nil {
		return err
	}
	var d document.Directory
	if err := res.JSON(&d); err != nil {
		return err
	}
	r.Note("The recursive delete marks the branch deleting in one transaction, recording directory.deleted in the outbox with it, answers 202, and after the commit nudges the files reclaimer, which runs at once rather than at its next interval. This polls the Location, as dirs delete --wait does, until it answers 404: the nudged pass has removed the branch's rows and objects. It gives up after %s.", sweepLimit)
	headers := []httpx.Header{httpx.IfMatch(d.Version)}
	res, err = call(ctx, r, styleOf(s.n, inv), s.client, http.MethodDelete, directoryRoute(s.org, s.branch.ID)+"?recursive=true", headers, nil)
	if err != nil {
		return err
	}
	if err := res.Expect(http.StatusAccepted); err != nil {
		return err
	}
	location := res.Header.Get("Location")
	if location == "" {
		return errors.New("the service answered 202 with no Location to follow")
	}
	took, err := document.AwaitSweep(ctx, document.NewClient(s.client), location, sweepLimit)
	if err != nil {
		return err
	}
	s.sweptAt = time.Now()
	r.Note("The sweep removed the branch within %s, at %s.", took.Round(time.Millisecond), clock(s.sweptAt))
	return nil
}

func (s *sweeper) nextExport(ctx context.Context, inv *cli.Invocation, r *scenario.Reporter) error {
	st := styleOf(s.n, inv)
	r.Note("The nudged pass counted at once in the service, but Mimir sees it only with the next export, up to %s away. This asks Mimir every %s for the time of the files series' latest sample until it is after the sweep, and gives up after %s.", exportInterval, exportPoll, exportLimit)
	_, err := s.mimir.Await(ctx, exportedQuery, exportPoll, exportLimit, func(samples []httpx.Sample) bool {
		return exportTime(samples).After(s.sweptAt)
	})
	if err != nil {
		return fmt.Errorf("waiting for an export of sweep_passes_total after the sweep at %s: %w", clock(s.sweptAt), err)
	}
	var at time.Time
	if at, s.after, err = s.read(ctx); err != nil {
		return err
	}
	backlog, err := s.backlog(ctx)
	if err != nil {
		return err
	}
	window := at.Sub(s.beforeAt).Round(time.Second)
	if err := show(r, fmt.Sprintf("Passes by outcome, exported at %s, %s after the first step's", clock(at), window), output.Table(st, deltaRows(s.before, s.after, backlog))); err != nil {
		return err
	}
	moved := movement(s.before, s.after)
	if moved["files"] < 1 {
		return fmt.Errorf("the export at %s, after the sweep, counts files at %d passes, as the first step's did: the nudged pass is missing", clock(at), s.after.total("files"))
	}
	r.Note("%s", narrateMovement(moved, s.policy.Interval.Value, window))
	return nil
}

func (s *sweeper) outboxRetention(ctx context.Context, inv *cli.Invocation, r *scenario.Reporter) error {
	st := styleOf(s.n, inv)
	age := s.policy.OutboxAge
	r.Note("This run's three commands recorded three events in the outbox, directory.created, file.uploaded, and directory.deleted, and the relay published each after its commit. A published row older than outbox_age is the outbox reclaimer's to delete, on its first interval pass after the row turns that old.")
	if age.From == localConfig {
		r.Note("%s sets outbox_age to %s and inbox_age to %s for the local stack, where %s's are %s and %s, a week, so here the outbox reclaimer has work: this run's rows are deleted about %s after their publish, while every other environment keeps a week of rows.", localConfig, age.Value, s.policy.InboxAge.Value, baseConfig, s.policy.baseOutbox, s.policy.baseInbox, age.Value)
	} else {
		r.Note("%s sets no outbox_age, so the local stack keeps %s's %s, and this run's rows outlive it by that long.", localConfig, baseConfig, age.Value)
	}
	r.Note("slab reads no outbox rows, so the counter is what it shows of that: the outbox's passes as of the last export. A pass that deleted rows and a pass that found none both count as drained; one that stopped at the batch, %s rows, with aged rows left counts as more, and sets sweep.backlogged until a pass drains.", s.policy.Batch.Value)
	backlog, err := s.backlog(ctx)
	if err != nil {
		return err
	}
	return show(r, "The outbox's passes", output.Table(st, countRowsOf([]string{"outbox"}, s.after, backlog)))
}

func (s *sweeper) grafanaView(ctx context.Context, inv *cli.Invocation, r *scenario.Reporter) error {
	st := styleOf(s.n, inv)
	r.Note("Grafana charts the same counter over time: in Explore, pick the %s datasource and run this query, which sums the passes by reclaimer and outcome, so each step in a line is a pass. files steps at each nudge as well as on the interval; outbox and inbox step on the interval alone.", mimirName)
	return showApart(r, "Observability", output.Metrics(st, inv.Get(s.n.GrafanaUp).URL, mimirName, passesQuery))
}

// countsOf returns the samples of passesQuery as pass counts.
func countsOf(samples []httpx.Sample) passCounts {
	c := passCounts{}
	for _, sm := range samples {
		rec, out := sm.Labels["sweep_reclaimer"], sm.Labels["sweep_outcome"]
		if c[rec] == nil {
			c[rec] = map[string]int64{}
		}
		c[rec][out] = int64(sm.Value)
	}
	return c
}

// byReclaimer returns the samples' values by their sweep_reclaimer label.
func byReclaimer(samples []httpx.Sample) map[string]float64 {
	m := make(map[string]float64, len(samples))
	for _, sm := range samples {
		if rec, ok := sm.Labels["sweep_reclaimer"]; ok {
			m[rec] = sm.Value
		}
	}
	return m
}

// exportTime returns the time exportedQuery's one sample names, a Unix
// time in fractional seconds, or the zero time when it has none.
func exportTime(samples []httpx.Sample) time.Time {
	if len(samples) == 0 {
		return time.Time{}
	}
	return time.UnixMilli(int64(samples[0].Value * 1000))
}

// movement returns each reclaimer's passes in after past those in before.
func movement(before, after passCounts) map[string]int64 {
	m := make(map[string]int64, len(reclaimers))
	for _, rec := range reclaimers {
		m[rec] = after.total(rec) - before.total(rec)
	}
	return m
}

// narrateMovement says what the window's movement shows, as it fell. The
// files reclaimer's passes hold the nudged one. No command names outbox or
// inbox, so theirs ran on the interval: go-database's sweep.Sweeper runs
// every reclaimer that is due at each delivery, a tick or a nudge, and
// counts one due once its interval less half a tick has passed since its
// last cycle (Sweeper.due), so the delivery a nudge earns runs them too
// when they are that close. It claims no more than the counts show: when
// files ran no more passes than the most either of the others ran, the
// counts cannot tell the nudged pass from a tick.
func narrateMovement(moved map[string]int64, interval string, window time.Duration) string {
	files, outbox, inbox := moved["files"], moved["outbox"], moved["inbox"]
	var b strings.Builder
	fmt.Fprintf(&b, "Over the %s between the two exports files ran %s, the nudged one among them. ", window, plural(files, "pass"))
	switch {
	case outbox == 0 && inbox == 0:
		fmt.Fprintf(&b, "outbox and inbox ran none: no command names them, and their %s interval did not come round in the window.", interval)
	default:
		fmt.Fprintf(&b, "outbox ran %s and inbox %s: no command names them, so they ran on their %s interval, at its ticks, or at the delivery the nudge earned when they were within half an interval of due, since the sweeper runs every reclaimer due at each delivery.", plural(outbox, "pass"), plural(inbox, "pass"), interval)
	}
	if files > max(outbox, inbox) {
		fmt.Fprintf(&b, " files' %s over the most either of them ran is the nudge's.", plural(files-max(outbox, inbox), "pass"))
	} else {
		b.WriteString(" files ran no more than they did, so the counts alone do not single out the nudged pass in this window: a nudge restarts files' interval, so the next tick can pass files by, and its delivery can run the other two early. The branch's 404 above is the nudged pass's work.")
	}
	return b.String()
}

// plural returns n and noun, with noun's plural when n is not one.
func plural(n int64, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return strconv.FormatInt(n, 10) + " " + noun + "es"
}

// countRows returns the rows of the pass-count table for every reclaimer.
func countRows(c passCounts, backlog map[string]float64) [][2]string {
	return countRowsOf(reclaimers, c, backlog)
}

// countRowsOf returns one row per reclaimer in names: its passes by outcome
// and its backlog gauge.
func countRowsOf(names []string, c passCounts, backlog map[string]float64) [][2]string {
	rows := make([][2]string, 0, len(names))
	for _, rec := range names {
		cells := make([]string, 0, len(outcomes)+1)
		for _, out := range outcomes {
			cells = append(cells, fmt.Sprintf("%s %-6d", out, c[rec][out]))
		}
		cells = append(cells, fmt.Sprintf("backlogged %g", backlog[rec]))
		rows = append(rows, [2]string{rec, strings.Join(cells, " ")})
	}
	return rows
}

// deltaRows returns one row per reclaimer: how many passes it ran between
// before and after, then each outcome's count, as before→after where it
// moved, and its backlog gauge.
func deltaRows(before, after passCounts, backlog map[string]float64) [][2]string {
	rows := make([][2]string, 0, len(reclaimers))
	for _, rec := range reclaimers {
		cells := []string{fmt.Sprintf("+%-3d", after.total(rec)-before.total(rec))}
		for _, out := range outcomes {
			was, now := before[rec][out], after[rec][out]
			cell := fmt.Sprintf("%s %d", out, now)
			if now != was {
				cell = fmt.Sprintf("%s %d→%d", out, was, now)
			}
			cells = append(cells, fmt.Sprintf("%-14s", cell))
		}
		cells = append(cells, fmt.Sprintf("backlogged %g", backlog[rec]))
		rows = append(rows, [2]string{rec, strings.Join(cells, " ")})
	}
	return rows
}

// clock renders t as the wall-clock time a reader compares with a log line.
func clock(t time.Time) string {
	return t.Format("15:04:05")
}

// sweepBlock is the sweep block as the local stack merges it: each value
// from localConfig where it sets one, else from baseConfig, as the
// service's configuration layers them, without the APP_SWEEP_* overrides
// of the environment the service runs in, which slab cannot see. It keeps
// baseConfig's retention ages too, for the narration's comparison.
type sweepBlock struct {
	Interval, Batch, StaleAge, OutboxAge, InboxAge setting
	baseOutbox, baseInbox                          string
}

// setting is one value of the sweep block, as written, and the file it
// came from.
type setting struct{ Value, From string }

// rows returns the block as table rows, each value naming its file.
func (b sweepBlock) rows() [][2]string {
	rows := make([][2]string, 0, 5)
	for _, kv := range []struct {
		key string
		v   setting
	}{
		{"interval", b.Interval},
		{"batch", b.Batch},
		{"stale_age", b.StaleAge},
		{"outbox_age", b.OutboxAge},
		{"inbox_age", b.InboxAge},
	} {
		rows = append(rows, [2]string{kv.key, fmt.Sprintf("%-6s (%s)", kv.v.Value, kv.v.From)})
	}
	return rows
}

// sweepFile is the part of a configuration file the scenario reads: its
// sweep block, each value as written. batch is a number, kept raw.
type sweepFile struct {
	Sweep struct {
		Interval  string          `json:"interval"`
		Batch     json.RawMessage `json:"batch"`
		StaleAge  string          `json:"stale_age"`
		OutboxAge string          `json:"outbox_age"`
		InboxAge  string          `json:"inbox_age"`
	} `json:"sweep"`
}

// loadSweepBlock reads the sweep block from baseConfig and localConfig in
// the repository the override locates, and merges the second over the
// first.
func loadSweepBlock(override string) (sweepBlock, error) {
	fsys, err := repo.FS(override)
	if err != nil {
		return sweepBlock{}, err
	}
	var files [2]sweepFile
	for i, name := range []string{baseConfig, localConfig} {
		raw, err := fs.ReadFile(fsys, name)
		if err != nil {
			return sweepBlock{}, err
		}
		if err := json.Unmarshal(raw, &files[i]); err != nil {
			return sweepBlock{}, fmt.Errorf("%s: %w", name, err)
		}
	}
	base, local := files[0].Sweep, files[1].Sweep
	pick := func(b, l string) setting {
		if l != "" {
			return setting{l, localConfig}
		}
		return setting{b, baseConfig}
	}
	return sweepBlock{
		Interval:   pick(base.Interval, local.Interval),
		Batch:      pick(string(base.Batch), string(local.Batch)),
		StaleAge:   pick(base.StaleAge, local.StaleAge),
		OutboxAge:  pick(base.OutboxAge, local.OutboxAge),
		InboxAge:   pick(base.InboxAge, local.InboxAge),
		baseOutbox: base.OutboxAge,
		baseInbox:  base.InboxAge,
	}, nil
}
