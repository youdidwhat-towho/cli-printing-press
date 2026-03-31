---
date: 2026-03-31
topic: auth-error-handling
---

# Better Auth Error Handling in Generated CLIs

## Problem Frame

Generated CLIs give poor feedback when auth is misconfigured. Steam returns HTTP 400 ("Required parameter 'key' is missing") which the error handler doesn't recognize as auth-related. Error messages are generic ("check your credentials") and don't name the specific env var. The `doctor` command shows "Auth: not configured" without telling the user how to fix it. Users have to figure out the env var name, format, and source on their own.

This is a pipeline problem, not a single-fix problem. The auth metadata (env var name, auth type, where to get a key) flows from crowd-sniff → spec → generator → templates, but the last mile — the generated error messages and doctor output — doesn't use most of it.

## Requirements

**Error Message Quality**

- R1. `classifyAPIError` pattern-matches HTTP 400 response bodies for auth keywords (`key`, `auth`, `token`, `api_key`, `unauthorized`, `missing`) and treats matching 400s as auth errors with actionable hints
- R2. Auth error messages (400/401/403) include the specific env var name from the spec (e.g., "Set STEAM_API_KEY=..." not "check your credentials")
- R3. Auth error messages include a one-line setup command: `export STEAM_API_KEY=<your-key>`
- R4. Non-auth 400 errors (bad input, missing required field, validation) are NOT incorrectly classified as auth errors

**Doctor Command**

- R5. `doctor` output names the specific env var when auth is not configured: `Auth: not configured. Set STEAM_API_KEY in your environment`
- R6. `doctor` output confirms auth is configured with the source: `Auth: configured (env: STEAM_API_KEY)`

**Mixed-Auth APIs**

- R7. Auth checking is per-call, not per-startup — CLIs with endpoints that don't need auth still work without a key for those endpoints
- R8. No fail-fast startup gate — the auth error only surfaces when an endpoint that needs auth is called without credentials

**Pipeline: Auth Metadata Propagation**

- R9. The helpers.go.tmpl template has access to `{{.Auth.EnvVars}}` so error messages can reference the correct env var name at generation time
- R10. The doctor.go.tmpl template has access to `{{.Auth.EnvVars}}` to show specific setup instructions

**Key Registration URL Discovery**

- R11. Add `key_url` field to `spec.AuthConfig` for the URL where users can register/obtain an API key
- R12. Crowd-sniff extracts key registration URLs from npm SDK READMEs when present (patterns like "Get your API key at [url]", "Register at [url]", "Sign up for an API key: [url]")
- R13. When `key_url` is populated, error messages and doctor output include it: `Get a key at: https://steamcommunity.com/dev/apikey`
- R14. When `key_url` is NOT populated, error messages and doctor output omit the URL entirely — no placeholder, no "unknown", just skip that line. A missing URL is better than a wrong one.

## Success Criteria

- Running `steam-pp-cli player GabeLoganNewell` without STEAM_API_KEY shows: `error: API key required. Set STEAM_API_KEY in your environment: export STEAM_API_KEY=<your-key>` followed by `Get a key at: https://steamcommunity.com/dev/apikey` (when key_url is known)
- Running `steam-pp-cli players 440` without STEAM_API_KEY still works (endpoint doesn't need auth)
- Running `steam-pp-cli doctor` without STEAM_API_KEY shows: `Auth: not configured. Set STEAM_API_KEY in your environment` + key URL when known
- Running `steam-pp-cli doctor` with STEAM_API_KEY shows: `Auth: configured (env: STEAM_API_KEY)`
- For an API where key_url is NOT discovered: error messages show env var name but no URL line

## Scope Boundaries

- No OAuth flow improvements — only API key and bearer token auth
- No per-endpoint auth metadata in the spec — the spec declares API-wide auth, not per-endpoint
- No interactive auth setup wizard — just better error messages and doctor output
- No changes to the crowd-sniff auth detection logic (already improved separately)
- Key URL discovery is best-effort from SDK READMEs — when not found, omit it cleanly

## Key Decisions

- **Per-call, not per-startup**: Mixed-auth APIs (like Steam) have endpoints that work without auth. A startup gate would block these unnecessarily. The error handler catches failures per-call and shows auth guidance only when relevant.
- **Pattern-match 400 bodies for auth keywords**: Some APIs return 400 instead of 401 for missing auth. Body pattern matching (`key`, `token`, `auth`, `unauthorized`) is more reliable than just status codes. Combined with "auth not configured" state for high confidence.
- **Template-time env var embedding**: The env var name is known at generation time from the spec. Embed it directly in the error message template, not at runtime. This is zero-cost at runtime and always correct.
- **Best-effort key URL, omit when unknown**: Crowd-sniff can extract key registration URLs from ~60% of npm SDKs with popular APIs. When found, it's included in error messages and doctor output. When not found, those lines are simply absent — no "URL unknown" placeholder. The skill's Phase 1 research can also find and manually add the URL to the spec, covering the remaining cases.

## Dependencies / Assumptions

- The spec's `auth.env_vars` field is populated (either from crowd-sniff detection or manual spec authoring)
- The helpers.go.tmpl and doctor.go.tmpl templates receive the full `*spec.APISpec` (or a struct embedding it) — need to verify helpers currently only gets `*spec.APISpec` vs the enriched `helpersTemplateData`

## Outstanding Questions

### Deferred to Planning

- [Affects R1][Technical] What specific regex/keyword list for 400 body matching avoids false positives? Need to check real 400 responses from multiple APIs.
- [Affects R9][Technical] Does helpers.go.tmpl currently receive `*spec.APISpec` directly or through `helpersTemplateData`? If the latter, `Auth.EnvVars` might not be accessible. Check generator.go.
- [Affects R12][Technical] What regex patterns reliably extract key registration URLs from npm READMEs without false positives? Need to sample 5-10 real SDK READMEs.

## Next Steps

→ `/ce:plan` for structured implementation planning
