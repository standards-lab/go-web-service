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
	"slices"
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
	"github.com/standards-lab/go-web-service/internal/outboxtest"
)

const (
	oldFileID = "00000000-0000-7000-8000-00000000000a"
	newFileID = "00000000-0000-7000-8000-00000000000b"
	dirID     = "00000000-0000-7000-8000-00000000000d"
)

func file(id string, status blobfs.Status, version int64) blobfs.File {
	now := time.Now()
	f := blobfs.File{ID: id, DirectoryID: dirID, Name: id + ".png", Status: status, Key: id + "/" + id + ".png", ContentType: "image/png", Version: version, CreatedAt: now, UpdatedAt: now}
	if status != blobfs.StatusPending {
		size, etag := int64(3), `"etag-`+id+`"`
		f.Size, f.ETag = &size, &etag
	}
	return f
}

func imagesDirectory() blobfs.Directory {
	now, root := time.Now(), blobfs.RootID
	return blobfs.Directory{ID: dirID, ParentID: &root, Name: "organization-images", Status: blobfs.DirectoryStatusActive, Version: 1, CreatedAt: now, UpdatedAt: now}
}

func exec(n int64) sqltest.Response { return sqltest.Response{Affected: n} }

func imagesDirectoryFound() sqltest.Response { return datatest.DirectoryRows(imagesDirectory()) }

func pendingFile(id string) sqltest.Response {
	return datatest.FileRows(file(id, blobfs.StatusPending, 1))
}

func completedFile(id string) sqltest.Response {
	return datatest.FileRows(file(id, blobfs.StatusAvailable, 2))
}

func activeLogo(id string) sqltest.Response { return completedFile(id) }

func deleteBegun(id string) sqltest.Response {
	return datatest.FileRows(file(id, blobfs.StatusDeleting, 3))
}

func noFile() sqltest.Response { return datatest.FileRows() }

func held() sqltest.Response { return exec(1) }

func holdMatchesNothing() sqltest.Response { return exec(0) }

func imageRemoved() sqltest.Response { return exec(1) }

func imageActivated() sqltest.Response { return exec(1) }

func purged() sqltest.Response { return exec(1) }

func pendingWrite(id string) []sqltest.Response {
	return []sqltest.Response{imagesDirectoryFound(), organizationFound(), pendingFile(id)}
}

func retired(id string) []sqltest.Response {
	return []sqltest.Response{deleteBegun(id), purged()}
}

func activeImageTaken() sqltest.Response {
	return sqltest.Response{Err: &sqlate.ConstraintError{Constraint: "ux_organization_image_active", Class: sqlate.ErrUniqueViolation, Err: errors.New("unique")}}
}

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

func TestStore_LogoProtocolsBindTheirFilesParameters(t *testing.T) {
	ctx := context.Background()
	activation := []sqltest.Response{held(), activeLogo(oldFileID), imageRemoved(), deleteBegun(oldFileID), imageActivated(), eventWritten()}
	putLogoReplacing := slices.Concat(pendingWrite(newFileID), []sqltest.Response{completedFile(newFileID)}, activation, []sqltest.Response{purged()})
	readLogo := []sqltest.Response{activeLogo(newFileID)}
	deleteLogo := []sqltest.Response{activeLogo(newFileID), imageRemoved(), deleteBegun(newFileID), eventWritten(), purged()}
	s, rec, fake := serviceOver(t, sqltest.ReturningDialect{}, slices.Concat(putLogoReplacing, readLogo, deleteLogo)...)
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
	if attach := rec.Calls()[13]; !strings.HasPrefix(attach.SQL, "INSERT INTO organization_image") || fmt.Sprint(attach.Args) != fmt.Sprint([]any{validID, newFileID}) {
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
		begin, q, commit,
		begin, x, q, x, q, x, x, commit, x,
		q,
		begin, q, x, q, x, commit, x,
	)
}

