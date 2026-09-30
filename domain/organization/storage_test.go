package organization_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/png"
	"io"
	"log/slog"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/standards-lab/blobfs"
	"github.com/standards-lab/blobfs/data/datatest"
	"github.com/standards-lab/go-storage"
	"github.com/standards-lab/go-web-sdk"
	"github.com/standards-lab/sqlate"
	"github.com/standards-lab/sqlate/sqltest"

	"github.com/standards-lab/go-web-service/domain/organization"
)

const (
	oldFileID = "00000000-0000-7000-8000-00000000000a"
	newFileID = "00000000-0000-7000-8000-00000000000b"
	dirID     = "00000000-0000-7000-8000-00000000000d"
)

// file is a logo's file row at a status and version; an available one
// carries the size and the entity tag its completion recorded.
func file(id string, status blobfs.Status, version int64) blobfs.File {
	now := time.Now()
	f := blobfs.File{ID: id, DirectoryID: dirID, Name: id + ".png", Status: status, Key: id + "/" + id + ".png", ContentType: "image/png", Version: version, CreatedAt: now, UpdatedAt: now}
	if status != blobfs.StatusPending {
		size, etag := int64(3), `"etag-`+id+`"`
		f.Size, f.ETag = &size, &etag
	}
	return f
}

// imagesDirectory is the structural directory the logos' files sit in.
func imagesDirectory() blobfs.Directory {
	now, root := time.Now(), blobfs.RootID
	return blobfs.Directory{ID: dirID, ParentID: &root, Name: "organization-images", Status: blobfs.DirectoryStatusActive, Version: 1, CreatedAt: now, UpdatedAt: now}
}

func exec(n int64) sqltest.Response { return sqltest.Response{Affected: n} }

// logoUpload is a three-byte PNG upload as the handler reads it.
func logoUpload(t *testing.T) web.Upload {
	t.Helper()
	r := httptest.NewRequest("PUT", "/", strings.NewReader("png"))
	r.Header.Set("Content-Type", "image/png")
	u, err := web.ReadUpload(httptest.NewRecorder(), r, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	return u
}

// The storage wiring test: the logo's three protocols run end to end over
// the scripted driver and the fake object store, a replacement first, so
// every image statement binds its file's parameters and each step lands on
// the session the protocol gives it. The returning dialect runs blobfs's
// returning commands as one statement each.
func TestStore_LogoProtocolsBindTheirFilesParameters(t *testing.T) {
	ctx := context.Background()
	s, rec, fake := serviceOver(t, sqltest.ReturningDialect{},
		// PutLogo, replacing the active logo.
		datatest.DirectoryRows(imagesDirectory()), // the images directory, found on the pool
		exists(), // write: the organization exists
		datatest.FileRows(file(newFileID, blobfs.StatusPending, 1)),   // write: the pending file, alone
		datatest.FileRows(file(newFileID, blobfs.StatusAvailable, 2)), // complete, on the pool
		exec(1), // activate: the hold
		datatest.FileRows(file(oldFileID, blobfs.StatusAvailable, 2)), // activate: the current logo
		exec(1), // activate: the replaced image removed
		datatest.FileRows(file(oldFileID, blobfs.StatusDeleting, 3)), // activate: the replaced file's delete begun
		exec(1), // activate: the new image, active
		exec(1), // the replaced file purged, on the pool
		// Logo.
		datatest.FileRows(file(newFileID, blobfs.StatusAvailable, 2)),
		// DeleteLogo.
		datatest.FileRows(file(newFileID, blobfs.StatusAvailable, 2)), // the active logo
		exec(1), // its image removed
		datatest.FileRows(file(newFileID, blobfs.StatusDeleting, 3)), // the delete begun
		exec(1), // the purge
	)
	old := file(oldFileID, blobfs.StatusAvailable, 2)
	if _, err := fake.Put(ctx, old.Key, strings.NewReader("old"), storage.PutOptions{}); err != nil {
		t.Fatal(err)
	}
	id, err := s.PutLogo(ctx, validID, logoUpload(t))
	if err != nil || id != (organization.LogoIdentity{ID: newFileID}) {
		t.Fatalf("PutLogo = %+v, %v", id, err)
	}
	create := rec.Calls()[3]
	if name, key := create.Args[1], create.Args[2]; !strings.HasPrefix(create.SQL, "INSERT INTO blobfs_file") || name != fmt.Sprint(create.Args[0])+".png" || key != fmt.Sprint(create.Args[0])+"/"+fmt.Sprint(name) {
		t.Errorf("create = %q %v; want the file named for its minted id", create.SQL, create.Args)
	}
	if attach := rec.Calls()[11]; !strings.HasPrefix(attach.SQL, "INSERT INTO organization_image") || fmt.Sprint(attach.Args) != fmt.Sprint([]any{validID, newFileID}) {
		t.Errorf("attach = %q %v; want the new file bound in the activation", attach.SQL, attach.Args)
	}
	if fake.Puts() != 2 {
		t.Errorf("puts = %d, want the replaced logo's seed and the upload", fake.Puts())
	}
	if _, err := fake.Get(ctx, old.Key, storage.GetOptions{}); !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("the replaced logo's object outlived its retire: %v", err)
	}

	logo, err := s.Logo(ctx, validID)
	if err != nil || logo.Object != (web.Object{ContentType: "image/png", Size: 3, ETag: `"etag-` + newFileID + `"`, ModifiedAt: logo.Object.ModifiedAt}) {
		t.Fatalf("Logo = %+v, %v", logo.Object, err)
	}
	body, err := logo.Open()
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := io.ReadAll(body); string(b) != "png" {
		t.Errorf("logo bytes = %q", b)
	}
	_ = body.Close()

	if err := s.DeleteLogo(ctx, validID); err != nil {
		t.Fatalf("DeleteLogo = %v", err)
	}
	if _, err := logo.Open(); err == nil {
		t.Error("the logo's object outlived its delete")
	}
	sameOps(t, rec,
		q,
		begin, q, q, commit,
		q,
		begin, x, q, x, q, x, commit, x,
		q,
		begin, q, x, q, commit, x,
	)
}

