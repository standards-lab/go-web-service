package database

// Steps is the body of the down and steps operations: how many migrations
// to revert, or to apply when positive and revert when negative.
type Steps struct {
	Steps int `json:"steps"`
}

// Force is the body of the force operation: the version the history is set
// to, 0 emptying it.
type Force struct {
	Version int `json:"version"`
}