func TestStore_AFailedPutAbandonsThePendingRow(t *testing.T) {
	abandoned := []sqltest.Response{datatest.FileRows(file(newFileID, blobfs.StatusDeleting, 2)), purged()}
	s, rec, fake := serviceOver(t, sqltest.ReturningDialect{}, append(pendingWrite(newFileID), abandoned...)...)
	fake.FailPut(errors.New("put failed"))
	if _, err := s.PutLogo(context.Background(), validID, logoUpload(t)); err == nil {
		t.Fatal("PutLogo succeeded over a failed put")
	}
	sameOps(t, rec, q, begin, q, q, commit, begin, q, commit, x)
	noImage(t, rec)
}

func TestStore_ARefusedCompletionDeletesTheLogosObject(t *testing.T) {
	cases := map[string]struct {
		reads []sqltest.Response
		want  error
	}{
		"reclaiming, its own delete in the active images directory": {[]sqltest.Response{datatest.FileRows(file(newFileID, blobfs.StatusDeleting, 2)), imagesDirectoryFound()}, blobfs.ErrDeleting},
		"reclaimed": {[]sqltest.Response{noFile()}, blobfs.ErrNotFound},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			completionMatchesNothing := noFile()
			s, rec, fake := serviceOver(t, sqltest.ReturningDialect{}, slices.Concat(pendingWrite(newFileID), []sqltest.Response{completionMatchesNothing}, c.reads)...)
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
			want := []sqltest.Op{q, begin, q, q, commit, begin, q}
			for range c.reads {
				want = append(want, q)
			}
			sameOps(t, rec, append(want, sqltest.OpRollback)...)
			noImage(t, rec)
		})
	}
}

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

func TestStore_AHangUpAfterTheWriteRetiresTheNewFile(t *testing.T) {
	s, rec, fake := serviceOver(t, sqltest.ReturningDialect{}, slices.Concat(pendingWrite(newFileID), []sqltest.Response{completedFile(newFileID)}, retired(newFileID))...)
	directoryWriteCompletionAndActivationBegin := 9
	ctx := newHangUp(rec, directoryWriteCompletionAndActivationBegin)
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

func TestStore_ALostActivationIs409AndRetiresTheNewFile(t *testing.T) {
	s, rec, fake := serviceOver(t, sqltest.ReturningDialect{}, slices.Concat(
		pendingWrite(newFileID),
		[]sqltest.Response{completedFile(newFileID), held(), noFile(), activeImageTaken()},
		retired(newFileID),
	)...)
	r := web.NewRouter()
	r.Mount(web.NewModule(organization.Routes(s, web.Limits{DefaultSize: 20, MaxSize: 100}, transfer, slog.New(slog.DiscardHandler))))
	problem(t, upload(t, r, "/organizations/"+validID+"/logo", "image/png", "png", 3), 409)
	sameOps(t, rec,
		q,
		begin, q, q, commit,
		begin, q, commit,
		begin, x, q, x, sqltest.OpRollback,
		begin, q, commit, x,
	)
	if _, err := fake.Get(context.Background(), file(newFileID, blobfs.StatusPending, 1).Key, storage.GetOptions{}); !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("the losing logo's object outlived its retire: %v", err)
	}
}

func TestStore_AHoldRefusedByTheFilesOwnDeleteIs409(t *testing.T) {
	s, rec, fake := serviceOver(t, sqltest.ReturningDialect{}, slices.Concat(
		pendingWrite(newFileID),
		[]sqltest.Response{completedFile(newFileID), holdMatchesNothing(), deleteBegun(newFileID), imagesDirectoryFound()},
		retired(newFileID),
	)...)
	r := web.NewRouter()
	r.Mount(web.NewModule(organization.Routes(s, web.Limits{DefaultSize: 20, MaxSize: 100}, transfer, slog.New(slog.DiscardHandler))))
	body := problem(t, upload(t, r, "/organizations/"+validID+"/logo", "image/png", "png", 3), 409)
	if body["detail"] != "the file is being deleted" {
		t.Errorf("detail = %v; want the file's own delete", body["detail"])
	}
	sameOps(t, rec,
		q,
		begin, q, q, commit,
		begin, q, commit,
		begin, x, q, q, sqltest.OpRollback,
		begin, q, commit, x,
	)
	noImage(t, rec)
	if _, err := fake.Get(context.Background(), file(newFileID, blobfs.StatusPending, 1).Key, storage.GetOptions{}); !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("the refused logo's object outlived its retire: %v", err)
	}
}

