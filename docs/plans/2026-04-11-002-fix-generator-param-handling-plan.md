---
title: "fix: Generator param handling — positional query params, required defaults, dot column names"
type: fix
status: active
date: 2026-04-11
origin: docs/retros/2026-04-11-movie-goat-retro.md
---

# fix: Generator param handling — positional query params, required defaults, dot column names

## Overview

Three generator bugs discovered during the movie-goat session cause silent failures, usability errors, and runtime crashes in printed CLIs. All three are in the generator's template/schema layer and affect most future CLIs — not just movie-goat.

## Problem Frame

1. **Positional params emitted as path substitutions even when they're query params.** Search endpoints like `/search/movie?query=Inception` get `replacePathParam(path, "query", args[0])` which silently drops the query because `{query}` doesn't exist in the path. Search commands return 0 results with no error.

2. **Required-flag check emitted even when a default exists.** Enum params with `required: true` and `default: "day"` still error with "required flag not set" when invoked without the flag, despite the default being perfectly valid.

3. **Dot-notation param names produce invalid SQLite column names.** Params like `primary_release_date.gte` pass through `toSnakeCase()` unchanged, producing `CREATE TABLE (..., primary_release_date.gte TEXT)` which crashes with a SQL syntax error on first database access.

## Requirements Trace

- R1. Positional params that don't appear in the path template must be emitted as query params
- R2. Required-flag checks must be skipped when the param has a non-nil default value
- R3. SQLite column names must be valid identifiers — dots replaced with underscores

## Scope Boundaries

- These are generator template and schema builder fixes only
- No changes to the spec parser, OpenAPI parser, or spec format
- No changes to printed CLIs — they benefit on next generation

## Context & Research

### Relevant Code and Patterns

- `internal/generator/templates/command_endpoint.go.tmpl` lines 70-83 — positional param emission
- `internal/generator/templates/command_endpoint.go.tmpl` lines 46-51 — required check guard
- `internal/generator/schema_builder.go` `toSnakeCase()` lines 361-377 — column name conversion
- `internal/generator/schema_builder.go` `safeSQLName()` lines 338-343 — SQL reserved word quoting

### Institutional Learnings

- (see origin: `docs/retros/2026-04-11-movie-goat-retro.md`) — F1, F2, F3 findings with full session evidence

## Key Technical Decisions

- **Positional query params: check path template, not param metadata.** The param's `.Positional` flag means "take from args" but doesn't distinguish path-segment from query-string. The path template (`/movie/{movieId}` vs `/search/movie`) is the authoritative source for where the value goes.
- **Dot replacement in toSnakeCase, not quoting in safeSQLName.** Replacing dots with underscores is cleaner than quoting — `vote_average_gte` is a better column name than `"vote_average.gte"`. The column is internal to the store, not user-facing.

## Open Questions

### Resolved During Planning

- **Should the template use a Go template function or inline logic?** Inline `strings.Contains` check in the template is sufficient — no new template function needed. The path string is available at template render time.
- **Should `toSnakeCase` also handle other special chars?** Yes, but scoped to common API param characters: dots, brackets, slashes. Dots are the priority since they're confirmed to cause crashes. Brackets and slashes can be deferred.

### Deferred to Implementation

- Exact behavior when a positional param name partially matches a path segment (e.g., param `id` with path `/items/{itemId}`) — unlikely edge case, verify with a test

## Implementation Units

- [ ] **Unit 1: Fix positional param emission in command template**

**Goal:** Positional params only use `replacePathParam` when their name appears as `{name}` in the endpoint path. Otherwise, they go into the query params map.

**Requirements:** R1

**Dependencies:** None

**Files:**
- Modify: `internal/generator/templates/command_endpoint.go.tmpl`
- Modify: `internal/generator/generator.go` (add `pathContainsParam` template function)
- Test: `internal/generator/generator_test.go`

**Approach:**
- Add a template function `pathContainsParam(path, name string) bool` that checks `strings.Contains(path, "{"+name+"}")` to `generator.go`'s funcMap
- In the template, wrap the `replacePathParam` emission for positional params in `{{if pathContainsParam .Endpoint.Path .Name}}`. If false, emit `params["{{.Name}}"] = args[{{$i}}]` instead
- The non-paginated branch (line 114+) and paginated branch (line 97+) both need the positional param added to their params maps when it's a query param

**Patterns to follow:**
- Existing template functions in `generator.go` funcMap (e.g., `flagName`, `camel`, `lower`)
- The `{{if and (not .Positional) (not .PathParam)}}` pattern already used for query params on lines 99 and 107

