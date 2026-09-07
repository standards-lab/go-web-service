//go:build integration

package integration_test

import (
	"net/http"
	"os"
	"testing"

	"github.com/standards-lab/go-web-service/integration"
)

// databaseAddr is the compose database as the forwarder reaches it, from
// the same variables the harness passes the service.
func databaseAddr() string {
	host, port := "127.0.0.1", "5432"
	if v := os.Getenv("APP_DATABASE_HOST"); v != "" {
		host = v
	}
	if v := os.Getenv("APP_DATABASE_PORT"); v != "" {
		port = v
	}
	return host + ":" + port
}

// A database outage is a temporary condition, not a server fault: while
// the database is unreachable the readiness probe, a read, and a command
// each answer 503, and when it returns the service recovers without a
// restart.
func TestOutage(t *testing.T) {
	f := integration.Forward(t, databaseAddr())
	s := integration.Start(t, integration.Options{Seed: true, Database: f.Addr()})
	c := s.Client()
	integration.Reset(t, c)
	all := tree(t, c)
	fin := all["finance"]
	edit := map[string]any{"code": "fin", "name": "Finance"}

	f.Sever()

	ready := c.Get(t, "/readyz").Problem(t, http.StatusServiceUnavailable)
	if ready.Detail == "" {
		t.Error("readyz 503 carries no detail")
	}
	c.Get(t, organizations).Problem(t, http.StatusServiceUnavailable)
	c.Get(t, organizations+"/"+fin.ID).Problem(t, http.StatusServiceUnavailable)
	c.Put(t, organizations+"/"+fin.ID, edit, integration.IfMatch(fin.Version)).Problem(t, http.StatusServiceUnavailable)
	c.Get(t, "/healthz").Expect(t, http.StatusOK) // the process is up; only the dependency is down

	f.Restore(t)

	integration.WaitFor(t, "readiness after the database returns", func() bool {
		return c.Get(t, "/readyz").Status == http.StatusOK
	})
	if p := integration.Decode[organizationPage](t, c.Get(t, organizations), http.StatusOK); p.Total != seededTotal {
		t.Errorf("total after recovery = %d", p.Total)
	}
	ident := integration.Decode[identity](t, c.Put(t, organizations+"/"+fin.ID, edit, integration.IfMatch(fin.Version)), http.StatusOK)
	if ident.Version != fin.Version+1 {
		t.Errorf("edit after recovery = %+v", ident)
	}
	if code := s.Stop(t); code != 0 {
		t.Fatalf("exit = %d:\n%s", code, s.Output())
	}
}