func TestStore_AHoldRefusedAfterTheReclaimIs409(t *testing.T) {
	abandonFindsTheRowPurged := []sqltest.Response{noFile(), noFile()}
	s, rec, _ := serviceOver(t, sqltest.ReturningDialect{}, slices.Concat(
		pendingWrite(newFileID),
		[]sqltest.Response{completedFile(newFileID), holdMatchesNothing(), deleteBegun(newFileID), imagesDirectoryFound()},
		abandonFindsTheRowPurged,
	)...)
	r := web.NewRouter()
	r.Mount(web.NewModule(organization.Routes(s, web.Limits{DefaultSize: 20, MaxSize: 100}, transfer, slog.New(slog.DiscardHandler))))
	body := problem(t, upload(t, r, "/organizations/"+validID+"/logo", "image/png", "png", 3), 409)
	if body["detail"] != "the file is being deleted" {
		t.Errorf("detail = %v; want the file's own delete", body["detail"])
	}
	sameOps(t, rec,
		q,
		begin, q, q, commit,
		begin, q, commit,
		begin, x, q, q, sqltest.OpRollback,
		begin, q, q, sqltest.OpRollback,
	)
	noImage(t, rec)
}

func noImage(t *testing.T, rec *sqltest.Recorder) {
	t.Helper()
	for _, c := range rec.Calls() {
		if strings.Contains(c.SQL, "organization_image") {
			t.Errorf("image statement ran: %q", c.SQL)
		}
	}
}

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

func TestStore_PutLogoForAMissingOrganizationIs404(t *testing.T) {
	h := module(t, imagesDirectoryFound(), sqltest.Response{Columns: []string{"version"}})
	problem(t, upload(t, h, "/organizations/"+validID+"/logo", "image/png", "png", 3), 404)
}

func TestPutLogo_AnswersCreatedWithLocation(t *testing.T) {
	s, db, _ := serviceOver(t, sqltest.ReturningDialect{}, append(pendingWrite(newFileID),
		completedFile(newFileID), held(), noFile(), imageActivated(), eventWritten(),
	)...)
	r := web.NewRouter()
	r.Mount(web.NewModule(organization.Routes(s, web.Limits{DefaultSize: 20, MaxSize: 100}, transfer, slog.New(slog.DiscardHandler))))
	path := "/organizations/" + validID + "/logo"
	rec := upload(t, r, path, "image/png", "png", 3)
	if rec.Code != 201 || rec.Header().Get("Location") != path || strings.TrimSpace(rec.Body.String()) != `{"id":"`+newFileID+`"}` {
		t.Fatalf("status %d, Location %q, body %s", rec.Code, rec.Header().Get("Location"), rec.Body)
	}
	sameOps(t, db, q, begin, q, q, commit, begin, q, commit, begin, x, q, x, x, commit)
}

const seededLogoID = "5eed0001-0000-4000-8000-000000000001"

func seedFixtures(t *testing.T) (fstest.MapFS, []byte) {
	t.Helper()
	var b bytes.Buffer
	if err := png.Encode(&b, image.NewRGBA(image.Rect(0, 0, 1, 1))); err != nil {
		t.Fatal(err)
	}
	return fstest.MapFS{"acme.png": {Data: b.Bytes()}}, b.Bytes()
}

var logoRows = json.RawMessage(`[{"organization":"/acme","id":"` + seededLogoID + `","fixture":"acme.png"}]`)

