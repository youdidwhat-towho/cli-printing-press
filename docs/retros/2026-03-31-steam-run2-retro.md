# Printing Press Retro: Steam Web API (Run 2)

## Session Stats
- API: Steam Web API
- Spec source: Zuplo/Steam-OpenAPI (158 operations, OpenAPI 3.0)
- Scorecard: 68 → 70/100 (after polish)
- Verify pass rate: 80% → 75% (more commands diluted pass rate)
- Fix loops: 1 (generation) + 1 (polish)
- Manual code edits: 1 (root description rewrite, STEAM_API_KEY env var)
- Features built from scratch: 22 (16 wrapper + 6 transcendence) + 6 insight commands in polish

This is Run 2, after merging scorer fixes from PR #100 (verify name derivation, dogfood dead-flag detection, dry-run cache poisoning, template improvements).

## Findings

### 1. Path validity 0/10 despite all CLI paths coming from the spec (Scorer bug)

- **Scorer correct?** No — investigating. The CLI has 150+ `path := "/ISteamUser/GetPlayerSummaries/v2"` assignments extracted directly from the spec. The spec has matching paths. The scorecard function `evaluatePathValidity()` (scorecard.go:987-1021) samples 10 command files and regex-matches `\bpath\s*(?::=|=|:)\s*"([^"]+)"`, then checks each against the spec via `specPathExists()`. Either the sampler picks wrapper commands that don't have `path :=` patterns, or the comparison function fails on Steam's 3-segment paths. The paths exist on both sides — this is a scorer detection issue, not a CLI issue.
- **Root cause:** `evaluatePathValidity()` in scorecard.go:987 — need to trace why it finds 0 matches when the CLI clearly has spec-derived paths. Likely: the sampler picks 10 random files and may land on wrapper/transcendence commands (player.go, profile.go, etc.) which construct paths differently than the generated commands.
- **Frequency:** Any API where Claude adds wrapper commands — the sampler dilutes the pool.
- **Recommendation: Fix the scorer.** Either increase the sample size, or weight the sample toward generated command files (which always have `path :=` patterns), or scan all files instead of 10.
- **Impact:** 10 points. This alone accounts for a third of the gap to a perfect score.

### 2. Dead flag false positives for method receiver pattern (Scorer bug — refinement of PR #100 fix)

- **Scorer correct?** No. The 3 remaining dead flags (`noCache`, `rateLimit`, `timeout`) are used in `root.go` via the method receiver `f`:
  - `c.NoCache = f.noCache` in `func (f *rootFlags) newClient()`
  - `client.New(cfg, f.timeout, f.rateLimit)` in same method
  - Dogfood searches for `flags.<name>` but the method receiver is `f`, not `flags`. PR #100 fixed the root.go skip but only catches `flags.xxx` patterns, not `f.xxx` patterns.
- **Root cause:** `checkDeadFlags()` in dogfood.go:365 — the fix from PR #100 includes root.go but only matches `flags.<name>`. The rootFlags struct is accessed as `f` in methods and `flags` in Execute(). Need to match any `\.\<name\>` access on the rootFlags struct, not just `flags.xxx`.
- **Frequency:** Every API.
- **Recommendation: Fix the scorer.** Extend the dead-flag search to also match `f.<name>` and `\.\<name\>` patterns in root.go, recognizing the method receiver.
- **Impact:** ~1 point (dead code 4/5 → 5/5), but 3 false warnings per CLI erode trust.

### 3. Dead function false positives for transitive callers (Scorer bug)

- **Scorer correct?** No. All 6 remaining dead functions (`colorEnabled`, `compactListFields`, `compactObjectFields`, `levenshteinDistance`, `printCSV`, `rateLimitErr`) are called by other helper functions. Dogfood's `checkDeadFunctions()` (dogfood.go:407) searches for `\bfuncName\s*\(` in ALL files except `helpers.go` — but these functions are called FROM helpers.go by other helpers. The detection excludes the file where the definitions AND the calls live.
- **Root cause:** `checkDeadFunctions()` in dogfood.go:407 — skips `helpers.go` to avoid matching definitions, but also skips legitimate internal call chains within helpers.go. `colorEnabled` is called by `bold()`, `green()`, `red()`, `yellow()` — all in helpers.go.
- **Frequency:** Every API. The generator always emits helper chains.
- **Recommendation: Fix the scorer.** When scanning helpers.go for function usage, exclude the function's own definition line but include calls from other functions in the same file.
- **Impact:** 0-1 point directly, but 6 false warnings per CLI.

