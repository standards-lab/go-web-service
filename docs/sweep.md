# Sweep

The sweep reclaims what the service leaves behind. It is go-database's general sweeper
(`sweep.Sweeper`), one node, `sweeper`, that runs three reclaimers, each a library's own:

- `files` is blobfs's sweep. It finishes the rows that a recursive directory delete, a write
  that stops partway, and a delete that stops partway leave for later: a branch marked deleting,
  a pending upload, a file whose delete began. It removes each file's object and then its row,
  and each directory once it is empty, and reclaims the uploads and deletes left unfinished past
  `sweep.stale_age` (an hour). Each recursive delete nudges it, so a marked branch is swept at
  once.
- `outbox` is go-messaging's outbox retention. It deletes the published outbox rows published
  more than `sweep.outbox_age` (7 days) ago; an unpublished or quarantined row is never deleted.
- `inbox` is go-messaging's inbox retention. It deletes the inbox rows handled more than
  `sweep.inbox_age` (7 days) ago.

Every reclaimer shares one policy: it is due every `sweep.interval` (30 seconds) and at
startup, so a branch marked before a restart is swept at the next start, and each of its
passes handles at most `sweep.batch` (100) records. A due reclaimer runs a cycle: passes while
a pass reports more, for at most one interval, after which it yields to the reclaimers behind
it and stays due, so one large backlog never starves the others. A cycle first claims the
session advisory lock `sweep.<name>` (`sweep.files`, `sweep.outbox`, `sweep.inbox`) without
waiting; a reclaimer whose lock another replica holds skips the cycle, so two replicas on one
database never run the same reclaimer's cycle at once. Each pass holds the quiesce gate shared,
so a schema verb never meets one. A pass the store or the database refuses never ends its cycle
or fails the service: every pass is idempotent, so a later pass finds what it left. Once the
service begins to drain, the sweeper finishes the pass in flight and runs no further one, whatever
remains, so a large backlog never holds the drain; the next start finds what is left.

The sweep is a Reactor in the architecture's sense, one that dispatches to no Domain Service: the
composition root runs it on a reactor, the process's one runner for work driven by an
occurrence for the process lifetime. `/readyz` reports it as the `sweeper` check; a backlog never
makes the service unready.

A `files` pass that did work or was refused is logged at info as `"sweep pass"`, with the
files, directories, and stale rows it removed and whether it reported more, so a refused pass
logs its counts even when they are all zero; its error is in the warn record below. A pass that
failed, any reclaimer's, is logged at warn as `"sweep pass failed"`, with the reclaimer, whether
it reported more, its duration, and the error. A cycle whose claim fails, such as on a lost
connection, runs no pass and is logged at warn as `"sweep claim failed"`, with the reclaimer and
the error. Two metrics report the passes:

| Metric | Kind | Attributes | Meaning |
|--------|------|------------|---------|
| `sweep.passes` | counter | `sweep.reclaimer` (`files`, `outbox`, `inbox`), `sweep.outcome` (`drained`, `more`, `error`) | The passes run; `error` when the pass failed, whatever it reported, `more` when it reported more, `drained` otherwise |
| `sweep.backlogged` | gauge | `sweep.reclaimer` | 1 while the reclaimer's last pass reported more, else 0 |

The collector writes them to Mimir under Prometheus names, labelled `job="go-web-service"`:
`sweep.passes` is `sweep_passes_total`, by `sweep_reclaimer` and `sweep_outcome`, and
`sweep.backlogged` is `sweep_backlogged_ratio`, by `sweep_reclaimer`, since a gauge whose unit is
`1` gains `_ratio`. `mise run slab -- scenario sweeper` reads both there.
