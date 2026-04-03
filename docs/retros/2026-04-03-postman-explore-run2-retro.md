# Printing Press Retro: Postman Explore (Run 2)

## Session Stats
- API: postman-explore
- Spec source: catalog (community/sniffed, from previous run)
- Scorecard: 90/100 Grade A
- Verify pass rate: 71%
- Fix loops: 1 (no improvement — failures are positional-arg false negatives)
- Manual code edits: 6
- Features built from scratch: 8

## Findings

### 1. serviceForPath() returns API slug instead of actual service name (Bug)

- **What happened:** Generator emitted `return "postman-explore"` as the default service name. The proxy-envelope API uses two services: "publishing" for browse endpoints and "search" for search endpoints. Had to manually rewrite to route by path prefix.
- **Root cause:** Generator template (`client.go.tmpl`) uses `.ProxyRoutes` for path-prefix-to-service mapping, but `.ProxyRoutes` was empty. The profiler/parser didn't detect separate services because the spec has a single server URL and no `x-proxy-routes` annotation.
- **Cross-API check:** Any proxy-envelope API with multiple backend services. The Postman Explore API has this pattern (publishing + search). Other APIs with gateway/proxy patterns would too.
- **Frequency:** subclass: proxy-envelope APIs with multiple services
- **Fallback if machine doesn't fix it:** Claude must manually detect from spec structure or live probing which paths map to which services. Reliability: sometimes — Claude might miss the split, especially if the spec doesn't annotate services.
- **Worth a machine fix?** Yes. The catalog entry already has `ClientPattern: "proxy-envelope"` and `Notes` describing the proxy. The catalog could include a `ProxyRoutes` field mapping path prefixes to service names.
- **Inherent or fixable:** Fixable. The catalog YAML could carry `proxy_routes: {"/search-all": "search", "/": "publishing"}` and the generator could consume it.
- **Durable fix:** Add `proxy_routes` field to catalog schema. When present, populate `.ProxyRoutes` in the generator context. The template already handles `.ProxyRoutes` — it just needs data.
  - Condition: spec has `proxy_routes` in catalog entry
  - Guard: Standard REST APIs without proxy pattern — no change
  - Frequency estimate: ~5% of APIs use proxy-envelope
- **Test:** Generate from postman-explore catalog entry → verify `serviceForPath` routes "/search-all" to "search" and "/" to "publishing". Negative: generate from Stripe → verify single service name.
- **Evidence:** Client returned `invalidServiceError` for service "postman-explore"; had to probe "publishing" and "search" manually.

### 2. defaultSyncResources() derived wrong resource list (Bug)

- **What happened:** Generator produced `defaultSyncResources()` returning `["api"]` — a single entry mapped to `/v1/api/team`. The API has 6 entity types to sync (collection, workspace, api, flow, team, category). Had to manually rewrite to list all 6 with correct API paths.
- **Root cause:** Profiler's resource name extraction from paths. Both `/v1/api/networkentity` and `/v1/api/team` share the `/v1/api/` prefix. The profiler likely extracted "api" as the resource name for both, causing a collision where only one survived. The `/v1/api/networkentity` endpoint is parameterized by `entityType` query param — the profiler doesn't recognize that one endpoint represents 4 resources.
- **Cross-API check:** Any API where one endpoint serves multiple entity types via a query parameter (like `entityType=collection|workspace`). Also APIs where multiple endpoints share a parent path segment.
- **Frequency:** subclass: APIs with multi-entity list endpoints (enum-parameterized)
- **Fallback if machine doesn't fix it:** Claude must read the spec, identify the entityType enum, and manually expand to separate sync resources. Reliability: sometimes — Claude might not realize the enum represents separate resources.
- **Worth a machine fix?** Yes, but complex. The profiler would need to detect enum params on list endpoints and expand each enum value into a separate sync resource.
- **Inherent or fixable:** Fixable with design work. The profiler could detect enum query params on list endpoints and expand: `/v1/api/networkentity?entityType=collection` → resource "collection", etc.
- **Durable fix:** In the profiler's syncable resource detection: when a GET list endpoint has a required enum query parameter, expand each enum value into a separate sync resource. Include the enum value in the resource path.
  - Condition: List endpoint has required enum param
  - Guard: Endpoints without enum params — unchanged
  - Frequency estimate: ~10% of APIs have enum-parameterized list endpoints
