package integration_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/standards-lab/go-web-sdk/webtest"

	"github.com/standards-lab/go-web-service/integration"
)

// The schema helpers walk the admin mount's verbs: Revert reverts each set
// by name, the last declared first, down to version zero.
func TestState_RevertWalksTheSetsDownLastFirst(t *testing.T) {
	versions := map[string]int{"lib": 2, "app": 1}
	var order []string
	status := func() map[string]any {
		return map[string]any{
			"ready": versions["lib"] == 2 && versions["app"] == 1,
			"sets": []map[string]any{
				{"name": "lib", "version": versions["lib"]},
				{"name": "app", "version": versions["app"]},
			},
		}
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /admin/database/schema", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(status())
	})
	mux.HandleFunc("POST /admin/database/schema/down", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Set   string `json:"set"`
			Steps int    `json:"steps"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		order = append(order, body.Set)
		versions[body.Set] -= body.Steps
		_ = json.NewEncoder(w).Encode(status())
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	c := webtest.NewClient(srv.URL)

	if st := integration.Schema(t, c); st.Set("lib").Version != 2 || st.Set(integration.AppSet).Version != 1 || !st.Ready {
		t.Errorf("schema = %+v", st)
	}
	integration.Revert(t, c)
	if versions["lib"] != 0 || versions["app"] != 0 || len(order) != 2 || order[0] != "app" || order[1] != "lib" {
		t.Errorf("Revert left %v after reverting %v, want both at zero, app first", versions, order)
	}
}

// Reset is one confirmed call to the state operation, carrying the name; Seed
// carries a name only when given one.
func TestState_ResetIsOneCall(t *testing.T) {
	var states, seeds []string
	mux := http.NewServeMux()
	mux.HandleFunc("POST /admin/database/state", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			State   string
			Confirm bool
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if !body.Confirm {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		states = append(states, body.State)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"state":  body.State,
			"schema": map[string]any{"ready": true, "sets": []map[string]any{{"name": "app", "version": 1}}},
			"seeded": map[string]int{"organizations": 7},
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
	if tr.State != "default" || tr.Schema.Set(integration.AppSet).Version != 1 || !tr.Schema.Ready || tr.Seeded["organizations"] != 7 {
		t.Errorf("transition = %+v", tr)
	}
	integration.Seed(t, c, "")
	integration.Seed(t, c, "empty")
	if len(states) != 1 || states[0] != "default" || len(seeds) != 2 || seeds[0] != "" || seeds[1] != "empty" {
		t.Errorf("state calls = %v, seed calls = %v", states, seeds)
	}
}
