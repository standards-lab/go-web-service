package integration

import (
	"net/http"
	"testing"

	"github.com/standards-lab/go-web-sdk/webtest"
)

// The admin mount's paths the harness drives state through.
const (
	pathSchema     = "/admin/database/schema"
	pathSchemaDown = "/admin/database/schema/down"
	pathSeed       = "/admin/database/seed"
	pathStates     = "/admin/database/states"
	pathState      = "/admin/database/state"
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

// Transition is the state operation's result: the state reached, the
// schema after it, and the rows its set inserted.
type Transition struct {
	State  string       `json:"state"`
	Schema SchemaStatus `json:"schema"`
	Seeded Seeded       `json:"seeded"`
}

// Reset puts the database in the named state through the one operation an
// operator runs on the admin mount: every migration reverted, the set
// applied, the state's set seeded.
func Reset(t testing.TB, c *webtest.Client, state string) Transition {
	t.Helper()
	return webtest.Decode[Transition](t, c.Post(t, pathState, map[string]string{"state": state}), http.StatusOK)
}

// States reads the names the service declares.
func States(t testing.TB, c *webtest.Client) []string {
	t.Helper()
	return webtest.Decode[[]string](t, c.Get(t, pathStates), http.StatusOK)
}

// Revert reverts every applied migration, leaving the schema empty and the
// set pending, the state a startup applies from.
func Revert(t testing.TB, c *webtest.Client) {
	t.Helper()
	st := Schema(t, c)
	for st.Version > 0 {
		st = webtest.Decode[SchemaStatus](t, c.Post(t, pathSchemaDown, nil), http.StatusOK)
	}
}

// Schema reads the schema status.
func Schema(t testing.TB, c *webtest.Client) SchemaStatus {
	t.Helper()
	return webtest.Decode[SchemaStatus](t, c.Get(t, pathSchema), http.StatusOK)
}

// Seed applies the named set over the database as it stands, or the
// service's configured set when state is empty, and returns what it
// inserted.
func Seed(t testing.TB, c *webtest.Client, state string) Seeded {
	t.Helper()
	var body any
	if state != "" {
		body = map[string]string{"state": state}
	}
	return webtest.Decode[Seeded](t, c.Post(t, pathSeed, body), http.StatusOK)
}
