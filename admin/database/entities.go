package database

// Steps is the body of the down and steps operations: the migration set
// the operation acts on, and how many of its migrations to revert, or to
// apply when positive and revert when negative. down reverts one when
// Steps is omitted.
type Steps struct {
	Set   string `json:"set"`
	Steps int    `json:"steps"`
}

// Force is the body of the force operation: the migration set whose
// history is set, and the version it is set to, 0 emptying it.
type Force struct {
	Set     string `json:"set"`
	Version int    `json:"version"`
}

// State is the optional body of the seed operation: the name of the state
// whose set to apply.
type State struct {
	State string `json:"state"`
}

// Reset is the body of the state operation: the name of the state to reset
// to, and the explicit confirmation a reset requires, since it reverts
// every migration set and drops every row before reapplying them.
type Reset struct {
	State   string `json:"state"`
	Confirm bool   `json:"confirm"`
}