func TestLogoSeed_WritesAndActivatesTheFixture(t *testing.T) {
	ctx := context.Background()
	fixtures, body := seedFixtures(t)
	s, rec, fake := serviceOver(t, sqltest.ReturningDialect{}, slices.Concat(
		organizationWithoutALogo(),
		freshWrite(seededLogoID),
		[]sqltest.Response{completedFile(seededLogoID)},
		firstActivation(),
	)...)
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
	if attach := rec.Calls()[13]; !strings.HasPrefix(attach.SQL, "INSERT INTO organization_image") || fmt.Sprint(attach.Args) != fmt.Sprint([]any{validID, seededLogoID}) {
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
	sameOps(t, rec, q, q, q, begin, q, q, commit, begin, q, commit, begin, x, q, x, x, commit)
	wantLogoUploaded(t, rec)
}

func organizationWithoutALogo() []sqltest.Response {
	return []sqltest.Response{row(), noFile(), imagesDirectoryFound()}
}

func freshWrite(id string) []sqltest.Response {
	nameUnheld := noFile()
	return []sqltest.Response{nameUnheld, pendingFile(id)}
}

func firstActivation() []sqltest.Response {
	return []sqltest.Response{held(), noFile(), imageActivated(), eventWritten()}
}

func wantLogoUploaded(t *testing.T, rec *sqltest.Recorder) {
	t.Helper()
	events := outboxtest.Committed(t, rec)
	if len(events) != 1 {
		t.Fatalf("events committed = %d; want the activation's LogoUploaded", len(events))
	}
	outboxtest.Want(t, events[0], organization.LogoUploaded.Type(), seededLogoID, map[string]any{
		"file_id": seededLogoID, "organization_id": validID, "content_type": "image/png",
		"size": float64(3), "etag": `"etag-` + seededLogoID + `"`, "version": float64(2),
	})
}

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
	outboxtest.NoneCommitted(t, rec)
}

func TestLogoSeed_ActivatesAFileAnEarlierRunCompleted(t *testing.T) {
	fixtures, _ := seedFixtures(t)
	writeFindsTheFile := completedFile(seededLogoID)
	s, rec, fake := serviceOver(t, sqltest.ReturningDialect{}, slices.Concat(
		organizationWithoutALogo(), []sqltest.Response{writeFindsTheFile}, firstActivation(),
	)...)
	if n, err := s.LogoSeed().Write(context.Background(), logoRows, fixtures); err != nil || n != 1 {
		t.Fatalf("Write = %d, %v; want the found file activated", n, err)
	}
	if fake.Puts() != 0 {
		t.Errorf("puts = %d; want none", fake.Puts())
	}
	sameOps(t, rec, q, q, q, begin, q, commit, begin, x, q, x, x, commit)
	wantLogoUploaded(t, rec)
}

func TestLogoSeed_ARolledBackActivationCommitsNoEvent(t *testing.T) {
	ctx := context.Background()
	fixtures, _ := seedFixtures(t)
	s, rec, fake := serviceOver(t, sqltest.ReturningDialect{}, slices.Concat(
		organizationWithoutALogo(),
		freshWrite(seededLogoID),
		[]sqltest.Response{completedFile(seededLogoID), held(), noFile(), imageActivated(), {Err: errors.New("the outbox refused the row")}},
		retired(seededLogoID),
	)...)
	if _, err := s.LogoSeed().Write(ctx, logoRows, fixtures); err == nil || !strings.Contains(err.Error(), "the outbox refused the row") {
		t.Fatalf("Write = %v; want the event's write refused", err)
	}
	sameOps(t, rec, q, q, q, begin, q, q, commit, begin, q, commit, begin, x, q, x, x, sqltest.OpRollback, begin, q, commit, x)
	if retire := rec.Calls()[17]; !strings.Contains(retire.SQL, "status = 'deleting'") || retire.Args[1] != int64(2) {
		t.Errorf("retire = %q %v; want the delete begun at the completed version", retire.SQL, retire.Args)
	}
	if _, err := fake.Get(ctx, file(seededLogoID, blobfs.StatusPending, 1).Key, storage.GetOptions{}); !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("the retired file's object outlived it: %v", err)
	}
	outboxtest.NoneCommitted(t, rec)
}

