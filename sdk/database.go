package sdk

import (
	"context"
	"database/sql"
	"slices"

	"github.com/standards-lab/go-database"
	"github.com/standards-lab/go-database/query"
)

// Scan reads one row of a statement's result into a T. A domain package
// supplies one per entity, scanning in its projection's column order.
type Scan[T any] func(*sql.Rows) (T, error)

// Columns builds a select list from column names under one table prefix,
// with extra appended for computed columns. An empty prefix renders bare
// names.
func Columns(prefix string, names []string, extra ...query.Column) []query.Column {
	cols := make([]query.Column, 0, len(names)+len(extra))
	for _, name := range names {
		col := name
		if prefix != "" {
			col = prefix + "." + name
		}
		cols = append(cols, query.Column{Expr: query.Col(col)})
	}
	return append(cols, extra...)
}

// Fields builds projected fields whose contract names are their column
// names.
func Fields(names []string) []query.Field {
	fields := make([]query.Field, len(names))
	for i, name := range names {
		fields[i] = query.Field{Name: name, Expr: query.Col(name)}
	}
	return fields
}

// RecursivePath is a computed-field pattern: a self-referencing table whose
// rows compose a path by walking the parent chain — Segment joined by
// Separator, prefixed with it when Rooted ("/acme/engineering"; unrooted,
// "v1.data.reads"). Parent names the self-reference, Key the column it
// joins, and Columns the carried set, Key included, in scan order.
type RecursivePath struct {
	Table     string
	Key       string
	Parent    string
	Segment   string
	Field     string
	Separator string
	Rooted    bool
	Columns   []string
}

// Projection builds the pattern's whole read model: a recursive CTE
// (standard SQL:1999) re-presenting the table with Field as one extra
// column, and the projection reading from it — Key first, the carried
// columns, Field last. The pattern owns the full projection because the CTE
// replaces the FROM and already requires every input the projection needs;
// a layer composing several computed fields would decompose this into
// projection transformers, a seam recorded for the library promotion. An
// empty Separator panics as a wiring mistake.
func (r RecursivePath) Projection() query.Projection {
	if r.Separator == "" {
		panic("sdk: RecursivePath requires a Separator")
	}

	name := r.Table + "_" + r.Field
	sep := "'" + r.Separator + "'"

	root := query.Column{Expr: query.Col("o." + r.Segment)}
	if r.Rooted {
		root = query.Column{Expr: query.Raw(sep + " || o." + r.Segment)}
	}

	cte := query.CTE{
		Name:      name,
		Columns:   append(slices.Clone(r.Columns), r.Field),
		Recursive: true,
		Query: query.UnionAll(
			query.Select{
				Columns: Columns("o", r.Columns, root),
				From:    query.Table(r.Table).As("o"),
				Where:   query.Col("o." + r.Parent).IsNull(),
			},
			query.Select{
				Columns: Columns("o", r.Columns, query.Column{
					Expr: query.Raw("l." + r.Field + " || " + sep + " || o." + r.Segment),
				}),
				From: query.Table(r.Table).As("o").Join(
					query.Table(name).As("l"),
					query.Col("o."+r.Parent).Eq(query.Col("l."+r.Key)),
				),
			},
		),
	}

	fields := make([]string, 0, len(r.Columns))
	for _, c := range r.Columns {
		if c != r.Key {
			fields = append(fields, c)
		}
	}
	fields = append(fields, r.Field)

	return query.Projection{
		With:   []query.CTE{cte},
		From:   query.Table(name),
		Key:    query.Field{Name: r.Key, Expr: query.Col(r.Key)},
		Fields: Fields(fields),
	}
}

// SelectList runs the paginated list operation: the projection's count and
// page statements over one shared WHERE, returning the page's items and the
// total row count. Each executor here renders the SQL its operation would be
// hand-written as — never another operation's scaffolding.
func SelectList[T any](
	ctx context.Context,
	db *database.DB,
	p query.Projection,
	dir query.Directives,
	scan Scan[T],
) ([]T, int, error) {
	stmts, err := p.Statements(db.Dialect(), dir)
	if err != nil {
		return nil, 0, err
	}

	var total int
	if err := db.Conn().
		QueryRowContext(ctx, stmts.Count.SQL, stmts.Count.Args...).
		Scan(&total); err != nil {
		return nil, 0, err
	}

	items, err := SelectPage(ctx, db, stmts.Page, scan)
	if err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

// SelectOne runs the single-row read: a bare select with one equality
// filter on a projected field, assembled on the query composition core —
// no ordering, no paging, no count. A missing row is [sql.ErrNoRows]; an
// unknown field is a *[query.UnknownFieldError]; when the filtered field is
// not unique, the first row wins, as with [sql.DB.QueryRow].
func SelectOne[T any](
	ctx context.Context,
	db *database.DB,
	p query.Projection,
	field, value string,
	scan Scan[T],
) (T, error) {
	var zero T
	expr, err := fieldExpr(p, field)
	if err != nil {
		return zero, err
	}
	stmt := query.Select{
		With:    p.With,
		Columns: columns(p),
		From:    p.From,
		Where:   expr.Eq(value),
	}
	text, args, err := stmt.SQL(db.Dialect())
	if err != nil {
		return zero, err
	}
	items, err := SelectPage(ctx, db, query.Statement{SQL: text, Args: args}, scan)
	if err != nil {
		return zero, err
	}
	if len(items) == 0 {
		return zero, sql.ErrNoRows
	}
	return items[0], nil
}

// SelectPage executes one rendered statement and scans every row it
// returns.
func SelectPage[T any](
	ctx context.Context,
	db *database.DB,
	stmt query.Statement,
	scan Scan[T],
) ([]T, error) {
	rows, err := db.Conn().QueryContext(ctx, stmt.SQL, stmt.Args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []T
	for rows.Next() {
		item, err := scan(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

// columns assembles a projection's select list, key first, every field
// aliased to its contract name. It duplicates resolution the library
// ideally owns — staging debt, deleted when the one-row read becomes a
// Projection operation in go-database.
func columns(p query.Projection) []query.Column {
	cols := make([]query.Column, 0, len(p.Fields)+1)
	cols = append(cols, query.Column{Expr: p.Key.Expr, Alias: p.Key.Name})
	for _, f := range p.Fields {
		cols = append(cols, query.Column{Expr: f.Expr, Alias: f.Name})
	}
	return cols
}

// fieldExpr resolves a projected field name to its expression, key
// included; an unknown name is the projection's typed error.
func fieldExpr(p query.Projection, name string) (query.Expression, error) {
	if p.Key.Name == name {
		return p.Key.Expr, nil
	}
	for _, f := range p.Fields {
		if f.Name == name {
			return f.Expr, nil
		}
	}
	return query.Expression{}, &query.UnknownFieldError{Field: name, Use: query.FieldUseFilter}
}
