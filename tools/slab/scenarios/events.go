package scenarios

import (
	"context"
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

	"github.com/standards-lab/go-web-service/tools/slab/domain/document"
	"github.com/standards-lab/go-web-service/tools/slab/domain/organization"
	"github.com/standards-lab/go-web-service/tools/slab/httpx"
	"github.com/standards-lab/go-web-service/tools/slab/output"
	"github.com/standards-lab/go-web-service/tools/slab/repo"
)

// watchCommand is the command the events scenario tells the reader to run
// in another terminal first: slab's own watcher, which prints each event
// the steps make the service publish.
const watchCommand = "mise run slab -- events watch"

// eventsParentPath is the seeded organization the scenario creates its
// organization under, before the transfer moves it to the root.
const eventsParentPath = "/acme"

// The seeded fixtures the logo steps upload, under fixturesDir: a first
// logo, then a second that replaces it. Any seeded PNG passes the upload's
// sniffed-type rule.
const (
	firstLogoFixture  = "operations.png"
	secondLogoFixture = "product.png"
)

// The directories the document steps create under the organization's root.
// The organization is new each run, so the names need no run stamp.
const (
	reportsDir = "reports"
	scratchDir = "scratch"
	emptyDir   = "empty"
	archiveDir = "archive"
)

// The files the upload step writes, their names after the move, and their
// bytes, inline: only the events' data is shown, never a file's content.
const (
	reportsFile    = "summary.txt"
	movedFile      = "summary-final.txt"
	scratchFile    = "notes.txt"
	reportsContent = "The quarter in one line.\n"
	scratchContent = "Scratch notes, swept with their directory.\n"
	textPlain      = "text/plain; charset=utf-8"
)

// eventsTotal is the number of events the scenario makes the service
// publish, and organizationEvents the organization domain's share of them,
// which its closing note states.
const (
	eventsTotal        = 20
	organizationEvents = 8
)

// events is what the events scenario's steps hand forward: the client, the
// run's stamp, the organization it creates with its version as last
// written, and the directories and the moved file as last written. Each is
// replaced by the step that writes it, the client by the first step, so
// nothing a run reads is left from a run before it.
type events struct {
	n       Nodes
	client  *httpx.Client
	stamp   string
	org     organization.Identity
	reports document.Identity
	scratch document.Identity
	empty   document.Identity
	file    document.Identity
}

// Events returns the events scenario over n: one command for each event
// the service publishes, against the running service, each step naming
// the events the watcher in another terminal prints for it. It writes no
// reset and names what it creates for the run, so it runs twice in a row
// without a reset between; each run leaves its first organization behind,
// with an empty document root, which no API removes. It declares the
// service node, which it calls, the repo node, which locates the fixtures
// its logo steps upload, and the no color node, which decides its blocks'
// color; it points at no trace, so it declares no grafana up.
func Events(n Nodes) scenario.Scenario {
	s := &events{n: n}
	return scenario.Scenario{
		Name:    "events",
		Summary: "Every event the service publishes, one command at a time against the running service, for slab events watch to print in another terminal",
		Nodes:   []graph.Ref{n.Service, n.Repo, n.NoColor},
		Steps: []scenario.Step{
			{Intent: "Organization Creation", Action: s.createOrganization},
			{Intent: "Organization Edit", Action: s.editOrganization},
			{Intent: "Organization Transfer", Action: s.transferOrganization},
			{Intent: "Logo Upload", Action: s.uploadLogo},
			{Intent: "Logo Replacement", Action: s.replaceLogo},
			{Intent: "Logo Delete", Action: s.deleteLogo},
			{Intent: "First Directory", Action: s.firstDirectory},
			{Intent: "More Directories", Action: s.moreDirectories},
			{Intent: "File Uploads", Action: s.uploadFiles},
			{Intent: "Directory Move", Action: s.moveDirectory},
			{Intent: "File Move", Action: s.moveFile},
			{Intent: "File Delete", Action: s.deleteFile},
			{Intent: "Empty Directory Delete", Action: s.deleteEmptyDirectory},
			{Intent: "Branch Delete", Action: s.deleteBranch},
			{Intent: "Last Directory Delete", Action: s.deleteLastDirectory},
			{Intent: "Organization Delete", Action: s.deleteOrganization},
		},
	}
}

