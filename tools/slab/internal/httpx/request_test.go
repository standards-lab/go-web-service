package httpx_test

import (
	"testing"

	"github.com/standards-lab/go-web-service/tools/slab/internal/httpx"
)

func TestRawQuery_EscapesValuesAndLeavesNamesAsWritten(t *testing.T) {
	got := httpx.RawQuery([2]string{"code[like]", "%o%"}, [2]string{"size", "2"}, [2]string{"page", "1"})
	if want := "code[like]=%25o%25&size=2&page=1"; got != want {
		t.Errorf("RawQuery = %q, want %q", got, want)
	}
}

func TestRawQuery_WithNoPairsIsEmpty(t *testing.T) {
	if got := httpx.RawQuery(); got != "" {
		t.Errorf("RawQuery() = %q, want an empty string", got)
	}
}

func TestIfMatch_QuotesTheVersion(t *testing.T) {
	h := httpx.IfMatch(3)
	if h.Name != "If-Match" || h.Value != `"3"` {
		t.Errorf("IfMatch(3) = %+v, want If-Match: \"3\"", h)
	}
}
