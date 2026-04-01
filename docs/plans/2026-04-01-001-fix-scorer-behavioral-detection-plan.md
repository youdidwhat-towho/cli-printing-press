---
title: "fix: Scorer behavioral detection — path validity, insight/workflow, dogfood false positives"
type: fix
status: active
date: 2026-04-01
origin: docs/retros/2026-03-31-steam-run2-retro.md
---

# Fix: Scorer Behavioral Detection

## Overview

Fix 5 scoring tool issues that collectively cost ~17 points on every CLI. The theme: scorers should detect behavior, not filenames or naming patterns. Path validity samples the wrong files. Insight and workflow scoring checks filename prefixes instead of what commands actually do. Dogfood misses method-receiver flag usage and intra-file function calls.

## Problem Frame

The Steam CLI scored 70/100 but analysis showed ~17 points were lost to scorer detection bugs, not CLI quality gaps. The CLI has real insight commands (`playtime.go` with playtime distributions, `completionist.go` with cross-game achievement rates), real workflows (`profile.go` with 5 API calls, `compare.go` with library comparison), and correct spec-derived paths — but the scorers can't see them because they check filenames and naming patterns instead of code behavior.

(see origin: docs/retros/2026-03-31-steam-run2-retro.md)

## Requirements Trace

- R1. Path validity scores >0/10 when generated commands contain spec-derived `path :=` assignments
- R2. Insight detection finds commands that produce derived/aggregated output regardless of filename
- R3. Workflow detection finds commands that combine 2+ API calls regardless of filename
- R4. Dead-flag detection matches `f.<name>` method receiver pattern, not just `flags.<name>`
- R5. Dead-function detection recognizes intra-file calls within helpers.go

## Scope Boundaries