- **Test:** Parse postman-explore spec → profiler produces 5 syncable resources (collection, workspace, api, flow, team) not 1. Negative: parse Stripe → unchanged (no enum-parameterized list endpoints).
- **Evidence:** `defaultSyncResources()` returned `["api"]`, mapped to `/v1/api/team`.

### 3. determinePaginationDefaults() hardcoded to cursor pagination (Assumption mismatch)

- **What happened:** Generator hardcoded `{cursorParam: "after", limitParam: "limit", limit: 100}`. The Postman Explore API uses offset-based pagination (`offset` param). Had to change `cursorParam` from "after" to "offset".
- **Scorer correct?** N/A — not surfaced by a score penalty.
- **Root cause:** The template (`sync.go.tmpl`) hardcodes defaults instead of using the profiler's detected pagination params. The profiler DOES detect the correct params via `mostCommon()` analysis, but the template doesn't consume `p.Pagination.CursorParam`.
- **Cross-API check:** Offset pagination is extremely common — ~40% of REST APIs use offset/limit instead of cursor/after.
- **Frequency:** most APIs (40%+ use offset pagination)
- **Fallback if machine doesn't fix it:** Claude manually edits one line. Reliability: usually — but it's easy to miss, and the wrong pagination param means sync never advances past page 1.
- **Worth a machine fix?** Absolutely. The profiler already detects the correct param. The template just needs to use it.
- **Inherent or fixable:** Fixable. Trivial template change.
- **Durable fix:** In `sync.go.tmpl`, replace hardcoded `cursorParam: "after"` with `cursorParam: "{{.Pagination.CursorParam}}"`. The profiler already populates `Pagination.CursorParam` with the detected value.
- **Test:** Generate from postman-explore spec (has `offset` param) → verify `determinePaginationDefaults()` returns `offset`. Generate from GitHub spec (has `after` param) → verify returns `after`.
- **Evidence:** Sync used `after=` which the API ignored; entities never paginated.

### 4. Dogfood reports false "unregistered commands" for subcommands (Scorer bug)

- **What happened:** Dogfood reported 15 "unregistered commands" including `apigetcategory`, `apilistcategories`, etc. All of these ARE registered as subcommands (e.g., `api get-category`).
- **Scorer correct?** No — scorer bug. The detection logic at `dogfood.go:966-1029` scans for `func new*Cmd()` patterns, extracts the CamelCase suffix (e.g., `ApiGetCategory`), lowercases it to `apigetcategory`, then searches for that string in the `--help` output. The help output shows `get-category` (kebab-case), which does NOT contain `apigetcategory`. The matching fails because it doesn't convert CamelCase to kebab-case.
- **Root cause:** `dogfood.go:982` — `cmdName := strings.ToLower(match[1])` converts `ApiGetCategory` to `apigetcategory` and then checks `strings.Contains(helpLower, cmdName)` at line 1023. The help output contains `get-category` under `api` subcommand, not `apigetcategory`.
- **Cross-API check:** Every CLI with subcommands (api get-X, auth login, etc.) — this is most CLIs.
- **Frequency:** every API
- **Fallback if machine doesn't fix it:** N/A — this is a scorer fix, not a generator fix.
- **Worth a machine fix?** Yes — fix the scorer.
- **Durable fix:** In `dogfood.go`, convert the CamelCase function name to kebab-case before matching against help output. E.g., `ApiGetCategory` → `api-get-category` or split on uppercase boundaries and search for each component. Alternatively, search for each word separately: `api`, `get`, `category` all appear in the help output.
- **Test:** Run dogfood on a CLI with subcommands (api get-X) → no false unregistered commands.
- **Evidence:** Dogfood reported `apigetcategory, apigetnetworkentitycounts, apilistcategories, apilistnetworkentities, apilistteams` etc. as unregistered.

