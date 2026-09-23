// Package scenario defines what a slab scenario is, the reporter it narrates
// through, the cobra command that runs one, and the listing that prints them.
//
// A scenario is an ordered list of steps, each an intent sentence (the
// narrated heading), an action, and what the action prints through the
// [Reporter] it is given. There are two observation channels: a request and
// its response, and the same plus a trace pointer, which names Grafana's
// Explore URL, the service, and the trace id read off X-Request-Id rather
// than a deep link, whose encoded pane state is longer and no easier to
// follow. A [Need] states one precondition and the mise task that satisfies
// it, checked before the first step; a Check that only reads the run
// configuration can be a bare function reference such as httpx.Live.
//
// There is no registry: [Command] builds one cobra command from a scenario a
// caller hands it, the same way a domain package's Commands is called.
package scenario
