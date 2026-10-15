package scenarios

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"time"

	"github.com/standards-lab/go-cli-sdk"
	"github.com/standards-lab/go-cli-sdk/scenario"
	"github.com/standards-lab/go-core/graph"

	"github.com/standards-lab/go-web-service/tools/slab/admin/database"
	"github.com/standards-lab/go-web-service/tools/slab/domain/document"
	"github.com/standards-lab/go-web-service/tools/slab/domain/organization"
	"github.com/standards-lab/go-web-service/tools/slab/httpx"
	"github.com/standards-lab/go-web-service/tools/slab/output"
	"github.com/standards-lab/go-web-service/tools/slab/repo"
	"github.com/standards-lab/go-web-service/tools/slab/style"
)

// The organizations the storage scenario addresses by path: acme, whose
// logo it replaces and whose seeded document tree it reads, and finance,
// the other organization its isolation step asks for acme's ids.
const (
	storageOrgPath = "/acme"
	otherOrgPath   = "/acme/finance"
)

// The seeded entries the steps target, by their path under acme's document
// root; their ids come from the seed file, never from a listing.
const (
	guardedFilePath = "/README.txt"
	takenDirPath    = "/engineering"
)

// replacementFixture is the seeded fixture the logo step uploads in place
// of acme's own: another organization's logo, a PNG that passes the
// upload's sniffed-type rule, under fixturesDir.
const replacementFixture = "engineering.png"

// cursorPageSize is the page size the cursor walk asks for: one, so acme's
// three seeded top-level directories take three pages and two cursors.
const cursorPageSize = 1

// detailNameTaken is the curated detail of a taken name's 409:
// data.DetailNameTaken, restated because slab does not import the service.
const detailNameTaken = "an entry with that name already exists"

// schemaRoute and seedRoute are the database admin service's schema status
// and additive seed, inside the /admin mount, the routes database.Client's
// Status and Seed send to; the steps build their requests beside their
// narration, as reset does.
const (
	schemaRoute = database.Database + "/schema"
	seedRoute   = database.Database + "/seed"
)

// declaredSets are the sets the schema step expects the service to
// declare: blobfs's own set and the application's own.
var declaredSets = []string{"blobfs", "app"}

// seedFile is the part of data/seeds/<state>.json the scenario reads: the
// organizations and logos only counted, and the document trees whole, for
// the fixed ids the steps target and the counts the reset reports.
type seedFile struct {
	Organizations []json.RawMessage `json:"organizations"`
	Logos         []json.RawMessage `json:"logos"`
	Documents     []seedTree        `json:"documents"`
}

// seedTree is one organization's seeded document hierarchy: the
// organization's path, its root's fixed id, and the entries under it.
type seedTree struct {
	Organization string      `json:"organization"`
	Root         string      `json:"root"`
	Entries      []seedEntry `json:"entries"`
}

// seedEntry is a seeded directory or file: a file names its content type,
// and a directory names none and may hold entries of its own.
type seedEntry struct {
	ID          string      `json:"id"`
	Name        string      `json:"name"`
	ContentType string      `json:"content_type"`
	Entries     []seedEntry `json:"entries"`
}

// seeded is the counts the seeder reports for a state: the admin seed's
// response, and the seeded member of the state reset's.
type seeded struct {
	Documents     int `json:"documents"`
	Logos         int `json:"logos"`
	Organizations int `json:"organizations"`
}

// seedCounts is what a reset to the seed file's state reports seeding:
// every organization, every logo, and every document entry, directories
// and files, the roots not counted.
func (s seedFile) seedCounts() seeded {
	n := 0
	var count func([]seedEntry)
	count = func(entries []seedEntry) {
		for _, e := range entries {
			n++
			count(e.Entries)
		}
	}
	for _, t := range s.Documents {
		count(t.Entries)
	}
	return seeded{Documents: n, Logos: len(s.Logos), Organizations: len(s.Organizations)}
}

