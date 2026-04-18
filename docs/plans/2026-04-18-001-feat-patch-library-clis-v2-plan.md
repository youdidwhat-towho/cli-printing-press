---
title: "feat(cli): printing-press patch v2 — AST injection + rollout to 21 library CLIs"
type: feat
status: draft
date: 2026-04-18
supersedes: 2026-04-17-003-feat-patch-library-clis-plan.md
---

# feat(cli): `printing-press patch` v2 — AST injection + rollout to 21 library CLIs

## What changed from v1

The v1 plan proposed re-rendering `root.go` from current templates. Document review surfaced four P0 findings:
1. 10 of 21 CLIs have no spec on disk (plan-driven, sniff-driven, internal-yaml, graphql-sdl)
2. Re-rendering would silently delete novel `AddCommand` lines on synthetic CLIs like ESPN (25 novel commands) — `go build` doesn't catch orphaned-but-unreferenced files
3. `feedback` resource collision with Pagliacci's spec-derived `newFeedbackCmd`
4. Missing `internal/cliutil/*` and `agent_context.go` in older CLIs would break re-rendered root.go imports

The AST-injection pivot makes all four disappear. A prototype (`/tmp/ast-prototype/`, ~270 LOC using `github.com/dave/dst`) was validated against ESPN: produced a buildable binary with all 25 novel commands intact plus the three new ones (`profile`, `feedback`, plus `--profile` / `--deliver` flags). No spec read, no template re-render, no module-path rewrite, no `VisionSet`/`Narrative`/`PromotedCommands` reconstruction.

## Goal

Ship `printing-press patch <library-cli-dir>` and apply it to all 21 CLIs in `~/Code/printing-press-library/library/` so PR #218's three compounding features (`--profile`, `--deliver`, `feedback`) land without a full reprint. One PR per category against the library repo.

## Non-goals

