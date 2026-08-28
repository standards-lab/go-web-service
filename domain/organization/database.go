package organization

import (
	"context"
	"database/sql"
	"fmt"
	"maps"
	"slices"

	"github.com/standards-lab/go-database"
	"github.com/standards-lab/go-database/ast"
	"github.com/standards-lab/go-database/exec"
	"github.com/standards-lab/go-database/operation"
	"github.com/standards-lab/go-web-sdk"
)

// treeLock is the service's advisory-lock key registry, one constant per
// contended structure; the organization tree is its first entry. Transfers
// take the lock so two concurrent transfers cannot each pass the cycle
// check and commit a cycle past the other. Native Postgres; a port swaps in
// the engine's application lock (sp_getapplock, GET_LOCK, DBMS_LOCK) or a
// FOR UPDATE mutex row.
const treeLock int64 = 1

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
var projection = operation.RecursivePath{
	Table:     "organization",
	Key:       "id",
	Parent:    "parent_id",
	Segment:   "code",
	Field:     "path",
	Separator: "/",
	Rooted:    true,
	Columns:   columns,
}.Projection()

// keyField and versionField are the command contract's shared fields: the
// key every guarded command addresses, and the version column the guard
// manages exclusively.
var (
	keyField     = operation.Field{Name: "id", Expr: ast.Col("id")}
	versionField = operation.Field{Name: "version", Expr: ast.Col("version")}
)

