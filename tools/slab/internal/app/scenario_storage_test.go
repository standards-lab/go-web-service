package app_test

import (
	"bytes"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/standards-lab/go-core/process"

	"github.com/standards-lab/go-web-service/tools/slab/domain/document"
	"github.com/standards-lab/go-web-service/tools/slab/domain/organization"
)

// runStorageScenario runs the storage scenario against fake and returns
// the run; the fake's own error, a request no route answers, fails the
// test. Grafana's URL is the fake's too, and the scenario never probes it.
func runStorageScenario(t *testing.T, fake *storageFake) scenarioRun {
	t.Helper()
	return runOrgScenario(t, "storage", fake, func() error { return fake.err })
}

// runStorageOK is runStorageScenario expecting the run to succeed, and
// returns its narration.
func runStorageOK(t *testing.T, fake *storageFake) string {
	t.Helper()
	r := runStorageScenario(t, fake)
	if r.code != process.ExitOK {
		t.Fatalf("code = %d, want %d; stderr = %q\n%s", r.code, process.ExitOK, r.stderr, r.stdout)
	}
	return r.stdout
}

// runStorageFailing is runStorageScenario expecting the run to fail at a
// step, and returns the narration and the one report on stderr.
func runStorageFailing(t *testing.T, fake *storageFake) (out, report string) {
	t.Helper()
	r := runStorageScenario(t, fake)
	if r.code != process.ExitFailure {
		t.Fatalf("code = %d, want %d; stderr = %q\n%s", r.code, process.ExitFailure, r.stderr, r.stdout)
	}
	if n := strings.Count(r.stderr, "slab scenario"); n != 1 {
		t.Errorf("stderr reports %d times, want once:\n%s", n, r.stderr)
	}
	return r.stdout, r.stderr
}

// storageSteps is the scenario's headings in order.
var storageSteps = []string{
	"[1/16] Migration Sets", "[2/16] Additive Seed", "[3/16] Logo Read",
	"[4/16] Logo Revalidation", "[5/16] Logo Replacement", "[6/16] Logo Delete",
	"[7/16] Document Tree", "[8/16] Cursor Walk", "[9/16] Missing If-Match",
	"[10/16] Stale If-Match", "[11/16] Taken Name", "[12/16] Another Organization",
	"[13/16] Branch Creation", "[14/16] Recursive Delete", "[15/16] Sweep", "[16/16] Reset",
}

// The storage scenario builds the service node, with the base setting it
// reads, then the settings its steps read; it points at no trace, so it
// neither builds nor probes grafana up.
func TestScenario_StorageRunsSixteenStepsInOrder(t *testing.T) {
	fake := newStorageFake(t)
	r := runStorageScenario(t, fake)
	if r.code != process.ExitOK {
		t.Fatalf("code = %d, want %d; stderr = %q\n%s", r.code, process.ExitOK, r.stderr, r.stdout)
	}
	inOrder(t, r.stdout, storageSteps)
	if want := []string{"service", "base", "client", "repo", "no color", "lifecycle config"}; !slices.Equal(r.built, want) {
		t.Errorf("nodes built = %q, want %q", r.built, want)
	}
	if slices.Contains(fake.requests, "GET /api/health") {
		t.Errorf("the run probed Grafana; sent:\n%s", strings.Join(fake.requests, "\n"))
	}
}

