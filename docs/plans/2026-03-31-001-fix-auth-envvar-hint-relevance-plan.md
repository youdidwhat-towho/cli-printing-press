---
title: "fix: Filter crowd-sniff auth env var hints by API name relevance"
type: fix
status: active
date: 2026-03-31
origin: docs/retros/2026-03-30-steam-retro.md
---

# fix: Filter crowd-sniff auth env var hints by API name relevance

## Overview

`extractEnvVarHint()` returns the first env var matching the auth keyword pattern, regardless of whether it's related to the API being sniffed. For Steam, it picked `COOKIE_SECRET` from a random SDK instead of deriving `STEAM_API_KEY` from the API name.

## Problem Frame

When crowd-sniff scans npm SDK source code, it finds all `process.env.X` references matching auth keywords (API, KEY, TOKEN, SECRET). The first match wins, even if it's from unrelated code in the SDK (cookie handling, session management). The generated CLI then uses this wrong env var name, causing auth to silently fail unless the user notices and fixes it.

## Requirements Trace

- R1. Env var hints containing the API name (case-insensitive) are preferred over generic matches
- R2. If no API-name-relevant hint is found, fall back to `deriveEnvVar()` (generates `STEAM_API_KEY` from api name + auth type) rather than using a random match
- R3. No regressions for APIs where the env var hint IS relevant (e.g., `process.env.NOTION_API_KEY` for a Notion API scan)

## Scope Boundaries

- Only changes `extractEnvVarHint()` in `internal/crowdsniff/npm.go`
- Does not change how auth type/header/in are detected — only the env var name

## Key Technical Decisions

- **Prefer API-name match over first-match**: Score candidates by whether they contain the API name. `STEAM_API_KEY` scores higher than `COOKIE_SECRET` when sniffing "steam".
- **Fall back to deriveEnvVar, not first match**: If no candidate contains the API name, return empty string and let `deriveEnvVar()` in specgen.go generate a sensible default. A derived name (`STEAM_API_KEY`) is better than a random match (`COOKIE_SECRET`).

## Implementation Units

- [ ] **Unit 1: Add API name relevance filter to extractEnvVarHint**

  **Goal:** `extractEnvVarHint` prefers env vars containing the API name, falls back to empty string if none match.

  **Requirements:** R1, R2, R3

  **Dependencies:** None

  **Files:**
  - Modify: `internal/crowdsniff/npm.go` — change `extractEnvVarHint` signature to accept api name, filter candidates
  - Modify: `internal/crowdsniff/npm.go` — update call site in `GrepAuth` to pass api name
  - Test: `internal/crowdsniff/npm_test.go`

  **Approach:**
  - Change `extractEnvVarHint(content string)` to `extractEnvVarHint(content string, apiName string)`
  - Collect all matches, then score: contains upper(apiName) → priority 1, generic auth keyword only → priority 2
  - Return the highest-priority match. If only priority 2 matches exist, return empty string (let deriveEnvVar handle it)
  - Update `GrepAuth` to accept and pass the api name — it currently receives `sourceTier` but not the api name. Add `apiName` parameter.
  - Update callers: `processPackageTarball` and `Discover` pass the api name through

  **Patterns to follow:**
  - Existing `GrepAuth` function signature and pattern

  **Test scenarios:**
  - Happy path: Content with `process.env.STEAM_API_KEY` and `process.env.COOKIE_SECRET`, api name "steam" → returns `STEAM_API_KEY`
  - Happy path: Content with `process.env.NOTION_API_KEY`, api name "notion" → returns `NOTION_API_KEY`
  - Edge case: Content with only `process.env.COOKIE_SECRET`, api name "steam" → returns empty (falls back to deriveEnvVar)
  - Edge case: Content with no env var references → returns empty
  - Edge case: API name with special chars (e.g., "cal.com") → normalized for matching
  - Negative test: Content with `process.env.STEAM_API_KEY`, api name "notion" → does NOT return STEAM_API_KEY (wrong API)

  **Verification:**
  - `go build ./...` and `go test ./internal/crowdsniff/...` pass
  - Run `printing-press crowd-sniff --api steam` → env var is `STEAM_API_KEY`, not `COOKIE_SECRET`

## Sources & References

- **Origin:** [docs/retros/2026-03-30-steam-retro.md](docs/retros/2026-03-30-steam-retro.md) — finding #2 (auth detection)
- Target file: `internal/crowdsniff/npm.go:579-593` (`extractEnvVarHint`)
- Related: `internal/crowdsniff/specgen.go` (`deriveEnvVar` fallback)
