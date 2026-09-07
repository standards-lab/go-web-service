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