// A put that fails abandons the write: the pending row is retired by the
// delete protocol, and no image was ever written, so nothing is left to
// block the organization's delete.
func TestStore_AFailedPutAbandonsThePendingRow(t *testing.T) {
	s, rec, fake := serviceOver(t, sqltest.ReturningDialect{},
		datatest.DirectoryRows(imagesDirectory()), exists(), datatest.FileRows(file(newFileID, blobfs.StatusPending, 1)),
		datatest.FileRows(file(newFileID, blobfs.StatusDeleting, 2)), exec(1), // the abandon: the delete begun, the purge
	)
	fake.FailPut(errors.New("put failed"))
	if _, err := s.PutLogo(context.Background(), validID, logoUpload(t)); err == nil {
		t.Fatal("PutLogo succeeded over a failed put")
	}
	sameOps(t, rec, q, begin, q, q, commit, begin, q, commit, x)
	noImage(t, rec)
}

// The writer rule: a completion refused because the stale reclaim reached
// the pending row, or removed it, deletes the object the put stored under
// the key the write holds, leaves the row to the sweep, and activates
// nothing.
func TestStore_ARefusedCompletionDeletesTheLogosObject(t *testing.T) {
	cases := map[string]struct {
		reads []sqltest.Response
		want  error
	}{
		// blobfs reads the deleting row's directory to say whose delete
		// refused it: the file's own, in the active images directory.
		"reclaiming": {[]sqltest.Response{datatest.FileRows(file(newFileID, blobfs.StatusDeleting, 2)), datatest.DirectoryRows(imagesDirectory())}, blobfs.ErrDeleting},
		"reclaimed":  {[]sqltest.Response{datatest.FileRows()}, blobfs.ErrNotFound},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			s, rec, fake := serviceOver(t, sqltest.ReturningDialect{}, append([]sqltest.Response{
				datatest.DirectoryRows(imagesDirectory()), exists(), datatest.FileRows(file(newFileID, blobfs.StatusPending, 1)),
				datatest.FileRows(), // the completion matches no pending row; blobfs reads why
			}, c.reads...)...)
			_, err := s.PutLogo(context.Background(), validID, logoUpload(t))
			if !errors.Is(err, c.want) {
				t.Fatalf("PutLogo = %v; want %v", err, c.want)
			}
			if de := (*blobfs.DeletingError)(nil); errors.As(err, &de) && de.Directory {
				t.Errorf("PutLogo = %v; want the file's own delete, not its directory's", err)
			}
			if fake.Puts() != 1 {
				t.Errorf("puts = %d; want the object put once", fake.Puts())
			}
			key := file(newFileID, blobfs.StatusPending, 1).Key
			if _, err := fake.Get(context.Background(), key, storage.GetOptions{}); !errors.Is(err, storage.ErrNotFound) {
				t.Errorf("the object outlived the refused completion: %v", err)
			}
			want := []sqltest.Op{q, begin, q, q, commit, q}
			for range c.reads {
				want = append(want, q)
			}
			sameOps(t, rec, want...)
			noImage(t, rec)
		})
	}
}