func (s *events) createOrganization(ctx context.Context, inv *cli.Invocation, r *scenario.Reporter) error {
	s.client = inv.Get(s.n.Service).Client
	s.stamp = strconv.FormatInt(time.Now().UnixNano(), 36)
	r.Note("Run %q in another terminal first: it prints each event the service publishes as it arrives, its type, id, subject, time, and data. Each step below names the events it makes the watcher print. The service records an event in the transaction that makes its change true and its relay publishes it after the commit, so the watcher prints it a moment after the response.", watchCommand)
	parent, err := lookup(ctx, s.client, eventsParentPath)
	if err != nil {
		return err
	}
	code := "events-" + s.stamp
	r.Note("This creates %s under acme, a code named for this run, so a second run never collides with the first. The watcher prints web-service.organization.created, its subject the new organization's id and its data the stored row.", code)
	body := organization.CreateOrganization{ParentID: &parent.ID, Code: code, Name: "Events " + s.stamp}
	res, err := call(ctx, r, styleOf(s.n, inv), s.client, http.MethodPost, organization.Organizations, nil, body)
	if err != nil {
		return err
	}
	if err := res.Expect(http.StatusCreated); err != nil {
		return err
	}
	return res.JSON(&s.org)
}

func (s *events) editOrganization(ctx context.Context, inv *cli.Invocation, r *scenario.Reporter) error {
	r.Note("An edit renames the organization at the version the create returned. The watcher prints web-service.organization.edited, its data the row at its new version.")
	body := organization.EditOrganization{Code: "events-" + s.stamp, Name: "Events " + s.stamp + " (edited)"}
	return s.writeOrganization(ctx, inv, r, http.MethodPut, organization.Organizations+"/"+s.org.ID, body)
}

func (s *events) transferOrganization(ctx context.Context, inv *cli.Invocation, r *scenario.Reporter) error {
	r.Note("A transfer moves the organization out from under acme to the root: a null parent_id. The watcher prints web-service.organization.transferred.")
	body := organization.TransferOrganization{ParentID: nil}
	return s.writeOrganization(ctx, inv, r, http.MethodPost, organization.Organizations+"/"+s.org.ID+"/transfer", body)
}

// writeOrganization sends a narrated guarded command to route at the
// organization's version as last written, expects 200, and keeps the
// version the response names, one past the one sent.
func (s *events) writeOrganization(ctx context.Context, inv *cli.Invocation, r *scenario.Reporter, method, route string, body any) error {
	headers := []httpx.Header{httpx.IfMatch(s.org.Version)}
	res, err := call(ctx, r, styleOf(s.n, inv), s.client, method, route, headers, body)
	if err != nil {
		return err
	}
	if err := res.Expect(http.StatusOK); err != nil {
		return err
	}
	id, err := identityOf(res, s.org.ID)
	if err != nil {
		return err
	}
	if err := expectVersion(id, s.org.Version+1); err != nil {
		return err
	}
	s.org = id
	return nil
}

func (s *events) uploadLogo(ctx context.Context, inv *cli.Invocation, r *scenario.Reporter) error {
	r.Note("A logo PUT stores the seeded fixture %s as the organization's logo. The watcher prints web-service.organization.logo.uploaded, its data the stored file's description, never its bytes.", firstLogoFixture)
	return s.putLogo(ctx, inv, r, firstLogoFixture)
}

func (s *events) replaceLogo(ctx context.Context, inv *cli.Invocation, r *scenario.Reporter) error {
	r.Note("A second PUT, of %s, replaces the active logo. The watcher prints web-service.organization.logo.uploaded again and nothing for the logo it replaced: a replacement emits no logo.deleted.", secondLogoFixture)
	return s.putLogo(ctx, inv, r, secondLogoFixture)
}

