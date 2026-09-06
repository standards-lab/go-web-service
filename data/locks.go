package data

// The advisory-lock name registry. An advisory lock is database-global,
// so every name the service takes is declared here, in one list, as
// <domain>.<structure>; a domain's locking statement takes the name as its
// parameter and the engine hashes it. Two domains that need the same
// serialization share the one name.
const (
	// LockOrganizationTree serializes structural moves in the organization
	// tree: a transfer's cycle check reads one node's lineage while the
	// update moves another.
	LockOrganizationTree = "organization.tree"
)
