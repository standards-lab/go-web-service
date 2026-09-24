package storage_test

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/standards-lab/go-web-service/tools/slab/admin/storage"
	"github.com/standards-lab/go-web-service/tools/slab/httpx"
	"github.com/standards-lab/go-web-service/tools/slab/output"
)

// Each subcommand sends its endpoint's request with no body and prints the
// reply.
func TestCommands_SendOneBodilessRequestPerEndpoint(t *testing.T) {
	for args, want := range map[string]string{
		"diagnostics": "GET /admin/storage/diagnostics",
		"container":   "POST /admin/storage/container",
	} {
		t.Run(args, func(t *testing.T) {
			var got []string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, _ := io.ReadAll(r.Body)
				got = append(got, r.Method+" "+r.URL.RequestURI()+string(body))
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, `{"ready":true}`)
			}))
			defer srv.Close()
			var out bytes.Buffer
			cmd := storage.Commands(func() *storage.Client {
				return storage.NewClient(httpx.NewClient(srv.URL))
			}, output.New(&out, io.Discard, nil))
			cmd.SetOut(&out)
			cmd.SetArgs(strings.Fields(args))
			if err := cmd.Execute(); err != nil {
				t.Fatal(err)
			}
			if len(got) != 1 || got[0] != want {
				t.Errorf("requests = %v, want %q", got, want)
			}
			if !strings.Contains(out.String(), `"ready": true`) {
				t.Errorf("stdout = %q", out.String())
			}
		})
	}
}
