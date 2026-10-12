# goal · slab

- **State:** building
- **Task:** scenarios
- **Branch:** scenarios

## Tasks

1. [x] commands
2. [ ] scenarios

## Task brief · slab · scenarios
Problem       slab's scenarios run on its own runner as `slab demo <name>` and are
              listed by `slab list`, against the scenario convention. Its needs print
              as narration instead of failing at start. style.JSON returns partly
              colored text on input that breaks partway. The tempo setting has no
              reader once demo goes.
Behaviors     1. `slab scenario <name>` runs sqlate, domain, problems, and storage on
                 go-cli-sdk's runner, with the same steps and intents. Headings and
                 notes are plain. Request, response, SQL, JSON, table, and trace blocks
                 print through Show, colored only on a terminal and only when none of
                 --no-color, SLAB_NO_COLOR, NO_COLOR is set.
              2. A block's caption prints as a plain note before its block.
              3. The "service" node probes the service's readiness endpoint at start.
                 Nothing answering fails the run before any step, naming the service
                 node and `mise run serve`; a 503 names `mise run db:up`. Exit 1.
              4. The "grafana up" node probes Grafana's health at start; failure fails
                 the run before any step, naming the node and `mise run otel:up`. Exit 1.
              5. Each scenario declares only the nodes its steps read, and its listing
                 "uses" line names them: need nodes first, then the settings it reads
                 (repo, no color). sqlate names no need node; domain and problems name
                 service and grafana up; storage names service.
              6. `slab scenario` alone prints its help and exits 2. That help and the
                 root's help both end with the same listing.
              7. `slab list`, `slab demo`, `slab scenario <unknown>`, and
                 `slab scenario sqlate extra` exit 2 as usage errors.
              8. A failed step is reported once on stderr as
                 "slab scenario <name>: step i (intent): <err>", exit 1, and no step
                 runs after it.
              9. --tempo and SLAB_TEMPO no longer exist; `slab --tempo x org list`
                 exits 2 (unknown flag).
              10. style.JSON returns input that breaks after its first token unchanged,
                  with no partial color.
              11. slab's README and usage, go-web-service's README, and the slab mise
                  task description name `slab scenario <name>`. Outside the CHANGELOG's
                  history no `slab demo` or `slab list` remains; the CHANGELOG's
                  Unreleased section records the move and the removed tempo setting.
Test seams    The App, through apptest: dispatching argument lists, with the service and
              Grafana as httptest servers reached through SLAB_BASE and SLAB_GRAFANA.
              style.JSON's own unit tests for behavior 10.
Slices        1. style.JSON returns input that breaks partway unchanged.
                 Demo: its test on a document cut after the first key.
              2. The scenarios package on go-cli-sdk's runner, sqlate first; narration
                 blocks become output render functions passed to Show, captions as
                 notes; demo stays mounted. Demo: `slab scenario sqlate`.
              3. The service and grafana up need nodes; domain, problems, and storage
                 move onto them, steps reading node values in place of env and
                 repo.FS(ctx). Demo: `slab scenario domain` with nothing running fails
                 at start naming service and `mise run serve`.
              4. demo, list, slab's own runner, env.Env, and the tempo setting deleted;
                 the root's Footer becomes the listing. Demo: `slab list` exits 2; both
                 help screens end with the listing.
              5. Documentation: slab's README and usage, go-web-service's README, the
                 mise task description, the CHANGELOG. Demo: grep finds no `slab demo`
                 or `slab list` outside the CHANGELOG's history.
Out of scope  Any go-cli-sdk change; folding slab into the root module; per-scenario
              flags; probing postgres or azurite directly; changing any scenario's
              steps, intents, or requests; colored headings or notes.
Door          two-way: slab is an unreleased dev tool in this repository, no tag is
              pushed, and a revert restores `slab demo`, `slab list`, and --tempo.

## Progress

slices 4/5 committed · standards — · spec — · editor —

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

## Pending edits

- coordinator · context/cli-applications.md, "Where the tools differ": slab is no longer a cobra-era tool. Say slab runs on go-cli-sdk v0.2.0 with a composition root modeled on the template's, and follows the scenario convention (root-level `scenarios` package on go-cli-sdk's `scenario` runner, `slab scenario <name>`, listed in the root's and the `scenario` parent's help). Remove slab from the "Scenarios" entry (clutch alone remains). In "Presentation flags", slab reads color from its `no color` node (`--no-color`, `SLAB_NO_COLOR`) plus `NO_COLOR`, and colors only Show blocks. "Signal context" and "Infrastructure lifetime" slab entries stay. The architecture principle names no tool; no edit there.
