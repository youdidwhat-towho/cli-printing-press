# Printing Press Retro: Kalshi

## Session Stats
- API: Kalshi (CFTC-regulated prediction market exchange)
- Spec source: Official OpenAPI 3.0 (91 endpoints)
- Scorecard: 89/100 (Grade A)
- Verify pass rate: 96% (after polish)
- Fix loops: 2
- Manual code edits: 4 (config.go, client.go, auth.go, doctor.go — full rewrites)
- Features built from scratch: 9 (all transcendence commands)

## Findings

### F1. `--name` flag ignored for single-spec generation (Bug)
- **What happened:** Running `printing-press generate --spec <url> --name kalshi` still generated the CLI as `kalshi-trade-manual` (derived from the spec title "Kalshi Trade API Manual Endpoints"). The `--name` flag was silently ignored.
- **Scorer correct?** N/A (not a scorer issue)
- **Root cause:** In `internal/cli/root.go` lines 254-283, when a single spec is provided, `cliName` (from `--name`) is never applied to `apiSpec.Name`. It is only used at line 258-261 for multi-spec merging. The output directory at line 282-283 is derived from `apiSpec.Name` (spec title), not `cliName`. The directory rename at lines 328-341 cements the spec-derived name.
- **Cross-API check:** Affects every API where the spec title doesn't match the desired CLI name. Most OpenAPI specs have verbose titles like "Stripe Payment Processing API" or "Discord Developer Gateway API" — users will want `stripe` or `discord`, not the full title.
- **Frequency:** Every API where spec title ≠ desired name (most APIs)
- **Fallback if not fixed:** Claude renames manually post-generation (go.mod, all imports, cmd/, cobra Use fields, README, manifest). This is ~15 minutes of fragile find-and-replace that could break imports.
- **Worth a Printing Press fix?** Absolutely. This is a documented flag that doesn't work.
- **Inherent or fixable:** Fixable. Three-line fix in root.go.
- **Durable fix:** In the single-spec branch (after line 261), add: `if cliName != "" { apiSpec.Name = cliName }`. This propagates through `DefaultOutputDir`, `generator.New`, and the manifest.
- **Test:** `printing-press generate --spec ./testdata/verbose-title.yaml --name short` → output dir should be `short`, go.mod should be `short-pp-cli`, binary should be `short-pp-cli`.
- **Evidence:** Session: ran `printing-press generate --spec https://docs.kalshi.com/openapi.yaml --name kalshi` and got `kalshi-trade-manual` output.

### F2. Sync extractPageItems uses hardcoded generic wrapper keys (Template gap)
- **What happened:** Kalshi API wraps responses as `{"markets": [...], "cursor": "..."}`. The sync template's `extractPageItems` only tries generic keys: `data`, `results`, `items`, `records`, `nodes`, `entries`. Since `markets` isn't in the list, sync found 0 items and fell through to `upsertSingleObject`, which failed with "missing id."
- **Scorer correct?** N/A
- **Root cause:** `internal/generator/templates/sync.go.tmpl` line 357 has a hardcoded list of wrapper keys. The profiler knows each resource's response wrapper key (from the OpenAPI response schema) but doesn't pass it to the sync template.
- **Cross-API check:** Affects any API that wraps responses with the resource name (e.g., `{"users": [...]}`, `{"orders": [...]}`). This is extremely common — Stripe, Kalshi, Linear, and most REST APIs use this pattern.
- **Frequency:** Most APIs (the majority of REST APIs wrap with resource names)
- **Fallback:** Claude manually adds API-specific wrapper keys to the list after generation. Unreliable — Claude might not realize sync is broken until dogfood testing.
- **Worth a Printing Press fix?** Yes — high-frequency, easy to fix, and the profiler already has the data.
- **Inherent or fixable:** Fixable. The profiler should extract response wrapper keys from the OpenAPI response schemas and pass them to the sync template. As a simpler first step, the template could try ALL single-key objects in the envelope (if exactly one key maps to an array, use it).
- **Durable fix:** In `extractPageItems`, after the hardcoded keys fail, try every key in the envelope: if exactly one key maps to a JSON array, use it. This is heuristic but covers the resource-name-as-wrapper pattern universally. No profiler changes needed.
- **Test:** Generate from a spec where `/markets` returns `{"markets": [...]}`. Sync should extract items correctly without manual edits.
- **Evidence:** Sync failed with "missing id for markets" until wrapper keys were manually added.

