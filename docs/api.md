# API

The organization domain is mounted under `/api`:

| Method | Path | What it does |
|--------|------|--------------|
| `GET` | `/api/organizations` | List organizations (paged, filtered, sorted) |
| `GET` | `/api/organizations/{id}` | Find one by id |
| `GET` | `/api/organizations/lookup?path=/acme/engineering` | Find one by hierarchical path |
| `POST` | `/api/organizations` | Create an organization |
| `PUT` | `/api/organizations/{id}` | Replace its code and name |
| `POST` | `/api/organizations/{id}/transfer` | Move it under a new parent (`parent_id`, null for the root) |
| `DELETE` | `/api/organizations/{id}` | Delete an organization |
| `PUT` | `/api/organizations/{id}/logo` | Store its logo (raw body), replacing any |
| `GET` | `/api/organizations/{id}/logo` | Read its logo's bytes |
| `DELETE` | `/api/organizations/{id}/logo` | Delete its logo |

The list takes `page`, `size`, and `sort` (`sort=-path,code`), and every other parameter as a
filter on a field of the read model: `code=acme` is equality, `code=acme&code=finance` is
membership, and `name[like]=%25ing` names an operator in brackets. Its envelope carries the
filtered `total`, `more`, and a `next` cursor. Sending `cursor=<next>` in place of `page`, with
the same sort and filters, continues after the last row, so a tree that changes between
requests neither skips nor repeats one. A continued page omits `page`; a sort on `parent_id`,
the one nullable field, pages by number alone. The total is counted in the page's own
statement, from the rows it returns, so a page past the last, which returns none, omits `total`
too; its `more` is false. The document listings follow the same contract.

The guarded commands (`PUT`, transfer, `DELETE`) take the row's version in `If-Match: "3"`; a
missing header answers 428 and a stale version 412. Every rejection is an RFC 9457 problem. A
conflict, 409, carries a curated `detail` naming its kind and never the underlying error's
text: "the request conflicts with the current state" for a taken code, a missing parent, a
cycle, a concurrent logo replacement, and a delete of an organization that still has children,
a logo, or a document root, and "the file is being deleted" for a logo `PUT` whose new file's
delete began before its activation. A seeded organization holds a logo, so its delete answers
409 until its logo is deleted.

The logo is a raw body of at most 1 MiB in a raster type, PNG, JPEG, WebP, or GIF; any other
type, SVG included, answers 415. A `PUT` answers 201 with the logo's `Location` and
`{"id": …}`, the new file's id, and carries no version. It answers 201 whether or not it
replaced a logo, since every `PUT` stores a new file. The logo routes take no `If-Match`: the
last write wins, and a 409, not a precondition, tells apart two replacements racing to
activate. The `GET` is served `Cache-Control: no-cache` with the object's `ETag` and
`Last-Modified`, so a client revalidates on every use. Once a replacement or a delete commits,
the service purges the old file's object and row. If the purge fails, the service logs it at
warn and the request still succeeds (201 or 204); the file stays deleting until the sweep's stale
reclaim.

```sh
curl localhost:8080/api/organizations                          # the seeded reference data, paged
curl 'localhost:8080/api/organizations/lookup?path=/acme/engineering'  # lookup by path
```

The document domain is mounted under `/api/documents/{org}`, each organization's hierarchy of
directories and files over blobfs and the object store. A directory id may be `root`, the
organization's document root, which its first write creates. Until then, `root`'s listings
answer an empty page, or 404 for an organization that does not exist; like any listing, they
refuse a bad sort, filter, or cursor with 400:

| Method | Path | What it does |
|--------|------|--------------|
| `POST` | `/directories` | Create a directory (`parent_id`, `name`) |
| `GET` | `/directories/{id}` | Read a directory with its path and `status` (`active` or `deleting`) |
| `GET` | `/directories/{id}/directories` | List its child directories (paged, filtered, sorted) |
| `GET` | `/directories/{id}/files` | List its files, pending and available (paged, filtered, sorted) |
| `DELETE` | `/directories/{id}` | Delete an empty directory (204), or with `?recursive=true` its branch (202) |
| `POST` | `/directories/{id}/move` | Move it under a new parent within the root |
| `POST` | `/directories/{id}/files?name={name}` | Upload a file (raw body, at most 10 MiB) |
| `GET` | `/files/{id}` | Read a file's metadata |
| `GET` | `/files/{id}/content` | Download an available file, as an attachment |
| `DELETE` | `/files/{id}` | Delete a file |
| `POST` | `/files/{id}/move` | Move it into a directory within the root |

- An upload is a `POST` to its directory's files, with the file's name in the required `name`
  parameter. It answers 201 with the file's metadata read as its `Location`. It creates a new
  file under an id the server mints, so it is not idempotent: a retry after a lost response
  answers 409 on the name the first took.
- An upload the store refuses while it is unreachable answers 503. If the store also refuses
  the abandon's object delete, the file is left deleting, hidden from the listings, and keeps
  its name until the sweep's stale reclaim (`sweep.stale_age`); an upload of that name
  meanwhile answers 409 "the file is being deleted". An upload whose body ends before its
  declared length answers 400, and one whose body arrives slower than `server.transfer_rate`
  allows answers 408, since the request is at fault, never 503.
- The moves and the deletes take the row's version in `If-Match`: 428 when it is missing, 412
  when it is stale.
- A recursive delete marks the branch deleting and answers 202 with the directory's read as its
  `Location`; the [sweep](sweep.md) then removes the rows and their objects. Until it finishes,
  the directory reads `deleting`, its listings answer 404, and a write into it answers 409 "the
  directory is being deleted". A repeated recursive delete answers 202 and nudges the sweep
  again. A recursive delete of `root` removes the document root, and the next write creates a
  new one.
- Every conflict carries one of six curated details: "an entry with that name already exists",
  "the directory is not empty", "the directory is being deleted", "the file is being deleted",
  "the file is referenced", or "the request conflicts with the current state". A move of a file
  whose own delete began, in a directory that is not deleting, answers "the file is being
  deleted"; a file in a marked branch, or a move into one, answers the directory's detail.

An id outside the organization's document root answers 404, as an absent one does, so no
request reads, moves, or deletes across organizations. A download is served as
`Content-Disposition: attachment` with `Cache-Control: private, no-cache`, never rendered
inline, since stored HTML would run in the API's origin. A download whose object cannot be
opened answers its problem with `Cache-Control: no-store` and none of the file's headers.

Two validators share HTTP's entity-tag syntax, the version and the object ETag, and neither
stands in for the other:

- The version is a row's body field in every read and command response, never an `ETag`
  header. A client quotes the version it read as the `If-Match` of a guarded command (`"3"`).
- The object ETag is the object store's tag for a download's or a logo's bytes, served as
  `ETag` with `Last-Modified` beside it; a conditional `GET` naming either (`If-None-Match`,
  `If-Modified-Since`) answers 304. The object ETag is not a version, and `If-Match` refuses a
  download's as malformed.
