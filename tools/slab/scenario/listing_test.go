package scenario_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/standards-lab/go-web-service/tools/slab/scenario"
)

func TestWriteListing_NamesEachScenarioWithItsSummaryAndNeeds(t *testing.T) {
	var out bytes.Buffer
	scenario.WriteListing(&out, []scenario.Scenario{stubOne, stubTwo})
	want := "" +
		"  stub:one  the first stub\n" +
		"            needs a stub dependency (mise run stub-up)\n" +
		"  stub:two  the second stub\n"
	if out.String() != want {
		t.Errorf("WriteListing wrote:\n%s\nwant:\n%s", out.String(), want)
	}
}

func TestWriteListing_AlignsSummariesPastTheLongestName(t *testing.T) {
	var out bytes.Buffer
	scenario.WriteListing(&out, []scenario.Scenario{
		{Name: "a", Summary: "short name"},
		{Name: "a-long-name", Summary: "long name", Needs: []scenario.Need{{What: "nothing"}}},
	})
	for _, want := range []string{
		"  a            short name\n",
		"  a-long-name  long name\n",
		"               needs nothing\n",
	} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("WriteListing lacks %q:\n%s", want, out.String())
		}
	}
}

func TestWriteListing_SaysSoWhenThereAreNoScenarios(t *testing.T) {
	var out bytes.Buffer
	scenario.WriteListing(&out, nil)
	if got := out.String(); got != "no scenarios\n" {
		t.Errorf("WriteListing over nothing wrote %q", got)
	}
}
