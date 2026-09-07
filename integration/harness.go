package integration

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

// Failsafe bounds every wait for an event that should occur, so a broken
// composition fails a test instead of hanging the run.
const Failsafe = 15 * time.Second

// The compose stack's defaults, the values config.json and
// secrets.example.json pair with. The harness reads the same APP_DATABASE_*
// variables the service does, so a stack moved off the defaults follows one
// setting.
const (
	defaultDatabaseHost     = "127.0.0.1"
	defaultDatabasePort     = "5432"
	defaultDatabasePassword = "app"
)

// The build Main produced for the run: the binary's path and the module
// root, the working directory the service loads its configuration files
// from.
var (
	binary string
	root   string
)

// Main is the suite's TestMain: it builds cmd/server once, with the race
// detector so the service runs under it too, runs the tests, and removes
// the build. A build failure ends the run before any test starts.
func Main(m *testing.M) {
	code, err := run(m)
	if err != nil {
		fmt.Fprintln(os.Stderr, "integration:", err)
		code = 1
	}
	os.Exit(code)
}

func run(m *testing.M) (int, error) {
	var err error
	if root, err = moduleRoot(); err != nil {
		return 0, err
	}
	dir, err := os.MkdirTemp("", "go-web-service-integration-")
	if err != nil {
		return 0, err
	}
	defer func() { _ = os.RemoveAll(dir) }()

	binary = filepath.Join(dir, "server")
	build := exec.Command("go", "build", "-race", "-o", binary, "./cmd/server")
	build.Dir = root
	build.Stdout, build.Stderr = os.Stderr, os.Stderr
	if err := build.Run(); err != nil {
		return 0, fmt.Errorf("build cmd/server: %w", err)
	}
	return m.Run(), nil
}