func TestLogoSeed_ALogoActivatedMeanwhileRetiresTheSeededFile(t *testing.T) {
	ctx := context.Background()
	fixtures, _ := seedFixtures(t)
	clientsLogo := activeLogo(newFileID)
	s, rec, fake := serviceOver(t, sqltest.ReturningDialect{}, slices.Concat(
		organizationWithoutALogo(),
		freshWrite(seededLogoID),
		[]sqltest.Response{completedFile(seededLogoID), held(), clientsLogo},
		retired(seededLogoID),
	)...)
	if n, err := s.LogoSeed().Write(ctx, logoRows, fixtures); err != nil || n != 0 {
		t.Fatalf("Write = %d, %v; want nothing seeded", n, err)
	}
	if _, err := fake.Get(ctx, file(seededLogoID, blobfs.StatusPending, 1).Key, storage.GetOptions{}); !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("the retired file's object outlived it: %v", err)
	}
	sameOps(t, rec, q, q, q, begin, q, q, commit, begin, q, commit, begin, x, q, commit, begin, q, commit, x)
	for _, c := range rec.Calls() {
		if strings.HasPrefix(c.SQL, "INSERT INTO organization_image") {
			t.Errorf("the seeded file was bound: %q", c.SQL)
		}
	}
}

func TestLogoSeed_LeavesAFileItDoesNotOwnAlone(t *testing.T) {
	fixtures, _ := seedFixtures(t)
	type seedCase struct {
		responses []sqltest.Response
		ops       []sqltest.Op
	}
	rollback := sqltest.OpRollback
	cases := map[string]seedCase{
		"found, bound to another organization's image": {append(organizationWithoutALogo(),
			completedFile(seededLogoID), held(), noFile(),
			sqltest.Response{Err: &sqlate.ConstraintError{Constraint: "uq_organization_image_file", Class: sqlate.ErrUniqueViolation, Err: errors.New("unique")}},
		), []sqltest.Op{q, q, q, begin, q, commit, begin, x, q, x, rollback}},
		"found deleting": {append(organizationWithoutALogo(),
			deleteBegun(seededLogoID), imagesDirectoryFound(),
		), []sqltest.Op{q, q, q, begin, q, commit, q}},
	}
	for _, status := range []blobfs.Status{blobfs.StatusPending, blobfs.StatusAvailable} {
		other := file(newFileID, status, 2)
		other.Name = seededLogoID + ".png"
		cases["its name held under another id, "+string(status)] = seedCase{
			append(organizationWithoutALogo(), datatest.FileRows(other)),
			[]sqltest.Op{q, q, q, begin, q, commit},
		}
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
			sameOps(t, rec, c.ops...)
		})
	}
}

func TestLogoSeed_ALostActivationToAConcurrentSeedLeavesTheFile(t *testing.T) {
	fixtures, _ := seedFixtures(t)
	otherSeedsPendingRow := pendingFile(seededLogoID)
	s, rec, fake := serviceOver(t, sqltest.ReturningDialect{}, append(organizationWithoutALogo(),
		otherSeedsPendingRow, completedFile(seededLogoID),
		held(), noFile(), activeImageTaken(),
		activeLogo(seededLogoID),
	)...)
	if n, err := s.LogoSeed().Write(context.Background(), logoRows, fixtures); err != nil || n != 0 {
		t.Fatalf("Write = %d, %v; want the other seed's activation left", n, err)
	}
	if fake.Puts() != 1 {
		t.Errorf("puts = %d; want the resumed row's put", fake.Puts())
	}
	sameOps(t, rec, q, q, q, begin, q, commit, begin, q, commit, begin, x, q, x, sqltest.OpRollback, q)
}