// tree returns the seeded tree of the organization at orgPath.
func (s seedFile) tree(orgPath string) (seedTree, error) {
	for _, t := range s.Documents {
		if t.Organization == orgPath {
			return t, nil
		}
	}
	return seedTree{}, fmt.Errorf("the seed has no document tree for %s", orgPath)
}

// paths returns every entry of t by its path under the root, a directory's
// with a trailing slash, mapped to its fixed id.
func (t seedTree) paths() map[string]string {
	out := make(map[string]string)
	var walk func(prefix string, entries []seedEntry)
	walk = func(prefix string, entries []seedEntry) {
		for _, e := range entries {
			p := prefix + e.Name
			if e.ContentType == "" {
				out[p+"/"] = e.ID
				walk(p+"/", e.Entries)
				continue
			}
			out[p] = e.ID
		}
	}
	walk("/", t.Entries)
	return out
}

// loadSeed reads and decodes data/seeds/<seedState>.json from the
// repository the repo override locates.
func loadSeed(override string) (seedFile, error) {
	fsys, err := repo.FS(override)
	if err != nil {
		return seedFile{}, err
	}
	file := path.Join(seedsDir, seedState+".json")
	raw, err := fs.ReadFile(fsys, file)
	if err != nil {
		return seedFile{}, err
	}
	var s seedFile
	if err := json.Unmarshal(raw, &s); err != nil {
		return seedFile{}, fmt.Errorf("%s: %w", file, err)
	}
	return s, nil
}

// storage is what the storage scenario's steps hand forward: the client,
// the seed file and acme's seeded paths, the two organizations, the logo's
// validator, the guarded file as last read, and the branch the recursive
// delete removes, with the Location its 202 named. Each is replaced by the
// step that reads it first, the client by the first step, so nothing a
// run reads is left from a run before it.
type storage struct {
	n        Nodes
	client   *httpx.Client
	seed     seedFile
	paths    map[string]string
	root     string
	acme     organization.Organization
	other    organization.Organization
	etag     string
	guarded  document.File
	branch   document.Directory
	location string
}

// Storage returns the storage scenario over n: the migration sets, a
// logo's replace cycle, acme's document tree and a cursor walk, the scoped
// refusals, a recursive delete swept to 404, and a reset, against the
// running service. It starts with an additive seed rather than a reset,
// and names what it creates for the run, so it runs twice in a row without
// a reset between. It declares the service node, which it calls, the repo
// node, which locates the seed file and the fixture it reads, and the no
// color node, which decides its blocks' color; it points at no trace, so
// it declares no grafana up.
func Storage(n Nodes) scenario.Scenario {
	s := &storage{n: n}
	return scenario.Scenario{
		Name:    "storage",
		Summary: "Storage end to end against the running service: the migration sets, a logo's replace cycle, acme's document tree and a cursor walk, the scoped refusals, a recursive delete swept to 404, and a reset",
		Nodes:   []graph.Ref{n.Service, n.Repo, n.NoColor},
		Steps: []scenario.Step{
			{Intent: "Migration Sets", Action: s.migrationSets},
			{Intent: "Additive Seed", Action: s.additiveSeed},
			{Intent: "Logo Read", Action: s.readLogo},
			{Intent: "Logo Revalidation", Action: s.revalidateLogo},
			{Intent: "Logo Replacement", Action: s.replaceLogo},
			{Intent: "Logo Delete", Action: s.deleteLogo},
			{Intent: "Document Tree", Action: s.documentTree},
			{Intent: "Cursor Walk", Action: s.cursorWalk},
			{Intent: "Missing If-Match", Action: s.missingIfMatch},
			{Intent: "Stale If-Match", Action: s.staleIfMatch},
			{Intent: "Taken Name", Action: s.takenName},
			{Intent: "Another Organization", Action: s.anotherOrganization},
			{Intent: "Branch Creation", Action: s.createBranch},
			{Intent: "Recursive Delete", Action: s.recursiveDelete},
			{Intent: "Sweep", Action: s.sweep},
			{Intent: "Reset", Action: s.reset},
		},
	}
}