// hangUp is a request's context whose client hangs up once the recorder
// has logged after calls: its Done closes then, and Err reports the
// cancellation. Every call returns the one channel, so a watcher that read
// it earlier sees it close too.
type hangUp struct {
	context.Context
	rec   *sqltest.Recorder
	after int
	once  sync.Once
	done  chan struct{}
}

func newHangUp(rec *sqltest.Recorder, after int) *hangUp {
	return &hangUp{Context: context.Background(), rec: rec, after: after, done: make(chan struct{})}
}

func (c *hangUp) Done() <-chan struct{} {
	if len(c.rec.Calls()) >= c.after {
		c.once.Do(func() { close(c.done) })
	}
	return c.done
}

func (c *hangUp) Err() error {
	select {
	case <-c.Done():
		return context.Canceled
	default:
		return nil
	}
}

// A client that hangs up once its file is written, as the activation
// begins, cancels the activation, and the new file, available and
// unreferenced, is still retired, its row and its object: the stale
// reclaim, which reaches only pending and deleting rows, would never
// remove it.
func TestStore_AHangUpAfterTheWriteRetiresTheNewFile(t *testing.T) {
	s, rec, fake := serviceOver(t, sqltest.ReturningDialect{},
		datatest.DirectoryRows(imagesDirectory()), exists(), datatest.FileRows(file(newFileID, blobfs.StatusPending, 1)),
		datatest.FileRows(file(newFileID, blobfs.StatusAvailable, 2)),         // complete, on the pool
		datatest.FileRows(file(newFileID, blobfs.StatusDeleting, 3)), exec(1), // the new file retired
	)
	// The calls before the hang-up: the directory, the write's
	// transaction, the completion, and the activation's begin.
	ctx := newHangUp(rec, 7)
	if _, err := s.PutLogo(ctx, validID, logoUpload(t)); !errors.Is(err, context.Canceled) {
		t.Fatalf("PutLogo = %v; want the hang-up's cancellation", err)
	}
	if rec.Pending() != 0 {
		t.Errorf("pending = %d; want the retire to run after the hang-up: %v", rec.Pending(), rec.Ops())
	}
	var retired bool
	for _, c := range rec.Calls() {
		if strings.HasPrefix(c.SQL, "UPDATE blobfs_file") && len(c.Args) > 0 && c.Args[0] == newFileID {
			retired = true
		}
	}
	if !retired {
		t.Errorf("the new file's delete never began: %v", rec.Ops())
	}
	if _, err := fake.Get(context.Background(), file(newFileID, blobfs.StatusPending, 1).Key, storage.GetOptions{}); !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("the new logo's object outlived the hang-up: %v", err)
	}
	noImage(t, rec)
}

// A replacement that loses the race to activate is the unique violation,
// 409 on the wire, and its own file is retired rather than left behind:
// the activation rolled back, so no image references it.
func TestStore_ALostActivationIs409AndRetiresTheNewFile(t *testing.T) {
	s, rec, fake := serviceOver(t, sqltest.ReturningDialect{},
		datatest.DirectoryRows(imagesDirectory()), exists(), datatest.FileRows(file(newFileID, blobfs.StatusPending, 1)),
		datatest.FileRows(file(newFileID, blobfs.StatusAvailable, 2)),
		exec(1), datatest.FileRows(), // the hold, no current logo
		sqltest.Response{Err: &sqlate.ConstraintError{Constraint: "ux_organization_image_active", Class: sqlate.ErrUniqueViolation, Err: errors.New("unique")}},
		datatest.FileRows(file(newFileID, blobfs.StatusDeleting, 3)), exec(1), // the new file retired
	)
	r := web.NewRouter()
	r.Mount(web.NewModule(organization.Routes(s, web.Limits{DefaultSize: 20, MaxSize: 100}, slog.New(slog.DiscardHandler))))
	problem(t, upload(t, r, "/organizations/"+validID+"/logo", "image/png", "png", 3), 409)
	sameOps(t, rec,
		q,
		begin, q, q, commit,
		q,
		begin, x, q, x, sqltest.OpRollback,
		begin, q, commit, x,
	)
	if _, err := fake.Get(context.Background(), file(newFileID, blobfs.StatusPending, 1).Key, storage.GetOptions{}); !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("the losing logo's object outlived its retire: %v", err)
	}
}

