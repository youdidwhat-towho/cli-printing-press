---
title: Multi-Source API Discovery Design Patterns
date: 2026-03-30
category: best-practices
module: crowd-sniff API discovery
problem_type: best_practice
component: tooling
severity: medium
applies_when:
  - Aggregating API endpoints from heterogeneous sources (npm, GitHub, etc.)
  - Implementing resilient multi-source data collection with different auth/rate limits
  - Designing testable HTTP client patterns for external API integration
  - Extracting structured data from downloaded archives
tags:
  - api-discovery
  - multi-source-aggregation
  - error-resilience
  - http-client-testing
  - security-hardening
  - path-normalization
---

# Multi-Source API Discovery Design Patterns

## Context

Building the `crowd-sniff` command required aggregating API endpoint data from multiple external sources (npm registry, GitHub code search) that have fundamentally different authentication models, rate limits, data formats, and reliability characteristics. No existing codebase pattern covered multi-source external API integration with parallel execution, graceful degradation, and cross-source deduplication. The brainstorm-plan-implement-review pipeline surfaced six reusable patterns.

## Guidance

### 1. Testable HTTP Clients via Injectable Base URLs

Sources accept a configurable `BaseURL` in their options struct. Tests use `httptest.NewServer` and inject its URL. This avoids the anti-pattern in `research.go` where `httptest.NewServer` was created but never used because URLs were hardcoded.

For APIs on multiple hosts (npm uses `registry.npmjs.org` for search and `api.npmjs.org` for downloads), use separate configurable URLs per host:

```go
type NPMOptions struct {
    RegistryBaseURL  string // defaults to "https://registry.npmjs.org"
    DownloadsBaseURL string // defaults to "https://api.npmjs.org"
    HTTPClient       *http.Client
}
```

### 2. errgroup.Group (not WithContext) for Independent Sources

When the requirement says "each source is optional and independent," `errgroup.WithContext` is wrong. A 401 from GitHub would cancel the npm source via shared context cancellation. Use plain `errgroup.Group` for synchronization only.

Sources never return errors to the group. They log warnings to stderr and return empty results:

```go
g := new(errgroup.Group)
g.Go(func() error {
    result, err := npmSource.Discover(ctx, apiName)
    if err != nil {
        fmt.Fprintf(os.Stderr, "warning: npm source: %v\n", err)
        return nil // never propagate
    }
    npmResult = result
    return nil
})
```

### 3. Two-Step Path Normalization

SDK code uses inconsistent parameter syntax. Normalization requires two steps:

- **Step 1 — Unify syntax**: `:id`, `{user_id}`, `<id>`, `$id` all become `{id}`
- **Step 2 — Replace concrete values**: UUIDs, numeric IDs, hashes become `{uuid}`, `{id}`, `{hash}`

websniff only does step 2 (it sees real traffic URLs with concrete values). crowd-sniff needs both because SDK source code uses various parameter syntaxes. Without step 1, `/users/:id` from npm and `/users/{user_id}` from GitHub fail to deduplicate.

### 4. Tarball Extraction Security Checklist

When downloading and extracting archives from external registries:

1. **HTTPS-only URLs** — reject http:// and file:// schemes before downloading
2. **io.LimitReader at 10MB** — not Content-Length (unreliable with chunked encoding)
3. **Reject symlinks** — skip `tar.TypeSymlink` and `tar.TypeLink` entries (prevents reading `/etc/passwd` via malicious symlink)
4. **Path containment** — validate every extracted path resolves within the temp directory root
5. **Deferred cleanup** — `defer os.RemoveAll(tmpDir)` ensures temp dirs are always removed

### 5. SourceResult Wrapper for Multi-Signal Returns

When sources produce endpoints AND other signals (base URL candidates), wrap them in a result struct:

```go
type SourceResult struct {
    Endpoints         []DiscoveredEndpoint
    BaseURLCandidates []string
}
```

This was caught during the plan's architecture review — the original interface only returned endpoints, leaving base URL discovery as an architectural gap the CLI command would have to work around.

### 6. Document Review Catches Real Bugs

The `ce:review` correctness reviewer found a cartesian product bug where `extractMethodCallEndpoints` paired every HTTP method with every URL path on the same line (cross-product of N methods x M paths). The fix: use `FindAllStringSubmatchIndex` for positional matching — find the first path after each method call's position, not all paths globally.

This confirms that dedicated correctness review with structured reviewer personas catches category errors that escape typical code review.

## Why This Matters

These patterns compound. The next external API integration (Postman source when that CLI ships, or any future multi-source feature) can follow these patterns directly. The testable HTTP client pattern alone would have saved debugging time on the existing `research.go` code if it had been established first.

The security checklist for tarball extraction is especially important — npm packages are user-uploaded content. Every archive extraction is an attack surface.

## When to Apply

- Any feature querying multiple external APIs in parallel
- Any code downloading and extracting archives from untrusted sources
- Any aggregation pipeline deduplicating results from heterogeneous sources
- Any HTTP client code that needs test coverage without hitting real APIs

## Examples

### Before (research.go anti-pattern)

```go
// httptest.NewServer created in test but never used because URLs are hardcoded
func fetchReadme(apiName string) (string, error) {
    resp, err := http.Get("https://api.github.com/repos/...")
    // ...
}
```

### After (crowd-sniff pattern)

```go
// BaseURL configurable; fully testable with httptest.NewServer
source := NewNPMSource(NPMOptions{RegistryBaseURL: server.URL})
result, err := source.Discover(ctx, "notion")
```

### Before (errgroup.WithContext — wrong for independent sources)

```go
g, ctx := errgroup.WithContext(cmd.Context())
g.Go(func() error {
    // GitHub 401 cancels ctx, killing npm mid-flight
    return githubSource.Discover(ctx, name)
})
```

### After (errgroup.Group — sources are independent)

```go
g := new(errgroup.Group)
g.Go(func() error {
    result, err := githubSource.Discover(ctx, name)
    if err != nil {
        fmt.Fprintf(os.Stderr, "warning: %v\n", err)
        return nil // never cancel other sources
    }
    githubResult = result
    return nil
})
```

## Related

- `docs/solutions/security-issues/filepath-join-traversal-with-user-input-2026-03-29.md` — path traversal protection pattern (applied in tarball extraction)
- `docs/solutions/best-practices/validation-must-not-mutate-source-directory-2026-03-29.md` — temp dir cleanup pattern (applied in npm source)
- `docs/solutions/best-practices/adaptive-rate-limiting-sniffed-apis.md` — rate limiting for generated CLIs (related: GitHub code search uses 6s ticker for 10 req/min)
- `docs/solutions/best-practices/checkout-scoped-printing-press-output-layout-2026-03-28.md` — output path conventions (crowd-sniff defaults to `~/.cache/printing-press/crowd-sniff/`)
- `docs/brainstorms/2026-03-29-crowd-sniff-requirements.md` — origin requirements document
- `docs/plans/2026-03-29-003-feat-crowd-sniff-plan.md` — implementation plan