func (s *storage) migrationSets(ctx context.Context, inv *cli.Invocation, r *scenario.Reporter) error {
	s.client = inv.Get(s.n.Service).Client
	st := styleOf(s.n, inv)
	r.Note("The service's schema is two migration sets, applied at startup in declaration order: blobfs, go-database's file hierarchy (directories, files, and their statuses), and app, the service's own tables, whose owner rows (an organization's logo image and its document root) reference blobfs's. The schema status reports each set's applied version against the latest it carries.")
	res, err := call(ctx, r, st, s.client, http.MethodGet, schemaRoute, nil, nil)
	if err != nil {
		return err
	}
	if err := res.Expect(http.StatusOK); err != nil {
		return err
	}
	var status struct {
		Ready bool `json:"ready"`
		Sets  []struct {
			Name    string `json:"name"`
			Version int    `json:"version"`
			Latest  int    `json:"latest"`
			Dirty   bool   `json:"dirty"`
		} `json:"sets"`
	}
	if err := res.JSON(&status); err != nil {
		return err
	}
	if !status.Ready {
		return errors.New("the schema status is not ready")
	}
	var rows [][2]string
	for _, want := range declaredSets {
		found := false
		for _, set := range status.Sets {
			if set.Name != want {
				continue
			}
			found = true
			if set.Dirty || set.Version != set.Latest {
				return fmt.Errorf("the %s set is at version %d of %d (dirty %t), want it at its latest and clean", set.Name, set.Version, set.Latest, set.Dirty)
			}
			rows = append(rows, [2]string{set.Name, fmt.Sprintf("version %d of %d", set.Version, set.Latest)})
		}
		if !found {
			return fmt.Errorf("the schema status lists no %s set", want)
		}
	}
	return show(r, "Migration sets", output.Table(st, rows))
}

func (s *storage) additiveSeed(ctx context.Context, inv *cli.Invocation, r *scenario.Reporter) error {
	seed, err := loadSeed(inv.Get(s.n.Repo))
	if err != nil {
		return err
	}
	s.seed = seed
	tree, err := seed.tree(storageOrgPath)
	if err != nil {
		return err
	}
	s.paths, s.root = tree.paths(), tree.Root
	r.Note("A seed is additive: it applies the %q state's rows over what is there and leaves alone what it already finds, so on a database the state seeded it reports zero rows written. A logo that an interrupted run deleted is written again, so the steps below always start from acme's seeded logo and document tree.", seedState)
	body := database.State{State: seedState}
	res, err := call(ctx, r, styleOf(s.n, inv), s.client, http.MethodPost, seedRoute, nil, body)
	if err != nil {
		return err
	}
	if err := res.Expect(http.StatusOK); err != nil {
		return err
	}
	var n seeded
	if err := res.JSON(&n); err != nil {
		return err
	}
	if s.acme, err = lookup(ctx, s.client, storageOrgPath); err != nil {
		return err
	}
	s.other, err = lookup(ctx, s.client, otherOrgPath)
	return err
}

func (s *storage) readLogo(ctx context.Context, inv *cli.Invocation, r *scenario.Reporter) error {
	r.Note("The logo is served by proxy from the object store. Its ETag is the store's tag for the bytes, and Last-Modified the file's last change; the bytes themselves are summarized below rather than printed.")
	res, err := s.getLogo(ctx, r, styleOf(s.n, inv))
	if err != nil {
		return err
	}
	if err := res.Expect(http.StatusOK); err != nil {
		return err
	}
	s.etag = res.Header.Get("ETag")
	if s.etag == "" {
		return errors.New("the logo carries no ETag")
	}
	return nil
}

