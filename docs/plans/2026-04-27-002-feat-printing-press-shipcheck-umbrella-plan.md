---
title: Add `printing-press shipcheck` umbrella command + Phase 4 enforcement + polish-worker hook
type: feat
status: active
date: 2026-04-27
origin: docs/retros/ (none — origin is retro issue mvanhorn/cli-printing-press#351, WU-1)
---

# Add `printing-press shipcheck` umbrella + Phase 4 enforcement + polish-worker hook

## Overview

Adds a single `printing-press shipcheck` subcommand that runs all five verification legs (`dogfood`, `verify`, `workflow-verify`, `verify-skill`, `scorecard`) in sequence and propagates their exit codes through one umbrella verdict. Updates the `/printing-press` skill's Phase 4 prose to recommend the umbrella as the canonical shipcheck invocation. Updates the polish-worker agent so `verify-skill` runs in its diagnostic loop and gates `ship_recommendation`.

The five legs already exist and remain callable standalone — the umbrella is pure orchestration. Backwards compatibility is preserved for any operator or CI script that calls the legs directly today.

---

## Problem Frame

The retro for `company-goat` (issue mvanhorn/cli-printing-press#351, finding F1) traced a CI failure on PR #140 to a Phase 4 enforcement gap:

- The `/printing-press` skill's Phase 4 block lists five commands as a code block to run during shipcheck. An operator can copy three of them, run them, and conclude shipcheck passed — silently skipping `verify-skill` and `workflow-verify`. That is what happened on the run that produced #140; the public-library CI surfaced 14 `verify-skill` errors that local Phase 4 missed.
- The polish-worker agent's diagnostic loop runs `dogfood`, `verify`, and `scorecard`, but does not run `verify-skill` or `workflow-verify`. So polish does not catch the same class of failures even when the operator delegates to it.

The fix is to make skipping these legs structurally impossible during the local loop: a single umbrella command that is the canonical Phase 4 invocation, plus a polish-worker contract that runs `verify-skill` and gates ship recommendation on it.

---

## Requirements Trace

- R1. A single `printing-press shipcheck --dir <cli-dir> [--spec <path>] [...]` command runs `dogfood`, `verify`, `workflow-verify`, `verify-skill`, `scorecard` in sequence.
- R2. Umbrella exits 0 only when every leg exits 0. Exits non-zero when any leg fails, with the exit code reflecting the first failing leg.
- R3. Umbrella prints a per-leg verdict summary at the end so the operator can see which legs passed and which failed in one place.
- R4. Each leg's full output streams to the operator's terminal as it runs (no buffering); the summary appears below.
- R5. `--json` flag emits a structured envelope with per-leg verdicts for programmatic consumers.
- R6. Existing standalone leg commands (`printing-press dogfood`, `printing-press verify`, etc.) continue to work unchanged.
- R7. `/printing-press` skill Phase 4 prose recommends `shipcheck` as the canonical invocation; per-leg interpretation prose is preserved (still useful context).
- R8. `polish-worker` agent runs `verify-skill` in its diagnostic loop and hard-gates `ship_recommendation` on `verify-skill` exit 0.
- R9. Subprocess execution model: each leg is spawned as `exec.Command("printing-press", "<leg>", ...)`. Confirmed via design question; preserves stable ExitError propagation, real-time output, and ease of testing with stubs.

---

## Scope Boundaries

- This plan adds an orchestration layer only. **Out of scope:** rewriting any underlying check tool, changing what any leg does, or fixing scorer false positives that surface during the umbrella's run (those are retro WU-2 through WU-8).
- **Out of scope:** introducing new legs (e.g., MCP-coverage, secret-scan). If a future leg is wanted, it lands in a separate WU. The umbrella is designed to make adding a leg cheap, but no new leg ships in this WU.
- **Out of scope:** any change to how the legs invoke their own internals (e.g., `verify-skill`'s python script). The umbrella treats each leg as an opaque subprocess.
- **Out of scope:** WU-3 (auto-emit MCP tools for `extra_commands`). If U2 surfaces a clean place to add an MCP-coverage leg, note as follow-on but do not implement.
- **Out of scope:** parallelizing legs. Sequential execution keeps output readable, avoids races on the CLI's working directory between dogfood + verify, and matches operator expectations.

### Deferred to Follow-Up Work

- **MCP-coverage leg in shipcheck:** depends on WU-3 emitting MCP tools for `extra_commands`; today there is nothing meaningful to check. Add as a sixth leg once WU-3 lands.
- **Secret-scan leg in shipcheck:** today the publish skill runs scrubs at upload time. A pre-publish secret scan would be valuable but is its own WU.

---

## Context & Research

### Relevant Code and Patterns

- `internal/cli/root.go` — binary's command registration. Existing legs registered at lines 43–62 via `rootCmd.AddCommand(newXxxCmd())`. Pattern to mirror for `newShipcheckCmd()`.
- `internal/cli/dogfood.go`, `internal/cli/verify.go`, `internal/cli/workflow_verify.go`, `internal/cli/verify_skill.go`, `internal/cli/scorecard.go` — existing leg implementations. Confirmed flag surfaces:
  - `dogfood`: `--dir` (required), `--spec`, `--research-dir`, `--json`
  - `verify`: `--dir` (required), `--spec`, `--api-key`, `--env-var`, `--threshold`, `--fix`, `--max-iterations`, `--json`
  - `workflow-verify`: `--dir` (required), `--json`
  - `verify-skill`: `--dir` (required), `--only`, `--json`, `--strict`
  - `scorecard`: `--dir`, `--research-dir`, `--spec`, `--json`, `--live-check`, `--live-check-timeout`
- `internal/cli/exitcodes.go` — `ExitError` type with `Code int`, `Err error`, `Silent bool`. Use this for umbrella's own input-validation errors. Leg exit codes are propagated via subprocess exit codes, not `ExitError`.
- `internal/cli/verify_skill.go` line 95–117 — pattern for spawning a subprocess (`exec.Command`), forwarding stdout/stderr, and propagating the child's exit code through an `ExitError`. The umbrella will reuse this pattern, generalized across multiple subprocesses.
- `skills/printing-press/SKILL.md` lines 1724–1781 — the Phase 4 block to update. Includes the 5-command code block, per-leg interpretation prose, ship threshold list.
- `agents/polish-worker.md` — polish-worker contract. Phase 1 diagnostic block is at the top of the file under `## Phase 1: Baseline`; Categories table follows; output contract (`---POLISH-RESULT---` block with `ship_recommendation`) is later in the file.

### Institutional Learnings

- `verify-skill` ships its actual checker as an embedded python script (`internal/cli/verify_skill_bundled.py`) executed via `exec.Command("python3", ...)`. The pattern is a small Go shim that wraps an external process — the umbrella generalizes this further: a Go shim that wraps a sequence of `printing-press` self-invocations.
- The `exec.Command(os.Args[0], ...)` self-invocation pattern is standard for Cobra-based CLIs that want an umbrella subcommand to drive its siblings without coupling to their internals. Resolving the binary path via `os.Args[0]` plus `exec.LookPath` ensures the umbrella runs the same binary that hosts it (no `PATH` collision risk during tests).

### External References

- Not used. This is internal repo work; all patterns exist locally.

---

## Key Technical Decisions

- **Subprocess per leg, not in-process function calls.** Confirmed via design question. Trades 5 × 5ms startup overhead for stable exit-code propagation, real-time per-leg output, and easy stub-based testing. Matches the `verify-skill` shim pattern.
- **Self-invocation via `os.Args[0]` resolved once at startup.** Use `exec.LookPath` against the running binary's path so the umbrella never accidentally runs an older `printing-press` from `$PATH`. Tests can override via an internal hook.
- **Sequential execution, run all legs even on failure.** Operators want to see all gaps in one pass. Fail-fast would force `dogfood-fix-rerun-verify-fix-rerun` cycles instead of a single `shipcheck-fix-rerun-shipcheck` cycle.
- **Verdict aggregation = max of leg exit codes.** Preserves first-failure code shape and surfaces a meaningful number to scripts that branch on exit codes. Summary table shows per-leg codes for the human.
- **Recommended defaults baked in: `verify --fix` and `scorecard --live-check`.** These are the recommended invocations from current Phase 4 prose. Provide opt-out flags (`--no-fix`, `--no-live-check`) for cases where the operator wants a quick read without the auto-fix loop or live-check sample. Default-on matches "the operator should run one command and get the canonical result."
- **No `--only` flag on the umbrella.** If a user wants to run only one leg, they should run that leg directly. The umbrella's job is to be the all-or-nothing canonical sweep.
- **Skill Phase 4 prose: replace the 5-command block, keep per-leg interpretation prose.** The interpretation prose explains what each leg is for; that context is still useful when a leg fails and the operator needs to understand the verdict. Only the invocation collapses; the explanatory content stays.
- **Polish-worker: keep the existing Phase 1 diagnostic block; add `verify-skill` and `workflow-verify` to it; do not switch the polish-worker to call `shipcheck`.** Polish has its own structured Phase 1 / Phase 2 / Phase 4.85 flow that the polish-worker agent prompt parses; folding into `shipcheck` would lose that structure. Inserting two more leg invocations is the minimal correct change.

---

## Open Questions

### Resolved During Planning

- **Should the umbrella support `--only` for selective leg execution?** No. Operators wanting selective runs use the legs directly. Umbrella's contract is all-or-nothing.
- **Should the umbrella spawn legs in parallel?** No. Sequential. Avoids races on the working directory between `dogfood` and `verify`, keeps output readable.
- **Should `verify --fix` be on or off by default in shipcheck?** On by default, with `--no-fix` to disable. Matches the current Phase 4 recommendation.
- **What's the JSON envelope shape?** `{passed: bool, exit_code: int, legs: [{name, exit_code, passed, started_at, elapsed}], started_at, elapsed}`. Each leg's own JSON output is NOT nested — operators wanting structured per-leg detail run the leg directly with `--json`.

### Deferred to Implementation

- Whether `shipcheck` should print a leading "running 5 legs against <dir>" header before the first leg starts, or just let `dogfood`'s output be the first thing the user sees. Decide during implementation based on whether the per-leg streaming output already provides enough context.
- Whether the summary table is markdown-pipe formatted (`| LEG | RESULT |`) or column-formatted plain text. Both work; pick whichever matches the closest existing summary in the binary (e.g., what does `verify`'s table look like?).

---

## High-Level Technical Design

> *This illustrates the intended approach and is directional guidance for review, not implementation specification. The implementing agent should treat it as context, not code to reproduce.*

The umbrella's runtime shape:

```
shipcheck RunE
├── resolve self-binary path (exec.LookPath against os.Args[0])
├── validate --dir exists and contains a built CLI
├── for each leg in [dogfood, verify, workflow-verify, verify-skill, scorecard]:
│     ├── build args (common: --dir; per-leg: --spec, --research-dir, --fix, --live-check, ...)
│     ├── stream child stdout/stderr to ours (os.Stdout/os.Stderr)
│     ├── capture exit code
│     └── append LegResult{name, exit_code, started_at, elapsed} to results[]
├── render summary table (per-leg verdict + total)
└── return ExitError with code = max(leg_exit_codes), Silent=true (summary already printed)
```

Each leg is a small struct in shipcheck.go:

```go
type leg struct {
    name string                                  // "dogfood", "verify", ...
    args func(opts) []string                     // closure over umbrella flags + per-leg defaults
    skip func(opts) (skip bool, reason string)   // optional — e.g., scorecard skipped when --no-live-check is incompatible with the leg
}
```

A package-level slice `var legs = []leg{...}` enumerates the five in canonical order. Adding a future leg = append to the slice + define `args` builder.

---

## Implementation Units

- U1. **Add `shipcheck` umbrella command with leg orchestration**

**Goal:** A working `printing-press shipcheck --dir <cli> [--spec <path>] [--research-dir <path>]` command that runs all five legs in sequence, propagates exit codes, prints a summary, and is registered on the binary's root command.

**Requirements:** R1, R2, R3, R4, R6, R9.

**Dependencies:** none.

**Files:**
- Create: `internal/cli/shipcheck.go`
- Modify: `internal/cli/root.go` (add `rootCmd.AddCommand(newShipcheckCmd())` near the existing leg registrations)
- Test: `internal/cli/shipcheck_test.go`

**Approach:**
- Add `newShipcheckCmd()` returning `*cobra.Command` following the existing pattern in `dogfood.go`, `verify.go`.
- Resolve self-binary path once at the start of `RunE` via `exec.LookPath(os.Args[0])`. Surface a clear error if the binary can't find itself (corrupted `os.Args[0]`, unusual launch context).
- Validate `--dir` is non-empty and points to a directory containing a buildable Go module (look for `go.mod` and either `cmd/<name>-pp-cli/main.go` or `internal/cli/`). Use `ExitInputError` for validation failures.
- Define a package-level `legs` slice with one entry per leg (`dogfood`, `verify`, `workflow-verify`, `verify-skill`, `scorecard`). Each entry has a `name` and an `args func(*shipcheckOpts) []string` closure that builds its argv from the umbrella's flag values.
- Iterate the slice. For each leg: build argv, run via `exec.Command(selfBinary, argv...)`, attach stdin/stdout/stderr to `os.Stdin/os.Stdout/os.Stderr`, capture exit code via `cmd.Run()` plus `ExitError.ExitCode()`. Track per-leg `LegResult`.
- After all legs run, render a summary table to `os.Stdout` (operator's terminal) listing per-leg verdict, exit code, and elapsed.
- Compute `umbrellaCode = max(leg_exit_codes)`. If non-zero, return `&ExitError{Code: umbrellaCode, Err: fmt.Errorf("shipcheck failed: %d/%d legs failed", failingCount, len(legs)), Silent: true}` to propagate the code without printing a duplicate error message.

**Patterns to follow:**
- `internal/cli/verify_skill.go` lines 90–125 for the subprocess + stream + exit-code-propagation pattern.
- `internal/cli/dogfood.go` for the cobra command shape (Use, Short, Long, Example, RunE signature, flag declarations).
- `internal/cli/exitcodes.go` for `ExitError`/`ExitInputError` semantics.

**Test scenarios:**
- Happy path: stub binary on PATH that exits 0 for all leg invocations → `shipcheck` returns nil, summary shows 5 passing legs, total exit code 0.
- One leg fails: stub binary exits 1 only for `verify-skill` → umbrella returns `ExitError{Code: 1}`, summary shows 4 passing + verify-skill failing, all 5 legs were invoked (no fail-fast).
- Multiple legs fail: stub exits non-zero for `dogfood` (code 2) and `scorecard` (code 1) → umbrella returns `ExitError{Code: 2}` (max of failing codes), summary shows both failures.
- Edge: `--dir` empty or missing → `ExitInputError` before any leg runs, no subprocesses spawned.
- Edge: `--dir` does not exist on disk → `ExitInputError`.
- Edge: self-binary path can't be resolved (test by mutating `os.Args[0]` to something invalid) → meaningful error returned, no spawn attempts.
- Integration: stub printing-press binary placed in a temp dir, prepended to `PATH`, invoked via the umbrella → verifies argv passed to each leg matches expected (e.g., `verify` gets `--dir X --fix` by default, `dogfood` gets `--dir X --spec Y --research-dir Z`).

**Verification:**
- `go test ./internal/cli/...` passes including new shipcheck tests.
- `go build -o printing-press ./cmd/printing-press && ./printing-press shipcheck --help` shows the new command with documented flags.
- `./printing-press shipcheck --dir <existing-cli>` runs all five legs visibly and prints a summary; exit code matches whether any leg failed.

---

- U2. **Add `--json` output and selective flag pass-through to shipcheck**

**Goal:** Operators and scripts can run `printing-press shipcheck --json` to get a stable structured envelope. Common per-leg flags (`--api-key`, `--env-var`, `--no-fix`, `--no-live-check`, `--strict`) pass through to the appropriate leg.

**Requirements:** R5, R6.

**Dependencies:** U1.

**Files:**
- Modify: `internal/cli/shipcheck.go`
- Modify: `internal/cli/shipcheck_test.go`

**Approach:**
- Add umbrella flags: `--json` (bool), `--no-fix` (bool, default false → `verify --fix` is on by default), `--no-live-check` (bool, default false → `scorecard --live-check` is on by default), `--api-key` (string), `--env-var` (string), `--strict` (bool, passes to verify-skill).
- Update each leg's `args` closure to read the umbrella flags. Examples:
  - `dogfood`: `--dir <D>` plus `--spec <S>` if set plus `--research-dir <R>` if set.
  - `verify`: `--dir <D>`, `--spec <S>` if set, `--fix` unless `--no-fix`, `--api-key <K>` if set, `--env-var <E>` if set.
  - `scorecard`: `--dir <D>`, `--research-dir <R>` if set, `--spec <S>` if set, `--live-check` unless `--no-live-check`.
  - `verify-skill`: `--dir <D>`, `--strict` if set.
- When `--json` is set: do NOT pass `--json` through to legs (their JSON would interleave with each other). Instead, capture each leg's exit code as before, suppress the human summary table, and emit a single JSON envelope at the end:

  ```json
  {
    "passed": true,
    "exit_code": 0,
    "started_at": "2026-04-27T13:45:00Z",
    "elapsed_ms": 12450,
    "legs": [
      {"name": "dogfood", "exit_code": 0, "passed": true, "elapsed_ms": 1200, "command": "printing-press dogfood --dir ..."},
      {"name": "verify", "exit_code": 0, "passed": true, "elapsed_ms": 4800, "command": "printing-press verify --dir ... --fix"},
      ...
    ]
  }
  ```

- Each leg's stdout/stderr still streams to ours during the run (the JSON envelope is end-of-run only). Operators piping `--json` to `jq` should `2>/dev/null` if they want clean JSON.

**Patterns to follow:**
- `internal/cli/verify_skill.go` for the `--json` pass-through decision (it does pass `--json` through, but the umbrella's case is different because per-leg JSON would collide).
- Existing JSON envelope shapes in `internal/cli/*.go` for naming conventions (`exit_code` over `code`, `elapsed_ms` over `duration`).

**Test scenarios:**
- Happy path: `--json` produces parseable JSON conforming to the envelope above; `passed: true` when all legs pass; `legs[].command` reflects the full argv per leg.
- `--no-fix` happy: argv built for `verify` does NOT contain `--fix`; `argv` for other legs unchanged.
- `--no-live-check` happy: argv built for `scorecard` does NOT contain `--live-check`.
- `--api-key X` happy: argv for `verify` contains `--api-key X`; other legs unchanged.
- `--env-var GITHUB_TOKEN` happy: argv for `verify` contains `--env-var GITHUB_TOKEN`.
- `--strict` happy: argv for `verify-skill` contains `--strict`.
- Defaults: with no opt-out flags, `verify` argv contains `--fix`, `scorecard` argv contains `--live-check`.
- JSON failure case: when one leg fails, envelope `passed=false`, `exit_code` reflects max failing code, the failing leg's `passed=false`.

**Verification:**
- `./printing-press shipcheck --dir <cli> --json | jq '.passed'` returns a boolean.
- `./printing-press shipcheck --dir <cli> --no-fix --json | jq '.legs[] | select(.name=="verify").command'` does not contain `--fix`.

---

- U3. **Update `/printing-press` skill Phase 4 prose to recommend `shipcheck`**

**Goal:** Phase 4 of `skills/printing-press/SKILL.md` recommends `printing-press shipcheck` as the canonical invocation. Per-leg interpretation prose (what each leg is for, the fix order, the ship threshold criteria) is preserved. Operators can still see what each leg is for and what its failure means; they just don't have to remember to invoke each one.

**Requirements:** R7.

**Dependencies:** U1 (the umbrella must exist before the skill recommends it).

**Files:**
- Modify: `skills/printing-press/SKILL.md` (lines ~1724–1790 — the `## Phase 4: Shipcheck` block)

**Approach:**
- Replace the 5-command code block (lines ~1735–1739) with:

  ```bash
  printing-press shipcheck \
    --dir "$CLI_WORK_DIR" \
    --spec <same-spec> \
    --research-dir "$API_RUN_DIR"
  ```

  Note: any operator who wants the legacy invocation can still run each leg individually; mention this once in the prose so existing scripts don't surprise the operator.
- Keep the "Interpretation:" block (the bullet list explaining what each leg catches). This is the per-leg interpretation prose that's still useful when a leg fails.
- Keep the "Fix order" numbered list (it's still relevant when an operator is iterating on fixes).
- Update the "Ship threshold" section to lead with: "`shipcheck` exits 0" and then enumerate the per-leg conditions as supporting detail. This preserves all existing per-leg threshold semantics while making the umbrella the canonical signal.
- Skim the rest of the skill for any other 5-command-block invocations of the same legs and decide case-by-case whether to collapse. Do NOT collapse standalone leg invocations elsewhere (e.g., during fix loops) — those are intentional iteration points, not shipcheck.

**Patterns to follow:**
- The skill already has umbrella-style command invocations elsewhere (e.g., `printing-press generate ...` does many things via one command). Match the same prose density for the new shipcheck block.

**Test scenarios:**
- Test expectation: none — this is a prose change in a skill markdown file. Verification is manual review against the diff. (No automated test harness asserts SKILL.md content; adding one would over-specify the prose.)

**Verification:**
- Manual review of the diff against `skills/printing-press/SKILL.md`. Confirm:
  - One `printing-press shipcheck ...` invocation in the Phase 4 block.
  - Per-leg interpretation prose preserved.
  - Fix order list preserved.
  - Ship threshold leads with `shipcheck` exit 0 and references per-leg conditions as supporting detail.
  - No other content changed.

---

- U4. **Polish-worker runs `verify-skill` and `workflow-verify`; hard-gates `ship_recommendation` on verify-skill exit 0**

**Goal:** The polish-worker agent runs `verify-skill` and `workflow-verify` in its Phase 1 diagnostic loop alongside the existing `dogfood`, `verify`, `scorecard`, `go vet` invocations. Surfaces verify-skill findings as a category. Refuses to return `ship_recommendation: ship` when `verify-skill` has unresolved findings.

**Requirements:** R8.

**Dependencies:** none (polish-worker invokes `verify-skill` directly, not via `shipcheck`; this is intentional — see Key Technical Decisions).

**Files:**
- Modify: `agents/polish-worker.md`

**Approach:**
- In `## Phase 1: Baseline`, add two new diagnostic invocations:

  ```bash
  printing-press verify-skill --dir "$CLI_DIR" --json 2>&1 | tee /tmp/polish-verify-skill.json
  printing-press workflow-verify --dir "$CLI_DIR" --json 2>&1 | tee /tmp/polish-workflow-verify.json
  ```

- Add two rows to the Categories table:
  - `SKILL static-check failures` — source: `verify-skill --json` — what to look for: any non-empty `findings[]` with `severity=error`.
  - `Workflow gaps` — source: `workflow-verify --json` — what to look for: verdict `workflow-fail`.
- In Phase 2 (the fix loop), add a fix priority for `verify-skill` errors before "README gaps":
  - For `flag-commands` errors: inline the flag declarations in the affected command's source file (this is the WU-3 / F3 workaround until the verify-skill machine fix lands).
  - For `flag-names` errors: declare the flag in `internal/cli/*.go` or remove the SKILL reference.
  - For `positional-args` errors: change `Use: "X <arg>"` to `Use: "X [arg]"` when the command also accepts the value via a flag (the F2 fix); otherwise update the SKILL example to include the required positional.
- In the output contract (the `---POLISH-RESULT---` block specification), add: `ship_recommendation: ship` requires `verify-skill` exit 0 AND `workflow-verify` verdict not `workflow-fail`. If either gate fails after the fix pass, set `ship_recommendation: hold` and surface the unresolved findings under `remaining_issues`.

**Patterns to follow:**
- The existing Phase 1 / Phase 2 / Phase 4.85 / output-contract structure in `agents/polish-worker.md`. Insert the new content into the existing sections; don't restructure.

**Test scenarios:**
- Test expectation: none — this is a prose change in an agent markdown file. Verification is by exercising the agent in a real polish run during U5 verification (or in the next time the polish-worker is invoked) and confirming it now runs both legs and gates ship_recommendation correctly.

**Verification:**
- Manual review of the diff against `agents/polish-worker.md`. Confirm:
  - Phase 1 diagnostic block adds the two new commands.
  - Categories table has two new rows.
  - Phase 2 fix loop documents the verify-skill remediation patterns (flag-commands, flag-names, positional-args).
  - Output contract requires verify-skill and workflow-verify to pass before `ship_recommendation: ship`.
- After landing, the next polish run on a CLI with a deliberate verify-skill mismatch should report it under `findings` and refuse to ship.

---

## System-Wide Impact

- **Interaction graph:** `shipcheck` self-invokes the binary five times via `exec.Command`. The legs are independent and have no inter-leg dependencies — none of them write files the next leg reads beyond what the legs already write today (e.g., `dogfood` writing `research.json` updates that `scorecard` consumes when run later). Sequential ordering preserves the existing leg contract.
- **Error propagation:** Each leg's exit code propagates through subprocess `Run()` to a per-leg `LegResult`. The umbrella aggregates via `max()` and returns one `ExitError`. No silent suppression; the human summary always renders.
- **State lifecycle risks:** None. The legs are read-only with respect to each other except for `dogfood`'s research.json updates and `verify`'s `--fix` source-code edits. Both already happen in the current Phase 4 sequence; ordering is unchanged.
- **API surface parity:** None. The legs' standalone CLIs are unchanged. `shipcheck` is purely additive.
- **Integration coverage:** A unit test stubs the binary self-invocation by writing a temporary `printing-press` shell script on `PATH` that exits with controllable codes per argv. Cross-layer behavior (umbrella → real leg → real CLI) is exercised by an end-to-end smoke test against a known-good CLI in the test environment, but that test runs only when explicitly opted in (`PRESS_INTEGRATION=1`) since it depends on `~/printing-press/library/` state.
- **Unchanged invariants:** `printing-press dogfood`, `printing-press verify`, `printing-press workflow-verify`, `printing-press verify-skill`, `printing-press scorecard` continue to work standalone with their current flag surfaces. CI scripts and operator workflows that invoke them directly are not affected.

---

## Risks & Dependencies

| Risk | Mitigation |
|------|------------|
| Self-binary resolution fails in unusual launch contexts (e.g., binary renamed, `os.Args[0]` is empty) | Fail fast with a clear error message naming what was tried (`os.Args[0]` value, `exec.LookPath` result). Never fall back to `printing-press` from `$PATH` — that risks invoking a different version. |
| A leg's flag surface changes and the umbrella's argv builder goes stale | Keep the per-leg argv builder a closure with named flag accesses (no string-templated argv). Add a small comment per leg pointing at the leg's source file so a future change to the leg sets up the maintainer to update both files. |
| Stub-binary tests are flaky on Windows (no shell scripts) | Use a Go-built test helper binary placed on `PATH` for integration tests, not a shell script. Pattern matches `verify-skill_test.go` if applicable. |
| `verify --fix` mutates source during `shipcheck` and confuses operators who expected a read-only check | Document this in `shipcheck --help`. Provide `--no-fix` for read-only mode. Default-on matches current Phase 4 prose. |
| Polish-worker invokes the legs directly instead of via `shipcheck`, drifting from the umbrella | This is intentional (Key Technical Decisions). Polish has its own Phase 1/2/4.85 structure that doesn't fold into `shipcheck` cleanly. Both paths run the same legs; the umbrella is for operators, the agent's manual sequence is for the agent. Document this in `agents/polish-worker.md`. |
| `shipcheck` becomes a dumping ground for new legs over time, growing into something hard to reason about | The `legs` slice pattern keeps additions cheap and reviewable: one append + one args closure. Document the "add a leg here" comment in `shipcheck.go`. New legs from future WUs (MCP-coverage, secret-scan) will follow the same shape. |

---

## Documentation / Operational Notes

- Add `printing-press shipcheck --help` output to the binary's command reference if one exists.
- After landing, the retro issue mvanhorn/cli-printing-press#351 should be updated with a comment that WU-1 has shipped and references the merge commit. (The retro author handles this; not part of this plan's implementation.)
- No CHANGELOG entry needed unless this binary uses release-please-style changelogs (it does — see AGENTS.md). The conventional commit format `feat(cli): add shipcheck umbrella` will populate the next release notes automatically. Same for `feat(skills): ...` and the polish-worker update.

---

## Sources & References

- **Origin:** retro issue [mvanhorn/cli-printing-press#351](https://github.com/mvanhorn/cli-printing-press/issues/351), WU-1
- **Retro proof:** `manuscripts/company-goat/20260427-121015/proofs/20260427-134000-retro-company-goat-pp-cli.md`
- **Public-library CI failure that motivated this:** `mvanhorn/printing-press-library#140` (first run, before fix)
- Related code: `internal/cli/verify_skill.go` (subprocess-shim pattern), `internal/cli/exitcodes.go` (`ExitError`), `internal/cli/root.go` (binary command registration), `internal/cli/dogfood.go` (cobra command shape)
- AGENTS.md commit conventions: `feat(cli)`, `feat(skills)` scopes apply.
