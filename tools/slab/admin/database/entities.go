package database

// Steps is the body of the schema down and schema steps commands, the shape
// of admin/database's Steps: how many migrations to revert for down, which
// requires a positive count, or for steps to apply when positive and revert
// when negative.
type Steps struct {
	Steps int `json:"steps"`
}

// Force is the body of the schema force command, the shape of
// admin/database's Force: the version the history is set to, 0 emptying it.
type Force struct {
	Version int `json:"version"`
}

// State is the body of the state command and the optional body of the seed
// command, the shape of admin/database's State: the name of the state to
// reset to, or of the set to apply.
type State struct {
	State string `json:"state"`
}