func (s *storage) revalidateLogo(ctx context.Context, inv *cli.Invocation, r *scenario.Reporter) error {
	r.Note("A conditional read names the ETag it holds in If-None-Match. The bytes have not changed, so the answer is 304 Not Modified with no body, and the object is never opened.")
	headers := []httpx.Header{{Name: "If-None-Match", Value: s.etag}}
	res, err := call(ctx, r, styleOf(s.n, inv), s.client, http.MethodGet, logoRoute(s.acme.ID), headers, nil)
	if err != nil {
		return err
	}
	return res.Expect(http.StatusNotModified)
}

func (s *storage) replaceLogo(ctx context.Context, inv *cli.Invocation, r *scenario.Reporter) error {
	fsys, err := repo.FS(inv.Get(s.n.Repo))
	if err != nil {
		return err
	}
	file := path.Join(fixturesDir, replacementFixture)
	png, err := fs.ReadFile(fsys, file)
	if err != nil {
		return err
	}
	r.Note("The logo is a singleton addressed by its organization, so it takes no version: a PUT replaces whatever is active, the last write winning. It answers 201 with the new file's id and no version, since every PUT creates a new file, and the replaced file is retired. This uploads another organization's seeded logo, %s, as acme's; the read after it shows the new bytes under a new ETag.", replacementFixture)
	st := styleOf(s.n, inv)
	headers := []httpx.Header{{Name: "Content-Type", Value: "image/png"}}
	if err := showApart(r, "", output.Request(st, http.MethodPut, logoRoute(s.acme.ID), headers, fmt.Sprintf("(%d bytes of %s)", len(png), file))); err != nil {
		return err
	}
	res, err := s.client.Put(ctx, logoRoute(s.acme.ID), png, headers...)
	if err != nil {
		return err
	}
	if err := showApart(r, "", output.Response(st, res)); err != nil {
		return err
	}
	if err := res.Expect(http.StatusCreated); err != nil {
		return err
	}
	var created map[string]any
	if err := res.JSON(&created); err != nil {
		return err
	}
	if id, _ := created["id"].(string); id == "" {
		return fmt.Errorf("the logo PUT names no id; body: %s", res.Body)
	}
	if _, ok := created["version"]; ok {
		return fmt.Errorf("the logo PUT names a version, which a logo does not take; body: %s", res.Body)
	}
	res, err = s.getLogo(ctx, r, st)
	if err != nil {
		return err
	}
	if err := res.Expect(http.StatusOK); err != nil {
		return err
	}
	if !bytes.Equal(res.Body, png) {
		return fmt.Errorf("the logo read back %d bytes that are not the %d uploaded", len(res.Body), len(png))
	}
	etag := res.Header.Get("ETag")
	if etag == "" || etag == s.etag {
		return fmt.Errorf("the replaced logo's ETag is %q, want a new tag, not the seeded logo's %q", etag, s.etag)
	}
	return nil
}

func (s *storage) deleteLogo(ctx context.Context, inv *cli.Invocation, r *scenario.Reporter) error {
	r.Note("A DELETE retires whatever logo is active, again with no version, and the read after it is 404. The seeded logo comes back with the reset at the end of this run, and the additive seed at the start of the next run writes it again if this one stops before then.")
	st := styleOf(s.n, inv)
	res, err := call(ctx, r, st, s.client, http.MethodDelete, logoRoute(s.acme.ID), nil, nil)
	if err != nil {
		return err
	}
	if err := res.Expect(http.StatusNoContent); err != nil {
		return err
	}
	res, err = call(ctx, r, st, s.client, http.MethodGet, logoRoute(s.acme.ID), nil, nil)
	if err != nil {
		return err
	}
	_, err = res.Problem(http.StatusNotFound)
	return err
}

// getLogo sends a narrated GET of acme's logo, its response shown
// summarized: the logo's bytes are a PNG, which would print as noise.
func (s *storage) getLogo(ctx context.Context, r *scenario.Reporter, st style.Style) (*httpx.Response, error) {
	if err := showApart(r, "", output.Request(st, http.MethodGet, logoRoute(s.acme.ID), nil, nil)); err != nil {
		return nil, err
	}
	res, err := s.client.Get(ctx, logoRoute(s.acme.ID))
	if err != nil {
		return nil, err
	}
	return res, showApart(r, "", output.Response(st, summarized(res)))
}

