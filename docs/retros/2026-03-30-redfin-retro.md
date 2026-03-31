# Printing Press Retro: Redfin

## Session Stats
- API: Redfin (unofficial Stingray API)
- Spec source: Hand-crafted OpenAPI from community docs (no official spec, no sniff - bot detection blocked automated browsers)
- Scorecard: 81/100 (Grade A)
- Verify pass rate: 52%
- Fix loops: 2 (both ineffective)
- Manual code edits: 5 (client JSONP strip, User-Agent, env vars, promoted cmd removal, store rewrite)
- Features built from scratch: 17 files (search, property, portfolio, trends, deals, mortgage, score, invest, pulse, compare-hoods, track, report, analyze-zips, data, stale, schools, store rewrite)
- Generated LOC: 5,106 | Hand-written LOC: 5,504

## Findings

### 1. Verify cannot test commands that require positional args (tool limitation)

- **What happened:** 11 of 23 commands failed verify because `workflowTestFlags()` in `internal/pipeline/runtime.go:308` only provides mock args for a hardcoded set of known workflow commands (pr-triage, stale, actions-health, etc.). Hand-written transcendence commands (pulse, deals, mortgage, score, etc.) are unknown to verify, so they're invoked with no positional args. Cobra's `Args: cobra.ExactArgs(1)` rejects the invocation before `RunE` ever runs.
- **Root cause:** `internal/pipeline/runtime.go` - `workflowTestFlags()` has a closed set of known commands. `classifyCommandKind()` (line 282) defaults everything unknown to "read", meaning these commands get the full dry-run + exec test suite but with no args to satisfy their cobra Args constraint.
- **Cross-API check:** This will occur on every API where Claude builds custom commands beyond the generated stingray/API layer. The absorb manifest always produces hand-written commands. This is not API-specific.
- **Frequency:** every API
- **Fallback if machine doesn't fix it:** Claude would need to either (a) avoid ExactArgs and check inside RunE, or (b) the skill would need to instruct Claude to do this. Claude sometimes forgets, and the verify failure gives no actionable guidance (it just says "manual execution fix required"). Reliability: sometimes.
- **Worth a machine fix?** Yes. This is the single biggest quality gap. 11/23 commands failing verify is a false negative that makes the verify tool unreliable for hand-built commands.
- **Inherent or fixable:** Fixable. Two complementary approaches:
  1. **Verify: infer required args from --help output.** Parse the `Use:` line (e.g., `pulse <region>`) and supply synthetic values for positional args. `<region>` -> `"mock-region"`, `<property-id>` -> `"12345"`, `<price>` -> `"500000"`, `<zip>` -> `"94102"`.
  2. **Verify: classify custom commands as "data-layer" when they call store.Open().** Grep the command source for `store.Open` and reclassify. Data-layer commands skip the dry-run/exec tests that fail.
- **Durable fix:** Option 1 is more durable. In `runtime.go`, after discovering commands, parse each command's `--help` output to extract the Use line. Use a regex like `<(\w+[-\w]*)>` to find positional arg placeholders. Map common placeholder names to synthetic values: region/location -> "mock-city", property-id/id -> "12345", price -> "500000", zip -> "94102", url -> "/mock/path". Supply these as extra args alongside `--dry-run`.
- **Test:** Generate a CLI with a custom command requiring `<region>` positional arg. Verify should supply "mock-city" and pass.
- **Evidence:** Verify output showing 11 commands at score 1/3 despite all working correctly when tested manually with real args.

### 2. Generated store has only generic tables, no entity-specific schema (template gap)

