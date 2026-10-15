package app

// NewSweepMetrics exposes newSweepMetrics to the external test package, so
// the sweeper's instruments are tested under a manual reader.
var NewSweepMetrics = newSweepMetrics