func (s *storage) documentTree(ctx context.Context, inv *cli.Invocation, r *scenario.Reporter) error {
	st := styleOf(s.n, inv)
	r.Note("Each organization's documents hang from its own root, one directory the %q alias names, with every entry beneath it the organization's by containment. A directory's contents read as two listings, its child directories and its files, each paged, filtered, and sorted. Below are the root's two listings, then the whole tree the seed wrote, walked a listing at a time, every entry at the fixed id the seed file gives it.", document.RootAlias)
	var dirs document.DirectoryPage
	listDirs := directoryRoute(s.acme.ID, document.RootAlias) + "/directories?" + httpx.RawQuery([2]string{"sort", "name"})
	if err := s.get(ctx, r, st, listDirs, &dirs); err != nil {
		return err
	}
	var files document.FilePage
	listFiles := directoryRoute(s.acme.ID, document.RootAlias) + "/files?" + httpx.RawQuery([2]string{"sort", "name"})
	if err := s.get(ctx, r, st, listFiles, &files); err != nil {
		return err
	}
	found := make(map[string]string)
	var rows [][2]string
	var walk func(prefix string, dirs []document.Directory, files []document.File) error
	walk = func(prefix string, dirs []document.Directory, files []document.File) error {
		for _, f := range files {
			found[prefix+f.Name] = f.ID
			rows = append(rows, [2]string{prefix + f.Name, f.ID})
		}
		for _, d := range dirs {
			p := prefix + d.Name + "/"
			found[p] = d.ID
			rows = append(rows, [2]string{p, d.ID})
			var sub document.DirectoryPage
			if err := s.getQuiet(ctx, directoryRoute(s.acme.ID, d.ID)+"/directories?"+httpx.RawQuery([2]string{"sort", "name"}), &sub); err != nil {
				return err
			}
			var subFiles document.FilePage
			if err := s.getQuiet(ctx, directoryRoute(s.acme.ID, d.ID)+"/files?"+httpx.RawQuery([2]string{"sort", "name"}), &subFiles); err != nil {
				return err
			}
			if err := walk(p, sub.Items, subFiles.Items); err != nil {
				return err
			}
		}
		return nil
	}
	if err := walk("/", dirs.Items, files.Items); err != nil {
		return err
	}
	if err := show(r, "acme's document tree", output.Table(st, rows)); err != nil {
		return err
	}
	for p, id := range s.paths {
		if found[p] != id {
			return fmt.Errorf("the tree has %s at id %q, want the seeded id %s", p, found[p], id)
		}
	}
	for _, d := range dirs.Items {
		if d.ParentID == nil || *d.ParentID != s.root {
			return fmt.Errorf("the root's listing shows %s under a parent other than the seeded root %s", d.Name, s.root)
		}
	}
	return nil
}

// get sends a narrated GET of p, expects 200, and decodes the body into v.
func (s *storage) get(ctx context.Context, r *scenario.Reporter, st style.Style, p string, v any) error {
	res, err := call(ctx, r, st, s.client, http.MethodGet, p, nil, nil)
	if err != nil {
		return err
	}
	if err := res.Expect(http.StatusOK); err != nil {
		return err
	}
	return res.JSON(v)
}

// getQuiet is get without the narration, for the reads a step makes to
// build what it prints rather than to show.
func (s *storage) getQuiet(ctx context.Context, p string, v any) error {
	res, err := s.client.Get(ctx, p)
	if err != nil {
		return err
	}
	if err := res.Expect(http.StatusOK); err != nil {
		return fmt.Errorf("GET %s: %w", p, err)
	}
	return res.JSON(v)
}

