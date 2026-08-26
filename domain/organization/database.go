package organization

import (
	"context"
	"database/sql"
	"maps"
	"net/url"
	"slices"

	"github.com/standards-lab/go-database"
	"github.com/standards-lab/go-database/query"
	"github.com/standards-lab/go-web-sdk"
	"github.com/standards-lab/go-web-service/sdk"
)

// columns is the organization table's column set. It is the single source
// of ordering: the CTE's declared columns, both branch select lists, the
// projected fields, and scanOrganization all derive from it, with the
// computed path last.
var columns = []string{
	"id",
	"parent_id",
	"code",
	"name",
	"version",
	"created_at",
	"updated_at",
}

// projection is the layer's read model: the organization columns plus path,
// composed at read time by the recursive-path pattern walking parent_id and
// joining sibling-unique codes. Its field names are the read contract's
// whole sort and filter vocabulary.
var projection = sdk.RecursivePath{
	Table:     "organization",
	Key:       "id",
	Parent:    "parent_id",
	Segment:   "code",
	Field:     "path",
	Separator: "/",
	Rooted:    true,
	Columns:   columns,
}.Projection()

// directives lowers the parsed web directives and the filter parameters
// onto the query vocabulary: every filter is an exact match on a projected
// field, values passing through as text for the engine to type from
// context. Keys iterate sorted so the rendered WHERE is deterministic.
func directives(d web.Directives, filters url.Values) query.Directives {
	dir := query.Directives{
		Page: query.Page{Number: d.Page, Size: d.Size},
	}
	for _, s := range d.Sort {
		dir.Sort = append(dir.Sort, query.Sort{
			Field:      s.Field,
			Descending: s.Descending,
		})
	}
	for _, field := range slices.Sorted(maps.Keys(filters)) {
		dir.Filters = append(dir.Filters, query.Filter{
			Field: field,
			Op:    query.OpEq,
			Value: filters.Get(field),
		})
	}
	return dir
}

// selectOrganizations is the layer's list operation: count and page over
// one shared WHERE.
func selectOrganizations(
	ctx context.Context,
	db *database.DB,
	d web.Directives,
	filters url.Values,
) ([]Organization, int, error) {
	return sdk.SelectList(
		ctx, db,
		projection,
		directives(d, filters),
		scanOrganization,
	)
}

// selectOrganization is the layer's single-row read over one projected
// field; a missing row is sql.ErrNoRows.
func selectOrganization(
	ctx context.Context,
	db *database.DB,
	field, value string,
) (Organization, error) {
	return sdk.SelectOne(
		ctx, db,
		projection,
		field, value,
		scanOrganization,
	)
}

// scanOrganization reads one projected row, in the projection's column
// order: key first, then the fields, path last.
func scanOrganization(rows *sql.Rows) (Organization, error) {
	var o Organization
	err := rows.Scan(
		&o.ID, &o.ParentID, &o.Code, &o.Name,
		&o.Version, &o.CreatedAt, &o.UpdatedAt, &o.Path,
	)
	return o, err
}
