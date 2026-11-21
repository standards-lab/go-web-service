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
		{1, `{"set":"app","steps":1}`},
		{-2, `{"set":"app","steps":-2}`},
		{0, `{"set":"app","steps":0}`},
	} {
		raw, err := json.Marshal(database.Steps{Set: "app", Steps: tc.steps})
		if err != nil {
			t.Fatal(err)
		}
		if string(raw) != tc.want {
			t.Errorf("body = %s, want %s", raw, tc.want)
		}
	}
}

func TestDown_OmitsUnsetSteps(t *testing.T) {
	for _, tc := range []struct {
		body database.Down
		want string
	}{
		{database.Down{Set: "app"}, `{"set":"app"}`},
		{database.Down{Set: "app", Steps: 2}, `{"set":"app","steps":2}`},
	} {
		raw, err := json.Marshal(tc.body)
		if err != nil {
			t.Fatal(err)
		}
		if string(raw) != tc.want {
			t.Errorf("body = %s, want %s", raw, tc.want)
		}
	}
}

func TestForce_MarshalsUnderTheServiceFieldName(t *testing.T) {
	raw, err := json.Marshal(database.Force{Set: "app"})
	if err != nil {
		t.Fatal(err)
	}
	if want := `{"set":"app","version":0}`; string(raw) != want {
		t.Errorf("body = %s, want %s", raw, want)
	}
	raw, err = json.Marshal(database.Force{Set: "app", Version: 3})
	if err != nil {
		t.Fatal(err)
	}
	if want := `{"set":"app","version":3}`; string(raw) != want {
		t.Errorf("body = %s, want %s", raw, want)
	}
}

func TestReset_SendsTheConfirmationAsGiven(t *testing.T) {
	for _, tc := range []struct {
		body database.Reset
		want string
	}{
		{database.Reset{State: "default"}, `{"state":"default","confirm":false}`},
		{database.Reset{State: "default", Confirm: true}, `{"state":"default","confirm":true}`},
	} {
		raw, err := json.Marshal(tc.body)
		if err != nil {
			t.Fatal(err)
		}
		if string(raw) != tc.want {
			t.Errorf("body = %s, want %s", raw, tc.want)
		}
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
