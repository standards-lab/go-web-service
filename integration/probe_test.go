//go:build integration

package integration_test

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/standards-lab/go-core/process/processtest"
	"github.com/standards-lab/go-web-sdk/webtest"

	"github.com/standards-lab/go-web-service/integration"
)

// readyChecks is the readiness probe's checks in the order it lists them:
// the coordinator itself, then each service's check in start order.
var readyChecks = []string{"lifecycle", "database", "storage", "broker", "schema", "sweeper", "relay"}

// checkNames is the names of a readiness report's checks, in its order.
func checkNames(checks []check) []string {
	names := make([]string, len(checks))
	for i, c := range checks {
		names[i] = c.Name
	}
	return names
}

// assertReady asserts the readiness probe answers 200 as JSON, uncached,
// with status ready and every check, in start order, ready.
func assertReady(t *testing.T, c *webtest.Client) {
	t.Helper()
	r := c.Get(t, "/readyz").Expect(t, http.StatusOK)
	assertProbeHeaders(t, "readyz", r)
	var ready readiness
	r.JSON(t, &ready)
	if ready.Status != "ready" {
		t.Errorf("readyz status = %q, want ready", ready.Status)
	}
	if names := checkNames(ready.Checks); !slices.Equal(names, readyChecks) {
		t.Errorf("readyz checks = %v, want %v", names, readyChecks)
	}
	for _, ch := range ready.Checks {
		if !ch.Ready {
			t.Errorf("readyz check %s not ready", ch.Name)
		}
	}
}

// assertProbeHeaders asserts a probe's answer is JSON a cache never keeps.
func assertProbeHeaders(t *testing.T, probe string, r *webtest.Response) {
	t.Helper()
	if ct := r.Header.Get("Content-Type"); ct != "application/json" {
		t.Errorf("%s Content-Type = %q, want application/json", probe, ct)
	}
	if cc := r.Header.Get("Cache-Control"); cc != "no-store" {
		t.Errorf("%s Cache-Control = %q, want no-store", probe, cc)
	}
}

// logRecords is every JSON log record in out, skipping lines that are not
// JSON objects, such as the OpenTelemetry error handler's own output.
func logRecords(out string) []map[string]any {
	var records []map[string]any
	for line := range strings.Lines(out) {
		var r map[string]any
		if err := json.Unmarshal([]byte(line), &r); err == nil {
			records = append(records, r)
		}
	}
	return records
}

// withMsg is the records in out whose msg is msg.
func withMsg(out, msg string) []map[string]any {
	var matched []map[string]any
	for _, r := range logRecords(out) {
		if r["msg"] == msg {
			matched = append(matched, r)
		}
	}
	return matched
}

// The probes answer as JSON a cache never keeps: liveness {"status":"ok"}
// with the request's id, readiness {"status":"ready"} with every check
// listed in start order.
func TestProbes(t *testing.T) {
	s := integration.Start(t, integration.Options{})
	c := s.Client()

	r := c.Get(t, "/healthz").Expect(t, http.StatusOK)
	assertProbeHeaders(t, "healthz", r)
	if id := r.Header.Get("X-Request-Id"); id == "" {
		t.Error("healthz carries no X-Request-Id")
	}
	var live map[string]string
	r.JSON(t, &live)
	if len(live) != 1 || live["status"] != "ok" {
		t.Errorf("healthz body = %v, want {status: ok}", live)
	}

	assertReady(t, c)
}

// The ready record names the address the server bound, not the one it
// was configured with: on port 0 the record carries the assigned port, it
// is logged once, and that address answers the readiness probe.
func TestProbes_ReadyRecordNamesBoundAddress(t *testing.T) {
	s := integration.Launch(t, integration.Options{Env: []string{"APP_SERVER_PORT=0"}})

	var addr string
	s.Await(t, "the server ready record", func() bool {
		ready := withMsg(s.Output(), "server ready")
		if len(ready) == 0 {
			return false
		}
		addr, _ = ready[0]["addr"].(string)
		return true
	})
	host, port, err := net.SplitHostPort(addr)
	if err != nil || host != "127.0.0.1" || port == "0" || port == "" {
		t.Fatalf("server ready addr = %q (%v), want 127.0.0.1 and the bound port", addr, err)
	}
	assertReady(t, webtest.NewClient("http://"+addr))
	if n := len(withMsg(s.Output(), "server ready")); n != 1 {
		t.Errorf("server ready logged %d times, want once", n)
	}
}

// A port another listener holds fails startup: the process exits 1 with a
// startup error that names the server, and is never ready.
func TestStartup_PortInUse(t *testing.T) {
	held, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("hold a port: %v", err)
	}
	t.Cleanup(func() { _ = held.Close() })
	_, port, _ := net.SplitHostPort(held.Addr().String())

	s := integration.Launch(t, integration.Options{Env: []string{"APP_SERVER_PORT=" + port}})
	if code := s.Wait(t); code != 1 {
		t.Fatalf("exit = %d, want 1:\n%s", code, s.Output())
	}
	var named bool
	for _, r := range logRecords(s.Output()) {
		if e, _ := r["error"].(string); strings.HasPrefix(e, "startup:") && strings.Contains(e, "server") {
			named = true
		}
	}
	if !named {
		t.Errorf("no startup: error naming server:\n%s", s.Output())
	}
	if ready := withMsg(s.Output(), "server ready"); len(ready) != 0 {
		t.Errorf("server ready logged on a failed startup: %v", ready)
	}
}

