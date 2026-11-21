package document_test

import (
	"context"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/standards-lab/blobfs"
	"github.com/standards-lab/blobfs/data/datatest"
	"github.com/standards-lab/go-storage"
	"github.com/standards-lab/sqlate"
	"github.com/standards-lab/sqlate/sqltest"

	"github.com/standards-lab/go-web-service/domain/document"
	"github.com/standards-lab/go-web-service/internal/outboxtest"
)

const (
	parentOrgID = "00000000-0000-7000-8000-00000000000a"
	seedRootID  = "5eed0002-0000-4000-8000-000000000000"
	seedDirID   = "5eed0002-0000-4000-8000-000000000001"
	seedFileID  = "5eed0002-0000-4000-8000-000000000002"
)

var treeRows = json.RawMessage(`[{"organization":"/acme/engineering","root":"` + seedRootID + `","entries":[
	{"id":"` + seedDirID + `","name":"reports","entries":[
		{"id":"` + seedFileID + `","name":"q1.csv","content_type":"text/csv","content":"a,b\n"}
	]}
]}]`)

func seedFile(status blobfs.Status, version int64) blobfs.File {
	f := file(seedFileID, seedDirID, status, version)
	f.Name, f.Key, f.ContentType = "q1.csv", seedFileID+"/q1.csv", "text/csv"
	if f.Size != nil {
		size := int64(4)
		f.Size = &size
	}
	return f
}

func idRow(id string) sqltest.Response {
	return sqltest.Response{Columns: []string{"id"}, Rows: [][]driver.Value{{id}}}
}

func pathResolved() []sqltest.Response {
	acme, engineering := idRow(parentOrgID), organization()
	return []sqltest.Response{acme, engineering}
}

func abandoned() []sqltest.Response {
	return []sqltest.Response{datatest.FileRows(seedFile(blobfs.StatusDeleting, 2)), exec(1)}
}

func nameUnheldIDTaken() []sqltest.Response {
	return []sqltest.Response{datatest.DirectoryRows(), takenID()}
}

func takenID() sqltest.Response {
	return sqltest.Response{Err: &sqlate.ConstraintError{Constraint: blobfs.ConstraintPrimaryKeyDirectory, Class: sqlate.ErrUniqueViolation, Err: errors.New("duplicate key")}}
}

