package organization_test

import (
	"context"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/standards-lab/blobfs"
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

// fileRows scripts a read of blobfs file rows, one per file given.
func fileRows(files ...blobfs.File) sqltest.Response {
	r := sqltest.Response{Columns: []string{"id", "directory_id", "name", "status", "key", "size", "content_type", "etag", "version", "created_at", "updated_at"}}
	for _, f := range files {
		var size, etag driver.Value
		if f.Size != nil {
			size = *f.Size
		}
		if f.ETag != nil {
			etag = *f.ETag
		}
		r.Rows = append(r.Rows, []driver.Value{f.ID, dirID, f.Name, string(f.Status), f.Key, size, f.ContentType, etag, f.Version, f.CreatedAt, f.UpdatedAt})
	}
	return r
}

// file is a logo's file row at a status and version; an available one
// carries the size and the entity tag its completion recorded.
func file(id string, status blobfs.Status, version int64) blobfs.File {
	now := time.Now()
	f := blobfs.File{ID: id, Name: id + ".png", Status: status, Key: id + "/" + id + ".png", ContentType: "image/png", Version: version, CreatedAt: now, UpdatedAt: now}
	if status != blobfs.StatusPending {
		size, etag := int64(3), `"etag-`+id+`"`
		f.Size, f.ETag = &size, &etag
	}
	return f
}

func directoryRow() sqltest.Response {
	now := time.Now()
	return sqltest.Response{
		Columns: []string{"id", "parent_id", "name", "status", "version", "created_at", "updated_at"},
		Rows:    [][]driver.Value{{dirID, blobfs.RootID, "organization-images", string(blobfs.DirectoryStatusActive), int64(1), now, now}},
	}
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
		directoryRow(), // the images directory, found on the pool
		row(),          // write: the organization exists
		fileRows(file(newFileID, blobfs.StatusPending, 1)),   // write: the pending file, alone
		fileRows(file(newFileID, blobfs.StatusAvailable, 2)), // complete, on the pool
		exec(1), // activate: the hold
		fileRows(file(oldFileID, blobfs.StatusAvailable, 2)), // activate: the current logo
		exec(1), // activate: the replaced image removed
		fileRows(file(oldFileID, blobfs.StatusDeleting, 3)), // activate: the replaced file's delete begun
		exec(1), // activate: the new image, active
		exec(1), // the replaced file purged, on the pool
		// Logo.
		fileRows(file(newFileID, blobfs.StatusAvailable, 2)),
		// DeleteLogo.
		fileRows(file(newFileID, blobfs.StatusAvailable, 2)), // the active logo
		exec(1), // its image removed
		fileRows(file(newFileID, blobfs.StatusDeleting, 3)), // the delete begun
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
		directoryRow(), row(), fileRows(file(newFileID, blobfs.StatusPending, 1)),
		fileRows(file(newFileID, blobfs.StatusDeleting, 2)), exec(1), // the abandon: the delete begun, the purge
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
		read sqltest.Response
		want error
	}{
		"reclaiming": {fileRows(file(newFileID, blobfs.StatusDeleting, 2)), blobfs.ErrDeleting},
		"reclaimed":  {fileRows(), blobfs.ErrNotFound},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			s, rec, fake := serviceOver(t, sqltest.ReturningDialect{},
				directoryRow(), row(), fileRows(file(newFileID, blobfs.StatusPending, 1)),
				fileRows(), c.read, // the completion matches no pending row; blobfs reads why
			)
			if _, err := s.PutLogo(context.Background(), validID, logoUpload(t)); !errors.Is(err, c.want) {
				t.Fatalf("PutLogo = %v; want %v", err, c.want)
			}
			if fake.Puts() != 1 {
				t.Errorf("puts = %d; want the object put once", fake.Puts())
			}
			key := file(newFileID, blobfs.StatusPending, 1).Key
			if _, err := fake.Get(context.Background(), key, storage.GetOptions{}); !errors.Is(err, storage.ErrNotFound) {
				t.Errorf("the object outlived the refused completion: %v", err)
			}
			sameOps(t, rec, q, begin, q, q, commit, q, q)
			noImage(t, rec)
		})
	}
}

// A replacement that loses the race to activate is the unique violation,
// 409 on the wire, and its own file is retired rather than left behind:
// the activation rolled back, so no image references it.
func TestStore_ALostActivationIs409AndRetiresTheNewFile(t *testing.T) {
	s, rec, fake := serviceOver(t, sqltest.ReturningDialect{},
		directoryRow(), row(), fileRows(file(newFileID, blobfs.StatusPending, 1)),
		fileRows(file(newFileID, blobfs.StatusAvailable, 2)),
		exec(1), fileRows(), // the hold, no current logo
		sqltest.Response{Err: &sqlate.ConstraintError{Constraint: "ux_organization_image_active", Class: sqlate.ErrUniqueViolation, Err: errors.New("unique")}},
		fileRows(file(newFileID, blobfs.StatusDeleting, 3)), exec(1), // the new file retired
	)
	r := web.NewRouter()
	r.Mount(web.NewModule(organization.Routes(s, web.Limits{DefaultSize: 20, MaxSize: 100})))
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
	h := module(t, directoryRow(), sqltest.Response{Columns: []string{"id", "parent_id", "code", "name", "version", "created_at", "updated_at", "path"}})
	problem(t, upload(t, h, "/organizations/"+validID+"/logo", "image/png", "png", 3), 404)
}

// A first logo answers 201 with the new file's identity and the logo's own
// path as its Location; there is no replaced file to retire.
func TestPutLogo_AnswersCreatedWithLocation(t *testing.T) {
	s, db, _ := serviceOver(t, sqltest.ReturningDialect{},
		directoryRow(), row(), fileRows(file(newFileID, blobfs.StatusPending, 1)),
		fileRows(file(newFileID, blobfs.StatusAvailable, 2)),
		exec(1), fileRows(), exec(1), // the hold, no current logo, the new image
	)
	r := web.NewRouter()
	r.Mount(web.NewModule(organization.Routes(s, web.Limits{DefaultSize: 20, MaxSize: 100})))
	path := "/organizations/" + validID + "/logo"
	rec := upload(t, r, path, "image/png", "png", 3)
	if rec.Code != 201 || rec.Header().Get("Location") != path || strings.TrimSpace(rec.Body.String()) != `{"id":"`+newFileID+`"}` {
		t.Fatalf("status %d, Location %q, body %s", rec.Code, rec.Header().Get("Location"), rec.Body)
	}
	sameOps(t, db, q, begin, q, q, commit, q, begin, x, q, x, commit)
}
