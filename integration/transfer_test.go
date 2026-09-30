//go:build integration

package integration_test

import (
	"bytes"
	"crypto/rand"
	"errors"
	"io"
	"net"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/standards-lab/go-web-sdk/webtest"

	"github.com/standards-lab/go-web-service/integration"
)

// pacedBody sends left bytes in chunks of size, pausing before each chunk:
// a client uploading at a steady pace.
type pacedBody struct {
	left, size int
	pause      time.Duration
}

func (p *pacedBody) Read(b []byte) (int, error) {
	if p.left == 0 {
		return 0, io.EOF
	}
	time.Sleep(p.pause)
	n := min(p.size, p.left, len(b))
	for i := range n {
		b[i] = 'x'
	}
	p.left -= n
	return n, nil
}

// pacedPost uploads 8 KiB to path at one chunk of 512 bytes per pause and
// returns the response's status and how long the request took.
func pacedPost(t *testing.T, s *integration.Service, path string, pause time.Duration) (int, time.Duration) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, s.URL()+path, &pacedBody{left: 8 << 10, size: 512, pause: pause})
	if err != nil {
		t.Fatal(err)
	}
	req.ContentLength = 8 << 10
	req.Header.Set("Content-Type", "text/plain")
	start := time.Now()
	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		t.Fatalf("POST %s: %v", path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, resp.Body)
	return resp.StatusCode, time.Since(start)
}

// An upload's deadline is its own: under a one-second read timeout and a
// transfer rate of 4 KiB/s, 8 KiB earn three seconds. A client within the
// rate uploads for longer than the read timeout; a client slower than the
// rate is refused with a 408 once its three seconds pass.
func TestTransferDeadlines(t *testing.T) {
	s := integration.Start(t, integration.Options{Seed: integration.Default, Env: []string{
		"APP_SERVER_READ_TIMEOUT=1s",
		"APP_SERVER_WRITE_TIMEOUT=1s",
		"APP_SERVER_TRANSFER_RATE=4096",
	}})
	c := s.Client()
	integration.Reset(t, c, integration.Default)
	docs := "/api/documents/" + tree(t, c)[docsOrg].ID

	status, took := pacedPost(t, s, docs+"/directories/root/files?name=steady.txt", 100*time.Millisecond)
	if status != http.StatusCreated || took < 1500*time.Millisecond {
		t.Errorf("steady upload = %d after %s; want 201 after about 1.6s, past the 1s read timeout", status, took)
	}
	status, took = pacedPost(t, s, docs+"/directories/root/files?name=slow.txt", 400*time.Millisecond)
	if status != http.StatusRequestTimeout || took < 2500*time.Millisecond || took > 5*time.Second {
		t.Errorf("slow upload = %d after %s; want 408 after about 3s", status, took)
	}
}

// stallRelay forwards to target and, once stalled, passes no more bytes
// from target to the client after a budget, as a store that stops sending
// mid-body; Release lets them through again.
type stallRelay struct {
	target string
	ln     net.Listener

	mu      sync.Mutex
	cond    *sync.Cond
	budget  int64 // bytes still allowed downstream once stalled; -1 is unlimited
	stalled bool
}

func newStallRelay(t *testing.T, target string) *stallRelay {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	r := &stallRelay{target: target, ln: ln, budget: -1}
	r.cond = sync.NewCond(&r.mu)
	t.Cleanup(func() {
		r.Release()
		_ = ln.Close()
	})
	go r.serve()
	return r
}

func (r *stallRelay) Addr() string { return r.ln.Addr().String() }

// StallAfter lets n more bytes reach clients, then holds the rest.
func (r *stallRelay) StallAfter(n int64) {
	r.mu.Lock()
	r.budget, r.stalled = n, true
	r.mu.Unlock()
}

// Release passes every held byte and stops stalling.
func (r *stallRelay) Release() {
	r.mu.Lock()
	r.budget, r.stalled = -1, false
	r.cond.Broadcast()
	r.mu.Unlock()
}

func (r *stallRelay) serve() {
	for {
		down, err := r.ln.Accept()
		if err != nil {
			return
		}
		up, err := net.Dial("tcp", r.target)
		if err != nil {
			_ = down.Close()
			continue
		}
		go func() {
			_, _ = io.Copy(up, down)
			_ = up.Close()
		}()
		go func() {
			r.downstream(down, up)
			_ = down.Close()
		}()
	}
}

// downstream copies from up to down, holding what exceeds the budget until
// Release.
func (r *stallRelay) downstream(down, up net.Conn) {
	buf := make([]byte, 32<<10)
	for {
		n, err := up.Read(buf)
		for p := buf[:n]; len(p) > 0; {
			r.mu.Lock()
			for r.stalled && r.budget == 0 {
				r.cond.Wait()
			}
			allow := int64(len(p))
			if r.stalled {
				allow = min(allow, r.budget)
				r.budget -= allow
			}
			r.mu.Unlock()
			if _, werr := down.Write(p[:allow]); werr != nil {
				return
			}
			p = p[allow:]
		}
		if err != nil {
			return
		}
	}
}

// A store that stops sending mid-download is cut off: the read idle
// timeout ends the response short within seconds, where it would otherwise
// hold the request open, and once the store sends again a download is
// whole.
func TestStorageStall(t *testing.T) {
	relay := newStallRelay(t, integration.StorageAddr())
	s := integration.Start(t, integration.Options{Seed: integration.Default, Storage: relay.Addr(), Env: []string{
		"APP_STORAGE_READ_IDLE_TIMEOUT=2s",
		"APP_STORAGE_OPTIONS_TRY_TIMEOUT=1s",
	}})
	c := s.Client()
	integration.Reset(t, c, integration.Default)
	docs := "/api/documents/" + tree(t, c)[docsOrg].ID
	content := make([]byte, 4<<20)
	_, _ = rand.Read(content)
	f := webtest.Decode[identity](t, c.Post(t, docs+"/directories/root/files?name=big.bin", webtest.Raw{ContentType: "application/octet-stream", Body: content}), http.StatusCreated)
	url := s.URL() + docs + "/files/" + f.ID + "/content"

	relay.StallAfter(256 << 10)
	start := time.Now()
	resp, err := (&http.Client{Timeout: 30 * time.Second}).Get(url)
	if err != nil {
		t.Fatalf("GET content: %v", err)
	}
	got, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	took := time.Since(start)
	if resp.StatusCode != http.StatusOK || err == nil || len(got) >= len(content) {
		t.Fatalf("stalled download = %d, %d of %d bytes, %v; want a 200 cut short", resp.StatusCode, len(got), len(content), err)
	}
	if !errors.Is(err, io.ErrUnexpectedEOF) || took > 10*time.Second {
		t.Errorf("stalled download ended after %s with %v; want it cut off within seconds", took, err)
	}

	relay.Release()
	r := c.Get(t, docs+"/files/"+f.ID+"/content").Expect(t, http.StatusOK)
	if !bytes.Equal(r.Body, content) {
		t.Errorf("download after the store recovered = %d bytes; want all %d, intact", len(r.Body), len(content))
	}
}