func TestLogoSeed_ALostActivationToAnotherLogoRetiresTheSeededFile(t *testing.T) {
	ctx := context.Background()
	fixtures, _ := seedFixtures(t)
	clientsLogo := activeLogo(newFileID)
	s, rec, fake := serviceOver(t, sqltest.ReturningDialect{}, slices.Concat(
		organizationWithoutALogo(),
		freshWrite(seededLogoID),
		[]sqltest.Response{completedFile(seededLogoID), held(), noFile(), activeImageTaken(), clientsLogo},
		retired(seededLogoID),
	)...)
	if n, err := s.LogoSeed().Write(ctx, logoRows, fixtures); err != nil || n != 0 {
		t.Fatalf("Write = %d, %v; want the other logo left and no error", n, err)
	}
	if _, err := fake.Get(ctx, file(seededLogoID, blobfs.StatusPending, 1).Key, storage.GetOptions{}); !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("the retired file's object outlived it: %v", err)
	}
	sameOps(t, rec, q, q, q, begin, q, q, commit, begin, q, commit, begin, x, q, x, sqltest.OpRollback, q, begin, q, commit, x)
}

func TestLogoSeed_AFailedReReadAfterALostActivationRetiresNothing(t *testing.T) {
	ctx := context.Background()
	fixtures, _ := seedFixtures(t)
	lost := errors.New("connection lost")
	activeLogoReReadFails := sqltest.Response{Err: lost}
	s, rec, fake := serviceOver(t, sqltest.ReturningDialect{}, slices.Concat(
		organizationWithoutALogo(),
		freshWrite(seededLogoID),
		[]sqltest.Response{completedFile(seededLogoID), held(), noFile(), activeImageTaken(), activeLogoReReadFails},
	)...)
	if n, err := s.LogoSeed().Write(ctx, logoRows, fixtures); !errors.Is(err, lost) || n != 0 {
		t.Fatalf("Write = %d, %v; want the re-read's failure", n, err)
	}
	if _, err := fake.Get(ctx, file(seededLogoID, blobfs.StatusPending, 1).Key, storage.GetOptions{}); err != nil {
		t.Errorf("the seeded file's object was removed: %v", err)
	}
	sameOps(t, rec, q, q, q, begin, q, q, commit, begin, q, commit, begin, x, q, x, sqltest.OpRollback, q)
}

type requestIDHandler struct{ slog.Handler }

func (h requestIDHandler) Handle(ctx context.Context, r slog.Record) error {
	if id, ok := web.RequestIDFrom(ctx); ok {
		r.AddAttrs(slog.String("request_id", id))
	}
	return h.Handler.Handle(ctx, r)
}

func TestStore_AFailedPurgeAfterTheCommitIsLogged(t *testing.T) {
	purgeLost := sqltest.Response{Err: errors.New("connection lost")}
	ctx := web.WithRequestID(context.Background(), "req-purge")
	cases := map[string]struct {
		responses []sqltest.Response
		run       func(*organization.Service) error
	}{
		"replace": {slices.Concat(pendingWrite(newFileID), []sqltest.Response{
			completedFile(newFileID),
			held(), activeLogo(oldFileID), imageRemoved(), deleteBegun(oldFileID), imageActivated(), eventWritten(),
			purgeLost,
		}), func(s *organization.Service) error {
			_, err := s.PutLogo(ctx, validID, logoUpload(t))
			return err
		}},
		"delete": {[]sqltest.Response{
			activeLogo(oldFileID), imageRemoved(), deleteBegun(oldFileID), eventWritten(),
			purgeLost,
		}, func(s *organization.Service) error { return s.DeleteLogo(ctx, validID) }},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			var logs bytes.Buffer
			s, rec, _ := serviceLogging(t, slog.New(requestIDHandler{slog.NewTextHandler(&logs, nil)}), sqltest.ReturningDialect{}, c.responses...)
			if err := c.run(s); err != nil {
				t.Fatalf("= %v; want success once the change committed", err)
			}
			if rec.Pending() != 0 {
				t.Errorf("pending = %d", rec.Pending())
			}
			if out := logs.String(); !strings.Contains(out, "level=WARN") || !strings.Contains(out, oldFileID) || !strings.Contains(out, "request_id=req-purge") {
				t.Errorf("log = %q; want a warning naming the file, under the request's id", out)
			}
		})
	}
}

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