- **Async `--wait` / `jobs` deliberately out of scope.** That unit re-renders per-endpoint command files, which requires a spec. For the 10 spec-less CLIs this is impossible, and for the 11 spec-present ones, the per-endpoint mutation is large enough that a targeted reprint is cheaper and safer. Address via reprint when a specific CLI needs async.
- **Scorecard re-run not included.** Scorecard is orthogonal (machine-only change from PR #218); it runs correctly against any library CLI already.
- **README untouched.** Some CLIs have hand-curated READMEs with agent-hints; re-rendering risks loss. SKILL.md is also left untouched for the same reason.
- **No backfill of existing manifests.** The separate `fix(cli): publish archives spec` PR handles *future* publishes.

## Architecture

### AST injection (automated via `dst`)

Applied to `internal/cli/root.go` in each library CLI:

| Target | Mutation | Idempotency check |
|---|---|---|
| `rootFlags` struct | Append 4 fields: `profileName string`, `deliverSpec string`, `deliverBuf *bytes.Buffer`, `deliverSink DeliverSink` | Skip if field already present (by name) |
| Import block | Add `bytes`, `io`, `os` (whichever are absent) | Skip each import if already present |
| `Execute()` body | Insert 2 `PersistentFlags().StringVar` calls after the last existing flag registration | Skip if `--profile` already registered |
| `PersistentPreRunE` body | Insert deliver-setup block + profile-lookup block at the top of the function body | Skip if `flags.deliverSpec` reference already present |
| `Execute()` body | Insert 2 `rootCmd.AddCommand(...)` calls (`newProfileCmd`, `newFeedbackCmd`) after the last existing AddCommand | Skip if `newProfileCmd` already registered |
| Post-`rootCmd.Execute()` | Insert deliverBuf flush block before final `return err` | Skip if `Deliver(` already called in function |

All insertion points located via `dst.Inspect` with positional matching (last `PersistentFlags` call, last `AddCommand` call, function body of named function). Run `goimports` post-pass to sort the new imports.

### Drop-in files (rendered from templates, substituting only `{{.Name}}` and `{{envName .Name}}`)

- `internal/cli/profile.go` — 328 LOC, self-contained, uses `{{.Name}}` only
- `internal/cli/deliver.go` — 113 LOC, self-contained, uses `{{.Name}}` only
- `internal/cli/feedback.go` — 222 LOC, self-contained, uses `{{.Name}}` and `{{envName .Name}}`

`{{.Name}}` comes from `.printing-press.json`'s `api_name` field. `currentYear` hard-coded to the current year. No other template variables needed (these templates do not reference `modulePath`, `Owner`, `VisionSet`, or any spec-derived data).

### Collision detection (refuse to patch)

Before any write, scan the target CLI for conflicts. Fail loudly with an actionable message:

| Conflict | Signal | Example case |
|---|---|---|
| `feedback` resource in spec | `internal/cli/feedback.go` already exists | Pagliacci (has Pagliacci-specific feedback API) |
| `profile` resource in spec | `internal/cli/profile.go` already exists | hypothetical |
| `deliver` resource in spec | `internal/cli/deliver.go` already exists | hypothetical |
| `jobs` resource in spec | `internal/cli/jobs.go` already exists | N/A currently, but async unit adds one |
| `--profile` / `--deliver` flag already registered | AST scan | Already-patched CLI |
| Custom `rootFlags` fields named `profileName`/`deliverSpec`/`deliverBuf`/`deliverSink` | AST scan | Already-patched CLI (→ skip silently) |

Resource-level collisions are **fatal**. Flag-level collisions are **idempotent** (patcher has already run — no-op).

## Implementation

### Layout

```
internal/patch/
  patch.go          ~200 LOC   public API (Patch, Options, Report)
  ast_inject.go     ~250 LOC   dst-based injection routines
  ast_inject_test.go ~200 LOC  golden tests for each mutation
  dropins.go        ~80 LOC    render profile.go, deliver.go, feedback.go
  dropins_test.go   ~60 LOC    verify rendered content
  collisions.go     ~50 LOC    detect + report conflicts
  collisions_test.go ~80 LOC   per-collision-type test
cmd/printing-press/
  patch.go          ~60 LOC    cobra subcommand, flag plumbing
```

### `Patch(opts)` flow

```go
type Options struct {
  Dir       string  // CLI root, e.g. ~/.../library/productivity/cal-com
  DryRun    bool    // print file list + diffs, write nothing
  Force     bool    // overwrite even if collisions detected
  SkipBuild bool    // skip post-patch `go build` gate
}

type Report struct {
  Dir            string
  Name           string   // from .printing-press.json
  FilesCreated   []string
  FilesModified  []string
  Collisions     []Collision  // empty on success
  BuildOK        bool
  BuildOutput    string       // captured on failure
}

func Patch(opts Options) (*Report, error)
```

Steps:
1. Read `.printing-press.json` → `api_name`, `cli_name`
2. Read existing `internal/cli/root.go` — refuse if missing
3. Parse root.go with `decorator.Parse`
4. Detect collisions (resource-level → fatal unless `--force`)
5. Apply AST mutations (all idempotent — safe to re-run)
6. Write patched root.go
7. Render drop-ins (profile.go, deliver.go, feedback.go) — skip any that already exist
8. `gofmt -w internal/cli/` + `goimports -w internal/cli/root.go`
9. Unless `--skip-build`: `go build ./...` in the CLI dir, capture output
10. Return `Report`

### Public CLI surface

```
printing-press patch <library-cli-dir> [flags]
  --dry-run     Show what would change without writing
  --force       Overwrite on resource-level collision
  --skip-build  Skip post-patch `go build` gate
  --json        Emit Report as JSON (for batch scripting)
```

## Rollout: apply to 21 library CLIs

### Phase 1: build the patcher

Single PR against `cli-printing-press`. Tests cover:
- Golden root.go patched for a simple-shape CLI (cal-com fixture)
- Golden root.go patched for a synthetic CLI (ESPN fixture — asserts all 25 novel AddCommand lines preserved)
- Collision detected for a spec-with-feedback-resource fixture (Pagliacci-shaped)
- Idempotency: second run is a no-op (reports 0 modifications)
- `--dry-run` writes nothing

### Phase 2: dry-run audit across all 21 CLIs

```bash
for dir in ~/Code/printing-press-library/library/*/*/; do
  printing-press patch "$dir" --dry-run --json > /tmp/patch-dryrun/$(basename "$dir").json
done
```

Inspect results. Expected outcomes:
- **~19 CLIs**: clean patch (3 files created, 1 modified, 0 collisions)
- **~1 CLI**: resource-level collision (Pagliacci's `feedback`). Mark for manual review.
- **~1 CLI**: may expose an unexpected root.go shape (very old version). Mark for reprint.

Do not proceed until dry-run is clean across the intended cohort.

### Phase 3: apply by category, one PR each

Against `printing-press-library`. Order chosen for low-to-high stakes:

| PR | Category | CLIs | Why this order |
|---|---|---|---|
| 1 | `other/` + `devices/` + `social-and-messaging/` | weather-goat (empty cats filtered out) | Smallest blast radius, rehearsal |
| 2 | `developer-tools/` | agent-capture, postman-explore, trigger-dev | Self-contained, easy to validate |
| 3 | `media-and-entertainment/` | archive-is, espn, hackernews, movie-goat, steam-web | Largest novel-command surface — prove AST preserves them |
| 4 | `food-and-dining/` | pagliacci-pizza (manual if collision), recipe-goat | Exercises collision handling |
| 5 | `commerce/` | dominos-pp-cli, instacart, yahoo-finance | |
| 6 | `marketing/` + `travel/` + `payments/` + `monitoring/` | dub, flightgoat, kalshi | |
| 7 | `productivity/` + `project-management/` + `sales-and-crm/` | cal-com, slack, linear, hubspot | Largest command surfaces (cal-com has 30+), save for last |

Per-CLI validation before committing each batch:
```bash
printing-press patch <cli-dir> --json | tee /tmp/patch-report.json
# Requires BuildOK: true, FilesCreated: 3, FilesModified: 1
<cli-binary> --help | grep -E "^\s+(profile|feedback)\s"   # new commands present
<cli-binary> --help | grep -E "^\s+--(profile|deliver)\s"  # new flags present
<cli-binary> profile list                                  # smoke test
<cli-binary> feedback --help                               # smoke test
```

Commit message per batch:
```
feat(cli): apply PR #218 patch — profile, deliver, feedback (<category>)

Applied via printing-press patch on <date>. No reprint.
CLIs: <list>
```

### Phase 4: edge-case handling

- **Pagliacci's `feedback` collision**: skip the drop-in, AST-inject only. Document in the commit message. The CLI gets `--profile` / `--deliver` but not the `feedback` subcommand (because its own `feedback` command takes the name).
- **Version-skew CLIs (`dominos-pp-cli` at `0.4.0`, `agent-capture` at `1.0.x`)**: dry-run first; if the AST shape doesn't match (e.g., different `rootFlags` structure), mark for reprint instead of hand-patching.
- **Build failures post-patch**: revert the working tree in that CLI's directory and mark for reprint. Do not ship a half-patched CLI.

### Phase 5: follow-up (separate PR)

`fix(cli): generate archives plan/sniff source spec` — closes the 10-CLI spec gap forward so future patch/emboss runs have a spec to work from. Out of scope for this rollout but worth landing soon.

## Resolved concerns from v1 review

| v1 concern | Status under v2 |
|---|---|
| No spec on disk (P0) | Not needed — AST patch reads only `.printing-press.json` |
| Novel commands silently deleted (P0) | Impossible — AST-insert doesn't touch existing AddCommand lines; prototype verified on ESPN |
| Feedback resource collision (P0) | First-class: collision detection refuses to patch, user resolves manually |
| Missing cliutil/agent_context (P0) | Not touched — patch doesn't reference them |
| HasAsyncJobs build failure (P1) | Gone — async unit dropped from scope |
| Non-goal contradiction (P1) | Resolved — patch truly does only PR #218's three features, no template re-render |
| Shipcheck bypass (P1) | `go build` is the gate; scorecard unchanged; verify per-CLI via `--help` smoke test |
| ModuleOverride complexity (P1) | Gone — no imports touched, existing module path preserved trivially |
| SKILL.md narrative loss (P1) | Gone — SKILL.md not in scope |
| Identity shift / immutability (P1) | Addressed — patched CLIs remain a single base run; no provenance rewrite |
| Hash-based idempotency brittleness (P2) | Gone — idempotency via AST presence checks, not content hashes |
| RenderSubset over-abstraction (P2) | Gone — no re-render |
| Adoption: who runs patch going forward (P2) | Explicit ownership: backfill is one-shot via this rollout; future features that qualify for patch will include patch runs as part of their PR |

## Risks

- **AST patch fails on an unexpected root.go shape** — mitigation: `--dry-run` first; idempotency checks return no-op rather than guessing; refuse to patch if `rootCmd.PersistentFlags()` block not found
- **`goimports` reorders imports in ways that produce a noisy diff** — mitigation: acceptable; diff noise is a one-time cost
- **Per-CLI build failures surface unknown dependencies** — mitigation: each CLI batched separately; failed ones marked for reprint, not bulk-forced
- **Pagliacci's collision case is not the only one** — mitigation: Phase 2 dry-run exposes all collisions before any writes; adjust per-CLI plan accordingly

## Estimated scope

- `internal/patch/*`: ~900 LOC total (implementation + tests)
- `cmd/printing-press/patch.go`: ~60 LOC
- One PR against `cli-printing-press`, roughly half the size of PR #218
- Rollout: 7 PRs against `printing-press-library`, each touching 1-5 CLIs