func TestScenario_StorageSendsTheScenarioSequence(t *testing.T) {
	fake := newStorageFake(t)
	runStorageOK(t, fake)
	paths := fake.acmeTree.paths()
	logo := organization.Organizations + "/" + fakeAcmeID + "/logo"
	docs := document.Documents + "/" + fakeAcmeID
	readme := docs + "/files/" + paths[guardedFilePath]
	// Each entry is one request the fake recorded, in the order the steps
	// send them, matched whole or, where prefix is set, by its start;
	// requests between them are not checked.
	want := []struct {
		line   string
		prefix bool
	}{
		{"GET " + schemaRoute, false},
		{"POST " + seedRoute, false},
		{"GET " + logo, false},
		{"GET " + logo + ` If-None-Match: "0x1"`, false},
		{"PUT " + logo, false},
		{"GET " + logo, false},
		{"DELETE " + logo, false},
		{"GET " + logo, false},
		{"GET " + docs + "/directories/root/directories?sort=name", false},
		{"GET " + docs + "/directories/root/files?sort=name", false},
		{"GET " + docs + "/directories/root/directories?size=1&sort=name", false},
		{"GET " + docs + "/directories/root/directories?size=1&sort=name&cursor=after%3Aengineering", false},
		{"GET " + docs + "/directories/root/directories?size=1&sort=name&cursor=after%3Afinance", false},
		{"GET " + readme, false},
		{"DELETE " + readme, false},
		{"DELETE " + readme + ` If-Match: "1"`, false},
		{"POST " + docs + "/directories", false},
		{"GET " + docs + "/directories/" + paths[takenDirPath+"/"], false},
		{"GET " + document.Documents + "/" + fakeFinanceID + "/directories/" + paths[takenDirPath+"/"], false},
		{"POST " + docs + "/directories", false},
		{"POST " + docs + "/directories", false},
		{"POST " + docs + "/directories/", true},
		{"DELETE " + docs + "/directories/", true},
		{"POST " + stateRoute, false},
	}
	i := 0
	for _, got := range fake.requests {
		if i == len(want) {
			break
		}
		if w := want[i]; got == w.line || (w.prefix && strings.HasPrefix(got, w.line)) {
			i++
		}
	}
	if i < len(want) {
		t.Errorf("the requests lack %q in order; sent:\n%s", want[i].line, strings.Join(fake.requests, "\n"))
	}
	if !strings.Contains(strings.Join(fake.requests, "\n"), `?recursive=true If-Match: "1"`) {
		t.Errorf("the recursive delete did not guard on the branch's version; sent:\n%s", strings.Join(fake.requests, "\n"))
	}
}

func TestScenario_StorageLeavesTheFakeAsTheResetSeededIt(t *testing.T) {
	fake := newStorageFake(t)
	runStorageOK(t, fake)
	if !bytes.Equal(fake.logo, fakeSeedLogo) {
		t.Error("the run left acme without its seeded logo")
	}
	if got, want := len(fake.nodes), len(fake.acmeTree.paths())+1; got != want {
		t.Errorf("the run left %d nodes, want the seeded %d", got, want)
	}
}

func TestScenario_StorageRunsTwiceAgainstTheSameFake(t *testing.T) {
	fake := newStorageFake(t)
	for run := 1; run <= 2; run++ {
		t.Logf("run %d", run)
		runStorageOK(t, fake)
	}
}

func TestScenario_StorageRestoresADeletedLogoThroughTheAdditiveSeed(t *testing.T) {
	fake := newStorageFake(t)
	fake.logo = nil // an earlier run stopped after its logo delete
	out := runStorageOK(t, fake)
	if !strings.Contains(step(t, out, "[2/16]"), `"logos": 1`) {
		t.Errorf("the additive seed does not report the logo it wrote again:\n%s", out)
	}
}

func TestScenario_StorageFailsNamingTheStepOnAnUnexpectedResponse(t *testing.T) {
	fake := newStorageFake(t)
	fake.fault = http.MethodDelete + " " + organization.Organizations + "/" + fakeAcmeID + "/logo"
	out, report := runStorageFailing(t, fake)
	if want := "slab scenario storage: step 6 (Logo Delete): status = 500"; !strings.HasPrefix(report, want) {
		t.Errorf("stderr = %q, want it to start with %q", report, want)
	}
	if strings.Contains(out, "[7/16]") {
		t.Errorf("the run went on past the failed step:\n%s", out)
	}
}

func TestScenario_StorageFailsWhenTheConflictDetailIsNotTheCuratedOne(t *testing.T) {
	fake := newStorageFake(t)
	fake.takenDetail = "the request conflicts with the current state"
	_, report := runStorageFailing(t, fake)
	if want := "slab scenario storage: step 11 (Taken Name): the conflict's detail is"; !strings.HasPrefix(report, want) {
		t.Errorf("stderr = %q, want it to start with %q", report, want)
	}
}

