---
title: Printing Press machine fixes from company-goat retro
type: fix
status: active
date: 2026-04-27
origin: PR mvanhorn/printing-press-library#140 follow-up commits 1fa0332 + 9a3b72d
---

# Printing Press machine fixes from company-goat retro

## Overview

The `company-goat-pp-cli` PR ([library #140](https://github.com/mvanhorn/printing-press-library/pull/140)) shipped with `feat(company-goat): add company-goat`, then required two follow-up commits to be usable:

- `1fa0332` — `fix(company-goat): inline --domain/--pick flag declarations + use [co] in Use strings`
- `9a3b72d` — `fix(company-goat): handle SEC throttling and agent output` (transplanted ~228 lines of rate-limiting machinery into `internal/source/sec/sec.go`)

Both fixes were systemic, not SEC-specific. Each surfaced a machine gap that would recur on every future synthetic combo CLI. This plan addresses all seven gaps found in the analysis. The work splits into four landings; the first is the highest leverage and unblocks the static checks that catch the rest.

- **U1: Promote rate-limit primitives to `internal/cliutil/ratelimit.go`.** The generated `internal/client/client.go` has a private `adaptiveLimiter` that hand-written sibling clients (`internal/source/<name>/`, `internal/recipes/`, `internal/phgraphql/`, etc.) cannot reuse. Without a shared exported primitive, agents either reinvent rate-limiting per source or skip it entirely — the latter is what happened to SEC EDGAR. Promote `AdaptiveLimiter`, `RateLimitError`, `RetryAfter`, and capped exponential `Backoff` into `cliutil`. Update `client.go.tmpl` to use the public types.
- **U2: Source-aware reimplementation/throttle check.** `internal/pipeline/reimplementation_check.go` only inspects `internal/cli/*.go` — it never opens `internal/source/<name>/*.go` (or any sibling internal package whose `*.go` makes outbound HTTP calls). Extend the check to scan sibling internal packages and warn when a file makes outbound HTTP without using `cliutil.AdaptiveLimiter` or surfacing `*cliutil.RateLimitError`. After U1 lands, this is a string-match check.
- **U3: verify-skill follows one level of helper indirection.** `internal/cli/verify_skill_bundled.py`'s `flag_declared_in` regex matches direct `cmd.Flags().StringVar(&x, "name", ...)` calls only. A combo CLI with 14 commands sharing a `--domain`/`--pick` flag pattern can't satisfy this without inlining the same declaration 14 times. Teach the verifier to follow one-level helper indirection: when the command file calls `addXxxFlags(cmd, &t)` and the helper's body declares the flag, count it as declared on the command.
- **U4: SKILL Phase 3 + 4 rules.** Add Principle 10 to the Agent Build Checklist (every per-source HTTP client uses `cliutil.AdaptiveLimiter` and surfaces `RateLimitError`, never empty results). Add a sub-rule to Principle 8 (positional-OR-flag commands use `[brackets]` not `<angles>`). Add a per-source row to the Phase 4 dogfood matrix guidance (with limiter exhausted, command surfaces a typed error, not empty success).

U1 must land before U2 (U2's static check string-matches `cliutil.AdaptiveLimiter`, which only exists after U1). U3 and U4 are independent. Recommended sequence: U1 → U2 → U3 → U4.

---

## Problem Frame

The two follow-up commits in PR #140 surface seven distinct machine gaps. Verified diagnoses against `internal/` source at HEAD:

### Gap A — `adaptiveLimiter` is private to `internal/client/`

`internal/generator/templates/client.go.tmpl:46` declares `type adaptiveLimiter struct` (lowercase, unexported). The `Wait`, `OnSuccess`, `OnRateLimit`, `Rate` methods, the `newAdaptiveLimiter` constructor, and the `retryAfter` helper (line 804) are all unexported and live in `package client`. Hand-written sibling packages cannot import them.

Concrete impact in PR #140: `internal/source/sec/sec.go` had `c.HTTP.Do(req)` with no limiter, no 429 retry, no `Retry-After` parsing. SEC throttling silently produced empty Form D results — identical in shape to "this company has no filings." The fix transplanted ~228 lines duplicating the limiter into the source package.

Beyond company-goat: `recipe-goat/internal/recipes/archive.go:172` returns a plain `"CDX API rate limited (HTTP 429)"` error string with no retry; `producthunt/internal/phgraphql/client.go` likewise has no shared limiter import path. Every synthetic combo CLI hits this.

### Gap B — `reimplementation_check` is `internal/cli`-shaped

`internal/pipeline/reimplementation_check.go:142-181` only walks `internal/cli/*.go`. The `hasSiblingInternalImport` helper (added in retro #350) at line 382-393 detects whether a `cli/*.go` file *imports* a sibling internal package — but that import is treated as a positive signal that *vindicates* the command. The check never opens the sibling package itself to verify what's there.

PR #140 documents this honestly: *"the dogfood reimplementation_check heuristic only inspects internal/client and produces 7/7 false positives on this CLI."* Result: 7 commands flagged as suspicious that were actually fine, while the actual problem (SEC source missing rate-limit handling) went undetected.

### Gap C — `verify` never exercises 429 / throttling

`grep -n "429\|RateLimit\|Retry-After\|throttl\|rate.limit" internal/pipeline/verify.go` returns zero matches. Live verify hits the real API once and PASSes if exit code is 0, which is exactly what SEC's "I rate-limited you and you swallowed it" path produces. The published proof file recorded `verify 100%` on a CLI whose killer feature returned empty results under load.

After U1 lands, this becomes a static check: "the source file calls `cliutil.AdaptiveLimiter.Wait` and treats `*cliutil.RateLimitError` as a hard failure (not a `continue` in a loop, which is the exact pattern in the original `SearchAndFetchAll`)." We fold this into U2 rather than building 429 fault injection — the static check delivers the same protection for far less complexity.

### Gap D — SKILL Phase 3 has no per-source-client rule

The 9-principle Agent Build Checklist (`skills/printing-press/SKILL.md:1614-1633`) covers command-shape issues only — verify-friendly RunE, side-effect convention, `--json`, exit codes, `dryRunOK`. It says nothing about the shape of the per-source HTTP client itself. The "absence-of-correctness" delegation rule (line 1663) is the closest match but misses this case: empty results that *look* correct but actually mean throttling.

### Gap E — Full-dogfood test matrix has no source-level row

The agent who ran the full dogfood matrix on company-goat tested every command + `--json` + error paths but didn't ask "for each named source, when its limiter exhausts, is the output distinguishable from a legitimate empty?" That row doesn't exist in the matrix because Phase 4 derives rows from the command tree, not from data sources.

### Gap F — verify-skill rejects shared flag helpers

`internal/cli/verify_skill_bundled.py:79` declares `FLAG_DECL_RE = re.compile(r'(Persistent)?Flags\(\)\.(StringVar|...)P?\(&[^,]+,\s*"([a-z][a-z0-9-]*)"')`. The regex requires the flag to be declared by a literal `cmd.Flags().StringVar(&..., "name", ...)` call. When 14 commands share a `--domain`/`--pick` flag and use `addTargetFlags(cmd, &t)` (whose body has the literal `cmd.Flags().StringVar(...)`), the regex finds the declaration in `helpers.go` but `flag_declared_in(cmd_files, flag)` (line 600) only sees the command's own file and reports "declared elsewhere but not on \<path\>" (line 611).

Fix in PR #140 was to inline 14 declarations — busywork driven entirely by the static analyzer's limit, not by an actual CLI bug.

### Gap G — `Use:` string ergonomics for "positional OR flag"

`Use: "snapshot <co>"` declared `<co>` as required, but the command actually accepts `<co>` OR `--domain` (and validates one of them inside RunE). `verify-skill`'s `check_positional_args` (line 625) parses `<co>` as required and flags `<cli> snapshot --domain example.com` recipes as missing positional args. Fix in PR #140 changed Use to `[co]` (optional). Any combo CLI that resolves an entity by either positional or flag hits the same trap.

---

## Architecture Decisions

**Promote to `cliutil`, not a new top-level package.** The existing `internal/cliutil/` package is already the canonical home for cross-source helpers (`fanout.go`, `text.go`, `verifyenv.go`, `probe.go`). Adding `ratelimit.go` is the smallest change that doesn't introduce a new package. AGENTS.md already names cliutil as "the generator-owned Go package emitted into every printed CLI." This is the natural extension.

**Public types, not factory functions.** `cliutil.NewAdaptiveLimiter(rate)` returns `*cliutil.AdaptiveLimiter` (exported struct, exported methods). `cliutil.RateLimitError` is an exported struct so callers can `errors.As`. `cliutil.RetryAfter(resp)` and `cliutil.Backoff(attempt)` are package-level helpers. No interfaces — the concrete struct is the contract.

**Generated `client.go` keeps the same behavior.** After U1, `client.go.tmpl` switches from `*adaptiveLimiter` to `*cliutil.AdaptiveLimiter`. The behavior is byte-equivalent: same default rates (`floor` set by `RateLimit` from spec or fallback), same `OnRateLimit` halving, same `OnSuccess` ramp-up, same `Wait` semantics. Goldens regenerate but only for import + type-name changes.

**Static check, not fault injection.** Gap C originally wanted verify to inject 429s. After U1 promotes the limiter to a public symbol, the static check "does this file call `cliutil.AdaptiveLimiter.Wait`" is sufficient — it forces every per-source client onto the shared retry path. Fault injection would have required mock-server infrastructure for every API and a way to declare "this source's primary endpoint is X." Static check is one regex.

**verify-skill helper indirection: AST-light, regex-heavy.** Don't add a Go AST parser to the Python verifier. Instead, when a flag isn't declared directly in the command file but is referenced in a helper call (e.g., `addXxxFlags(cmd, ...)`), look up the helper by name in any `internal/cli/*.go` file and check whether its body contains a `Flags().StringVar(...)` call for the flag. One level of indirection only — no recursive lookup.

**Phase 3 SKILL rules are short.** Principle 10 is one sentence + one code example. Principle 8's `[brackets]` sub-rule is one sentence. The Phase 4 matrix row is one bullet. Long prose is the wrong shape for build checklists; agents skim them.

---

## U1 — Promote rate-limit primitives to `internal/cliutil/ratelimit.go`

### Goal

Every printed CLI ships with `internal/cliutil/ratelimit.go` exporting `AdaptiveLimiter`, `RateLimitError`, `RetryAfter`, and `Backoff`. The generated `internal/client/client.go` uses these public types. Hand-written sibling packages can `import "<modulePath>/internal/cliutil"` and use the same primitives.

### Files Changed

**New file:**
- `internal/generator/templates/cliutil_ratelimit.go.tmpl`

**Modified:**
- `internal/generator/templates/client.go.tmpl` — drop private `adaptiveLimiter` struct + methods + `retryAfter` helper; switch field type to `*cliutil.AdaptiveLimiter`; update call sites; add cliutil import.
- `internal/generator/templates/cliutil_test.go.tmpl` — add tests for new types.
- `internal/generator/generator.go:1015-1019` — register the new template in `renderSingleFiles`.
- `testdata/golden/cases/generate-golden-api/artifacts.txt` — add `internal/cliutil/ratelimit.go`.
- `testdata/golden/expected/generate-golden-api/printing-press-golden/internal/cliutil/ratelimit.go` — new fixture.
- `testdata/golden/expected/generate-golden-api/printing-press-golden/internal/client/client.go` — regenerated fixture.

### Implementation Sketch

`cliutil_ratelimit.go.tmpl` exports:

```go
package cliutil

// AdaptiveLimiter mirrors the limiter previously private to internal/client.
// Hand-written sibling packages (internal/source/<name>/, internal/recipes/,
// internal/phgraphql/, etc.) MUST use this primitive instead of rolling their
// own — the printing-press dogfood reimplementation check enforces this.
type AdaptiveLimiter struct { /* same fields as private adaptiveLimiter */ }

func NewAdaptiveLimiter(ratePerSec float64) *AdaptiveLimiter
func (l *AdaptiveLimiter) Wait()
func (l *AdaptiveLimiter) OnSuccess()
func (l *AdaptiveLimiter) OnRateLimit()
func (l *AdaptiveLimiter) Rate() float64

// RateLimitError signals that the upstream returned 429 after retries were
// exhausted. Callers MUST surface this as a hard error — not as empty results
// — because empty-on-throttle is indistinguishable from "no data exists" and
// silently corrupts downstream queries (company-goat retro, PR #140).
type RateLimitError struct {
    URL        string
    RetryAfter time.Duration
    Body       string
}
func (e *RateLimitError) Error() string

// RetryAfter parses an HTTP Retry-After header (seconds or HTTP-date) capped
// at 60s. Returns 5s when the header is missing or malformed.
func RetryAfter(resp *http.Response) time.Duration

// Backoff returns a deterministic exponential-with-jitter wait for retry
// attempt N (0-indexed). Capped at 30s to keep tests bounded.
func Backoff(attempt int) time.Duration
```

`client.go.tmpl` changes:
- Line 36: `limiter *adaptiveLimiter` → `limiter *cliutil.AdaptiveLimiter`.
- Lines 42-125: delete the private `adaptiveLimiter` struct, methods, and `newAdaptiveLimiter` constructor — replaced by cliutil.
- Lines 226, 236: `newAdaptiveLimiter(rateLimit)` → `cliutil.NewAdaptiveLimiter(rateLimit)`.
- Lines 384, 479, 492, 494, 384: `c.limiter.Wait()` → unchanged (method moved, not renamed). Same for `OnSuccess`, `OnRateLimit`, `Rate`.
- Line 493: `retryAfter(resp)` → `cliutil.RetryAfter(resp)`.
- Lines 802-826: delete the private `retryAfter` helper + `maxRetryWait` constant — moved to cliutil.
- Imports: add `"<modulePath>/internal/cliutil"`.

### Tests

In `cliutil_test.go.tmpl`, add cases:
1. `TestAdaptiveLimiter_RampsUpAfterSuccesses` — call `OnSuccess()` rampAfter+1 times, assert `Rate()` increased by 1.25×.
2. `TestAdaptiveLimiter_HalvesOnRateLimit` — set rate, call `OnRateLimit()`, assert rate is halved and ceiling stored.
3. `TestAdaptiveLimiter_FloorAtZeroPointFive` — repeatedly call `OnRateLimit()`, assert rate never drops below 0.5.
4. `TestRateLimitError_ErrorMessage` — verify the `Error()` string includes URL and RetryAfter.
5. `TestRetryAfter_Seconds` — `Retry-After: 10` → 10s.
6. `TestRetryAfter_HTTPDate` — `Retry-After: <future date>` → ~delta.
7. `TestRetryAfter_Cap` — `Retry-After: 600` → capped at 60s.
8. `TestRetryAfter_Missing` → 5s default.
9. `TestBackoff_DoublesPerAttempt` — `Backoff(0)`=1s, `Backoff(1)`≈2s, etc., capped at 30s.

Add a generator test in `internal/generator/client_test.go` (or extend an existing test):
- `TestClientGo_ImportsCliutil` — render `client.go.tmpl` against a stock spec; assert the output imports `<modulePath>/internal/cliutil` and uses `*cliutil.AdaptiveLimiter`.

### Golden Update

Run `scripts/golden.sh update` after the changes. Expected diffs:
- `printing-press-golden/internal/cliutil/ratelimit.go` — new file (~150 lines).
- `printing-press-golden/internal/client/client.go` — import diff, type-name diffs, ~80 fewer lines (limiter + retryAfter removed).
- `printing-press-golden/internal/cliutil/cliutil_test.go` — new test cases (only if the test fixture is part of artifacts.txt; check first).

Inspect each diff manually. Document in the final summary which goldens changed and why.

### Risks

- **Existing printed CLIs in `~/printing-press/library/` will regenerate to a different shape on the next run.** That's expected and desired; they currently lack the limiter and shouldn't be the source of truth for the machine.
- **The unexported `adaptiveLimiter` symbol disappears from `client.go`.** Any code outside the generator that references it would break — `grep` confirms no such code exists in this repo.
- **Behavior parity is structural, not byte-equivalent.** Methods are identical but field zero-values may differ if the type is renamed. Verify with the `TestAdaptiveLimiter_*` tests.

### Acceptance

- `go test ./...` passes.
- `scripts/golden.sh verify` passes (or update fixtures + explain).
- A regenerated golden CLI's `internal/client/client.go` imports `cliutil` and references `*cliutil.AdaptiveLimiter`.
- A regenerated golden CLI's `internal/cliutil/ratelimit.go` exists and contains the four exported symbols.

---

## U2 — Source-aware reimplementation/throttle check

### Goal

`reimplementation_check` extends to inspect sibling internal packages (`internal/source/`, `internal/recipes/`, `internal/phgraphql/`, etc. — anything not in the `reservedInternalPackages` set). For each `*.go` file in those packages that makes outbound HTTP calls, emit a finding when the file does not use `cliutil.AdaptiveLimiter` (Wait/OnSuccess/OnRateLimit) AND does not surface `*cliutil.RateLimitError`.

### Files Changed

**Modified:**
- `internal/pipeline/reimplementation_check.go` — extend `ReimplementationCheckResult` with a `SiblingClients` field; add `checkSiblingClients` that walks non-reserved internal packages.
- `internal/pipeline/dogfood.go` — render the new findings in the report; thread them through verdicts.
- `internal/pipeline/dogfood_test.go` — fixture-based tests for the new check.

### Implementation Sketch

In `reimplementation_check.go`, add:

```go
type SiblingClientFinding struct {
    Package string `json:"package"`
    File    string `json:"file"`
    Reason  string `json:"reason"` // "no rate limiter" | "swallows 429"
}

// httpCallRe matches outbound HTTP calls in any package shape.
var httpCallRe = regexp.MustCompile(
    `\b(http\.(Get|Post|NewRequest|Do)|c\.(Do|Get|Post)|HTTPClient\.Do|HTTP\.Do)\s*\(`,
)
// limiterUseRe — at least one of these must be present in a sibling package
// that makes outbound HTTP calls.
var limiterUseRe = regexp.MustCompile(`cliutil\.(AdaptiveLimiter|NewAdaptiveLimiter)\b|\.OnRateLimit\s*\(|\.OnSuccess\s*\(`)
// rateLimitErrorRe — either the cliutil type or a local 429-handler that returns
// a typed error (not a continue or a swallow).
var rateLimitErrorRe = regexp.MustCompile(`cliutil\.RateLimitError\b|RateLimitError\b`)

func checkSiblingClients(cliDir string) []SiblingClientFinding {
    // Walk internal/<pkg>/*.go for pkg not in reservedInternalPackages and
    // not 'cli' (already covered by command-level check).
    // For each *.go that matches httpCallRe, require limiterUseRe AND
    // rateLimitErrorRe. Emit a finding otherwise.
}
```

Wire into `checkReimplementation`'s caller (`dogfood.go:260`). Add a verdict row that warns when `len(SiblingClients) > 0`. Match the existing pattern at `dogfood.go:1332`.

### Tests

`internal/pipeline/reimplementation_check_test.go`:
1. `TestSiblingClient_PassesWithLimiter` — fixture with HTTP call + `cliutil.AdaptiveLimiter` + `cliutil.RateLimitError` returns no findings.
2. `TestSiblingClient_FlagsMissingLimiter` — fixture with HTTP call + no limiter → one finding with `reason: "no rate limiter"`.
3. `TestSiblingClient_FlagsSwallow429` — fixture with HTTP call + limiter but no `RateLimitError` propagation → one finding with `reason: "swallows 429"`.
4. `TestSiblingClient_IgnoresReservedPackages` — adds a file in `internal/store/` (which is a reserved package); assert no finding.
5. `TestSiblingClient_NoFileNoFinding` — no sibling internal packages → no findings.

### Acceptance

- `go test ./internal/pipeline/...` passes.
- Running dogfood against company-goat at HEAD before the SEC fix (recreate the broken state in a fixture) emits one finding for `internal/source/sec/sec.go: no rate limiter`.
- Running dogfood against company-goat *after* the SEC fix (current state, with the inlined limiter) emits zero findings — the check accepts both the cliutil import path *and* the local-copy pattern as legitimate, because the local copy still uses `RateLimitError` and a Wait-shaped limiter. (Long-term, after agents migrate to cliutil, the local-copy path becomes unused.)

### Risks

- **False positives** on packages that do non-HTTP work but happen to have an `http.Get` in a comment or a test fixture. Filter test files (`*_test.go`) and use a tight-enough regex.
- **False negatives** on packages with novel HTTP shapes (e.g., a package that wraps `*http.Request` in another struct). Acceptable — the check is a heuristic, like the existing reimplementation check. Document the regex; new shapes can be added.
- **Existing CLIs without limiters in their sibling packages** will start generating warnings. That is the goal — not a risk. Document in the U2 commit so users running `printing-press dogfood` against a 6-month-old CLI understand what changed.

---

## U3 — verify-skill follows one-level helper indirection

### Goal

`flag_declared_in(cmd_files, flag)` returns true when the flag is declared either directly in `cmd_files` OR by a helper called from `cmd_files` whose body declares it. One level only — no recursive resolution.

### Files Changed

**Modified:**
- `internal/cli/verify_skill_bundled.py` (and the byte-identical `scripts/verify-skill/verify_skill.py`).
- `internal/cli/verify_skill_test.go` — add test cases.

### Implementation Sketch

After the existing `flag_declared_in(cmd_files, flag)` call in `check_flag_commands` (line 600), add a fallback that:

1. Scans `cmd_files` for calls matching `(\w+)\(\s*(?:cmd|cmd\b)`. Captures helper names invoked with `cmd` as first arg.
2. For each helper name, finds the helper's body in any `internal/cli/*.go` file (defined as `func <name>(`).
3. Runs `FLAG_DECL_RE` over the helper's body. If the flag matches, count it.

Pseudocode:

```python
def flag_declared_via_helper(cli_dir, cmd_files, flag_name):
    helper_call_re = re.compile(r'\b([a-zA-Z_]\w*)\s*\(\s*cmd\b')
    helper_names = set()
    for f in cmd_files:
        for m in helper_call_re.finditer(f.read_text()):
            helper_names.add(m.group(1))
    if not helper_names:
        return False
    for go_file in (cli_dir / "internal/cli").glob("*.go"):
        text = go_file.read_text()
        for name in helper_names:
            if not re.search(rf'\bfunc\s+{re.escape(name)}\s*\(', text):
                continue
            # Extract a generous window after the func declaration.
            # We don't need to brace-match precisely; FLAG_DECL_RE on a
            # 2KB window is enough to catch every realistic helper.
            for m in re.finditer(rf'func\s+{re.escape(name)}\s*\([^)]*\)\s*\{{', text):
                window = text[m.end():m.end()+2000]
                for fm in FLAG_DECL_RE.finditer(window):
                    if fm.group(3) == flag_name:
                        return True
    return False
```

Wire it in `check_flag_commands`:

```python
if cmd_files and flag_declared_in(cmd_files, flag):
    continue
if persistent_flag_declared(cli_dir, flag):
    continue
if flag_declared_via_helper(cli_dir, cmd_files, flag):  # NEW
    continue
# ... existing finding emission
```

Sync the bundled and canonical scripts (the byte-identical-hash test in `internal/cli/release_test.go` will fail otherwise).

### Tests

In `internal/cli/verify_skill_test.go`:
1. `TestVerifySkill_FlagDeclaredViaHelper_OneLevel` — fixture with `addTargetFlags(cmd, &t)` in command file and the helper body in `helpers.go` with `cmd.Flags().StringVar(&t.domain, "domain", ...)`. Assert no finding for `--domain`.
2. `TestVerifySkill_FlagNotDeclaredAnywhere` — neither in command file nor helper. Assert one finding.
3. `TestVerifySkill_HelperWithoutCmdArg` — helper called as `addThing(other)` (no `cmd`); should not be considered. Assert appropriate behavior (probably one finding if the flag is otherwise undeclared).

### Acceptance

- `go test ./internal/cli/...` passes.
- The bundled and canonical script hashes match (`TestVerifySkillScriptInSync` passes).
- Re-running `verify-skill` on company-goat *before* the inlining fix produces zero `flag-commands` findings for `--domain` and `--pick`.

### Risks

- **False positives** if a helper's body declares the flag for an unrelated command (e.g., `addRootFlags(cmd)` declaring `--root-only` but verify-skill thinks it applies to a leaf). The window-based approach can't tell. Mitigation: log the helper name in the verdict so a reviewer can confirm; keep the severity at `error` so the agent investigates.
- **Window cutoff** — 2KB is enough for every helper observed in printed CLIs, but a contrived helper longer than that could escape detection. Acceptable.

---

## U4 — SKILL Phase 3 + Phase 4 rules

### Goal

Add three small rules to `skills/printing-press/SKILL.md`:

1. **Principle 10** in the Agent Build Checklist: every per-source HTTP client uses `cliutil.AdaptiveLimiter` and surfaces `cliutil.RateLimitError`, never empty results.
2. **Principle 8 sub-rule**: positional-OR-flag commands MUST use `[brackets]` not `<angles>`.
3. **Phase 4 dogfood matrix row**: per source, verify that limiter exhaustion produces a typed error, not empty success.

### Files Changed

**Modified:**
- `skills/printing-press/SKILL.md` — three small additions in the Agent Build Checklist (around line 1614) and the Phase 4 dogfood matrix guidance.

### Implementation Sketch

Add Principle 10 after Principle 9 (line 1633), short and direct:

```
10. **Per-source rate limiting**: any hand-written client in `internal/source/<name>/`,
    `internal/recipes/`, or any other sibling internal package that makes outbound HTTP
    calls MUST use `cliutil.AdaptiveLimiter` and return `*cliutil.RateLimitError` from
    its public methods when 429 retries are exhausted. Returning empty results on
    throttle is a silent corruption — downstream queries can't tell "the source has no
    data" from "the source rate-limited us." Reference: company-goat retro, PR #140.
```

Add a sub-bullet under Principle 8 (line 1623), short:

> Commands that accept either a positional `<x>` OR a flag `--y` as alternatives MUST declare `Use: "<cmd> [x]"` (square brackets, not angle brackets) and validate "exactly one of x or --y" inside RunE. Required positionals declared with angle brackets break verify-skill recipes that use the flag-only form.

Add to the Phase 4 dogfood-matrix guidance (around line 1751 — `verify-skill` discussion). Rather than burying it there, add a new short subsection after "Full dogfood means everything":

> **Per-source row for combo CLIs.** When a synthetic combo CLI lists N named data sources (`internal/source/<name>/`, `internal/recipes/`, etc.), the dogfood test matrix MUST add one row per source: with the limiter at floor rate and 429s injected (or the upstream genuinely throttling), assert that the user-facing command surfaces a typed error referencing the source — not empty JSON / `0 results`. A passing row says: "the CLI distinguishes 'no data' from 'we got rate-limited' for this source." Dogfood without this row passed company-goat with SEC EDGAR silently broken; PR #140 caught it only after publish.

### Tests

`skills/` doesn't have unit tests, but `internal/generator/skill_test.go` does render-time tests on the SKILL template. None of the changes here are template-driven (they're prose in the human-authored SKILL), so no Go test is required. Add a SKILL.md format check via `go vet` or markdown linter only if one already exists (it doesn't — confirmed by repo grep).

### Acceptance

- `git diff skills/printing-press/SKILL.md` shows three small additions in the right sections.
- A future run on a synthetic combo CLI surfaces all three rules during Phase 3 review.

### Risks

- **SKILL bloat** — these add ~25 lines total. The existing checklist is already long; agents skim it. Mitigation: keep each rule to 1-3 sentences with an explicit reference back to the company-goat retro for context.

---

## Sequencing & Dependencies

```
U1 (cliutil/ratelimit) ───┬─→ U2 (source-aware check) ───→ ship
                          │
                          └─→ U4 (SKILL rules ref cliutil)

U3 (verify-skill helper)  ──────────────────────────────→ ship  (independent)
```

- U2's static check string-matches `cliutil.AdaptiveLimiter` — so U1 must merge first.
- U4's Principle 10 names `cliutil.AdaptiveLimiter` — same constraint.
- U3 has no dependency on the others; can land first or last.

Recommended PR order: U1 → U2 → U3 → U4. Each PR follows commit-style `fix(cli):` or `fix(skills):` per AGENTS.md.

## Out of Scope

- **Verify mode that injects 429s.** Originally Gap C; folded into U2 as a static check. Real fault injection requires per-API mock servers and a way to declare "this source's primary endpoint is X" in the spec. Out of scope for this plan; revisit if static-check false-negatives become a problem.
- **Fixing existing published CLIs in `~/printing-press/library/`.** This plan touches the machine; published CLIs regenerate on the next run. Manual backports are user-driven, not machine-driven.
- **Renaming `internal/source/` to a generator-recognized convention.** Today, sibling internal packages can be named anything (`source/`, `recipes/`, `phgraphql/`, `atom/`). U2's check is regex-based and works for all of them, so no rename is needed.
- **Promoting more helpers to cliutil.** Only rate-limiting primitives are promoted in this plan. `cleanText`, `fanout`, `verifyenv`, `probe`, `freshness` are already there. If future retros find more shared shapes (e.g., HTML extraction), promote them in their own plans.

## Acceptance — Plan-level

- `go test ./...` passes after all four landings.
- `scripts/golden.sh verify` passes (with U1's expected fixture updates).
- A regenerated golden CLI shows `internal/cliutil/ratelimit.go` and a `client.go` that imports it.
- Running dogfood on a synthetic CLI fixture without limiters in `internal/source/<x>/` emits a `SiblingClientFinding`.
- `verify-skill` on a CLI with `addTargetFlags(cmd, &t)` indirection does not emit `flag-commands` findings.
- SKILL.md contains Principles 8-sub, 10, and the per-source matrix row.
