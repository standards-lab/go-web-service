// Package database is the database admin domain: the HTTP half of the
// database admin service, a route group under the admin mount over
// go-database's admin.Service, whose operations are the library functions
// startup runs. The admin mount is the /admin route group, the
// administrative counterpart of /api; an admin domain is a package under
// admin/ named for the infrastructure service it administers. The mount
// serves on the API listener until the planned management listener gives
// it an authenticated listener of its own.
//
// A state reset emits one event, [StateReset], through the messaging
// runtime's recorder, after the reset's apply and before its seed, so it
// precedes the seed's creation events on the stream; it emits none for
// the entities the revert drops. The schema verbs emit nothing, and the
// seed verb emits only its creations' events, which the domains raise.
//
// The package's API:
//
//   - [Routes]: the route group, its schema-changing verbs holding a
//     [SchemaGate] exclusively.
//   - [Steps], [Force], [State], and [Reset]: the verbs' request bodies.
//   - [ErrUnconfirmed]: a reset whose body does not confirm it, a 400.
//   - [StateReset]: the event a state reset emits, its data the state's
//     name.
//   - [OnReset], over a [Recorder]: the go-database reset hook that
//     commits StateReset, which the composition root wires.
package database