func TestScenario_StorageFailsWhenASeededEntryIsMissingFromTheTree(t *testing.T) {
	fake := newStorageFake(t)
	fake.nodes[fake.seed.Documents[0].Entries[0].ID].name = "renamed.txt"
	_, report := runStorageFailing(t, fake)
	if want := "slab scenario storage: step 7 (Document Tree): the tree has /README.txt"; !strings.HasPrefix(report, want) {
		t.Errorf("stderr = %q, want it to start with %q", report, want)
	}
}

func TestScenario_StorageNarratesEachStepAsNoteRequestResponse(t *testing.T) {
	out := runStorageOK(t, newStorageFake(t))
	logo := organization.Organizations + "/" + fakeAcmeID + "/logo"
	for _, c := range []struct{ heading, note, request, status string }{
		{"[1/16]", "two migration sets", "GET " + schemaRoute, "HTTP 200 OK"},
		{"[2/16]", "A seed is additive", "POST " + seedRoute, "HTTP 200 OK"},
		{"[3/16]", "served by proxy", "GET " + logo, "HTTP 200 OK"},
		{"[4/16]", "If-None-Match", "GET " + logo, "HTTP 304 Not Modified"},
		{"[5/16]", "takes no version", "PUT " + logo, "HTTP 201 Created"},
		{"[6/16]", "retires whatever logo", "DELETE " + logo, "HTTP 204 No Content"},
		{"[7/16]", "hang from its own root", "GET " + document.Documents, "HTTP 200 OK"},
		{"[8/16]", "next cursor", "GET " + document.Documents, "HTTP 200 OK"},
		{"[9/16]", "428 Precondition Required", "DELETE " + document.Documents, "HTTP 428 Precondition Required"},
		{"[10/16]", "412 Precondition Failed", "DELETE " + document.Documents, "HTTP 412 Precondition Failed"},
		{"[11/16]", "Names are unique", "POST " + document.Documents, "HTTP 409 Conflict"},
		{"[12/16]", "checks its scope first", "GET " + document.Documents + "/" + fakeFinanceID, "HTTP 404 Not Found"},
		{"[13/16]", "A small branch", "POST " + document.Documents, "HTTP 201 Created"},
		{"[14/16]", "two stages", "DELETE " + document.Documents, "HTTP 202 Accepted"},
		{"[15/16]", "until it answers 404", "GET " + document.Documents, "HTTP 404 Not Found"},
		{"[16/16]", "actually clears", "POST " + stateRoute, "HTTP 200 OK"},
	} {
		flat := strings.Join(strings.Fields(step(t, out, c.heading)), " ")
		note, request, status := strings.Index(flat, c.note), strings.Index(flat, c.request), strings.Index(flat, c.status)
		if note < 0 || request < 0 || status < 0 {
			t.Errorf("step %s lacks its note %q, request %q, or status %q:\n%s", c.heading, c.note, c.request, c.status, flat)
			continue
		}
		if note > request || request > status {
			t.Errorf("step %s is not note, request, response in order:\n%s", c.heading, flat)
		}
	}
}

func TestScenario_StorageSummarizesTheLogoBytesAndShowsItsValidators(t *testing.T) {
	out := runStorageOK(t, newStorageFake(t))
	read := step(t, out, "[3/16]")
	for _, want := range []string{
		fmt.Sprintf("    (%d bytes of image/png)\n", len(fakeSeedLogo)),
		"    ETag: \"0x1\"\n",
		"    Last-Modified: ",
	} {
		if !strings.Contains(read, want) {
			t.Errorf("the logo read lacks %q:\n%s", want, read)
		}
	}
	if strings.Contains(read, "PNG") {
		t.Errorf("the logo read prints the bytes:\n%s", read)
	}
	if replace := step(t, out, "[5/16]"); !strings.Contains(replace, "bytes of "+"data/seeds/fixtures/"+replacementFixture+")") {
		t.Errorf("the replacement's request does not name its fixture:\n%s", replace)
	}
}

