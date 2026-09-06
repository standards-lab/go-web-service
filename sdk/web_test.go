package sdk_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

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
