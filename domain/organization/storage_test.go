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
		Columns: []string{"id", "parent_id", "name", "version", "created_at", "updated_at"},
		Rows:    [][]driver.Value{{dirID, blobfs.RootID, "organization-images", int64(1), now, now}},
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
		fileRows(file(newFileID, blobfs.StatusPending, 1)), // write: the pending file
		exec(1), // write: the inactive image
		fileRows(file(newFileID, blobfs.StatusAvailable, 2)), // complete, on the pool
		exec(1), // activate: the hold
		fileRows(file(oldFileID, blobfs.StatusAvailable, 2)), // activate: the current logo
		exec(1), // activate: the current image cleared
		exec(1), // activate: the new image set
		exec(1), // retire: the replaced image removed
		fileRows(file(oldFileID, blobfs.StatusDeleting, 3)), // retire: the delete begun
		exec(1), // retire: the purge, on the pool
		// Logo.
		fileRows(file(newFileID, blobfs.StatusAvailable, 2)),
		// DeleteLogo.
		fileRows(file(newFileID, blobfs.StatusAvailable, 2)), // the active logo
		exec(1), // its image removed
		fileRows(file(newFileID, blobfs.StatusDeleting, 3)), // the delete begun
		exec(1), // the purge
	)
	id, err := s.PutLogo(ctx, validID, logoUpload(t))
	if err != nil || id != (organization.Identity{ID: newFileID, Version: 2}) {
		t.Fatalf("PutLogo = %+v, %v", id, err)
	}
	create := rec.Calls()[3]
	if name, key := create.Args[2], create.Args[3]; !strings.HasPrefix(create.SQL, "INSERT INTO blobfs_file") || name != fmt.Sprint(create.Args[0])+".png" || key != fmt.Sprint(create.Args[0])+"/"+fmt.Sprint(name) {
		t.Errorf("create = %q %v; want the file named for its minted id", create.SQL, create.Args)
	}
	if fake.Puts() != 1 {
		t.Errorf("puts = %d, want the upload stored once", fake.Puts())
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
	if rec.Pending() != 0 || rec.RowsLeaked() != 0 {
		t.Errorf("pending = %d, leaked = %d", rec.Pending(), rec.RowsLeaked())
	}
	ops := rec.Ops()
	want := []sqltest.Op{
		sqltest.OpQuery,
		sqltest.OpBegin, sqltest.OpQuery, sqltest.OpQuery, sqltest.OpExec, sqltest.OpCommit,
		sqltest.OpQuery,
		sqltest.OpBegin, sqltest.OpExec, sqltest.OpQuery, sqltest.OpExec, sqltest.OpExec, sqltest.OpCommit,
		sqltest.OpBegin, sqltest.OpExec, sqltest.OpQuery, sqltest.OpCommit, sqltest.OpExec,
		sqltest.OpQuery,
		sqltest.OpBegin, sqltest.OpQuery, sqltest.OpExec, sqltest.OpQuery, sqltest.OpCommit, sqltest.OpExec,
	}
	if fmt.Sprint(ops) != fmt.Sprint(want) {
		t.Errorf("ops = %v\nwant %v", ops, want)
	}
}

// A replacement that loses the race to activate is the unique violation,
// and its own file is retired rather than left behind.
func TestStore_ALostActivationRetiresTheNewFile(t *testing.T) {
	s, rec, _ := serviceOver(t, sqltest.ReturningDialect{},
		directoryRow(), row(), fileRows(file(newFileID, blobfs.StatusPending, 1)), exec(1),
		fileRows(file(newFileID, blobfs.StatusAvailable, 2)),
		exec(1), fileRows(), exec(0), // the hold, no current logo, nothing cleared
		sqltest.Response{Err: fmt.Errorf("ux_organization_image_active: %w", sqlate.ErrUniqueViolation)},
		exec(1), fileRows(file(newFileID, blobfs.StatusDeleting, 3)), exec(1), // the new file retired
	)
	if _, err := s.PutLogo(context.Background(), validID, logoUpload(t)); !errors.Is(err, sqlate.ErrUniqueViolation) {
		t.Fatalf("PutLogo = %v; want the unique violation", err)
	}
	if rec.Pending() != 0 {
		t.Errorf("pending = %d; want the retire run", rec.Pending())
	}
}

// A logo for an organization that does not exist is the missing row,
// before the file's row is written.
func TestStore_PutLogoForAMissingOrganizationIs404(t *testing.T) {
	h := module(t, directoryRow(), sqltest.Response{Columns: []string{"id", "parent_id", "code", "name", "version", "created_at", "updated_at", "path"}})
	problem(t, upload(t, h, "/organizations/"+validID+"/logo", "image/png", "png", 3), 404)
}

// A first logo answers 201 with the new file's identity and the logo's own
// path as its Location; there is no replaced file to retire.
func TestPutLogo_AnswersCreatedWithLocation(t *testing.T) {
	s, _, _ := serviceOver(t, sqltest.ReturningDialect{},
		directoryRow(), row(), fileRows(file(newFileID, blobfs.StatusPending, 1)), exec(1),
		fileRows(file(newFileID, blobfs.StatusAvailable, 2)),
		exec(1), fileRows(), exec(0), exec(1),
	)
	r := web.NewRouter()
	r.Mount(web.NewModule(organization.Routes(s, web.Limits{DefaultSize: 20, MaxSize: 100})))
	path := "/organizations/" + validID + "/logo"
	rec := upload(t, r, path, "image/png", "png", 3)
	if rec.Code != 201 || rec.Header().Get("Location") != path || !strings.Contains(rec.Body.String(), `"id":"`+newFileID+`","version":2`) {
		t.Fatalf("status %d, Location %q, body %s", rec.Code, rec.Header().Get("Location"), rec.Body)
	}
}