func TestSeed_WritesTheTreeUnderItsIDs(t *testing.T) {
	ctx := context.Background()
	noRootYet := root()
	rootEnsured := []sqltest.Response{organization(), datatest.DirectoryRows(), datatest.DirectoryRows(directory(seedRootID, blobfs.RootID, orgID, 1)), exec(1), written()}
	directoryCreated := []sqltest.Response{datatest.DirectoryRows(), datatest.DirectoryRows(directory(seedDirID, seedRootID, "reports", 1)), written()}
	fileWritten := []sqltest.Response{datatest.FileRows(), datatest.FileRows(seedFile(blobfs.StatusPending, 1)), datatest.FileRows(seedFile(blobfs.StatusAvailable, 2)), written()}
	s, rec, fake := serviceOver(t, slices.Concat(pathResolved(), []sqltest.Response{noRootYet}, rootEnsured, directoryCreated, fileWritten)...)
	n, err := s.Seed().Write(ctx, treeRows, nil)
	if err != nil || n != 2 {
		t.Fatalf("Write = %d, %v; want the directory and the file", n, err)
	}
	calls := rec.Calls()
	if calls[0].Args[0] != nil || calls[0].Args[1] != "acme" || calls[1].Args[0] != parentOrgID || calls[1].Args[1] != "engineering" {
		t.Errorf("path walk bound %v then %v; want acme under no parent, then engineering under acme", calls[0].Args, calls[1].Args)
	}
	for i, id := range map[int]string{6: seedRootID, 12: seedDirID, 17: seedFileID} {
		if !strings.HasPrefix(calls[i].SQL, "INSERT INTO blobfs_") || calls[i].Args[0] != id {
			t.Errorf("call %d = %q %v; want the insert under %s", i, calls[i].SQL, calls[i].Args, id)
		}
	}
	blob, err := fake.Get(ctx, seedFile(blobfs.StatusPending, 1).Key, storage.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	_ = blob.Body.Close()
	if blob.ContentType != "text/csv" || blob.Size != 4 {
		t.Errorf("stored %d bytes as %q; want the inline content as text/csv", blob.Size, blob.ContentType)
	}
	sameOps(t, rec,
		q, q,
		q, begin, q, q, q, x, x, commit,
		begin, q, q, x, commit,
		begin, q, q, commit, begin, q, x, commit,
	)
	events := outboxtest.Committed(t, rec)
	if len(events) != 3 {
		t.Fatalf("events committed = %d; want the root's, the directory's, and the file's", len(events))
	}
	outboxtest.Want(t, events[0], document.DirectoryCreated.Type(), seedRootID,
		map[string]any{"directory_id": seedRootID, "organization_id": orgID, "parent_id": nil, "name": "/", "version": float64(1)})
	outboxtest.Want(t, events[1], document.DirectoryCreated.Type(), seedDirID,
		map[string]any{"directory_id": seedDirID, "organization_id": orgID, "parent_id": seedRootID, "name": "reports", "version": float64(1)})
	outboxtest.Want(t, events[2], document.FileUploaded.Type(), seedFileID, map[string]any{
		"file_id": seedFileID, "organization_id": orgID, "directory_id": seedDirID, "name": "q1.csv",
		"content_type": "text/csv", "size": float64(4), "etag": `"etag-` + seedFileID + `"`, "version": float64(2),
	})
}

func TestSeed_ARolledBackCreationCommitsNoEvent(t *testing.T) {
	refused := sqltest.Response{Err: errors.New("the outbox refused the row")}
	cases := map[string]struct {
		responses []sqltest.Response
		ops       []sqltest.Op
	}{
		"directory": {
			append(pathResolved(), root(seedRootID),
				datatest.DirectoryRows(), datatest.DirectoryRows(directory(seedDirID, seedRootID, "reports", 1)), refused,
			),
			[]sqltest.Op{q, q, q, begin, q, q, x, rollback},
		},
		"file": {
			slices.Concat(pathResolved(), []sqltest.Response{
				root(seedRootID),
				datatest.DirectoryRows(directory(seedDirID, seedRootID, "reports", 1)),
				datatest.FileRows(), datatest.FileRows(seedFile(blobfs.StatusPending, 1)),
				datatest.FileRows(seedFile(blobfs.StatusAvailable, 2)), refused,
			}, abandoned()),
			[]sqltest.Op{q, q, q, begin, q, commit, begin, q, q, commit, begin, q, x, rollback, begin, q, commit, x},
		},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			s, rec, _ := serviceOver(t, c.responses...)
			if _, err := s.Seed().Write(context.Background(), treeRows, nil); err == nil || !strings.Contains(err.Error(), "the outbox refused the row") {
				t.Fatalf("Write = %v; want the event's write refused", err)
			}
			sameOps(t, rec, c.ops...)
			if events := outboxtest.Committed(t, rec); len(events) != 0 {
				t.Errorf("events committed = %v; want none", events)
			}
		})
	}
}

func TestSeed_ASeededTreeIsLeftAsItIs(t *testing.T) {
	s, rec, fake := serviceOver(t, append(pathResolved(),
		root(seedRootID),
		datatest.DirectoryRows(directory(seedDirID, seedRootID, "reports", 1)),
		datatest.FileRows(seedFile(blobfs.StatusAvailable, 2)),
	)...)
	if n, err := s.Seed().Write(context.Background(), treeRows, nil); err != nil || n != 0 {
		t.Fatalf("Write = %d, %v; want nothing created", n, err)
	}
	if fake.Puts() != 0 {
		t.Errorf("puts = %d; want none", fake.Puts())
	}
	sameOps(t, rec, q, q, q, begin, q, commit, begin, q, commit)
	noEvent(t, rec)
}

