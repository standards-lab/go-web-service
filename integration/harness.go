package integration

import (
	"context"
	"net"
	"net/url"
	"os"
	"strconv"
	"testing"

	"github.com/standards-lab/go-core/process/processtest"
	"github.com/standards-lab/go-storage"
	"github.com/standards-lab/go-storage/azureblob"
	"github.com/standards-lab/go-web-sdk/webtest"
)

// The compose stack's defaults, the values config.json and
// secrets.example.json pair with. The harness reads the same APP_DATABASE_*,
// APP_STORAGE_*, and APP_OBSERVABILITY_ENDPOINT variables the service does,
// so a stack moved off the defaults follows one setting. The storage
// account and key are Azurite's published development credential.
const (
	defaultDatabaseHost          = "127.0.0.1"
	defaultDatabasePort          = "5432"
	defaultDatabasePassword      = "app"
	defaultStorageEndpoint       = "http://127.0.0.1:10000/devstoreaccount1"
	defaultStorageAccount        = "devstoreaccount1"
	defaultStorageKey            = "Eby8vdM02xNOcqFlqUwJPLlmEtlCDXJ1OUzFT50uSRZ6IFsuFq2UVErCz4I6tq/K1SZFPTOtr/KBHBeksoGMGw=="
	defaultObservabilityEndpoint = "127.0.0.1:4317"
	// storageContainer is config.json's container, which the harness
	// leaves the service on.
	storageContainer = "go-web-service"
)

// getenv is the parent's value of name, or def when it sets none.
func getenv(name, def string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return def
}

// ServiceZone is the time zone every service process runs in, through TZ:
// one whose offset is never zero (+05:30 all year, with no daylight
// saving), so a time the service encodes in time.Local rather than
// time.UTC shows an offset in its JSON on any date. The process reads the
// host's zone database; a host without one would run it in UTC silently,
// which TestJSONTimesUTC refuses up front.
const ServiceZone = "Asia/Kolkata"

// defaultRateLimitRequests overrides the service's own default (300 per
// minute) generously upward, so no scenario's own request volume against
// one process trips the limit; a scenario that means to exercise the limit
// itself sets Options.Env to a tighter value, which wins as the later
// entry.
const defaultRateLimitRequests = "1000"

// Main is the suite's TestMain: it builds cmd/server once, with the race
// detector so the service runs under it too, runs the tests, and removes
// the build. A build failure ends the run before any test starts.
func Main(m *testing.M) {
	processtest.Main(m, "./cmd/server")
}

// Default is the state the suite starts its cases from: the reference
// tree the data package declares under that name.
const Default = "default"

