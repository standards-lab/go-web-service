package style_test

import (
	"strings"
	"testing"

	"github.com/standards-lab/go-web-service/tools/slab/style"
)

func TestJSON_OffLeavesTextUntouched(t *testing.T) {
	off := style.New(false)
	doc := "{\n  \"a\": 1\n}"
	if got := off.JSON(doc); got != doc {
		t.Errorf("JSON with color off = %q", got)
	}
}

func TestJSON_ColorsKeysAndValuesAndKeepsTheText(t *testing.T) {
	on := style.New(true)
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
		if !strings.Contains(got, on.Key(key)) {
			t.Errorf("key %s is not colored as a key", key)
		}
	}
	for _, val := range []string{`"a <b> c"`, `3`, `"x"`, `1.5`} {
		if !strings.Contains(got, on.Value(val)) {
			t.Errorf("value %s is not colored as a value", val)
		}
	}
	for _, lit := range []string{"true", "null"} {
		if strings.Contains(got, on.Value(lit)) || strings.Contains(got, on.Key(lit)) {
			t.Errorf("literal %s is colored", lit)
		}
	}
}

func TestJSON_LeavesTextThatIsNoJSONUnchanged(t *testing.T) {
	on := style.New(true)
	if got := on.JSON("not json"); got != "not json" {
		t.Errorf("JSON(%q) = %q, want it unchanged", "not json", got)
	}
}
