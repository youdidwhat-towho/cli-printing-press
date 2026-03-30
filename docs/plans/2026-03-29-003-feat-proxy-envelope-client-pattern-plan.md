---
title: "feat: Generator support for proxy-envelope client pattern"
type: feat
status: active
date: 2026-03-29
origin: docs/brainstorms/2026-03-29-client-pattern-proxy-envelope-requirements.md
---

# feat: Generator Support for Proxy-Envelope Client Pattern

## Overview

Teach the printing-press generator to emit a proxy-wrapping HTTP client when `client_pattern: proxy-envelope` is set. Instead of building `base_url + path` URLs, the generated client wraps all requests as `POST base_url` with a `{service, method, path, body?}` JSON envelope. This eliminates the single most expensive manual customization needed for sniffed APIs.

## Problem Frame

During the postman-explore-pp-cli build, the proxy client wrapper had to be re-applied 3 times across regeneration cycles — each time carefully preserving rate limiter integration, retry logic, and caching. The `client_pattern` field exists in the catalog schema (PR #61) but the generator ignores it. (see origin: docs/brainstorms/2026-03-29-client-pattern-proxy-envelope-requirements.md)

## Requirements Trace

- R1. Proxy-wrapping `do()` when `ClientPattern == "proxy-envelope"`
- R2. Envelope: `{service, method, path, body?}` POST'd to `BaseURL`
- R3. Rate limiter, retry, cache, dry-run unchanged
- R4. Auth still emitted when spec defines it
- R5-R7. Service routing: default single-service (API slug), override via `x-proxy-routes`
- R8-R9. `ClientPattern` on APISpec, set via `--client-pattern` flag
- R10. Template conditional in `client.go.tmpl`
- R11-R12. Skill integration for sniff detection and generate flags

## Scope Boundaries

- Proxy-envelope only — no GraphQL pattern
- No envelope shape configurability — `{service, method, path, body?}` is hardcoded
- No catalog schema changes (already has `client_pattern`)
- Skill sniff detection is heuristic guidance, not automated detection

## Context & Research

### Relevant Code and Patterns

- `internal/generator/templates/client.go.tmpl` — The `do()` method has clearly separable sections: request construction (lines 231-268) is independent from the retry loop (lines 224-318). The proxy conditional wraps only the request construction, sharing all retry/cache/rate-limiter logic.
- `internal/openapi/parser.go:140-148` — Existing pattern for extracting OpenAPI extensions (`x-api-name` from `doc.Info.Extensions`). The `x-proxy-routes` extension follows this exact pattern.
- `internal/spec/spec.go` — `SpecSource` field was added in PR #62 following the same pattern `ClientPattern` will follow.
- `internal/generator/generator.go:123` — Templates receive `*spec.APISpec` directly via `renderTemplate(tmplName, outPath, g.Spec)`. New fields on APISpec are automatically accessible in templates.
- `internal/cli/root.go:322` — `--spec-source` flag pattern to follow for `--client-pattern`.

### Institutional Learnings

- `docs/solutions/best-practices/adaptive-rate-limiting-sniffed-apis.md` — Documents the adaptive rate limiter that the proxy client must preserve. The learning confirms that the proxy pattern requires the rate limiter integration to stay intact.

## Key Technical Decisions

- **Template conditional, not separate template file**: The proxy `do()` differs only in request construction (~15 lines). The retry loop, rate limiter calls, response handling, and error classification are identical. A `{{if eq .ClientPattern "proxy-envelope"}}` block wrapping the request construction section avoids duplicating ~100 lines of shared logic. (see origin)
- **Shared retry loop with strategy injection**: Rather than duplicating the entire `do()` method, the template emits a `buildRequest()` helper that returns `*http.Request`. The `do()` loop calls `buildRequest()` then proceeds with shared retry logic. The conditional controls which `buildRequest()` is emitted.
- **x-proxy-routes in spec info.extensions**: Follows the existing `x-api-name` pattern. Parsed during OpenAPI loading, stored on APISpec as a `map[string]string`. When absent, a default route maps all paths to the API name slug.
- **`--client-pattern` flag validated same as `--spec-source`**: Allowed values: `rest`, `proxy-envelope`. Invalid values rejected with a clear error message.

## Open Questions

### Resolved During Planning

- **How much of `do()` can be shared?** The request construction section (lines 231-268) is the only divergent part. The template emits a `buildRequest()` helper selected by `ClientPattern`. The rest of `do()` is shared.
- **x-proxy-routes schema:** `info.extensions["x-proxy-routes"]` as a `map[string]string` mapping path prefixes to service names. Parsed in `openapi/parser.go` alongside `x-api-name`. Stored on APISpec as `ProxyRoutes map[string]string`.
- **Proxy pattern detection heuristic:** During sniff, if all captured Performance API URLs resolve to the same path AND intercepted fetch bodies contain `{service, method, path}` keys, it's a proxy-envelope. This is behavioral guidance in SKILL.md, not automated detection.

### Deferred to Implementation

- Exact placement of the `buildRequest()` helper in client.go.tmpl — depends on reading the current template structure at implementation time
- Whether `x-proxy-routes` needs path prefix matching (longest match) or exact match — start with prefix match, simplify if it turns out to be unnecessary

## High-Level Technical Design

> *This illustrates the intended approach and is directional guidance for review, not implementation specification. The implementing agent should treat it as context, not code to reproduce.*

```
client.go.tmpl do() method structure:

  func (c *Client) do(method, path, params, body) {
    url = c.BaseURL + path          // ← REST path
    // OR
    url = c.BaseURL                 // ← proxy-envelope (always same URL)

    bodyBytes = marshal(body)

    if c.DryRun { return dryRun(...) }

    for attempt := 0..maxRetries {
      c.limiter.Wait()               // shared

      req = buildRequest(...)        // ← CONDITIONAL: REST vs proxy-envelope

      resp = c.HTTPClient.Do(req)    // shared

      if resp < 400 {                // shared
        c.limiter.OnSuccess()
        return resp
      }
      if resp == 429 {               // shared
        c.limiter.OnRateLimit()
        sleep(retryAfter)
      }
      if resp >= 500 { ... }         // shared
    }
  }

  // REST buildRequest: standard HTTP method + URL + params + body
  // Proxy buildRequest: POST + BaseURL + envelope{service, method, path+params, body}
```

## Implementation Units

- [ ] **Unit 1: Add ClientPattern and ProxyRoutes to APISpec**

**Goal:** Make `client_pattern` and `x-proxy-routes` available to templates.

**Requirements:** R8

**Dependencies:** None

**Files:**
- Modify: `internal/spec/spec.go`
- Modify: `internal/openapi/parser.go`
- Test: `internal/spec/spec_test.go`
- Test: `internal/openapi/parser_test.go`

**Approach:**
- Add `ClientPattern string` and `ProxyRoutes map[string]string` to `APISpec`
- In `openapi/parser.go`, extract `x-proxy-routes` from `doc.Info.Extensions` (follow the `x-api-name` pattern at line 140)
- ProxyRoutes is a simple `map[string]string` where keys are path prefixes and values are service names

**Patterns to follow:**
- `SpecSource` field addition in PR #62
- `x-api-name` extension extraction in `openapi/parser.go:140-148`

**Test scenarios:**
- Happy path: Spec with `x-proxy-routes: {"/v1/api/": "publishing", "/search-all": "search"}` → parsed APISpec has ProxyRoutes map with both entries
- Happy path: Spec without `x-proxy-routes` → ProxyRoutes is nil/empty
- Edge case: `x-proxy-routes` with wrong type (not a map) → gracefully ignored, ProxyRoutes stays nil

**Verification:**
- `go test ./internal/spec/... ./internal/openapi/...` passes
- Template can access `{{.ClientPattern}}` and `{{.ProxyRoutes}}`

- [ ] **Unit 2: Add --client-pattern flag to generate command**

**Goal:** Users can set `ClientPattern` via CLI flag.

**Requirements:** R9

**Dependencies:** Unit 1

**Files:**
- Modify: `internal/cli/root.go`
- Test: `internal/cli/root_test.go` (if exists)

**Approach:**
- Add `--client-pattern` flag (string, default empty)
- Validate against `rest`, `proxy-envelope` (same pattern as `--spec-source` validation)
- Set `apiSpec.ClientPattern` after parsing, in both the `--docs` and `--spec` paths
- Empty string means "rest" (default behavior)

**Patterns to follow:**
- `--spec-source` flag at line 322 and validation at lines 226-233

**Test scenarios:**
- Happy path: `--client-pattern proxy-envelope` → apiSpec.ClientPattern set to "proxy-envelope"
- Happy path: No flag → ClientPattern empty (REST default)
- Error path: `--client-pattern soap` → exits with validation error naming allowed values

**Verification:**
- `printing-press generate --help` shows `--client-pattern` flag
- Invalid values rejected with clear error

- [ ] **Unit 3: Emit proxy-envelope client in client.go.tmpl**

**Goal:** The template emits a proxy-wrapping `do()` method when `ClientPattern == "proxy-envelope"`.

**Requirements:** R1, R2, R3, R4, R5, R6, R7, R10

**Dependencies:** Unit 1, Unit 2

**Files:**
- Modify: `internal/generator/templates/client.go.tmpl`
- Test: `internal/generator/generator_test.go`

**Approach:**
- Add `proxyEnvelope` struct and `serviceForPath()` helper inside a `{{if eq .ClientPattern "proxy-envelope"}}` block
- `serviceForPath()` checks `ProxyRoutes` map (longest prefix match), falls back to API name slug
- Replace the request construction section of `do()` with a conditional: proxy-envelope wraps in envelope and POST's to `BaseURL`; REST builds standard HTTP request (existing behavior)
- Both paths share the retry loop, rate limiter, cache, and response handling
- Dry-run output shows the envelope for proxy-envelope mode
- MCP tools template (`mcp_tools.go.tmpl`) also needs the conditional for `newMCPClient()`

**Patterns to follow:**
- Existing `{{if eq .SpecSource "sniffed"}}` conditionals in `root.go.tmpl` and `mcp_tools.go.tmpl`
- The hand-built proxy client in postman-explore-pp-cli's `client.go` (the proven implementation)

**Test scenarios:**
- Happy path: Generate from spec with `ClientPattern = "proxy-envelope"` → client.go contains `proxyEnvelope` struct and `serviceForPath()` function
- Happy path: Generate from spec with `ClientPattern = ""` → client.go is identical to today's output (no proxy code)
- Happy path: ProxyRoutes with 2 entries → `serviceForPath()` routes correctly by prefix
- Edge case: Empty ProxyRoutes → `serviceForPath()` returns API name slug for all paths
- Integration: Generated proxy-envelope CLI compiles and passes `go vet`

**Verification:**
- `go test ./internal/generator/...` passes (compilation tests verify generated code compiles)
- A generated CLI with `--client-pattern proxy-envelope` has a working proxy `do()` method

- [ ] **Unit 4: Update SKILL.md with proxy pattern detection and generate flags**

**Goal:** The skill knows how to detect proxy patterns during sniffing and passes the right flags to generate.

**Requirements:** R11, R12

**Dependencies:** None (parallel with Units 1-3)

**Files:**
- Modify: `skills/printing-press/SKILL.md`

**Approach:**
- In the sniff gate section, add a "Proxy Pattern Detection" heuristic: if all captured XHR/fetch URLs resolve to the same path AND intercepted bodies contain `{service, method, path}` keys, flag it as proxy-envelope
- When a proxy pattern is detected, the skill should: (a) write `x-proxy-routes` into the spec if multiple services are discovered, (b) pass `--client-pattern proxy-envelope` to the generate command
- Update Phase 2 generate commands for sniff-based CLIs to include `--client-pattern proxy-envelope` when detected

**Test scenarios:**
Test expectation: none — skill instruction changes are behavioral guidance for Claude, not compiled code.

**Verification:**
- SKILL.md contains proxy pattern detection heuristic in sniff gate section
- Phase 2 generate commands include `--client-pattern proxy-envelope` when appropriate

## System-Wide Impact

- **Template backward compatibility:** Empty `ClientPattern` produces identical output to today. No existing CLIs are affected.
- **Generated CLI dependencies:** No new dependencies. The proxy envelope code uses stdlib only (same as the hand-built version).
- **Catalog integration:** The catalog already has `client_pattern`. When the generate command learns to read from catalog entries, proxy-envelope will automatically propagate. This is a future enhancement, not required now.
- **MCP server parity:** The `mcp_tools.go.tmpl` also constructs a client. It needs the same conditional to work with proxy-envelope APIs.

## Risks & Dependencies

| Risk | Mitigation |
|------|------------|
| Template conditional makes client.go.tmpl harder to read | The conditional wraps only the request construction section (~15 lines per branch), not the entire 100-line do() method. Code review will catch readability issues. |
| Envelope shape assumption (`{service, method, path, body?}`) is too rigid | Scoped as a known limitation. The hardcoded shape matches the only proxy pattern we've encountered. Configurability is a future enhancement if needed. |
| `x-proxy-routes` prefix matching gets complex | Start with simple iteration over the map. Longest-prefix-match is a well-understood algorithm. If the map has <10 entries (typical), performance is irrelevant. |

## Sources & References

- **Origin document:** [docs/brainstorms/2026-03-29-client-pattern-proxy-envelope-requirements.md](docs/brainstorms/2026-03-29-client-pattern-proxy-envelope-requirements.md)
- Related code: `internal/generator/templates/client.go.tmpl` (do() method), `internal/openapi/parser.go` (x-api-name pattern)
- Related PRs: #61 (catalog schema with client_pattern), #62 (adaptive rate limiter with SpecSource)
- Related learning: `docs/solutions/best-practices/adaptive-rate-limiting-sniffed-apis.md`