// putLogo uploads the seeded fixture name as the organization's logo,
// narrated with its request's body summarized, and expects 201.
func (s *events) putLogo(ctx context.Context, inv *cli.Invocation, r *scenario.Reporter, name string) error {
	fsys, err := repo.FS(inv.Get(s.n.Repo))
	if err != nil {
		return err
	}
	file := path.Join(fixturesDir, name)
	png, err := fs.ReadFile(fsys, file)
	if err != nil {
		return err
	}
	st := styleOf(s.n, inv)
	headers := []httpx.Header{{Name: "Content-Type", Value: "image/png"}}
	if err := showApart(r, "", output.Request(st, http.MethodPut, logoRoute(s.org.ID), headers, fmt.Sprintf("(%d bytes of %s)", len(png), file))); err != nil {
		return err
	}
	res, err := s.client.Put(ctx, logoRoute(s.org.ID), png, headers...)
	if err != nil {
		return err
	}
	if err := showApart(r, "", output.Response(st, res)); err != nil {
		return err
	}
	return res.Expect(http.StatusCreated)
}

func (s *events) deleteLogo(ctx context.Context, inv *cli.Invocation, r *scenario.Reporter) error {
	r.Note("A DELETE retires the active logo. The watcher prints web-service.organization.logo.deleted.")
	res, err := call(ctx, r, styleOf(s.n, inv), s.client, http.MethodDelete, logoRoute(s.org.ID), nil, nil)
	if err != nil {
		return err
	}
	return res.Expect(http.StatusNoContent)
}

func (s *events) firstDirectory(ctx context.Context, inv *cli.Invocation, r *scenario.Reporter) error {
	r.Note("An organization's document root is created by its first document write, in that write's transaction. So creating %s under the root alias makes the watcher print web-service.document.directory.created twice: first for the root, named /, then for %s.", reportsDir, reportsDir)
	var err error
	s.reports, err = s.createDirectory(ctx, inv, r, document.RootAlias, reportsDir)
	return err
}

func (s *events) moreDirectories(ctx context.Context, inv *cli.Invocation, r *scenario.Reporter) error {
	r.Note("Two more directories under the root, %s and %s; the root is there now, so the watcher prints web-service.document.directory.created once for each.", scratchDir, emptyDir)
	var err error
	if s.scratch, err = s.createDirectory(ctx, inv, r, document.RootAlias, scratchDir); err != nil {
		return err
	}
	s.empty, err = s.createDirectory(ctx, inv, r, document.RootAlias, emptyDir)
	return err
}

// createDirectory creates the directory name under parent in the
// organization's documents, narrated.
func (s *events) createDirectory(ctx context.Context, inv *cli.Invocation, r *scenario.Reporter, parent, name string) (document.Identity, error) {
	route := documentsRoute(s.org.ID) + "/directories"
	return createDirectory(ctx, r, styleOf(s.n, inv), s.client, route, document.CreateDirectory{ParentID: parent, Name: name})
}

func (s *events) uploadFiles(ctx context.Context, inv *cli.Invocation, r *scenario.Reporter) error {
	r.Note("A file into %s and one into %s, their bytes inline. The watcher prints web-service.document.file.uploaded for each once its bytes are stored, its data the file's metadata, never its content.", reportsDir, scratchDir)
	var err error
	if s.file, err = s.upload(ctx, inv, r, s.reports.ID, reportsFile, reportsContent); err != nil {
		return err
	}
	_, err = s.upload(ctx, inv, r, s.scratch.ID, scratchFile, scratchContent)
	return err
}

// upload sends a narrated upload of content as name into the directory dir
// and expects 201.
func (s *events) upload(ctx context.Context, inv *cli.Invocation, r *scenario.Reporter, dir, name, content string) (document.Identity, error) {
	route := directoryRoute(s.org.ID, dir) + "/files?" + url.Values{"name": {name}}.Encode()
	headers := []httpx.Header{{Name: "Content-Type", Value: textPlain}}
	res, err := call(ctx, r, styleOf(s.n, inv), s.client, http.MethodPost, route, headers, content)
	if err != nil {
		return document.Identity{}, err
	}
	if err := res.Expect(http.StatusCreated); err != nil {
		return document.Identity{}, err
	}
	var id document.Identity
	if err := res.JSON(&id); err != nil {
		return document.Identity{}, err
	}
	return id, nil
}