### 4. Type fidelity: IntVar for SteamIDs (Generator bug — scorer is correct)

- **Scorer correct?** Yes. The generator emits `IntVar` for `steamid` and `appid` parameters despite these being IDs that should be `StringVar`. SteamID64 is a 17-digit number that overflows int64. `appid` is also better as string for consistency and to avoid int-zero confusion.
- **Root cause:** `internal/generator/templates/command_endpoint.go.tmpl` — the `goType` template function maps OpenAPI `integer` type to Go `int`, which generates `IntVar`. But ID fields should be strings regardless of their OpenAPI type.
- **Frequency:** Any API where IDs are declared as integers in the spec. Very common — many specs declare IDs as `integer` even when they should be strings.
- **Recommendation: Fix the generator.** In the `goType` template function or the command template, if a parameter name contains "id" (case-insensitive), override the type to `string` regardless of the spec declaration.
- **Impact:** 1-2 points on type_fidelity.

### 5. Type fidelity: 300 dead code marker imports (Generator bug — scorer is correct)

- **Scorer correct?** Yes. The generator emits `var _ = strings.ReplaceAll` and `var _ = fmt.Sprintf` in every command file to prevent import errors. The scorer's type_fidelity dimension checks for these as dead code markers and deducts 1 point. There are 300 of them in the Steam CLI.
- **Root cause:** `command_endpoint.go.tmpl` lines 16-20 — unconditionally emits `var _ = strings.ReplaceAll // ensure import` and similar lines. These are a code smell even if technically harmless.
- **Frequency:** Every API. Every generated command file has these.
- **Recommendation: Fix the generator.** Remove the dummy import markers. Instead, use proper import management — only import what's actually used in each generated command file. This requires the template to know which imports each command needs.
- **Impact:** 1 point on type_fidelity.

### 6. Insight 6/10 — scorer uses filename prefixes as primary detection (Scorer design flaw + real gap)

- **Scorer correct?** Partially. The CLI has real insight commands (`playtime.go` computes playtime distributions, `completionist.go` tracks cross-game achievement rates, `rare.go` finds rarest achievements) — but the scorer doesn't detect them because its primary method is **filename prefix matching** against a hardcoded list: `health`, `stats`, `trends`, `patterns`, `forecast`, `stale`, `analytics`, etc. (scorecard.go:821-823). A command named `playtime.go` that does real analytics doesn't count, but an empty file named `trends.go` would.
  
  The secondary detection method (store + aggregation pattern: `COUNT(`, `GROUP BY`, `AVG(`) is more behavioral but only catches commands that run raw SQL aggregations — not commands that aggregate in Go code (which is what Claude naturally writes).
  
  **The scorer's detection method is the primary problem.** It should detect insight behavior, not insight filenames. Better approaches:
  - Detect commands that make 2+ API calls and compute derived results (not just pass-through)
  - Detect commands that produce summary/aggregate output (count, percentage, comparison, ranking)
  - Detect commands that join data from multiple sources (the actual definition of "insight")
  - Use the store + aggregation pattern but expand what counts as aggregation (Go-level `sort.Slice`, `len()` comparisons, percentage calculations — not just SQL keywords)
  
- **Root cause:** `scoreInsight()` in scorecard.go:814-877 — detection relies on filename heuristics rather than behavioral analysis. The hardcoded prefix list creates a naming game rather than measuring actual insight capability.
- **Frequency:** Every API. Claude builds genuinely useful analytics commands but names them for the domain (`playtime`, `completionist`, `rare`) not for the scorer (`trends`, `stats`, `health`).
- **Recommendation: Fix the scorer's detection method.** The insight dimension should measure whether commands produce derived/aggregated output, not whether filenames match a prefix list. Short-term: expand the prefix list to include common domain terms. Long-term: detect behavioral patterns (2+ data sources combined, summary output shape, ranking/comparison logic).
- **There is also a real gap:** The generator could emit 1-2 generic insight templates that work for any API — `stale.go` (find inactive records by timestamp) and `health.go` (API status + data freshness summary) are domain-agnostic. This narrows the gap from both sides: smarter scorer + more generated templates.
- **Impact:** 4 points.