// A new logo whose own delete began before its activation, the stale
// reclaim reaching it, is refused at the hold: blobfs reads the file's
// directory, active, and names the file's own delete, which is 409 with
// the file's detail on the wire. The activation rolled back, so the file
// is retired, a retry of the delete already begun.
func TestStore_AHoldRefusedByTheFilesOwnDeleteIs409(t *testing.T) {
	s, rec, fake := serviceOver(t, sqltest.ReturningDialect{},
		datatest.DirectoryRows(imagesDirectory()), exists(), datatest.FileRows(file(newFileID, blobfs.StatusPending, 1)),
		datatest.FileRows(file(newFileID, blobfs.StatusAvailable, 2)),
		exec(0), datatest.FileRows(file(newFileID, blobfs.StatusDeleting, 3)), // the hold matches nothing; blobfs reads the row: deleting
		datatest.DirectoryRows(imagesDirectory()),                             // and its directory: active
		datatest.FileRows(file(newFileID, blobfs.StatusDeleting, 3)), exec(1), // the new file retired
	)
	r := web.NewRouter()
	r.Mount(web.NewModule(organization.Routes(s, web.Limits{DefaultSize: 20, MaxSize: 100}, slog.New(slog.DiscardHandler))))
	body := problem(t, upload(t, r, "/organizations/"+validID+"/logo", "image/png", "png", 3), 409)
	if body["detail"] != "the file is being deleted" {
		t.Errorf("detail = %v; want the file's own delete", body["detail"])
	}
	sameOps(t, rec,
		q,
		begin, q, q, commit,
		q,
		begin, x, q, q, sqltest.OpRollback,
		begin, q, commit, x,
	)
	noImage(t, rec)
	if _, err := fake.Get(context.Background(), file(newFileID, blobfs.StatusPending, 1).Key, storage.GetOptions{}); !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("the refused logo's object outlived its retire: %v", err)
	}
}

// A refused hold whose file the stale reclaim already finished is still the
// refusal's 409: the abandon finds nothing to remove, and that absence is
// not the answer.
func TestStore_AHoldRefusedAfterTheReclaimIs409(t *testing.T) {
	s, rec, _ := serviceOver(t, sqltest.ReturningDialect{},
		datatest.DirectoryRows(imagesDirectory()), exists(), datatest.FileRows(file(newFileID, blobfs.StatusPending, 1)),
		datatest.FileRows(file(newFileID, blobfs.StatusAvailable, 2)),
		exec(0), datatest.FileRows(file(newFileID, blobfs.StatusDeleting, 3)), // the hold matches nothing; blobfs reads the row: deleting
		datatest.DirectoryRows(imagesDirectory()), // and its directory: active
		datatest.FileRows(), datatest.FileRows(),  // the abandon's delete matches nothing; its read finds the row purged
	)
	r := web.NewRouter()
	r.Mount(web.NewModule(organization.Routes(s, web.Limits{DefaultSize: 20, MaxSize: 100}, slog.New(slog.DiscardHandler))))
	body := problem(t, upload(t, r, "/organizations/"+validID+"/logo", "image/png", "png", 3), 409)
	if body["detail"] != "the file is being deleted" {
		t.Errorf("detail = %v; want the file's own delete", body["detail"])
	}
	sameOps(t, rec,
		q,
		begin, q, q, commit,
		q,
		begin, x, q, q, sqltest.OpRollback,
		begin, q, q, sqltest.OpRollback,
	)
	noImage(t, rec)
}

// noImage fails the test if any statement touched organization_image.
func noImage(t *testing.T, rec *sqltest.Recorder) {
	t.Helper()
	for _, c := range rec.Calls() {
		if strings.Contains(c.SQL, "organization_image") {
			t.Errorf("image statement ran: %q", c.SQL)
		}
	}
}

// sameOps fails the test unless the recorder's operations are want and
// every scripted response was consumed.
func sameOps(t *testing.T, rec *sqltest.Recorder, want ...sqltest.Op) {
	t.Helper()
	if got := rec.Ops(); fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("ops = %v\nwant %v", got, want)
	}
	if rec.Pending() != 0 || rec.RowsLeaked() != 0 {
		t.Errorf("pending = %d, leaked = %d", rec.Pending(), rec.RowsLeaked())
	}
}

