//go:build integration

package integration_test

import (
	"net/http"
	"slices"
	"testing"

	"github.com/standards-lab/go-core/process/processtest"
	"github.com/standards-lab/go-web-sdk/webtest"

	"github.com/standards-lab/go-web-service/integration"
)

func TestDatabaseOutage_Answers503AndRecoversWithoutARestart(t *testing.T) {
	f := processtest.Forward(t, integration.DatabaseAddr())
	s := integration.Start(t, integration.Options{Seed: integration.Default, Database: f.Addr()})
	c := s.Client()
	integration.Reset(t, c, integration.Default)
	all := tree(t, c)
	fin := all["finance"]
	edit := map[string]any{"code": "fin", "name": "Finance"}

	f.Sever()

	ready := c.Get(t, "/readyz").Problem(t, http.StatusServiceUnavailable)
	if ready.Detail == "" {
		t.Error("readyz 503 carries no detail")
	}
	_ = c.Get(t, organizations).Problem(t, http.StatusServiceUnavailable)
	_ = c.Get(t, organizations+"/"+fin.ID).Problem(t, http.StatusServiceUnavailable)
	_ = c.Put(t, organizations+"/"+fin.ID, edit, webtest.IfMatch(fin.Version)).Problem(t, http.StatusServiceUnavailable)
	c.Get(t, "/healthz").Expect(t, http.StatusOK)

	f.Restore(t)

	processtest.WaitFor(t, "readiness after the database returns", func() bool {
		return c.Get(t, "/readyz").Status == http.StatusOK
	})
	if p := webtest.Decode[organizationPage](t, c.Get(t, organizations), http.StatusOK); p.total() != seededTotal {
		t.Errorf("total after recovery = %d", p.total())
	}
	ident := webtest.Decode[identity](t, c.Put(t, organizations+"/"+fin.ID, edit, webtest.IfMatch(fin.Version)), http.StatusOK)
	if ident.Version != fin.Version+1 {
		t.Errorf("edit after recovery = %+v", ident)
	}
	if code := s.Stop(t); code != 0 {
		t.Fatalf("exit = %d:\n%s", code, s.Output())
	}
}

func TestStorageOutage_RefusesObjectsServesMetadataAndRecovers(t *testing.T) {
	f := processtest.Forward(t, integration.StorageAddr())
	s := integration.Start(t, integration.Options{Seed: integration.Default, Storage: f.Addr()})
	c := s.Client()
	integration.Reset(t, c, integration.Default)
	docs := "/api/documents/" + tree(t, c)[docsOrg].ID
	body := webtest.Raw{ContentType: "text/plain", Body: []byte("report")}
	q3 := webtest.Decode[identity](t, c.Post(t, docs+"/directories/root/files?name=q3.txt", body), http.StatusCreated)
	content := docs + "/files/" + q3.ID + "/content"
	reads := []string{
		docs + "/directories/root",
		docs + "/directories/root/files",
		docs + "/directories/root/directories",
		docs + "/files/" + q3.ID,
		organizations,
	}

	f.Sever()
	restored := false
	t.Cleanup(func() {
		if !restored {
			f.Restore(t)
		}
	})

	r := c.Get(t, "/readyz")
	_ = r.Problem(t, http.StatusServiceUnavailable)
	var report struct {
		Checks []check `json:"checks"`
	}
	r.JSON(t, &report)
	if names := checkNames(report.Checks); !slices.Equal(names, readyChecks) {
		t.Errorf("readyz checks = %v, want %v", names, readyChecks)
	}
	var unready []string
	for _, ch := range report.Checks {
		if !ch.Ready {
			unready = append(unready, ch.Name)
		}
	}
	if !slices.Equal(unready, []string{"storage"}) {
		t.Errorf("readyz's unready checks = %v; want storage alone", unready)
	}
	_ = c.Get(t, content).Problem(t, http.StatusServiceUnavailable)
	_ = slowPost(t, s, docs+"/directories/root/files?name=q4.txt", body).Problem(t, http.StatusServiceUnavailable)
	for _, path := range reads {
		c.Get(t, path).Expect(t, http.StatusOK)
	}
	c.Get(t, "/healthz").Expect(t, http.StatusOK)

	f.Restore(t)
	restored = true

	processtest.WaitFor(t, "readiness after the store returns", func() bool {
		return c.Get(t, "/readyz").Status == http.StatusOK
	})
	if r := c.Get(t, content).Expect(t, http.StatusOK); string(r.Body) != "report" {
		t.Errorf("download after recovery = %q", r.Body)
	}
	notTheRefusedUploadsName := "q5.txt"
	q5 := webtest.Decode[identity](t, c.Post(t, docs+"/directories/root/files?name="+notTheRefusedUploadsName, body), http.StatusCreated)
	if r := c.Get(t, docs+"/files/"+q5.ID+"/content").Expect(t, http.StatusOK); string(r.Body) != "report" {
		t.Errorf("the upload after recovery = %q", r.Body)
	}
	if code := s.Stop(t); code != 0 {
		t.Fatalf("exit = %d:\n%s", code, s.Output())
	}
}