func (s *events) moveDirectory(ctx context.Context, inv *cli.Invocation, r *scenario.Reporter) error {
	r.Note("A move takes %s from the root into %s and renames it %s, in one command. The watcher prints web-service.document.directory.moved, its data the directory at its new parent, name, and version.", emptyDir, reportsDir, archiveDir)
	body := document.MoveDirectory{ParentID: s.reports.ID, Name: archiveDir}
	route := directoryRoute(s.org.ID, s.empty.ID) + "/move"
	var err error
	s.empty, err = s.move(ctx, inv, r, route, s.empty, body)
	return err
}

func (s *events) moveFile(ctx context.Context, inv *cli.Invocation, r *scenario.Reporter) error {
	r.Note("A file move can rename in place: %s stays in %s as %s. The watcher prints web-service.document.file.moved.", reportsFile, reportsDir, movedFile)
	body := document.MoveFile{DirectoryID: s.reports.ID, Name: movedFile}
	route := documentsRoute(s.org.ID) + "/files/" + s.file.ID + "/move"
	var err error
	s.file, err = s.move(ctx, inv, r, route, s.file, body)
	return err
}

// move sends a narrated move of target to route with body, guarded at
// target's version, expects 200, and returns the identity the response
// names.
func (s *events) move(ctx context.Context, inv *cli.Invocation, r *scenario.Reporter, route string, target document.Identity, body any) (document.Identity, error) {
	headers := []httpx.Header{httpx.IfMatch(target.Version)}
	res, err := call(ctx, r, styleOf(s.n, inv), s.client, http.MethodPost, route, headers, body)
	if err != nil {
		return document.Identity{}, err
	}
	if err := res.Expect(http.StatusOK); err != nil {
		return document.Identity{}, err
	}
	var id document.Identity
	if err := res.JSON(&id); err != nil {
		return document.Identity{}, err
	}
	if id.ID != target.ID {
		return document.Identity{}, fmt.Errorf("the move names id %s, want %s", id.ID, target.ID)
	}
	return id, nil
}

func (s *events) deleteFile(ctx context.Context, inv *cli.Invocation, r *scenario.Reporter) error {
	r.Note("Deleting %s at the version its move returned makes the watcher print web-service.document.file.deleted.", movedFile)
	headers := []httpx.Header{httpx.IfMatch(s.file.Version)}
	res, err := call(ctx, r, styleOf(s.n, inv), s.client, http.MethodDelete, documentsRoute(s.org.ID)+"/files/"+s.file.ID, headers, nil)
	if err != nil {
		return err
	}
	return res.Expect(http.StatusNoContent)
}

func (s *events) deleteEmptyDirectory(ctx context.Context, inv *cli.Invocation, r *scenario.Reporter) error {
	r.Note("An empty directory's delete, without recursive, removes it at once, 204. Deleting %s makes the watcher print web-service.document.directory.deleted.", archiveDir)
	headers := []httpx.Header{httpx.IfMatch(s.empty.Version)}
	res, err := call(ctx, r, styleOf(s.n, inv), s.client, http.MethodDelete, directoryRoute(s.org.ID, s.empty.ID), headers, nil)
	if err != nil {
		return err
	}
	return res.Expect(http.StatusNoContent)
}

func (s *events) deleteBranch(ctx context.Context, inv *cli.Invocation, r *scenario.Reporter) error {
	r.Note("A recursive delete marks %s and everything beneath it deleting and answers 202; the sweep then removes the rows and their objects, and this waits for it, as dirs delete --wait does. The watcher prints one web-service.document.directory.deleted, for the marked directory alone, in the mark's transaction: %s beneath it emits nothing, and neither does the sweep's reclamation.", scratchDir, scratchFile)
	return s.deleteRecursively(ctx, inv, r, s.scratch.ID)
}

