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