func TestScenario_StoragePrintsTheTreeTheDeletingStatusAndTheSeededCounts(t *testing.T) {
	fake := newStorageFake(t)
	out := runStorageOK(t, fake)
	paths := fake.acmeTree.paths()
	treeStep := step(t, out, "[7/16]")
	if !strings.Contains(treeStep, "\n\n  acme's document tree\n") {
		t.Errorf("the tree step lacks its table:\n%s", treeStep)
	}
	for p, id := range paths {
		if !strings.Contains(treeStep, p) || !strings.Contains(treeStep, id) {
			t.Errorf("the tree table lacks %s at %s", p, id)
		}
	}
	// The tree's last row is the seed's deepest file under its last
	// directory, as the walk lists a directory's files, then its
	// subdirectories, each by name.
	setOff(t, out, []block{
		{"Migration sets", "    app "},
		{"acme's document tree", "    /operations/logistics/carriers.csv "},
		{"Seeded", "    documents "},
	})
	if !strings.Contains(step(t, out, "[14/16]"), `"status": "deleting"`) {
		t.Errorf("the recursive delete does not show the deleting status:\n%s", out)
	}
	if !strings.Contains(step(t, out, "[15/16]"), "  The sweep removed the branch within ") {
		t.Errorf("the sweep step does not report the wait:\n%s", out)
	}
	counts := fake.seed.counts()
	reset := step(t, out, "[16/16]")
	for _, row := range []string{
		fmt.Sprintf("organizations  %d", counts.Organizations),
		fmt.Sprintf("logos          %d", counts.Logos),
		fmt.Sprintf("documents      %d", counts.Documents),
	} {
		if !strings.Contains(reset, row) {
			t.Errorf("the reset's table lacks %q:\n%s", row, reset)
		}
	}
}

func TestScenario_StorageFailsWhenTheResetSeedsOtherThanTheSeedFile(t *testing.T) {
	fake := newStorageFake(t)
	fake.miscount = 1
	_, report := runStorageFailing(t, fake)
	if want := "slab scenario storage: step 16 (Reset): the reset seeded"; !strings.HasPrefix(report, want) {
		t.Errorf("stderr = %q, want it to start with %q", report, want)
	}
}

// A directory beyond the seeded ones, such as a branch an interrupted run
// left behind, falls wherever its name sorts, and the walk still finds the
// seeded ones in order among the rest.
func TestScenario_StorageCursorWalkToleratesAnUnseededDirectory(t *testing.T) {
	fake := newStorageFake(t)
	root := fake.acmeTree.Root
	fake.nodes["left-behind"] = &fakeNode{id: "left-behind", parent: root, name: "f-left-behind", dir: true, status: document.DirectoryActive, version: 1}
	out := runStorageOK(t, fake)
	if !strings.Contains(step(t, out, "[8/16]"), `"name": "f-left-behind"`) {
		t.Errorf("the walk did not page through the extra directory:\n%s", out)
	}
}

func TestScenario_StorageFailsWhenTheCursorWalkIsOutOfOrder(t *testing.T) {
	fake := newStorageFake(t)
	fake.descendingWalk = true
	_, report := runStorageFailing(t, fake)
	if want := "slab scenario storage: step 8 (Cursor Walk): the walk visited"; !strings.HasPrefix(report, want) {
		t.Errorf("stderr = %q, want it to start with %q", report, want)
	}
}

// A body that is no JSON, a download's bytes, is summarized; a problem
// document is JSON and prints as it came.
func TestScenario_StoragePrintsAFailedLogoReadsProblemAsIs(t *testing.T) {
	fake := newStorageFake(t)
	fake.fault = http.MethodGet + " " + organization.Organizations + "/" + fakeAcmeID + "/logo"
	out, report := runStorageFailing(t, fake)
	if want := "slab scenario storage: step 3 (Logo Read): status = 500"; !strings.HasPrefix(report, want) {
		t.Errorf("stderr = %q, want it to start with %q", report, want)
	}
	read := step(t, out, "[3/16]")
	if !strings.Contains(read, `"status": 500`) || strings.Contains(read, "bytes of") {
		t.Errorf("the logo read does not print the problem as it came:\n%s", read)
	}
}