func (s *events) deleteLastDirectory(ctx context.Context, inv *cli.Invocation, r *scenario.Reporter) error {
	r.Note("The same recursive delete for %s, empty now, waited out. The watcher prints web-service.document.directory.deleted. The organization's root stays: no API removes it.", reportsDir)
	return s.deleteRecursively(ctx, inv, r, s.reports.ID)
}

// deleteRecursively deletes the directory id with recursive=true, narrated,
// expects 202, and waits for the sweep to remove it through
// document.AwaitSweep. The directory's version is read first without
// narration: it is bookkeeping, and a read emits no event.
func (s *events) deleteRecursively(ctx context.Context, inv *cli.Invocation, r *scenario.Reporter, id string) error {
	res, err := s.client.Get(ctx, directoryRoute(s.org.ID, id))
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
	headers := []httpx.Header{httpx.IfMatch(d.Version)}
	res, err = call(ctx, r, styleOf(s.n, inv), s.client, http.MethodDelete, directoryRoute(s.org.ID, id)+"?recursive=true", headers, nil)
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
	r.Note("The sweep removed %s within %s.", d.Name, took.Round(time.Millisecond))
	return nil
}

func (s *events) deleteOrganization(ctx context.Context, inv *cli.Invocation, r *scenario.Reporter) error {
	st := styleOf(s.n, inv)
	org, err := s.find(ctx)
	if err != nil {
		return err
	}
	r.Note("The organization this run created cannot be deleted: its document root and its logo history reference it, and no API removes the root. Its delete is 409 Conflict, and a command that fails commits no event, so the watcher prints nothing for it.")
	headers := []httpx.Header{httpx.IfMatch(org.Version)}
	res, err := call(ctx, r, st, s.client, http.MethodDelete, organization.Organizations+"/"+org.ID, headers, nil)
	if err != nil {
		return err
	}
	if _, err := res.Problem(http.StatusConflict); err != nil {
		return err
	}
	code := "events-" + s.stamp + "-deleted"
	r.Note("So the delete runs on a second organization made for it, %s at the root, with no documents and no logo. The watcher prints web-service.organization.created, then web-service.organization.deleted.", code)
	body := organization.CreateOrganization{Code: code, Name: "Events " + s.stamp + " (deleted)"}
	res, err = call(ctx, r, st, s.client, http.MethodPost, organization.Organizations, nil, body)
	if err != nil {
		return err
	}
	if err := res.Expect(http.StatusCreated); err != nil {
		return err
	}
	var created organization.Identity
	if err := res.JSON(&created); err != nil {
		return err
	}
	headers = []httpx.Header{httpx.IfMatch(created.Version)}
	res, err = call(ctx, r, st, s.client, http.MethodDelete, organization.Organizations+"/"+created.ID, headers, nil)
	if err != nil {
		return err
	}
	if err := res.Expect(http.StatusNoContent); err != nil {
		return err
	}
	r.Note("That is all %d events the service's commands publish: %d from the organization domain (created twice, edited, transferred, logo.uploaded twice, logo.deleted, and deleted) and %d from the document domain (directory.created four times, file.uploaded twice, directory.moved, file.moved, file.deleted, and directory.deleted three times). The run leaves events-%s behind at the root, its document root empty; a state reset (mise run slab -- admin database state --state default --confirm) clears it, and publishes web-service.database.reset.", eventsTotal, organizationEvents, eventsTotal-organizationEvents, s.stamp)
	return nil
}

// find reads the organization the run created, without narration, for the
// version its delete guards on: the steps since its transfer may have
// changed it, and a read emits no event.
func (s *events) find(ctx context.Context) (organization.Organization, error) {
	res, err := s.client.Get(ctx, organization.Organizations+"/"+s.org.ID)
	if err != nil {
		return organization.Organization{}, err
	}
	if err := res.Expect(http.StatusOK); err != nil {
		return organization.Organization{}, err
	}
	var o organization.Organization
	if err := res.JSON(&o); err != nil {
		return organization.Organization{}, err
	}
	return o, nil
}
