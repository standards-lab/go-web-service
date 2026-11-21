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

var readyChecks = []string{"lifecycle", "database", "storage", "broker", "schema", "sweeper", "relay"}

func checkNames(checks []check) []string {
	names := make([]string, len(checks))
	for i, c := range checks {
		names[i] = c.Name
	}
	return names
}

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

func assertProbeHeaders(t *testing.T, probe string, r *webtest.Response) {
	t.Helper()
	if ct := r.Header.Get("Content-Type"); ct != "application/json" {
		t.Errorf("%s Content-Type = %q, want application/json", probe, ct)
	}
	if cc := r.Header.Get("Cache-Control"); cc != "no-store" {
		t.Errorf("%s Cache-Control = %q, want no-store", probe, cc)
	}
}

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

func withMsg(out, msg string) []map[string]any {
	var matched []map[string]any
	for _, r := range logRecords(out) {
		if r["msg"] == msg {
			matched = append(matched, r)
		}
	}
	return matched
}

func TestProbes_AnswerUncachedJSONWithEveryCheckInStartOrder(t *testing.T) {
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

func TestProbes_ReadyRecordNamesTheBoundAddressOnce(t *testing.T) {
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

func TestStartup_PortInUseExitsOneNamingTheServerNeverReady(t *testing.T) {
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

func TestStartup_ConfigLoadFailureExitsOneBeforeComposing(t *testing.T) {
	res := processtest.Run(t, processtest.Cmd{Env: []string{"APP_ENV=", "APP_SHUTDOWN_TIMEOUT=soon"}})
	if res.Code != 1 {
		t.Errorf("exit = %d, want 1", res.Code)
	}
	if !strings.HasPrefix(res.Stderr, "config load failed: ") || !strings.Contains(res.Stderr, "APP_SHUTDOWN_TIMEOUT") {
		t.Errorf("stderr = %q, want config load failed naming APP_SHUTDOWN_TIMEOUT", res.Stderr)
	}
	if res.Stdout != "" {
		t.Errorf("stdout = %q, want nothing in the service's log", res.Stdout)
	}
}

const (
	shutdownTimeout = time.Second
	shutdownMargin  = 3 * time.Second
)

type heldUpload struct {
	conn net.Conn
	r    *bufio.Reader
}

const (
	heldSize = 1024
	heldPart = "partial"
)

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
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			t.Fatalf("read the interim response: %v", err)
		}
		if endOfInterimResponse := line == "\r\n"; endOfInterimResponse {
			break
		}
	}
	if _, err := conn.Write([]byte(heldPart)); err != nil {
		t.Fatalf("send part of the body: %v", err)
	}
	return &heldUpload{conn: conn, r: r}
}

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

func (h *heldUpload) release() { _ = h.conn.Close() }

func TestShutdown_HeldUploadExitsOneOnceTheTimeoutElapses(t *testing.T) {
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