### 7. Workflows 8/10 — scorer uses same filename-prefix approach (Scorer design flaw + real gap)

- **Scorer correct?** Partially. Same issue as insight. The scorer's `scoreWorkflows()` (scorecard.go:728-812) primarily matches filename prefixes (`stale`, `orphan`, `triage`, `sync`, `export`, `search`, etc.) and secondarily checks for multi-API-call commands (2+ `c.Get`/`c.Post` calls in one RunE). The Steam CLI has compound commands like `profile.go` (5 API calls in one command), `compare.go` (2 player lookups + library comparison), `friends.go` (friend list + batch profile resolution) — all genuine workflows. But `profile.go` doesn't match any prefix, and the multi-API detection works but the threshold is high (7+ for 10/10).
  
  **Same fix direction as insight:** Detect workflow behavior (multi-step operations, data aggregation, store usage) rather than filename patterns. A command that makes 3 API calls and combines results IS a workflow regardless of what the file is named.
  
- **Root cause:** `scoreWorkflows()` in scorecard.go:728-812 — same filename-heuristic problem as insight.
- **Frequency:** Every API.
- **Recommendation: Fix the scorer's detection method** alongside insight. Both dimensions should detect behavior, not filenames. The multi-API pattern detection (2+ API calls) already exists and is the right approach — it just needs to be the primary detection method with a reasonable threshold, not a secondary fallback behind filename matching.
- **Impact:** 2 points.

### 8. Data pipeline 7/10 — generic Search (Real gap — scorer is correct)

- **Scorer correct?** Yes. The CLI has domain-specific Upsert methods (UpsertIpublishedFileService, etc.) but uses generic `db.Search()` instead of domain-specific `db.SearchPlayers()`, `db.SearchGames()`. The scorer gives +3 for domain-specific Search and the CLI gets 0 for this.
- **Root cause:** The generator emits domain-specific Upsert but not domain-specific Search. The wrapper commands use the generic store methods.
- **Frequency:** Every API. The store template doesn't generate per-entity search methods.
- **Recommendation: Fix the generator.** Emit domain-specific Search methods alongside Upsert methods in the store template.
- **Impact:** 3 points.

### 9. Sync correctness 7/10 — missing path parameters (Real gap — scorer is correct)

- **Scorer correct?** Yes. The scorer gives +3 bonus for `/{` patterns in sync.go (path parameters enabling per-resource sync). Steam's sync.go has 0 path params because the syncable resources don't use parameterized paths. This is inherent to Steam's API structure (list endpoints like `/ISteamApps/GetAppList/v2/` don't have path params).
- **Frequency:** API subclass — APIs without path params in list endpoints.
- **Recommendation:** Partially scorer design issue — the +3 bonus for path params penalizes APIs that don't use them. But also a real gap: the sync path resolution from the earlier retro (finding #6, WU-5) would help here.
- **Impact:** 3 points.

### 10. README 7/10 — missing Cookbook section (Real gap — scorer is correct)

