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

// stamped is any API resource's timestamps, as the JSON carries them.
type stamped struct {
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

// utc asserts that both of the resource's times are RFC 3339 in UTC: the
// text ends in Z, not in the offset of the zone the service runs in.
func utc(t *testing.T, what string, r stamped) {
	t.Helper()
	for field, v := range map[string]string{"created_at": r.CreatedAt, "updated_at": r.UpdatedAt} {
		if _, err := time.Parse(time.RFC3339Nano, v); err != nil || !strings.HasSuffix(v, "Z") {
			t.Errorf("%s %s = %q, want an RFC 3339 time in UTC, ending in Z", what, field, v)
		}
	}
}

// Every time the API returns is in UTC, whatever zone the service runs in:
// the libraries return each time.Time in time.UTC, so the JSON encodes it
// with a Z. The service runs in integration.ServiceZone, so a time left in
// time.Local would carry its offset: +01:00 under British Summer Time, and
// Z, indistinguishably, in winter. The organization's times are the
// service's own rows, read through sqlate; the directory's and the file's
// are blobfs's.
func TestJSONTimesUTC(t *testing.T) {
	// The service resolves TZ from this host's zone database, as this
	// lookup does; without the zone it would run in UTC and prove nothing.
	if _, err := time.LoadLocation(integration.ServiceZone); err != nil {
		t.Fatalf("the host has no zone database entry for %s: %v", integration.ServiceZone, err)
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
