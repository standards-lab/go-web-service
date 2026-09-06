package data

import (
	"context"

	"github.com/standards-lab/sqlate"
	"github.com/standards-lab/sqlate/query"
)

// The advisory-lock name registry. An advisory lock is database-global,
// so every name the service takes is declared here, in one list, as
// <domain>.<structure>, and taken through Lock; the engine hashes the
// name. Two domains that need the same serialization share the one name.
const (
	// LockOrganizationTree serializes structural moves in the organization
	// tree: a transfer's cycle check reads one node's lineage while the
	// update moves another.
	LockOrganizationTree = "organization.tree"
)

// Lock takes the named advisory lock for the rest of the transaction s
// is: a transaction that must not interleave with another on the same
// structure takes it first. Outside a transaction the statement refuses
// to run. The statement is the one native lock.sql, so the port list
// carries the mechanism once.
func (d *Database) Lock(ctx context.Context, s sqlate.Session, name string) error {
	_, err := d.lock.Exec(ctx, s, query.Args{"name": name})
	return err
}
