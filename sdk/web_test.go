package sdk_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/standards-lab/go-web-sdk"

	"github.com/standards-lab/go-web-service/sdk"
)

type payload struct {
	Name string `json:"name"`
}

func command(t *testing.T, id, ifMatch, body string) (string, int64, payload, error) {
	t.Helper()
	mux := http.NewServeMux()
	var gotID string
	var gotVersion int64
	var gotBody payload
	var gotErr error
	mux.HandleFunc("PUT /things/{id}", func(w http.ResponseWriter, r *http.Request) {
		gotID, gotVersion, gotBody, gotErr = sdk.Command[payload](w, r, 1<<10)
	})
	r := httptest.NewRequest("PUT", "/things/"+id, strings.NewReader(body))
	if ifMatch != "" {
		r.Header.Set("If-Match", ifMatch)
	}
	mux.ServeHTTP(httptest.NewRecorder(), r)
	return gotID, gotVersion, gotBody, gotErr
}

func TestCommand_ReadsTheThreeInputs(t *testing.T) {
	const id = "0192b3a4-5c6d-7e8f-9a0b-1c2d3e4f5a6b"
	gotID, version, body, err := command(t, id, `"3"`, `{"name":"x"}`)
	if err != nil || gotID != id || version != 3 || body.Name != "x" {
		t.Fatalf("Command = %q, %d, %+v, %v", gotID, version, body, err)
	}
}

func TestCommand_RejectsInOrder(t *testing.T) {
	const id = "0192b3a4-5c6d-7e8f-9a0b-1c2d3e4f5a6b"
	var pathErr *web.PathError
	var preErr *web.PreconditionError
	var bodyErr *web.BodyError
	if _, _, _, err := command(t, "nope", "", `{"nope":"x"}`); !errors.As(err, &pathErr) {
		t.Errorf("bad id first: %v", err)
	}
	if _, _, _, err := command(t, id, "", `{"nope":"x"}`); !errors.As(err, &preErr) || !preErr.Missing {
		t.Errorf("missing If-Match second: %v", err)
	}
	if _, _, _, err := command(t, id, `"1"`, `{"nope":"x"}`); !errors.As(err, &bodyErr) {
		t.Errorf("bad body last: %v", err)
	}
}
