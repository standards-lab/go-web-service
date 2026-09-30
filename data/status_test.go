package data_test

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/standards-lab/blobfs"
	"github.com/standards-lab/go-database"
	"github.com/standards-lab/go-storage"
	"github.com/standards-lab/go-web-sdk"
	"github.com/standards-lab/sqlate"
	"github.com/standards-lab/sqlate/query"

	"github.com/standards-lab/go-web-service/data"
)

func TestStatus(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want int
		ok   bool
	}{
		{"unknown field", &query.UnknownFieldError{Field: "nope", Use: query.FieldUseFilter}, 400, true},
		{"unknown operator", &query.UnknownOperatorError{Op: "between"}, 400, true},
		{"invalid value", &query.InvalidValueError{Field: "created_at", Err: errors.New("bad")}, 400, true},
		{"no rows", sql.ErrNoRows, 404, true},
		{"unique violation", &sqlate.ConstraintError{Constraint: "uq", Class: sqlate.ErrUniqueViolation, Err: errors.New("dup")}, 409, true},
		{"foreign key violation", fmt.Errorf("store: %w", sqlate.ErrForeignKeyViolation), 409, true},
		{"version mismatch", query.ErrVersionMismatch, 412, true},
		{"not ready", database.ErrNotReady, 503, true},
		{"session connection failed", fmt.Errorf("%w: refused", sqlate.ErrConnectionFailed), 503, true},
		{"connection lost mid-read", fmt.Errorf("query: %w", io.ErrUnexpectedEOF), 503, true},
		{"upload body cut short", fmt.Errorf("write file: %w", fmt.Errorf("%w: %w", data.ErrBodyRead, io.ErrUnexpectedEOF)), 400, true},
		{"upload body too slow", fmt.Errorf("write file: %w", fmt.Errorf("%w: %w", data.ErrBodyTimeout, os.ErrDeadlineExceeded)), 408, true},
		{"blobfs invalid name", &blobfs.NameError{Name: "a/b", Reason: "contains a slash"}, 400, true},
		{"blobfs root", blobfs.ErrRootDirectory, 400, true},
		{"blobfs not found", fmt.Errorf("find: %w", blobfs.ErrNotFound), 404, true},
		{"object not found", storage.ErrNotFound, 404, true},
		{"blobfs name taken wins over its unique violation", &blobfs.ViolationError{Sentinel: blobfs.ErrNameTaken, Constraint: "blobfs_uq_file_directory_name", Err: &sqlate.ConstraintError{Class: sqlate.ErrUniqueViolation, Err: errors.New("dup")}}, 409, true},
		{"blobfs not empty", blobfs.ErrNotEmpty, 409, true},
		{"blobfs referenced", blobfs.ErrReferenced, 409, true},
		{"blobfs cycle", blobfs.ErrCycle, 409, true},
		{"blobfs id taken", blobfs.ErrIDTaken, 409, true},
		{"blobfs deleting", blobfs.ErrDeleting, 409, true},
		{"file deleting", &blobfs.DeletingError{ID: "f"}, 409, true},
		{"directory deleting", &blobfs.DeletingError{Directory: true, ID: "d"}, 409, true},
		{"blobfs not deleting", blobfs.ErrNotDeleting, 409, true},
		{"blobfs invalid transition", &blobfs.TransitionError{From: blobfs.StatusAvailable, To: blobfs.StatusPending}, 409, true},
		{"object too large", storage.ErrTooLarge, 413, true},
		{"store not ready", storage.ErrNotReady, 503, true},
		{"store unavailable", fmt.Errorf("%w: refused", storage.ErrUnavailable), 503, true},
		{"container not found", fmt.Errorf("get: %w", storage.ErrContainerNotFound), 503, true},
		{"check violation is unmatched", &sqlate.ConstraintError{Constraint: "cc", Class: sqlate.ErrCheckViolation, Err: errors.New("cc")}, 0, false},
		{"not-null violation is unmatched", sqlate.ErrNotNullViolation, 0, false},
		{"other", errors.New("boom"), 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := data.Status(tc.err)
			if got.Status != tc.want || ok != tc.ok {
				t.Fatalf("Status(%v) = %d, %t; want %d, %t", tc.err, got.Status, ok, tc.want, tc.ok)
			}
			if tc.want != http.StatusConflict && tc.want != http.StatusBadRequest && tc.want != http.StatusRequestTimeout && got.Detail != "" {
				t.Errorf("Status(%v) detail = %q; only a conflict or a request's refusal or timeout carries one", tc.err, got.Detail)
			}
		})
	}
}