### F3. UpsertBatch only uses "id" as primary key (Template gap)
- **What happened:** Kalshi uses `ticker` as the primary identifier for markets, events, and series. The store template's `UpsertBatch` calls `lookupFieldValue(obj, "id")` and skips items where id is empty. All 20,000+ market records were silently skipped (0 records synced).
- **Scorer correct?** N/A
- **Root cause:** `internal/generator/templates/store.go.tmpl` line 346 hardcodes `"id"`. The `extractID` function in `sync.go.tmpl` line 486 tries `id, ID, uuid, slug, name` but not `ticker`, `key`, or other API-specific identifiers.
- **Cross-API check:** Affects APIs that don't use `id` as the primary field name. Common alternatives: `ticker` (financial APIs), `key` (KV stores), `slug` (content APIs), `uid` (Firebase), `code` (currency/country APIs).
- **Frequency:** API subclass: financial/trading APIs (~20% of APIs)
- **Fallback:** Claude adds `ticker` manually. Semi-reliable — Claude would catch it during dogfood testing but only after sync silently returns 0 records.
- **Worth a Printing Press fix?** Yes. The profiler can detect which field is the primary identifier from the OpenAPI schema (look for `x-identifier`, unique constraints, or the first path parameter).
- **Inherent or fixable:** Fixable. Two approaches: (1) expand the hardcoded list to include common alternatives, or (2) have the profiler detect the primary key field and emit it into the template.
- **Durable fix:** Approach 1 (quick): Add `ticker`, `key`, `code`, `uid` to both `UpsertBatch` and `extractID`. Approach 2 (better): The profiler reads the primary key from `x-identifier` or the first path parameter of detail endpoints (e.g., `/markets/{ticker}` → primary key is `ticker`). Emit the primary key into `store.go` and `sync.go` templates.
- **Test:** Generate from a spec where entities use `ticker` as ID. Sync should upsert correctly without manual edits. Also test that APIs using `id` still work (regression).
- **Evidence:** Sync showed "missing id for markets" — 0 records synced from 20,000+ market API responses.

### F4. Spec archived as "spec.json" even when content is YAML (Bug)
- **What happened:** The Kalshi OpenAPI spec is served as YAML. The generator archived it as `spec.json` inside the output directory, causing `printing-press scorecard --spec spec.json` to fail with "invalid character 'o' looking for beginning of value."
- **Scorer correct?** The scorecard correctly rejects invalid JSON. The bug is in the archiver.
- **Root cause:** `internal/cli/root.go` lines 357-360: `archiveName := "spec.json"` is the default. The condition `!openapi.IsOpenAPI(specRawBytes[0])` only switches to `.yaml` when the content is NOT OpenAPI. Since Kalshi's YAML IS OpenAPI, `IsOpenAPI` returns true, and the YAML content gets named `.json`.
- **Cross-API check:** Affects every API that serves OpenAPI specs in YAML format (very common — GitHub, Stripe, and many others serve YAML).
- **Frequency:** Most APIs (YAML is the dominant OpenAPI format)
- **Fallback:** Claude renames `spec.json` to `spec.yaml` manually. Easy but forgettable.
- **Worth a Printing Press fix?** Yes — trivial fix, prevents scorer failures.
- **Inherent or fixable:** Fixable. Check the content format (JSON vs YAML), not just whether it's OpenAPI.
- **Durable fix:** Replace the condition with: `if json.Valid(specRawBytes[0]) { archiveName = "spec.json" } else { archiveName = "spec.yaml" }`. This checks the actual format, not the schema type.
- **Test:** Generate from a YAML OpenAPI spec. `spec.yaml` should be created, not `spec.json`. Generate from a JSON OpenAPI spec — `spec.json` should still be created.
- **Evidence:** `printing-press scorecard --spec spec.json` failed because the file was YAML.

