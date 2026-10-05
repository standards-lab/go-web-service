package style_test

import (
	"testing"

	"github.com/standards-lab/go-web-service/tools/slab/style"
)

func TestSQL_OffLeavesTextUntouched(t *testing.T) {
	off := style.New(false)
	sql := "SELECT id FROM organization WHERE code = $1"
	if got := off.SQL(sql); got != sql {
		t.Errorf("SQL with color off = %q", got)
	}
}

// settings holds SET and order holds ORDER, so a keyword matched inside a
// longer word would show here; lower-case select is no keyword, since the
// match is exact.
func TestSQL_BoldsWholeWordKeywordsOnly(t *testing.T) {
	on := style.New(true)
	got := on.SQL("SELECT settings, order_no FROM t ORDER BY id -- select")
	want := on.Bold("SELECT") + " settings, order_no " +
		on.Bold("FROM") + " t " +
		on.Bold("ORDER") + " " + on.Bold("BY") + " id -- select"
	if got != want {
		t.Errorf("SQL = %q\nwant %q", got, want)
	}
}