// moduleRoot resolves the module directory, the working directory the
// service loads its configuration files from.
func moduleRoot() (string, error) {
	out, err := exec.Command("go", "list", "-m", "-f", "{{.Dir}}").Output()
	if err != nil {
		return "", fmt.Errorf("locate module root: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

// Options shapes one service process. The zero value runs the service with
// seeding off against the compose database.
type Options struct {
	// Seed turns startup and on-demand seeding on (APP_ADMIN_SEED).
	Seed bool
	// Database overrides the database address as host:port, the way a test
	// routes the service through a Forwarder. Empty uses the compose
	// database.
	Database string
	// Env appends further KEY=VALUE overrides, applied last.
	Env []string
}

// Service is one running service process: its address, its captured
// output, and its exit.
type Service struct {
	cmd    *exec.Cmd
	addr   string
	out    *output
	exited chan struct{}
	code   int
}

// Start runs the service with opts on a reserved loopback port and returns
// once its liveness probe answers, failing the test with the captured output
// if the process exits or Failsafe elapses first. The process is killed at
// test cleanup if it is still running.
func Start(t testing.TB, opts Options) *Service {
	t.Helper()
	if binary == "" {
		t.Fatal("integration: no service binary; the suite's TestMain must call Main")
	}
	host, port, err := splitHostPort(opts.Database)
	if opts.Database != "" && err != nil {
		t.Fatalf("Options.Database: %v", err)
	}
	addr := fmt.Sprintf("127.0.0.1:%d", FreePort(t))

	cmd := exec.Command(binary)
	cmd.Dir = root
	cmd.Env = environment(opts, addr, host, port)
	out := &output{}
	cmd.Stdout, cmd.Stderr = out, out
	if err := cmd.Start(); err != nil {
		t.Fatalf("start service: %v", err)
	}

	s := &Service{cmd: cmd, addr: addr, out: out, exited: make(chan struct{})}
	go func() {
		defer close(s.exited)
		err := cmd.Wait()
		s.code = cmd.ProcessState.ExitCode()
		if err != nil && s.code < 0 {
			s.code = -1
		}
	}()
	t.Cleanup(func() {
		select {
		case <-s.exited:
		default:
			_ = cmd.Process.Kill()
			<-s.exited
		}
	})

	probe := &http.Client{Timeout: time.Second}
	deadline := time.Now().Add(Failsafe)
	for time.Now().Before(deadline) {
		if s.Exited() {
			t.Fatalf("service exited with %d before ready:\n%s", s.code, out.String())
		}
		if res, err := probe.Get(s.URL() + "/healthz"); err == nil {
			_ = res.Body.Close()
			if res.StatusCode == http.StatusOK {
				return s
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("service not live within %s:\n%s", Failsafe, out.String())
	return nil
}

// FreePort reserves an ephemeral loopback port and releases it for the
// service to bind. The window between release and bind is the usual one of
// a port-based harness; a lost race fails the bind, and Start reports the
// exit with the output.
func FreePort(t testing.TB) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve port: %v", err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	if err := l.Close(); err != nil {
		t.Fatalf("release port: %v", err)
	}
	return port
}

// environment composes the process environment: the parent's, with the
// service's own variables set for the run. APP_ENV is cleared so no overlay
// applies: the base file and these variables are the whole configuration.
// The database address is the override when given, else the parent's
// APP_DATABASE_* variables, else the compose defaults.
func environment(opts Options, addr, dbHost, dbPort string) []string {
	host, port := defaultDatabaseHost, defaultDatabasePort
	if v := os.Getenv("APP_DATABASE_HOST"); v != "" {
		host = v
	}
	if v := os.Getenv("APP_DATABASE_PORT"); v != "" {
		port = v
	}
	if opts.Database != "" {
		host, port = dbHost, dbPort
	}
	serverHost, serverPort, _ := splitHostPort(addr)
	password := defaultDatabasePassword
	if v := os.Getenv("APP_DATABASE_PASSWORD"); v != "" {
		password = v
	}

	env := []string{
		"APP_ENV=",
		"APP_LOG_LEVEL=debug",
		"APP_LOG_FORMAT=text",
		"APP_SERVER_HOST=" + serverHost,
		"APP_SERVER_PORT=" + serverPort,
		"APP_DATABASE_HOST=" + host,
		"APP_DATABASE_PORT=" + port,
		"APP_DATABASE_PASSWORD=" + password,
		"APP_ADMIN_SEED=" + fmt.Sprint(opts.Seed),
	}
	env = append(env, opts.Env...)
	return append(os.Environ(), env...)
}

func splitHostPort(addr string) (string, string, error) {
	i := strings.LastIndex(addr, ":")
	if i <= 0 || i == len(addr)-1 {
		return "", "", fmt.Errorf("%q is not host:port", addr)
	}
	return addr[:i], addr[i+1:], nil
}

// Addr is the service's address, host:port.
func (s *Service) Addr() string { return s.addr }

// URL is the service's base URL.
func (s *Service) URL() string { return "http://" + s.addr }

// Client returns a client bound to the service.
func (s *Service) Client() *Client { return NewClient(s.URL()) }

// Output is everything the service has written so far.
func (s *Service) Output() string { return s.out.String() }

// Stop interrupts the service, the signal a terminal or an orchestrator
// sends, waits for it to exit, and returns its exit code; a process still
// running after Failsafe is killed and the test fails.
func (s *Service) Stop(t testing.TB) int {
	t.Helper()
	if s.Exited() {
		return s.code
	}
	if err := s.cmd.Process.Signal(syscall.SIGINT); err != nil {
		t.Fatalf("interrupt service: %v", err)
	}
	return s.Wait(t)
}

// Wait blocks until the service exits and returns its exit code, failing
// the test if Failsafe elapses first.
func (s *Service) Wait(t testing.TB) int {
	t.Helper()
	select {
	case <-s.exited:
		return s.code
	case <-time.After(Failsafe):
		_ = s.cmd.Process.Kill()
		<-s.exited
		t.Fatalf("service did not exit within %s:\n%s", Failsafe, s.Output())
		return -1
	}
}

// Exited reports whether the process has ended.
func (s *Service) Exited() bool {
	select {
	case <-s.exited:
		return true
	default:
		return false
	}
}

// output is the process's captured writes, read whole on a failure.
type output struct {
	mu  sync.Mutex
	buf strings.Builder
}

func (o *output) Write(p []byte) (int, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.buf.Write(p)
}

func (o *output) String() string {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.buf.String()
}

// WaitFor polls fn until it returns true or Failsafe elapses, failing the
// test with what.
func WaitFor(t testing.TB, what string, fn func() bool) {
	t.Helper()
	deadline := time.Now().Add(Failsafe)
	for time.Now().Before(deadline) {
		if fn() {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}
