package database_test

import (
	"encoding/json"
	"testing"

	"github.com/standards-lab/go-web-service/tools/slab/admin/database"
)

func TestSteps_MarshalsUnderTheServiceFieldName(t *testing.T) {
	for _, tc := range []struct {
		steps int
		want  string
	}{
		{1, `{"steps":1}`},
		{-2, `{"steps":-2}`},
		{0, `{"steps":0}`},
	} {
		raw, err := json.Marshal(database.Steps{Steps: tc.steps})
		if err != nil {
			t.Fatal(err)
		}
		if string(raw) != tc.want {
			t.Errorf("body = %s, want %s", raw, tc.want)
		}
	}
}

// Zero is a meaningful version (an empty history), so it must not be
// omitted.
func TestForce_MarshalsUnderTheServiceFieldName(t *testing.T) {
	raw, err := json.Marshal(database.Force{})
	if err != nil {
		t.Fatal(err)
	}
	if want := `{"version":0}`; string(raw) != want {
		t.Errorf("body = %s, want %s", raw, want)
	}
	raw, err = json.Marshal(database.Force{Version: 3})
	if err != nil {
		t.Fatal(err)
	}
	if want := `{"version":3}`; string(raw) != want {
		t.Errorf("body = %s, want %s", raw, want)
	}
}

func TestState_MarshalsUnderTheServiceFieldName(t *testing.T) {
	raw, err := json.Marshal(database.State{State: "default"})
	if err != nil {
		t.Fatal(err)
	}
	if want := `{"state":"default"}`; string(raw) != want {
		t.Errorf("body = %s, want %s", raw, want)
	}
}