func (s *storage) cursorWalk(ctx context.Context, inv *cli.Invocation, r *scenario.Reporter) error {
	st := styleOf(s.n, inv)
	r.Note("A listing's envelope carries a next cursor while more rows follow. Sending cursor=<next> in place of page, with the same sort, continues after the last row the previous page showed, so a tree that changes between requests neither skips nor repeats an entry; a continued page carries no page number. This walks the root's child directories %d to a page until more is false.", cursorPageSize)
	base := directoryRoute(s.acme.ID, document.RootAlias) + "/directories?"
	var names []string
	total, cursor := -1, ""
	for pages := 0; ; pages++ {
		if pages > 100 {
			return errors.New("the cursor walk passed 100 pages without an end")
		}
		query := [][2]string{{"size", strconv.Itoa(cursorPageSize)}, {"sort", "name"}}
		if cursor != "" {
			query = append(query, [2]string{"cursor", cursor})
		}
		var p document.DirectoryPage
		if err := s.get(ctx, r, st, base+httpx.RawQuery(query...), &p); err != nil {
			return err
		}
		if total < 0 && p.Total != nil {
			total = *p.Total
		}
		for _, d := range p.Items {
			names = append(names, d.Name)
		}
		if !p.More || p.Next == "" {
			break
		}
		cursor = p.Next
	}
	if total >= 0 && len(names) != total {
		return fmt.Errorf("the walk visited %d directories, but the first page's total is %d", len(names), total)
	}
	var seededNames []string
	tree, err := s.seed.tree(storageOrgPath)
	if err != nil {
		return err
	}
	for _, e := range tree.Entries {
		if e.ContentType == "" {
			seededNames = append(seededNames, e.Name)
		}
	}
	if !inOrder(names, seededNames) {
		return fmt.Errorf("the walk visited %v, want the seeded %v among them in name order", names, seededNames)
	}
	return nil
}

// inOrder reports whether want appears within got in the same relative
// order: a sorted walk visits the seeded names in their order, with any
// other entry wherever its name falls.
func inOrder(got, want []string) bool {
	i := 0
	for _, g := range got {
		if i < len(want) && g == want[i] {
			i++
		}
	}
	return i == len(want)
}

// seededID returns the fixed id of acme's seeded entry at p.
func (s *storage) seededID(p string) (string, error) {
	id, ok := s.paths[p]
	if !ok {
		return "", fmt.Errorf("the seed has no entry at %s", p)
	}
	return id, nil
}

func (s *storage) missingIfMatch(ctx context.Context, inv *cli.Invocation, r *scenario.Reporter) error {
	st := styleOf(s.n, inv)
	id, err := s.seededID(guardedFilePath)
	if err != nil {
		return err
	}
	r.Note("A move or a delete acts on a row the client read, which another request may have changed since, so it carries the version it read as If-Match. This reads the seeded %s for its version, then deletes it with no If-Match: 428 Precondition Required, refused before anything is touched.", guardedFilePath[1:])
	filePath := documentsRoute(s.acme.ID) + "/files/" + id
	if err := s.get(ctx, r, st, filePath, &s.guarded); err != nil {
		return err
	}
	res, err := call(ctx, r, st, s.client, http.MethodDelete, filePath, nil, nil)
	if err != nil {
		return err
	}
	_, err = res.Problem(http.StatusPreconditionRequired)
	return err
}

func (s *storage) staleIfMatch(ctx context.Context, inv *cli.Invocation, r *scenario.Reporter) error {
	st := styleOf(s.n, inv)
	stale := s.guarded.Version - 1
	if stale < 1 {
		stale = s.guarded.Version + 1
	}
	r.Note("The same delete with a well-formed If-Match naming version %d, not the %d the read showed, is 412 Precondition Failed: the guarded statement matches no row at that version, so the file stays.", stale, s.guarded.Version)
	filePath := documentsRoute(s.acme.ID) + "/files/" + s.guarded.ID
	headers := []httpx.Header{httpx.IfMatch(stale)}
	res, err := call(ctx, r, st, s.client, http.MethodDelete, filePath, headers, nil)
	if err != nil {
		return err
	}
	_, err = res.Problem(http.StatusPreconditionFailed)
	return err
}

