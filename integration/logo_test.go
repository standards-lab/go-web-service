//go:build integration

package integration_test

import (
	"bytes"
	"fmt"
	"net"
	"net/http"
	"regexp"
	"testing"

	"github.com/standards-lab/go-core/process/processtest"
	"github.com/standards-lab/go-web-sdk/webtest"

	"github.com/standards-lab/go-web-service/integration"
)

// reclaimRecord is the sweep reactor's record of a completed pass that
// reclaimed at least one stale row, as the JSON log carries it.
var reclaimRecord = regexp.MustCompile(`"msg":"sweep pass",.*"stale":[1-9]`)

// The logo's write protocol through the real API and the real stores. A
// logo stores, serves, and deletes. Then two uploads fail partway, and
// neither leaves a logo or anything that blocks the organization's delete:
// a client that aborts its body, and an upload whose object put fails. The
// object store is relayed through a forwarder the case severs, so the put
// fails and so does the abandon's object delete, leaving the file's row
// deleting: a stale row, which the service's sweep reclaims once the store
// is back, on a short interval and a one-second stale age.
func TestOrganizationLogo(t *testing.T) {
	f := processtest.Forward(t, integration.StorageAddr())
	s := integration.Start(t, integration.Options{
		Seed:    integration.Default,
		Storage: f.Addr(),
		Env:     []string{"APP_SWEEP_INTERVAL=200ms", "APP_SWEEP_STALE_AGE=1s"},
	})
	c := s.Client()
	integration.Reset(t, c, integration.Default)
	leaf := tree(t, c)["logistics"]
	logo := organizations + "/" + leaf.ID + "/logo"
	png := webtest.Raw{ContentType: "image/png", Body: []byte("png")}

	c.Put(t, logo, png).Expect(t, http.StatusCreated)
	if r := c.Get(t, logo); r.Status != http.StatusOK || string(r.Body) != "png" {
		t.Fatalf("logo = %d %q; want the stored bytes", r.Status, r.Body)
	}
	c.Delete(t, logo).Expect(t, http.StatusNoContent)

	// A client that declares a body and hangs up after three bytes: the
	// upload streams, so the write is under way when the body runs out.
	conn, err := net.Dial("tcp", s.Addr())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fmt.Fprintf(conn, "PUT %s HTTP/1.1\r\nHost: %s\r\nContent-Type: image/png\r\nContent-Length: 1024\r\n\r\npng", logo, s.Addr()); err != nil {
		t.Fatal(err)
	}
	_ = conn.Close()
	_ = c.Get(t, logo).Problem(t, http.StatusNotFound)

	// The store severed, the put fails once the provider's retries are
	// spent, and the abandon's object delete after it, which together
	// outlast the harness client's failsafe; the request gets a client of
	// its own.
	f.Sever()
	restored := false
	t.Cleanup(func() {
		if !restored {
			f.Restore(t)
		}
	})
	req, err := http.NewRequest(http.MethodPut, s.URL()+logo, bytes.NewReader(png.Body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", png.ContentType)
	resp, err := (&http.Client{Timeout: 4 * processtest.Failsafe}).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("PUT with the store severed = %d; want 503", resp.StatusCode)
	}
	_ = c.Get(t, logo).Problem(t, http.StatusNotFound)

	f.Restore(t)
	restored = true
	s.Await(t, "the abandoned write's row reclaimed", func() bool { return reclaimRecord.MatchString(s.Output()) })
	_ = c.Get(t, logo).Problem(t, http.StatusNotFound)
	c.Delete(t, organizations+"/"+leaf.ID, webtest.IfMatch(leaf.Version)).Expect(t, http.StatusNoContent)

	if code := s.Stop(t); code != 0 {
		t.Errorf("exit code = %d, want 0", code)
	}
}
