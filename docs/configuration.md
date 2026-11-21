# Configuration

Configuration layers in a fixed precedence, later sources winning:

1. `config.json`, the base file, every knob at its default.
2. `config.<APP_ENV>.json`, the environment overlay; `mise.toml` sets `APP_ENV=local`, which
   activates the committed `config.local.json` (loopback host, debug logging, the `default`
   seed set, and a `sweep.outbox_age` and `sweep.inbox_age` of `2m`, so the local outbox
   reclaimer has work).
3. `secrets.json` and `secrets.<APP_ENV>.json`, gitignored secret layers; the database password
   and the object store's key go here.
4. `APP_*` environment variables, the final override: the log, server, and shutdown variables,
   the `APP_DATABASE_*` family (`APP_DATABASE_HOST`, `APP_DATABASE_NAME`,
   `APP_DATABASE_USER`, `APP_DATABASE_PASSWORD`, `APP_DATABASE_PORT`, and the pool settings),
   the `APP_STORAGE_*` family (below), the `APP_MESSAGING_*` and `APP_NATS_*` families
   (below), `APP_OBSERVABILITY_ENDPOINT` and
   `APP_OBSERVABILITY_SAMPLE_RATIO`, the rate limit (`APP_RATE_LIMIT_REQUESTS`,
   `APP_RATE_LIMIT_WINDOW`), the reads paging policy
   (`APP_READS_DEFAULT_SIZE`, `APP_READS_MAX_SIZE`), the admin seed set (`APP_ADMIN_SEED`, a
   state name), and the sweep's interval, pass size, stale age, and retention ages
   (`APP_SWEEP_INTERVAL`, `APP_SWEEP_BATCH`, `APP_SWEEP_STALE_AGE`, `APP_SWEEP_OUTBOX_AGE`,
   `APP_SWEEP_INBOX_AGE`). An empty value counts as unset, as it does for every
   override, so only a file's `"seed": ""` clears a configured seed set.

Every file is optional: a deployment can run on the base file and environment variables alone,
or on environment variables only. Each file decodes strictly: a key the configuration does not
declare fails the load.

The `storage` block is go-storage's, each setting with its override:

| Setting | Variable | Default |
|---------|----------|---------|
| `endpoint` | `APP_STORAGE_ENDPOINT` | none, the account's public service URL; the `local` overlay names Azurite's `http://127.0.0.1:10000/devstoreaccount1` |
| `container` | `APP_STORAGE_CONTAINER` | `go-web-service` (`config.json`), required |
| `account` | `APP_STORAGE_ACCOUNT` | none, required; the `local` overlay names `devstoreaccount1` |
| `key` | `APP_STORAGE_KEY` | none, required; a secret, and `secrets.example.json` carries Azurite's published development key |
| `max_object_size` | `APP_STORAGE_MAX_OBJECT_SIZE` | 10485760 bytes, 10 MiB (`config.json`); 0 is unbounded |
| `list_page_size` | `APP_STORAGE_LIST_PAGE_SIZE` | 0, the provider's own page size |
| `request_timeout` | `APP_STORAGE_REQUEST_TIMEOUT` | `3s` (`config.json`; go-storage's own is `10s`), bounding the store's own startup check and readiness probe, so `/readyz` answers within it when the store is down; the object operations take no bound from it |
| `read_idle_timeout` | `APP_STORAGE_READ_IDLE_TIMEOUT` | `30s`, bounding each read of a download's body from the store while the read is in progress, so a store that stops sending is cut off and a slow client never is |
| `options.<key>` | `APP_STORAGE_OPTIONS_<KEY>` | azureblob's own settings: `try_timeout`, `max_retries`, `block_size`, and `concurrency`, described below |

azureblob's `try_timeout` bounds each try of one object operation, never a whole transfer: a
`Put` block's upload, or the part of a `Get`'s body read within the try. A download's body
resumes past a try's deadline, so it runs as long as its client reads. `max_retries` sets how
many times a failed try is retried, and how many times a download's body resumes per read.
`config.json` keeps the SDK's 3, with exponential backoff from 800 ms, and sets `try_timeout` to
`5s`, below `read_idle_timeout`, so a stalled try resumes once before the store is cut off. A
store that stalls on every try is refused after about 26s (four tries and the backoff), inside
the server's 30s `write_timeout`, so the 503 reaches the client; the base configuration's test
holds that budget. The `local` overlay and the integration harness set `max_retries` to `1`, so
a store that is down is refused in about a second. `block_size` and `concurrency` size a
`Put`'s staged blocks; their product, the body azureblob reads ahead of the store, must be at
least `max_object_size`, so a store that stalls never slows the body's reads into a 408.

The server's `read_timeout` and `write_timeout` are `30s` (`config.json`, go-web-sdk's
defaults), sized for a request that moves no large body. An upload or a download sets its own
connection deadlines through go-web-sdk's `Transfer`: the timeout plus the time the body takes at
`transfer_rate` (`APP_SERVER_TRANSFER_RATE`, 65536 bytes per second), the slowest pace a client
is allowed. The body's size counts up to the route's limit (10 MiB for a document, 1 MiB for a
logo). An upload slower than that pace answers 408; a download slower than that ends short.

The `sweep` block schedules the [sweep](sweep.md): `interval` (`APP_SWEEP_INTERVAL`, `30s`), the
backstop wake; `batch` (`APP_SWEEP_BATCH`, 100), the records one pass handles; `stale_age`
(`APP_SWEEP_STALE_AGE`, `1h`), the age past which a pass reclaims an unfinished upload or delete;
`outbox_age` (`APP_SWEEP_OUTBOX_AGE`, `168h`, 7 days), the age past which a pass deletes a
published outbox row; and `inbox_age` (`APP_SWEEP_INBOX_AGE`, `168h`), the age past which a pass
deletes a handled inbox row, which must outlast every redelivery the broker can make. The interval
and the batch apply to every reclaimer, and a non-positive age fails startup, naming the key.

The `messaging` block is go-messaging's: `source` (`APP_MESSAGING_SOURCE`, `web-service` in
`config.json`, required), the CloudEvents source of every event the service emits;
`relay_poll` (`APP_MESSAGING_RELAY_POLL`, `250ms`), the relay's wait between passes once the
outbox is empty; and `relay_timeout` (`APP_MESSAGING_RELAY_TIMEOUT`, `10s`), the bound on one
publish. The `nats` block is go-messaging's NATS provider's: `url` (`APP_NATS_URL`,
`nats://127.0.0.1:4222` in `config.json`), `name` (`APP_NATS_NAME`, the connection's name),
`stream` (`APP_NATS_STREAM`, `WEB_SERVICE`), `prefix` (`APP_NATS_PREFIX`, `web-service`, the
namespace of the service's event types), `owner` (`APP_NATS_OWNER`, `true`: the service creates
or updates its stream on start), and, on the owner only, `duplicates` (`APP_NATS_DUPLICATES`,
the stream's deduplication window) and `max_age` (`APP_NATS_MAX_AGE`, its retention, 0 keeping
every event). An invalid block fails startup, naming it.

The compose file honors `POSTGRES_PORT` for the Postgres container's published port,
`AZURITE_BLOB_PORT` for Azurite's, and `NATS_PORT` for NATS's, but the service does not read
them: moving a port off its default splits the two until the matching `APP_DATABASE_PORT`,
`APP_STORAGE_ENDPOINT`, or `APP_NATS_URL` variable follows. Postgres's user, password, and database (`app` each) are baked into
`compose/postgres/Dockerfile`; they pair with `config.json` and `secrets.example.json`, so change
both sides together.
