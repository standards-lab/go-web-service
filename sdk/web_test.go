package sdk_test

import (
	"errors"
	"net/http/httptest"
	"testing"

	"github.com/standards-lab/go-web-service/sdk"
)

func ifMatch(t *testing.T, header string) (int64, error) {
	t.Helper()
	r := httptest.NewRequest("PATCH", "/", nil)
	if header != "" {
		r.Header.Set("If-Match", header)
	}
	return sdk.IfMatch(r)
}

func TestIfMatch_ParsesStrongIntegerTags(t *testing.T) {
	cases := map[string]int64{
		`"3"`:   3,
		`"0"`:   0,
		` "7" `: 7,
		`"-1"`:  -1,
	}
	for header, want := range cases {
		got, err := ifMatch(t, header)
		if err != nil {
			t.Errorf("IfMatch(%q) error: %v", header, err)
			continue
		}
		if got != want {
			t.Errorf("IfMatch(%q) = %d, want %d", header, got, want)
		}
	}
}

func TestIfMatch_MissingHeader(t *testing.T) {
	_, err := ifMatch(t, "")

	var pre *sdk.PreconditionError
	if !errors.As(err, &pre) {
		t.Fatalf("error = %v, want *PreconditionError", err)
	}
	if !pre.Missing {
		t.Error("Missing = false, want true for an absent header")
	}
	if pre.Error() == "" {
		t.Error("missing-header error carries no message")
	}
}

func TestIfMatch_RejectsMalformedTags(t *testing.T) {
	cases := []string{`3`, `*`, `W/"3"`, `""`, `"abc"`, `"1", "2"`}
	for _, header := range cases {
		_, err := ifMatch(t, header)

		var pre *sdk.PreconditionError
		if !errors.As(err, &pre) {
			t.Errorf("IfMatch(%q) error = %v, want *PreconditionError", header, err)
			continue
		}
		if pre.Missing {
			t.Errorf("IfMatch(%q): Missing = true, want false for a present header", header)
		}
	}
}
