package database

// Down is the body of the schema down command, the shape of
// admin/database's Steps for down: the migration set to revert, and how
// many of its migrations, omitted so the service reverts one.
type Down struct {
	Set   string `json:"set"`
	Steps int    `json:"steps,omitempty"`
}

// Steps is the body of the schema steps command, the shape of
// admin/database's Steps: the migration set, and how many of its
// migrations to apply when positive or revert when negative. Zero is sent
// as given, for the service to refuse.
type Steps struct {
	Set   string `json:"set"`
	Steps int    `json:"steps"`
}

// Force is the body of the schema force command, the shape of
// admin/database's Force: the migration set, and the version its history
// is set to, 0 emptying it.
type Force struct {
	Set     string `json:"set"`
	Version int    `json:"version"`
}

// State is the optional body of the seed command, the shape of
// admin/database's State: the name of the state whose set to apply.
type State struct {
	State string `json:"state"`
}

// Reset is the body of the state command, the shape of admin/database's
// Reset: the name of the state to reset to, and the confirmation the
// service requires, sent as given so an unconfirmed reset is the service's
// to refuse.
type Reset struct {
	State   string `json:"state"`
	Confirm bool   `json:"confirm"`
}
