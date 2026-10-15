// Package outboxtest reads the events a command writes through
// go-messaging's Postgres outbox, as sqlate's scripted driver records the
// outbox's inserts, for the unit tier of every package that emits. It
// imports no provider: a test builds the outbox's sink itself and stamps
// it here.
//
// The package exports:
//
//   - [Stamped], a recorder over a sink that stamps every event with
//     [Source], [ID], and [Time]
//   - [IsWrite] and [Writes]: the outbox's inserts among the recorded
//     calls
//   - [Decode]: the event an insert wrote
//   - [Committed] and [NoneCommitted]: the events whose transaction
//     committed, a rolled-back insert never among them
//   - [Want]: an event's type, subject, stamp, and exact data
package outboxtest
