// Package statement reads a compiled sqlate statement set the way a
// scenario displays it: one statement by name, and its parameters as the
// placeholders a dialect renders them to.
package statement

import (
	"fmt"

	"github.com/standards-lab/sqlate/query"
)

// Find returns the statement named name from stmts, as an error rather than
// the panic Statements.Statement reserves for a constructor's defect.
func Find(stmts *query.Statements, name string) (query.Statement, error) {
	for _, st := range stmts.Statements() {
		if st.Name() == name {
			return st, nil
		}
	}
	return query.Statement{}, fmt.Errorf("no statement %q among the compiled statements", name)
}

// Placeholders maps each parameter name to the placeholder its position
// renders, in position order: placeholder(1) for names[0], and so on.
func Placeholders(placeholder func(int) string, names []string) [][2]string {
	rows := make([][2]string, len(names))
	for i, name := range names {
		rows[i] = [2]string{placeholder(i + 1), name}
	}
	return rows
}
