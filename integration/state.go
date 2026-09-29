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

// AppSet is the service's own migration set, the last the migrator
// declares.
const AppSet = "app"

// SchemaStatus is the schema's state as the admin mount reports it: ready
// once every migration set is current, and each set in declaration order.
type SchemaStatus struct {
	Ready bool        `json:"ready"`
	Sets  []SetStatus `json:"sets"`
}

// SetStatus is one migration set's state, the fields the harness and the
// suite read.
type SetStatus struct {
	Name       string      `json:"name"`
	Version    int         `json:"version"`
	Latest     int         `json:"latest"`
	Dirty      bool        `json:"dirty"`
	Pending    []int       `json:"pending"`
	Migrations []Migration `json:"migrations"`
}

// Migration is one migration of a set, as the status lists it.
type Migration struct {
	Version       int    `json:"version"`
	Name          string `json:"name"`
	Transactional bool   `json:"transactional"`
	Applied       bool   `json:"applied"`
}

// Set returns the named set's state, or the zero SetStatus when the
// migrator declares no such set.
func (s SchemaStatus) Set(name string) SetStatus {
	for _, set := range s.Sets {
		if set.Name == name {
			return set
		}
	}
	return SetStatus{}
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
// operator runs on the admin mount, confirmed as it requires: every
// migration set reverted, the sets applied, the state's rows seeded.
func Reset(t testing.TB, c *webtest.Client, state string) Transition {
	t.Helper()
	return webtest.Decode[Transition](t, c.Post(t, pathState, map[string]any{"state": state, "confirm": true}), http.StatusOK)
}

// States reads the names the service declares.
func States(t testing.TB, c *webtest.Client) []string {
	t.Helper()
	return webtest.Decode[[]string](t, c.Get(t, pathStates), http.StatusOK)
}

// Revert reverts every applied migration, the last declared set first as
// the migrator requires, leaving the schema empty and every set pending,
// the state a startup applies from.
func Revert(t testing.TB, c *webtest.Client) {
	t.Helper()
	sets := Schema(t, c).Sets
	for i := len(sets) - 1; i >= 0; i-- {
		if sets[i].Version == 0 {
			continue
		}
		body := map[string]any{"set": sets[i].Name, "steps": sets[i].Version}
		webtest.Decode[SchemaStatus](t, c.Post(t, pathSchemaDown, body), http.StatusOK)
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