- **What happened:** The generator emitted a store with `resources` (generic) and `stingray` (single JSON blob table). The entire store had to be rewritten to add 7 domain tables (properties, valuations, price_history, regions, trends, portfolio, scoring_profiles) with typed columns, proper indexes, and FTS. This was 400+ lines of hand-written Go.
- **Root cause:** `internal/generator/templates/store.go.tmpl` - The store template emits one generic `resources` table and one table per API service. It doesn't analyze the spec to derive entity-specific tables with typed columns.
- **Cross-API check:** Every CLI session includes a Phase 3 where the absorb manifest demands entity-specific tables. The linear-pp-cli needed issues/projects/cycles tables. The discord-pp-cli needed guilds/channels/messages tables. This is universal.
- **Frequency:** every API
- **Fallback if machine doesn't fix it:** Claude rewrites the store from scratch every time. This is the single largest hand-written block (~400 LOC). Claude usually gets it right but it's error-prone and inconsistent across CLIs. Reliability: usually, but quality varies.
- **Worth a machine fix?** Yes. This is the highest-leverage improvement. If the generator could derive entity tables from spec schemas, it would eliminate the biggest chunk of manual work.
- **Inherent or fixable:** Fixable. The spec already has response schemas that name the primary entities. The generator could:
  1. Identify the top-N entities from response schemas (objects with id/name/status-like fields)
  2. Generate a table per entity with typed columns for high-gravity fields
  3. Generate `Upsert<Entity>` and `Get<Entity>` methods
  4. Generate FTS virtual tables for entities with text fields
- **Durable fix:** In `internal/generator/entity_mapper.go` (or new file), build an entity extraction pipeline that reads spec schemas, identifies primary entities, and emits table DDL + Go struct types + CRUD methods. Feed this into the store template as `.Entities`. The template iterates and emits one table + methods per entity.
  - Condition: Always run when spec has response schemas with object types
  - Guard: Skip for specs with no response schemas (fall back to generic store)
- **Test:** Generate a CLI from the GitHub spec. Verify the store has tables for `issues`, `pull_requests`, `repositories` with typed columns, not just a generic `resources` table.
- **Evidence:** Store rewrite from 408 LOC generic store to 900+ LOC with 7 domain tables.

### 3. Promoted command collides with API group command (bug)

- **What happened:** The generator emitted both `newStingrayCmd()` (the API group command with subcommands) and `newStingrayPromotedCmd()` (a top-level alias for `get-above-the-fold`). Both registered as `stingray` in the root command, causing a duplicate entry in `--help`. Had to manually remove the promoted command.
- **Root cause:** `internal/generator/generator.go:456` - `buildPromotedCommands()` doesn't check whether the promoted name collides with the API service group command name.
- **Cross-API check:** Occurs when the API has a single service (not multi-service). The promoted command name is derived from the service name, which is the same as the group command name.
- **Frequency:** most APIs (any single-service API)
- **Fallback if machine doesn't fix it:** Claude notices the duplicate in `--help` and removes it. Reliability: usually, but it's easy to miss during a fast session.
- **Worth a machine fix?** Yes. Simple guard.
- **Inherent or fixable:** Fixable. In `buildPromotedCommands()`, skip any promoted command whose name matches an existing service group command.
- **Durable fix:** In `internal/generator/generator.go`, add a check in the promoted commands loop: if `pc.PromotedName == serviceName`, skip it.
- **Test:** Generate from a single-service spec. Verify no duplicate commands in `--help` output.
- **Evidence:** `--help` showed two `stingray` entries.

### 4. Client doesn't handle non-JSON response prefixes (template gap)

