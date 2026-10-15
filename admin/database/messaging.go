package database

import (
	"context"

	"github.com/standards-lab/go-core/event"
	"github.com/standards-lab/sqlate"
)

// StateReset is the event a state reset emits, once per reset, its subject
// the name of the state the reset applied and its data that name, a JSON
// string. A reset reverts every set, which drops every row and every
// unpublished outbox row, so it emits no event per entity it removes: a
// consumer that holds a copy of the service's entities discards them on
// StateReset, and the seed's own creation events, which follow it on the
// stream, rebuild them. The schema verbs (up, down, steps, force) emit
// nothing, nor does the seed verb beyond its creations' events. Its type and its data are public API: a
// consumer in another service binds both.
var StateReset = event.Define[string]("web-service.database.reset")

// Recorder is the recorder a reset emits its event through: go-messaging's
// Runtime.Recorder, which the composition root injects.
type Recorder = event.Recorder[*sqlate.Tx]

// OnReset returns the hook the composition root sets as go-database's
// admin.Options.OnReset: it commits StateReset for the state in a
// transaction of its own on db, which holds the outbox write alone. The
// admin service runs the hook after the reset's apply and before its seed,
// so the event commits ahead of every seed event on the stream, and a
// failed write stops the reset before it seeds.
func OnReset(db *sqlate.DB, events *Recorder) func(ctx context.Context, state string) error {
	return func(ctx context.Context, state string) error {
		_, err := db.Transact(ctx, events.Emit(ctx, func(_ *sqlate.Tx, q *event.Queue) (struct{}, error) {
			StateReset.Raise(q, state, state)
			return struct{}{}, nil
		}))
		return err
	}
}
