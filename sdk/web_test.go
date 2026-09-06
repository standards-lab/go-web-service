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

func pathID(t *testing.T, value string) (string, error) {
	t.Helper()
	mux := http.NewServeMux()
	var id string
	var err error
	mux.HandleFunc("GET /things/{id}", func(_ http.ResponseWriter, r *http.Request) {
		id, err = sdk.PathID(r, "id")
	})
	mux.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/things/"+value, nil))
	return id, err
}

func TestPathID_ReturnsCanonicalForm(t *testing.T) {
	const want = "0192b3a4-5c6d-7e8f-9a0b-1c2d3e4f5a6b"
	for _, in := range []string{want, "0192B3A4-5C6D-7E8F-9A0B-1C2D3E4F5A6B"} {
		got, err := pathID(t, in)
		if err != nil || got != want {
			t.Errorf("PathID(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
}

func TestPathID_RejectsNonUUIDs(t *testing.T) {
	for _, in := range []string{"42", "not-a-uuid", "0192b3a4-5c6d-7e8f-9a0b"} {
		_, err := pathID(t, in)
		var pe *sdk.PathError
		if !errors.As(err, &pe) || pe.Name != "id" || pe.Value != in {
			t.Errorf("PathID(%q) error = %v; want a *PathError naming id and the value", in, err)
		}
	}
}

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
	var pathErr *sdk.PathError
	var preErr *web.PreconditionError
	var bodyErr *web.BodyError
	if _, _, _, err := command(t, "nope", "", ""); !errors.As(err, &pathErr) {
		t.Errorf("bad id first: %v", err)
	}
	if _, _, _, err := command(t, id, "", `{"name":"x"}`); !errors.As(err, &preErr) || !preErr.Missing {
		t.Errorf("missing If-Match second: %v", err)
	}
	if _, _, _, err := command(t, id, `"1"`, `{"nope":"x"}`); !errors.As(err, &bodyErr) {
		t.Errorf("bad body last: %v", err)
	}
}