const (
	q, x          = sqltest.OpQuery, sqltest.OpExec
	begin, commit = sqltest.OpBegin, sqltest.OpCommit
)

// A logo for an organization that does not exist is the missing row,
// before the file's row is written.
func TestStore_PutLogoForAMissingOrganizationIs404(t *testing.T) {
	h := module(t, datatest.DirectoryRows(imagesDirectory()), sqltest.Response{Columns: []string{"version"}})
	problem(t, upload(t, h, "/organizations/"+validID+"/logo", "image/png", "png", 3), 404)
}

// A first logo answers 201 with the new file's identity and the logo's own
// path as its Location; there is no replaced file to retire.
func TestPutLogo_AnswersCreatedWithLocation(t *testing.T) {
	s, db, _ := serviceOver(t, sqltest.ReturningDialect{},
		datatest.DirectoryRows(imagesDirectory()), exists(), datatest.FileRows(file(newFileID, blobfs.StatusPending, 1)),
		datatest.FileRows(file(newFileID, blobfs.StatusAvailable, 2)),
		exec(1), datatest.FileRows(), exec(1), // the hold, no current logo, the new image
	)
	r := web.NewRouter()
	r.Mount(web.NewModule(organization.Routes(s, web.Limits{DefaultSize: 20, MaxSize: 100}, slog.New(slog.DiscardHandler))))
	path := "/organizations/" + validID + "/logo"
	rec := upload(t, r, path, "image/png", "png", 3)
	if rec.Code != 201 || rec.Header().Get("Location") != path || strings.TrimSpace(rec.Body.String()) != `{"id":"`+newFileID+`"}` {
		t.Fatalf("status %d, Location %q, body %s", rec.Code, rec.Header().Get("Location"), rec.Body)
	}
	sameOps(t, db, q, begin, q, q, commit, q, begin, x, q, x, commit)
}

// seededLogoID is the fixed id a state gives the seeded logo's file.
const seededLogoID = "5eed0001-0000-4000-8000-000000000001"

// seedFixtures holds one fixture, a 1×1 PNG, as the seed's fixtures
// directory would.
func seedFixtures(t *testing.T) (fstest.MapFS, []byte) {
	t.Helper()
	var b bytes.Buffer
	if err := png.Encode(&b, image.NewRGBA(image.Rect(0, 0, 1, 1))); err != nil {
		t.Fatal(err)
	}
	return fstest.MapFS{"acme.png": {Data: b.Bytes()}}, b.Bytes()
}

// logoRows is the logos a state names: acme's, by path, under the fixed id.
var logoRows = json.RawMessage(`[{"organization":"/acme","id":"` + seededLogoID + `","fixture":"acme.png"}]`)

// The logo seed writes the fixture under the row's fixed id by the write
// protocol's retry-safe form, after the organization is resolved by path
// and found without a logo, then activates it as the organization's logo.
func TestLogoSeed_WritesAndActivatesTheFixture(t *testing.T) {
	ctx := context.Background()
	fixtures, body := seedFixtures(t)
	s, rec, fake := serviceOver(t, sqltest.ReturningDialect{},
		row(),               // the organization, by path
		datatest.FileRows(), // no active logo
		datatest.DirectoryRows(imagesDirectory()),                        // the images directory, found on the pool
		datatest.FileRows(),                                              // write: no file holds the name
		datatest.FileRows(file(seededLogoID, blobfs.StatusPending, 1)),   // write: the pending file, under the fixed id
		datatest.FileRows(file(seededLogoID, blobfs.StatusAvailable, 2)), // complete, on the pool
		exec(1), datatest.FileRows(), exec(1), // activate: the hold, still no logo, the image
	)
	n, err := s.LogoSeed().Write(ctx, logoRows, fixtures)
	if err != nil || n != 1 {
		t.Fatalf("Write = %d, %v; want the logo seeded", n, err)
	}
	if path := rec.Calls()[0].Args; fmt.Sprint(path) != "[/acme]" {
		t.Errorf("organization read with %v; want its path", path)
	}
	if insert := rec.Calls()[5]; !strings.HasPrefix(insert.SQL, "INSERT INTO blobfs_file") || insert.Args[0] != seededLogoID || insert.Args[1] != seededLogoID+".png" {
		t.Errorf("insert = %q %v; want the file under its fixed id, named for it", insert.SQL, insert.Args)
	}
	if attach := rec.Calls()[11]; !strings.HasPrefix(attach.SQL, "INSERT INTO organization_image") || fmt.Sprint(attach.Args) != fmt.Sprint([]any{validID, seededLogoID}) {
		t.Errorf("attach = %q %v; want the seeded file bound", attach.SQL, attach.Args)
	}
	blob, err := fake.Get(ctx, file(seededLogoID, blobfs.StatusPending, 1).Key, storage.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = blob.Body.Close() }()
	if b, _ := io.ReadAll(blob.Body); !bytes.Equal(b, body) || blob.ContentType != "image/png" {
		t.Errorf("stored %d bytes as %q; want the fixture as image/png", len(b), blob.ContentType)
	}
	sameOps(t, rec, q, q, q, begin, q, q, commit, q, begin, x, q, x, commit)
}

