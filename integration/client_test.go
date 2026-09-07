package integration_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/standards-lab/go-web-sdk"

	"github.com/standards-lab/go-web-service/integration"
)

// The client reads a problem the way the SDK writes one, and the schema
// helpers walk the admin mount's verbs: Revert steps down until the
// version is zero.
func TestClient_ProblemsAndSchemaState(t *testing.T) {
	version := 2
	mux := http.NewServeMux()
	mux.HandleFunc("GET /admin/database/schema", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"version": version, "ready": version == 2})
	})
	mux.HandleFunc("POST /admin/database/schema/down", func(w http.ResponseWriter, r *http.Request) {
		version--
		_ = json.NewEncoder(w).Encode(map[string]any{"version": version})
	})
	mux.HandleFunc("PUT /things/{id}", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("If-Match") != `"3"` {
			_ = web.Problem{Status: http.StatusPreconditionRequired, Detail: "If-Match required"}.Write(w)
			return
		}
		var body map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)
		_ = json.NewEncoder(w).Encode(map[string]any{"id": r.PathValue("id"), "name": body["name"]})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	c := integration.NewClient(srv.URL)

	p := c.Put(t, "/things/1", map[string]string{"name": "x"}).Problem(t, http.StatusPreconditionRequired)
	if p.Detail != "If-Match required" {
		t.Errorf("problem detail = %q", p.Detail)
	}
	got := integration.Decode[map[string]string](t, c.Put(t, "/things/1", map[string]string{"name": "x"}, integration.IfMatch(3)), http.StatusOK)
	if got["id"] != "1" || got["name"] != "x" {
		t.Errorf("decoded = %v", got)
	}

	if st := integration.Schema(t, c); st.Version != 2 || !st.Ready {
		t.Errorf("schema = %+v", st)
	}
	integration.Revert(t, c)
	if version != 0 {
		t.Errorf("Revert left version %d", version)
	}
}
