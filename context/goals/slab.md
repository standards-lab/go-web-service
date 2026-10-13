# goal · slab

- **State:** idle
- **Task:** none
- **Branch:** none

## Tasks

1. [x] commands
2. [x] scenarios

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

- scenarios: the unchecked needs (postgres, azurite, the observability profile) fold into two nodes. "service" probes /readyz and names `mise run serve` when nothing answers or `mise run db:up` on a 503; "grafana up" covers otel:up. Rejected: one node per need, which would need a postgres DSN setting.
- scenarios: a scenario declares only the need nodes and the settings its steps read (repo, no color); the need nodes' values carry the base and Grafana URLs. Listing reads e.g. "uses service, grafana up, repo, no color".
- scenarios: the tempo setting (--tempo, SLAB_TEMPO) is removed, since nothing reads it once demo goes. Revises the commands decision that kept every root flag.
- scenarios: narration blocks become render functions in output, printed through go-cli-sdk's Show. Rejected: a new slab package; a styled Reporter in go-cli-sdk.
- scenarios: a block's caption prints as a plain Note before its Show block, so captions lose their color.
- scenarios: tested at the App seam via apptest with httptest servers. Rejected: a cli.Run harness in the scenarios package (steps take *cli.Invocation, which only a dispatch fills).
- scenarios: style.JSON also returns input that ends inside an open object or array unchanged, since Go's Decoder.Token reports end of input, not an error, for a document cut between tokens.
- scenarios: go-cli-sdk's Reporter prints no blank line around Show, so the HTTP scenarios print an empty Note before each request and response block. The trace pointer's caption is a plain "Observability", and the sweep's tick is a plain Note.
- scenarios: the need nodes' values (Service and Grafana, each with Start) live in the scenarios package, because scenarios.Nodes names them and scenarios can't import internal/app; internal/app defines the nodes. Service carries the HTTP client the root builds from the client node, since nothing below the root constructs a dependency (standards review).
- scenarios: a need node's Start returns ctx.Err() when the run ended during its probe, so a sibling cancelled by another need's failure isn't reported as a second failure.
- scenarios: "grafana up" declares After(service), so with both down the run reports the service failure (`mise run serve`). After orders only and builds nothing, so no "uses" line changes.
- scenarios: the scenarios are built once in New and keep their step state in a struct across runs. go-cli-sdk v0.2.0's Scenario takes a fixed Steps slice with no per-run hook, and each run's first step overwrites what later steps read.
- scenarios: repo.Root and repo.FS take the override string. The env package is deleted, since the setting nodes read SLAB_* through go-core's config.EnvName. httpx's Live and GrafanaLive gave way to Probe, ProbeGrafana, and StatusError in probe.go.
- scenarios: the summaries drop "(needs ...)" and "(no compose stack needed)", which the "uses" line replaces. storage's run-time branch prefix stays `storage-demo-`: it is a request body, and changing it is out of scope.
- scenarios (redirect): SQL blocks also dim `--`/`--|` comment lines and color sqlate's `{{…}}` markers, since the comments and markers are most of what sqlate teaches.
- scenarios (redirect): SQL, JSON, and table blocks get a blank line before and after, as the old runner printed, revising the flush spacing BUILD chose.
- scenarios (redirect): captions stay plain notes; a caption inside its block would sit at the block's indent.
- scenarios (redirect): an unclosed `{{` is plain code, so it doesn't hide a trailing `--` comment after it.
- scenarios (redirect): a `--` inside a quoted string counts as a comment, since the repository's statements bind values rather than quote them.
- scenarios (redirect): the trace pointer keeps the request's spacing, a blank line before it and none after.
- scenarios (redirect): at the App seam only the storage "Seeded" table's trailing blank line is distinguishable, since go-cli-sdk's reporter collapses consecutive blank lines; the other blocks' spacing is asserted on the output rather than pinned to `show`.

## Pending edits

- coordinator · context/cli-applications.md:
  - "The layout and conventions": replace "The slab goal relies on two of them:" with "slab follows them, including these two:".
  - "Where the tools differ": replace the lead "The record of the cobra-era tools, which the standard replaces:" with "The record of the tools that came before the standard. slab now runs on go-cli-sdk v0.2.0, with a composition root modeled on go-cli-sdk-template's, and follows the scenario convention: its root-level `scenarios` package runs on go-cli-sdk's `scenario` runner as `slab scenario <name>`, and the root's and the `scenario` parent's help both end with the listing. blobfs and clutch stay on cobra:". The "Signal context" and "Infrastructure lifetime" entries stay as they are.
  - "Where the tools differ", "Scenarios": replace the entry with "**Scenarios:** clutch keeps both its runner and its scenarios in `scenario`, with a plain reporter over `output`."
  - "Where the tools differ", "Presentation flags": append "slab reads color from its `no color` node (`--no-color`, `SLAB_NO_COLOR`) and from `NO_COLOR`, and colors only the blocks a scenario step prints through Show, never a heading, a note, or a caption."
  - "Role interfaces" table, Starter row: append ", slab's scenarios.Service and scenarios.Grafana" to "Implemented by". They are the need nodes' values, and the Coordinator starts them before a scenario's first step.
  - "The plan", the slab entry: replace "**slab** aligns in its own `slab` goal, adopting the conventions in "The layout and conventions"." with "**slab** follows the conventions in "The layout and conventions"."
  - The architecture's CLI applications page names no tool, so it needs no edit.
