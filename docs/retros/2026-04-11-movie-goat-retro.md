# Printing Press Retro: movie-goat

## Session Stats
- API: TMDb + OMDb (multi-source movie CLI)
- Spec source: Internal YAML (hand-written for TMDb API v3)
- Scorecard: 87/100 (Grade A, after polish)
- Verify pass rate: 100% (after polish)
- Fix loops: 2 (shipcheck + polish)
- Manual code edits: 12+
- Features built from scratch: 7 transcendence commands + OMDb client + write-through caching + sync ceiling

## Findings

### F1. Positional query params treated as path params (bug)
- **What happened:** Search endpoints like `/search/movie?query=Inception` had `query` defined as positional+required in the spec. The generator emitted `replacePathParam(path, "query", args[0])` which tries to substitute `{query}` in the URL path — but `/search/movie` has no `{query}` path segment. The query was silently dropped, returning 0 results.
- **Scorer correct?** N/A — scorer doesn't check this. Live testing caught it.
- **Root cause:** `command_endpoint.go.tmpl` line 77 — ALL positional params go through `replacePathParam()`. The template doesn't distinguish between path-segment positional params (like `{movieId}` in `/movie/{movieId}`) and query-string positional params (like `query` in `/search/movie?query=...`). The decision is made purely by `.Positional=true`, not by whether the param name appears in the path template.
- **Cross-API check:** Every API with search endpoints has this pattern. TMDb, Stripe (`/search`), Algolia, Elasticsearch — any API where the primary search term is positional but sent as a query param.
- **Frequency:** Most APIs with search endpoints.
- **Fallback:** Claude catches this when the first search returns empty results, but only after a live test. If testing is skipped, the CLI ships broken.
- **Worth a Printing Press fix?** Yes — this is a common pattern that fails silently.
- **Inherent or fixable:** Fixable. The template should check whether the param name appears as `{paramName}` in the endpoint path before calling `replacePathParam`. If the param is positional but NOT in the path template, add it to the query params map instead.
- **Durable fix:** In `command_endpoint.go.tmpl`, before emitting `replacePathParam` for a positional param, check `strings.Contains(path, "{"+paramName+"}")`. If false, emit `params[paramName] = args[N]` instead. This is a template-level fix — no spec changes needed.
- **Test:** Positive: search endpoint with positional `query` param → query goes as `?query=...`. Negative: path endpoint with `{movieId}` positional → still uses `replacePathParam`.
- **Evidence:** `movies search "Inception"` returned 0 results until manually fixed. Fixed in 4 files (movies_search, tv_search, people_search, search_multi).

### F2. Required-flag check emitted despite default value (bug)
- **What happened:** Trending command had `time-window` with `required: true` and `default: "day"`. The generator emitted `if !cmd.Flags().Changed("time-window") { return error }` even though the flag has a perfectly good default. Running `movie-goat-pp-cli trending` without `--time-window` errored, despite the flag already being "day".
- **Scorer correct?** N/A — not a scoring issue, a usability bug.
- **Root cause:** `command_endpoint.go.tmpl` lines 46-51. The required check tests `{{if and .Required (not .Positional)}}` but does NOT check `.Default`. Any required non-positional param gets the guard, even with a default.
- **Cross-API check:** Any API with enum params that have sensible defaults. TMDb trending (`day`/`week`), Stripe API version headers, pagination params with defaults.
- **Frequency:** Most APIs — enum params with defaults are extremely common.
- **Fallback:** Claude or the user notices the error on first run and removes the check. Simple fix but annoying.
- **Worth a Printing Press fix?** Yes — one-line template change eliminates a recurring manual fix.
- **Inherent or fixable:** Fixable. Add `(not .Default)` to the condition.
- **Durable fix:** Change template line to `{{if and .Required (not .Positional) (not .Default)}}`. Skip the required check when a default exists.
- **Test:** Positive: param with `required: true, default: "day"` → no required check emitted. Negative: param with `required: true, default: nil` → required check still emitted.
- **Evidence:** `trending` command required `--time-window` despite default. Fixed in 4 files (promoted_trending, trending_movies, trending_tv, trending_people).