// An organization with an active logo, the seed's from an earlier run or a
// client's, is left alone: nothing is written.
func TestLogoSeed_LeavesAnActiveLogoAlone(t *testing.T) {
	fixtures, _ := seedFixtures(t)
	s, rec, fake := serviceOver(t, sqltest.ReturningDialect{},
		row(), datatest.FileRows(file(newFileID, blobfs.StatusAvailable, 2)),
	)
	if n, err := s.LogoSeed().Write(context.Background(), logoRows, fixtures); err != nil || n != 0 {
		t.Fatalf("Write = %d, %v; want nothing seeded", n, err)
	}
	if fake.Puts() != 0 {
		t.Errorf("puts = %d; want none", fake.Puts())
	}
	sameOps(t, rec, q, q)
}

// A file an interrupted run completed but never activated is found by its
// name and activated, nothing put again.
func TestLogoSeed_ActivatesAFileAnEarlierRunCompleted(t *testing.T) {
	fixtures, _ := seedFixtures(t)
	s, rec, fake := serviceOver(t, sqltest.ReturningDialect{},
		row(), datatest.FileRows(), datatest.DirectoryRows(imagesDirectory()),
		datatest.FileRows(file(seededLogoID, blobfs.StatusAvailable, 2)), // write: the file, found
		exec(1), datatest.FileRows(), exec(1),
	)
	if n, err := s.LogoSeed().Write(context.Background(), logoRows, fixtures); err != nil || n != 1 {
		t.Fatalf("Write = %d, %v; want the found file activated", n, err)
	}
	if fake.Puts() != 0 {
		t.Errorf("puts = %d; want none", fake.Puts())
	}
	sameOps(t, rec, q, q, q, begin, q, commit, begin, x, q, x, commit)
}

// A logo that became active between the check and the activation is left
// alone, and the seeded file, which no image references, is retired.
func TestLogoSeed_ALogoActivatedMeanwhileRetiresTheSeededFile(t *testing.T) {
	ctx := context.Background()
	fixtures, _ := seedFixtures(t)
	s, rec, fake := serviceOver(t, sqltest.ReturningDialect{},
		row(), datatest.FileRows(), datatest.DirectoryRows(imagesDirectory()),
		datatest.FileRows(), datatest.FileRows(file(seededLogoID, blobfs.StatusPending, 1)),
		datatest.FileRows(file(seededLogoID, blobfs.StatusAvailable, 2)),
		exec(1), datatest.FileRows(file(newFileID, blobfs.StatusAvailable, 2)), // the hold; a client's logo
		datatest.FileRows(file(seededLogoID, blobfs.StatusDeleting, 3)), exec(1), // the seeded file retired
	)
	if n, err := s.LogoSeed().Write(ctx, logoRows, fixtures); err != nil || n != 0 {
		t.Fatalf("Write = %d, %v; want nothing seeded", n, err)
	}
	if _, err := fake.Get(ctx, file(seededLogoID, blobfs.StatusPending, 1).Key, storage.GetOptions{}); !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("the retired file's object outlived it: %v", err)
	}
	sameOps(t, rec, q, q, q, begin, q, q, commit, q, begin, x, q, commit, begin, q, commit, x)
	for _, c := range rec.Calls() {
		if strings.HasPrefix(c.SQL, "INSERT INTO organization_image") {
			t.Errorf("the seeded file was bound: %q", c.SQL)
		}
	}
}

