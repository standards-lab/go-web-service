package integration

import (
	"fmt"
	"net"
	"os"
	"strconv"
	"testing"

	"github.com/standards-lab/go-core/process/processtest"
	"github.com/standards-lab/go-web-sdk/webtest"
)

// The compose stack's defaults, the values config.json and
// secrets.example.json pair with. The harness reads the same APP_DATABASE_*
// variables the service does, so a stack moved off the defaults follows one
// setting.
const (
	defaultDatabaseHost     = "127.0.0.1"
	defaultDatabasePort     = "5432"
	defaultDatabasePassword = "app"
)

// Main is the suite's TestMain: it builds cmd/server once, with the race
// detector so the service runs under it too, runs the tests, and removes
// the build. A build failure ends the run before any test starts.
func Main(m *testing.M) {
	processtest.Main(m, "./cmd/server")
}

// Options shapes one service process. The zero value runs the service with
// seeding off against the compose database.
type Options struct {
	// Seed turns startup and on-demand seeding on (APP_ADMIN_SEED).
	Seed bool
	// Database overrides the database address as host:port, the way a test
	// routes the service through a processtest.Forwarder. Empty uses the
	// compose database.
	Database string
	// Env appends further KEY=VALUE overrides, applied last.
	Env []string
}

// Service is one running service process: its address, its captured
// output, and its exit.
type Service struct {
	*processtest.Process
	addr   string
	client *webtest.Client
}

// Start runs the service with opts and returns once it is live: Launch
// then Ready.
func Start(t testing.TB, opts Options) *Service {
	t.Helper()
	return Launch(t, opts).Ready(t)
}

// Launch runs the service with opts on a reserved loopback port and returns
// without waiting, so a test can start several processes at once; Ready
// waits for one.
func Launch(t testing.TB, opts Options) *Service {
	t.Helper()
	host, port, err := net.SplitHostPort(opts.Database)
	if opts.Database != "" && err != nil {
		t.Fatalf("Options.Database: %v", err)
	}
	addr := net.JoinHostPort("127.0.0.1", strconv.Itoa(processtest.FreePort(t)))
	s := &Service{addr: addr, client: webtest.NewClient("http://" + addr)}
	s.Process = processtest.Launch(t, environment(opts, addr, host, port)...)
	return s
}

// Ready waits until the service's liveness probe answers, failing the test
// with the captured output if the process exits or the failsafe elapses
// first. The server is the root lifecycle stage, so a live probe means
// every stage beneath it started.
func (s *Service) Ready(t testing.TB) *Service {
	t.Helper()
	s.Await(t, "liveness", func() bool { return webtest.Live(s.URL()) })
	return s
}

// environment composes the service's own variables for the run. APP_ENV is
// cleared so no overlay applies: the base file and these variables are the
// whole configuration. The database address is the override when given,
// else the parent's APP_DATABASE_* variables, else the compose defaults.
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
	serverHost, serverPort, _ := net.SplitHostPort(addr)
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
	return append(env, opts.Env...)
}

// Addr is the service's address, host:port.
func (s *Service) Addr() string { return s.addr }

// URL is the service's base URL.
func (s *Service) URL() string { return "http://" + s.addr }

// Client returns the client bound to the service, one per process so its
// connection is reused across calls.
func (s *Service) Client() *webtest.Client { return s.client }