func (s *storage) takenName(ctx context.Context, inv *cli.Invocation, r *scenario.Reporter) error {
	st := styleOf(s.n, inv)
	name := path.Base(takenDirPath)
	r.Note("Names are unique within a directory. Creating a second %s directory under the root is a 409 Conflict whose detail is a fixed text naming the kind of conflict, %q, never the underlying error's own, which would name the library's operation, ids, and constraints.", name, detailNameTaken)
	route := documentsRoute(s.acme.ID) + "/directories"
	body := document.CreateDirectory{ParentID: document.RootAlias, Name: name}
	res, err := call(ctx, r, st, s.client, http.MethodPost, route, nil, body)
	if err != nil {
		return err
	}
	p, err := res.Problem(http.StatusConflict)
	if err != nil {
		return err
	}
	if p.Detail != detailNameTaken {
		return fmt.Errorf("the conflict's detail is %q, want %q", p.Detail, detailNameTaken)
	}
	return nil
}

func (s *storage) anotherOrganization(ctx context.Context, inv *cli.Invocation, r *scenario.Reporter) error {
	st := styleOf(s.n, inv)
	id, err := s.seededID(takenDirPath + "/")
	if err != nil {
		return err
	}
	r.Note("Every route that takes an id checks its scope first: the id must lie within the addressed organization's root. acme's %s directory reads under acme, and the same id under finance is 404, indistinguishable from an id that does not exist, so no request reads, moves, or deletes across organizations.", path.Base(takenDirPath))
	var d document.Directory
	if err := s.get(ctx, r, st, directoryRoute(s.acme.ID, id), &d); err != nil {
		return err
	}
	other := directoryRoute(s.other.ID, id)
	res, err := call(ctx, r, st, s.client, http.MethodGet, other, nil, nil)
	if err != nil {
		return err
	}
	_, err = res.Problem(http.StatusNotFound)
	return err
}

func (s *storage) createBranch(ctx context.Context, inv *cli.Invocation, r *scenario.Reporter) error {
	st := styleOf(s.n, inv)
	name := "storage-demo-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	r.Note("A small branch for the recursive delete to remove: a directory under the root named for this run, %s, so a second run never collides with the first, a subdirectory in it, and a file in that. The directory's read closes the step with its status, active, and the version the delete guards on.", name)
	route := documentsRoute(s.acme.ID) + "/directories"
	top, err := createDirectory(ctx, r, st, s.client, route, document.CreateDirectory{ParentID: document.RootAlias, Name: name})
	if err != nil {
		return err
	}
	sub, err := createDirectory(ctx, r, st, s.client, route, document.CreateDirectory{ParentID: top.ID, Name: "drafts"})
	if err != nil {
		return err
	}
	filePath := directoryRoute(s.acme.ID, sub.ID) + "/files?name=notes.txt"
	headers := []httpx.Header{{Name: "Content-Type", Value: "text/plain; charset=utf-8"}}
	content := "Draft notes, removed with their branch.\n"
	res, err := call(ctx, r, st, s.client, http.MethodPost, filePath, headers, content)
	if err != nil {
		return err
	}
	if err := res.Expect(http.StatusCreated); err != nil {
		return err
	}
	if err := s.get(ctx, r, st, directoryRoute(s.acme.ID, top.ID), &s.branch); err != nil {
		return err
	}
	if s.branch.Status != document.DirectoryActive {
		return fmt.Errorf("the new directory's status is %q, want %q", s.branch.Status, document.DirectoryActive)
	}
	return nil
}

