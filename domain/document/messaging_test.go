package document_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/standards-lab/blobfs"
	"github.com/standards-lab/blobfs/data/datatest"
	"github.com/standards-lab/go-core/event"
	"github.com/standards-lab/go-storage"
	"github.com/standards-lab/sqlate/sqltest"

	"github.com/standards-lab/go-web-service/domain/document"
)

// outboxWrites is every outbox insert the recorder ran, in order.
func outboxWrites(rec *sqltest.Recorder) []sqltest.Call {
	var out []sqltest.Call
	for _, c := range rec.Calls() {
		if c.Op == sqltest.OpExec && strings.Contains(c.SQL, "messaging_outbox") {
			out = append(out, c)
		}
	}
	return out
}

// noEvent fails the test if the recorder wrote any event.
func noEvent(t *testing.T, rec *sqltest.Recorder) {
	t.Helper()
	if w := outboxWrites(rec); len(w) != 0 {
		t.Errorf("events written = %d; want none", len(w))
	}
}

// bytesOf is a bound argument's bytes, as the driver was handed them.
func bytesOf(t *testing.T, arg any) []byte {
	t.Helper()
	switch v := arg.(type) {
	case []byte:
		return v
	case string:
		return []byte(v)
	case json.RawMessage:
		return v
	}
	t.Fatalf("argument %T is neither bytes nor a string", arg)
	return nil
}

// writtenEvent decodes the outbox insert's event: its parameters are the
// source, the id, the header as JSON, and the data, in that order.
func writtenEvent(t *testing.T, c sqltest.Call) event.Event {
	t.Helper()
	if len(c.Args) != 4 {
		t.Fatalf("outbox insert args = %v; want source, id, header, data", c.Args)
	}
	var h event.Header
	if err := json.Unmarshal(bytesOf(t, c.Args[2]), &h); err != nil {
		t.Fatalf("decode the header: %v", err)
	}
	e, err := event.Decode(h, bytesOf(t, c.Args[3]))
	if err != nil {
		t.Fatalf("decode the event: %v", err)
	}
	return e
}

// An upload that completes writes exactly one FileUploaded, in the
// transaction that makes the file available: the outbox insert runs after
// the completion and before its commit. The event is about the file, its
// data the file's metadata as the completion left it, under the API's
// field names, and never its content.
func TestUploadFile_EmitsFileUploadedInTheCompletion(t *testing.T) {
	s, rec, _ := serviceOver(t,
		root(rootID), within(true), datatest.FileRows(file(fileID, dirID, blobfs.StatusPending, 1)),
		datatest.FileRows(file(fileID, dirID, blobfs.StatusAvailable, 2)), exec(1),
	)
	if _, err := s.UploadFile(context.Background(), orgID, dirID, "report.txt", textUpload(t)); err != nil {
		t.Fatalf("UploadFile = %v", err)
	}
	sameOps(t, rec, begin, q, q, q, commit, begin, q, x, commit)
	writes := outboxWrites(rec)
	if len(writes) != 1 {
		t.Fatalf("events written = %d; want one", len(writes))
	}
	e := writtenEvent(t, writes[0])
	if e.Type != "web-service.document.file.uploaded" || e.Type != document.FileUploaded.Type() || e.Subject != fileID ||
		e.Source != "web-service" || e.ID != eventID || !e.Time.Equal(eventTime) || e.DataContentType != "application/json" {
		t.Errorf("event = %+v; want FileUploaded about the file, stamped by the recorder", e)
	}
	var got map[string]any
	if err := json.Unmarshal(e.Data, &got); err != nil {
		t.Fatal(err)
	}
	want := map[string]any{
		"file_id": fileID, "organization_id": orgID, "directory_id": dirID, "name": "report.txt",
		"content_type": "text/plain", "size": float64(6), "etag": `"etag-` + fileID + `"`, "version": float64(2),
	}
	if len(got) != len(want) {
		t.Errorf("data = %v; want exactly %v", got, want)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("data[%q] = %v; want %v", k, got[k], v)
		}
	}
}

// An event whose write fails fails the completion with it: the
// transaction rolls back, so the file never reads available and no event
// commits, and the write is abandoned as a failed completion is, the row
// removed at its pending version and the object just put deleted.
func TestUploadFile_AFailedEmitRollsBackTheCompletion(t *testing.T) {
	s, rec, fake := serviceOver(t,
		root(rootID), within(true), datatest.FileRows(file(fileID, dirID, blobfs.StatusPending, 1)),
		datatest.FileRows(file(fileID, dirID, blobfs.StatusAvailable, 2)), sqltest.Response{Err: errors.New("the outbox refused the row")},
		// The abandon: the delete begun at the pending version, the object
		// deleted, the row purged.
		datatest.FileRows(file(fileID, dirID, blobfs.StatusDeleting, 2)), exec(1),
	)
	_, err := s.UploadFile(context.Background(), orgID, dirID, "report.txt", textUpload(t))
	if err == nil || !strings.Contains(err.Error(), "the outbox refused the row") {
		t.Fatalf("UploadFile = %v; want the event's write refused", err)
	}
	sameOps(t, rec, begin, q, q, q, commit, begin, q, x, sqltest.OpRollback, begin, q, commit, x)
	abandon := rec.Calls()[len(rec.Calls())-3]
	if !strings.Contains(abandon.SQL, "status = 'deleting'") || abandon.Args[1] != int64(1) {
		t.Errorf("abandon = %q %v; want the delete begun at the pending version", abandon.SQL, abandon.Args)
	}
	if _, err := fake.Get(context.Background(), fileID+"/report.txt", storage.GetOptions{}); !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("the abandoned upload's object is still stored: %v", err)
	}
}
