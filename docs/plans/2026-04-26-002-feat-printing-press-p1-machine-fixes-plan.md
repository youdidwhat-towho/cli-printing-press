---
title: "feat: Printing Press P1 machine fixes (doctor transport, no-auth gen, html noscript, dryRun helper)"
type: feat
status: active
date: 2026-04-26
origin: https://github.com/mvanhorn/cli-printing-press/issues/333
---

# feat: Printing Press P1 machine fixes (doctor transport, no-auth gen, html noscript, dryRun helper)

## Overview

Four independent P1 work units from the allrecipes retro (issue #333). Each fixes a generator template or scorer that produced extra hand-edits during the allrecipes generation:

- **U1** — generated `doctor` reachability probe currently uses stdlib `http.Client`, ignoring `http_transport` and silently failing on Cloudflare-fronted CLIs. Move it onto the same Surf-based client every other command uses.
- **U2** — generator emits `internal/cli/auth.go` even when the spec declares `auth.type: none`, leaving a dead `auth set-token / status / logout` UI. Skip emission and registration; exempt the scorer's auth dimension.
- **U3** — `extractHTMLLink`'s `nodeText` includes raw `<img>` markup from `<noscript>` subtrees because HTML parsers preserve raw-text-mode tag content as TextNodes. Skip `noscript`/`script`/`style`/`template` subtrees and surface the first nested image URL on a new field.
- **U4** — hand-written transcendence commands need a `--dry-run` short-circuit + must avoid `Args: cobra.MinimumNArgs(N)` and `MarkFlagRequired(...)` for verify compatibility. Emit a `dryRunOK` helper and document the pattern in the SKILL.

Reinforces (not weakens) the Surf TLS-impersonated transport everywhere it's already used.

---

## Problem Frame

Generated CLIs fronted by Cloudflare/Akamai/Vercel-WAF (e.g., `allrecipes`, `recipe-goat`, future Dotdash sites) ship with friction the Printing Press could eliminate at generation time:

1. The `doctor` command lies about connectivity because its reachability probe uses stdlib HTTP, ignoring the `http_transport: browser-chrome` directive that every other command honors. Users running `doctor` against a working CLI see "API unreachable" — a false negative that erodes trust in the doctor signal.

2. Specs declaring `auth.type: none` (public-data APIs, recipe sites, public score feeds) ship with a fully-wired `auth set-token / status / logout` subcommand that does nothing useful. Worse, the scorer docks the auth dimension because the auth subcommand "isn't doing the right thing" — penalizing CLIs that correctly reflect their no-auth spec.

3. The HTML-extract `mode: links` parser silently leaks raw `<img>` markup into result fields whenever sites wrap above-the-fold images in `<noscript>` (Allrecipes, Food Network, NYT, BBC Good Food — every Dotdash/Meredith property does this). The defect is genuine: `nodeText` walks all descendants, including `<noscript>` subtrees, which the html parser preserves as raw TextNodes per spec. Every HTML-scrape CLI ships with a custom search-card parser to work around this.

4. Hand-written transcendence commands (the work that lands in Phase 3 of every printing-press generation) consistently fail verify's `--dry-run` probe because authors use `Args: cobra.MinimumNArgs(N)` or `MarkFlagRequired(...)`. Cobra evaluates both before RunE runs, so a `--dry-run` guard in the command body cannot reach. The polish-worker rewrites the same 3-line pattern on every CLI; the cycle is wasted because the underlying generator/skill never tells the implementing agent the right pattern.

All four are repeating across the catalog. Each fix is small, well-bounded, and independent.

---

## Requirements Trace

- R1. Generated `doctor` reachability probe uses the same HTTP client as the rest of the CLI (currently `flags.newClient()` returns a Surf-wrapped `*client.Client`). Doctor accurately reports reachability for any spec value of `http_transport`.
- R2. Doctor distinguishes "transport is reaching but blocked by interstitial" from "transport failed entirely" — the diagnostic names the protection vendor (Cloudflare/Akamai/Vercel/AWS WAF) so the user has actionable context.
- R3. Generator does not emit `internal/cli/auth.go` when the spec's `auth.type` is `"none"` AND no traffic-analysis browser hint applies AND no GraphQL persisted-query state exists.
- R4. Generator does not register `newAuthCmd(&flags)` in `root.go` when auth.go was not emitted.
- R5. Scorecard's auth dimension exempts no-auth specs from the "no auth subcommand" deduction; scoring reflects "this CLI correctly has no auth," not "this CLI is missing auth."
- R6. HTML-extract link mode skips `<noscript>`, `<script>`, `<style>`, `<template>` subtrees when computing anchor text. Result fields contain only rendered text.
- R7. HTML-extract `htmlLink` struct surfaces the first nested `<img src=...>` URL on a separate field (e.g., `Image`).
- R8. Generated CLIs include a `dryRunOK(cmd, flags) bool` helper in the cmd helpers file (named so collisions with hand-written code are unlikely) that returns true when `flags.dryRun` is set, signaling the caller to short-circuit.
- R9. The Printing Press SKILL Phase 3 build instructions explicitly document the verify-friendly RunE pattern: no `Args: cobra.MinimumNArgs(N)`, no `MarkFlagRequired(...)`; check inside RunE; fall through to `cmd.Help()` for help-only invocations; check `dryRunOK` before any IO.
- R10. Existing CLIs (notion, github, stripe, recipe-goat, etc.) regenerate without behavioral regressions. The four template/scorer/skill changes maintain backward compatibility for specs that don't trigger the new guards.

---

## Scope Boundaries

- Not refactoring the doctor command's overall structure or output schema. Only the HTTP probe and the credential validation client are swapped.
- Not adding new auth flows or scorecard dimensions. Only making existing auth code paths conditional and exempting the auth dimension when no auth applies.
- Not redesigning the HTML extraction pipeline. Only adding raw-text-mode tag suppression and one new field on `htmlLink`.
- Not auto-rewriting hand-written commands across already-shipped CLIs. WU-4 emits a helper for new generations and documents the pattern for the agent; existing CLIs are unaffected unless they regenerate.
- Not changing how Surf or `http_transport` is selected, configured, or passed through. The transport story is unchanged; doctor just uses it.

### Deferred to Follow-Up Work

- WU-5 (Insight scorer content detection), WU-6 (TypeFidelity regex flag-type expansion), WU-7 (data-pipeline scorer content detection), WU-8 (move provenance helpers to subpackage) — separate plan, not part of this set. P2 priority.

---

## Context & Research

### Relevant Code and Patterns

**WU-1 (doctor)**
- `internal/generator/templates/doctor.go.tmpl` — current reachability probe at lines 19 (`net/http` import), 152-157 (`httpClient := &http.Client{}` + HEAD probe), 167 (GET on health endpoints), 220 (auth credentials probe). All four call sites use the stdlib client.
- `internal/generator/templates/client.go.tmpl` — defines the client.Client wrapper and its constructor; this is the consumer side of `flags.newClient()` (the function lives in `root.go.tmpl`).
- `internal/cli/root.go.tmpl` — `flags.newClient()` factory. Returns a `*client.Client` configured with the spec's transport (Surf when `http_transport` is `browser-chrome` or `browser-chrome-h3`).
- The client.Client exposes `Get(path, params)` and `GetWithHeaders(path, params, headers)` — both go through Surf when configured.
- Reference: I already executed this fix manually for the allrecipes CLI (see `~/printing-press/library/allrecipes/internal/cli/doctor.go`). The hand-edit replaced the stdlib block with `c, err := flags.newClient()` + `c.Get("/search", ...)`. The same pattern generalizes: pick a known-public path from the spec or use `/` as a fallback probe.
- Cloudflare / Akamai / Vercel / AWS WAF interstitial detection: existing helper `looksLikeHTMLChallenge` in `internal/generator/templates/html_extract.go.tmpl:280` already lists the relevant body markers. Reuse that detector or factor it into a shared helper.

**WU-2 (auth-none)**
- `internal/generator/generator.go:1244` (`renderAuthFiles()`) — current code unconditionally renders one of three auth templates. The "always render" comment on line 1241 ("`Always render auth command - use full OAuth2 template...`") is the load-bearing assumption to break. The function already has an exception for `auth_browser.go.tmpl` when traffic-analysis hints `graphql_persisted_query`; the new exit path adds an early-return for `auth.type: none` AND no such hint.
- `internal/generator/templates/root.go.tmpl:200` — `rootCmd.AddCommand(newAuthCmd(&flags))`. Wrap in `{{if .Auth.IsAuthenticated}}` (or equivalent already-exposed predicate).
- `internal/spec/spec.go:Auth` struct — already has `Type` field. Existing helpers like `g.Spec.Auth.Type == "none"` are the right guard.
- `internal/pipeline/scorecard.go:scoreAuth` (lines 295-336) — currently penalizes when `authContent` is empty (line 304: `if authContent != "" { score += 1 }` — missing auth = 0 instead of +1). Also penalizes when `os.Getenv` count is zero in config.go. The fix: when the parsed spec declares `auth.type: none`, skip the per-signal scoring and return the full 10/10 (or auto-award the "free grant" + give credit for the secure-permissions and config-load signals that DO apply to no-auth CLIs).
- The function reads four files but never reads the spec. The cleanest fix takes the spec (or its parsed auth struct) as an additional argument so the no-auth exemption can fire deterministically.
- Looking at the call site at `scorecard.go:114` (`sc.Steinberger.Auth = scoreAuth(outputDir)`), the surrounding context (the broader `Score` function) already has access to the spec — so we can plumb it through.

**WU-3 (html-extract noscript)**
- `internal/generator/templates/html_extract.go.tmpl:246-255` — `nodeText(n)` walks all descendants and concatenates TextNode `Data`. The defect is that html parsers (golang.org/x/net/html) preserve `<noscript>`, `<script>`, `<style>`, `<template>` content as raw TextNode children of those parents (per HTML spec — these elements have CDATA-like content models). Skip these children before the recursion descends.
- `walkHTML` at line 217 is the recursion primitive. Either fix `walkHTML` to honor a "skip children of these tags" rule, or add a `nodeTextSkipping` variant that's local to `extractHTMLLink`.
- `htmlLink` struct at lines 33-39 — current fields: `Rank`, `Name`, `Text`, `URL`, `Slug`. Add `Image string `json:"image,omitempty"`` and populate it from the first descendant `<img>` element's `src` attribute (skipping `<noscript><img>` because that subtree is being suppressed).
- `attrValue(n, "src")` already exists at line 240 and is the reuse path.

**WU-4 (dryRunOK helper + SKILL)**
- `internal/generator/templates/helpers.go.tmpl` — current shared helpers file emitted into `internal/cli/helpers.go`. Add a small `dryRunOK(cmd *cobra.Command, flags *rootFlags) bool` returning `flags.dryRun` (cheap; named so the call site is self-documenting).
- The doctor template (post-WU-1) and the generated `*_search.go` / `*_get.go` already short-circuit on `flags.dryRun` — keep their behavior; the new helper standardizes the idiom for hand-written commands.
- `skills/printing-press/SKILL.md` — Phase 3 starts at line 1522 ("Phase 3: Build The GOAT"). The "Agent Build Checklist (per command)" section at line 1575 lists 7 principles; add an 8th: "**Verify-friendly RunE:** no `Args: cobra.MinimumNArgs(N)`, no `MarkFlagRequired(...)`; first line of RunE checks `len(args) == 0` → `cmd.Help()`, second checks `dryRunOK(cmd, flags)` → `nil`."
- Reference for the template structure: `cmd_helpers.go` I emitted by hand for allrecipes (`~/printing-press/library/allrecipes/internal/cli/cmd_helpers.go`). The existing `helpers.go.tmpl` is a cleaner host than introducing a new `cmd_helpers.go.tmpl` file; keep the surface compact.

### Institutional Learnings

- `docs/retros/2026-04-13-recipe-goat-retro.md` (already shipped) flagged a related symptom: hand-written commands repeatedly fail verify because they require positional args. WU-4 closes that gap.
- `docs/plans/2026-04-13-002-feat-cloudflare-cli-learnings-plan.md` — established the "browser-chrome transport for Dotdash/Cloudflare-fronted sites" pattern. WU-1's fix lets the doctor honor that decision instead of silently bypassing it.
- `docs/plans/2026-04-19-004-feat-auth-doctor-plan.md` — adjacent work on the printing-press binary's `auth doctor` subcommand, not the CLI's own `doctor`. Different scope; mention here so a reviewer doesn't conflate them.

### External References

None required. All fixes are repo-internal generator/scorer/skill changes with established patterns.

---

## Key Technical Decisions

- **Doctor reuses `flags.newClient()` rather than constructing a parallel Surf client.** Reuses existing transport configuration, rate limiter, cache, and request decoration. The CLI is the source of truth for "how does this CLI talk to the API"; doctor must match.
- **Cloudflare interstitial detection is post-fetch body inspection, not transport-layer.** When Surf reaches the wall (rare but possible on flagged IPs), the response is still a 200 with the JS challenge HTML. Detect by body markers, not by status code. The `looksLikeHTMLChallenge` helper already exists in `html_extract.go.tmpl` and lists the right markers; promote it to a shared `internal/cliutil/` helper or duplicate-and-pin in the doctor template.
- **No-auth exemption is plumbed via spec to the scorer, not detected by file absence.** The scorer currently asks "does `auth.go` exist? does config.go have `os.Getenv`?" These are file-presence proxies. The principled fix is to ask the spec: `if spec.Auth.Type == "none" → exempt`. Plumbing requires `scoreAuth` to take the spec (or auth struct) as an arg.
- **HTML extract suppression list is fixed, not configurable.** `noscript / script / style / template` are the four HTML5 elements that browsers parse as raw text. There's no domain-specific tuning here — every site that uses noscript image fallbacks benefits, none are penalized. Don't expose as spec config.
- **`htmlLink.Image` is populated with the first non-suppressed `<img src>`.** If the visible image is rendered at top level (no noscript wrap), it's captured. If it's only inside `<noscript>` (the suppressed subtree), `Image` stays empty — that's correct, the spec author can declare a custom html_extract path for the alt-text or fallback if they want it.
- **`dryRunOK` is a function, not a struct method.** Lives alongside the existing helpers in `helpers.go.tmpl`. Hand-written commands import nothing extra; they just call `dryRunOK(cmd, flags)` as the second line of RunE.
- **SKILL Phase 3 update is additive, not restructuring.** Add one bullet to the existing 7-principle checklist + a small "Verify-friendly RunE template" snippet. Don't reorganize.

---

## Open Questions

### Resolved During Planning

- *Should doctor probe a known-public path from the spec or always probe `/`?* — Use `/` as the default probe (works for any base URL). For specs that declare a `verify_path` for credential validation, reuse that path for credential probes only. Reachability stays on `/`.
- *Should the no-auth exemption skip ALL signals or only the missing-file penalty?* — Skip the entire dimension scoring path; return 10/10 with a `note: "auth.type:none — exempted from auth scoring"` field in the JSON output. Mixed-signal scoring tempts future maintainers to add new auth signals that punish no-auth CLIs again.
- *Can the htmlLink Image field be empty in valid output?* — Yes. Many anchor-link cards have no nested `<img>` (text-only listings). Empty is a valid signal, not an error.
- *Does the SKILL change need any backward-compat treatment for in-flight Phase 3 work?* — No. The bullet adds a recommendation; existing implementations that don't follow it still work, they just need polish-worker to fix dry-run plumbing. WU-4 reduces polish-worker churn for new generations.

### Deferred to Implementation

- *Exact placement of `looksLikeHTMLChallenge` (shared cliutil vs. doctor-template-local).* — Defer; either works. The shared cliutil placement is cleaner architecturally but introduces a cross-package dependency; the local placement is duplicative but self-contained. Implementer picks based on whether the existing `internal/cliutil/` package is already imported by the doctor's surrounding code.
- *Whether to apply the `dryRunOK` helper to the generated `*_search.go` / `*_get.go` handlers.* — Defer. Those handlers already short-circuit on `flags.dryRun`; converting them to use the helper is consistency churn that should land separately if at all.
- *Whether `scoreAuth`'s "free grant" of +2 (currently a TODO) should be removed in this same change.* — Defer. The free grant is unrelated to no-auth handling; removing it is a separate scoring rebalancing that warrants its own plan.

---

## High-Level Technical Design

> *This illustrates the intended approach and is directional guidance for review, not implementation specification. The implementing agent should treat it as context, not code to reproduce.*

The four units modify three files in the templates and one in the scorer plus one skill markdown. Their relationships:

```
Generator templates                                         Scorer
─────────────────────                                       ──────
doctor.go.tmpl    ────────────────U1                        scorecard.go ──U2
  uses flags.newClient (post-U1)                              scoreAuth(spec) (post-U2)
                                                             gates on spec.Auth.Type=="none"

root.go.tmpl      ────────────────U2
  conditional newAuthCmd register
  (gated by IsAuthenticated)

helpers.go.tmpl   ────────────────U4
  emits dryRunOK helper

html_extract.go.tmpl ─────────────U3
  walkHTML or local skipping variant
  htmlLink.Image new field
  htmlLink.Name uses suppressed-walk

generator.go      ────────────────U2
  renderAuthFiles early-return
  on auth.type:none

skills/printing-press/SKILL.md ───U4
  Phase 3 build checklist +1 bullet
  + Verify-friendly RunE example
```

The doctor's HTTP probe shifts from:

    httpClient := &http.Client{Timeout: 5*time.Second}
    headResp, err := httpClient.Do(headReq)

to (directional sketch):

    c, err := flags.newClient()
    body, err := c.Get("/", nil)
    if err != nil { /* transport failed entirely */ }
    if looksLikeHTMLChallenge("", string(body)) {
        /* transport reached, interstitial blocked us — name vendor */
    }

The auth gate is a single early-return at the top of `renderAuthFiles`:

    if g.Spec.Auth.Type == "none" &&
       !g.hasTrafficAnalysisHint("graphql_persisted_query") &&
       g.Spec.Auth.AuthorizationURL == "" {
        return nil  // no auth.go emitted
    }

The HTML extract suppression rule, in `nodeText` or its caller:

    walkHTML(n, func(child *xhtml.Node) {
        if child.Type == xhtml.ElementNode && isRawTextTag(child.Data) {
            return  // SKIP this node and its subtree
        }
        if child.Type == xhtml.TextNode {
            parts = append(parts, child.Data)
        }
    })
    where isRawTextTag(name) -> name in {"noscript","script","style","template"}

The dryRunOK helper:

    func dryRunOK(_ *cobra.Command, flags *rootFlags) bool {
        return flags != nil && flags.dryRun
    }

Each of these is small and self-contained. The plan separates them into four units so they can land as independent commits and so a reviewer can validate each piece in isolation.

---

## Implementation Units

- U1. **Doctor template uses configured transport**

**Goal:** Replace the stdlib `http.Client` reachability probe in `doctor.go.tmpl` with calls through `flags.newClient()` so the doctor honors `http_transport` and accurately reports connectivity for browser-chrome / browser-chrome-h3 specs. Add Cloudflare/Akamai/Vercel-WAF interstitial detection for the case where Surf reaches the wall.

**Requirements:** R1, R2, R10

**Dependencies:** None.

**Files:**
- Modify: `internal/generator/templates/doctor.go.tmpl`
- Modify (likely): `internal/cliutil/` — promote `looksLikeHTMLChallenge` from `html_extract.go.tmpl` to a shared cliutil helper if not already there. May not be needed if the doctor template duplicates the markers locally; implementer's call.
- Test: `internal/generator/doctor_test.go` (extend if exists; otherwise add a focused regen-and-assert test)

**Approach:**
- Remove stdlib `net/http` import unless still used elsewhere in the template (e.g., for credential validation; that path also moves onto `flags.newClient` per R1).
- Replace the `httpClient := &http.Client{}` block with `c, err := flags.newClient()`. Handle error → mark API as "client init error" with the underlying error message.
- Replace the `httpClient.Do(headReq)` HEAD probe with `body, err := c.Get("/", nil)`. The reachability question is "did the request return a non-error response," not "did it return a specific status."
- For the credential validation block (when auth is configured): replace `httpClient.Do(authReq)` with `c.GetWithHeaders(verifyPath, nil, map[string]string{authHeaderName: authHeader})`. The Authorization header injection moves from manual `req.Header.Set` to the client's headers param.
- After a successful fetch, inspect the body for known interstitial markers using `looksLikeHTMLChallenge`. When the body matches: report "blocked by `<vendor>` interstitial — the configured transport reached the wall. Try a different network or check the browser-chrome transport binding." Distinguish vendor by which marker matched (cloudflare / akamai / vercel / aws waf / datadome).
- Do NOT add any stdlib fallback path. If `flags.newClient()` returns an error, propagate it. If `c.Get()` returns an error, report it. The user's transport choice is honored end-to-end.

**Patterns to follow:**
- The existing hand-edit in `~/printing-press/library/allrecipes/internal/cli/doctor.go` (post-Phase-4 of the allrecipes generation) is the working reference. Generalize it into the template.
- `looksLikeHTMLChallenge` at `internal/generator/templates/html_extract.go.tmpl:280` lists the markers.
- Existing client-aware code in `internal/generator/templates/sync.go.tmpl` (or any sync command) — those use `flags.newClient()` already and are the reference for error handling.

**Test scenarios:**
- Happy path: generate a CLI with `http_transport: standard`. Run doctor against a public test endpoint (mock server returning `<html><body>OK</body></html>`). Assert: `report["api"] == "reachable"`. The doctor code uses `flags.newClient()`.
- Happy path: generate a CLI with `http_transport: browser-chrome`. Run doctor in offline test mode (the client.Client init succeeds even without network); assert that the generated doctor.go imports do NOT include `net/http` and DO use `flags.newClient()` (string match on generated output).
- Edge case: spec without `auth.verify_path` runs the reachability probe but skips credential validation entirely; doctor reports `auth: not required` or auth is unconfigured.
- Error path: simulate `flags.newClient()` returning an error (mock the constructor to fail). Doctor reports `api: "client init error: <msg>"` and exits non-zero only when `--fail-on error`.
- Error path: response body contains the Cloudflare interstitial marker. Doctor reports `api: "blocked by Cloudflare 'Just a moment...' interstitial — ..."` with vendor named. Test by feeding the well-known interstitial fixture as the mock response body.
- Negative: response body contains `<title>Cloudflare error</title>` BUT also has actual content (200 with embedded error message, not a true challenge page). Detection should NOT false-positive on this; only the actual challenge markers ("Just a moment", "challenges.cloudflare.com", etc.) trigger.
- Regression: regenerate `notion-pp-cli` and assert that doctor still passes its existing tests (no behavior change for standard-transport CLIs).
- Integration: regenerate `recipe-goat-pp-cli` (already uses browser-chrome) and assert doctor reports `api: "reachable (via browser-chrome transport)"` against the real recipe-goat base URL when run with internet.

**Verification:**
- `go test ./internal/generator/...` passes including new doctor template tests.
- Manually regenerate `allrecipes` from spec; the generated `internal/cli/doctor.go` no longer imports `net/http` (or imports it only for `*http.Request` types, not `http.Client`).
- The hand-patch I applied earlier becomes unnecessary — regenerated allrecipes doctor matches my hand-edit shape.

---

- U2. **Skip auth subcommand and registration when auth.type=="none"**

**Goal:** When the spec declares `auth.type: none` AND no graphql_persisted_query traffic-analysis hint AND no AuthorizationURL, generator does not emit `internal/cli/auth.go` and does not register `newAuthCmd(&flags)`. Scorecard auth dimension exempts no-auth specs from "no auth subcommand" deductions.

**Requirements:** R3, R4, R5, R10

**Dependencies:** None.

**Files:**
- Modify: `internal/generator/generator.go` (`renderAuthFiles` function around line 1240)
- Modify: `internal/generator/templates/root.go.tmpl` (around line 200)
- Modify: `internal/pipeline/scorecard.go` (`scoreAuth` function around line 295; signature plumbing through callers around line 114)
- Modify: `internal/generator/templates/auth.go.tmpl`, `auth_simple.go.tmpl`, `auth_browser.go.tmpl` — no edits required; they just don't get rendered for no-auth specs
- Test: `internal/generator/generator_test.go` (auth rendering test); `internal/pipeline/scorecard_test.go` (auth dimension test)

**Approach:**
- In `generator.go:renderAuthFiles`, add an early return at the top:
  - When `g.Spec.Auth.Type == "none"` AND `g.Spec.Auth.AuthorizationURL == ""` AND `!g.hasTrafficAnalysisHint("graphql_persisted_query")` → return nil immediately. No auth.go is written.
- In `root.go.tmpl`, wrap the `rootCmd.AddCommand(newAuthCmd(&flags))` line in a Go template conditional: `{{if not .Spec.Auth.IsNone}}` (or equivalent — there may already be an `IsAuthenticated`-style predicate; check `internal/spec/spec.go` for what's exposed to templates).
- In `scoreAuth`, change the signature to accept the parsed spec (or just the `Auth` struct). Callers at `scorecard.go:114` already have the spec in scope.
- Add the no-auth exemption at the top of `scoreAuth`: when `spec.Auth.Type == "none"`, return 10 with a marker that downstream JSON output can surface (e.g., set a sibling `notes["auth"] = "exempted: auth.type:none"` if the score struct supports it, or accept that 10/10 alone communicates the exemption).
- Existing no-auth signals (config permissions, has-store, etc.) are NOT used; we exit early.
- Do not change behavior for auth.type:api_key, bearer_token, oauth, cookie, composed, session_handshake, etc.

**Patterns to follow:**
- Existing template conditionals in `root.go.tmpl` for sync/import/export commands — those gate on `.HasStore` and similar predicates. The auth gate is the same shape.
- Existing early-return conditionals in `generator.go` (other render functions skip on guards). `renderMCPFiles` is a comparable pattern.
- `scoreAuth`'s style: the current "auto-award 2 points so the ceiling is 10/10" pattern at line 333 is the existing pattern for "this dimension can't fully measure what we want, so we cap." Extending that with a no-auth exemption is consistent.

**Test scenarios:**
- Happy path (no-auth): generate from a spec with `auth: {type: none}`. Assert: `internal/cli/auth.go` does NOT exist in the output; `root.go` does NOT contain `newAuthCmd`; scorecard `auth: 10`.
- Happy path (api-key auth): generate from a spec with `auth: {type: api_key, header: X-API-Key}`. Assert: `internal/cli/auth.go` exists with the simple template's contents; `root.go` contains `newAuthCmd`; scorecard auth scoring runs the existing path.
- Edge case: spec with `auth: {type: none}` AND `traffic_analysis: {hints: ["graphql_persisted_query"]}`. Assert: auth.go IS emitted (auth_browser template) — because GraphQL persisted-queries need browser-aware refresh flows even without traditional auth. Existing exception preserved.
- Edge case: spec with `auth: {type: none}` AND `auth.authorization_url: <url>`. Assert: auth.go IS emitted (auth.go.tmpl OAuth path) — AuthorizationURL implies OAuth even if Type is "none."
- Error path: scoreAuth called without a spec arg (after the signature change, all callers must pass it). Compile-time guard via Go's static typing; no runtime test needed.
- Regression: re-run scorecard on three existing CLIs of different auth shapes (notion = api_key, github = bearer, recipe-goat = api_key, slack = composed). Their auth scores must not change.
- Negative: a spec without an `auth:` section (legacy-style spec where Auth defaults to a non-none zero value). Assert: behavior is unchanged (the early-return only fires on explicit `auth.type:none`).

**Verification:**
- `go test ./internal/generator/... ./internal/pipeline/...` passes.
- Regenerate `allrecipes` from spec → the manual `auth.go` deletion + root.go registration removal I performed during the run becomes unnecessary; the generator does it.
- Regenerate `notion-pp-cli` → no diff in auth-related files; scorecard auth unchanged.

---

- U3. **HTML extract mode:links produces clean text + separate image URL**

**Goal:** `extractHTMLLink` skips `<noscript>`, `<script>`, `<style>`, `<template>` subtrees so raw `<img>` markup from JS-fallback content doesn't leak into result fields. Add `htmlLink.Image` populated from the first nested image URL.

**Requirements:** R6, R7, R10

**Dependencies:** None.

**Files:**
- Modify: `internal/generator/templates/html_extract.go.tmpl`:
  - `htmlLink` struct (lines 33-39): add `Image string` field
  - `nodeText` function (lines 246-255): add raw-text-tag suppression OR add a `nodeTextSkipping` variant used by `extractHTMLLink`
  - `extractHTMLLink` function (around line 122): populate `Image` from first non-suppressed `<img src>`
- Test: `internal/generator/templates/html_extract_test.go` if such a test exists (check; the template is rendered into every CLI but the logic could be tested against fixtures)

**Approach:**
- Define `var rawTextTags = map[string]struct{}{"noscript":{}, "script":{}, "style":{}, "template":{}}`.
- Modify `nodeText` (or introduce `nodeTextNoRawText`) to skip subtrees rooted at any element in `rawTextTags`. Implementation choice: walk children manually, checking each child's `Type == ElementNode` and skipping descent when `rawTextTags[strings.ToLower(child.Data)]` matches. The current `walkHTML` is a generic recursion; the cleanest fix is a tighter walker local to text extraction, leaving `walkHTML` untouched (it's used for other things like meta-tag scanning that shouldn't suppress content).
- In `extractHTMLLink`, after computing the suppressed text, also walk the anchor's children to find the first `<img>` element NOT inside a suppressed subtree. Use `attrValue(img, "src")` and pass through `normalizeHTMLURL` to get an absolute URL.
- The `htmlLink.Image` field is omitted from JSON output when empty (`json:"image,omitempty"`).
- Verify `splitRankedHTMLLinkText` and other callers of cleaned anchor text still work — they consume the cleaned string, not the raw nodes.
- Do not add new spec configuration. The four suppressed tags are stable across HTML5; this isn't a per-spec choice.

**Patterns to follow:**
- `walkHTML` (lines 217-225) — the existing recursion. The new walker mirrors its shape but adds the suppression check.
- `attrValue` (line 240) — reuse for `src` lookup.
- `normalizeHTMLURL` (line 168) — already used to absolute-ize anchor `href`s; reuse for image `src` URLs.

**Test scenarios:**
- Happy path: anchor with both rendered `<img>` and `<noscript><img></noscript>` (Allrecipes pattern). Assert: `Name` contains only the visible link text (no `<img src=...>` markup); `Image` is the rendered image URL, not the noscript fallback.
- Happy path: anchor with no `<img>` at all (text-only listing). Assert: `Name` is the visible text; `Image` is empty string (omitted from JSON).
- Edge case: anchor wraps `<noscript>` with multiple image fallbacks. Assert: `Name` excludes all noscript content; `Image` is empty (because the first non-suppressed image doesn't exist).
- Edge case: anchor with `<script>` injected inline (some sites do this for analytics). Assert: script content is suppressed in Name.
- Edge case: anchor with `<style>` block (rare but valid). Assert: style content is suppressed.
- Edge case: anchor with `<template>` (HTML5 template tag for client-side rendering). Assert: template content is suppressed.
- Edge case: anchor wrapping a single visible image (no alt text, no surrounding text). Assert: `Name` falls back to image alt or empty; `Image` is captured.
- Edge case: nested anchor (browsers don't render this but HTML allows it post-parsing). Behavior should be deterministic; document the expected outcome in test.
- Regression: existing recipe-goat fixtures (cookbook search) parse with no behavioral change. `Name` and `Text` for plain anchor text remain identical.
- Negative: HTML with malformed noscript (missing close tag). The html parser auto-closes; suppression should still work because the parser's tree structure is what we're walking.

**Verification:**
- `go test ./internal/generator/...` passes including new html_extract template tests.
- Manually regenerate `allrecipes`; run `recipes search --q brownies --json`. Assert: each result's `Name` is clean (e.g., "S'mores Brownies 342 Ratings") with no `<img src=...>` markup.
- Manually compare the regenerated allrecipes search output against my hand-written `internal/recipes/search.go` output. They should produce equivalent SearchResult title fields for the same input HTML.

---

- U4. **Verify-friendly RunE pattern for hand-written commands**

**Goal:** Emit a `dryRunOK(cmd, flags) bool` helper in `helpers.go.tmpl` and document the verify-friendly RunE pattern in the Printing Press SKILL Phase 3 build instructions so hand-written transcendence commands pass verify on first run.

**Requirements:** R8, R9

**Dependencies:** None. (Independent of WU-1/2/3 — these change different files.)

**Files:**
- Modify: `internal/generator/templates/helpers.go.tmpl`
- Modify: `skills/printing-press/SKILL.md` (Phase 3 — Build The GOAT, around line 1522; Agent Build Checklist around line 1575)
- Test: `internal/generator/generator_test.go` (verify the new helper renders into output)

**Approach:**
- In `helpers.go.tmpl`, add a small helper:
  - `dryRunOK(cmd *cobra.Command, flags *rootFlags) bool` returning `flags != nil && flags.dryRun`. Cheap; named so the call site reads naturally: `if dryRunOK(cmd, flags) { return nil }`.
  - Place it near other generic helpers (after `replacePathParam` if still present, otherwise wherever other no-arg utility helpers live).
- In `skills/printing-press/SKILL.md`:
  - Add an 8th principle to the "Agent Build Checklist (per command)" list at line 1575: "**Verify-friendly RunE:** no `Args: cobra.MinimumNArgs(N)`, no `MarkFlagRequired(...)` for hand-written commands. RunE checks `len(args) == 0` first → falls through to `cmd.Help()`. Then calls `dryRunOK(cmd, flags)` → returns nil if dry-run. Then runs real logic."
  - Add a small subsection "Verify-friendly RunE template" with a directional code sketch (framed as guidance):

        RunE: func(cmd *cobra.Command, args []string) error {
            if len(args) == 0 {
                return cmd.Help()
            }
            if dryRunOK(cmd, flags) {
                return nil
            }
            // ... real logic ...
        }

  - Note explicitly: cobra evaluates `Args:` and `MarkFlagRequired` BEFORE RunE runs, so a dry-run guard inside RunE cannot reach if those gates fail. For hand-written commands, do the validation inside RunE.
- The SKILL change is additive: existing language about `Args: cobra.MinimumNArgs(N)` (which DOES appear elsewhere as a pattern for spec-derived commands) is unchanged. The new guidance specifically applies to hand-written novel-feature commands in Phase 3.

**Patterns to follow:**
- Existing helpers in `helpers.go.tmpl` — short, no-doc-comment-needed utilities (string helpers, etc.).
- Existing SKILL.md Phase 3 section structure — bullet checklist format, then prose templates.

**Test scenarios:**
- Happy path: regenerate any CLI; assert `internal/cli/helpers.go` contains the `dryRunOK` function with the right signature.
- Happy path: a hand-written command author follows the SKILL guidance; verify probes the command with `--dry-run`; the command returns nil and exits 0. (This is a meta-test; the actual validation happens during a future generation's Phase 4 verify pass.)
- Negative: regenerate an existing CLI like `notion-pp-cli`; assert no behavioral change to its handlers (existing handlers don't use the new helper but build is unaffected).
- Verify the SKILL change renders cleanly in markdown (no syntax issues).

**Verification:**
- `go test ./internal/generator/...` passes; the new helper appears in the rendered helpers.go.
- Run a fresh `/printing-press <api>` against a small spec; the resulting `internal/cli/helpers.go` contains `dryRunOK`.
- Read the updated SKILL.md and confirm the Phase 3 build instructions now mention the verify-friendly pattern and `dryRunOK`.

---

## System-Wide Impact

- **Interaction graph:**
  - WU-1: doctor → flags.newClient → client.Client → Surf transport. All four call sites in doctor template move to the wrapped client.
  - WU-2: generator.renderAuthFiles → spec.Auth.Type → conditional render. root.go template → conditional command registration. scorecard.scoreAuth → spec arg → exemption.
  - WU-3: html_extract.nodeText → suppression list. extractHTMLLink → first-image walk. Downstream: every generated `*_search.go` link-mode handler.
  - WU-4: helpers.go.tmpl → new symbol. SKILL Phase 3 → guidance for hand-written code. No runtime interaction surface.

- **Error propagation:**
  - WU-1: doctor errors propagate through `report["api"]` and are surfaced in JSON output. Vendor-named blockages give actionable text. Transport errors propagate honestly (no fallback).
  - WU-2: generator early-return on no-auth doesn't error; scorecard exemption returns 10/10 cleanly.
  - WU-3: HTML parsing errors continue to propagate as before. Suppression doesn't introduce new error paths.
  - WU-4: helper is pure; no error surface.

- **State lifecycle risks:**
  - None of the four units introduce new persistent state, caching, or rollout concerns. All are stateless transformations.

- **API surface parity:**
  - WU-1 changes the doctor's *implementation* but not its JSON output schema. The `report` object's keys are unchanged for the success path; new diagnostic strings appear in failure messages but the field is the same `report["api"]` string. Documented as additive.
  - WU-2 removes `auth set-token / status / logout` subcommands from no-auth CLIs. Users running `<cli> auth set-token` on a regenerated no-auth CLI will get "unknown command" instead of a no-op. This is intentional but should be noted in the no-auth CLI's Troubleshooting.
  - WU-3 adds `htmlLink.Image` field. Existing consumers ignore unknown fields by default (Go json decoder behavior); JSON consumers should treat it as additive.
  - WU-4 is internal helper plus SKILL guidance; no external API change.

- **Integration coverage:**
  - WU-1 needs a regenerate-and-run integration test against a Cloudflare-fronted CLI (allrecipes, recipe-goat). Mocks alone won't prove the Surf path works end-to-end through the new doctor.
  - WU-3 needs a regenerate-and-run check of the allrecipes search output to confirm the noscript fix produces clean fields against the live Allrecipes search page.

- **Unchanged invariants:**
  - Surf TLS impersonation behavior is unchanged. WU-1 reuses it; WU-2/3/4 don't touch transport.
  - `http_transport: standard` CLIs continue to use the underlying http.DefaultTransport via Surf's standard mode; no regression for non-Cloudflare CLIs.
  - Auth-needing CLIs continue to ship with full auth subcommand trees and full auth scoring.
  - The HTML extract `mode: page` path (used by recipe-get-style commands) is unchanged; only `mode: links` text extraction is hardened.

---

## Risks & Dependencies

| Risk | Mitigation |
|------|------------|
| WU-1: `flags.newClient()` may not be reachable from the doctor's PreRunE timing in some templates (e.g., when doctor has its own short-circuit before global PreRunE runs). | Verify by reading the existing doctor template's RunE. The current code already calls into `cfg, err := config.Load(flags.configPath)` early, which is comparable timing. If the constructor depends on PreRunE-set state that doctor doesn't set, document and add the missing init. |
| WU-2: scoreAuth signature change cascades to all callers. | The function has one caller (`scorecard.go:114`). Plumbing the spec is a one-line change at the call site. Verify no test fixtures call `scoreAuth(dir)` directly without the spec; if they do, update them. |
| WU-2: existing CLIs in the test fixtures may have implicit `auth.type:none` specs that newly trigger the exemption, changing their golden scores. | Run scorecard against every fixture spec post-change; if a no-auth fixture exists, it should now score 10 instead of whatever it scored before. Document this in the test diff. |
| WU-3: skipping noscript may break CLIs that intentionally extract noscript content (rare but possible — e.g., extracting fallback URLs for users with JS disabled). | Audit existing CLIs that use html_extract `mode: links` (recipe-goat, hackernews?, agent-capture?) and verify none rely on noscript content. If one does, expose the suppression list as a spec config knob. |
| WU-4: skill change is documentation; agents may not read it consistently. | Skill guidance is complementary to the helper emission. The helper exists regardless; agents who don't read the skill still get correct hand-written commands when they use the helper, and agents who do read the skill get the full pattern. The polish-worker continues as a backstop. |
| All four: regenerating existing library CLIs with the new templates may surface unexpected diffs in `auth.go` (deleted), `doctor.go` (rewritten), `helpers.go` (new symbol). | Verify on a sample of 3 existing CLIs (notion, github, recipe-goat) before merge; document the expected diff shape in the PR description. |

---

## Documentation / Operational Notes

- The retro at `https://files.catbox.moe/h5zu55.md` documents the original allrecipes generation that surfaced these findings. Link it from the PR description.
- The validation comment at https://github.com/mvanhorn/cli-printing-press/issues/333#issuecomment-4325110340 contains corrected diagnoses for F4 (noscript-specific, not generic nested HTML) and F8 (regex misses flag types — out of scope for this plan; that's WU-6 in a separate plan).
- After merge, regenerate `allrecipes`, `recipe-goat`, and one no-auth CLI (e.g., a public-data CLI in the catalog) to confirm the fixes ship cleanly.
- Update CHANGELOG.md under the next release with: "feat(cli): doctor honors http_transport; auth subcommand skipped for no-auth specs; html-extract suppresses noscript subtrees; new dryRunOK helper for hand-written commands."

---

## Sources & References

- **Origin issue:** [mvanhorn/cli-printing-press#333](https://github.com/mvanhorn/cli-printing-press/issues/333)
- **Validation comment:** [issue #333 comment](https://github.com/mvanhorn/cli-printing-press/issues/333#issuecomment-4325110340)
- **Retro document:** https://files.catbox.moe/h5zu55.md (also at `~/printing-press/manuscripts/allrecipes/20260426-230519/proofs/20260427-000900-retro-allrecipes-pp-cli.md`)
- Related code:
  - `internal/generator/templates/doctor.go.tmpl`
  - `internal/generator/generator.go:renderAuthFiles`
  - `internal/generator/templates/root.go.tmpl`
  - `internal/generator/templates/helpers.go.tmpl`
  - `internal/generator/templates/html_extract.go.tmpl`
  - `internal/pipeline/scorecard.go:scoreAuth`
  - `skills/printing-press/SKILL.md` Phase 3
- Related plans:
  - `docs/plans/2026-04-13-002-feat-cloudflare-cli-learnings-plan.md` (browser-chrome transport adoption)
  - `docs/plans/2026-04-19-004-feat-auth-doctor-plan.md` (separate concern: printing-press binary's auth-doctor subcommand)
- Related retro: `docs/retros/2026-04-13-recipe-goat-retro.md` (related symptom: hand-written command verify failures)