### F5. Generator produces OAuth-style auth for RSA-PSS APIs (Assumption mismatch)
- **What happened:** The generator created config.go with OAuth token fields (`AccessToken`, `RefreshToken`, `TokenExpiry`, `SaveTokens`, `ClearTokens`) and client.go with a simple `KALSHI-ACCESS-KEY` header. Kalshi requires RSA-PSS signature auth with 3 headers (KEY, TIMESTAMP, SIGNATURE). All four files (config.go, client.go, auth.go, doctor.go) had to be rewritten.
- **Scorer correct?** N/A — scorer rated auth 10/10 after manual rewrite.
- **Root cause:** The generator templates assume one of: no auth, bearer token, API key header, or OAuth2. RSA-PSS signing (where each request is cryptographically signed with a private key) isn't in the template vocabulary.
- **Cross-API check:** Rare but significant when it hits. APIs with RSA-PSS or HMAC signing: Kalshi, AWS (SigV4), Coinbase, some payment gateways. Maybe 5-10% of APIs.
- **Frequency:** API subclass: cryptographic-signing APIs (~5-10%)
- **Fallback:** Claude rewrites the auth system. Took ~15 minutes but requires deep understanding of the target API's auth scheme. Moderately reliable — Claude knows RSA-PSS but might miss salt length or message format details.
- **Worth a Printing Press fix?** Medium priority. The fix is complex (new auth template variant) and the frequency is low. But when it hits, it's 4 files rewritten.
- **Inherent or fixable:** Partially fixable. The profiler could detect `securitySchemes` with `x-signing-algorithm` or specific header patterns (HMAC, RSA-PSS) and select a signing template variant. The signing implementation itself would need to be API-specific, so the template can only scaffold the structure.
- **Durable fix:** Add a "signing" auth template variant that emits: private key loading in config.go, message construction + signing in client.go, key setup guidance in auth.go. Parameterize by: algorithm (RSA-PSS, HMAC-SHA256), headers (configurable names), message format (configurable concatenation pattern). Condition: profiler detects 3+ required headers with "signature", "timestamp", or "hmac" in their names.
- **Test:** Generate from a spec with RSA-PSS security scheme. Config should load a private key. Client should sign requests. Auth should show key setup guidance.
- **Evidence:** All four auth-related files were fully rewritten during build.