- Not changing generator templates (that's a separate plan — findings #4, #5, #10 from the retro)
- Not changing the store template for domain-specific Search (retro finding #8)
- Not changing verify's auth env var handling (retro finding #11)
- Not changing the sync path params bonus (retro skip — scorer design choice, not bug)

## Context & Research

### Relevant Code and Patterns

**Scorecard (`internal/pipeline/scorecard.go`):**
- `evaluatePathValidity()` at ~line 987: samples 10 files via `sampleCommandFiles()`, extracts paths with regex `\bpath\s*(?::=|=|:)\s*"([^"]+)"`, compares via `specPathExists()`
- `scoreInsight()` at ~line 814: primary=filename prefix list (16 terms), secondary=store+SQL aggregation
- `scoreWorkflows()` at ~line 728: primary=filename prefix list (25 terms), secondary=multi-API-call detection (2+ `c.Get`/`c.Post`)
- `scoreTypeFidelity()` at ~line 1290: checks `var _ = strings.ReplaceAll` markers for -1pt

**Dogfood (`internal/pipeline/dogfood.go`):**
- `checkDeadFlags()` at ~line 365: post-PR #100, includes root.go but filters `&flags.` declarations. Searches for `flags.<name>` — misses `f.<name>` method receiver.
- `checkDeadFunctions()` at ~line 413: skips helpers.go entirely. Misses calls like `bold()` calling `colorEnabled()` within the same file.

**Existing tests:**
- `scorecard_artifacts_test.go`: creates temp CLI dirs, generates scorecard JSON, asserts on scores
- `dogfood_test.go`: mock root.go/helpers.go fixtures, asserts DeadFlags/DeadFuncs counts

### Institutional Learnings

- PR #100 established the pattern: trace the scorer's code path, prove it's wrong, fix the tool. Same approach here.
- AGENTS.md: table-driven tests with testify/assert. Run `go test ./...` before considering work done.

## Key Technical Decisions

- **Insight/workflow: behavioral detection as primary, prefixes as supplementary** — The existing prefix lists stay as a fallback signal but are no longer the primary detection method. Behavioral patterns (multi-API calls, derived output, store usage + aggregation) become primary. Rationale: prefix lists create a naming game; behavioral detection measures actual capability. The prefix list still catches simple cases where the filename is the behavior (e.g., `analytics.go`).

- **Insight behavioral signals: expand beyond SQL keywords** — The current store+aggregation detection only matches SQL keywords (`COUNT(`, `GROUP BY`). Real insight commands aggregate in Go code (`sort.Slice`, percentage calculations, `len()` comparisons). Expand the pattern set to include Go-level aggregation while keeping SQL patterns.

- **Path validity: scan all command files, not 10** — The current 10-file sample is too small when wrapper commands dilute the pool. Scanning all files is cheap (just regex on file contents) and eliminates the sampling bias. Cap at a reasonable maximum if performance is a concern.

- **Dogfood dead-flag: match any struct accessor, not just `flags.`** — The rootFlags struct is accessed as `flags` in `Execute()` and as `f` in method receivers. The fix should match `\.<fieldName>\b` in root.go after filtering declarations, rather than requiring `flags.` prefix.

## Open Questions

### Resolved During Planning

- **Should we remove the prefix lists entirely?** No — keep them as supplementary signals. Some filenames are genuinely indicative (`analytics.go`, `sync.go`). The change is making behavioral detection primary, not removing prefix detection.
- **Will expanding insight detection cause false positives?** Unlikely — the behavioral signals require BOTH multi-source input AND derived output. A simple GET-and-display command won't match. Test with negative cases.

### Deferred to Implementation

- **Exact regex patterns for Go-level aggregation** — the retro suggests `sort.Slice`, `* 100`, `/ total`, `len()` comparisons. The exact pattern set needs tuning during implementation against real CLI code.
- **Threshold calibration for insight/workflow** — the current thresholds (6+ for 10/10 insight, 7+ for 10/10 workflow) may need adjustment if behavioral detection finds more commands than prefix detection did.

## Implementation Units

- [ ] **Unit 1: Fix path_validity sampling**

**Goal:** Path validity scans all command files instead of sampling 10, so generated commands with spec-derived paths are always found

**Requirements:** R1

**Dependencies:** None

**Files:**
- Modify: `internal/pipeline/scorecard.go` (`evaluatePathValidity`, `sampleCommandFiles`)
- Test: `internal/pipeline/scorecard_test.go` or `scorecard_artifacts_test.go`

**Approach:**
- Replace `sampleCommandFiles(dir, 10)` with scanning all `.go` files in `internal/cli/` (excluding infrastructure files from `infraCoreFiles`)
- Keep the same path extraction regex and `specPathExists()` comparison
- If performance is a concern, cap at 50 files — but scanning 200 files with a regex is negligible

**Patterns to follow:**
- The `scoreInsight()` and `scoreWorkflows()` functions already scan all files in the directory — reuse that pattern

**Test scenarios:**
- Happy path: CLI with 150 generated commands + 20 wrappers → path_validity finds paths in generated commands, scores >0
- Happy path: CLI with only wrapper commands (no `path :=` assignments) → scores 0 (correct, no paths to validate)
- Edge case: Empty CLI dir → scores 0 without panic
- Integration: Generate from Steam spec → path_validity >0/10

**Verification:**
- Run scorecard on Steam CLI → path_validity >0/10
- Run scorecard on an existing Stripe/GitHub CLI → score unchanged or improved

---

- [ ] **Unit 2: Rewrite insight scoring to detect behavior**

**Goal:** `scoreInsight()` detects commands that produce derived/aggregated output, not just commands with matching filenames

**Requirements:** R2

**Dependencies:** None

**Files:**
- Modify: `internal/pipeline/scorecard.go` (`scoreInsight`)
- Test: `internal/pipeline/scorecard_test.go` or `scorecard_artifacts_test.go`

**Approach:**
- Keep the existing prefix list as a supplementary signal
- Add behavioral detection as primary: a command counts as "insight" if it matches ANY of:
  - (a) Filename matches an insight prefix (existing — kept as supplementary)
  - (b) Uses store AND has aggregation patterns (existing — expand pattern set)
  - (c) Makes 2+ API calls AND produces derived output (NEW)
- Expand aggregation patterns beyond SQL to include Go-level: `sort.Slice`, `* 100`, `/ total`, percentage patterns, `fmt.Sprintf("%.`, `len(` used in arithmetic context
- Deduplicate: a command matching multiple signals counts once

**Patterns to follow:**
- `scoreWorkflows()` already does multi-API detection — reuse that pattern for insight

**Test scenarios:**
- Happy path: File named `playtime.go` with 2 API calls + `sort.Slice` + percentage calculation → detected as insight
- Happy path: File named `analytics.go` with store usage → detected via prefix (backward compatible)
- Happy path: File named `completionist.go` with store + `* 100` calculation → detected via behavioral pattern
- Edge case: File named `trends.go` with only `return cmd.Help()` → NOT detected (behavior required, prefix alone insufficient for empty commands... actually the current behavior counts this, and we keep prefixes as supplementary, so this WOULD count. Note this in the test as "prefix-only detection still works for backward compat")
- Negative: Simple GET-and-display command (one API call, no derived output) → not counted

**Verification:**
- Run scorecard on Steam CLI → insight ≥8/10 (was 6/10)
- Run scorecard on a CLI with no insight commands → still scores 0

---

- [ ] **Unit 3: Rewrite workflow scoring to detect behavior**

**Goal:** `scoreWorkflows()` detects commands that combine multiple operations, not just commands with matching filenames

**Requirements:** R3

**Dependencies:** None (can run in parallel with Unit 2)

**Files:**
- Modify: `internal/pipeline/scorecard.go` (`scoreWorkflows`)
- Test: `internal/pipeline/scorecard_test.go` or `scorecard_artifacts_test.go`

**Approach:**
- Keep existing prefix list as supplementary
- Make multi-API detection primary: a command counts as "workflow" if it matches ANY of:
  - (a) Filename matches a workflow prefix (existing — kept)
  - (b) Makes 2+ distinct API calls in same RunE (existing — promote from secondary to primary)
  - (c) Uses store AND makes 1+ API call (store-backed workflow)
- The multi-API detection already exists at lines 776-795 — the change is making it count toward the total alongside prefix matches, not as a separate secondary check

**Test scenarios:**
- Happy path: File named `profile.go` with 5 `c.Get` calls → detected as workflow via multi-API
- Happy path: File named `compare.go` with 2 player lookups → detected via multi-API
- Happy path: File named `sync.go` → detected via prefix (backward compat)
- Negative: File with single `c.Get` call and no store → not counted

**Verification:**
- Run scorecard on Steam CLI → workflows ≥9/10 (was 8/10)

---

- [ ] **Unit 4: Fix dogfood dead-flag method receiver detection**

**Goal:** Dead-flag detection matches `f.<name>` and any other struct accessor pattern, not just `flags.<name>`

**Requirements:** R4

**Dependencies:** None

**Files:**
- Modify: `internal/pipeline/dogfood.go` (`checkDeadFlags`)
- Test: `internal/pipeline/dogfood_test.go`

**Approach:**
- Currently searches for `flags.<fieldName>` in all files including root.go (after filtering declarations)
- Change the search pattern to match `\.<fieldName>\b` — any struct accessor followed by the field name
- This catches `flags.noCache`, `f.noCache`, `rf.noCache`, or any other variable name
- The declaration filter (`&flags.`) still works to exclude registration lines

**Patterns to follow:**
- PR #100's approach: filter declarations, then search for usage. Extend the usage pattern.

**Test scenarios:**
- Happy path: Mock root.go with `func (f *rootFlags) newClient()` using `f.noCache` → `noCache` NOT reported dead
- Happy path: Mock root.go with `flags.agent` in PersistentPreRunE → `agent` NOT reported dead (existing, should still work)
- Happy path: Flag declared but never accessed via any pattern → IS reported dead
- Edge case: Field name appears in a string literal (`"noCache"`) → should NOT count as usage (word boundary helps)

**Verification:**
- Run dogfood on Steam CLI → 0 false dead-flag warnings
- Dogfood test suite passes

---

- [ ] **Unit 5: Fix dogfood dead-function intra-file detection**

**Goal:** Dead-function detection recognizes calls within helpers.go from other functions in the same file

**Requirements:** R5

**Dependencies:** None

**Files:**
- Modify: `internal/pipeline/dogfood.go` (`checkDeadFunctions`)
- Test: `internal/pipeline/dogfood_test.go`

**Approach:**
- Currently skips helpers.go entirely when scanning for function usage
- Change to: include helpers.go in the scan, but for each function being checked, exclude lines that are the function's own definition (the `func funcName(` line)
- One approach: build a "usage source" from helpers.go by removing all `func <name>(` definition lines, then search that for call patterns
- This catches `colorEnabled()` being called by `bold()` within helpers.go

**Patterns to follow:**
- PR #100's dead-flag fix used the same pattern: include the file but filter out declarations. Apply the same logic to dead-function detection.

**Test scenarios:**
- Happy path: helpers.go with `func bold() { if colorEnabled() { ... } }` → `colorEnabled` NOT reported dead
- Happy path: helpers.go with `func deadHelper()` never called by any function → IS reported dead
- Happy path: helpers.go with chain: `compactFields → compactListFields → ...` → none reported dead
- Edge case: Function name appears only in a comment → should still be reported dead

**Verification:**
- Run dogfood on Steam CLI → 0 false dead-function warnings (was 6)
- Dogfood test suite passes

## System-Wide Impact

- **Scorecard changes (Units 1-3):** Affect all future scorecard runs. Existing CLIs may score differently — behavioral detection may find more insight/workflow commands than prefix detection did, so scores could increase for CLIs that were unfairly penalized. No CLI should score worse.
- **Dogfood changes (Units 4-5):** Affect all future dogfood runs. Dead-flag and dead-function counts will decrease for CLIs with the standard helper patterns. No CLI should get more warnings.
- **Unchanged invariants:** Scorecard JSON output schema unchanged. Dogfood `DeadCodeResult` struct unchanged. Verify behavior unchanged. Generator output unchanged.

## Risks & Dependencies

| Risk | Mitigation |
|------|------------|
| Behavioral insight detection causes false positives | Require BOTH multi-source input AND derived output. Test with negative cases (simple pass-through commands). |
| Expanded regex patterns are too broad | Start with specific patterns (`sort.Slice`, `* 100`, `/ total`) and expand incrementally based on results |
| Path validity scanning all files is slow | Unlikely — regex on 200 files is negligible. Cap at 100 if needed. |
| Dogfood `\.<field>\b` pattern matches unrelated struct fields | The search is scoped to root.go and the field names come from `rootFlags` declarations — false matches with other structs are unlikely since the field names are specific (`noCache`, `rateLimit`). |

## Sources & References

- **Origin document:** [docs/retros/2026-03-31-steam-run2-retro.md](docs/retros/2026-03-31-steam-run2-retro.md)
- **Previous fix:** PR #100 (mvanhorn/cli-printing-press#100) — established the pattern of tracing scorer bugs and fixing the tool
- Scorecard code: `internal/pipeline/scorecard.go`
- Dogfood code: `internal/pipeline/dogfood.go`
- Existing tests: `internal/pipeline/scorecard_artifacts_test.go`, `internal/pipeline/dogfood_test.go`
