# goal · slab

- **State:** building
- **Task:** commands
- **Branch:** commands

## Tasks

1. [ ] commands
2. [ ] scenarios

## Task brief · commands

```
## Task brief · slab · commands
Problem       slab is the last workspace tool on cobra, which breaks Go Elemental's
              no-frameworks line, and it doesn't follow the CLI composition root: an
              aggregate Config, no graph, every command reaching configuration, help
              exiting 0 and usage errors exiting 1. go-cli-sdk v0.2.0 and the
              template's root are released, so slab can take them now.
Behaviors     1. slab --help, a parent with no or an unknown subcommand, and every
                 usage error (missing argument, unknown flag, exclusive flags given
                 together, a missing required flag) exit 2 with go-cli-sdk's usage text.
              2. Every org, docs, and admin command keeps its words, arguments, and
                 flags, and sends the same request as today (method, path, query,
                 headers, body).
              3. A command builds only the nodes it declares: version builds nothing;
                 an org command builds the config and client nodes and nothing else.
              4. Configuration is graph nodes read from SLAB_* env (prefix SLAB), with
                 the root flags --base, --grafana, --tempo, --repo, --no-color
                 overriding it, accepted at any depth.
              5. A successful response prints as today: pretty JSON, colored only on a
                 terminal without --no-color or NO_COLOR, or the status line for an
                 empty body.
              6. A problem response exits 1 with one report on stderr,
                 "slab <path>: " followed by the status line, detail, instance, type,
                 and extras; stdout stays empty.
              7. slab version prints the main module's version, or (devel).
              8. demo <name> and list still run the four scenarios and print the
                 listing, on slab's own runner.
              9. cobra and pflag are gone from slab's module requirements.
Test seams    The App: New(cli.Streams) then Run(ctx, args) → exit code, with stdout
              and stderr captured, and apptest's Builds (and Halt) over its graph,
              against an httptest stub service.
Slices        0. Upgrade: go-core v0.8.0 and go-web-sdk v0.15.2 in both modules,
                 mise-action v5.1.2; done when `mise run currency` exits 0 and
                 `mise run check` passes.
              1. Root: the App on go-cli-sdk with config and lifecycle nodes, the
                 version command, apptest, and exit codes; slab --help and version
                 demoable.
              2. The organization commands on cli with Use, the flag-input helpers on
                 Invocation, and the problem report; org get against a stub demoable.
              3. The document commands; docs dirs/files demoable.
              4. The admin database and storage commands; admin database diagnostics
                 demoable.
              5. demo and list on cli over slab's own runner; cobra and pflag removed
                 from the module; the README's module reason and command docs updated.
Out of scope  The scenarios package, scenario <name>, Needs as nodes, help-footer
              listings, colored scenario blocks, the style.JSON fix (task scenarios);
              folding slab into the root module; any change to go-cli-sdk, go-core,
              or the template.
Door          Two-way: no release (go-web-service is untagged); the exit-code
              change (usage 1→2, help 0→2) reaches only slab's callers, and a revert
              restores it.
```

## Progress

slices 6/6 committed · standards ✓ · spec ✓ · editor ✓

## Decisions

- slab: two tasks, `commands` then `scenarios`. About 12k lines is more than one session, and demo and list stay on slab's own runner in between.
- commands: a problem response's error text carries every member (status line, detail, instance, type, extras) after "slab <path>: ", not go-web-sdk's one-line Problem.Error().
- commands: the root flags --base, --grafana, --tempo, --repo, and --no-color stay, overriding SLAB_* env.
- commands: tools/slab stays its own Go module, keeping go-cli-sdk, x/term, and x/sys out of cmd/server's module. Folding it into the root module would be a task of its own.
- commands: every root flag has a SLAB_* variable; SLAB_REPO and SLAB_NO_COLOR are new, and NO_COLOR still works through style.
- commands: the App and apptest come from the template; families not yet moved route to a temporary cobra bridge (legacy.go), which is removed in slice 5.
- commands: input refusals (body, version, query, --filter, logo media type) are checked in Validate, so they exit 2 as usage errors and build nothing; a read failure at Run exits 1.
- commands: the problem report is output.Report, which output.Expect returns for every problem response.
- commands: configuration is one node per setting (base, grafana, tempo, repo, no color), each read in its constructor from SLAB_* with the root flag overriding when set; no aggregate Config.
- commands: the App-seam family tests live in internal/app, so no root-level package imports internal/*.
- scenarios: Needs (service, Grafana) become nodes the scenario declares. Each checks it can reach its target at start and names the mise task. Rejected: a Needs field in go-cli-sdk, which would need a go-cli-sdk release first.
- scenarios: only the blocks a step prints through Show are colored, and headings and notes stay plain. Rejected: a styled Reporter in go-cli-sdk.
- commands: PLAN re-ran currency on 2026-10-12; go-core v0.8.0, go-web-sdk v0.15.2, mise-action v5.1.2 trail, all in slice 0; go-cli-sdk v0.2.0 is still latest, so the approved brief stands.
- scenarios: the backlog goal slab-json-partial-color folds in. style.JSON returns input that breaks after its first token unchanged, which fixes the code rather than the comment.
- commands: a bare parent (`org`, `docs`, `docs dirs`, `docs files`, `admin`, `admin database`, `admin storage`, `demo`) prints its help and exits 2, as go-cli-sdk does for requested help.
- commands: `demo` declares the five setting nodes (base, grafana, tempo, repo, no color) and no client node; each scenario builds its own HTTP clients from that environment, since it addresses the service, Grafana, and Tempo by name.
- commands: `slab demo <unknown>`, a missing scenario name, and an extra argument are usage errors (exit 2); a failed scenario step or need returns its error, reported once on stderr with exit 1.
- commands: `dirs delete --wait` refusals (without `--recursive`, a negative duration) are checked in Validate, so they are usage errors.
- commands: a missing `--version` reads go-cli-sdk's "required flag --version not set" on every command that needs it.
- commands: `slab -- list` runs `list`, as go-cli-sdk parses `--`.
- commands: an unreadable setting variable fails with the setting's node name and the variable, such as "no color: SLAB_NO_COLOR: ...".
- commands: CI pins jdx/mise-action by tag, so the upgrade keeps a tag (v5.1.2).

## Pending edits

- coordinator · context/cli-applications.md, "Where the tools differ": the section's lead calls its entries the record of the cobra-era tools, and slab is no longer one. Say that slab runs on go-cli-sdk v0.2.0 with a composition root modeled on the template's, and that its remaining difference is scenarios: slab keeps its own runner in `scenario` and the scenarios in `demo`, run as `slab demo <name>` and listed by `slab list`, not `scenario <name>` on go-cli-sdk's runner, until task `scenarios`. In "Presentation flags", slab reads color from its `no color` setting node (`--no-color`, `SLAB_NO_COLOR`) plus `NO_COLOR`. The slab entries under "Signal context" and "Infrastructure lifetime" stay true. The architecture's principles/cli-applications.md names no tool, so it needs no edit.
