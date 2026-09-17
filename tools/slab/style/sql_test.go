package style

import "testing"

func TestSQL_OffLeavesTextUntouched(t *testing.T) {
	off := New(false)
	sql := "SELECT id FROM organization WHERE code = $1"
	if got := off.SQL(sql); got != sql {
		t.Errorf("SQL with color off = %q", got)
	}
}

func TestSQL_BoldsWholeWordKeywordsOnly(t *testing.T) {
	on := New(true)
	got := on.SQL("SELECT settings FROM t ORDER BY id")
	want := ansiBold + "SELECT" + ansiReset + " settings " +
		ansiBold + "FROM" + ansiReset + " t " +
		ansiBold + "ORDER" + ansiReset + " " + ansiBold + "BY" + ansiReset + " id"
	if got != want {
		t.Errorf("SQL = %q\nwant %q", got, want)
	}
	if strip(got) != "SELECT settings FROM t ORDER BY id" {
		t.Errorf("SQL changed the text under the escapes: %q", strip(got))
	}
}
