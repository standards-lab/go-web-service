package configtest

import (
	"fmt"
	"net"
	"testing"

	"github.com/standards-lab/go-core/logging"
	"github.com/standards-lab/go-web-service/internal/config"
)

// Minimal returns an unfinalized Config with every block's required fields
// set and nothing else — the base for tests that exercise Finalize
// themselves, under their own prefix.
func Minimal() *config.Config {
	cfg := &config.Config{}
	cfg.Database.Name = "app"
	cfg.Storage.Container = "go-web-service"
	cfg.Observability.Endpoint = "127.0.0.1:4317"
	return cfg
}

// Config returns a finalized Config whose composition performs no I/O: the
// server on a loopback ephemeral port, debug logging so requests leave
// records, and every connectable subsystem aimed at a closed loopback port
// so a dial is refused immediately instead of timing out, the object store
// with its retries off. The empty prefix disables environment overrides.
func Config(t *testing.T) *config.Config {
	t.Helper()
	cfg := Minimal()
	cfg.Server.Host = "127.0.0.1"
	cfg.Server.Port = new(int)
	cfg.Log.Level = logging.LevelDebug
	cfg.Database.User = "app"
	cfg.Database.Host = "127.0.0.1"
	port := ClosedPort(t)
	cfg.Database.Port = &port
	cfg.Storage.Account = "devstoreaccount1"
	cfg.Storage.Key = "a2V5"
	cfg.Storage.Endpoint = fmt.Sprintf("http://127.0.0.1:%d/devstoreaccount1", ClosedPort(t))
	// One try: the provider's default retries would hold startup open
	// against the closed port its dial is refused on.
	cfg.Storage.Options = map[string]string{"max_retries": "0"}
	cfg.Observability.Endpoint = fmt.Sprintf("127.0.0.1:%d", ClosedPort(t))
	if err := cfg.Finalize(""); err != nil {
		t.Fatalf("finalize hermetic config: %v", err)
	}
	return cfg
}

// ClosedPort reserves an ephemeral loopback port and releases it, so a
// connection attempt against it is refused immediately.
func ClosedPort(t *testing.T) int {
	t.Helper()
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve port: %v", err)
	}
	port := lis.Addr().(*net.TCPAddr).Port
	if err := lis.Close(); err != nil {
		t.Fatalf("release port: %v", err)
	}
	return port
}
