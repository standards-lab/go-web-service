package integration

import (
	"slices"
	"testing"
)

// The storage override replaces the endpoint's address and keeps the rest
// of it, the account path the service signs against; without one the
// service gets the compose store's endpoint.
func TestEnvironment_StorageOverride(t *testing.T) {
	t.Setenv("APP_STORAGE_ENDPOINT", "http://127.0.0.1:10001/devstoreaccount1")

	env := environment(Options{Storage: "127.0.0.1:4242"}, "127.0.0.1:8080", "", "")
	if !slices.Contains(env, "APP_STORAGE_ENDPOINT=http://127.0.0.1:4242/devstoreaccount1") {
		t.Errorf("environment with a storage override = %v", env)
	}
	env = environment(Options{}, "127.0.0.1:8080", "", "")
	if !slices.Contains(env, "APP_STORAGE_ENDPOINT=http://127.0.0.1:10001/devstoreaccount1") {
		t.Errorf("environment without one = %v", env)
	}
	if got := StorageAddr(); got != "127.0.0.1:10001" {
		t.Errorf("StorageAddr = %q, want the endpoint's address", got)
	}
}