func TestSeed_AnEntryItDoesNotOwnIsLeftAlone(t *testing.T) {
	cases := map[string]struct {
		found []sqltest.Response
		ops   []sqltest.Op
	}{
		"moved, its insert refused again on the rerun": {slices.Concat(nameUnheldIDTaken(), nameUnheldIDTaken()), []sqltest.Op{begin, q, q, rollback, begin, q, q, rollback}},
		"deleting": {[]sqltest.Response{datatest.DirectoryRows(deleting(directory(seedDirID, seedRootID, "reports", 1)))}, []sqltest.Op{begin, q, rollback}},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			responses := slices.Concat(pathResolved(), []sqltest.Response{root(seedRootID)}, c.found)
			s, rec, fake := serviceOver(t, responses...)
			if n, err := s.Seed().Write(context.Background(), treeRows, nil); err != nil || n != 0 {
				t.Fatalf("Write = %d, %v; want the directory left alone", n, err)
			}
			if fake.Puts() != 0 {
				t.Errorf("puts = %d; want none", fake.Puts())
			}
			nothingBeneathTheDirectory := append([]sqltest.Op{q, q, q}, c.ops...)
			sameOps(t, rec, nothingBeneathTheDirectory...)
			noEvent(t, rec)
		})
	}
}

func TestSeed_AClientsFileUnderTheNameIsLeftAlone(t *testing.T) {
	for _, status := range []blobfs.Status{blobfs.StatusPending, blobfs.StatusAvailable} {
		t.Run(string(status), func(t *testing.T) {
			client := seedFile(status, 1)
			client.ID, client.Key = fileID, fileID+"/q1.csv"
			nameHeldByTheClient := datatest.FileRows(client)
			s, rec, fake := serviceOver(t, append(pathResolved(),
				root(seedRootID),
				datatest.DirectoryRows(directory(seedDirID, seedRootID, "reports", 1)),
				nameHeldByTheClient,
			)...)
			if n, err := s.Seed().Write(context.Background(), treeRows, nil); err != nil || n != 0 {
				t.Fatalf("Write = %d, %v; want the client's file left alone", n, err)
			}
			if fake.Puts() != 0 {
				t.Errorf("puts = %d; want none over the client's object", fake.Puts())
			}
			sameOps(t, rec, q, q, q, begin, q, commit, begin, q, commit)
			noEvent(t, rec)
		})
	}
}

func TestSeed_ADirectoryAConcurrentSeedCreatedIsFound(t *testing.T) {
	foundOnTheRerun := datatest.DirectoryRows(directory(seedDirID, seedRootID, "reports", 1))
	s, rec, _ := serviceOver(t, slices.Concat(pathResolved(),
		[]sqltest.Response{root(seedRootID)},
		nameUnheldIDTaken(),
		[]sqltest.Response{foundOnTheRerun, datatest.FileRows(seedFile(blobfs.StatusAvailable, 2))},
	)...)
	if n, err := s.Seed().Write(context.Background(), treeRows, nil); err != nil || n != 0 {
		t.Fatalf("Write = %d, %v; want the other seed's tree found", n, err)
	}
	sameOps(t, rec, q, q, q, begin, q, q, rollback, begin, q, commit, begin, q, commit)
	noEvent(t, rec)
}

func TestSeed_RefusesADefectInTheTree(t *testing.T) {
	t.Run("missing organization", func(t *testing.T) {
		s, _, _ := serviceOver(t, idRow(parentOrgID), sqltest.Response{Columns: []string{"id"}})
		if _, err := s.Seed().Write(context.Background(), treeRows, nil); err == nil || !strings.Contains(err.Error(), "/acme/engineering") {
			t.Fatalf("Write = %v; want the path named", err)
		}
	})
	t.Run("both", func(t *testing.T) {
		rows := json.RawMessage(`[{"organization":"/acme","root":"` + seedRootID + `","entries":[{"id":"` + seedDirID + `","name":"x","entries":[],"content_type":"text/plain"}]}]`)
		s, _, _ := serviceOver(t, organization(), root(seedRootID))
		if _, err := s.Seed().Write(context.Background(), rows, nil); err == nil || !strings.Contains(err.Error(), "both") {
			t.Fatalf("Write = %v; want the entry refused", err)
		}
	})
}
