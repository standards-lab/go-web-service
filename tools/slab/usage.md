# Usage

A walkthrough of `slab`'s commands, run by hand against the real running stack. Each block is
copyable as written; a few need a value from the previous block's output substituted in (marked
`<...>`).

## Bring up the stack

```sh
cd /path/to/go-web-service
mise run stack-up
mise run serve &   # or run it in another terminal
```

## `org` — the organization domain

```sh
mise run slab -- org list
```

```sh
# grab acme's id from the list above, then:
mise run slab -- org get <acme-id>
```

```sh
mise run slab -- org get-by-path acme/engineering
```

```sh
# the logo: store, read back, revalidate, replace, remove
mise run slab -- org logo put <acme-id> acme.png                   # 201, the new file's id
mise run slab -- org logo get <acme-id> --out /tmp/acme.png        # headers; bytes to the file
mise run slab -- org logo get <acme-id> --if-none-match '"<etag>"' # 304 Not Modified
mise run slab -- org logo put <acme-id> other.png                  # replaces; the old file is retired
mise run slab -- org logo put <acme-id> logo.svg                   # 415: not an accepted type
mise run slab -- org logo delete <acme-id>                         # 204; then get is 404
```

```sh
mise run slab -- org create --code sales --name "Sales" --parent-id <acme-id>
```

```sh
# use the id/version create just returned:
mise run slab -- org edit <new-id> --version 1 --code sales --name "Sales and Marketing"
```

```sh
# --body's embedded "version" stands in for --version:
mise run slab -- org edit <new-id> --body '{"code":"sales","name":"Sales & Marketing","version":2}'
```

```sh
mise run slab -- org transfer <new-id> --version 3 --parent-id=   # moves it to root
```

```sh
mise run slab -- org delete <new-id> --version 4   # prints "204 No Content"
```

```sh
# query grammar:
mise run slab -- org list --filter 'code[like]=%e%' --size 2 --page 1 --sort code
```

```sh
# a cursor walk: each page prints "next"; pass it back with the same sort to continue
mise run slab -- org list --size 3 --sort -path                      # page 1, total 7, more, next
mise run slab -- org list --size 3 --sort -path --cursor <next>      # no page number
mise run slab -- org list --page 2 --cursor <next>                   # cobra usage error: exclusive
```

```sh
# the --body/flags mutual exclusion, expect a cobra usage error:
mise run slab -- org create --code x --name y --body '{}'
```

## `docs` — the document domain

Every command takes the organization's id first; a directory is its id or `root`. The
organization's document root is created by its first write.

```sh
mise run slab -- docs dirs create <acme-id> --parent-id root --name reports       # 201, the new directory's id and version
mise run slab -- docs dirs get <acme-id> root                                  # the root, its path "/", status "active"
mise run slab -- docs dirs list <acme-id> root --sort name                     # the root's child directories
```

```sh
# upload, list, read back, revalidate:
mise run slab -- docs files put <acme-id> <reports-id> q3.pdf                  # 201 at version 2; stored as q3.pdf, application/pdf
mise run slab -- docs files put <acme-id> <reports-id> notes --name "Q3 notes" # no extension: application/octet-stream
mise run slab -- docs files list <acme-id> <reports-id> --size 1               # page 1 and next; continue with --cursor <next>
mise run slab -- docs files show <acme-id> <file-id>                           # the metadata: status, size, version
mise run slab -- docs files get <acme-id> <file-id> --out /tmp/q3.pdf          # headers, Content-Disposition among them
mise run slab -- docs files get <acme-id> <file-id> --if-none-match '"<etag>"' # 304 Not Modified
```

```sh
# moves take the version they guard on, as the flag or inside --body:
mise run slab -- docs files move <acme-id> <file-id> --version 2 --directory-id <reports-id> --name q3-final.pdf
mise run slab -- docs dirs move <acme-id> <reports-id> --body '{"parent_id":"root","name":"archive","version":1}'
```

```sh
# the deletes take the version they guard on, as org delete does:
mise run slab -- docs files delete <acme-id> <file-id>                         # cobra usage error: --version is required
mise run slab -- docs files delete <acme-id> <file-id> --version 2             # 412: the move left it at version 3
mise run slab -- docs files delete <acme-id> <file-id> --version 3             # 204
mise run slab -- docs dirs delete <acme-id> <reports-id> --version 2           # 409: the directory is not empty
```

```sh
# a recursive delete marks the branch and answers before the sweep removes it:
mise run slab -- docs dirs delete <acme-id> <reports-id> --version 2 --recursive   # 202, its Location
mise run slab -- docs dirs get <acme-id> <reports-id>                              # status "deleting", until the sweep is done
mise run slab -- docs dirs list <acme-id> <reports-id>                             # 404: a listing within a deleting branch
mise run slab -- docs dirs get <acme-id> <reports-id>                              # 404 once the sweep is done
```

```sh
# or wait for the sweep in the same command:
mise run slab -- docs dirs create <acme-id> --parent-id root --name scratch                # 201
mise run slab -- docs dirs delete <acme-id> <scratch-id> --version 1 --recursive --wait 30s   # 202, then 404
```

## `admin database` — the admin mount

```sh
mise run slab -- admin database schema status
mise run slab -- admin database diagnostics
mise run slab -- admin database patterns
mise run slab -- admin database statements
mise run slab -- admin database states
```

`schema status` reports each migration set in declaration order. `down`, `steps`, and `force`
act on one set, named with `--set`:

```sh
mise run slab -- admin database schema down --set app
mise run slab -- admin database schema status     # the app set: pending [1]
mise run slab -- admin database schema up          # restores it
mise run slab -- admin database schema verify
mise run slab -- admin database schema force --set app --version 1
mise run slab -- admin database schema down --set nope   # 400: the set is not declared
```

`seed` is additive — it inserts a set's rows onto the schema as it stands, never removing what's
already there, so `seed --state empty` onto a populated database changes nothing. `state` is what
actually clears first (revert every migration set, reapply them, then seed), and it requires
`--confirm`:

```sh
mise run slab -- admin database seed --state default
mise run slab -- admin database state --state empty             # 400: unconfirmed
mise run slab -- admin database state --state empty --confirm
mise run slab -- org list      # total: 0
mise run slab -- admin database state --state default --confirm
mise run slab -- org list      # total: 7 again
```

## `admin storage` — the object store

```sh
mise run slab -- admin storage diagnostics     # ready, the container, the key length bound
mise run slab -- admin storage container       # creates the container; succeeds when it exists
```

## `demo` — the narrated scenarios

```sh
mise run slab -- demo sqlate     # no stack needed, deterministic output
mise run slab -- demo domain
mise run slab -- demo problems
mise run slab -- list
```

## Clean up

```sh
kill %1   # or Ctrl-C the terminal running `mise run serve`
mise run stack-down
```