### 5. Verify fails on commands requiring positional args with enum values (Scorer bug)

- **What happened:** Verify failed 5 commands (browse, open, similar, stale, trending) with `exec_fail`. These commands require positional args. Verify's `inferPositionalArgs()` correctly detected the `<type>` placeholder and mapped it to `"mock-value"` via `syntheticArgValue()`. But `"mock-value"` is rejected by browse's type validation (expects collections/workspaces/apis/flows).
- **Scorer correct?** Partially. The arg inference works — it correctly finds `<type>` and provides a value. But the default fallback `"mock-value"` is useless for enum-constrained args. For `stale` and `open`, the issue is different — they need a local SQLite DB that doesn't exist during verify.
- **Root cause:** `runtime.go:441-458` — `syntheticArgValue()` has specific mappings for `query`, `name`, `id`, `region` etc. but no mapping for `type`. The `default` case returns `"mock-value"`.
- **Cross-API check:** Any CLI with enum-constrained positional args. Common patterns: `browse <type>`, `list <resource>`, `show <format>`.
- **Frequency:** most APIs — generated CLIs frequently have commands like `browse <type>` or `list <resource>`
- **Fallback if machine doesn't fix it:** N/A — scorer fix.
- **Worth a machine fix?** Yes — fix the scorer.
- **Durable fix:** Two approaches:
  1. Add common placeholder names to `syntheticArgValue()`: `"type" → "collection"`, `"resource" → "items"`, `"format" → "json"`
  2. Better: parse the command's help output for valid values (many help texts list valid args in the description) and pick the first one
- **Test:** Run verify on a CLI with `browse <type>` → verify uses a valid type value. No false exec_fail.
- **Evidence:** Verify ran `browse mock-value` which failed with "unknown type".

### 6. Generator emits spec enum values without live validation (Discovered optimization)

- **What happened:** The spec listed sort values as `[popular, recent, featured, new, week, alltime]`. The live API only accepts `popular` and `featured` — the other 4 return `invalidParamsError`. Had to test each value and fix the command flags.
- **Root cause:** The spec was reverse-engineered from a previous sniff run and included values observed in the website's JavaScript code, but the API doesn't actually accept all of them server-side.
- **Cross-API check:** Any sniffed spec — client-side code may reference enum values the server doesn't support.
- **Frequency:** subclass: sniffed specs
- **Fallback if machine doesn't fix it:** Claude tests each enum value during verify/live-smoke and fixes invalid ones. Reliability: usually catches it if live testing runs.
- **Inherent or fixable:** Partially fixable. The verify tool could probe enum values against the live API (when available) and flag invalid ones. But this requires API access during verification.
- **Durable fix:** In verify's `--fix` mode: for commands with enum flags, try each enum value and mark invalid ones. Remove invalid values from the flag description.
- **Test:** Verify against postman-explore with live API → detect and report invalid sort values.
- **Evidence:** `browse collections --sort week` returned HTTP 400 `invalidParamsError`.

### 7. No user-friendly wrapper commands generated for nested API commands (Template gap)

