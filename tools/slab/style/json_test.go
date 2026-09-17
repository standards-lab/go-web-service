package style

import (
	"strings"
	"testing"
)

func TestJSON_OffLeavesTextUntouched(t *testing.T) {
	off := New(false)
	doc := "{\n  \"a\": 1\n}"
	if got := off.JSON(doc); got != doc {
		t.Errorf("JSON with color off = %q", got)
	}
}

func TestJSON_ColorsKeysAndValuesAndKeepsTheText(t *testing.T) {
	on := New(true)
	doc := "{\n" +
		"  \"name\": \"a <b> c\",\n" +
		"  \"count\": 3,\n" +
		"  \"ok\": true,\n" +
		"  \"none\": null,\n" +
		"  \"items\": [\n" +
		"    \"x\",\n" +
		"    {\n" +
		"      \"deep\": 1.5\n" +
		"    }\n" +
		"  ]\n" +
		"}"
	got := on.JSON(doc)
	if strip(got) != doc {
		t.Fatalf("JSON changed the text under the escapes:\n%s", strip(got))
	}
	for _, key := range []string{`"name"`, `"count"`, `"ok"`, `"none"`, `"items"`, `"deep"`} {
		if !strings.Contains(got, ansiBlue+key+ansiReset) {
			t.Errorf("key %s is not colored as a key", key)
		}
	}
	for _, val := range []string{`"a <b> c"`, `3`, `"x"`, `1.5`} {
		if !strings.Contains(got, ansiGreen+val+ansiReset) {
			t.Errorf("value %s is not colored as a value", val)
		}
	}
	for _, lit := range []string{"true", "null"} {
		if strings.Contains(got, ansiGreen+lit) || strings.Contains(got, ansiBlue+lit) {
			t.Errorf("literal %s is colored", lit)
		}
	}
}