### F3. SQLite column names with dots are invalid SQL (bug)
- **What happened:** The spec had params named `primary_release_date.gte` and `vote_average.gte` (TMDb's dot-notation filter syntax). The schema builder preserved the dots in column names, producing `CREATE TABLE discover (..., primary_release_date.gte TEXT, ...)` which is invalid SQL. The CLI crashed on first SQLite access.
- **Scorer correct?** N/A — runtime crash, not a scoring issue.
- **Root cause:** `schema_builder.go` `toSnakeCase()` function (lines 361-377) handles hyphens and camelCase but does NOT handle dots. `safeSQLName()` (lines 336-343) only quotes SQL reserved words, not identifiers with special characters.
- **Cross-API check:** Any API with dot-notation params. TMDb (`vote_average.gte`), Elasticsearch (`field.nested`), GraphQL-derived specs, any API that uses dots for namespacing.
- **Frequency:** API subclass: APIs with dot-notation filter params. Moderate frequency.
- **Fallback:** Immediate crash on database access — Claude catches it, but only after testing.
- **Worth a Printing Press fix?** Yes — silent crash from a naming convention is unacceptable.
- **Inherent or fixable:** Fixable. Replace dots with underscores in `toSnakeCase()` or quote all identifiers in `safeSQLName()`.
- **Durable fix:** In `toSnakeCase()`, add `s = strings.ReplaceAll(s, ".", "_")` before the existing transformations. This converts `primary_release_date.gte` → `primary_release_date_gte`. Alternative: wrap all column names in double-quotes in the SQL template, but replacing dots is cleaner.
- **Test:** Positive: param `vote_average.gte` → column `vote_average_gte`. Negative: param `movie_id` (no dots) → unchanged.
- **Evidence:** `Error: opening local database: migration failed: SQL logic error: near ".": syntax error`

### F4. No write-through caching — local store only grows via explicit sync (template gap)
- **What happened:** The `resolveRead` function in `data_source.go.tmpl` fetches from the live API and returns the result — but never writes it to the local SQLite store. The FTS search index only contains data from explicit `sync` runs. For a large-catalog API like TMDb (900K+ movies), running `sync` to populate the store is impractical. The user expected that browsing the CLI would naturally build up a searchable local corpus.
- **Scorer correct?** N/A — not scored. This is a UX gap.
- **Root cause:** `data_source.go.tmpl` lines 86-91 and 93-97 — the "live" and "auto" cases return data immediately without touching the store. The store is write-only from `sync.go`.
- **Cross-API check:** This matters most for large-catalog APIs (TMDb, Spotify, Steam, npm) where sync-everything is infeasible. For small, bounded APIs (Linear, Cal.com), sync-everything works fine and write-through adds complexity without clear benefit.
- **Frequency:** API subclass: large-catalog APIs where full sync is impractical. Growing as the Printing Press takes on more public catalog APIs.
- **Fallback:** Claude builds it manually each time, but it's non-trivial: needs response envelope detection, ID extraction, SQLite upsert, and handling of both array and single-object responses. Took ~4 iterations in this session to get right (goroutine timing, envelope parsing, FTS indexing).
- **Worth a Printing Press fix?** Yes, but conditionally. Write-through should be opt-in or auto-detected based on catalog size. For bounded APIs, it's unnecessary overhead. For large catalogs, it's essential UX.
- **Inherent or fixable:** Fixable. The template already has the store import and `resolveRead` knows the resource type. Adding `store.Upsert()` after a successful live read is ~20 lines. The nuance is response envelope detection (TMDb wraps in `{"results": [...]}`), which the sync code already handles via `extractPageItems`.
- **Durable fix:** Add a `writeThroughCache(resourceType, data)` function to `data_source.go.tmpl` that: (1) opens the store, (2) tries to extract items from common envelope patterns (`results`, `data`, `items`, or raw array), (3) upserts each item with its `id` field. Call it from `resolveRead` after successful live reads. Gate it behind a template flag (e.g., `WriteThrough bool` on the generator config) so bounded APIs can skip it.
- **Test:** Positive: `movies search "Inception"` → results appear in `search --data-source local`. Negative: bounded API (Linear) → write-through disabled, sync is the only path.
- **Evidence:** Required 4 iterations: async goroutine (process exited too early), envelope parsing (TMDb wraps in `results`), single-object handling, and search command FTS branch fix. All manual.

### F5. Sync has no page ceiling — unbounded APIs can sync forever (template gap)
- **What happened:** The `sync` command paginates through all available pages with no limit. For TMDb's `/movie/popular` (500+ pages, 10K+ movies), a `sync --full` would make thousands of API calls, take forever, and fill the local store with data the user doesn't need. We manually added `--max-pages 5` (default) to cap it.
- **Scorer correct?** N/A — not scored.
- **Root cause:** `sync.go.tmpl` has no concept of a page ceiling. The pagination loop runs until `hasMore=false` or `nextCursor=""`. This works for bounded APIs (fetch all my Linear issues) but is dangerous for public catalog APIs.
- **Cross-API check:** Same pattern as F4 — large-catalog APIs. TMDb popular/trending, npm search, Spotify catalog, Steam game listings.
- **Frequency:** API subclass: large-catalog public APIs. Same subclass as F4.
- **Fallback:** Claude or the user notices the sync running forever and kills it. Then manually adds a page limit flag. Easy fix but should be a default.
- **Worth a Printing Press fix?** Yes. A sensible default ceiling (e.g., 10 pages) with `--max-pages` override is trivial to add and prevents runaway syncs.
- **Inherent or fixable:** Fixable. Add `--max-pages` flag to `sync.go.tmpl` with a default of 10. Check `pagesFetched >= maxPages` in the pagination loop.
- **Durable fix:** In `sync.go.tmpl`: add `maxPages int` variable, `--max-pages` flag with default 10, page counter in the loop, break when ceiling is reached. Log "reached --max-pages limit (N pages, M items)" so the user knows it was capped, not empty.
- **Test:** Positive: `sync --max-pages 3` → syncs exactly 3 pages. `sync --max-pages 0` → unlimited. Negative: bounded API sync → still completes normally (ceiling not reached).
- **Evidence:** User said "Storing 11M movies sounds like a failure waiting to happen" and we added `--max-pages 5` manually.

### F6. Search command hardcodes one search endpoint (template gap)
- **What happened:** The generated `search` command (the promoted, user-facing one) hardcoded `GET /search/tv` as the live search endpoint instead of using the multi-search endpoint `/search/multi`. This meant the top-level `search` command only found TV shows, not movies or people.
- **Scorer correct?** N/A — not scored.
- **Root cause:** The generator picks a search endpoint for the promoted `search` command, but the heuristic picked `/search/tv` (the last search-type endpoint alphabetically?) instead of `/search/multi` which covers all types.
- **Cross-API check:** Any API with multiple search endpoints where one is a superset. TMDb (`/search/multi` > `/search/movie`), GitHub (`/search/repositories` vs `/search/code` vs general search).
- **Frequency:** API subclass: APIs with multiple search scopes.
- **Fallback:** Claude notices during testing and changes the path. Simple one-line fix.
- **Worth a Printing Press fix?** Medium. The generator should prefer multi/global search endpoints over resource-specific ones.
- **Durable fix:** When selecting the promoted search endpoint, prefer endpoints with "multi" or "all" in the path. If none exist, prefer the endpoint that covers the most resource types.
- **Test:** Positive: API with `/search/multi` and `/search/movie` → promoted search uses `/search/multi`.
- **Evidence:** `search "Inception" --data-source local` returned nothing because live search was hitting `/search/tv` which doesn't return movies.

### F7. Auth protocol scorer doesn't recognize query-param auth (scorer bug)
- **What happened:** Scorecard auth_protocol scored 3/10. The scorer checked whether the client uses `Bearer` prefix in the Authorization header. TMDb v3 sends the API key as `?api_key=<key>` query parameter — a valid and documented auth method that the scorer doesn't recognize.
- **Scorer correct?** No — scorer bug. Query-param API key auth is a legitimate auth pattern used by TMDb, OMDb, Google Maps, and many other APIs.
- **Root cause:** The scorecard's auth_protocol dimension likely grep-checks for `Authorization` header or `Bearer` prefix. Query-param auth (`?api_key=`) is invisible to this check.
- **Cross-API check:** TMDb, OMDb, Google Maps, YouTube Data API, many legacy REST APIs. Query-param auth is the second most common auth pattern after Bearer headers.
- **Frequency:** API subclass: APIs using query-param auth. Moderate frequency.
- **Fallback:** The score is cosmetic — the CLI works correctly. But it misleads users into thinking auth is broken.
- **Worth a Printing Press fix?** Yes — fix the scorer, not the Printing Press. The scorer should recognize query-param auth as valid.
- **Durable fix:** In the scorecard command, check for query-param auth patterns: `url.Values`, `q.Set("api_key"`, `q.Set("key"`, `params["apikey"]` in addition to `Authorization` header patterns.
- **Test:** Positive: CLI with `q.Set("api_key", ...)` → auth_protocol PASS. Negative: CLI with no auth at all → auth_protocol FAIL.
- **Evidence:** Scorecard auth_protocol 3/10 despite the CLI authenticating correctly against TMDb.

### F8. No multi-API enrichment scaffolding (missing scaffolding)
- **What happened:** The entire OMDb enrichment layer — client, config field, wiring into `movies get`, response merging — was built from scratch. The Printing Press has no concept of a secondary API that enriches the primary API's responses.
- **Scorer correct?** N/A.
- **Root cause:** The Printing Press assumes one API per CLI. The spec format has one `base_url`, one `auth` block. There's no way to declare "primary API: TMDb, enrichment API: OMDb" in the spec.
- **Cross-API check:** Multi-source CLIs are rare but growing: movie CLIs (TMDb + OMDb), travel CLIs (Google Flights + Kayak), shopping CLIs (price comparison). Also relevant: any CLI that cross-references with a public data source.
- **Frequency:** API subclass: multi-source CLIs. Low frequency currently, but the "movie-goat" pattern suggests this will become a differentiator.
- **Fallback:** Claude builds it by hand each time. The OMDb client was ~50 lines. The wiring was another ~30. Not terrible, but error-prone (auth, response merging, graceful degradation).
- **Worth a Printing Press fix?** P3 — interesting but low frequency. The complexity of supporting arbitrary secondary APIs in the spec format is high relative to the frequency.
- **Inherent or fixable:** Partially fixable. A lightweight approach: support an `enrichment` block in the spec with a second `base_url` and `auth`, plus field-mapping rules. Heavy approach: full multi-API spec support. The lightweight approach covers the 80% case.
- **Durable fix:** Skip for now. Note as a future direction. The hand-built approach works well enough for the few CLIs that need it.
- **Test:** N/A — future feature.
- **Evidence:** Built `internal/omdb/client.go` and wired it into `movies_get.go` entirely by hand.

## Prioritized Improvements

### P1 — High priority
| Finding | Title | Component | Frequency | Fallback Reliability | Complexity | Guards |
|---------|-------|-----------|-----------|---------------------|------------|--------|
| F1 | Positional query params treated as path params | Generator template `command_endpoint.go.tmpl` | Most APIs with search | Low — silent failure, 0 results | Small | Check if param name appears in path template |
| F2 | Required check ignores default value | Generator template `command_endpoint.go.tmpl` | Most APIs | Medium — easy to fix but annoying | Small | Add `(not .Default)` to condition |
| F3 | SQLite column names with dots invalid | `schema_builder.go` `toSnakeCase()` | APIs with dot-notation params | Low — immediate crash | Small | Replace dots with underscores |

### P2 — Medium priority
| Finding | Title | Component | Frequency | Fallback Reliability | Complexity | Guards |
|---------|-------|-----------|-----------|---------------------|------------|--------|
| F4 | No write-through caching | Generator template `data_source.go.tmpl` | Large-catalog APIs | Low — 4 iterations to get right manually | Medium | Gate behind config flag for large catalogs |
| F5 | Sync no page ceiling | Generator template `sync.go.tmpl` | Large-catalog APIs | Medium — user kills it, adds flag | Small | Add `--max-pages` with default 10 |
| F7 | Scorer doesn't recognize query-param auth | Scorecard command | APIs with query-param auth | N/A — cosmetic but misleading | Small | Check for `q.Set("api_key"` patterns |

### P3 — Low priority
| Finding | Title | Component | Frequency | Fallback Reliability | Complexity | Guards |
|---------|-------|-----------|-----------|---------------------|------------|--------|
| F6 | Search command picks wrong endpoint | Generator promoted command selection | APIs with multiple search scopes | High — one-line fix | Small | Prefer "multi"/"all" endpoints |
| F8 | No multi-API enrichment scaffolding | Spec format + generator | Multi-source CLIs | Medium — ~80 lines manual | Large | Future direction |

### Skip
| Finding | Title | Why unlikely to recur |
|---------|-------|----------------------|
| Genre-bridge recommendation algorithm | Custom to movie recommendation use case | Genuinely domain-specific, not a template pattern |
| Async goroutine write-through timing | Implementation detail | Synchronous write-through is the correct default |

## Work Units

### WU-1: Fix positional query param emission (from F1)
- **Goal:** Positional params that don't appear in the path template should be emitted as query params, not path substitutions
- **Target:** `internal/generator/templates/command_endpoint.go.tmpl` around line 77
- **Acceptance criteria:**
  - Positive: spec with `query` positional param on `/search/movie` path → emits `params["query"] = args[0]`
  - Negative: spec with `movieId` positional param on `/movie/{movieId}` path → still emits `replacePathParam`
- **Scope boundary:** Does not change how the spec parser classifies params. Template-only fix.
- **Dependencies:** None
- **Complexity:** Small

### WU-2: Skip required check when default exists (from F2)
- **Goal:** Params with `required: true` AND a non-nil `default` should not emit the "required flag not set" guard
- **Target:** `internal/generator/templates/command_endpoint.go.tmpl` lines 46-51
- **Acceptance criteria:**
  - Positive: `required: true, default: "day"` → no required check emitted
  - Negative: `required: true, default: nil` → required check still emitted
- **Scope boundary:** Does not change how defaults are applied to flags (that already works).
- **Dependencies:** None
- **Complexity:** Small

### WU-3: Sanitize dots in SQLite column names (from F3)
- **Goal:** Column names derived from params with dots should produce valid SQL
- **Target:** `internal/generator/schema_builder.go` `toSnakeCase()` function (line 361+)
- **Acceptance criteria:**
  - Positive: param `vote_average.gte` → column `vote_average_gte`
  - Negative: param `movie_id` → column `movie_id` (unchanged)
  - Negative: param `field.nested.deep` → column `field_nested_deep`
- **Scope boundary:** Only affects SQLite column name generation. Does not change flag names or API param names.
- **Dependencies:** None
- **Complexity:** Small

### WU-4: Add write-through caching to data source template (from F4)
- **Goal:** Live API responses are automatically upserted into the local SQLite store so FTS search grows organically with usage
- **Target:** `internal/generator/templates/data_source.go.tmpl`
- **Acceptance criteria:**
  - Positive: after a live `movies search "X"`, `search "X" --data-source local` finds results
  - Positive: handles TMDb-style `{"results": [...]}` envelope AND raw arrays AND single objects
  - Negative: bounded-API CLIs (Linear, Cal.com) don't get write-through unless opted in
- **Scope boundary:** Does not change sync behavior. Write-through is additive, not a replacement for sync.
- **Dependencies:** WU-3 (column names must be valid for the store to work)
- **Complexity:** Medium

### WU-5: Add sync page ceiling (from F5)
- **Goal:** Sync command has a `--max-pages` flag with a sensible default to prevent runaway pagination on large-catalog APIs
- **Target:** `internal/generator/templates/sync.go.tmpl`
- **Acceptance criteria:**
  - Positive: `sync --max-pages 3` → fetches exactly 3 pages per resource
  - Positive: `sync --max-pages 0` → unlimited (backwards compatible)
  - Positive: logs "reached --max-pages limit (N pages, M items)" when ceiling is hit
- **Scope boundary:** Does not change pagination logic or sync state management.
- **Dependencies:** None
- **Complexity:** Small

### WU-6: Fix scorecard auth_protocol for query-param auth (from F7)
- **Goal:** Scorecard recognizes query-param API key auth as a valid auth pattern
- **Target:** Scorecard command (likely `internal/cli/scorecard.go` or scoring helper)
- **Acceptance criteria:**
  - Positive: CLI with `q.Set("api_key", config.ApiKey)` → auth_protocol scores >= 8/10
  - Negative: CLI with no auth code at all → auth_protocol still fails
- **Scope boundary:** Scorer only. Does not change how the generator emits auth code.
- **Dependencies:** None
- **Complexity:** Small

## Anti-patterns
- **Don't assume all positional params are path segments.** The path template is the source of truth for where params go, not the `.Positional` flag.
- **Don't emit guards that contradict defaults.** If a param has a default, it's never "not set."
- **Don't trust user-facing names as valid SQL identifiers.** API param names can contain dots, slashes, brackets — always sanitize before using as column names.
- **Don't assume sync-everything is safe.** The generator should have a concept of catalog size and adjust sync defaults accordingly.

## What the Printing Press Got Right
- **Quality gates passed first try.** All 7 gates (go mod tidy, go vet, go build, binary, --help, version, doctor) passed on the initial generation. No compilation errors from the generator itself.
- **The command tree is excellent.** 23 commands covering search, discover, trending, genres, people, TV — all correctly structured with flags, examples, and output modes. The `--json`, `--select`, `--compact`, `--agent` flags work on every command.
- **The data source layer is well-designed.** The `auto/live/local` dispatch with provenance metadata is a strong pattern. Write-through was additive, not a replacement.
- **The MCP server was generated automatically.** 25 tools, stdio transport, ready to use. No manual work.
- **Scorecard 87/100 after polish.** Strong baseline that only needed 6 transcendence commands and 2 infrastructure fixes (auth, write-through) to be publish-ready.
- **The absorb gate works.** Researching 12 tools and cataloging 24 features ensured nothing was missed from the competitive landscape.