// Every conflict carries its curated detail and none of the error's own
// text. The errors are shaped as the libraries wrap them, each naming its
// operation, ids, and constraint, and the writer opts 409 into error text,
// the worst case: the matcher's detail still wins, so nothing of the
// error reaches the body.
func TestStatus_ConflictsCarryACuratedDetail(t *testing.T) {
	const dir = "0198c0de-0000-7000-8000-000000000001"
	violation := func(sentinel error, constraint string, class error) error {
		return &blobfs.ViolationError{Sentinel: sentinel, Constraint: constraint, Err: &sqlate.ConstraintError{Constraint: constraint, Class: class, Err: errors.New(`duplicate key value violates unique constraint "` + constraint + `"`)}}
	}
	cases := []struct {
		name   string
		err    error
		detail string
	}{
		{"name taken", fmt.Errorf("data: create directory under %s as %q: %w", dir, "reports", violation(blobfs.ErrNameTaken, "blobfs_uq_directory_parent_name", sqlate.ErrUniqueViolation)), "an entry with that name already exists"},
		{"id taken", fmt.Errorf("data: create file in %s: %w", dir, violation(blobfs.ErrIDTaken, "blobfs_pk_file", sqlate.ErrUniqueViolation)), "an entry with that name already exists"},
		{"not empty", fmt.Errorf("data: delete directory %s: %w", dir, violation(blobfs.ErrNotEmpty, "blobfs_fk_directory_parent", sqlate.ErrForeignKeyViolation)), "the directory is not empty"},
		{"deleting", fmt.Errorf("data: create directory under %s: the directory %s is deleting: %w", dir, dir, blobfs.ErrDeleting), "the directory is being deleted"},
		{"file deleting", fmt.Errorf("data: move file %s: %w", dir, &blobfs.DeletingError{ID: dir}), "the file is being deleted"},
		{"file deleting with its cause", fmt.Errorf("data: write file %s: %w", dir, &blobfs.DeletingError{ID: dir, Err: &blobfs.TransitionError{From: blobfs.StatusDeleting, To: blobfs.StatusAvailable}}), "the file is being deleted"},
		{"file in a deleting directory", fmt.Errorf("data: move file %s: %w", dir, &blobfs.DeletingError{Directory: true, ID: dir}), "the directory is being deleted"},
		{"referenced", fmt.Errorf("data: delete directory %s: %w", dir, violation(blobfs.ErrReferenced, "organization_directory_fk_directory", sqlate.ErrForeignKeyViolation)), "the file is referenced"},
		{"not deleting", fmt.Errorf("data: purge file %s: %w", dir, blobfs.ErrNotDeleting), "the request conflicts with the current state"},
		{"invalid transition", fmt.Errorf("data: complete file %s: %w", dir, &blobfs.TransitionError{From: blobfs.StatusAvailable, To: blobfs.StatusAvailable}), "the request conflicts with the current state"},
		{"cycle", fmt.Errorf("data: move directory %s: %w", dir, blobfs.ErrCycle), "the request conflicts with the current state"},
		{"unique violation", fmt.Errorf("create: %w", &sqlate.ConstraintError{Constraint: "organization_uq_parent_code", Class: sqlate.ErrUniqueViolation, Err: errors.New(`duplicate key value violates unique constraint "organization_uq_parent_code"`)}), "the request conflicts with the current state"},
		{"foreign key violation", fmt.Errorf("create: %w", &sqlate.ConstraintError{Constraint: "organization_fk_parent", Class: sqlate.ErrForeignKeyViolation, Err: errors.New(`insert violates foreign key constraint "organization_fk_parent"`)}), "the request conflicts with the current state"},
	}
	ew := web.NewErrorWriter(slog.New(slog.DiscardHandler), data.Status)
	ew.Detail(http.StatusConflict)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p, ok := data.Status(tc.err)
			if !ok || p.Status != http.StatusConflict || p.Detail != tc.detail {
				t.Fatalf("Status = %+v, %t; want 409 with %q", p, ok, tc.detail)
			}
			rec := httptest.NewRecorder()
			if err := ew.Write(rec, httptest.NewRequest("GET", "/x", nil), tc.err); err != nil {
				t.Fatal(err)
			}
			var body web.Problem
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || rec.Code != 409 || body.Detail != tc.detail {
				t.Fatalf("wire = %d %s (%v); want 409 with %q", rec.Code, rec.Body, err, tc.detail)
			}
			noRawText(t, rec.Body.String())
		})
	}
}

// noRawText fails the test when body carries any of an error's own text:
// a library's operation prefix, an id, or a constraint's name.
func noRawText(t *testing.T, body string) {
	t.Helper()
	for _, leak := range []string{"data:", "blobfs:", "blobfs_", "_uq_", "_fk_", "_pk_", "constraint", "0198c0de"} {
		if strings.Contains(body, leak) {
			t.Errorf("body %s carries %q", body, leak)
		}
	}
}

// A 400's detail is the refusal alone: the input the request got wrong, in
// the typed error's words without its library prefix, and none of the
// operation chain that wrapped it, which names the service's own ids.
func TestStatus_ARequestsRefusalCarriesOnlyItsInput(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want string
	}{
		{"malformed cursor",
			fmt.Errorf("data: continue files in 01a0ee49-0000-7000-8000-000000000001: %w", &query.CursorError{Reason: query.CursorMalformed}),
			"cursor is malformed"},
		{"foreign cursor",
			fmt.Errorf("data: continue directories in 01a0ee49-0000-7000-8000-000000000001: %w", &query.CursorError{Reason: query.CursorMismatch}),
			"cursor was issued for another base, ordering, or filters"},
		{"unknown field", fmt.Errorf("data: list files in d: %w", &query.UnknownFieldError{Field: "nope", Use: query.FieldUseSort}),
			`unknown sort field "nope"`},
		{"invalid name", fmt.Errorf("data: create file in d: %w", &blobfs.NameError{Name: "a/b", Reason: "contains a slash"}),
			`invalid name "a/b": contains a slash`},
		{"upload body cut short", fmt.Errorf("data: write file 01a0ee49-0000-7000-8000-000000000001: %w", fmt.Errorf("%w: %w", data.ErrBodyRead, io.ErrUnexpectedEOF)),
			"the request body could not be read"},
		{"root", fmt.Errorf("data: delete directory 00000000-0000-0000-0000-000000000000: %w", blobfs.ErrRootDirectory),
			strings.TrimPrefix(blobfs.ErrRootDirectory.Error(), "blobfs: ")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := data.Status(tc.err)
			if !ok || got.Status != http.StatusBadRequest || got.Detail != tc.want {
				t.Errorf("Status = %d %q, %t; want 400 %q", got.Status, got.Detail, ok, tc.want)
			}
		})
	}
}
