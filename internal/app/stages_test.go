package app

import "testing"

// The stage table ascends in the process's dependency order. The schema's
// stage is go-database's constant, so a release that moved it below the
// pool's or onto a later stage would reorder startup silently; this fails
// instead.
func TestStages_Ascend(t *testing.T) {
	stages := []struct {
		name  string
		stage int
	}{
		{"infrastructure", stageInfrastructure},
		{"schema", stageSchema},
		{"verify", stageVerify},
		{"reactors", stageReactors},
		{"root", stageRoot},
	}
	for i := 1; i < len(stages); i++ {
		if prev, cur := stages[i-1], stages[i]; cur.stage <= prev.stage {
			t.Errorf("stage %s (%d) does not follow %s (%d)", cur.name, cur.stage, prev.name, prev.stage)
		}
	}
}
