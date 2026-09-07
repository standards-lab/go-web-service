package integration

import (
	"net/http"
	"testing"
)

// The admin mount's paths the harness drives state through.
const (
	pathSchema     = "/admin/database/schema"
	pathSchemaDown = "/admin/database/schema/down"
	pathSchemaUp   = "/admin/database/schema/up"
	pathSeed       = "/admin/database/seed"
)

// SchemaStatus is the schema's state as the admin mount reports it, the
// fields the harness and the suite read.
type SchemaStatus struct {
	Version int   `json:"version"`
	Dirty   bool  `json:"dirty"`
	Pending []int `json:"pending"`
	Ready   bool  `json:"ready"`
}

// Seeded is the seed operation's result: the rows each table gained.
type Seeded map[string]int

// Reset puts the database in the seeded state the suite starts from,
// through the operations an operator runs on the admin mount: every
// migration reverted, the set applied, the reference data seeded. The
// service c is bound to must run with seeding on.
func Reset(t testing.TB, c *Client) {
	t.Helper()
	Revert(t, c)
	c.Post(t, pathSchemaUp, nil).Expect(t, http.StatusOK)
	c.Post(t, pathSeed, nil).Expect(t, http.StatusOK)
}

// Revert reverts every applied migration, leaving the schema empty and the
// set pending, the state a startup applies from.
func Revert(t testing.TB, c *Client) {
	t.Helper()
	st := Schema(t, c)
	for st.Version > 0 {
		st = Decode[SchemaStatus](t, c.Post(t, pathSchemaDown, nil), http.StatusOK)
	}
}

// Schema reads the schema status.
func Schema(t testing.TB, c *Client) SchemaStatus {
	t.Helper()
	return Decode[SchemaStatus](t, c.Get(t, pathSchema), http.StatusOK)
}

// Seed runs the seed and returns what it inserted.
func Seed(t testing.TB, c *Client) Seeded {
	t.Helper()
	return Decode[Seeded](t, c.Post(t, pathSeed, nil), http.StatusOK)
}
