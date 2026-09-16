// Package api is the service's HTTP contract as slab calls it: the routes,
// the seed and paging constants the narration states, the wire types the
// scenarios send and decode, and the calls more than one scenario makes.
//
// slab does not depend on the service's own module, so the wire types are
// restated here with the same json tags, the way integration/organization_test.go
// restates them: a black-box observer states the contract it expects, so a
// server-side rename breaks the demo instead of silently following it. A call
// lives here only when two scenarios make the identical call; a step whose
// value is showing its own literal request keeps that request in its scenario.
package api

import (
	"fmt"

	"github.com/standards-lab/go-web-sdk"
)

// The routes the scenarios call. Organizations is domain/organization's
// group, /organizations, inside internal/app's /api mount; State is the
// database admin service's reset, inside the /admin mount.
const (
	Organizations = "/api/organizations"
	State         = "/admin/database/state"
)

// SeedState is the named state a scenario's first step resets to, and
// SeedsDir is where the service's states live, relative to the repository
// root: data/seeds/default.json is the seven-row acme tree.
const (
	SeedState = "default"
	SeedsDir  = "data/seeds"
)

// DefaultPageSize is the page size a list request gets when it names none:
// internal/config/reads.go's defaultReadsDefaultSize, which config.json
// leaves in force.
const DefaultPageSize = 20

// MaxCommandBody is the byte limit domain/organization/handler.go's
// maxCommandBody puts on a command's request body; a body past it is
// answered with 413.
const MaxCommandBody = 1 << 16

// ServiceName is the service.name resource attribute the service exports,
// which Tempo and Loki index the trace under. The same string is stated in
// two other places: internal/app/telemetry.go's serviceName constant, which
// is unexported and so not importable, and
// compose/observability/otel-collector.yaml.
const ServiceName = "go-web-service"

// The command bodies, the shapes of domain/organization's CreateOrganization
// and EditOrganization.
type CreateOrganization struct {
	ParentID *string `json:"parent_id"`
	Code     string  `json:"code"`
	Name     string  `json:"name"`
}

type EditOrganization struct {
	Code string `json:"code"`
	Name string `json:"name"`
}

// Identity is every command's success envelope, the shape of
// domain/organization's Identity.
type Identity struct {
	ID      string `json:"id"`
	Version int64  `json:"version"`
}

// Organization is the read model as the API presents it.
type Organization struct {
	ID       string  `json:"id"`
	ParentID *string `json:"parent_id"`
	Code     string  `json:"code"`
	Name     string  `json:"name"`
	Version  int64   `json:"version"`
	Path     string  `json:"path"`
}

// Page is the list's envelope around Organization: go-web-sdk's own paginated
// success envelope, which the service writes as is.
type Page = web.Page[Organization]

// Tree is the organizations of one list response by code, the form a
// scenario keeps the seeded rows in to target a later command by code.
type Tree map[string]Organization

// Get returns the organization with code, or an error naming the code when
// the tree has none.
func (t Tree) Get(code string) (Organization, error) {
	o, ok := t[code]
	if !ok {
		return Organization{}, fmt.Errorf("the seeded tree has no organization with code %q", code)
	}
	return o, nil
}