// directives lowers the parsed query onto the operation vocabulary: every
// filter is an exact match on a projected
// field, values passing through as text for the engine to type from
// context. Keys iterate sorted so the rendered WHERE is deterministic.
func directives(q web.Query) operation.Directives {
	dir := operation.Directives{
		Page: operation.Page{Number: q.Page, Size: q.Size},
	}
	for _, s := range q.Sort {
		dir.Sort = append(dir.Sort, operation.Sort{
			Field:      s.Field,
			Descending: s.Descending,
		})
	}
	for _, field := range slices.Sorted(maps.Keys(q.Filters)) {
		dir.Filters = append(dir.Filters, operation.Filter{
			Field: field,
			Op:    operation.OpEq,
			Value: q.Filters.Get(field),
		})
	}
	return dir
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

// selectOrganizations is the layer's list operation: count and page over
// one shared WHERE.
func selectOrganizations(
	ctx context.Context,
	db *database.DB,
	q web.Query,
) ([]Organization, int, error) {
	return exec.List(ctx, db, projection, directives(q), scanOrganization)
}

// selectOrganization is the layer's single-row read over one projected
// field; a missing row is sql.ErrNoRows.
func selectOrganization(
	ctx context.Context,
	db *database.DB,
	field, value string,
) (Organization, error) {
	return exec.One(ctx, db, projection, field, value, scanOrganization)
}

// insertOrganization is the create operation: one identity-returning
// insert, the id and initial version minted by the engine and scanned back
// through the dialect's RETURNING rendering — a declared native capability,
// contained in the library layers.
func insertOrganization(
	ctx context.Context,
	db *database.DB,
	c CreateOrganization,
) (Identity, error) {
	var ident Identity
	err := database.ExecTx(ctx, db, func(tx *database.Tx) error {
		id, err := exec.Insert(ctx, tx, operation.Insertion{
			Into: "organization",
			Values: []ast.Assignment{
				{Column: "parent_id", Value: c.ParentID},
				{Column: "code", Value: c.Code},
				{Column: "name", Value: c.Name},
			},
			Identity: keyField,
			Version:  versionField,
		})
		if err != nil {
			return err
		}
		ident = Identity{ID: id.ID, Version: id.Version}
		return nil
	})
	return ident, err
}

// editOrganization is the edit operation: a guarded update of the
// descriptive fields. The guard owns the version column and appends its
// increment; updated_at is set with now() so the database stays the single
// clock.
func editOrganization(
	ctx context.Context,
	db *database.DB,
	id string,
	version int64,
	e EditOrganization,
) (Identity, error) {
	var ident Identity
	err := database.ExecTx(ctx, db, func(tx *database.Tx) error {
		v, err := exec.Update(ctx, tx, operation.GuardedUpdate{
			Table: "organization",
			Key:   keyField,
			ID:    id,
			Guard: operation.Guard{Column: "version", Version: version},
			Set: []ast.Assignment{
				{Column: "code", Value: e.Code},
				{Column: "name", Value: e.Name},
				{Column: "updated_at", Value: ast.Raw("now()")},
			},
		})
		if err != nil {
			return err
		}
		ident = Identity{ID: id, Version: v}
		return nil
	})
	return ident, err
}

// transferOrganization is the transfer operation: under the tree's advisory
// lock, the cycle check walks the new parent's ancestor chain, then a
// guarded update moves parent_id. A nonexistent new parent falls through
// the walk to the foreign-key violation.
func transferOrganization(
	ctx context.Context,
	db *database.DB,
	id string,
	version int64,
	t TransferOrganization,
) (Identity, error) {
	var ident Identity
	err := database.ExecTx(ctx, db, func(tx *database.Tx) error {
		if _, err := tx.ExecContext(
			ctx, "SELECT pg_advisory_xact_lock($1)", treeLock,
		); err != nil {
			return tx.Dialect().MapError(err)
		}
		if t.ParentID != nil {
			in, err := inSubtree(ctx, tx, id, *t.ParentID)
			if err != nil {
				return err
			}
			if in {
				return fmt.Errorf("%w: %s is in the subtree of %s", ErrCycle, *t.ParentID, id)
			}
		}
		v, err := exec.Update(ctx, tx, operation.GuardedUpdate{
			Table: "organization",
			Key:   keyField,
			ID:    id,
			Guard: operation.Guard{Column: "version", Version: version},
			Set: []ast.Assignment{
				{Column: "parent_id", Value: t.ParentID},
				{Column: "updated_at", Value: ast.Raw("now()")},
			},
		})
		if err != nil {
			return err
		}
		ident = Identity{ID: id, Version: v}
		return nil
	})
	return ident, err
}

// deleteOrganization is the delete operation: a guarded delete. Children
// block it as a foreign-key violation by the schema's restrict default.
func deleteOrganization(
	ctx context.Context,
	db *database.DB,
	id string,
	version int64,
) error {
	return database.ExecTx(ctx, db, func(tx *database.Tx) error {
		return exec.Delete(ctx, tx, operation.GuardedDelete{
			Table: "organization",
			Key:   keyField,
			ID:    id,
			Guard: operation.Guard{Column: "version", Version: version},
		})
	})
}

// inSubtree reports whether candidate sits in id's subtree — id itself
// included — by walking candidate's ancestor chain upward, a standard
// SQL:1999 recursive CTE. The anchor row makes a self-parent a cycle
// without a separate check.
func inSubtree(
	ctx context.Context,
	tx *database.Tx,
	id, candidate string,
) (bool, error) {
	branch := []ast.Column{
		{Expr: ast.Col("o.id")},
		{Expr: ast.Col("o.parent_id")},
	}
	stmt, err := ast.Select{
		With: []ast.CTE{{
			Name:      "ancestor",
			Columns:   []string{"id", "parent_id"},
			Recursive: true,
			Query: ast.UnionAll(
				ast.Select{
					Columns: branch,
					From:    ast.Table("organization").As("o"),
					Where:   ast.Col("o.id").Eq(candidate),
				},
				ast.Select{
					Columns: branch,
					From: ast.Table("organization").As("o").Join(
						ast.Table("ancestor").As("a"),
						ast.Col("o.id").Eq(ast.Col("a.parent_id")),
					),
				},
			),
		}},
		Columns: []ast.Column{{Expr: ast.Fn("COUNT", ast.Raw("*"))}},
		From:    ast.Table("ancestor"),
		Where:   ast.Col("id").Eq(id),
	}.Render(tx.Dialect())
	if err != nil {
		return false, err
	}
	var n int
	if err := tx.
		QueryRowContext(ctx, stmt.Text, stmt.Args...).
		Scan(&n); err != nil {
		return false, tx.Dialect().MapError(err)
	}
	return n > 0, nil
}
