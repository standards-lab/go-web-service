# goal · harness.adoption-go-web-service

- **State:** building
- **Task:** adopt
- **Branch:** adopt

## Tasks

1. [ ] adopt

## Task brief · adopt

**Problem**

go-web-service doesn't follow marathon 0.22.0's harness: no pin, a check that runs neither lint, a
README five times its budget, a context note over budget, an unmarked capability map, about 2,600
comment lines outside the five kinds, one pointer to a note that doesn't exist, and trailing
dependencies.

**Behaviors**
1. Currency reports nothing trailing. PostgreSQL is 19.0-alpine for development and integration.
   CHANGELOG [Unreleased] gets one line for the root module's requirement changes. The check and
   the integration tier pass.
2. The marathon tools are pinned at 0.22.0 with mise's automatic install off, and currency reports
   a newer marathon tag.
3. The check runs both modules' Go steps and sqlint, then lint-context and lint-references from the
   root, running both before failing; a missing lint prints `FAIL: <lint> is not on PATH; run mise
   install`; the description names everything it runs. CI installs the mise tools first.
4. golangci-lint requires every suppression to name its linter and give a reason (nolintlint with
   require-explanation and require-specific), covering both modules.
5. lint-context passes: the root README keeps its overview and links docs/ pages holding the moved
   sections, content unchanged; the domain-architecture note's reasoning with no other home moves
   to a docs/ page; map entries are a name, `(built)` or `(planned)`, and a link to their home.
6. Every pointer to a moved section, in Go comments, context, STANDARDS, and compose's README,
   names its new page. The management-listener entry points at the coordinator's
   `roadmap/v1.admin-listener.toml`.
7. Every comment in both modules' Go source and tests, scripts, and all configuration is one of the
   five kinds: unexported and test-function doc comments go; test step labels become names; the
   currency script's sections become named functions; configuration headers are a short
   description plus `See` lines; reasoning moves into exported docs, package docs, names, or docs/
   pages and is not lost. SQL headers stay. Behavior is unchanged.
8. STANDARDS' comment rule covers source, tests, scripts, SQL headers, and every configuration
   file. Its README-home lines name the README and its docs/ pages.

**Test seams**
- The check (both lints, golangci-lint), currency, and the integration tier.

**Slices**
1. Upgrade: dependencies, actions, images, PostgreSQL 19, the CHANGELOG line.
2. Context and README: docs/ pages, the capability map, domain-architecture, pointer fixes.
3. Pin and check: the pin, the lint loop, the suppression rule.
4. Configuration, scripts, and the STANDARDS comment rule.
5. Root module Go source comments.
6. Root module Go test comments, the integration package included.
7. slab module Go source comments.
8. slab module Go test comments.

**Out of scope**

v1.repository-docs' docs/ guide; adopting `web.NewClient`; moving to sqlate's `pgtest`; budgets for
compose's and slab's READMEs; a Markdown link check; coordinator and architecture edits (pending
edits); a release.

**Door**

Two-way. Nothing is tagged; the changes stay under Unreleased.

## Progress

slices 0/8 committed · standards — · spec — · editor —

## Decisions

None.

## Pending edits

- architecture · standards/go-elemental/principles/release-and-ci.md:141: "its README documents"
  the port exception becomes the Configuration docs page.
- coordinator · roadmap/response-caching.toml:4 ("go-web-service's README, Stack") and
  roadmap/web-service-port-settings.toml:4 ("Its README documents") name their new docs pages.
