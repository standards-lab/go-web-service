package app

import "github.com/standards-lab/go-core/lifecycle"

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

	// stageSchema verifies and corrects the schema, checks every statement
	// against it, and seeds. go-database's admin service runs it over the
	// pool started a stage earlier, and its seeder verifies the data
	// package's statements, each domain's, and blobfs's. The root declares
	// the service itself (admin.go).
	stageSchema = stageInfrastructure + 1

	// stageReactors runs the reactors (reactors.go) once every table they
	// touch is verified. The drain stops them after the server, so no
	// request nudges a stopped reactor's work into nothing, and before the
	// stages beneath them.
	stageReactors = stageSchema + 1

	// stageRoot is the request edge: the server (app.go), started after
	// every other stage and drained first.
	stageRoot = lifecycle.StageRoot
)