### F6. Publish skill doesn't enforce alphabetical ordering of registry.json entries (Skill instruction gap)
- **What happened:** The publish skill's Step 8 instructions say to "add or update the entry" in registry.json but don't mention sorting entries alphabetically. Different agents may append new entries at random positions, making the file harder to review and more likely to cause merge conflicts.
- **Scorer correct?** N/A
- **Root cause:** `skills/printing-press-publish/SKILL.md` Step 8 "Update registry.json" section. The instruction says: "Read `$PUBLISH_REPO_DIR/registry.json`, parse the entries array, add or update the entry for this CLI. Match on name field." No mention of sort order.
- **Cross-API check:** Affects every publish operation.
- **Frequency:** Every publish
- **Fallback:** Some agents sort by coincidence (this session's agent did), but there's no guarantee. Merge conflicts increase as more CLIs are published.
- **Worth a Printing Press fix?** Yes — trivial skill edit.
- **Inherent or fixable:** Fixable. One line added to the skill.
- **Durable fix:** Add to Step 8 after "Match on `name` field": "After adding/updating, sort the `entries` array alphabetically by `name` field. This prevents merge conflicts and makes the registry easier to review."
- **Test:** Run two publishes in sequence. Second publish should produce a registry with entries sorted by name, not appended at the end.
- **Evidence:** User noticed entries might not be alphabetically ordered when the skill is followed by different agents.

## Prioritized Improvements

### P1 — High priority
| Finding | Title | Component | Frequency | Fallback Reliability | Complexity |
|---------|-------|-----------|-----------|---------------------|------------|
| F1 | `--name` flag ignored for single-spec generation | Binary (`internal/cli/root.go`) | Most APIs | Low — 15-min manual rename | Small |
| F2 | Sync wrapper key detection | Generator template (`sync.go.tmpl`) | Most APIs | Low — sync silently fails | Small |
| F3 | Primary key detection in UpsertBatch | Generator template (`store.go.tmpl`, `sync.go.tmpl`) | 20% of APIs | Low — 0 records synced | Small |

### P2 — Medium priority
| Finding | Title | Component | Frequency | Fallback Reliability | Complexity |
|---------|-------|-----------|-----------|---------------------|------------|
| F4 | Spec archived with wrong extension | Binary (`internal/cli/root.go`) | Most APIs | High — easy manual rename | Small |
| F6 | Registry.json alphabetical ordering | Skill (`printing-press-publish/SKILL.md`) | Every publish | Medium — some agents sort | Small |

### P3 — Low priority
| Finding | Title | Component | Frequency | Fallback Reliability | Complexity |
|---------|-------|-----------|-----------|---------------------|------------|
| F5 | RSA-PSS auth template | Generator templates | 5-10% of APIs | Medium — Claude can rewrite | Large |

## Work Units

### WU-1: Fix --name flag for single-spec generation (from F1)
- **Goal:** When `--name` is passed with a single spec, the generated CLI should use that name instead of the spec title.
- **Target:** `internal/cli/root.go` — the single-spec generation branch after line 261
- **Acceptance criteria:**
  - positive test: `printing-press generate --spec verbose-title.yaml --name short` → output uses `short`, go.mod is `short-pp-cli`
  - negative test: `printing-press generate --spec verbose-title.yaml` (no --name) → still uses spec-derived name
  - negative test: multi-spec with --name → existing behavior preserved
- **Scope boundary:** Does not change name derivation logic (cleanSpecName). Only adds override when --name is explicit.
- **Dependencies:** None
- **Complexity:** small

### WU-2: Universal sync wrapper key detection (from F2)
- **Goal:** Sync should automatically detect API response wrapper keys without requiring manual edits.
- **Target:** `internal/generator/templates/sync.go.tmpl` — `extractPageItems` function
- **Acceptance criteria:**
  - positive test: API returns `{"markets": [...], "cursor": "..."}` → items extracted from `markets` key
  - positive test: API returns `{"data": [...]}` → existing behavior preserved
  - negative test: API returns `{"count": 5, "error": null}` → no false extraction from non-array keys
- **Scope boundary:** Does not change the profiler or spec analysis. Heuristic-based detection only.
- **Dependencies:** None
- **Complexity:** small

### WU-3: Expanded primary key detection (from F3)
- **Goal:** UpsertBatch and extractID should recognize common non-"id" primary key field names.
- **Target:** `internal/generator/templates/store.go.tmpl` (UpsertBatch), `internal/generator/templates/sync.go.tmpl` (extractID)
- **Acceptance criteria:**
  - positive test: record with `ticker` field → used as primary key
  - positive test: record with `id` field → existing behavior preserved (id takes priority)
  - negative test: record with `name` only → still works (existing fallback)
- **Scope boundary:** Adds field names to the hardcoded list. Does not add profiler-based detection (that's a separate WU).
- **Dependencies:** None
- **Complexity:** small

### WU-4: Fix spec archive file extension (from F4)
- **Goal:** Archived spec file extension should match the actual content format (YAML or JSON).
- **Target:** `internal/cli/root.go` — spec archiving block around line 357
- **Acceptance criteria:**
  - positive test: YAML OpenAPI spec → archived as `spec.yaml`
  - positive test: JSON OpenAPI spec → archived as `spec.json`
  - positive test: internal YAML spec → archived as `spec.yaml`
- **Scope boundary:** Does not change spec parsing or validation.
- **Dependencies:** None
- **Complexity:** small

### WU-5: Publish registry.json alphabetical ordering (from F6)
- **Goal:** Registry entries should always be sorted alphabetically by name after publish.
- **Target:** `skills/printing-press-publish/SKILL.md` — Step 8 "Update registry.json"
- **Acceptance criteria:**
  - positive test: After adding a new entry, all entries are sorted by name
  - positive test: After updating an existing entry, sort order is preserved
- **Scope boundary:** Skill instruction only. Does not add binary-level validation.
- **Dependencies:** None
- **Complexity:** small

## Anti-patterns
- Silent failures in sync: UpsertBatch skipped ALL records with `continue` — no error, no warning, just 0 records. The sync reported success with 0 records. This pattern should be changed to at least log a warning when >50% of items lack an extractable ID.
- Flag silently ignored: `--name` accepted the flag, showed no error, and used a different name. Ignored flags should either work or error — never silently do something else.

## What the Printing Press Got Right
- OpenAPI parser handled 91 endpoints cleanly — all path parameters, query parameters, and response schemas were correctly mapped
- Quality gates (go mod tidy, go vet, go build, --help, version, doctor) all passed on first generation
- Adaptive rate limiter worked perfectly during live testing — no 429s during 20,000+ market sync
- Agent-native flags (--json, --select, --compact, --csv, --dry-run, --agent) all wired correctly across 80+ commands
- MCP server generation produced 89 tools automatically
- The data layer template (SQLite + FTS5 + WAL + cursor pagination) is production-grade
- Scorecard at 89/100 on first pass — the templates produce high-quality CLIs even when auth needs manual work
