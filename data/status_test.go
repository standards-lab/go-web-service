package data_test

import (
	"database/sql"
	"errors"
	"fmt"
	"io"
	"testing"

	"github.com/standards-lab/blobfs"
	"github.com/standards-lab/go-database"
	"github.com/standards-lab/go-storage"
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
		{"pool connection failed", fmt.Errorf("%w: refused", database.ErrConnectionFailed), 503, true},
		{"session connection failed", fmt.Errorf("%w: refused", sqlate.ErrConnectionFailed), 503, true},
		{"connection lost mid-read", fmt.Errorf("query: %w", io.ErrUnexpectedEOF), 503, true},
		{"blobfs invalid name", &blobfs.NameError{Name: "a/b", Reason: "contains a slash"}, 400, true},
		{"blobfs root", blobfs.ErrRootDirectory, 400, true},
		{"blobfs not found", fmt.Errorf("find: %w", blobfs.ErrNotFound), 404, true},
		{"object not found", storage.ErrNotFound, 404, true},
		{"blobfs name taken wins over its unique violation", &blobfs.ViolationError{Sentinel: blobfs.ErrNameTaken, Constraint: "blobfs_uq_file_directory_name", Err: &sqlate.ConstraintError{Class: sqlate.ErrUniqueViolation, Err: errors.New("dup")}}, 409, true},
		{"blobfs not empty", blobfs.ErrNotEmpty, 409, true},
		{"blobfs referenced", blobfs.ErrReferenced, 409, true},
		{"blobfs cycle", blobfs.ErrCycle, 409, true},
		{"object too large", storage.ErrTooLarge, 413, true},
		{"store not ready", storage.ErrNotReady, 503, true},
		{"store unavailable", fmt.Errorf("%w: refused", storage.ErrUnavailable), 503, true},
		{"container gone", fmt.Errorf("%w: %w", data.ErrContainerGone, storage.ErrNotFound), 503, true},
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
		})
	}
}
