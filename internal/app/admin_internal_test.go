package app

import (
	"context"
	"log/slog"
	"slices"
	"strings"
	"testing"

	bfdata "github.com/standards-lab/blobfs/data"
	"github.com/standards-lab/sqlate"
	"github.com/standards-lab/sqlate/query"
	"github.com/standards-lab/sqlate/sqltest"

	"github.com/standards-lab/go-web-service/data"
)

// nudges is a sweeper that discards its nudges.
type nudges struct{}

func (nudges) Nudge() {}

// The seeder verifies every statement the service registers, each
// domain's and the data package's, and blobfs's: a store that registers
// statements but is left out of the root's list fails here.
func TestSeeder_VerifiesEveryRegisteredStatement(t *testing.T) {
	catalog := query.MustCatalog(query.Patterns(), bfdata.Patterns(), data.Patterns())
	fs, err := bfdata.New(catalog, sqltest.Dialect{})
	if err != nil {
		t.Fatal(err)
	}
	alone, blobfsRec := sqltest.Open(t)
	if err := fs.Verify(context.Background(), sqlate.Wrap(alone, sqltest.Dialect{})); err != nil {
		t.Fatal(err)
	}
	pool, rec := sqltest.Open(t)
	infra := &Infrastructure{
		Logger:  slog.New(slog.DiscardHandler),
		SQL:     data.New(sqlate.Wrap(pool, sqltest.Dialect{}), catalog),
		Storage: data.NewStorage(fs, nil),
	}
	dom := newDomain(infra, nudges{})
	if err := newSeeder(infra, dom).Verify(context.Background()); err != nil {
		t.Fatal(err)
	}

	prepared := rec.SQL(sqltest.OpPrepare)
	verified := func(text string) bool {
		return slices.ContainsFunc(prepared, func(sql string) bool { return strings.Contains(sql, text) })
	}
	for _, entry := range infra.SQL.Registry() {
		for _, st := range entry.Statements.Statements() {
			if !verified(st.Text()) {
				t.Errorf("%s's statement %s was not verified", entry.Name, st.Name())
			}
		}
	}
	for _, sql := range blobfsRec.SQL(sqltest.OpPrepare) {
		if !slices.Contains(prepared, sql) {
			t.Errorf("blobfs's statement %q was not verified", sql)
		}
	}
}