- **What happened:** Redfin's Stingray API prefixes all JSON responses with `{}&&` (JSONP/XSSI protection). The generated client returned raw bytes without stripping this prefix, causing JSON parse failures everywhere. Had to manually add `bytes.TrimPrefix` logic.
- **Root cause:** `internal/generator/templates/client.go.tmpl` - The client template assumes API responses are clean JSON. No handling for common response wrappers like JSONP prefixes, BOM markers, or response envelopes.
- **Cross-API check:** JSONP/XSSI prefixes (`)]}'`, `{}&&`, `)]}'\n`) are used by Google APIs, Facebook APIs, and other services that serve browser-facing JSON. This is a known API subclass pattern.
- **Frequency:** API subclass: JSONP-protected APIs (estimated 10-15% of APIs)
- **Fallback if machine doesn't fix it:** Claude must manually edit client.go every time. Reliability: sometimes (Claude must know the API uses JSONP protection, which requires research).
- **Worth a machine fix?** Yes. A response sanitization step in the client template is low-cost and high-value.
- **Inherent or fixable:** Fixable. Add a `sanitizeResponse()` function to the client template that strips known JSONP/XSSI prefixes before JSON parsing.
- **Durable fix:** In `client.go.tmpl`, add after reading the response body:
  ```go
  respBody = sanitizeJSONResponse(respBody)
  ```
  Where `sanitizeJSONResponse` strips `)]}'`, `{}&&`, and UTF-8 BOM. This is safe for clean JSON responses (no-op if no prefix found).
  - Condition: Always active (no-op for clean responses)
  - Guard: None needed (safe for all APIs)
- **Test:** Generate a CLI, mock a response with `{}&&{"ok":true}`, verify it parses correctly.
- **Evidence:** Client fix at `internal/client/client.go` adding bytes.TrimPrefix logic.

### 5. Env var names use hyphens instead of underscores (bug)

- **What happened:** Config env vars were `REDFIN-STINGRAY-UNOFFICIAL_CONFIG` and `REDFIN-STINGRAY-UNOFFICIAL_BASE_URL`. Hyphens are not valid in most shell env var names. Had to manually fix to `REDFIN_PP_CLI_CONFIG` and `REDFIN_PP_CLI_BASE_URL`.
- **Root cause:** `internal/generator/templates/config.go.tmpl` - The template derives env var names from the CLI name without replacing hyphens with underscores.
- **Cross-API check:** Any API whose derived name contains hyphens (most of them, since `naming.CLI()` uses kebab-case).
- **Frequency:** every API
- **Fallback if machine doesn't fix it:** Claude fixes it manually. Reliability: usually.
- **Worth a machine fix?** Yes. Trivial fix.
- **Inherent or fixable:** Fixable. In the config template, apply `strings.ReplaceAll(name, "-", "_")` and `strings.ToUpper()` when generating env var names.
- **Durable fix:** In `config.go.tmpl`, change the env var name derivation to use `{{envName .Name}}` where `envName` is a template function that converts to SCREAMING_SNAKE_CASE.
- **Test:** Generate a CLI named `foo-bar-pp-cli`. Verify env vars are `FOO_BAR_PP_CLI_CONFIG`, not `FOO-BAR-PP-CLI_CONFIG`.
- **Evidence:** Config fix at `internal/config/config.go`.

### 6. No-spec APIs need hand-crafted OpenAPI as intermediate format (recurring friction)

- **What happened:** Redfin has no official API or spec. Had to research 13 tools, reverse-engineer 40+ endpoints from community docs, and hand-write a 531-line OpenAPI YAML to feed the generator. This was a significant time investment.
- **Root cause:** The printing press pipeline assumes a spec exists (catalog, OpenAPI, or sniffed). When no spec exists, Claude must write one from scratch. The generator can't consume a "docs list" directly.
- **Cross-API check:** Many popular services have no official API (ESPN, Craigslist, etc.) or have unofficial APIs that are only documented in community reverse-engineering projects. This is a common input class.
- **Frequency:** API subclass: no-spec/unofficial APIs (estimated 20-30%)
- **Fallback if machine doesn't fix it:** Claude researches and writes the spec manually. Works, but time-intensive and error-prone (missed endpoints, wrong param types).
- **Worth a machine fix?** Yes, but medium complexity. The `--docs` flag exists but wasn't used here because the docs are scattered across GitHub repos and blog posts, not a single URL.
- **Inherent or fixable:** Partially fixable. The current `--docs` path could be enhanced to accept multiple URLs and synthesize a spec from them. A `--research` mode could take Claude's research brief as input and auto-generate a spec stub.
- **Durable fix:** New `printing-press generate --from-research <brief.md>` mode that parses a structured research brief (the one Claude already writes in Phase 1) and generates an OpenAPI spec from the endpoint list. This would replace the manual spec-writing step.
  - Condition: No spec found and no single docs URL available
  - Guard: Skip when spec or docs URL is available
  - Frequency estimate: ~20-30% of APIs
