package integration

import (
	"context"
	"net"
	"net/url"
	"os"
	"strconv"
	"testing"

	"github.com/standards-lab/go-core/process/processtest"
	"github.com/standards-lab/go-database"
	"github.com/standards-lab/go-database/postgres"
	"github.com/standards-lab/go-storage"
	"github.com/standards-lab/go-storage/azureblob"
	"github.com/standards-lab/go-web-sdk/webtest"
	"github.com/standards-lab/sqlate"
	pgdialect "github.com/standards-lab/sqlate/postgres"
)

const (
	defaultDatabaseHost          = "127.0.0.1"
	defaultDatabasePort          = "5432"
	defaultDatabasePassword      = "app"
	defaultStorageEndpoint       = "http://127.0.0.1:10000/devstoreaccount1"
	azuriteDevelopmentAccount    = "devstoreaccount1"
	azuriteDevelopmentKey        = "Eby8vdM02xNOcqFlqUwJPLlmEtlCDXJ1OUzFT50uSRZ6IFsuFq2UVErCz4I6tq/K1SZFPTOtr/KBHBeksoGMGw=="
	defaultObservabilityEndpoint = "127.0.0.1:4317"
	defaultNATSURL               = "nats://127.0.0.1:4222"
	configuredContainer          = "go-web-service"
)

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

const generousRateLimitRequests = "1000"

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
	// NATS overrides the broker's address as host:port in its URL, the way
	// a test routes the service through a processtest.Forwarder. Empty uses
	// the compose NATS.
	NATS string
	// Env appends further KEY=VALUE overrides, applied last. The harness
	// raises APP_RATE_LIMIT_REQUESTS well above the service's default, so
	// no test's own volume trips the limit; a test of the limit itself
	// sets a tighter value here.
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
// service's own record of it. The process's configuration is the base file
// and the harness's variables alone, with no APP_ENV overlay, in
// ServiceZone, and its object store retries once, as the local overlay
// sets, so a severed store's refusal answers in about a second.
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
	natsURL := brokerURL()
	if opts.NATS != "" {
		natsURL.Host = opts.NATS
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
		"APP_STORAGE_ACCOUNT=" + getenv("APP_STORAGE_ACCOUNT", azuriteDevelopmentAccount),
		"APP_STORAGE_KEY=" + getenv("APP_STORAGE_KEY", azuriteDevelopmentKey),
		"APP_STORAGE_OPTIONS_MAX_RETRIES=1",
		"APP_NATS_URL=" + natsURL.String(),
		"APP_OBSERVABILITY_ENDPOINT=" + endpoint,
		"APP_ADMIN_SEED=" + opts.Seed,
		"APP_RATE_LIMIT_REQUESTS=" + generousRateLimitRequests,
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

func databaseHostPort() (host, port string) {
	return getenv("APP_DATABASE_HOST", defaultDatabaseHost), getenv("APP_DATABASE_PORT", defaultDatabasePort)
}

// DatabaseAddr is the compose database's address, host:port, the target a
// test forwards the service's database through.
func DatabaseAddr() string { return net.JoinHostPort(databaseHostPort()) }

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

func brokerURL() *url.URL {
	u, err := url.Parse(getenv("APP_NATS_URL", defaultNATSURL))
	if err != nil || u.Host == "" {
		u, _ = url.Parse(defaultNATSURL)
	}
	return u
}

// NATSAddr is the compose NATS's address, host:port, the target a test
// forwards the service's broker through.
func NATSAddr() string { return brokerURL().Host }

// Objects starts a client of the container the service stores its objects
// in, against the compose store the harness points the service at, so a
// test can assert what the store holds beneath the API: an object the
// service should have deleted is storage.ErrNotFound on Stat. It is the
// harness's reader, not the service's; the store is shut down at cleanup.
func Objects(t testing.TB) *storage.Store {
	t.Helper()
	cfg := storage.Config{
		Endpoint:  storageURL().String(),
		Container: configuredContainer,
		Account:   getenv("APP_STORAGE_ACCOUNT", azuriteDevelopmentAccount),
		Key:       getenv("APP_STORAGE_KEY", azuriteDevelopmentKey),
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

// Database opens a session on the compose database the harness points the
// service at, as the app user config.json names, so a test can write or
// read beneath the API what no route reaches yet, such as an outbox row. It
// is the harness's session, not the service's; the pool is shut down at
// cleanup.
func Database(t testing.TB) *sqlate.DB {
	t.Helper()
	host, port := databaseHostPort()
	p, err := strconv.Atoi(port)
	if err != nil {
		t.Fatalf("database port %q: %v", port, err)
	}
	cfg := database.Config{
		Host:     host,
		Port:     &p,
		Name:     "app",
		User:     "app",
		Password: getenv("APP_DATABASE_PASSWORD", defaultDatabasePassword),
		Options:  map[string]string{"sslmode": "disable"},
	}
	if err := cfg.Finalize(""); err != nil {
		t.Fatalf("database config: %v", err)
	}
	db, err := postgres.New(cfg)
	if err != nil {
		t.Fatalf("database pool: %v", err)
	}
	if err := db.Start(context.Background()); err != nil {
		t.Fatalf("start the database pool: %v", err)
	}
	t.Cleanup(func() { _ = db.Shutdown(context.Background()) })
	return sqlate.Wrap(db.Conn(), pgdialect.Dialect{})
}
