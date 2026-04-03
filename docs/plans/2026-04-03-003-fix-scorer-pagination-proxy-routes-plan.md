---
title: "fix: Scorer false positives, pagination param plumbing, and catalog proxy_routes"
type: fix
status: active
date: 2026-04-03
origin: docs/retros/2026-04-03-postman-explore-run2-retro.md
---

# fix: Scorer false positives, pagination param plumbing, and catalog proxy_routes

## Overview

Three small, independent fixes surfaced by the Postman Explore Run 2 retro. Each addresses a different layer of the printing press:

1. **Dogfood and verify false positives** — dogfood's command-tree check fails on subcommands (CamelCase→lowercase doesn't match kebab-case help output), and verify's synthetic arg generator produces `"mock-value"` for enum-constrained positional args.
2. **Sync template pagination** — the generated `determinePaginationDefaults()` hardcodes `cursorParam: "after"` even though the profiler correctly detects `"offset"` for offset-paginated APIs.
3. **Catalog proxy_routes** — proxy-envelope APIs with multiple backend services (like Postman Explore's "publishing" + "search") have no way to declare service routing in the catalog. The generator template already handles `.ProxyRoutes` — it just needs data.

## Problem Frame

Every printed CLI goes through dogfood, verify, and scorecard. False positives in these tools waste time on non-issues and mask real problems. The Postman Explore run produced 15 false "unregistered commands" (dogfood) and 5 false exec_fail results (verify) — noise that obscured legitimate gaps.

Separately, the sync template's hardcoded cursor pagination breaks offset-paginated APIs (sync never advances past page 1), and proxy-envelope APIs require manual `serviceForPath` edits every time.

(see origin: `docs/retros/2026-04-03-postman-explore-run2-retro.md`, findings #3, #4, #5, #1)

## Requirements Trace

- R1. Dogfood's command-tree check must not produce false "unregistered" results for subcommands registered via cobra's `AddCommand`
- R2. Verify must provide valid synthetic arg values for common enum-constrained positional arg placeholders (type, resource, format, kind)
- R3. Generated `determinePaginationDefaults()` must use the profiler-detected cursor/offset param, not a hardcoded default
- R4. Catalog entries for proxy-envelope APIs can declare `proxy_routes` to map path prefixes to service names, and the generator must consume them

## Scope Boundaries

- Do not change verify's execution logic or command discovery — only fix arg synthesis and name matching
- Do not change the profiler's pagination detection — it already works correctly
- Do not auto-detect proxy routes from specs — require explicit catalog declaration
- Do not address finding #2 (enum-parameterized sync resource expansion) — that needs design work and is deferred to "Do Next"
- Do not address finding #7 (user-friendly wrapper commands) — that's a large template gap

## Context & Research

### Relevant Code and Patterns

- `internal/pipeline/dogfood.go:960-1030` — `checkCommandTree()` scans for `func new*Cmd()`, lowercases CamelCase suffix, matches against `--help` output. Uses `strings.Contains(helpLower, cmdName)`.
- `internal/pipeline/runtime.go:440-458` — `syntheticArgValue()` maps placeholder names to test values. Falls back to `"mock-value"`.
- `internal/generator/templates/sync.go.tmpl:296-304` — `determinePaginationDefaults()` hardcodes `cursorParam: "after"`.
- `internal/generator/generator.go:387-398` — template data struct passes `SyncableResources` and `SearchableFields` but **not** `Pagination`.
- `internal/profiler/profiler.go:39-45` — `PaginationProfile` struct with `CursorParam`, `PageSizeParam`, `SinceParam`, `ItemsKey`, `DefaultPageSize`.
- `internal/catalog/catalog.go:65-87` — `Entry` struct has `ClientPattern` but no `ProxyRoutes`.
- `internal/spec/spec.go:19-20` — `APISpec` already has `ProxyRoutes map[string]string` field.
- `internal/generator/templates/client.go.tmpl:121-150` — template already handles `.ProxyRoutes` with longest-prefix matching. Just needs data.

### Institutional Learnings

- Previous retros (steam, redfin) also surfaced pagination issues but focused on cursor-based APIs. This is the first offset-pagination finding.
- The proxy-envelope pattern was added for Postman Explore in the initial run. The template already supports `.ProxyRoutes` — it was just never populated from catalog data.

## Key Technical Decisions

- **CamelCase→kebab conversion for dogfood**: Insert hyphens before uppercase letters in the CamelCase suffix, then lowercase the result. `ApiGetCategory` → `api-get-category`. This matches cobra's convention and the generated command names. Alternatives considered: splitting into individual words and searching for each — rejected because it would produce false positives (e.g., `Category` appears in unrelated help text).

- **Synthetic arg inference from help text vs static mapping**: For now, expand `syntheticArgValue()` with common placeholder→value mappings. The retro suggests parsing help text for valid values as a better long-term approach, but that's more complex and the static mappings cover the common cases today.

- **Pagination template plumbing**: Pass the entire `PaginationProfile` struct to the sync template data. The template already has the `determinePaginationDefaults()` function — change it to use template values instead of hardcoded strings.

- **ProxyRoutes in catalog**: Add `proxy_routes` as `map[string]string` to the catalog `Entry` struct. When loading a catalog entry, copy its `ProxyRoutes` into the `APISpec.ProxyRoutes` field (which already exists). No validation needed beyond the existing client_pattern check.

## Open Questions

### Resolved During Planning

- **Should dogfood also check the cobra `Use:` field directly?** No — parsing Go source for string literals is fragile. CamelCase→kebab conversion from function names is more reliable and matches the generator's naming convention.
- **Should `syntheticArgValue` try all enum values until one succeeds?** No — that would be slow and requires building the binary. Static mappings for common placeholders are sufficient and fast.

### Deferred to Implementation

- **Exact CamelCase→kebab regex**: The implementation should handle edge cases like consecutive uppercase letters (`APIKey` → `api-key` not `a-p-i-key`). Use the same convention as cobra/Go naming.

## Implementation Units

- [ ] **Unit 1: Fix dogfood CamelCase→kebab command matching**

**Goal:** Eliminate false "unregistered commands" for subcommands

**Requirements:** R1

**Dependencies:** None

**Files:**
- Modify: `internal/pipeline/dogfood.go`
- Test: `internal/pipeline/dogfood_test.go`

**Approach:**
- In `checkCommandTree()`, after extracting the CamelCase suffix from `newXxxCmd()`, convert it to kebab-case before matching. Insert a hyphen before each uppercase letter (handling consecutive caps like API → api), then lowercase.
- The matching at line 1023 (`strings.Contains(helpLower, cmdName)`) then works because `api-get-category` appears in the help output under the `api` subcommand.

**Patterns to follow:**
- Existing `checkCommandTree` logic and test structure in `dogfood_test.go`

**Test scenarios:**
- Happy path: CLI with `newApiGetCategoryCmd()` → dogfood detects it as registered (matches `api` + `get-category` in help)
- Happy path: CLI with `newAuthLoginCmd()` → detected as registered
- Edge case: `newVersionCliCmd()` → correctly matches `version` in help
- Edge case: CLI with single-word command `newSyncCmd()` → still works (no hyphens needed)
- Integration: Run dogfood on postman-explore-pp-cli → 0 unregistered commands

**Verification:**
- `go test ./internal/pipeline/ -run TestCheckCommandTree` passes
- Dogfood on a CLI with subcommands reports 0 unregistered

---

- [ ] **Unit 2: Expand syntheticArgValue for enum-constrained args**

**Goal:** Verify passes exec test for commands with enum-constrained positional args

**Requirements:** R2

**Dependencies:** None

**Files:**
- Modify: `internal/pipeline/runtime.go`
- Test: `internal/pipeline/runtime_test.go`

**Approach:**
- Add mappings to `syntheticArgValue()` for common enum-constrained placeholder names: `"type"` → `"collection"`, `"resource"` → `"items"`, `"format"` → `"json"`, `"kind"` → `"default"`, `"entity"` → `"item"`, `"category"` → `"general"`, `"action"` → `"list"`.
- These are common generic placeholder names used across many CLIs.

**Patterns to follow:**
- Existing switch cases in `syntheticArgValue()`

**Test scenarios:**
- Happy path: `syntheticArgValue("type")` returns `"collection"`
- Happy path: `syntheticArgValue("resource")` returns `"items"`
- Edge case: `syntheticArgValue("unknown-thing")` still returns `"mock-value"` (default fallback unchanged)
- Edge case: existing mappings unchanged — `syntheticArgValue("query")` still returns `"mock-query"`

**Verification:**
- `go test ./internal/pipeline/ -run TestSyntheticArgValue` passes

---

- [ ] **Unit 3: Wire profiler pagination params into sync template**

**Goal:** Generated sync uses detected pagination param instead of hardcoded "after"

**Requirements:** R3

**Dependencies:** None

**Files:**
- Modify: `internal/generator/templates/sync.go.tmpl`
- Modify: `internal/generator/generator.go`
- Test: `internal/generator/generator_test.go`

**Approach:**
- In `generator.go`, add `Pagination profiler.PaginationProfile` to the sync template data struct (alongside `SyncableResources` and `SearchableFields`). Populate from `g.profile.Pagination`.
- In `sync.go.tmpl`, replace the hardcoded `determinePaginationDefaults()` body with template values: `cursorParam: "{{.Pagination.CursorParam}}"`, `limitParam: "{{.Pagination.PageSizeParam}}"`, `limit: {{.Pagination.DefaultPageSize}}`.
- Also template `determineSinceParam()` to use `{{.Pagination.SinceParam}}` with fallback to `"since"`.

**Patterns to follow:**
- How `SyncableResources` is already passed through the template data struct in `generator.go:387-398`

**Test scenarios:**
- Happy path: Generate from spec with `offset` pagination param → `determinePaginationDefaults()` returns `cursorParam: "offset"`
- Happy path: Generate from spec with `after` pagination param → returns `cursorParam: "after"`
- Edge case: Spec with no detected pagination → profiler defaults to `"after"` and `100` → template emits those defaults
- Integration: Generated CLI compiles and passes quality gates after template change

**Verification:**
- `go test ./internal/generator/ -run TestGenerate` passes
- Generated `sync.go` contains the profiler-detected param, not hardcoded `"after"`

---

- [ ] **Unit 4: Add proxy_routes to catalog schema and wire to generator**

**Goal:** Proxy-envelope APIs can declare service routing in their catalog entry

**Requirements:** R4

**Dependencies:** None

**Files:**
- Modify: `internal/catalog/catalog.go`
- Modify: `catalog/postman-explore.yaml`
- Modify: `internal/generator/generator.go` (or wherever catalog → APISpec mapping happens)
- Test: `internal/catalog/catalog_test.go`
- Test: `internal/generator/generator_test.go`

**Approach:**
- Add `ProxyRoutes map[string]string` field to the catalog `Entry` struct with yaml tag `proxy_routes,omitempty`.
- Update `postman-explore.yaml` to include `proxy_routes: {"/search-all": "search", "/": "publishing"}`.
- In the code path where catalog entries are loaded into `APISpec`, copy `entry.ProxyRoutes` to `spec.ProxyRoutes`. The `APISpec` struct already has `ProxyRoutes map[string]string` (spec.go:20).
- The `client.go.tmpl` template already handles `.ProxyRoutes` with longest-prefix matching — no template changes needed.
- Rebuild the binary after adding the catalog entry (catalog entries are embedded via `catalog.FS`).

**Patterns to follow:**
- How `ClientPattern` flows from catalog entry to APISpec (existing pattern)
- How `AuthRequired` was added to the catalog Entry struct

**Test scenarios:**
- Happy path: Parse catalog entry with `proxy_routes` → Entry.ProxyRoutes populated correctly
- Happy path: Generate from postman-explore catalog → `serviceForPath("/search-all")` returns `"search"`, `serviceForPath("/v1/api/team")` returns `"publishing"`
- Edge case: Catalog entry without `proxy_routes` → Entry.ProxyRoutes is nil, serviceForPath returns API name slug (unchanged behavior)
- Edge case: Catalog validation passes with and without `proxy_routes`

**Verification:**
- `go test ./internal/catalog/` passes
- `go test ./internal/generator/` passes
- `printing-press catalog show postman-explore --json` shows proxy_routes

## System-Wide Impact

- **Interaction graph:** Dogfood and verify are called by `printing-press dogfood`, `printing-press verify`, and the skill's shipcheck block. Fixing false positives improves all three callers.
- **Error propagation:** No new error paths. All changes are in existing code paths.
- **State lifecycle risks:** None — these are stateless functions.
- **API surface parity:** The catalog's `proxy_routes` field is new but backward-compatible (omitempty). Existing catalog entries are unchanged.
- **Unchanged invariants:** The profiler's pagination detection logic is not modified. The generator's template rendering pipeline is not modified beyond adding one field to the data struct. The client.go.tmpl proxy-envelope template is not modified.

## Risks & Dependencies

| Risk | Mitigation |
|------|------------|
| CamelCase→kebab conversion produces wrong results for edge cases (e.g., `APIv2List` → `ap-iv2-list` instead of `api-v2-list`) | Use Go's established camelCase-to-kebab pattern (split on uppercase boundaries, handle consecutive caps). Test with real function names from generated CLIs. |
| Adding `Pagination` to sync template data breaks existing generated CLIs | The template uses `{{.Pagination.CursorParam}}` which evaluates to `""` if the field is zero-value. The `determinePaginationDefaults` function already has defaults, so even if Pagination is empty, the function still returns sensible values. |
| Catalog entry rebuild required after adding proxy_routes to postman-explore.yaml | Document that `go build` must be run after catalog changes. This is already documented in CLAUDE.md. |

## Sources & References

- **Origin document:** [docs/retros/2026-04-03-postman-explore-run2-retro.md](docs/retros/2026-04-03-postman-explore-run2-retro.md)
- Related code: `internal/pipeline/dogfood.go`, `internal/pipeline/runtime.go`, `internal/generator/templates/sync.go.tmpl`, `internal/catalog/catalog.go`
- Related retros: `docs/retros/2026-03-30-postman-explore-retro.md` (first run)