// What the seed does not own it leaves alone, retiring nothing: a file
// found under the id that another organization's image binds, whose
// organization a client renamed so the state's path names a new row, a
// file found deleting, and a row that holds the seeded name under another
// id.
func TestLogoSeed_LeavesAFileItDoesNotOwnAlone(t *testing.T) {
	fixtures, _ := seedFixtures(t)
	type seedCase struct {
		responses []sqltest.Response
		ops       []sqltest.Op
	}
	rollback := sqltest.OpRollback
	cases := map[string]seedCase{
		"bound elsewhere": {[]sqltest.Response{
			row(), datatest.FileRows(), datatest.DirectoryRows(imagesDirectory()),
			datatest.FileRows(file(seededLogoID, blobfs.StatusAvailable, 2)), // write: the file, found
			exec(1), datatest.FileRows(), // the hold, no logo for this organization
			{Err: &sqlate.ConstraintError{Constraint: "uq_organization_image_file", Class: sqlate.ErrUniqueViolation, Err: errors.New("unique")}},
		}, []sqltest.Op{q, q, q, begin, q, commit, begin, x, q, x, rollback}},
		"deleting": {[]sqltest.Response{
			row(), datatest.FileRows(), datatest.DirectoryRows(imagesDirectory()),
			datatest.FileRows(file(seededLogoID, blobfs.StatusDeleting, 3)),
			datatest.DirectoryRows(imagesDirectory()), // blobfs reads its directory to say whose delete
		}, []sqltest.Op{q, q, q, begin, q, commit, q}},
	}
	// A row that holds the seeded name under another id, pending or
	// available, is not the seed's: nothing is put over it or activated.
	for _, status := range []blobfs.Status{blobfs.StatusPending, blobfs.StatusAvailable} {
		other := file(newFileID, status, 2)
		other.Name = seededLogoID + ".png"
		cases["held under another id, "+string(status)] = seedCase{[]sqltest.Response{
			row(), datatest.FileRows(), datatest.DirectoryRows(imagesDirectory()), datatest.FileRows(other),
		}, []sqltest.Op{q, q, q, begin, q, commit}}
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			s, rec, fake := serviceOver(t, sqltest.ReturningDialect{}, c.responses...)
			if n, err := s.LogoSeed().Write(context.Background(), logoRows, fixtures); err != nil || n != 0 {
				t.Fatalf("Write = %d, %v; want the file left alone", n, err)
			}
			if fake.Puts() != 0 {
				t.Errorf("puts = %d; want none", fake.Puts())
			}
			// Nothing after the activation: no delete begins.
			sameOps(t, rec, c.ops...)
		})
	}
}

// Two seeds that share the file both activate it; the one that loses the
// activation finds the file active for the organization and leaves it,
// retiring nothing, though it stored the file too.
func TestLogoSeed_ALostActivationToAConcurrentSeedLeavesTheFile(t *testing.T) {
	fixtures, _ := seedFixtures(t)
	s, rec, fake := serviceOver(t, sqltest.ReturningDialect{},
		row(), datatest.FileRows(), datatest.DirectoryRows(imagesDirectory()),
		datatest.FileRows(file(seededLogoID, blobfs.StatusPending, 1)),   // write: the other seed's pending row, resumed
		datatest.FileRows(file(seededLogoID, blobfs.StatusAvailable, 2)), // complete
		exec(1), datatest.FileRows(), // the hold, no logo yet
		sqltest.Response{Err: &sqlate.ConstraintError{Constraint: "ux_organization_image_active", Class: sqlate.ErrUniqueViolation, Err: errors.New("unique")}},
		datatest.FileRows(file(seededLogoID, blobfs.StatusAvailable, 2)), // the active logo read again: the seeded file
	)
	if n, err := s.LogoSeed().Write(context.Background(), logoRows, fixtures); err != nil || n != 0 {
		t.Fatalf("Write = %d, %v; want the other seed's activation left", n, err)
	}
	if fake.Puts() != 1 {
		t.Errorf("puts = %d; want the resumed row's put", fake.Puts())
	}
	sameOps(t, rec, q, q, q, begin, q, commit, q, begin, x, q, x, sqltest.OpRollback, q)
}