// A configuration that fails to load ends the process before anything is
// composed: it exits 1 with "config load failed" and the variable at fault
// on stderr, and writes nothing to stdout, the service's log.
func TestStartup_ConfigLoadFailure(t *testing.T) {
	res := processtest.Run(t, processtest.Cmd{Env: []string{"APP_ENV=", "APP_SHUTDOWN_TIMEOUT=soon"}})
	if res.Code != 1 {
		t.Errorf("exit = %d, want 1", res.Code)
	}
	if !strings.HasPrefix(res.Stderr, "config load failed: ") || !strings.Contains(res.Stderr, "APP_SHUTDOWN_TIMEOUT") {
		t.Errorf("stderr = %q, want config load failed naming APP_SHUTDOWN_TIMEOUT", res.Stderr)
	}
	if res.Stdout != "" {
		t.Errorf("stdout = %q, want nothing", res.Stdout)
	}
}

// shutdownTimeout is the drain bound the shutdown case configures, and
// shutdownMargin what it allows beyond it for the process to exit.
const (
	shutdownTimeout = time.Second
	shutdownMargin  = 3 * time.Second
)

// heldUpload is an upload in flight on a raw connection, its declared body
// partly sent, held after the server's 100 Continue: the handler's first
// read of the body, so the request is in its handler, and the write's
// first transaction has committed the pending row.
type heldUpload struct {
	conn net.Conn
	r    *bufio.Reader
}

// heldSize is the body a held upload declares, and heldPart what it sends
// before it is held.
const (
	heldSize = 1024
	heldPart = "partial"
)

// hold sends the upload to path on s and holds it as heldUpload says. The
// connection is closed at cleanup, unless finish or release ends the
// request first.
func hold(t *testing.T, s *integration.Service, path string) *heldUpload {
	t.Helper()
	conn, err := net.DialTimeout("tcp", s.Addr(), processtest.Failsafe)
	if err != nil {
		t.Fatalf("dial %s: %v", s.Addr(), err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	if err := conn.SetDeadline(time.Now().Add(4 * processtest.Failsafe)); err != nil {
		t.Fatal(err)
	}
	head := fmt.Sprintf("POST %s HTTP/1.1\r\nHost: %s\r\nContent-Type: text/plain\r\nContent-Length: %d\r\nExpect: 100-continue\r\n\r\n", path, s.Addr(), heldSize)
	if _, err := conn.Write([]byte(head)); err != nil {
		t.Fatalf("send the upload's head: %v", err)
	}
	r := bufio.NewReader(conn)
	status, err := r.ReadString('\n')
	if err != nil || !strings.HasPrefix(status, "HTTP/1.1 100 ") {
		t.Fatalf("upload answered %q, %v; want 100 Continue", strings.TrimSpace(status), err)
	}
	// The interim response ends at its blank line.
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			t.Fatalf("read the interim response: %v", err)
		}
		if line == "\r\n" {
			break
		}
	}
	if _, err := conn.Write([]byte(heldPart)); err != nil {
		t.Fatalf("send part of the body: %v", err)
	}
	return &heldUpload{conn: conn, r: r}
}

// finish sends the rest of the held upload's body and returns the status
// and the body of its response.
func (h *heldUpload) finish(t *testing.T) (int, []byte) {
	t.Helper()
	if _, err := h.conn.Write(bytes.Repeat([]byte("x"), heldSize-len(heldPart))); err != nil {
		t.Fatalf("send the rest of the body: %v", err)
	}
	resp, err := http.ReadResponse(h.r, nil)
	if err != nil {
		t.Fatalf("read the upload's response: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	var b bytes.Buffer
	_, _ = b.ReadFrom(resp.Body)
	return resp.StatusCode, b.Bytes()
}

// release closes the held upload's connection, ending its request without
// the rest of its body.
func (h *heldUpload) release() { _ = h.conn.Close() }

// The drain is bounded by shutdown_timeout: an upload held in flight past
// the interrupt keeps the server from draining cleanly, and the process
// exits 1 once the timeout elapses rather than waiting on the request.
func TestShutdown_BoundedByTimeout(t *testing.T) {
	s := integration.Start(t, integration.Options{
		Seed: integration.Default,
		Env:  []string{"APP_SHUTDOWN_TIMEOUT=" + shutdownTimeout.String()},
	})
	c := s.Client()
	integration.Reset(t, c, integration.Default)
	hold(t, s, "/api/documents/"+tree(t, c)[docsOrg].ID+"/directories/root/files?name=held.txt")

	start := time.Now()
	code := s.Stop(t)
	elapsed := time.Since(start)
	if code != 1 {
		t.Errorf("exit = %d, want 1:\n%s", code, s.Output())
	}
	if elapsed < shutdownTimeout {
		t.Errorf("exited %s after the interrupt, before the %s timeout; want the held request to hold the drain", elapsed, shutdownTimeout)
	}
	if elapsed > shutdownTimeout+shutdownMargin {
		t.Errorf("exited %s after the interrupt, want within %s plus %s", elapsed, shutdownTimeout, shutdownMargin)
	}
}
