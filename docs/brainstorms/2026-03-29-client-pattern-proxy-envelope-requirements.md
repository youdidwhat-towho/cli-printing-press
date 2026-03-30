---
date: 2026-03-29
topic: client-pattern-proxy-envelope
---

# Generator Support for Proxy-Envelope Client Pattern

## Problem Frame

When generating CLIs for sniffed APIs that use a proxy pattern (all requests POST'd to a single URL with a `{service, method, path, body?}` envelope), the generator produces a standard REST client that doesn't work. The skill must then manually rewrite `do()` in the generated `client.go` — a ~50-line change that must preserve rate limiter integration, retry logic, and caching. This customization had to be re-applied 3 times during the postman-explore-pp-cli build (once per regeneration cycle), making it the single most expensive post-generation fix.

The `client_pattern` field already exists in the catalog schema (PR #61) with `proxy-envelope` as a valid value, but the generator ignores it completely.

## Requirements

**Generator Template Selection**

- R1. When `spec.APISpec.ClientPattern` is `"proxy-envelope"`, the generator emits a proxy-wrapping `do()` method in `client.go` instead of the standard REST `do()` method
- R2. The proxy `do()` always POST's to `c.BaseURL` with a JSON envelope containing `service`, `method` (the logical HTTP method), `path` (with query params inlined), and `body` (for POST/PUT/PATCH requests)
- R3. All existing client features work unchanged with the proxy pattern: adaptive rate limiter, retry-on-429, exponential backoff on 5xx, caching for GET, dry-run display
- R4. Auth methods (`authHeader`, `refreshAccessToken`) are still emitted when the spec defines auth. Proxy-envelope does not imply no-auth — that's a separate concern controlled by the spec's `auth` section

**Service Routing**

- R5. Default: all paths route to a single service named after the API slug (e.g., `"postman-explore"`)
- R6. Override: if the spec contains an `x-proxy-routes` extension (a map of path prefixes to service names), the generated client uses it for routing
- R7. The `x-proxy-routes` extension is optional. When absent, the single-service default applies

**Plumbing**

- R8. Add `ClientPattern string` field to `spec.APISpec` (alongside the existing `SpecSource`)
- R9. The generate CLI command sets `ClientPattern` from a new `--client-pattern` flag or from the catalog entry when generating from a catalog API
- R10. The `client.go.tmpl` template uses `{{if eq .ClientPattern "proxy-envelope"}}` conditionals around the `do()` method and proxy-specific types

**Skill Integration**

- R11. During the sniff phase, when the skill discovers a proxy pattern (repeated calls to a single URL with envelope bodies), it writes `x-proxy-routes` into the generated spec
- R12. The skill's Phase 2 generate commands for sniff-based CLIs include `--client-pattern proxy-envelope`

## Success Criteria

- A `printing-press generate --spec <sniffed-spec> --client-pattern proxy-envelope` produces a CLI that works against the proxy API without any manual client.go modifications
- Regenerating the postman-explore-pp-cli with the proxy spec and `--client-pattern proxy-envelope` produces a working CLI where only UX customizations (top-level aliases, store tables) are needed — the client itself works out of the box
- Standard REST APIs are unaffected — `client_pattern` empty or `"rest"` produces the same `client.go` as today

## Scope Boundaries

- Proxy-envelope pattern only — no GraphQL client pattern in this work
- No changes to the catalog schema (already has `client_pattern`)
- No envelope shape configurability — the `{service, method, path, body?}` shape is hardcoded. If a future proxy API uses a different envelope shape, that's a separate enhancement
- The skill's sniff phase improvements (auto-detecting proxy patterns, writing x-proxy-routes) are included but lightweight — the skill already knows how to identify proxy patterns from the postman-explore experience

## Key Decisions

- **Convention + override for service routing**: Default single-service (API slug) covers most cases. `x-proxy-routes` handles multi-service APIs like Postman. No catalog schema changes needed.
- **Template conditional, not separate template file**: One `client.go.tmpl` with `{{if eq .ClientPattern "proxy-envelope"}}` blocks. The proxy `do()` is different enough to warrant separate blocks but similar enough (same retry/cache/rate-limit integration) that a completely separate template would duplicate too much.
- **Proxy URL comes from existing `servers` field**: No new field needed. The spec's server URL is the proxy endpoint.

## Dependencies / Assumptions

- `spec.APISpec` already has `SpecSource` (PR #62). `ClientPattern` follows the same pattern.
- The catalog schema already has `client_pattern` with `proxy-envelope` as a valid value (PR #61).
- The `x-proxy-routes` OpenAPI extension is a custom extension — it's our convention, not a standard. That's fine for a generator-specific feature.

## Outstanding Questions

### Deferred to Planning
- [Affects R10][Technical] How much of the proxy `do()` can share code with the REST `do()`? The retry loop, rate limiter, and cache are identical — only the request building differs. The planner should determine whether to extract a shared `executeWithRetry()` helper or duplicate the loop with different request construction.
- [Affects R6][Needs research] What does `x-proxy-routes` look like in YAML? The planner should design the extension schema (e.g., `x-proxy-routes: {"/v1/api/": "publishing", "/search-all": "search"}`).
- [Affects R11][Needs research] How does the skill detect the proxy pattern during sniffing? Currently it's manual observation ("all calls go to _api/ws/proxy"). Can we formalize this as a heuristic (e.g., all captured URLs resolve to the same path)?

## Next Steps

→ `/ce:plan` for structured implementation planning
