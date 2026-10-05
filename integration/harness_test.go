package integration_test

import (
	"testing"

	"github.com/standards-lab/go-web-service/integration"
)

// The store a test forwards through is the one the service is pointed
// at: the address of the parent's APP_STORAGE_ENDPOINT, or of the compose
// default when the parent sets none or one that does not parse.
func TestStorageAddr(t *testing.T) {
	for endpoint, want := range map[string]string{
		"http://127.0.0.1:10001/devstoreaccount1": "127.0.0.1:10001",
		"":            "127.0.0.1:10000",
		"not a url":   "127.0.0.1:10000",
		"http://[::1": "127.0.0.1:10000",
	} {
		t.Setenv("APP_STORAGE_ENDPOINT", endpoint)
		if got := integration.StorageAddr(); got != want {
			t.Errorf("StorageAddr with APP_STORAGE_ENDPOINT=%q = %q, want %q", endpoint, got, want)
		}
	}
}

// The database a test forwards through follows the same variables the
// service's database address does, the defaults beneath them.
func TestDatabaseAddr(t *testing.T) {
	t.Setenv("APP_DATABASE_HOST", "")
	t.Setenv("APP_DATABASE_PORT", "5433")
	if got := integration.DatabaseAddr(); got != "127.0.0.1:5433" {
		t.Errorf("DatabaseAddr = %q, want the default host and the parent's port", got)
	}
}
