---
title: "fix: Generator pipeline improvements — domain Search, auth inference, verify env, sync paths"
type: fix
status: active
date: 2026-04-01
origin: docs/retros/2026-04-01-steam-run3-retro.md
---

# Fix: Generator Pipeline Improvements

## Overview

Four generalized machine improvements carried forward from three Steam retros. Each affects every future generated CLI, not just Steam. Domain-specific Search in the store template (+3 scorecard), auth inference from query params (+2 scorecard), verify auth env passthrough (+5% verify), and sync path resolution for non-REST APIs (fixes broken sync).

## Problem Frame

At 85/100, the Steam CLI's remaining gaps are in the generator and verify pipeline — not the scorer or the CLI itself. The store generates Upsert per entity but only generic Search. The OpenAPI parser misses auth when specs lack `securitySchemes`. Verify can't test auth-requiring commands because it doesn't pass env vars. Sync builds paths from resource names instead of actual endpoints.

(see origin: docs/retros/2026-04-01-steam-run3-retro.md, findings #1, #2, #3, #6)

## Requirements Trace

- R1. Generated store has domain-specific Search methods (e.g., `SearchPlayers()`) alongside Upsert methods
- R2. OpenAPI parser infers query-param auth when >30% of operations have a `key`/`api_key` parameter
- R3. Verify passes all auth env vars from the spec during command testing
- R4. Sync uses actual endpoint paths from the profiler, not `"/" + resourceName`

## Scope Boundaries

- Not changing the scorecard (PRs #100-102 already fixed scorer issues)
- Not changing the retro skill
- Not fixing flag description quality (depends on spec content)
- Not adding sync path params bonus (inherent to API structure)

## Context & Research

### Relevant Code and Patterns

**Store Search:**
- `store.go.tmpl:341-372` — already generates `Search{{pascal .Name}}()` for tables with FTS5. The template iterates `{{range .Tables}}{{if .FTS5}}`. The issue: the scorecard checks for `\.Search[A-Z]` patterns, but the generated CLI may not have searchable tables or the profiler may not flag enough fields.
- `profiler.go:533-548` — `collectStringFields()` identifies searchable fields. `SearchableFields` map is passed to the generator.

**Auth Inference:**
- `parser.go:240-307` — `mapAuth()` extracts security schemes. Returns early if no scheme found. The fallback scan should go here.
- `spec.go:27-38` — `AuthConfig` struct already has `In` field for query-param auth.

**Verify Env:**
- `runtime.go:123-147` — `RunVerify()` constructs env with one token var. Needs to iterate `spec.Auth.EnvVars`.
- `runtime.go:462` — `runCommandTests()` receives `env []string` and passes to subprocess.

**Sync Path:**
- `profiler.go:63` — `SyncableResources []string` stores only names.
- `profiler.go:141` — adds resource name to syncable map but discards the endpoint path.
- `sync.go.tmpl:178` — `path := "/" + resource` is the broken path construction.
- `spec.go:53` — `Endpoint.Path` has the actual API path.

### Prior Art

- PRs #100-102 established the retro→plan→implement pattern. Each fix follows: read the code, understand the gap, make a targeted change, test with existing fixtures + new test cases.

## Key Technical Decisions

- **SyncableResources struct change:** Change from `[]string` to `[]SyncableResource{Name, Path}`. This ripples through the profiler, generator data struct, and sync template — but each change is mechanical. The profiler already has the endpoint path available at the point where it adds to syncable.

- **Auth inference threshold: 30%** — If >30% of operations have a `key`/`api_key` query param, infer auth. This avoids false positives on APIs where only 1-2 endpoints accept optional keys. Steam has 47/158 (30%) — right at the threshold.

- **Verify env: iterate spec.Auth.EnvVars** — The verify runtime already passes one env var. Extending to iterate the full list is a small change. The verify config already carries the spec.

## Open Questions

### Deferred to Implementation

- **Exact FTS5 table generation logic** — The store template already generates FTS5 tables for some resources. Need to trace when the profiler marks a table as having FTS5 vs not, to understand whether domain-specific Search methods are already emitted and just not used, or not emitted at all.
- **Auth inference: what happens when both securitySchemes AND query-param keys exist** — The guard should be: only infer if securitySchemes detection produced `Type: "none"`. If bearer/oauth was already detected, don't override.

## Implementation Units

- [ ] **Unit 1: Domain-specific Search methods in store template**

**Goal:** Every generated store emits `Search<Entity>()` methods for entities with searchable fields, enabling the scorecard's data_pipeline +3.

**Requirements:** R1

**Dependencies:** None

**Files:**
- Modify: `internal/generator/templates/store.go.tmpl`
- Modify: `internal/profiler/profiler.go` (if SearchableFields needs enhancement)
- Test: `internal/profiler/profiler_test.go`

**Approach:**
- The store template already has `Search{{pascal .Name}}` generation gated on `{{if .FTS5}}`. Verify this is working and producing the `\.Search[A-Z]` pattern the scorecard looks for.
- If the profiler's `SearchableFields` is too narrow (missing resources), expand `collectStringFields` to include more field types.
- If the template generates the methods but they're not detected by the scorecard, check the scorecard's regex against the actual generated code.

**Patterns to follow:**
- Existing `Upsert{{pascal .Name}}` pattern in `store.go.tmpl`

**Test scenarios:**
- Happy path: Generate from Petstore spec → store has `SearchPets()` method (pets have string name/status fields)
- Happy path: Generate from Discord spec → store has `SearchMessages()` (messages have content field)
- Edge case: Spec with no string fields → no Search methods emitted (no crash)
- Integration: Run scorecard on generated CLI → data_pipeline_integrity ≥9/10

**Verification:**
- Generated store.go contains `func (s *Store) Search<Entity>` for entities with searchable fields
- Scorecard data_pipeline score improves

---

- [ ] **Unit 2: Auth inference from query parameter names**

**Goal:** OpenAPI parser infers query-param auth when specs lack `securitySchemes` but have common auth param names

**Requirements:** R2

**Dependencies:** None

**Files:**
- Modify: `internal/openapi/parser.go` (`mapAuth` function)
- Test: `internal/openapi/parser_test.go`

**Approach:**
- In `mapAuth()`, after `selectSecurityScheme()` returns nil (no scheme found):
- Walk all operations in `doc.Paths`, collect query params matching common auth names
- Auth param names to check: `key`, `api_key`, `apikey`, `access_token`, `token`
- Count how many operations have at least one auth-like query param
- If count / total operations > 0.3 (30%), set `auth.Type = "api_key"`, `auth.In = "query"`, `auth.Header = <most_common_param_name>`
- Guard: only infer if the primary detection found no auth (`auth.Type == "" || auth.Type == "none"`)

**Patterns to follow:**
- Existing `mapAuth` flow: extract scheme → map to AuthConfig. The fallback scan is a new step after the primary detection.

**Test scenarios:**
- Happy path: Steam-like spec with no securitySchemes but `key` param on 47/158 operations → auth detected as `in: query, header: key`
- Happy path: Stripe spec with explicit bearer auth → fallback does NOT run (guard works)
- Edge case: Spec with `key` param on only 2/100 operations → no auth inferred (below 30%)
- Edge case: Spec with both `key` and `api_key` params → uses the more common one
- Negative: Spec with no query params at all → no auth inferred, no crash

**Verification:**
- Parse Steam public spec → `Auth.In == "query"` and `Auth.Header == "key"`
- Parse Stripe spec → bearer auth unchanged
- Scorecard auth score improves for specs with undeclared query-param auth

---

- [ ] **Unit 3: Verify auth env passthrough**

**Goal:** Verify passes all auth-related env vars from the spec to command test subprocesses

**Requirements:** R3

**Dependencies:** None (independent, but benefits from Unit 2 which provides more auth env vars)

**Files:**
- Modify: `internal/pipeline/runtime.go` (`RunVerify` function, env construction)
- Test: `internal/pipeline/runtime_test.go`

**Approach:**
- In `RunVerify`, where the env is constructed (lines 123-147), iterate `spec.Auth.EnvVars` instead of using a single hardcoded env var name
- For each env var in the list, check if it's set in the current environment and pass it through
- In mock mode, pass all auth env vars with `mock-token-for-testing` value
- The verify config already carries the spec — just need to read `cfg.Spec.Auth.EnvVars`

**Patterns to follow:**
- Existing env construction pattern at lines 123-147

**Test scenarios:**
- Happy path: Spec with `Auth.EnvVars: ["STEAM_API_KEY"]` → env includes `STEAM_API_KEY` during test
- Happy path: Spec with multiple env vars `["DISCORD_TOKEN", "DISCORD_BOT_TOKEN"]` → both passed
- Edge case: No env vars in spec → env construction unchanged (no crash)
- Integration: Run verify on Steam CLI with STEAM_API_KEY set → auth-requiring commands pass dry-run

**Verification:**
- Verify pass rate improves for CLIs with auth (75% → ~95% for Steam)

---

- [ ] **Unit 4: Sync path resolution for non-REST APIs**

**Goal:** Sync uses actual endpoint paths from the profiler instead of naive `"/" + resource` construction

**Requirements:** R4

**Dependencies:** None

**Files:**
- Modify: `internal/profiler/profiler.go` (`SyncableResources` type, population logic)
- Modify: `internal/generator/generator.go` (data struct passed to sync template)
- Modify: `internal/generator/templates/sync.go.tmpl` (use stored path)
- Test: `internal/profiler/profiler_test.go`

**Approach:**
- Define a `SyncableResource` struct with `Name` and `Path` fields in `profiler.go`
- Change `SyncableResources` from `[]string` to `[]SyncableResource`
- In the profiler's syncable detection (line 141), when adding a resource, also store the endpoint's `Path` field
- Update the generator's data struct to pass the new type to the sync template
- In `sync.go.tmpl`, replace `path := "/" + resource` with the stored path
- Keep `resource` as the display name / sync state key — only the API path changes

**Patterns to follow:**
- Existing `TableDef` struct pattern in `generator.go` — struct with Name + metadata passed to templates

**Test scenarios:**
- Happy path: Steam spec with `/ISteamApps/GetAppList/v2/` → sync uses actual path, not `/isteam-apps`
- Happy path: Stripe spec with `/customers` → sync still uses `/customers` (REST path = resource name)
- Edge case: Resource with no list endpoint → not in SyncableResources (no crash)
- Negative: Profiler output for Petstore → SyncableResources have correct paths

**Verification:**
- Generate from Steam spec → `sync --resources isteam-apps` calls `/ISteamApps/GetAppList/v2/`
- Generate from Stripe/Petstore spec → sync paths unchanged

## System-Wide Impact

- **Store template (Unit 1):** Adds methods to every generated store. Existing CLIs unaffected. New CLIs get more methods — compilation is additive (new functions, no changed signatures).
- **Auth inference (Unit 2):** Changes how the parser populates `AuthConfig`. Affects the generated `config.go` and `client.go` for APIs with undeclared auth. APIs with declared auth are unaffected (guard).
- **Verify env (Unit 3):** Changes verify's subprocess environment. More env vars passed = more commands can complete during testing. No regression risk — additional env vars don't break commands that don't use them.
- **Sync path (Unit 4):** Changes the `SyncableResources` type from `[]string` to `[]SyncableResource`. This ripples through profiler → generator → template. The struct carries the same data plus the path — no data is lost.
- **Unchanged:** Scorecard dimensions, dogfood detection, verify command discovery, template output for non-sync commands.

## Risks & Dependencies

| Risk | Mitigation |
|------|------------|
| Auth inference false positive (infers auth on public API) | 30% threshold + guard against overriding detected auth |
| SyncableResource struct change breaks profiler tests | Update profiler_test.go assertions to use the new struct |
| Store Search methods already emitted but not scoring | Investigate before implementing — may be a scorecard detection gap |
| Sync template change breaks existing REST CLIs | Store the REST path (`/customers`) as-is — only non-REST CLIs get different paths |

## Sources & References

- **Origin:** [docs/retros/2026-04-01-steam-run3-retro.md](docs/retros/2026-04-01-steam-run3-retro.md) — findings #1, #2, #3, #6
- **Prior retros:** [docs/retros/2026-03-31-steam-retro.md](docs/retros/2026-03-31-steam-retro.md) (findings #5, #6, #8), [docs/retros/2026-03-31-steam-run2-retro.md](docs/retros/2026-03-31-steam-run2-retro.md) (findings #8, #11)
- Store template: `internal/generator/templates/store.go.tmpl`
- Auth parser: `internal/openapi/parser.go` (`mapAuth`)
- Verify runtime: `internal/pipeline/runtime.go` (`RunVerify`)
- Profiler: `internal/profiler/profiler.go` (`SyncableResources`)
- PRs #100-102 (mvanhorn/cli-printing-press)