- **Test:** Feed the redfin brief to `--from-research` and verify it produces a valid OpenAPI spec with the documented endpoints.
- **Evidence:** 531-line hand-written OpenAPI spec at `research/redfin-stingray-spec.yaml`.

### 7. Skill doesn't instruct Claude to present ship decision after shipcheck (skill instruction gap)

- **What happened:** After shipcheck came back `ship-with-gaps` at 81/100, Claude wrote the shipcheck artifact, archived manuscripts, and printed a summary. Then stopped. Never asked the user whether they wanted to publish to GitHub, fix the gaps, or take any other action. The user had to ask "why didn't you ask me to publish?"
- **Root cause:** `skills/printing-press/SKILL.md` - The skill has detailed phases for research, generation, building, and verification, but no post-shipcheck decision phase. The skill ends at "write the shipcheck artifact" without instructing Claude to present the ship recommendation to the user and ask for next steps.
- **Cross-API check:** This will occur on every single printing press run. There's no instruction telling Claude what to do after shipcheck.
- **Frequency:** every API
- **Fallback if machine doesn't fix it:** Claude just stops and the user has to prompt for next steps. Reliability: never (Claude consistently stops after writing the shipcheck because the skill says nothing about what happens next).
- **Worth a machine fix?** Yes. This is a critical UX gap. The whole point of the skill is to ship a CLI, and it drops the ball at the last step.
- **Inherent or fixable:** Fixable. Add a Phase 6 to the skill.
- **Durable fix:** Add to `SKILL.md` after Phase 5:

  ```markdown
  ## Phase 6: Ship Decision

  Present the shipcheck results to the user via `AskUserQuestion`:

  > "Shipcheck: [verdict] ([score]/100). [summary of gaps if any]. What do you want to do?"
  >
  > 1. **Publish to GitHub** — init repo, commit, push, create release
  > 2. **Fix gaps first** — address the verify/dogfood failures before publishing
  > 3. **Done for now** — CLI is at ~/printing-press/library/<api>-pp-cli, ship later

  If the user picks "Publish to GitHub":
  1. `cd` to the CLI directory
  2. `git init && git add -A && git commit -m "Initial generation"`
  3. Create GitHub repo via `gh repo create mvanhorn/<api>-pp-cli --public --source=.`
  4. `git push -u origin main`
  5. Report the repo URL
  ```

- **Test:** Run a printing press session to completion. Verify Claude asks the user what to do after shipcheck instead of just stopping.
- **Evidence:** User feedback: "Why didn't you ask me to publish?"

### 8. Dead helper functions emitted by generator template (recurring friction)