// A different logo that wins the activation after the check, a client's
// upload racing the seed, is left alone: the bind's unique violation on
// the one active image is no error, and the seeded file, which no image
// references, is retired, as when the activation finds that logo itself.
func TestLogoSeed_ALostActivationToAnotherLogoRetiresTheSeededFile(t *testing.T) {
	ctx := context.Background()
	fixtures, _ := seedFixtures(t)
	s, rec, fake := serviceOver(t, sqltest.ReturningDialect{},
		row(), datatest.FileRows(), datatest.DirectoryRows(imagesDirectory()),
		datatest.FileRows(), datatest.FileRows(file(seededLogoID, blobfs.StatusPending, 1)),
		datatest.FileRows(file(seededLogoID, blobfs.StatusAvailable, 2)),
		exec(1), datatest.FileRows(), // the hold, no logo yet
		sqltest.Response{Err: &sqlate.ConstraintError{Constraint: "ux_organization_image_active", Class: sqlate.ErrUniqueViolation, Err: errors.New("unique")}},
		datatest.FileRows(file(newFileID, blobfs.StatusAvailable, 2)),            // the active logo read again: a client's
		datatest.FileRows(file(seededLogoID, blobfs.StatusDeleting, 3)), exec(1), // the seeded file retired
	)
	if n, err := s.LogoSeed().Write(ctx, logoRows, fixtures); err != nil || n != 0 {
		t.Fatalf("Write = %d, %v; want the other logo left and no error", n, err)
	}
	if _, err := fake.Get(ctx, file(seededLogoID, blobfs.StatusPending, 1).Key, storage.GetOptions{}); !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("the retired file's object outlived it: %v", err)
	}
	sameOps(t, rec, q, q, q, begin, q, q, commit, q, begin, x, q, x, sqltest.OpRollback, q, begin, q, commit, x)
}

// Once the change commits, a purge that fails is logged and the request
// succeeds: a replacement's replaced file and a delete's file are left
// deleting for the stale reclaim.
func TestStore_AFailedPurgeAfterTheCommitIsLogged(t *testing.T) {
	lost := sqltest.Response{Err: errors.New("connection lost")}
	cases := map[string]struct {
		responses []sqltest.Response
		run       func(*organization.Service) error
	}{
		"replace": {[]sqltest.Response{
			datatest.DirectoryRows(imagesDirectory()), exists(), datatest.FileRows(file(newFileID, blobfs.StatusPending, 1)),
			datatest.FileRows(file(newFileID, blobfs.StatusAvailable, 2)),
			exec(1), datatest.FileRows(file(oldFileID, blobfs.StatusAvailable, 2)), exec(1),
			datatest.FileRows(file(oldFileID, blobfs.StatusDeleting, 3)), exec(1),
			lost, // the replaced file's purge
		}, func(s *organization.Service) error {
			_, err := s.PutLogo(context.Background(), validID, logoUpload(t))
			return err
		}},
		"delete": {[]sqltest.Response{
			datatest.FileRows(file(oldFileID, blobfs.StatusAvailable, 2)), exec(1),
			datatest.FileRows(file(oldFileID, blobfs.StatusDeleting, 3)),
			lost, // the purge
		}, func(s *organization.Service) error { return s.DeleteLogo(context.Background(), validID) }},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			var logs bytes.Buffer
			s, rec, _ := serviceLogging(t, slog.New(slog.NewTextHandler(&logs, nil)), sqltest.ReturningDialect{}, c.responses...)
			if err := c.run(s); err != nil {
				t.Fatalf("= %v; want success once the change committed", err)
			}
			if rec.Pending() != 0 {
				t.Errorf("pending = %d", rec.Pending())
			}
			if out := logs.String(); !strings.Contains(out, "level=WARN") || !strings.Contains(out, oldFileID) {
				t.Errorf("log = %q; want a warning naming the file", out)
			}
		})
	}
}

// A fixture the upload would refuse, a type outside the allowlist or a
// body over its bound, or a row the file misspells, is refused before any
// I/O.
func TestLogoSeed_RefusesWhatTheUploadRefusesBeforeIO(t *testing.T) {
	cases := map[string]struct {
		fixtures fstest.MapFS
		rows     string
		want     string
	}{
		"svg":           {fstest.MapFS{"acme.png": {Data: []byte(`<svg xmlns="http://www.w3.org/2000/svg"/>`)}}, string(logoRows), "a logo is PNG"},
		"over 1 MiB":    {fstest.MapFS{"acme.png": {Data: make([]byte, 1<<20+1)}}, string(logoRows), "over the logo's"},
		"missing":       {fstest.MapFS{}, string(logoRows), "acme.png"},
		"unknown field": {fstest.MapFS{}, `[{"organization":"/acme","logo":"acme.png"}]`, "logo"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			s, rec, _ := serviceOver(t, sqltest.ReturningDialect{})
			if _, err := s.LogoSeed().Write(context.Background(), json.RawMessage(c.rows), c.fixtures); err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("Write = %v; want a refusal naming %q", err, c.want)
			}
			if len(rec.Calls()) != 0 {
				t.Errorf("a refused fixture reached the database: %v", rec.Ops())
			}
		})
	}
}