**Test scenarios:**
- Happy path: spec with positional `query` param on path `/search/movie` (no `{query}` in path) -> generated code emits `params["query"] = args[0]`, NOT `replacePathParam`
- Happy path: spec with positional `movieId` param on path `/movie/{movieId}` -> generated code emits `replacePathParam(path, "movieId", args[0])` as before
- Edge case: spec with two positional params, one in path and one not (e.g., `seriesId` in `/tv/{seriesId}/season` and `query` not in path) -> first uses replacePathParam, second uses params map
- Edge case: positional param name is substring of path segment (param `id` with path `/items/{itemId}`) -> should NOT match, should go to query params

**Verification:**
- Generate a CLI from a spec with search-style positional query params and verify the output code uses the params map
- `go test ./internal/generator/...` passes

- [ ] **Unit 2: Skip required check when default exists**

**Goal:** Params with `required: true` AND a non-nil `.Default` should not emit the "required flag not set" guard.

**Requirements:** R2

**Dependencies:** None (can be done in parallel with Unit 1)

**Files:**
- Modify: `internal/generator/templates/command_endpoint.go.tmpl`
- Test: `internal/generator/generator_test.go`

**Approach:**
- Change line 47 from `{{if and .Required (not .Positional)}}` to `{{if and .Required (not .Positional) (not .Default)}}`
- Same change for body params on line 56-57: add `(not .Default)` to the condition

**Patterns to follow:**
- The existing `(not .Positional)` guard on the same line

**Test scenarios:**
- Happy path: param with `required: true, default: "day"` -> no required check emitted in generated code
- Happy path: param with `required: true, default: nil` -> required check still emitted
- Edge case: param with `required: false, default: "day"` -> no required check (not required)
- Edge case: body param with `required: true, default: "json"` -> no required check for body params either

**Verification:**
- Generate a CLI from a spec with enum params that have defaults and verify the commands work without passing the flag
- `go test ./internal/generator/...` passes

- [ ] **Unit 3: Sanitize dots in SQLite column names**

**Goal:** `toSnakeCase()` replaces dots with underscores so generated SQLite migrations use valid column names.

**Requirements:** R3

**Dependencies:** None (can be done in parallel with Units 1 and 2)

**Files:**
- Modify: `internal/generator/schema_builder.go`
- Test: `internal/generator/schema_builder_test.go` (new file)

**Approach:**
- Add `s = strings.ReplaceAll(s, ".", "_")` as the first line of `toSnakeCase()`, before the hyphen replacement
- This converts `primary_release_date.gte` -> `primary_release_date_gte` and `vote_average.gte` -> `vote_average_gte`

**Patterns to follow:**
- The existing `strings.ReplaceAll(s, "-", "_")` on line 363

**Test scenarios:**
- Happy path: `toSnakeCase("primary_release_date.gte")` -> `"primary_release_date_gte"`
- Happy path: `toSnakeCase("vote_average.gte")` -> `"vote_average_gte"`
- Happy path: `toSnakeCase("field.nested.deep")` -> `"field_nested_deep"`
- Edge case: `toSnakeCase("movie_id")` -> `"movie_id"` (no dots, unchanged)
- Edge case: `toSnakeCase("camelCase")` -> `"camel_case"` (existing behavior preserved)
- Edge case: `toSnakeCase("kebab-case")` -> `"kebab_case"` (existing behavior preserved)
- Edge case: `toSnakeCase("with.dots-and-hyphens")` -> `"with_dots_and_hyphens"`

**Verification:**
- `go test ./internal/generator/...` passes with new table-driven test cases
- Generate a CLI from a spec with dot-notation params and verify the SQLite migration creates the table without errors

## System-Wide Impact

- **Interaction graph:** These are template-level fixes — they affect every future `printing-press generate` run. No runtime interaction changes.
- **Error propagation:** Unit 1 fixes a silent failure (0 results instead of error). Units 2 and 3 fix loud failures (error messages, crashes) — both are better after the fix.
- **Unchanged invariants:** The spec parser, OpenAPI parser, and all existing non-positional query param handling are unaffected. Path params that correctly appear in the path template still work as before.

## Risks & Dependencies

| Risk | Mitigation |
|------|------------|
| Unit 1 template change could affect non-search positional params | The `pathContainsParam` check is precise — only skips `replacePathParam` when the param name is NOT in the path. Existing path params are unaffected. |
| Unit 3 dot replacement could collide with existing column names | Unlikely — `vote_average.gte` and `vote_average_gte` are semantically the same param. If a spec has both, the duplicate column would be caught by the `CREATE TABLE` statement. |

## Sources & References

- **Origin document:** [docs/retros/2026-04-11-movie-goat-retro.md](docs/retros/2026-04-11-movie-goat-retro.md) — findings F1, F2, F3
- Related issue: [mvanhorn/cli-printing-press#171](https://github.com/mvanhorn/cli-printing-press/issues/171)
- Template: `internal/generator/templates/command_endpoint.go.tmpl`
- Schema builder: `internal/generator/schema_builder.go`