// Options shapes one service process. The zero value runs the service with
// no seed set against the compose database.
type Options struct {
	// Seed names the state whose set applies at startup and on a seed
	// request naming none (APP_ADMIN_SEED); empty names none.
	Seed string
	// Database overrides the database address as host:port, the way a test
	// routes the service through a processtest.Forwarder. Empty uses the
	// compose database.
	Database string
	// Storage overrides the object store's address as host:port in its
	// endpoint, the way a test routes the service through a
	// processtest.Forwarder. Empty uses the compose store.
	Storage string
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
// waits for one. A test that fails logs the process's output at its
// cleanup, before the process is stopped, so a failure a client sees, a
// 500 from any call, the harness's Reset included, comes with the
// service's own record of it.
func Launch(t testing.TB, opts Options) *Service {
	t.Helper()
	host, port, err := net.SplitHostPort(opts.Database)
	if opts.Database != "" && err != nil {
		t.Fatalf("Options.Database: %v", err)
	}
	addr := net.JoinHostPort("127.0.0.1", strconv.Itoa(processtest.FreePort(t)))
	s := &Service{addr: addr, client: webtest.NewClient("http://" + addr)}
	s.Process = processtest.Launch(t, environment(opts, addr, host, port)...)
	t.Cleanup(func() {
		if t.Failed() {
			t.Logf("service %s output:\n%s", addr, s.Output())
		}
	})
	return s
}

// Ready waits until the service's liveness probe answers, failing the test
// with the captured output if the process exits or the failsafe elapses
// first. The server is alone in the top layer of the lifecycle graph, so a
// live probe means every layer beneath it started.
func (s *Service) Ready(t testing.TB) *Service {
	t.Helper()
	s.Await(t, "liveness", func() bool { return webtest.Live(s.URL()) })
	return s
}

// environment composes the service's own variables for the run. APP_ENV is
// cleared so no overlay applies: the base file and these variables are the
// whole configuration. TZ is ServiceZone, so no process runs in UTC. The
// database address is the override when given, else the parent's
// APP_DATABASE_* variables, else the compose defaults.
func environment(opts Options, addr, dbHost, dbPort string) []string {
	host, port := databaseHostPort()
	if opts.Database != "" {
		host, port = dbHost, dbPort
	}
	serverHost, serverPort, _ := net.SplitHostPort(addr)
	password := defaultDatabasePassword
	if v := os.Getenv("APP_DATABASE_PASSWORD"); v != "" {
		password = v
	}
	endpoint := defaultObservabilityEndpoint
	if v := os.Getenv("APP_OBSERVABILITY_ENDPOINT"); v != "" {
		endpoint = v
	}
	storageEndpoint := storageURL()
	if opts.Storage != "" {
		storageEndpoint.Host = opts.Storage
	}

	env := []string{
		"TZ=" + ServiceZone,
		"APP_ENV=",
		"APP_LOG_LEVEL=debug",
		"APP_LOG_FORMAT=json",
		"APP_SERVER_HOST=" + serverHost,
		"APP_SERVER_PORT=" + serverPort,
		"APP_DATABASE_HOST=" + host,
		"APP_DATABASE_PORT=" + port,
		"APP_DATABASE_PASSWORD=" + password,
		"APP_STORAGE_ENDPOINT=" + storageEndpoint.String(),
		"APP_STORAGE_ACCOUNT=" + getenv("APP_STORAGE_ACCOUNT", defaultStorageAccount),
		"APP_STORAGE_KEY=" + getenv("APP_STORAGE_KEY", defaultStorageKey),
		// One retry, as the local overlay sets, so a severed store's refusal
		// answers in about a second rather than the SDK's backoff.
		"APP_STORAGE_OPTIONS_MAX_RETRIES=1",
		"APP_OBSERVABILITY_ENDPOINT=" + endpoint,
		"APP_ADMIN_SEED=" + opts.Seed,
		"APP_RATE_LIMIT_REQUESTS=" + defaultRateLimitRequests,
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

// databaseHostPort is the compose database's host and port, the parent's
// APP_DATABASE_HOST and APP_DATABASE_PORT or the defaults.
func databaseHostPort() (host, port string) {
	return getenv("APP_DATABASE_HOST", defaultDatabaseHost), getenv("APP_DATABASE_PORT", defaultDatabasePort)
}

// DatabaseAddr is the compose database's address, host:port, the target a
// test forwards the service's database through.
func DatabaseAddr() string { return net.JoinHostPort(databaseHostPort()) }

// storageURL is the compose store's endpoint, the parent's
// APP_STORAGE_ENDPOINT or the default. A value that does not parse is the
// default, as an unset one is.
func storageURL() *url.URL {
	u, err := url.Parse(getenv("APP_STORAGE_ENDPOINT", defaultStorageEndpoint))
	if err != nil || u.Host == "" {
		u, _ = url.Parse(defaultStorageEndpoint)
	}
	return u
}

// StorageAddr is the compose store's address, host:port, the target a
// test forwards the service's object store through.
func StorageAddr() string { return storageURL().Host }

// Objects starts a client of the container the service stores its objects
// in, against the compose store the harness points the service at, so a
// test can assert what the store holds beneath the API: an object the
// service should have deleted is storage.ErrNotFound on Stat. It is the
// harness's reader, not the service's; the store is shut down at cleanup.
func Objects(t testing.TB) *storage.Store {
	t.Helper()
	cfg := storage.Config{
		Endpoint:  storageURL().String(),
		Container: storageContainer,
		Account:   getenv("APP_STORAGE_ACCOUNT", defaultStorageAccount),
		Key:       getenv("APP_STORAGE_KEY", defaultStorageKey),
	}
	if err := cfg.Finalize(""); err != nil {
		t.Fatalf("object store config: %v", err)
	}
	client, err := azureblob.New(cfg)
	if err != nil {
		t.Fatalf("object store client: %v", err)
	}
	store := storage.New(client, cfg)
	if err := store.Start(context.Background()); err != nil {
		t.Fatalf("start the object store client: %v", err)
	}
	t.Cleanup(func() { _ = store.Shutdown(context.Background()) })
	return store
}
