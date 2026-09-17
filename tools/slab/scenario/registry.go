package scenario

// registry holds every registered scenario in registration order. Each
// capability's package registers its scenarios from an init function; the
// command tree reads them through All and Lookup.
var registry []Scenario

// Add registers s. Registering a name twice panics, since the command tree
// would otherwise carry two subcommands that answer to the same word.
func Add(s Scenario) {
	if _, dup := Lookup(s.Name); dup {
		panic("scenario: " + s.Name + " registered twice")
	}
	registry = append(registry, s)
}

// All returns the registered scenarios in registration order.
func All() []Scenario {
	out := make([]Scenario, len(registry))
	copy(out, registry)
	return out
}

// Lookup returns the scenario registered under name.
func Lookup(name string) (Scenario, bool) {
	for _, s := range registry {
		if s.Name == name {
			return s, true
		}
	}
	return Scenario{}, false
}
