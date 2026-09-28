package app

import (
	"github.com/standards-lab/go-core/lifecycle"
	"github.com/standards-lab/go-database/admin"
)

// The stage table: every lifecycle stage the process uses, named once, in
// the process's dependency order. A stage is the composition root's
// decision, not a library's or a domain's, because only the root knows
// what the process is composed of; each layer file registers its services
// at a stage named here and at no number of its own. The coordinator
// starts the stages ascending, the services within one concurrently, and
// drains them in reverse, so the table read upward is the drain order.
const (
	// stageInfrastructure starts the connections everything else runs
	// over: the database pool and the object store, each with its
	// readiness check.
	stageInfrastructure = 0

	// stageSchema verifies and corrects the schema: go-database's admin
	// service verifies, applies, verifies, and seeds, over the pool. Its
	// Register declares itself at admin.Stage, a library constant this
	// service cannot move, so the table names that value rather than
	// choosing one; TestStages_Ascend fails if a release moves it out of
	// order.
	stageSchema = admin.Stage

	// stageVerify checks every statement against the migrated schema:
	// blobfs's store (infrastructure.go) and each domain service
	// (domain.go) verify their own, concurrently, once the schema is
	// corrected.
	stageVerify = stageSchema + 1

	// stageReactors runs the reactors (reactors.go) once every table they
	// touch is verified. The drain stops them after the server, so no
	// request nudges a stopped reactor's work into nothing, and before the
	// stages beneath them.
	stageReactors = stageVerify + 1

	// stageRoot is the request edge: the server (app.go), started after
	// every numbered stage and drained first.
	stageRoot = lifecycle.StageRoot
)
