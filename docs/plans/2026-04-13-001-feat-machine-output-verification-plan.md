---
title: "feat: Machine-level output verification (M1-M4)"
type: feat
status: active
date: 2026-04-13
deepened: 2026-04-13
origin: docs/retros/2026-04-13-recipe-goat-retro.md
---

# feat: Machine-level output verification (M1-M4)

## Overview

Five user-visible bugs shipped in `recipe-goat-pp-cli` — HTML entities in titles, false-positive ingredient substitutions, silently-dropped sources in aggregation commands, nav-link URLs misidentified as recipe permalinks, pantry-staple noise in ingredient-match results. All five were caught in five minutes of hands-on testing by the user. None were caught by the machine's existing dogfood, verify, or scorecard gates.

The root cause is that the Printing Press currently tests structure (exit codes, registered commands, help parse) and live-sampling query-presence (via `scorecard --live-check` shipped in #210). It does not test whether a human would accept the command's output as sensible. This plan closes four of the resulting gaps with surgical, domain-general machine changes.

The five per-CLI bugs have already been fixed surgically in `mvanhorn/printing-press-library#68`. This plan is entirely about generalizing the patterns so future CLIs don't ship with the same classes of bug.

## Problem Frame

During a 5-minute end-to-end test of `recipe-goat-pp-cli` (installed from the public library), the user found:

1. **HTML entities in output** (`The Food Lab&#39;s Chocolate Chip Cookies`) — raw schema.org strings passed through the JSON-LD parser unencoded.
2. **Ingredient substitution false positives** (`sub buttermilk` returned oil/coconut oil, because matching used bi-directional substring — "buttermilk" contains "butter").
3. **Aggregation commands silently dropping sources** (`trending --site smittenkitchen,budgetbytes` returned only budgetbytes with no warning — `goat` does surface per-site errors; `trending` was inconsistent).
4. **URL denylist gaps** (`/random/` from budgetbytes homepage passed `looksLikeRecipeLink` because the generic `/[a-z0-9-]{6,}/?$` pattern accepts six-character segments).
5. **Pantry-staple noise in match results** (`cookbook match` counted "1 ice cube" as a missing ingredient).

Per AGENTS.md, the triage question is: *is this a machine change or a printed CLI change?* All five were fixable in the printed CLI (done in PR #68). The more important question is: *which patterns would compound across future CLIs?*

Three patterns generalize:
- **Preventing HTML entities at the scraper layer** — any future CLI that extracts text from HTML will hit this unless the machine provides a shared, entity-decoding text helper.
- **Enforcing the partial-failure pattern for aggregation commands** — `recipe-goat` has two fan-out commands and only one of them reports errors; the inconsistency is generator-level.
- **Detecting the "exit 0 but output is suspect" class of bug** — dogfood and verify both pass when the CLI executes cleanly; only a human-style review catches "why is butter in buttermilk results?"

Two patterns do **not** generalize:
- **The `/random/` denylist** is domain-specific (adding to every CLI's scraper would produce false positives for CLIs where `/random` is a real resource).
- **The pantry-staples stopword list** is recipe-specific (meaningless for Stripe or GitHub).

Those stay in the printed CLI (already merged in #68).

## Requirements Trace

- **R1. Aggregation command consistency.** The generator emits a shared fan-out helper so every future multi-source command collects per-source errors and surfaces them, eliminating the `goat` vs `trending` inconsistency class of bug. (Recipe-goat retro action item carries this implicitly through #5.)
- **R2. HTML entity decoding by default.** Agent-authored scraping code (in internal/* packages) has a standard text-normalization helper available, so the "forgot to `html.UnescapeString`" failure mode is a compile-time opt-out rather than a silent default.
- **R3. Output-sanity gates in live-check.** `scorecard --live-check` (already shipped, PR #210) extends its per-feature output assertions with at least one domain-general rule that would have caught the entity bug: no raw HTML entities in sampled output.
- **R4. Agentic output review phase.** The skill layer gains a new agentic-review phase (parallel to Phase 4.8's SKILL-review agent) that samples N novel-feature outputs and asks a reviewer "does this look right?" — the only mechanism that can catch pattern-recognition-shaped bugs (sub buttermilk → butter) without being domain-specific.
- **R5. Baseline test seeding.** Every generated CLI ships with at least one `*_test.go` covering pure-logic packages (the generator's own templates for text normalization, fan-out, etc.). Novel packages authored during Phase 3 have a Phase 3 gate requiring at least one table-driven happy-path test per pure-logic file.

## Scope Boundaries

- **Not rewriting the existing scorecard.** `scorecard --live-check` already works. This plan extends its output-assertions (R3), it does not replace it.
- **Not changing Phase 4.8.** The agentic SKILL-review phase is untouched. R4 adds a new phase adjacent to it.
- **Not changing dogfood.go or verify.go.** Those are structural checks by design. All new runtime-output checks go into live-check (which is the live-sampling layer the retro already established).
- **Not touching the `/random/` denylist or pantry-staples stopwords.** Those are domain-specific fixes already merged in #68.

### Deferred to Separate Tasks

- **Dogfood "auto-generated test matrix" (retro action #2).** Distinct work that enumerates the command tree exhaustively; larger in scope than R3. Separate PR.
- **"Ship-with-gaps" removal from Phase 4/5 verdicts (retro action #1).** Policy change, not verification-layer change. Separate PR.
- **Phase 3 "feature-level acceptance test" requirement (retro action #5).** This plan's R5 touches Phase 3 for pure-logic test seeding (now structurally enforced via the new dogfood test-presence check). Feature-level acceptance (e.g., "after `goat brownies`, ≥3 of top-5 titles contain 'brown'") is a broader contract change and remains separate.
- **LLM integration in the Go binary.** R4 is explicitly skill-level because the binary has no LLM client. Adding one is out of scope.

## Context & Research

### Relevant Code and Patterns

- `internal/pipeline/live_check.go` (491 lines) — the existing live-sampling scorecard dimension shipped in PR #210. Already captures stdout, already runs per-feature Example commands, already has worker-pool concurrency. R3 extends its per-feature assertions.
- `internal/generator/templates/helpers.go.tmpl` (31KB) — shared helpers emitted into every generated CLI's `internal/cli/helpers.go`. Referenced as a style template (short doc comments, one purpose per function); not the target for M1/M2 helpers after the deepening review (those move to a new `internal/cliutil/` package — see Key Technical Decisions).
- `internal/generator/templates/sync.go.tmpl` lines 108–148 — existing worker-pool idiom (default=4 concurrency, `sync.WaitGroup`, channel-based accumulation, stderr error format). The concrete pattern Unit 1's `FanoutRun` mirrors so the new helper feels consistent with already-generated code.
- `internal/generator/templates/captured_test.go.tmpl` — existing pattern for emitting `_test.go` alongside generated code. Precedent for R5's test-seeding templates.
- `internal/pipeline/scorecard.go` (2199 lines) — the scorecard aggregator. Already consumes `live_check.LiveCheckResult`. Extension for R3 is a rule addition, not a dimension addition.
- `skills/printing-press/SKILL.md` Phase 4.8 (around line 1677) — the existing agentic-review phase for SKILL.md. R4's new phase mirrors this contract: same dispatcher, same gating structure, different content.
- `recipe-goat`'s own fan-out patterns: `internal/cli/goat_cmd.go` (reports per-site errors correctly) vs `internal/cli/trending_cmd.go` (swallowed them until #68). The correct one is the pattern to extract.

### Institutional Learnings

- The recipe-goat retro (docs/retros/2026-04-13-recipe-goat-retro.md) established several overlapping machine-improvement patterns; #210 shipped action #4 (scorecard --live-check), #209 shipped action #7 (synthetic kind), the remaining high-severity items (#1-3) are policy/skill changes out of scope here.
- Live-check's current output-relevance rule (`outputMentionsQuery`) is a weak assertion that passes when any query token appears in stdout. Strengthening it with rule-based checks (R3) is incremental and preserves the existing calibration.
- Phase 4.8 is the established template for agentic review in this repo. Its prompt contract — "6 checks, each <50 words, return `PASS` or findings with severity" — is a pattern R4 should mirror.
- Per AGENTS.md: "never change the machine for one CLI's edge case." Two of the five original bugs failed this test (URL denylist, stopwords); their fixes correctly stayed in the printed CLI. This plan's four items all pass the test — each helps every future CLI that hits the same pattern.

### External References

Not applicable — no new framework adoption. The changes are internal to the generator and build on existing Go stdlib (`html`, `context`, `sync`).

## Key Technical Decisions

- **Shared helpers live in a new `internal/cliutil/` package, not inline in `helpers.go`.** Generated CLIs author novel code in `package cli` during Phase 3; emitting generic-name helpers (`cleanText`, `FanoutRun`, `FanoutError`, etc.) into the same package produces collision risk with no precedence guidance to agents. Moving them to `internal/cliutil/` with `cliutil.CleanText` / `cliutil.FanoutRun` call sites makes ownership unambiguous and survives an agent that happens to author its own `cleanText` in `package cli` for unrelated work. Trade-off accepted: one extra emitted package per CLI and one extra import line per call site. *(Revised from the original "helpers.go.tmpl, not a new package" choice after architecture-strategist review surfaced the collision risk as concrete, not hypothetical.)*
- **`FanoutRun` is generic over T (result type) and S (source type) and uses a functional-options signature.** Go 1.23 in `go.mod.tmpl` supports generics. Non-generic alternatives force callers to type-assert. The functional-options pattern — `FanoutRun(ctx, sources, name, fn, opts...)` with `WithConcurrency(n)` — lets the same helper serve per-call-site concurrency needs (scraping 15 sites at 4 vs fetching internal shards at 32) rather than forcing a package-level const that can only be set once.
- **FanoutRun specifies its concurrency contract explicitly, not just its signature.** Contract: (a) workers respect ctx — on `ctx.Done()` they stop pulling new jobs and in-flight fn calls receive the cancelled ctx; (b) unpulled sources produce a `FanoutError{Err: ctx.Err()}` so reporting stays complete; (c) errors attach to source index and emit in **source order** (not completion order) so `FanoutReportErrors` is deterministic and golden-testable; (d) the jobs channel is bounded at `2 * concurrency` so a 1000-source fan-out doesn't buffer 1000 goroutines. *Performance-oracle review surfaced these as specification gaps in the original design.*
- **Rate limiting stays out of `FanoutRun`.** Per-source rate limiting is a caller concern (wrap `fn` with a limiter or `time.Sleep`). Baking backoff/jitter into the helper would be domain-specific and hard to tune. The helper must not *preclude* rate limiting — the `fn` closure is where callers add it. Recipe-goat's food52/simplyrecipes 429s are the canonical example of why this belongs in caller-side wrappers, not the helper.
- **Output entity-scan lives in live_check, not verify.** Verify is structural; live_check already captures stdout; the entity regex belongs alongside `outputMentionsQuery`. Co-locating assertions keeps the "live sampling rules" in one file.
- **Agentic output review is a new Phase 4.85, not merged into 4.8.** Phase 4.8 has a tight 6-check contract focused on SKILL-vs-code consistency. Stuffing "review command output for plausibility" into the same phase would bloat the prompt and dilute the existing checks. A new phase with its own tight contract is cleaner, and mirrors Phase 4.8's four-section shape exactly (Dispatch, Gate, Why agentic vs template-only, Known blind spots).
- **Phase 4.85 is also invoked by `/printing-press-polish`, giving backfill for already-shipped CLIs.** Polish is the canonical path for revisiting published CLIs. Wiring Phase 4.85 into polish's existing verify+dogfood+scorecard block means every polish run re-reviews outputs of older CLIs — no separate backfill mechanism needed.
- **Phase 4.85 is interactive-only in v1.** Without a TTY, warnings default to fail-open-with-log and reviewer crashes (timeout, agent-budget exhaustion) treat as SKIP with detail in the scorecard. No `--auto-approve-warnings` flag yet — no current cron path exists for shipcheck; don't build for hypothetical consumers.
- **R4 runs after R3 (automated rules), not instead of it.** Rule-based live-check is cheap and deterministic; it should run every scorecard pass. Agentic review is tokenful; it runs once per run under the skill's Phase 4.85 gate, using the rule-pass output as input so the reviewer focuses on the squishy cases.
- **Test seeding is enforced *structurally*, not just advisorily.** Two leverage points: (a) the generator emits `cliutil_test.go` alongside the shared helpers — deterministic, compiles every build; (b) `dogfood.go` gains a test-presence check that walks generated CLI packages, identifies pure-logic packages (exported funcs, no `cobra.Command`), and flags packages with zero `_test.go` files as a dogfood issue. *(Revised from "advisory enforcement" after repo-research-analyst surfaced that `dogfood.go` already walks these packages but explicitly skips `_test.go` — adding the presence check is ~40 LOC in existing walk logic.)* The Phase 3 skill prompt still instructs agents to write tests; dogfood is the structural backstop.

## Open Questions

### Resolved During Planning

- **Does the Go binary have LLM integration?** No. All LLM work is skill-level (via the Agent tool). R4 is therefore skill-level by necessity, not preference.
- **Does a live-check exist already?** Yes — `internal/pipeline/live_check.go`, shipped in PR #210 as retro action #4. This plan extends it, does not replace it.
- **Is there an existing agentic review phase?** Yes — Phase 4.8 (Agentic SKILL Review) at SKILL.md line 1677. R4's new phase mirrors its contract.
- **Should the fan-out helper be a separate package?** **Yes** — revised during deepening. Helpers live in `internal/cliutil/`, not inline in `helpers.go`, after architecture-strategist review surfaced concrete symbol-collision risk with agent-authored code in `package cli`. See "Key Technical Decisions" above.
- **Does Go 1.23 support generics?** Yes, `go.mod.tmpl` already pins `go 1.23`.
- **Should novel-code test seeding use templates?** No for novel-generated code — mechanical test generation for unknown code produces noise. Yes for **generator-owned helpers**: `cliutil_test.go` is templated unconditionally. Structural enforcement via dogfood (walk packages, count `_test.go` presence, flag pure-logic packages without tests) backs both, paired with a Phase 3 skill prompt for novel code.
- **Can the `/printing-press-polish` skill provide a backfill path for Phase 4.85?** Yes — polish already dispatches verify+dogfood+scorecard against an already-shipped CLI. Wiring Phase 4.85 in alongside is a one-line addition.
- **How do reviewer crashes map to shipcheck verdict in unattended contexts?** Crashes (timeout, agent-budget exhaustion) map to `SKIP` with detail in the scorecard, not FAIL. `warning` findings in non-TTY contexts default to fail-open-with-log. Documented explicitly in Phase 4.85's Gate subsection.

### Deferred to Implementation

- **Exact regex for "raw HTML entity"** in R3. `&#\d+;` is the obvious start; whether to also flag named entities (`&amp;`, `&quot;`, etc.) needs a pass through existing live-check output to confirm no false positives on legitimate JSON output. Will test against ~5 existing CLIs during implementation.
- **Exact prompt wording for R4's agentic reviewer.** Phase 4.8's prompt is ~400 words; R4 should be similarly tight. Will iterate during Unit 4 implementation against live recipe-goat output as the calibration case.
- **Whether test-seeding for R5 uses build tags to skip when the target package is empty.** Template emission can be conditional on whether the package has exported functions worth testing; deferred until implementing.
- **Which existing generator templates need test-seeding companions.** Scope will be "every .tmpl file that contains pure-logic helpers" — exact list enumerated during Unit 5.

## High-Level Technical Design

> *This illustrates the intended approach and is directional guidance for review, not implementation specification. The implementing agent should treat it as context, not code to reproduce.*

### The layered verification stack after this plan

```
┌────────────────────────────────────────────────────────────────────┐
│ Generation time                                                    │
│  ┌──────────────────────────────────────────────────────────┐      │
│  │ Generator templates emit:                                │      │
│  │  - internal/cliutil/fanout.go (M1) — FanoutRun + friends │      │
│  │  - internal/cliutil/text.go   (M2) — CleanText           │      │
│  │  - internal/cliutil/cliutil_test.go (M5) — seed tests    │      │
│  └──────────────────────────────────────────────────────────┘      │
│                         │                                          │
│                         ▼                                          │
│  ┌──────────────────────────────────────────────────────────┐      │
│  │ Phase 3 gate: novel pure-logic pkgs require _test.go     │      │
│  │  + structural dogfood test-presence check (M5/R5)        │      │
│  │  — skill prompts agent; dogfood enforces                 │      │
│  └──────────────────────────────────────────────────────────┘      │
│                         │                                          │
│                         ▼                                          │
│  ┌──────────────────────────────────────────────────────────┐      │
│  │ Phase 4 (shipcheck): dogfood + verify + scorecard        │      │
│  │  └─ scorecard --live-check                               │      │
│  │      └─ per-feature: exit 0 + non-empty + query tokens   │      │
│  │         + no raw HTML entities (M2b / R3 NEW)            │      │
│  └──────────────────────────────────────────────────────────┘      │
│                         │                                          │
│                         ▼                                          │
│  ┌──────────────────────────────────────────────────────────┐      │
│  │ Phase 4.85 (NEW): Agentic Output Review (M3 / R4)        │      │
│  │  - dispatches general-purpose agent with sampled outputs │      │
│  │  - mirrors Phase 4.8 contract (tight prompt, findings)   │      │
│  │  - gate: errors block, warnings surface                  │      │
│  └──────────────────────────────────────────────────────────┘      │
│                         │                                          │
│                         ▼                                          │
│  ┌──────────────────────────────────────────────────────────┐      │
│  │ Phase 4.8 (existing): Agentic SKILL Review (unchanged)   │      │
│  └──────────────────────────────────────────────────────────┘      │
└────────────────────────────────────────────────────────────────────┘
```

### Fan-out helper call-site sketch (M1)

```go
// Before — the bug pattern from trending_cmd.go (silent drop):
for _, s := range sites {
    go func() {
        body, err := fetch(s)
        if err != nil { return }        // <-- silently lost
        collect(process(body))
    }()
}

// After — the pattern the helper makes obvious and consistent:
results, errs := cliutil.FanoutRun(ctx, sites,
    func(s Site) string { return s.Hostname },
    func(ctx context.Context, s Site) ([]Entry, error) {
        return fetch(ctx, s)
    },
    cliutil.WithConcurrency(4),        // <-- per-call, not package-level
)
cliutil.FanoutReportErrors(cmd.ErrOrStderr(), errs)   // <-- impossible to skip;
                                                      //     stderr in source order
```

The helper does not enforce that callers use it — it provides the easy path. Phase 3 agent prompts (R5) are the enforcement layer for novel fan-out code.

## Implementation Units

- [ ] **Unit 1: Add shared fan-out helper in new `internal/cliutil/` package**

**Goal:** Provide `FanoutRun` + `FanoutReportErrors` + `FanoutError` + `FanoutResult` in a dedicated `internal/cliutil` package emitted into every generated CLI so aggregation commands have an obvious, consistent, collision-free pattern for per-source error collection. (R1)

**Requirements:** R1

**Dependencies:** None

**Files:**
- Create: `internal/generator/templates/cliutil_fanout.go.tmpl` (new template emitting `internal/cliutil/fanout.go`)
- Modify: `internal/generator/generator.go` (register the new template; emit `internal/cliutil/fanout.go` into every generated CLI)
- Modify: `internal/generator/templates/go.mod.tmpl` (no change expected — Go 1.23 already pinned; verify during implementation)
- Test: `internal/generator/cliutil_fanout_test.go` (new, validates rendered template)

**Approach:**
- Emit to `internal/cliutil/fanout.go` (package `cliutil`). Callers write `cliutil.FanoutRun(...)` / `cliutil.FanoutReportErrors(...)`.
- Exported surface:
  - `FanoutError struct { Source string; Err error }`
  - `FanoutResult[T any] struct { Source string; Value T }`
  - `FanoutRun[S, T any](ctx, sources, name, fn, opts...)` — functional options; `WithConcurrency(n int)` is the first option.
  - `FanoutReportErrors(w io.Writer, errs []FanoutError)` — writes `"  warn: %s: %s\n"` per error (stderr-format match to `sync.go.tmpl`).
- Concurrency contract (specified as part of the helper doc and tested):
  - Default concurrency: 4. Override via `WithConcurrency(n)`.
  - Jobs channel bounded at `2 * concurrency` — producers block on send; no unbounded goroutine buffer for 1000-source fan-outs.
  - On `ctx.Done()`: workers stop pulling new jobs; in-flight fn calls receive the same cancelled ctx (caller decides whether fn returns early).
  - Unpulled sources emit a `FanoutError{Err: ctx.Err()}` so reporting stays complete; no silent drop on cancel.
  - Errors are collected by source *index* and emitted in source order (not completion order) so `FanoutReportErrors` is deterministic and golden-test-safe.
- Rate limiting is explicitly caller's responsibility. Doc comment: "Per-source rate limiting is the caller's responsibility; wrap `fn` with a limiter (e.g., `golang.org/x/time/rate`) if needed. Recipe scraping CLIs in particular have hit 429 under naive fan-out."
- `FanoutReportErrors` trims error strings to 120 chars, first line only (matches existing helpers.go `sanitizeErrorBody` style).

**Patterns to follow:**
- `internal/generator/templates/sync.go.tmpl` lines 108–148 — the existing worker-pool template in the generator. Use its default-concurrency (4), its stderr error format (`"  %s: error: %v\n"`), and its `sync.WaitGroup` + channel-close pattern as the idiom for the new helper. Matching these avoids two different concurrency styles in generated code.
- Existing small helpers in `helpers.go.tmpl` (e.g., `truncate`, `sanitizeErrorBody`) — short doc comments, one purpose per function.
- `recipe-goat/internal/cli/goat_cmd.go` — the correct fan-out pattern this helper extracts (per-site error surfacing, bounded concurrency).

**Test scenarios:**
- Happy path: `FanoutRun` with 3 sources all succeeding returns 3 results in **source order** (not completion order) and 0 errors.
- Happy path: mixed success — 2 succeed, 1 fails. 2 results with correct source attribution; 1 error with correct source + reason.
- Edge case: empty sources slice returns empty results and empty errors, no panic.
- Edge case: all sources fail. 0 results and N errors, in source order.
- Edge case: `WithConcurrency(1)` forces serial execution; completion order == source order for obvious verification.
- Edge case: context cancelled *before* FanoutRun called — all sources emit `FanoutError{Err: ctx.Err()}` immediately.
- Edge case: context cancelled mid-flight — already-started workers receive cancelled ctx; unpulled sources emit `FanoutError{Err: ctx.Err()}`; no silent drops.
- Edge case: 100 sources with `WithConcurrency(4)` — jobs channel bounded, no goroutine explosion. Verify by wrapping `fn` with an atomic counter that tracks concurrent-execution count; assert `max ≤ concurrency` across the run. (Directly tests the contract — `runtime.NumGoroutine` is noisy with test-runtime/pprof goroutines and produces flakes.)
- Error path: `fn` returns a typed error with newlines — `FanoutReportErrors` emits single-line `warn:` record (first line, 120-char truncation).
- Integration: `FanoutReportErrors` with 2 errors writes exactly 2 lines, correct format, deterministic order.
- Integration: `FanoutReportErrors` with empty errs is a no-op (writes nothing).

**Verification:**
- `go test ./internal/generator/...` passes.
- Rendering the template against a sample config produces valid Go (`go build` succeeds on a freshly generated CLI).
- The rendered CLI has `internal/cliutil/fanout.go` and can be imported from `internal/cli` as `cliutil`.

- [ ] **Unit 2: Add scraped-text normalization helper to `internal/cliutil/`**

**Goal:** Provide a `CleanText(s string) string` helper in the shared `internal/cliutil` package (exported) so agent-authored scraping code has a one-line, collision-free normalization path. Closes the "forgot to unescape HTML entities" failure mode. (R2)

**Requirements:** R2

**Dependencies:** Unit 1 (establishes the `internal/cliutil` package and its emission path)

**Files:**
- Create: `internal/generator/templates/cliutil_text.go.tmpl` (new template emitting `internal/cliutil/text.go`)
- Modify: `internal/generator/generator.go` (register the new template alongside Unit 1's `cliutil_fanout.go.tmpl`)
- Test: `internal/generator/cliutil_text_test.go` (new)

**Approach:**
- Emit to `internal/cliutil/text.go` (same `package cliutil` as Unit 1's fanout.go).
- Single exported function: `CleanText(s string) string` returning `html.UnescapeString(strings.TrimSpace(s))`.
- Doc comment: "CleanText normalizes scraped text by trimming whitespace and decoding HTML entities. Always use this when extracting strings from HTML or schema.org JSON-LD. Skipping this step is how recipe-goat's 'The Food Lab&#39;s' bug shipped."
- Do **not** add `CleanTextSlice([]string)` — callers iterate and call per-item. Keeping the surface minimal.

**Patterns to follow:**
- Unit 1's `cliutil_fanout.go.tmpl` — same package, same emission pattern, same doc-comment style.
- Existing small helpers in helpers.go.tmpl (`truncate`, `bold`, `green`) — one purpose, short doc, testable.

**Test scenarios:**
- Happy path: `CleanText("The Food Lab&#39;s Cookie")` → `"The Food Lab's Cookie"`.
- Happy path: `CleanText("  Chicken Tikka  ")` → `"Chicken Tikka"`.
- Happy path: named entities — `CleanText("AT&amp;T")` → `"AT&T"`.
- Edge case: empty string returns empty string.
- Edge case: string with no entities passes through untouched (except trimming).
- Edge case: nested escaping — `CleanText("&amp;amp;")` → `"&amp;"` (single unescape pass, matching `html.UnescapeString` stdlib behavior; documented in comment).

**Verification:**
- `go test ./internal/generator/...` passes.
- Rendered `internal/cliutil/text.go` declares `CleanText` with the expected signature.
- A generated CLI can call `cliutil.CleanText(...)` from `package cli`.

- [ ] **Unit 3: Extend live-check to detect raw HTML entities in sampled output**

**Goal:** When `scorecard --live-check` samples a novel feature's output, fail the feature if the captured stdout contains raw HTML entities. This catches the `&#39;` class of bug at the scorecard gate, pre-ship. (R3)

**Requirements:** R3

**Dependencies:** None (doesn't depend on Unit 1 or 2 — this is the detection layer)

**Files:**
- Modify: `internal/pipeline/live_check.go`
- Modify: `internal/pipeline/live_check_test.go`

**Approach:**
- Add a private function `containsRawHTMLEntities(output string) (bool, string)` returning `(true, "found &#39;")` or similar on match. Regex: `&#\d+;` (numeric entities only in first pass; named entities like `&amp;` might false-positive on legitimate JSON and need calibration).
- In `runOneFeatureCheck` (live_check.go:239, where `outputMentionsQuery` at line 425 is already called), after the existing query-mention check, run the entity check. On match, set `result.Reason = "raw HTML entities in output: <example match>"`.
- **Gating mode: WARN for Wave B first, FAIL after calibration.** Initial ship sets `result.Status = StatusWarn` (new status value if not present, or equivalent warning path that reports in scorecard but does not fail live-check). After a 2-week calibration window with data from the library, flip to `StatusFail` in a follow-up PR if no regressions observed on existing A-grade CLIs. See Phased Delivery below.
- Gate: only apply the check when output is not `--json` mode (JSON mode legitimately contains escape sequences and is not user-facing).
  - Detect JSON mode by checking whether `args` includes `--json` or the first non-empty output line starts with `{` or `[`.
- Add 3 test fixtures: clean output (passes), output with `&#39;` (fails), JSON output containing `"\u0026amp;"` (passes — JSON mode bypass).

**Patterns to follow:**
- Existing `outputMentionsQuery` in live_check.go — same shape (receives output string + optional context, returns bool+reason).
- Existing live_check_test.go table-driven tests.

**Test scenarios:**
- Happy path: output "The Food Lab's Cookies" → no entities detected.
- Error path: output "The Food Lab&#39;s Cookies" → detected, feature marked FAIL with reason mentioning `&#39;`.
- Error path: output with `&#8217;` (typographic apostrophe entity) → detected.
- Edge case: output contains `&#` but not a valid entity ("foo&#bar") → not detected (requires digits + semicolon).
- Edge case: JSON-mode feature output `{"title":"The Food Lab's..."}` with properly-escaped Unicode (`\u0027`) → passes.
- Edge case: empty output → no entity check applied (the existing "empty output" fail-reason wins first).
- Integration: live-check run against a fixture CLI with one clean feature + one entity-ridden feature reports 1/2 pass.

**Verification:**
- `go test ./internal/pipeline/... -run LiveCheck` passes.
- Regression check: run live-check against an existing CLI with known-clean output; no new failures introduced.

- [ ] **Unit 4: Add Phase 4.85 "Agentic Output Review" to the main skill (and wire into polish)**

**Goal:** Run an agentic reviewer over sampled command outputs to catch plausibility bugs that rule-based checks can't encode — the "sub buttermilk returning butter" class. Runs after Phase 4.8 in the main skill, and is also invoked by `/printing-press-polish` so already-shipped CLIs can be re-reviewed (backfill path). (R4)

**Requirements:** R4

**Dependencies:** Unit 3 (R3's rule-based check runs first; R4's reviewer consumes its results + samples extra outputs for the agent)

**Files:**
- Modify: `skills/printing-press/SKILL.md` (new Phase 4.85 section inserted between 4.8 at line 1677 and Phase 5 at line 1724)
- Modify: `skills/printing-press-polish/SKILL.md` *and/or* `agents/polish-worker.md` — *(Correction from initial plan: `printing-press-polish/SKILL.md` does not dispatch verify/dogfood/scorecard directly; it delegates to the `cli-printing-press:polish-worker` agent at lines 114–122. The actual commands run in `agents/polish-worker.md` lines 43–45 and 207–209. The implementer must decide: (a) add Phase 4.85 inside polish-worker (worker runs the review during its fix-or-document loop), or (b) add a new dispatch step in polish SKILL.md after polish-worker returns (preserves worker's scope but adds a second agent call). The plan defers this architectural choice to implementation; see Deferred-to-Implementation section. Not a "one-line addition" as originally claimed.)*

**Approach:**

Mirror Phase 4.8's four-section structure *exactly* (not five — no separate "Test scenarios" block; calibration belongs in Verification):

1. **Dispatch** — prompt contract for the reviewer agent. Inputs: CLI binary path, sampled novel-feature commands (from `research.json`), live-check pass-rate report. Four numbered checks, each <50 words:
   1. **Output matches query intent.** For sampled novel features with a query argument, does the output contain results clearly related to the query? (Catches: sub buttermilk → butter; goat brownies → chili.)
   2. **No obvious format bugs.** Does the output contain raw HTML entities, mojibake, or malformed URLs? (Backup for R3 — rule-based catches `&#39;`; this layer catches mojibake and nav-link-as-content URLs.)
   3. **Aggregation commands show all requested sources.** For commands with `--source`/`--site`/`--region` CSV flags: if N sources requested, does output show N, or does stderr explain the missing ones? (Catches: trending silent drop.)
   4. **Result ordering/ranking makes sense.** For commands that rank/sort, does the top result look plausibly best? (Catches: broken score weights, off-by-one sort bugs.)

   Prompt suffix: "Only report issues a human user would notice in 5 minutes of testing. Return `PASS` or a list of findings with severity (`error` or `warning`), 50-word fix per finding."

2. **Gate** — severity-based outcomes matching Phase 4.8's contract:
   - `error` severity → fix before Phase 5. Same fix-now contract as 4.8.
   - `warning` severity → surface to user, proceed on approval.
   - **Non-interactive contract (v1):** If stdout is not a TTY (CI, cron, batch regeneration), `warning` findings default to fail-open-with-log (recorded in scorecard, shipcheck proceeds). Reviewer crashes (timeout, agent-budget exhaustion) map to `SKIP` with detail — shipcheck treats as informational, not blocking. No `--auto-approve-warnings` flag yet; revisit when a concrete cron path for shipcheck exists.

3. **Why agentic vs template-only** — rationale paragraph mirroring Phase 4.8's analogous subsection. Key argument: output-plausibility questions ("is butter a plausible substitute for buttermilk?") are not pattern-matchable against source; rule-based checks (R3) cover what regexes can; this phase covers the rest. The token cost is bounded (once per run, not per command) and the catch rate against the bug classes in the recipe-goat retro justifies it.

4. **Known blind spots** — explicit limitations so future readers don't expect more than it delivers:
   - Can't verify accuracy of numeric values (prices, ratings, rankings against ground-truth).
   - Can't detect data-freshness issues.
   - Can't judge subjective preferences (is this the "best" recipe?).
   - Sampled outputs only; full command-tree coverage belongs in Phase 5 dogfood.

**Polish-skill wiring:** The polish flow goes `skills/printing-press-polish/SKILL.md` → `cli-printing-press:polish-worker` agent → verify + dogfood + scorecard. Phase 4.85 invocation must land somewhere in that chain. Two viable placements, both acceptable; implementer picks:
- **Inside polish-worker** (edit `agents/polish-worker.md`): the worker runs Phase 4.85 as part of its own gate pipeline and feeds findings into its "fix-or-document" decision loop. Keeps polish's single-dispatch shape from the skill.
- **After polish-worker returns** (edit `skills/printing-press-polish/SKILL.md`): skill dispatches polish-worker, then dispatches Phase 4.85 as a separate step. Preserves polish-worker's scope but adds a second agent call from the skill.

Either gives already-shipped CLIs a review on every polish run — the backfill mechanism called for by the "forward-only review is a rollout gap" risk. Pick at implementation time based on the polish-worker's current complexity; if the worker is already bulky, prefer the post-worker option.

**Patterns to follow:**
- `skills/printing-press/SKILL.md` Phase 4.8 (lines 1677–1722) — four-section structural template, numbered checks, severity-based gating, explicit blind-spots list. Mirror the *exact* section names and ordering.
- Phase 5's "fix now" contract for `error`-severity findings.
- `skills/printing-press-polish/SKILL.md` existing verify-dispatch pattern — same agent-invocation shape.

**Verification:**
- New Phase 4.85 section parses as valid markdown (no broken headings, no YAML frontmatter disruption).
- Section structure matches Phase 4.8 exactly: Dispatch / Gate / Why agentic vs template-only / Known blind spots. No stray Test-scenarios block.
- When the skill is loaded and the flow reaches the shipcheck block, 4.85 fires after 4.8 and gates Phase 5.
- `/printing-press-polish` invokes Phase 4.85 alongside existing gates.
- **Calibration runs (during implementation, broader than original plan):**
  - **Targeted cases** (must match expected outcomes or iterate on prompt):
    - Pre-fix recipe-goat (before PR #68): Phase 4.85 surfaces findings on bugs #2 (sub buttermilk), #3 (trending silent drop), #4 (random URL). Expected: multiple `warning` findings.
    - Post-fix recipe-goat (after PR #68): Expected PASS.
    - hackernews-pp-cli (known-clean CLI): Expected PASS.
  - **Full library sweep** (all ~14 CLIs in `~/printing-press/library/`): measure false-positive rate and token cost across diverse domains (transactional APIs, document stores, scraping CLIs, data APIs). Record findings per CLI; any CLI with >2 false-positive findings triggers a prompt revision before landing. The two-CLI calibration in the original plan was too narrow — Phase 4.85's checks were designed from recipe-goat and need to be tested across domains they weren't designed for.
  - **Ship as WARN for Wave B:** Initial ship sets all findings to `warning` severity regardless of the prompt's internal severity classification. After 2 weeks of data from the full library, reclassify `error` severity as blocking in a follow-up PR if false-positive rate is <10%. See Phased Delivery below.

- [ ] **Unit 5: Structural test-presence gate + generator-seeded cliutil tests + Phase 3 skill gate**

**Goal:** Close the "zero tests shipped in generated CLIs" gap with three leverage points:
  - **(a) Template-level test seeding** — the generator emits a `cliutil_test.go` alongside the shared helpers so every CLI ships with at least the helpers covered.
  - **(b) Structural dogfood check** — `internal/pipeline/dogfood.go` gains a test-presence walker that flags pure-logic packages in the generated CLI with zero `_test.go` files. Converts the gate from advisory to structural.
  - **(c) Phase 3 skill prompt** — the main skill's Phase 3 completion checklist instructs the agent to write tests for novel pure-logic packages, backed by the dogfood check so half-hearted skipping fails shipcheck. (R5)

**Requirements:** R5

**Dependencies:** Unit 1 + Unit 2 (the template test file exercises `cliutil.FanoutRun` + `cliutil.CleanText`)

**Files:**
- Create: `internal/generator/templates/cliutil_test.go.tmpl` (new template emitting `internal/cliutil/cliutil_test.go`)
- Modify: `internal/generator/generator.go` (register the new template; emit alongside Units 1 & 2's cliutil templates)
- Modify: `internal/generator/generator_test.go` (add test that rendered CLI has `internal/cliutil/cliutil_test.go` and that `go test ./internal/cliutil/...` passes on the rendered CLI)
- Modify: `internal/pipeline/dogfood.go` — add a new per-package walker. *(Correction from initial plan: `dogfood.go:1271` is a single-directory, non-recursive helper (`listGoFiles`), not a package walker; `:1466` inside `checkConfigConsistency` flattens all `.go` files into a single slice. Neither groups by package. Unit 5 must build a new package-grouped walker that iterates `internal/*` subdirectories, groups `.go` files per package, detects `_test.go` presence per package, and scans for `cobra.Command{}` literals. Realistic size: 60–100 LOC plus tests, not ~40. The new code can reuse the existing dir-reading helpers but the grouping logic is net-new.)*
- Modify: `internal/pipeline/dogfood.go` — add `checkTestPresence` function following the shape of existing `checkDeadFlags` / `checkDeadFunctions` (takes `dir`, returns a result struct). A package is flagged when: under `internal/`, has exported functions (any `^func [A-Z]`), contains no `cobra.Command`-constructing code (grep for `cobra.Command{`, `&cobra.Command{`), and has zero `_test.go` files. Wire result into `DogfoodReport.Issues` on hit.
- Modify: `internal/pipeline/dogfood.go` — add `TestPresenceResult` struct and wire into `DogfoodReport` alongside the existing per-check results.
- Modify: `internal/pipeline/dogfood_test.go` — add table-driven cases: all-packages-have-tests (pass), pure-logic package missing test (fail), command package missing test (skip — not flagged), package with only unexported funcs (skip).
- Modify: `skills/printing-press/SKILL.md` Phase 3 Completion Gate (around line 1576) — the gate is a numbered 3-step list. Add step 4: "pure-logic packages under `internal/` have a `_test.go` with ≥1 table-driven happy-path test per exported function. `printing-press dogfood` now surfaces violations as structural issues — unaddressed violations fail shipcheck."

**Approach:**
- `cliutil_test.go.tmpl` covers `CleanText` (3–4 cases: clean input, entity decoding, whitespace, nested) and `FanoutRun` (3 cases: all-pass, mixed success+error, cancelled ctx). Table-driven style matching existing generator test conventions (`testify/assert` or stdlib `t.Errorf` — match whatever the existing captured_test.go.tmpl uses).
- Generator emits the new test file unconditionally when the cliutil package is emitted. No config guard — `internal/cliutil` is always emitted, so its test always compiles.
- Dogfood structural check: `dogfood.go` currently has `listGoFiles(dir)` at line 1271 (single-directory, non-recursive) and a flattened `filepath.Walk` at line 1466 (all `.go` files into a single slice). Neither is a per-package walker. Unit 5 adds a new helper that walks `internal/<pkg>/` subdirectories, groups `.go` files per package, counts `_test.go` files *and* test-function declarations per package (`^func Test[A-Z]`), and scans each package for `cobra.Command{}` literals. `checkTestPresence` consumes the grouped data and applies a two-tier check against pure-logic packages (exported funcs present, no `cobra.Command` usage):
  - **Zero `_test.go` files → dogfood error.** Flag as `"pure-logic package internal/recipes has 0 _test.go files"`. Fails shipcheck.
  - **Fewer than 3 `Test*` functions → warning surfaced to Phase 4.85.** Pass to Phase 4.85's agentic reviewer with context: `"package X has N tests covering M exported functions — verify test quality is proportional."` Phase 4.85's agent judges whether the thin coverage is acceptable (trivial wrapper package) or suspicious (agent wrote one-liner tests to pass the gate). This closes the "checkbox enforcement" weakness — trivial conformance triggers an agentic second look.
  - Output format matches existing `DogfoodReport.Issues` and a new `DogfoodReport.TestPresenceWarnings` slice for the Phase 4.85 pipe.
- Phase 3 gate: add to the existing Phase 3 Completion Gate bullet list (don't invent a new section). The gate is now structurally enforced by dogfood, not just by agent compliance — closes the "agent claims tests aren't useful" risk explicitly.

**Patterns to follow:**
- `internal/generator/templates/captured_test.go.tmpl` — existing precedent for emitted test templates.
- `internal/pipeline/dogfood.go` `checkDeadFlags` (`dogfood.go:~1271`) and `checkDeadFunctions` (`dogfood.go:~1466`) — the package-walk pattern. `checkTestPresence` should follow the same shape: walk, count, flag, return result struct.
- Existing generator tests that verify specific symbols are present in rendered output (pattern in `generator_test.go`).

**Test scenarios:**

*Template seeding (generator tests):*
- Happy path: rendering the cliutil templates + cliutil_test.go.tmpl produces a generated CLI where `go test ./internal/cliutil/...` passes.
- Happy path: rendered `internal/cliutil/cliutil_test.go` covers `CleanText` and `FanoutRun`.
- Edge case: template respects existing conditionals (e.g., auth-type variants) and doesn't break auth-less CLIs.

*Dogfood test-presence check (pipeline tests):*
- Happy path: CLI with all pure-logic packages tested → `checkTestPresence` returns 0 violations.
- Failure path: CLI with `internal/recipes` package containing exported funcs but no `_test.go` → 1 violation reported with package name and test-file count.
- Edge case: package with only unexported functions → not flagged (no public surface to test).
- Edge case: package containing `cobra.Command{}` (command wiring) → not flagged even if no tests (command glue is not pure-logic).
- Edge case: package with both exported funcs and cobra.Command → not flagged (mixed package, covered by command tests).
- Integration: run dogfood against recipe-goat pre-fix (before Unit 5) — expect violations reported for `internal/recipes` (which has `jsonld.go`, `subs.go`, etc. with no tests). Run against post-fix recipe-goat (with tests added for this plan) — expect 0 violations.

*Integration:*
- Regenerate one existing small CLI (weather-goat or similar) end-to-end; confirm `go test ./...` on the output passes and `dogfood` reports 0 test-presence violations.

**Verification:**
- `go test ./internal/generator/...` and `go test ./internal/pipeline/...` both pass.
- A freshly generated CLI has `internal/cliutil/cliutil_test.go`; `go test ./...` inside passes.
- `printing-press dogfood --dir <generated-cli>` surfaces `test_presence` violations for any pure-logic package lacking tests.
- Phase 3 SKILL.md Completion Gate bullet list contains the new item, and the gate is referenced from the shipcheck pre-flight checklist.

## System-Wide Impact

- **Interaction graph:** Generator templates (three new `cliutil_*.go.tmpl` files) touch every generated CLI by emitting a new `internal/cliutil/` package. Live-check is an existing consumer of command output that now applies stricter rules. Phase 4.85 is a new step in the skill's shipcheck flow *and* in `/printing-press-polish` — adds one agent dispatch per run in both paths. Dogfood gains a test-presence check within the existing package-walk (no new traversal cost).
- **Error propagation:** M3's new HTML-entity failure mode surfaces as a `LiveFeatureResult.StatusFail` with reason — existing scorecard gating already handles fail reasons. M4's Phase 4.85 error findings follow the same fix-before-ship contract as Phase 4.8. M5's new dogfood test-presence violations flow through the existing `DogfoodReport.Issues` array and the derived verdict path; a CLI missing tests on a pure-logic package will see this surface as an issue-list entry, not a silent pass.
- **State lifecycle risks:** None. All changes are evaluation-layer or template-layer; no new persistent state.
- **API surface parity:** Every generated CLI gains a new `internal/cliutil/` package with four exported symbols (`CleanText`, `FanoutRun`, `FanoutError`, `FanoutResult`, `FanoutReportErrors`) and options (`WithConcurrency`). These are purely additive; no existing helper signatures change. Agent-authored code in `package cli` is namespace-isolated from these (import via `cliutil.CleanText(...)`).
- **Scorer rollup effect:** M3 extends `LiveFeatureResult.Status` failure modes. Scorecard's live-check-derived pass rate already consumes pass/fail counts — a CLI with a previously-clean live-check that now fails on entity detection will see its scorecard live-check percentage drop, which could shift its Steinberger verdict. Calibrate by running scorecard against a sample of existing CLIs before merge to confirm the rule doesn't regress calibrated-A-grade CLIs into lower tiers. If regressions appear, either tighten the rule (numeric entities only) or ship with the rule as WARN-only in the first release.
- **Integration coverage:** Unit 5's "regenerate an existing CLI end-to-end and run `go test`" is the integration check that validates Units 1, 2, and 5 together. Unit 3's rule is exercised via `live_check_test.go` table-driven cases plus a manual recipe-goat regression pass. Unit 4's agentic review has no unit tests (SKILL.md prose); calibration against pre-fix and post-fix recipe-goat is the integration verification.
- **Unchanged invariants:** Verify's compile/vet/structural command tests are untouched. Dogfood's path-validity, auth, dead-code, wiring, novel-features checks are untouched — only the new test-presence check is added. The scorecard's existing dimensions keep their current weights; the live-check entity-detection rule is a fail-mode extension within an existing dimension, not a new dimension.

## Risks & Dependencies

| Risk | Mitigation |
|------|------------|
| M3's HTML-entity regex false-positives on legitimate JSON output (e.g., a URL-encoded query string that contains `&#`). | Gate the check to non-JSON-mode output. Scope the regex to numeric entities `&#\d+;` in v1; named entities only added if calibration against 5+ existing CLIs shows no false positives. |
| M4's agentic reviewer hallucinates plausibility concerns (flags correct output as suspect). | Tight prompt: "only findings a human would notice in 5 minutes." Calibration against known-clean CLI (hackernews) and known-broken CLI (pre-fix recipe-goat) before landing. Warning-severity-only for ambiguous findings. |
| M4 adds token cost to every run. | Runs once per run (not per command). Shipcheck already requires an API key; one more agent dispatch is marginal. Cost documented explicitly in the Phase 4.85 section. |
| **Existing published CLIs in `~/printing-press/library/` and `mvanhorn/printing-press-library` never pick up the new helpers automatically.** Silent ecosystem fork: new CLIs follow the R1/R2 patterns; old CLIs retain their inline fan-out and entity-unsafe scrape code. Breaks the "machine compounds learnings" thesis if left implicit. | **Accept drift explicitly as v1 policy.** Regeneration (emboss, republish) is the canonical update path — that's documented in AGENTS.md. Adding an `emboss --refresh-helpers` mode or `doctor`-time staleness detection is creep for v1; the right place for "library is drifting" signal is the library repo's own CI, not the tool. State the policy in the commit message and in the main skill's "What Changed" section. |
| **Phase 4.85 is forward-only on new runs — already-shipped CLIs never get re-reviewed.** Recipe-goat's own buttermilk bug would still be in the published library if the printed-CLI fix hadn't been manual. | **Wire Phase 4.85 into `/printing-press-polish`** so every polish run re-reviews the CLI's outputs. Polish is the canonical "revisit a shipped CLI" path and already dispatches verify + dogfood + scorecard; adding Phase 4.85 is one more step. Backfill path exists — no separate campaign needed. |
| **Phase 4.85 gate stalls in unattended contexts** if shipcheck runs from a CI/cron path without a user to approve warnings. A flaky reviewer (risk above) + no approver = wedged pipeline. Plan doesn't currently say how reviewer crashes (timeout, agent-budget) map to verdict. | **Non-interactive policy documented in Phase 4.85:** if stdout is not a TTY, `warning` findings default to fail-open-with-log (scorecard records them, shipcheck proceeds). Reviewer crashes map to `SKIP` with detail. No `--auto-approve-warnings` flag in v1 — no current cron path for shipcheck, don't build for hypothetical consumers. Revisit when a concrete use case appears. |
| **Symbol collision if agent-authored novel code defines a function with the same name as a generator-emitted helper** (e.g., an agent writes `cleanText` in `package cli` for unrelated purposes). Fatal compile error with no precedence guidance. | **Helpers moved to `internal/cliutil/` package** (revised from original "inline in helpers.go" decision). Call sites use `cliutil.CleanText` / `cliutil.FanoutRun`. Agents authoring novel code in `package cli` are namespace-isolated from the shared helpers. AGENTS.md glossary notes the `cliutil` package as generator-owned. |
| **Hardcoded FanoutRun concurrency=4 can't serve all call sites in one CLI** — a scrape-heavy CLI needs 4 (avoid 429s); an internal-API-only CLI could tolerate 32. A package-level const is set once and can't vary between call sites. | Expose via the functional-options pattern: `FanoutRun(ctx, sources, name, fn, WithConcurrency(n))`. Default remains 4 (matches `sync.go.tmpl` and is safe for scraping). Callers opt into higher concurrency per-call. Idiom is trivial now; painful to retrofit after callers adopt. |
| **Rate limiting is out of scope for `FanoutRun`** — recipe-goat already hits 429s on food52 and simplyrecipes under naive fan-out. Users may expect the helper to handle this. | Explicit doc comment: "Per-source rate limiting is the caller's responsibility; wrap `fn` with a limiter (e.g., `golang.org/x/time/rate`) if needed." Document recipe-goat's 429 observation as the canonical "why this is your problem" example. Baking backoff/jitter into the helper would be domain-specific and hard to tune. |
| Generic `FanoutRun[S, T any]` requires Go 1.18+; downstream CLI consumers might not support generics. | `go.mod.tmpl` pins `go 1.23`. Any consumer generating CLIs from this tool is already on modern Go. Document the Go-version requirement in the helper's doc comment. |

## Phased Delivery

The 5 units land in two waves to prevent a surge of coupled changes from hitting user trust simultaneously. Adversarial review raised this as a systemic risk — individual units look low-risk, but the aggregate ships 3 new gates + a new package + a skill phase on every future CLI at once. Staging gives calibration time.

### Wave A (first PR): Library additions + structural test-presence gate

**Lands together:** Units 1, 2, 5.

- Unit 1: `cliutil.FanoutRun`, `cliutil.FanoutReportErrors`, `cliutil.FanoutError`, `cliutil.FanoutResult`
- Unit 2: `cliutil.CleanText`
- Unit 5: `cliutil_test.go` template + dogfood test-presence walker (0-tests-error, <3-tests-warning path)

**Why together:** Units 2 and 5 depend on Unit 1's `internal/cliutil/` package existing. All three are additive to the generated CLI (new package, new dogfood check, new template). No scorecard-verdict impact — dogfood's test-presence check is new but a CLI with no tests fails dogfood rather than shipping a misleading A-grade scorecard.

**Exit criteria:** one existing CLI regenerated end-to-end compiles and passes `go test ./internal/cliutil/...`. No regression on any other dogfood check.

### Wave B (separate PR, after ≥1 week of Wave A in use): Output verification gates

**Lands together:** Units 3 and 4, both in **WARN mode** initially.

- Unit 3: live-check HTML entity detection — ships with `StatusWarn` (non-blocking). Findings recorded in scorecard, live-check pass-rate not affected.
- Unit 4: Phase 4.85 agentic output review — ships with all findings as `warning` severity regardless of the reviewer's internal classification. Surfaces findings; does not gate shipcheck.

**During WARN window (2 weeks):**
- Run the full library (~14 CLIs) through both gates. Measure:
  - Unit 3: how many CLIs trip the entity regex? Of those, is it real output bugs or false positives on legitimate JSON?
  - Unit 4: false-positive rate per domain class (transactional, document-store, scraping, data). Target: <10% false positives.
- Collect data in `docs/retros/` as a short calibration report.

### Wave C (third PR, post-calibration): Flip to FAIL

If Wave B calibration data shows <10% false positives and no A-grade CLI regressions:
- Unit 3: flip `StatusWarn` → `StatusFail` for entity detection.
- Unit 4: reclassify `error`-severity reviewer findings as blocking (matches the Phase 4.8 contract). `warning`-severity findings remain informational per the non-interactive policy.

If calibration shows problems:
- Narrow the rules (e.g., Unit 3 to numeric entities only with specific prefix classes; Unit 4 prompt revised to reduce false positives on specific domain classes).
- Extend WARN window until data supports the flip.

### Explicitly out-of-phase

- **Adoption enforcement for M1/M2 is deferred to a separate plan**, pending data from Wave A on actual agent usage of `cliutil.FanoutRun` and `cliutil.CleanText` in the first 3–5 CLIs generated after Wave A. If adoption is strong (≥80% of applicable commands use the helper), no enforcement needed. If adoption is weak, follow-up plan adds a dogfood check for reimplemented-fan-out patterns (grep `sync.WaitGroup` + channel patterns in aggregation commands without `cliutil` imports).

## Documentation / Operational Notes

- Update `AGENTS.md` glossary if any new terms warrant inclusion (unlikely — `cliutil` as generator-owned namespace is worth a one-liner; `FanoutRun` and `CleanText` are self-explanatory).
- Update SKILL.md Phase 3 (around line 1462, "Build The GOAT") to mention `cliutil.FanoutRun` and `cliutil.CleanText` as preferred patterns when the agent authors fan-out or scrape code.
- Update SKILL.md Phase 3 Completion Gate with the new test-seeding gate (Unit 5 covers this).
- `docs/retros/2026-04-13-recipe-goat-retro.md` remains the origin document; no update needed, but linking to this plan's PR in the retro's "status" line would be nice post-merge.

## Sources & References

- **Origin document:** [docs/retros/2026-04-13-recipe-goat-retro.md](../retros/2026-04-13-recipe-goat-retro.md)
- **Related PR (printed-CLI fixes, already open):** mvanhorn/printing-press-library#68
- **Related commits (context, already merged):**
  - `df242da` — scorecard --live-check (retro action #4, infrastructure this plan extends)
  - `caa283e` — synthetic spec kind (retro action #7)
  - `18cb521` — verify-skill + Phase 4.8 agentic SKILL reviewer (pattern this plan mirrors for Phase 4.85)
- **Key files:**
  - `internal/pipeline/live_check.go` — live-sampling infrastructure (extended by Unit 3)
  - `internal/pipeline/dogfood.go` — package walk at lines 1271 and 1466, extended by Unit 5 with test-presence check
  - `internal/generator/generator.go` — template registry, modified by Units 1/2/5 to emit the new cliutil templates
  - `internal/generator/templates/sync.go.tmpl` lines 108–148 — existing worker-pool pattern that Unit 1's FanoutRun mirrors for consistency
  - `internal/generator/templates/captured_test.go.tmpl` — existing precedent for generator-emitted test files
  - `internal/generator/templates/helpers.go.tmpl` — shared helpers (reference only — helpers are NOT added here in the revised plan)
  - `skills/printing-press/SKILL.md` — main orchestration skill (Phase 4.8 at line 1677, Phase 3 Completion Gate at line 1576); modified by Units 4 and 5
  - `skills/printing-press-polish/SKILL.md` — polish skill, modified by Unit 4 to invoke Phase 4.85 as a backfill path
  - `AGENTS.md` — machine-vs-printed-CLI triage rules; updated with `internal/cliutil/` as generator-owned namespace

## Revision history

- **2026-04-13 (initial):** Plan authored from recipe-goat retro findings and accepted M1-M4 triage.
- **2026-04-13 (deepened, interactive):** Three agents reviewed Implementation Units and Risks & Dependencies. Accepted findings integrated:
  - Unit 4 framing corrected to mirror Phase 4.8's exact four-section shape (Dispatch / Gate / Why agentic vs template-only / Known blind spots); removed contradictory Test-scenarios block.
  - Unit 5 promoted from "advisory" to structural via a new dogfood test-presence check (~40 LOC, reuses existing package-walk at dogfood.go:1271/1466).
  - Helpers relocated from inline `helpers.go.tmpl` to a new `internal/cliutil/` package after architecture review surfaced symbol-collision risk with agent-authored code in `package cli`.
  - FanoutRun signature revised to functional options (`WithConcurrency(n)`) after performance review — hardcoded default of 4 can't serve all call sites.
  - Concurrency contract (bounded jobs channel, source-order output, ctx-cancel semantics) specified in Unit 1's Approach.
  - Rate limiting explicitly scoped out of FanoutRun; recipe-goat 429s documented as canonical example in risks.
  - Phase 4.85 wired into `/printing-press-polish` as a backfill path for already-shipped CLIs.
  - Non-interactive policy (fail-open-with-log for warnings; SKIP for reviewer crashes) documented in Phase 4.85.
  - Five new risk rows added (drift, forward-only review, unattended stall, concurrency sizing, rate-limit scoping).
  - Scorer-rollup cross-boundary effect added to System-Wide Impact.
- **2026-04-13 (document-review auto-fixes):** Coherence + feasibility + adversarial reviews identified 7 auto-fixable accuracy issues. Applied:
  - **Unit 5 scope correction**: `dogfood.go:1271,1466` does not have an existing package walker (feasibility-reviewer grounded the correction by reading the code). `:1271` is a single-dir helper; `:1466` is a flattened file walk. Unit 5 must build a net-new package-grouped walker (60–100 LOC, not ~40). Plan text updated.
  - **Unit 4 Files list correction**: polish SKILL delegates to `polish-worker` agent; plan now lists both `polish/SKILL.md` and `agents/polish-worker.md` with the architectural-choice deferred to implementation (worker-embedded vs post-worker dispatch).
  - **Unit 3 function-name fix**: `checkFeature` → `runOneFeatureCheck` (actual function at live_check.go:239).
  - **Unit 1 test scenario fix**: `runtime.NumGoroutine` bound-check is flaky; replaced with atomic-counter wrap of `fn` that tests the contract directly.
  - **Unit 5 Phase 3 Gate fix**: gate is numbered 3-step list, not a bullet list. "Add a bullet" → "Add step 4".
  - **Terminology fix**: `cleanText` (camelCase) → `CleanText` (PascalCase exported symbol) in Documentation Notes.
  - **Open Question clarity fix**: "novel-code test seeding — no" → "no for novel code, yes for generator-owned helpers".
  - Adversarial review raised 7 premise-level findings (adoption enforcement, calibration basis, staged rollout) that were surfaced to the user as decisions rather than auto-applied.
- **2026-04-13 (post-review strategic integration):** User decisions on the 4 present-class findings integrated:
  - **Adoption enforcement for M1/M2**: accept soft-enforcement as v1; defer reimplementation-detector to a follow-up plan after observing adoption in the first 3–5 CLIs generated with the new helpers available. Documented in Phased Delivery's "Explicitly out-of-phase" section.
  - **Staged rollout**: new **Phased Delivery** section added with three waves — Wave A (Units 1, 2, 5 library additions + test-presence gate), Wave B (Units 3 + 4 as WARN for 2 weeks), Wave C (flip to FAIL after calibration). Unit 3 and Unit 4 Approach sections updated to reflect WARN-first behavior.
  - **Phase 4.85 calibration**: expanded from 2 CLIs (recipe-goat + hackernews) to full library sweep (~14 CLIs) measuring false-positive rate across diverse domains. Updated Unit 4 Verification.
  - **Test-presence gate quality**: hybrid enforcement — 0 `_test.go` files → dogfood error (fails shipcheck); 1-2 test functions → warning piped to Phase 4.85 for agentic review of thin coverage. Updated Unit 5 Approach with the two-tier check.