- **Scorer correct?** Yes. The README has Quick Start, Agent Usage, Doctor, Troubleshooting (4/4 sections = 4pts). But no "Cookbook" or "Recipes" section with 3+ code examples (0/2 from cookbook). Plus the README likely has placeholder values losing 0-2 from Quick Start quality.
- **Root cause:** The README template (from generator or Claude's polish) doesn't include a Cookbook section.
- **Frequency:** Every API.
- **Recommendation:** Add a Cookbook/Recipes section with 3+ real usage examples to every CLI README. The skill or the README template should include this.
- **Impact:** 2-3 points.

### 11. Verify: 21 wrapper commands score 1/3 (Scorer partially right)

- **Scorer correct?** Partially. The 21 commands pass --help but fail dry-run and exec because they need a `STEAM_API_KEY` to make API calls. Without the key, the dry-run shows the request shape but `steamAPIKey()` returns an auth error. The scorer is correct that these commands can't complete without auth. But the verify tool could detect that auth is the blocker and not count it as a failure — it could try with `STEAM_API_KEY` if available.
- **Frequency:** Any API requiring auth where the key isn't in the environment during verify.
- **Recommendation:** Enhance verify to pass known env vars (from the CLI's config template or manifest) during testing. Or add a `--env-file` flag to verify.
- **Impact:** 5% verify (75% → 80%+ if auth commands pass).

## Prioritized Improvements

### Fix the Scorer
| # | Scorer | Bug | Impact | Fix target |
|---|--------|-----|--------|------------|
| 1 | Scorecard | `evaluatePathValidity` samples too few files or hits wrappers instead of generated commands | 10 points | scorecard.go:987 |
| 6+7 | Scorecard | `scoreInsight` and `scoreWorkflows` use filename prefixes as primary detection instead of behavioral analysis | 6 points combined | scorecard.go:814-877, scorecard.go:728-812 |
| 2 | Dogfood | Dead-flag search doesn't match method receiver `f.xxx` pattern | ~1 point + 3 false warnings | dogfood.go:365 |
| 3 | Dogfood | Dead-function search skips internal calls within helpers.go | 0-1 point + 6 false warnings | dogfood.go:407 |

### Do Now
| # | Fix | Component | Frequency | Complexity |
|---|-----|-----------|-----------|------------|
| 5 | Remove dead code marker imports (`var _ = strings.ReplaceAll`) | `command_endpoint.go.tmpl` | Every API | Medium |
| 10 | Add Cookbook/Recipes section to README template | `readme.md.tmpl` or skill instruction | Every API | Small |
| 4 | Override IntVar to StringVar for ID parameters | `generator.go` goType function | Most APIs | Small |

### Do Next
| # | Fix | Component | Frequency | Complexity |
|---|-----|-----------|-----------|------------|
| 8 | Emit domain-specific Search methods alongside Upsert | Store template | Every API | Medium |
| 11 | Pass auth env vars to verify during testing | verify runtime | APIs with auth | Medium |

### Skip
| # | Fix | Why |
|---|-----|-----|
| 9 | Sync path params bonus | Inherent to APIs without path params in list endpoints. The +3 bonus is a scorecard design choice, not a bug. |

## Work Units

### WU-1: Fix path_validity scorer sampling (finding #1)
- **Goal:** Path validity correctly scores CLIs that have spec-derived paths in generated commands
- **Target files:** `internal/pipeline/scorecard.go` (~line 987, `evaluatePathValidity`)
- **Acceptance criteria:**
  - Generate from Steam spec → path_validity > 0/10
  - Generate from Stripe spec → path_validity still works
- **Scope boundary:** Only changes the sampling/matching in path_validity. Other scorecard dimensions unchanged.
- **Complexity:** Small (1 file, adjust sampling strategy)

### WU-2: Fix dogfood method receiver pattern (finding #2)
- **Goal:** Dead-flag detection matches `f.<name>` and `flags.<name>` patterns
- **Target files:** `internal/pipeline/dogfood.go` (~line 365, `checkDeadFlags`)
- **Acceptance criteria:**
  - Run dogfood → `noCache`, `rateLimit`, `timeout` NOT reported dead
  - Genuinely unused flag still caught
- **Scope boundary:** Only extends the search pattern. Doesn't change dead-function detection.
- **Complexity:** Small (1 file)

### WU-3: Fix dogfood internal call chain detection (finding #3)
- **Goal:** Dead-function detection recognizes calls within helpers.go from other functions
- **Target files:** `internal/pipeline/dogfood.go` (~line 407, `checkDeadFunctions`)
- **Acceptance criteria:**
  - Run dogfood → `colorEnabled`, `compactListFields`, etc. NOT reported dead
  - Truly unused function still caught
- **Scope boundary:** Only changes how helpers.go is scanned for usage.
- **Complexity:** Small (1 file)

### WU-4: Generator template improvements (findings #4, #5, #10)
- **Goal:** ID params use StringVar, remove dead import markers, add README Cookbook
- **Target files:**
  - `internal/generator/generator.go` (goType for ID params)
  - `internal/generator/templates/command_endpoint.go.tmpl` (remove `var _ = strings.ReplaceAll`)
  - `internal/generator/templates/readme.md.tmpl` (add Cookbook section)
- **Acceptance criteria:**
  - Generated commands with `steamid` param use `StringVar`, not `IntVar`
  - No `var _ = strings.ReplaceAll` in generated files
  - README has Cookbook section with 3+ examples
- **Complexity:** Medium (3 files)

### WU-5: Rewrite insight and workflow scoring to detect behavior, not filenames (findings #6, #7)
- **Goal:** `scoreInsight()` and `scoreWorkflows()` detect actual insight/workflow behavior instead of filename prefixes
- **Target files:** `internal/pipeline/scorecard.go` (~lines 728-877, `scoreInsight` and `scoreWorkflows`)
- **Acceptance criteria:**
  - A command named `playtime.go` that fetches games, computes statistics, and produces aggregate output IS detected as an insight command
  - A command named `profile.go` that makes 5 API calls and combines results IS detected as a workflow
  - A command named `trends.go` that does nothing useful is NOT detected as an insight command (behavior matters, not filename)
  - The Steam CLI's existing commands (playtime, completionist, rare, profile, compare, friends) score appropriately
  - Negative test: A trivial pass-through command is not falsely counted as insight/workflow
- **Approach:**
  - **Insight detection:** Count commands that (a) use the store OR make 2+ API calls, AND (b) produce derived output — detected via: percentage calculations (`* 100`, `/ total`), sorting/ranking (`sort.Slice`), cross-entity joins (2+ different data types combined), summary fields (`total`, `average`, `count`, `rate`). Keep the existing prefix list as a supplementary signal, not the primary one.
  - **Workflow detection:** Count commands that (a) make 2+ distinct API calls (existing secondary detection), OR (b) use the store AND produce non-pass-through output. Lower the threshold or make the multi-API detection primary instead of secondary.
- **Scope boundary:** Only changes insight and workflow scoring logic. Other scorecard dimensions unchanged.
- **Complexity:** Medium (1 file, but needs careful regex/pattern design to avoid false positives)

### WU-6: Emit domain-specific Search methods (finding #8)
- **Goal:** Store template generates per-entity Search methods alongside Upsert
- **Target files:**
  - `internal/generator/templates/store.go.tmpl`
  - `internal/generator/generator.go` (if data model changes needed)
- **Acceptance criteria:**
  - Generated store has `SearchPlayers()`, `SearchGames()` etc.
  - Scorecard data_pipeline score increases from 7 to 10
- **Complexity:** Medium (2 files, needs store template restructuring)

## Anti-patterns

- **Gaming the scorer instead of fixing the scorer:** We added 6 insight commands in polish and named them for the domain (`playtime`, `completionist`, `rare`) instead of for the scorer's prefix list (`trends`, `stats`, `health`). The temptation was to rename files to match — but that's working around a broken detection method. The right fix is making the scorer detect behavior (which our commands genuinely have), not making our code conform to a filename heuristic.
- **Treating all score gaps as CLI quality gaps:** Not every score deduction means the CLI is bad. Some deductions mean the scorer is measuring the wrong thing. The retro's scorer audit step exists to distinguish the two — skipping it leads to wasted effort on the wrong fix target.

## What the Machine Got Right

- **PR #100 scorer fixes validated:** Verify jumped from 44% → 80% for this API. The camelToKebab fix correctly resolved 25 false failures. The dogfood root.go skip fix resolved 3 of 6 false dead-flag warnings.
- **Template improvements worked:** Root description defaults to CLI-appropriate text. Help-guard pattern prevents verify failures on promoted commands. Dry-run cache poisoning is fixed.
- **Generated commands have correct spec paths:** The CLI's 150+ path assignments all come from the spec. The path_validity 0/10 is a scorer sampling issue, not a CLI issue.
