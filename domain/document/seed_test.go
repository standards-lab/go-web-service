package document_test

import (
	"context"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/standards-lab/blobfs"
	"github.com/standards-lab/blobfs/data/datatest"
	"github.com/standards-lab/go-storage"
	"github.com/standards-lab/sqlate"
	"github.com/standards-lab/sqlate/sqltest"
)

// A tree a state names: engineering's, by path, under a fixed root id,
// with one directory holding one file.
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

// seedFile is the tree's file row at a status and version.
func seedFile(status blobfs.Status, version int64) blobfs.File {
	f := file(seedFileID, seedDirID, status, version)
	f.Name, f.Key, f.ContentType = "q1.csv", seedFileID+"/q1.csv", "text/csv"
	return f
}

func idRow(id string) sqltest.Response {
	return sqltest.Response{Columns: []string{"id"}, Rows: [][]driver.Value{{id}}}
}

// On an organization without a root, the seed resolves the path a segment
// at a time, ensures the root under the tree's id with its owner row, then
// each entry under its own id: the directory on the pool, the file by the
// write protocol's retry-safe form, its content put and the row completed.
// The count is the entries created, the root not among them.
func TestSeed_WritesTheTreeUnderItsIDs(t *testing.T) {
	ctx := context.Background()
	s, rec, fake := serviceOver(t,
		idRow(parentOrgID), organization(), // the path: acme, then engineering
		root(), // no root yet
		organization(), datatest.DirectoryRows(), datatest.DirectoryRows(directory(seedRootID, blobfs.RootID, orgID, 1)), exec(1),
		datatest.DirectoryRows(), datatest.DirectoryRows(directory(seedDirID, seedRootID, "reports", 1)),
		datatest.FileRows(), datatest.FileRows(seedFile(blobfs.StatusPending, 1)),
		datatest.FileRows(seedFile(blobfs.StatusAvailable, 2)),
	)
	n, err := s.Seed().Write(ctx, treeRows, nil)
	if err != nil || n != 2 {
		t.Fatalf("Write = %d, %v; want the directory and the file", n, err)
	}
	calls := rec.Calls()
	if calls[0].Args[0] != nil || calls[0].Args[1] != "acme" || calls[1].Args[0] != parentOrgID || calls[1].Args[1] != "engineering" {
		t.Errorf("path walk bound %v then %v; want acme under no parent, then engineering under acme", calls[0].Args, calls[1].Args)
	}
	for i, id := range map[int]string{6: seedRootID, 10: seedDirID, 13: seedFileID} {
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
		q, begin, q, q, q, x, commit,
		q, q,
		begin, q, q, commit, q,
	)
}

// A seeded tree seeds nothing again: the root, the directory, and the file
// are each found by name, and nothing is put.
func TestSeed_ASeededTreeIsLeftAsItIs(t *testing.T) {
	s, rec, fake := serviceOver(t,
		idRow(parentOrgID), organization(),
		root(seedRootID),
		datatest.DirectoryRows(directory(seedDirID, seedRootID, "reports", 1)),
		datatest.FileRows(seedFile(blobfs.StatusAvailable, 2)),
	)
	if n, err := s.Seed().Write(context.Background(), treeRows, nil); err != nil || n != 0 {
		t.Fatalf("Write = %d, %v; want nothing created", n, err)
	}
	if fake.Puts() != 0 {
		t.Errorf("puts = %d; want none", fake.Puts())
	}
	sameOps(t, rec, q, q, q, q, begin, q, commit)
}

// An entry whose id a row carries under another name, one a client moved
// or renamed, is left where the client put it, and one whose delete is
// under way is left to the sweep: the directory with its contents,
// nothing written beneath it.
func TestSeed_AnEntryItDoesNotOwnIsLeftAlone(t *testing.T) {
	taken := sqltest.Response{Err: &sqlate.ConstraintError{Constraint: blobfs.ConstraintPrimaryKeyDirectory, Class: sqlate.ErrUniqueViolation, Err: errors.New("duplicate key")}}
	cases := map[string][]sqltest.Response{
		"moved":    {datatest.DirectoryRows(), taken, datatest.DirectoryRows(), taken}, // the retry is taken again
		"deleting": {datatest.DirectoryRows(deleting(directory(seedDirID, seedRootID, "reports", 1)))},
	}
	for name, found := range cases {
		t.Run(name, func(t *testing.T) {
			responses := append([]sqltest.Response{idRow(parentOrgID), organization(), root(seedRootID)}, found...)
			s, rec, fake := serviceOver(t, responses...)
			if n, err := s.Seed().Write(context.Background(), treeRows, nil); err != nil || n != 0 {
				t.Fatalf("Write = %d, %v; want the directory left alone", n, err)
			}
			if fake.Puts() != 0 {
				t.Errorf("puts = %d; want none", fake.Puts())
			}
			if rec.Pending() != 0 || len(rec.Calls()) != len(responses) {
				t.Errorf("ran %d statements with %d pending; want nothing beneath the directory", len(rec.Calls()), rec.Pending())
			}
		})
	}
}

// A file there by the entry's name under another id, a client's upload
// pending or complete, is not the seed's: nothing is put over its object,
// nothing is retired, and nothing is counted.
func TestSeed_AClientsFileUnderTheNameIsLeftAlone(t *testing.T) {
	for _, status := range []blobfs.Status{blobfs.StatusPending, blobfs.StatusAvailable} {
		t.Run(string(status), func(t *testing.T) {
			client := seedFile(status, 1)
			client.ID, client.Key = fileID, fileID+"/q1.csv"
			s, rec, fake := serviceOver(t,
				idRow(parentOrgID), organization(), root(seedRootID),
				datatest.DirectoryRows(directory(seedDirID, seedRootID, "reports", 1)),
				datatest.FileRows(client), // the name, held by the client's row
			)
			if n, err := s.Seed().Write(context.Background(), treeRows, nil); err != nil || n != 0 {
				t.Fatalf("Write = %d, %v; want the client's file left alone", n, err)
			}
			if fake.Puts() != 0 {
				t.Errorf("puts = %d; want none over the client's object", fake.Puts())
			}
			sameOps(t, rec, q, q, q, q, begin, q, commit)
		})
	}
}

// A concurrent seed that commits the directory between the lookup and the
// insert takes the id; the retry finds its directory by name and seeds
// beneath it.
func TestSeed_ADirectoryAConcurrentSeedCreatedIsFound(t *testing.T) {
	taken := sqltest.Response{Err: &sqlate.ConstraintError{Constraint: blobfs.ConstraintPrimaryKeyDirectory, Class: sqlate.ErrUniqueViolation, Err: errors.New("duplicate key")}}
	s, rec, _ := serviceOver(t,
		idRow(parentOrgID), organization(), root(seedRootID),
		datatest.DirectoryRows(), taken, datatest.DirectoryRows(directory(seedDirID, seedRootID, "reports", 1)), // taken, then found
		datatest.FileRows(seedFile(blobfs.StatusAvailable, 2)),
	)
	if n, err := s.Seed().Write(context.Background(), treeRows, nil); err != nil || n != 0 {
		t.Fatalf("Write = %d, %v; want the other seed's tree found", n, err)
	}
	sameOps(t, rec, q, q, q, q, q, q, begin, q, commit)
}

// A path no organization answers, or an entry that is both a directory
// and a file, is the seed's error, named with the tree's organization.
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
