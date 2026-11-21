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

var reclaimRecord = regexp.MustCompile(`"msg":"sweep pass",.*"stale":[1-9]`)

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

	objects := integration.Objects(t)
	stored := func(t *testing.T, id, ext string) bool {
		t.Helper()
		_, err := objects.Stat(context.Background(), id+"/"+id+ext)
		if err != nil && !errors.Is(err, storage.ErrNotFound) {
			t.Fatalf("stat the logo %s: %v", id, err)
		}
		return err == nil
	}

	var first, second identity
	var etag string
	t.Run("a logo stores, serves, and revalidates", func(t *testing.T) {
		first = webtest.Decode[identity](t, c.Put(t, logo, png), http.StatusCreated)
		r := c.Get(t, logo)
		if r.Status != http.StatusOK || string(r.Body) != "png" {
			t.Fatalf("logo = %d %q; want the stored bytes", r.Status, r.Body)
		}
		etag = r.Header.Get("ETag")
		if r := c.Get(t, logo, webtest.Header{Name: "If-None-Match", Value: etag}); r.Status != http.StatusNotModified || len(r.Body) != 0 {
			t.Errorf("logo revalidated = %d %q; want 304 and no body", r.Status, r.Body)
		}
		if !stored(t, first.ID, ".png") {
			t.Fatalf("the first logo's object, blobfs's id/name key, is not in the store")
		}
	})

	t.Run("a replacement takes no version and purges the replaced object", func(t *testing.T) {
		second = webtest.Decode[identity](t, c.Put(t, logo, webtest.Raw{ContentType: "image/jpeg", Body: []byte("jpeg")}), http.StatusCreated)
		r := c.Get(t, logo, webtest.Header{Name: "If-None-Match", Value: etag})
		if r.Status != http.StatusOK || string(r.Body) != "jpeg" || r.Header.Get("Content-Type") != "image/jpeg" || r.Header.Get("ETag") == etag {
			t.Errorf("replaced logo = %d %q, %v; want the new bytes under the same URL with a new tag", r.Status, r.Body, r.Header)
		}
		if stored(t, first.ID, ".png") || !stored(t, second.ID, ".jpg") {
			t.Errorf("after the replacement, the store holds the first logo or lacks the second")
		}
	})

	t.Run("a type outside the allowlist is refused and leaves the logo", func(t *testing.T) {
		_ = c.Put(t, logo, webtest.Raw{ContentType: "image/svg+xml", Body: []byte("<svg/>")}).Problem(t, http.StatusUnsupportedMediaType)
		if r := c.Get(t, logo); string(r.Body) != "jpeg" {
			t.Errorf("logo after a refused upload = %d %q; want the replacement's", r.Status, r.Body)
		}
	})

	t.Run("the logo deletes, its object with it", func(t *testing.T) {
		c.Delete(t, logo).Expect(t, http.StatusNoContent)
		if stored(t, second.ID, ".jpg") {
			t.Errorf("the deleted logo's object is still in the store")
		}
	})

	t.Run("a client that hangs up mid-body leaves no logo", func(t *testing.T) {
		conn, err := net.Dial("tcp", s.Addr())
		if err != nil {
			t.Fatal(err)
		}
		const declaredLength, sentBody = 1024, "png"
		if _, err := fmt.Fprintf(conn, "PUT %s HTTP/1.1\r\nHost: %s\r\nContent-Type: image/png\r\nContent-Length: %d\r\n\r\n%s", logo, s.Addr(), declaredLength, sentBody); err != nil {
			t.Fatal(err)
		}
		_ = conn.Close()
		_ = c.Get(t, logo).Problem(t, http.StatusNotFound)
	})

	t.Run("a put the severed store refuses leaves no logo, and the sweep reclaims its row", func(t *testing.T) {
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
		outlastsRetriesAndAbandon := &http.Client{Timeout: 4 * processtest.Failsafe}
		resp, err := outlastsRetriesAndAbandon.Do(req)
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
	})

	t.Run("neither failed upload blocks the organization's delete", func(t *testing.T) {
		c.Delete(t, organizations+"/"+leaf.ID, webtest.IfMatch(leaf.Version)).Expect(t, http.StatusNoContent)
	})

	if code := s.Stop(t); code != 0 {
		t.Errorf("exit code = %d, want 0", code)
	}
}