- **What happened:** Dogfood flagged 15 dead functions in `helpers.go`: apiErr, bold, colorEnabled, compactFields, compactListFields, compactObjectFields, filterFields, isTerminal, levenshteinDistance, newTabWriter, notFoundErr, paginatedGet, printCSV, printOutput, rateLimitErr. These are emitted by the generator but not called by the generated or hand-written commands.
- **Root cause:** `internal/generator/templates/helpers.go.tmpl` - The template emits a full set of helpers regardless of which ones the generated commands actually use.
- **Cross-API check:** Occurs on every generation. The helpers template is one-size-fits-all.
- **Frequency:** every API
- **Fallback if machine doesn't fix it:** Claude ignores them (they're harmless) or manually deletes them. Reliability: always (they're cosmetic, not functional). But they inflate LOC counts and dogfood reports.
- **Worth a machine fix?** Yes, but low priority. These are cosmetic.
- **Inherent or fixable:** Partially inherent (templates must be generic) but fixable via post-generation cleanup.
- **Durable fix:** Add a `printing-press polish --dead-code` post-processing step that runs `go vet` analysis and removes unused functions from helpers.go. Alternatively, make the generator conditionally emit helpers based on which commands were generated.
- **Test:** Generate a CLI, run polish, verify no dead functions in dogfood output.
- **Evidence:** Dogfood report showing 15 dead functions.

### 9. Bot detection blocks sniff and live smoke for protected sites (recurring friction)

- **What happened:** Redfin blocks both headless and headed automated browsers with bot detection. The sniff phase (Phase 1.7) failed completely, and live smoke (Phase 5) was skipped. This left us with no live API validation.
- **Root cause:** Not a machine bug. Redfin has aggressive bot detection. But the skill doesn't have a fallback path for when sniff fails due to bot detection.
- **Cross-API check:** Many high-value sites have bot detection: Zillow, LinkedIn, Amazon, etc. This is a common obstacle for no-spec APIs.
- **Frequency:** API subclass: bot-protected sites (estimated 15-25%)
- **Fallback if machine doesn't fix it:** Claude skips sniff and proceeds with docs. Reliability: always (Claude handled this correctly in this session).
- **Worth a machine fix?** Partial. The skill could be more explicit about the fallback path when bot detection is encountered.
- **Inherent or fixable:** Mostly inherent (can't bypass bot detection), but the skill could add stealth browser options (playwright with stealth plugin, residential proxies) as optional tools.
- **Durable fix:** In the sniff gate instructions, add a "bot detection fallback" section that explicitly says: "If both browser-use and agent-browser are blocked by bot detection, immediately fall back to docs-based generation. Do not retry. Report: 'Site has bot detection, proceeding with documented endpoints only.'" This makes the failure path fast instead of exploratory.
- **Test:** N/A (inherent limitation, just improving the instruction clarity).
- **Evidence:** browser-use and agent-browser both redirected to ratelimited.redfin.com.

## Prioritized Improvements

### Do Now
| # | Fix | Component | Frequency | Fallback Reliability | Complexity | Guards |
|---|-----|-----------|-----------|---------------------|------------|--------|
| 7 | Add Phase 6 ship decision to skill | SKILL.md | every API | never | small | none |
| 5 | Fix env var naming (hyphens -> underscores) | config.go.tmpl | every API | usually | small | none |
| 3 | Prevent promoted command name collision | generator.go | most APIs | usually | small | none |

### Do Next (needs design/planning)
| # | Fix | Component | Frequency | Fallback Reliability | Complexity | Guards |
|---|-----|-----------|-----------|---------------------|------------|--------|
| 1 | Verify: infer positional args from --help | runtime.go | every API | sometimes | medium | none |
| 4 | Client: sanitize JSONP/XSSI prefixes | client.go.tmpl | subclass: JSONP APIs | sometimes | small | no-op for clean JSON |
| 2 | Entity-specific store tables from spec | store.go.tmpl, entity_mapper.go | every API | usually (but inconsistent) | large | skip when no response schemas |
| 8 | Dead code cleanup post-processing | new polish command | every API | always (cosmetic) | medium | none |

### Skip
| # | Fix | Why unlikely to recur |
|---|-----|----------------------|
| 9 | Bot detection bypass | Inherent limitation. Improved skill instruction is sufficient. Can't bypass bot detection without ethical/legal concerns. |
| 6 | --from-research generation mode | High value but very large scope. The current pattern (Claude writes spec from research) works. This is a v2 feature, not a retro fix. |

## Work Units

### WU-1: Post-shipcheck ship decision (finding #7)
- **Goal:** Claude presents ship options after shipcheck instead of silently stopping
- **Target files:** `skills/printing-press/SKILL.md`
- **Acceptance criteria:**
  - positive: Run printing press to completion. After shipcheck, Claude asks "What do you want to do?" with publish/fix/done options
  - negative: N/A
- **Scope boundary:** Only adds the Phase 6 section. Does not change any other phase.
- **Complexity:** small

### WU-2: Env var and promoted command fixes (findings #3, #5)
- **Goal:** Generated CLIs have valid env var names and no duplicate promoted commands
- **Target files:** `internal/generator/templates/config.go.tmpl`, `internal/generator/generator.go`
- **Acceptance criteria:**
  - positive: Generate from a hyphenated API name -> env vars use SCREAMING_SNAKE_CASE
  - positive: Generate from a single-service spec -> no duplicate command names in --help
  - negative: Generate from a multi-service spec -> promoted commands still work
- **Scope boundary:** Does not change any other template behavior.
- **Complexity:** small

### WU-3: Verify positional arg inference (finding #1)
- **Goal:** Verify supplies synthetic positional args to commands that require them
- **Target files:** `internal/pipeline/runtime.go`
- **Acceptance criteria:**
  - positive: Generate a CLI with `pulse <region>`. Verify passes dry-run and exec.
  - positive: Generate a CLI with `mortgage [price]`. Verify passes with synthetic price.
  - negative: Commands without positional args still tested correctly
- **Scope boundary:** Only changes workflowTestFlags and/or command discovery. Does not change how verify scores results.
- **Complexity:** medium

### WU-4: JSONP/XSSI response sanitization (finding #4)
- **Goal:** Generated clients automatically strip common JSONP/XSSI response prefixes
- **Target files:** `internal/generator/templates/client.go.tmpl`
- **Acceptance criteria:**
  - positive: Client receiving `{}&&{"ok":true}` returns `{"ok":true}`
  - positive: Client receiving `)]}'{"ok":true}` returns `{"ok":true}`
  - negative: Client receiving clean `{"ok":true}` returns it unchanged
- **Scope boundary:** Only adds sanitization. Does not change request behavior.
- **Complexity:** small

### WU-5: Entity-specific store generation (finding #2)
- **Goal:** Generator emits domain-specific tables with typed columns from spec response schemas
- **Target files:** `internal/generator/entity_mapper.go` (new or existing), `internal/generator/templates/store.go.tmpl`, `internal/generator/generator.go`
- **Acceptance criteria:**
  - positive: Generate from GitHub spec -> store has `issues` table with `title TEXT`, `state TEXT`, etc.
  - positive: Generated store includes FTS virtual tables for text-heavy entities
  - negative: Spec with no response schemas -> falls back to generic store (no regression)
- **Scope boundary:** Does not change how sync or search commands work. Only changes the store schema and CRUD methods.
- **Complexity:** large

## Anti-patterns

- **Treating shipcheck output as terminal state.** The shipcheck is a decision point, not an endpoint. The skill must instruct Claude to act on the recommendation.
- **Delegating store rewrites to agents without typed method contracts.** Two agents wrote code concurrently -- one for the store, one for commands. The commands agent had to use generic store methods because the store agent might not have added typed methods yet. This created unnecessary indirection.

## What the Machine Got Right

- **Generator produced 28 working API commands from a hand-crafted spec.** All stingray commands compiled and worked correctly with the spec.
- **Adaptive rate limiter in the client.** Proactive rate limiting with ceiling discovery is a strong default for unofficial APIs.
- **Response caching.** The 5-minute cache in the client is appropriate for real estate data that doesn't change frequently.
- **Agent-native flags.** The `--agent` meta-flag that sets `--json --compact --no-input --no-color --yes` is a good pattern.
- **Exit code classification.** Typed exit codes (0/2/3/4/5/7) in helpers.go are well-designed.
- **MCP server generation.** The generator auto-emitted an MCP server alongside the CLI.
- **Spec filter flexibility.** The generator accepted the hand-crafted YAML spec without complaint via `--lenient --validate`.