func (s *storage) recursiveDelete(ctx context.Context, inv *cli.Invocation, r *scenario.Reporter) error {
	st := styleOf(s.n, inv)
	r.Note("With recursive=true a directory delete takes the branch in two stages: one transaction marks the directory and everything beneath it deleting, and the request answers 202 Accepted with the directory's read as its Location; the sweep, nudged after the commit, then removes the rows and their objects. Until it finishes the directory reads deleting, and a listing within the branch is 404. The nudged sweep is quick, so the read may already find the branch gone.")
	route := directoryRoute(s.acme.ID, s.branch.ID) + "?recursive=true"
	headers := []httpx.Header{httpx.IfMatch(s.branch.Version)}
	res, err := call(ctx, r, st, s.client, http.MethodDelete, route, headers, nil)
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
	u, err := url.Parse(location)
	if err != nil {
		return fmt.Errorf("location %q: %w", location, err)
	}
	s.location = u.RequestURI()
	res, err = call(ctx, r, st, s.client, http.MethodGet, s.location, nil, nil)
	if err != nil {
		return err
	}
	switch res.Status {
	case http.StatusOK:
		var d document.Directory
		if err := res.JSON(&d); err != nil {
			return err
		}
		if d.Status != document.DirectoryDeleting {
			return fmt.Errorf("the marked directory's status is %q, want %q", d.Status, document.DirectoryDeleting)
		}
	case http.StatusNotFound:
		r.Note("The sweep removed the branch before this read.")
	default:
		return res.Expect(http.StatusOK)
	}
	listing := s.location + "/directories"
	res, err = call(ctx, r, st, s.client, http.MethodGet, listing, nil, nil)
	if err != nil {
		return err
	}
	_, err = res.Problem(http.StatusNotFound)
	return err
}

func (s *storage) sweep(ctx context.Context, inv *cli.Invocation, r *scenario.Reporter) error {
	st := styleOf(s.n, inv)
	r.Note("This polls the Location, as dirs delete --wait does, until it answers 404: the sweep has removed the branch's rows and objects. It gives up after %s.", sweepLimit)
	took, err := document.AwaitSweep(ctx, document.NewClient(s.client), s.location, sweepLimit)
	if err != nil {
		return err
	}
	r.Note("The sweep removed the branch within %s.", took.Round(time.Millisecond))
	res, err := call(ctx, r, st, s.client, http.MethodGet, s.location, nil, nil)
	if err != nil {
		return err
	}
	_, err = res.Problem(http.StatusNotFound)
	return err
}

func (s *storage) reset(ctx context.Context, inv *cli.Invocation, r *scenario.Reporter) error {
	st := styleOf(s.n, inv)
	r.Note("A reset is what actually clears: it reverts every migration set, reapplies them, and seeds the %q state, answering with the schema status and what it seeded. The counts are the seed file's: its organizations, their logos, and acme's document entries, directories and files.", seedState)
	body := database.Reset{State: seedState, Confirm: true}
	res, err := call(ctx, r, st, s.client, http.MethodPost, stateRoute, nil, body)
	if err != nil {
		return err
	}
	if err := res.Expect(http.StatusOK); err != nil {
		return err
	}
	var tr struct {
		Seeded seeded `json:"seeded"`
	}
	if err := res.JSON(&tr); err != nil {
		return err
	}
	if err := show(r, "Seeded", output.Table(st, [][2]string{
		{"organizations", strconv.Itoa(tr.Seeded.Organizations)},
		{"logos", strconv.Itoa(tr.Seeded.Logos)},
		{"documents", strconv.Itoa(tr.Seeded.Documents)},
	})); err != nil {
		return err
	}
	if want := s.seed.seedCounts(); tr.Seeded != want {
		return fmt.Errorf("the reset seeded %+v, want the seed file's %+v", tr.Seeded, want)
	}
	return nil
}

// summarized returns res with a body that is not JSON replaced by a line
// naming its length and type, for a download whose bytes would print as
// noise; a JSON body, a problem document among them, is left as it is.
func summarized(res *httpx.Response) *httpx.Response {
	if len(res.Body) == 0 || json.Valid(res.Body) {
		return res
	}
	c := *res
	c.Body = []byte(fmt.Sprintf("(%d bytes of %s)", len(res.Body), res.Header.Get("Content-Type")))
	return &c
}
