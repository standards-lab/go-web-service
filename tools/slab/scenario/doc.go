// Package scenario defines what a slab scenario is, the reporter it narrates
// through, the cobra command that runs one, and the listing that prints them.
//
// A scenario is an ordered list of steps. Each step has an intent sentence
// (the narrated heading), an action, and what the action prints through the
// [Reporter] it is given. A step reports through one of two channels: a
// request and its response, or the same plus a trace pointer. The trace
// pointer names Grafana's Explore URL, the service, and the trace id read
// from X-Request-Id. It is not a deep link, because a deep link's encoded
// pane state is longer and no easier to follow.
//
// A [Need] states one precondition and the mise task that satisfies it, and
// is checked before the first step. A Check that only reads the run
// configuration can be a bare function reference such as httpx.Live.
//
// There is no registry: [Command] builds one cobra command from a scenario a
// caller hands it, the same way a domain package's Commands is called.
package scenario
