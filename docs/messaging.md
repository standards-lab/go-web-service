# Messaging

The service publishes events through [go-messaging](https://github.com/standards-lab/go-messaging)'s
runtime: a command writes its event into the Postgres outbox in its own transaction, and the
relay publishes the outbox's committed rows to NATS JetStream. In the graph (`internal/app`)
the broker is a node in the connections' layer, the runtime a node the domains Use for its
recorder, and the relay a reactor after the schema; `/readyz` reports the `broker` and
`relay` checks. With NATS away the service is unready and keeps running, its events waiting in
the outbox. The `messaging` and `nats` blocks under [Configuration](configuration.md) set it up.

The service owns the `WEB_SERVICE` stream: its broker creates or updates it at start, capturing
every subject under the prefix `web-service`. Every other client, slab and the integration
harness among them, binds the stream as it finds it and writes nothing. An event's NATS subject
is its type, unchanged, so the prefix is the namespace of every type the service emits and a
consumer subscribes by type; the CloudEvents `subject` attribute is the entity the event is
about, such as the file's id. An event's type and data are public API, as a route's path and
body are: a consumer in another service binds both.

The relay holds the quiesce gate shared for each row, as the sweep does for a pass, so a schema
verb runs with it paused ([Admin](admin.md)). The drain runs the server before the relay, whose
last pass publishes what was committed while the server drained; [Adding a
service](../README.md#adding-a-service) gives the rule a new producer follows.

## The event convention

- **One event per command.** Every public command that changes state commits exactly one event,
  in the transaction that makes that state true, so the event commits exactly when the state
  does. A command that fails commits none, and so does a retry of a delete already under way: a
  repeated file delete, which finds the file deleting already, or a repeated branch mark, which
  finds the directory deleting already, even when it marks a straggler. A read commits none, and
  neither do the sweep's reclamation of a marked branch or an abandoned write and the purges that
  finish a delete.
- **Seeds emit.** Every seed write that creates an entity commits the event its command
  commits, with the same data, in the transaction that creates it: an organization's
  `organization.created`, a logo's `organization.logo.uploaded` in its activation's, a
  directory's `document.directory.created`, and a file's `document.file.uploaded` in its
  completion's. A rerun over a seeded schema creates nothing and emits nothing. An
  organization's document root is created implicitly by its first write under the root alias, a
  client's or a seed's, and that write's transaction commits the root's
  `document.directory.created`; a write that finds the root, or loses the race to create it,
  commits no root event.
- **A reset emits once.** A state reset commits one `web-service.database.reset` after its
  revert and apply and before its seed, so it reaches the stream ahead of the seed's creation
  events; a consumer that holds a copy of the service's entities discards it there and rebuilds
  from what follows. The revert drops the outbox with every other table, so a reset loses the
  events still unpublished when it runs, and it emits nothing for the entities it drops. The
  schema verbs (`up`, `down`, `steps`, `force`) emit nothing.
- **Naming.** An operation on the domain itself is `<service>.<domain>.<operation>`, as
  `web-service.organization.created` is; an operation on a sub-element of the domain is
  `<service>.<domain>.<element>.<operation>`, as `web-service.document.file.uploaded` and
  `web-service.organization.logo.uploaded` are. The operation is a past-tense verb, and no type
  carries a version suffix before v1.

## The event catalog

Each event is a CloudEvent whose `source` is the configured `messaging.source` (`web-service`),
whose `subject` is the id of the entity its type names, and whose data is the JSON below. The
data carries an entity's stored state after the command, under the API's field names, never a
file's content, and never the state before: a move or a transfer carries only the new parent.
It carries no composed path, status, or timestamp. The types are defined in each
domain's `messaging.go` and their data in its `entities.go`.

| Type | Emitted by | Subject | Data |
|------|------------|---------|------|
| `web-service.organization.created` | `POST /api/organizations`; a seed's organization | organization id | `organization_id`, `parent_id` (null at the top), `code`, `name`, `version` |
| `web-service.organization.edited` | `PUT /api/organizations/{id}` | organization id | as `created`, as the edit left it |
| `web-service.organization.transferred` | `POST /api/organizations/{id}/transfer` | organization id | as `created`, under the new parent |
| `web-service.organization.deleted` | `DELETE /api/organizations/{id}` | organization id | `organization_id`, `version` (the version the row was removed at) |
| `web-service.organization.logo.uploaded` | `PUT /api/organizations/{id}/logo`; a seed's logo | logo's file id | `file_id`, `organization_id`, `content_type`, `size`, `etag`, `version` |
| `web-service.organization.logo.deleted` | `DELETE /api/organizations/{id}/logo` | logo's file id | `file_id`, `organization_id`, `version` (the version its release left it at, deleting) |
| `web-service.document.directory.created` | `POST /directories`; the document root's implicit creation; a seed's directory | directory id | `directory_id`, `organization_id`, `parent_id`, `name`, `version`; the root's `parent_id` is null and its `name` is `/` |
| `web-service.document.directory.moved` | `POST /directories/{id}/move` | directory id | as `created`, as the move left it |
| `web-service.document.directory.deleted` | `DELETE /directories/{id}` (an empty directory's removal) and `DELETE /directories/{id}?recursive=true` (a branch's mark, for the marked directory alone) | directory id | `directory_id`, `organization_id`, `version` (the removed row's, or the version the mark left it at) |
| `web-service.document.file.uploaded` | `POST /directories/{id}/files`; a seed's file | file id | `file_id`, `organization_id`, `directory_id`, `name`, `content_type`, `size`, `etag`, `version` |
| `web-service.document.file.moved` | `POST /files/{id}/move` | file id | as `uploaded`, as the move left it; `size` and `etag` are null for a pending file, whose write has not completed |
| `web-service.document.file.deleted` | `DELETE /files/{id}` | file id | `file_id`, `organization_id`, `version` (the version the delete left it at, deleting) |
| `web-service.database.reset` | `POST /admin/database/state` | the state's name | the state's name, a JSON string (`"empty"`) |

The document routes sit under `/api/documents/{org}`. A logo `PUT` that replaces a logo commits
`logo.uploaded` alone, no `logo.deleted` for the logo it replaces.

## The file protocols' hooks

A command whose change becomes true inside one of blobfs's file protocols passes the protocol a
hook, which runs in the transaction that makes the change true, where the domain commits its
event. A hook's error rolls that transaction back, so the change and the event commit together
or not at all.

- `Store.WriteFile`, blobfs's two-phase write, with `WriteFileRequest.OnComplete`: the hook runs
  in the completion's transaction once the row is available, and never on a refused completion.
  The document upload runs it.
- `Store.EnsureFile`, the two-phase write's retry-safe form under a fixed id, with
  `EnsureFileRequest.OnComplete`: the hook runs as `WriteFile`'s does, and only when this call
  completes the file; a file found complete, or completed by another writer, runs none. The
  document seed's files run it.
- `Store.RemoveFile`, blobfs's two-phase delete, with `RemoveOptions.OnDelete`: the hook runs in
  the delete's first transaction once the row is deleting; a retry that finds the row deleting
  already skips it and finishes the purge, and the purge runs no hook. The document file delete
  runs it.

The logo needs none: its write and its seed complete through blobfs's own protocols, and its
event commits in the activation's transaction that follows.

## Adding an event

1. **Define the kind** in the domain's `messaging.go` with go-core's `event.Define`, its doc
   comment stating when it commits and what its subject is (`document.FileUploaded`). Its data
   type sits in `entities.go` beside the read shapes, under the API's field names, and carries
   the entity's metadata, never its content (`document.FileUploadedData`). Name the type by
   [the convention](#the-event-convention), list both in the package doc, and add the type's row
   to [the catalog](#the-event-catalog).
2. **Emit it in the committing transaction.** The domain's store holds the `Recorder` the
   composition root injects (the runtime's, `domain.go`); the command calls the recorder's `Emit`
   on the transaction that changes the state and raises the kind there with its subject
   (`emitUploaded`). The write is one more statement on that transaction, so an event that fails
   to write fails the command. A command whose state becomes true inside a blobfs file protocol
   passes it a hook and emits there ([The file protocols'
   hooks](#the-file-protocols-hooks)). A seed write that creates an entity emits as its
   command does. A domain service runs in the server's requests and needs no graph edge to the
   relay; a producer outside a request Uses the relay's node ([Adding a
   service](../README.md#adding-a-service)).
3. **Test it.** In the unit tier the domain's service is built over sqlate's scripted driver
   with go-messaging's recorder over its Postgres outbox, its id and time fixed (`recorder` in
   `domain/document/database_test.go`, stamped by `internal/outboxtest`). The outbox insert is
   one recorded statement, so a test asserts it runs once, between the completion and its
   commit, decodes it to check the type, subject, and data (`outboxtest.Decode` and
   `outboxtest.Want`), and scripts a refused insert to prove the command rolls back with no
   event committed (`outboxtest.Committed`; `domain/document/messaging_test.go`). In the
   integration tier, `integration.Subscribe` binds the stream for the kind's type before the
   command runs and returns a `Stream`, and
   `Stream.Await` waits for the event about a subject. Absence cannot be awaited, so a test
   proves a command emits nothing with a marker: after the command it runs one that does emit,
   awaits the marker's event, and asserts it arrived alone, since the relay publishes in commit
   order (`marker` and `onlyMarkers` in `integration/events_test.go`).
4. **Watch it** with `mise run slab -- events watch`, which prints each event as it arrives;
   `--from-start` replays the stream first. `mise run slab -- scenario events`, run in another
   terminal, runs one command for each event type, so the watcher prints every payload. See
   [`tools/slab/README.md`](../tools/slab/README.md).