- **What happened:** Generator produced `api list-network-entities` and `search-all search_all` — functional but terrible UX. Had to write 8 user-friendly wrapper commands from scratch (search, browse, categories, stats, open, trending, stale, similar).
- **Root cause:** Generator emits commands directly from operationIds. For APIs with deeply nested or technical command names, this produces unusable CLIs. There's no mechanism to generate "user-friendly alias" commands that wrap the raw API commands.
- **Cross-API check:** Every API — the operationId-derived names are always developer-oriented, not user-oriented.
- **Frequency:** every API
- **Fallback if machine doesn't fix it:** Claude builds wrapper commands during Phase 3 every time. Reliability: always catches it (it's in the absorb manifest), but it's significant work.
- **Worth a machine fix?** Yes, but complex. The generator would need to identify "primary user workflows" from the spec and emit top-level convenience commands.
- **Inherent or fixable:** Partially fixable. A post-generation step could identify the most common operations (list, search, get) and emit user-friendly top-level commands. But naming them well requires product judgment that's hard to automate.
- **Durable fix:** Two levels:
  1. **Near-term:** Emit promoted/alias commands from Phase 1.5 absorb manifest features. The skill already creates this manifest — the generator could consume it.
  2. **Long-term:** Auto-detect primary entity types and emit standard commands (browse, search, inspect, open) as top-level aliases.
- **Test:** Generate from any spec → verify top-level commands include user-friendly names, not just operationId-derived ones.
- **Evidence:** Built 8 commands by hand in Phase 3.

### 8. Store migration doesn't create entity-specific tables for sniffed specs (Missing scaffolding)

- **What happened:** Generator created generic `resources` + `resources_fts` tables. Had to manually add 5 tables: entities (with extracted metric columns), categories, teams, metric_snapshots, watchlist.
- **Root cause:** Schema builder (`schema_builder.go`) scores data gravity from spec schemas. The Postman Explore spec has relatively simple schemas (no foreign keys, few temporal fields), scoring below the threshold for entity-specific table generation. Also, the schema builder doesn't extract metric arrays into individual columns.
- **Cross-API check:** APIs with nested metric arrays or complex response structures that benefit from column extraction.
- **Frequency:** subclass: APIs with metric/analytics data in array format
- **Worth a machine fix?** Partially. The generic `resources` table works for most APIs. Entity-specific tables with extracted columns are a transcendence feature that requires domain knowledge (knowing which metrics matter). Hard to generalize.
- **Inherent or fixable:** Partially fixable. The schema builder could detect arrays of `{name, value}` objects (metric patterns) and extract them into columns. But the watchlist and metric_snapshots tables are novel features that the generator shouldn't try to anticipate.
- **Durable fix:** In `schema_builder.go`: when a schema contains an array of objects with `metricName`/`metricValue` pattern, create extracted columns for each enum value.
  - Condition: Schema has metric-array pattern
  - Guard: APIs without metrics — unchanged
- **Test:** Parse postman-explore spec → store migration includes metric columns. Parse Stripe → unchanged.
- **Evidence:** Manually added entities table with fork_count, view_count, etc.

## Prioritized Improvements

### Fix the Scorer (highest priority)

| # | Scorer | Bug | Impact | Fix target |
|---|--------|-----|--------|------------|
| 4 | Dogfood | CamelCase→lowercase matching misses kebab-case subcommands | 15 false unregistered commands every run | `internal/pipeline/dogfood.go:982` — add CamelCase→kebab conversion |
| 5 | Verify | `syntheticArgValue()` default `"mock-value"` fails enum-constrained args | 5 false exec_fail per run | `internal/pipeline/runtime.go:441` — add "type"→"collection" mapping |

### Do Now

| # | Fix | Component | Frequency | Fallback Reliability | Complexity | Guards |
|---|-----|-----------|-----------|---------------------|------------|--------|
| 3 | Use profiler's detected pagination params in sync template | `sync.go.tmpl` | most APIs | usually | small | None needed — profiler already detects |
| 1 | Add proxy_routes to catalog schema | `catalog/`, `generator/` | proxy-envelope | sometimes | small | Only when catalog has proxy_routes |

### Do Next (needs design)

| # | Fix | Component | Frequency | Fallback Reliability | Complexity | Guards |
|---|-----|-----------|-----------|---------------------|------------|--------|
| 2 | Expand enum-parameterized list endpoints to multiple sync resources | profiler | 10% APIs | sometimes | medium | Only for required enum params on GET list |
| 7 | Emit user-friendly wrapper commands from absorb manifest | generator | every API | always (Claude builds) | large | Needs product judgment heuristics |

