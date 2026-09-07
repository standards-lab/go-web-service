package integration_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/standards-lab/go-web-sdk/webtest"

	"github.com/standards-lab/go-web-service/integration"
)

// The schema helpers walk the admin mount's verbs: Revert steps down until
// the version is zero.
func TestState_RevertWalksTheSchemaDown(t *testing.T) {
	version := 2
	mux := http.NewServeMux()
	mux.HandleFunc("GET /admin/database/schema", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"version": version, "ready": version == 2})
	})
	mux.HandleFunc("POST /admin/database/schema/down", func(w http.ResponseWriter, r *http.Request) {
		version--
		_ = json.NewEncoder(w).Encode(map[string]any{"version": version})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	c := webtest.NewClient(srv.URL)

	if st := integration.Schema(t, c); st.Version != 2 || !st.Ready {
		t.Errorf("schema = %+v", st)
	}
	integration.Revert(t, c)
	if version != 0 {
		t.Errorf("Revert left version %d", version)
	}
}

// Reset is one call to the state operation, carrying the name; Seed
// carries a name only when given one.
func TestState_ResetIsOneCall(t *testing.T) {
	var states, seeds []string
	mux := http.NewServeMux()
	mux.HandleFunc("POST /admin/database/state", func(w http.ResponseWriter, r *http.Request) {
		var body struct{ State string }
		_ = json.NewDecoder(r.Body).Decode(&body)
		states = append(states, body.State)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"state": body.State, "schema": map[string]any{"version": 1, "ready": true}, "seeded": map[string]int{"organizations": 7},
		})
	})
	mux.HandleFunc("POST /admin/database/seed", func(w http.ResponseWriter, r *http.Request) {
		var body struct{ State string }
		if r.ContentLength != 0 {
			_ = json.NewDecoder(r.Body).Decode(&body)
		}
		seeds = append(seeds, body.State)
		_ = json.NewEncoder(w).Encode(map[string]int{"organizations": 0})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	c := webtest.NewClient(srv.URL)

	tr := integration.Reset(t, c, integration.Default)
	if tr.State != "default" || tr.Schema.Version != 1 || !tr.Schema.Ready || tr.Seeded["organizations"] != 7 {
		t.Errorf("transition = %+v", tr)
	}
	integration.Seed(t, c, "")
	integration.Seed(t, c, "empty")
	if len(states) != 1 || states[0] != "default" || len(seeds) != 2 || seeds[0] != "" || seeds[1] != "empty" {
		t.Errorf("state calls = %v, seed calls = %v", states, seeds)
	}
}
