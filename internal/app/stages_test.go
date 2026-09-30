package app

import "testing"

// The stage table ascends in the process's dependency order, so an edit
// that reorders it fails here rather than reordering startup silently.
func TestStages_Ascend(t *testing.T) {
	stages := []struct {
		name  string
		stage int
	}{
		{"infrastructure", stageInfrastructure},
		{"schema", stageSchema},
		{"reactors", stageReactors},
		{"root", stageRoot},
	}
	for i := 1; i < len(stages); i++ {
		if prev, cur := stages[i-1], stages[i]; cur.stage <= prev.stage {
			t.Errorf("stage %s (%d) does not follow %s (%d)", cur.name, cur.stage, prev.name, prev.stage)
		}
	}
}