### Skip

| # | Fix | Why unlikely to recur |
|---|-----|----------------------|
| 6 | Validate spec enum values against live API | Requires API access during verify; most specs have correct enums. Sniffed specs are the exception, and verify --fix already catches runtime failures. |
| 8 | Extract metric arrays into store columns | Metric-array pattern is uncommon across APIs. The generic resources table is sufficient for most cases. |

## Work Units

### WU-1: Fix scorer false positives (findings #4, #5)

- **Goal:** Eliminate false dogfood "unregistered commands" and false verify exec_fail for enum-constrained args
- **Target files:**
  - `internal/pipeline/dogfood.go` (line 982 — CamelCase→kebab matching)
  - `internal/pipeline/runtime.go` (line 441 — syntheticArgValue additions)
- **Acceptance criteria:**
  - Run dogfood on postman-explore-pp-cli → 0 unregistered commands
  - Run verify on postman-explore-pp-cli → browse/trending pass exec test
  - Run dogfood on steam-web-pp-cli → no regression (subcommands still detected)
- **Scope boundary:** Don't change verify's exec logic — only fix name matching and arg synthesis
- **Complexity:** small (2 files, straightforward string conversion)

### WU-2: Wire profiler pagination params into sync template (finding #3)

- **Goal:** Generated sync uses the profiler-detected pagination param (offset vs cursor) instead of hardcoded "after"
- **Target files:**
  - `internal/generator/templates/sync.go.tmpl` (determinePaginationDefaults function)
  - `internal/generator/generator.go` (template data struct — verify Pagination is passed)
- **Acceptance criteria:**
  - Generate from postman-explore spec (offset pagination) → `determinePaginationDefaults()` returns `{cursorParam: "offset"}`
  - Generate from GitHub spec (cursor pagination) → returns `{cursorParam: "after"}`
- **Scope boundary:** Don't change the profiler's detection logic — it already works correctly
- **Complexity:** small (1 template change, verify data plumbing)

### WU-3: Add proxy_routes to catalog schema (finding #1)

- **Goal:** Proxy-envelope APIs can declare service-to-path mappings in their catalog entry, and the generator uses them
- **Target files:**
  - `internal/catalog/catalog.go` (add ProxyRoutes field)
  - `catalog/postman-explore.yaml` (add proxy_routes)
  - `internal/generator/generator.go` (populate .ProxyRoutes from catalog)
  - `internal/generator/templates/client.go.tmpl` (verify .ProxyRoutes is consumed)
- **Acceptance criteria:**
  - postman-explore catalog entry has `proxy_routes: {"/search-all": "search", "/": "publishing"}`
  - Generated client routes /search-all to "search" and / to "publishing"
  - APIs without proxy_routes — unchanged
- **Scope boundary:** Don't auto-detect proxy routes from specs — require explicit catalog declaration
- **Complexity:** small (4 files, straightforward field addition)

## Anti-patterns

- **Trusting spec enum values blindly** — Sniffed specs may include client-side enum values the server rejects. Always test enum values against the live API when possible.
- **Assuming cursor pagination as default** — Offset pagination is equally common. Default to the detected value, not a hardcoded one.

## What the Machine Got Right

- **Proxy-envelope client pattern** — The `--client-pattern proxy-envelope` flag correctly generated the proxy envelope wrapping (POST with service/method/path body). Only the service routing was wrong.
- **Quality gates** — All 7 gates passed on first generation. The generator produces compilable, runnable code reliably.
- **Data layer scaffolding** — Generic resources table + FTS5 + sync infrastructure work out of the box. Entity-specific tables were added for transcendence but the foundation was solid.
- **Agent-native defaults** — --json, --compact, --agent, --dry-run, --select all work correctly with zero manual setup. Agent Native scored 10/10.
- **Adaptive rate limiter** — The proactive rate limiter with 429 backoff worked perfectly against the Postman proxy API.
- **Response cache** — 5-minute GET cache prevented duplicate API calls during development.
