---
title: "fix: Address Kalshi retro findings — --name flag, sync wrapper keys, primary key detection, spec extension, registry ordering"
type: fix
status: active
date: 2026-04-10
origin: docs/retros/2026-04-10-kalshi-retro.md
---

# fix: Address Kalshi retro findings

## Overview

Five small fixes surfaced during the Kalshi CLI generation (issue #163). Three P1 fixes prevent the most common manual edits during generation. Two P2 fixes eliminate minor friction in archiving and publishing.

## Problem Frame

When generating the Kalshi CLI, the session required 4 full file rewrites and 3 sync pipeline patches — all for issues that recur across APIs. The `--name` flag was silently ignored, sync couldn't detect response wrapper keys, and `UpsertBatch` silently skipped all records that didn't have an `id` field. These are systemic template/binary issues, not Kalshi-specific.

## Requirements Trace

- R1. `--name` flag must override the spec-derived name for single-spec generation
- R2. Sync must detect API responses wrapped with the resource name as key (e.g., `{"markets": [...]}`)
- R3. `UpsertBatch` must find primary keys using the same fallback list as `extractID`
- R4. Archived spec files must use the correct extension based on content format
- R5. Publish skill must instruct agents to sort registry.json entries alphabetically

## Scope Boundaries

- No new auth template variants (F5 from retro — deferred as P3/large)
- No profiler-based primary key detection (future improvement on top of WU-3)
- No changes to how `cleanSpecName` derives names from spec titles

## Context & Research

### Relevant Code and Patterns

- `internal/cli/root.go` — generate command, spec archiving (lines 254-414)
- `internal/generator/templates/sync.go.tmpl` — `extractPageItems` (line 357), `extractID` (line 485)
- `internal/generator/templates/store.go.tmpl` — `UpsertBatch` (line 346)
- `skills/printing-press-publish/SKILL.md` — Step 8 registry.json update
- `internal/openapi/parser_test.go` — `TestCleanSpecName` (existing test pattern)

### Institutional Learnings

- From retro: silent failures (0 records synced with no error) are worse than crashes — users don't discover the problem until dogfood testing

## Key Technical Decisions

- **WU-2 uses heuristic detection, not profiler data:** After the hardcoded keys fail, try every key in the envelope — if exactly one maps to a JSON array, use it. This covers all resource-name-as-wrapper APIs universally without touching the profiler.
- **WU-3 expands the hardcoded list, not the profiler:** Adding `ticker`, `key`, `code`, `uid` to the fallback list is simpler and sufficient. Profiler-based detection can come later.
- **WU-4 checks JSON validity, not OpenAPI format:** `json.Valid()` correctly distinguishes JSON from YAML regardless of schema type.

## Implementation Units

- [ ] **Unit 1: Fix --name flag for single-spec generation**

**Goal:** When `--name` is passed with a single spec, the generated CLI uses that name instead of the spec title.

**Requirements:** R1

**Dependencies:** None

**Files:**
- Modify: `internal/cli/root.go`
- Test: `internal/cli/root_test.go` (or integration test)

**Approach:**
- After `apiSpec` is resolved from a single spec (line 306), check if `cliName != ""` and override `apiSpec.Name` with `cleanSpecName(cliName)`. This must happen before `DefaultOutputDir`, `generator.New`, the directory rename, and the manifest write.
- Apply `cleanSpecName` to the override so `--name "My Cool API"` normalizes to `my-cool-api`, consistent with spec-title derivation.

**Patterns to follow:**
- The multi-spec branch already uses `cliName` at line 312 (`mergeSpecs(specs, cliName)`)
- `cleanSpecName` in `internal/openapi/parser.go` is the canonical normalizer

**Test scenarios:**
- Happy path: `--spec verbose-title.yaml --name short` → `apiSpec.Name == "short"`, output dir is `short`, go.mod is `short-pp-cli`
- Happy path: `--spec verbose-title.yaml` (no --name) → name derived from spec title (existing behavior preserved)
- Edge case: `--name "My Cool API"` → normalized to `my-cool-api` via `cleanSpecName`
- Integration: multi-spec with --name → existing behavior preserved (no regression)

**Verification:**
- `printing-press generate --spec testdata/petstore.yaml --name pet --dry-run` shows `pet` as the output name
- Existing `TestGenerateProjectsCompile` still passes

---

- [ ] **Unit 2: Universal sync wrapper key detection**

**Goal:** Sync automatically detects API response wrapper keys like `{"markets": [...]}` without manual edits.

**Requirements:** R2

**Dependencies:** None

**Files:**
- Modify: `internal/generator/templates/sync.go.tmpl`

**Approach:**
- In `extractPageItems`, after the hardcoded key loop fails, add a fallback: iterate all keys in the envelope. If exactly one key maps to a valid JSON array with `len > 0`, use it. This covers resource-name wrappers universally.
- Keep the hardcoded keys as the first pass (they're faster and avoid false positives on envelopes with multiple array keys like `{"data": [...], "errors": [...]}`).

**Patterns to follow:**
- The existing `extractPageItems` structure — try strategies in order, return on first match

**Test scenarios:**
- Happy path: envelope `{"markets": [...], "cursor": "abc"}` → items extracted from `markets`, cursor from `cursor`
- Happy path: envelope `{"data": [...]}` → existing behavior preserved (matched by hardcoded key)
- Edge case: envelope `{"count": 5, "status": "ok"}` → no array key found, returns nil (no false extraction)
- Edge case: envelope `{"items": [...], "errors": [...]}` → matched by hardcoded `items` key (first pass), not ambiguous fallback
- Edge case: envelope `{"users": [...], "groups": [...]}` → two array keys, fallback should not pick one arbitrarily — return nil and let `upsertSingleObject` handle it

**Verification:**
- Generate a CLI from an OpenAPI spec with resource-name wrappers, run sync, confirm records are stored

---

- [ ] **Unit 3: Expanded primary key detection in UpsertBatch**

**Goal:** `UpsertBatch` recognizes common non-`id` primary key field names, consistent with `extractID`.

**Requirements:** R3

**Dependencies:** None

**Files:**
- Modify: `internal/generator/templates/store.go.tmpl`
- Modify: `internal/generator/templates/sync.go.tmpl`

**Approach:**
- In `UpsertBatch` (store.go.tmpl line 346), replace the single `lookupFieldValue(obj, "id")` with a loop trying: `id`, `ID`, `ticker`, `event_ticker`, `series_ticker`, `key`, `code`, `uid`, `uuid`, `slug`, `name` — in that priority order, stopping on first non-empty match.
- In `extractID` (sync.go.tmpl line 486), add `ticker`, `key`, `code`, `uid` to the existing list for consistency.
- Add a stderr warning when >50% of items in a batch have no extractable ID — this prevents the silent-failure anti-pattern.

**Patterns to follow:**
- `extractID` already tries multiple fields — extend the same pattern to `UpsertBatch`

**Test scenarios:**
- Happy path: batch item with `{"ticker": "AAPL", "price": 150}` → upserted with id `AAPL`
- Happy path: batch item with `{"id": "123", "ticker": "AAPL"}` → upserted with id `123` (id takes priority)
- Happy path: batch item with `{"name": "foo"}` → existing fallback preserved
- Edge case: batch where all items lack any known key → warning printed to stderr, 0 records inserted (not silent)
- Edge case: batch with `{"slug": "my-post"}` → upserted with id `my-post`

**Verification:**
- Regenerate a CLI, verify `UpsertBatch` in the generated `store.go` has the expanded field list
- Verify `extractID` in the generated `sync.go` matches

---

- [ ] **Unit 4: Fix spec archive file extension**

**Goal:** Archived spec files use `.yaml` when the content is YAML and `.json` when the content is JSON.

**Requirements:** R4

**Dependencies:** None

**Files:**
- Modify: `internal/cli/root.go`

**Approach:**
- Replace the `!openapi.IsOpenAPI()` condition (which checks schema type, not content format) with `json.Valid(specRawBytes[0])`. JSON content → `spec.json`. Non-JSON content → `spec.yaml`.
- The `encoding/json` package is already imported in root.go.

**Patterns to follow:**
- Standard Go `json.Valid()` — returns true for valid JSON, false for everything else

**Test scenarios:**
- Happy path: YAML OpenAPI spec → archived as `spec.yaml`
- Happy path: JSON OpenAPI spec → archived as `spec.json`
- Happy path: internal YAML spec (non-OpenAPI) → archived as `spec.yaml`
- Edge case: empty spec bytes → skip archiving (existing guard handles this)

**Verification:**
- Generate from a YAML OpenAPI spec, verify `spec.yaml` exists in output (not `spec.json`)
- `printing-press scorecard --spec <output>/spec.yaml` parses successfully

---

- [ ] **Unit 5: Publish registry.json alphabetical ordering**

**Goal:** Registry entries are always sorted alphabetically by `name` after publish.

**Requirements:** R5

**Dependencies:** None

**Files:**
- Modify: `skills/printing-press-publish/SKILL.md`

**Approach:**
- Add one sentence to Step 8 "Update registry.json" after "Match on `name` field. Preserve `schema_version` and any other top-level fields." → "After adding or updating the entry, sort the `entries` array alphabetically by `name` field to prevent merge conflicts and keep the file reviewable."

**Test expectation:** none — this is a skill instruction change, not code. Verification is that subsequent publishes produce sorted registries.

**Verification:**
- Run `/printing-press-publish` after this change, verify registry.json entries are sorted by name

## System-Wide Impact

- **Generated CLIs affected:** Units 1-3 change templates and binary behavior. All future generated CLIs benefit. No existing CLIs are affected (they're already generated).
- **Existing tests:** `TestGenerateProjectsCompile` and `TestCleanSpecName` must continue to pass. No existing test covers `--name` for single specs (that's the bug).
- **Publish workflow:** Unit 5 affects all future publishes. Existing registry.json may get a one-time reorder on the next publish — this is desired.

## Risks & Dependencies

| Risk | Mitigation |
|------|------------|
| Universal wrapper key heuristic (WU-2) picks wrong key from multi-array envelope | Only activate when exactly one key maps to an array. Two+ arrays → fall through to existing behavior |
| Expanded ID fields (WU-3) cause false matches on non-ID fields | Priority order ensures `id` wins when present. `ticker`/`key`/`code` are strongly idiomatic as identifiers |

## Sources & References

- **Origin document:** [docs/retros/2026-04-10-kalshi-retro.md](docs/retros/2026-04-10-kalshi-retro.md)
- Related issue: #163
- Generator template: `internal/generator/templates/sync.go.tmpl`
- Generator template: `internal/generator/templates/store.go.tmpl`
- Binary: `internal/cli/root.go`
- Skill: `skills/printing-press-publish/SKILL.md`
