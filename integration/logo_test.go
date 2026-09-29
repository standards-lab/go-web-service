//go:build integration

package integration_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"regexp"
	"testing"

	"github.com/standards-lab/go-core/process/processtest"
	"github.com/standards-lab/go-storage"
	"github.com/standards-lab/go-web-sdk/webtest"

	"github.com/standards-lab/go-web-service/integration"
)

// reclaimRecord is the sweep reactor's record of a completed pass that
// reclaimed at least one stale row, as the JSON log carries it.
var reclaimRecord = regexp.MustCompile(`"msg":"sweep pass",.*"stale":[1-9]`)

// The logo's write protocol through the real API and the real stores. A
// logo stores, serves, and revalidates; a second upload replaces it, its
// object purged from the store; a type outside the allowlist is refused;
// and the logo deletes, its object with it. Then two uploads fail
// partway, and neither leaves a logo or anything that blocks the
// organization's delete: a client that aborts its body, and an upload
// whose object put fails. The
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

	first := webtest.Decode[identity](t, c.Put(t, logo, png), http.StatusCreated)
	r := c.Get(t, logo)
	if r.Status != http.StatusOK || string(r.Body) != "png" {
		t.Fatalf("logo = %d %q; want the stored bytes", r.Status, r.Body)
	}
	etag := r.Header.Get("ETag")
	if r := c.Get(t, logo, webtest.Header{Name: "If-None-Match", Value: etag}); r.Status != http.StatusNotModified || len(r.Body) != 0 {
		t.Errorf("logo revalidated = %d %q; want 304 and no body", r.Status, r.Body)
	}

	// A replacement takes no version: the second PUT's logo is served
	// under the same URL with a new tag, and the replaced object is gone
	// from the store beneath the API. The object's key is blobfs's
	// id/name, the file named for its id.
	objects := integration.Objects(t)
	stored := func(id, ext string) bool {
		t.Helper()
		_, err := objects.Stat(context.Background(), id+"/"+id+ext)
		if err != nil && !errors.Is(err, storage.ErrNotFound) {
			t.Fatalf("stat the logo %s: %v", id, err)
		}
		return err == nil
	}
	if !stored(first.ID, ".png") {
		t.Fatalf("the first logo's object is not in the store")
	}
	second := webtest.Decode[identity](t, c.Put(t, logo, webtest.Raw{ContentType: "image/jpeg", Body: []byte("jpeg")}), http.StatusCreated)
	r = c.Get(t, logo, webtest.Header{Name: "If-None-Match", Value: etag})
	if r.Status != http.StatusOK || string(r.Body) != "jpeg" || r.Header.Get("Content-Type") != "image/jpeg" || r.Header.Get("ETag") == etag {
		t.Errorf("replaced logo = %d %q, %v; want the new bytes under a new tag", r.Status, r.Body, r.Header)
	}
	if stored(first.ID, ".png") || !stored(second.ID, ".jpg") {
		t.Errorf("after the replacement, the store holds the first logo or lacks the second")
	}

	// A type outside the allowlist is refused and leaves the logo as it
	// stands.
	_ = c.Put(t, logo, webtest.Raw{ContentType: "image/svg+xml", Body: []byte("<svg/>")}).Problem(t, http.StatusUnsupportedMediaType)
	if r := c.Get(t, logo); string(r.Body) != "jpeg" {
		t.Errorf("logo after a refused upload = %d %q; want the replacement's", r.Status, r.Body)
	}
	c.Delete(t, logo).Expect(t, http.StatusNoContent)
	if stored(second.ID, ".jpg") {
		t.Errorf("the deleted logo's object is still in the store")
	}

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
