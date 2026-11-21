# Admin

The database admin service is mounted under `/admin/database`. Every endpoint triggers the same
library function startup runs; the schema verbs answer with the resulting schema status.

| Method | Path | What it does |
|--------|------|--------------|
| `GET` | `/admin/database/diagnostics` | Ping latency, server version, pool counters |
| `GET` | `/admin/database/schema` | Each migration set's history against its embedded migrations |
| `GET` | `/admin/database/patterns` | The pattern catalog |
| `GET` | `/admin/database/statements` | Every domain's compiled statements |
| `GET` | `/admin/database/states` | The named states the service declares |
| `POST` | `/admin/database/schema/verify` | Verify every set's history and every statement |
| `POST` | `/admin/database/schema/up` | Apply every set's pending migrations |
| `POST` | `/admin/database/schema/down` | Revert a set's migrations (`{"set": "app", "steps": 1}`, one by default) |
| `POST` | `/admin/database/schema/steps` | Apply or revert a set's `{"set": "app", "steps": n}`, negative to revert |
| `POST` | `/admin/database/schema/force` | Set a set's history to `{"set": "app", "version": v}` without running a file |
| `POST` | `/admin/database/seed` | Apply the configured set, or `{"state": "…"}`, over what is there (403 with no set) |
| `POST` | `/admin/database/state` | Reset to `{"state": "…", "confirm": true}`: revert every set, apply them, seed the state (the configured set when `state` is empty; 403 with none) |

The schema is three migration sets, run in declaration order: `messaging`, go-messaging's outbox
and inbox; `blobfs`, blobfs's own tables; and `app`, the service's (`data/migrations`), whose
migrations reference the tables beneath. The
status reports each set; `up` and `verify` act on every set, and `down`, `steps`, and `force`
name the set they act on, refused with 400 when it is missing or undeclared.

A named state is one file under `data/seeds/`, keyed by each domain's seed contribution:
`organizations`, `logos`, and `documents`. `default` is the reference tree, a logo for each
organization, and acme's document tree; `empty` is nothing. The files a state names sit under
`data/seeds/fixtures/`. `admin.seed` names the state the service seeds at every start, not only
the first; of the checked-in configurations, only the local overlay names one (`default`). A
seed applies idempotently: it leaves a row or file that exists as it is and writes again any the
state names that is missing, so a seeded organization, logo, or file deleted since the last
start is restored at the next. The seed's response, and the state's `seeded` member, count what
the run stored under the same keys. The rows commit in one transaction first, and the files are
written after it commits, since a file's object is put outside any transaction. Each entity the
seed creates commits its command's event, and a rerun that finds everything there emits nothing
([Messaging](messaging.md#the-event-convention)).

The state operation is destructive, in the class of `down` and `force`, and
`mise run db:state <state>` runs it against the local service. A reset reverts the messaging
set too, dropping any event still pending in the outbox, and commits one
`web-service.database.reset` before it seeds. It reverts blobfs's tables but leaves
the objects in the container, which is accepted in development: a seeded file carries
a fixed id (the `5eed…` ids in the state file), so the reseed writes it again under the same key
and replaces the object left there.

The verbs that change the schema (`up`, `down`, `steps`, and the state reset) share a quiesce
gate with the sweep and the relay: a verb waits for a sweep pass or a relayed row in flight to
finish, and neither starts another until the verb is done. Without the gate, a pass and a reset would deadlock on the
document root's owner table. The gate is per process: with several replicas, another replica's
sweep can still meet a reset, which is a development operation.

The object storage admin service is mounted under `/admin/storage`:

| Method | Path | What it does |
|--------|------|--------------|
| `GET` | `/admin/storage/diagnostics` | Whether a live probe succeeds, the configured container, the provider's longest key |
| `POST` | `/admin/storage/container` | Create the configured container if it is missing, then answer with the diagnostics |

A store that cannot be reached answers 503 with the provider's reason. The admin mount serves on
the API listener until the management listener lands; it is not for a public deployment as it
stands.

A container deleted while the service runs makes every storage operation a 503, a download's
read included, and takes the storage check on `/readyz` down; the rows are untouched. `POST
/admin/storage/container` creates it again, empty, and readiness recovers, but each stored
file's row now names an object that no longer exists, and its download answers 404. In
development, a reset to a seeded state (`mise run db:state default`) writes the seeded files
again; anything else is uploaded again. The service does not reconcile rows against the store:
blobfs defers that reconciler until an orphaned object is observed or a consumer stores at
volume, and it would join the [sweep](sweep.md) as a reclaimer.
