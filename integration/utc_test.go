//go:build integration

package integration_test

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/standards-lab/go-web-sdk/webtest"

	"github.com/standards-lab/go-web-service/integration"
)

type stamped struct {
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

func utc(t *testing.T, what string, r stamped) {
	t.Helper()
	for field, v := range map[string]string{"created_at": r.CreatedAt, "updated_at": r.UpdatedAt} {
		if _, err := time.Parse(time.RFC3339Nano, v); err != nil || !strings.HasSuffix(v, "Z") {
			t.Errorf("%s %s = %q, want an RFC 3339 time in UTC, ending in Z", what, field, v)
		}
	}
}

func TestJSONTimesUTC(t *testing.T) {
	if _, err := time.LoadLocation(integration.ServiceZone); err != nil {
		t.Fatalf("the host has no zone database entry for %s: %v; without it the service runs in UTC and the test proves nothing", integration.ServiceZone, err)
	}

	s := integration.Start(t, integration.Options{Seed: integration.Default})
	c := s.Client()
	integration.Reset(t, c, integration.Default)
	org := tree(t, c)[docsOrg]
	docs := "/api/documents/" + org.ID

	utc(t, "organization", webtest.Decode[stamped](t, c.Get(t, organizations+"/"+org.ID), http.StatusOK))

	dir := webtest.Decode[identity](t, c.Post(t, docs+"/directories", map[string]string{"parent_id": "root", "name": "reports"}), http.StatusCreated)
	utc(t, "directory", webtest.Decode[stamped](t, c.Get(t, docs+"/directories/"+dir.ID), http.StatusOK))

	f := webtest.Decode[identity](t, c.Post(t, docs+"/directories/"+dir.ID+"/files?name=q3.txt", webtest.Raw{ContentType: "text/plain", Body: []byte("report")}), http.StatusCreated)
	utc(t, "file", webtest.Decode[stamped](t, c.Get(t, docs+"/files/"+f.ID), http.StatusOK))
}
