# Printing Press Retro: Steam Web API (Run 3)

## Session Stats
- API: Steam Web API
- Spec source: Zuplo/Steam-OpenAPI (158 operations, OpenAPI 3.0, reused from Run 1)
- Scorecard: **85/100 Grade A** (up from 68 → 70 → 84)
- Verify pass rate: 75% (61/81 passed, 0 critical)
- Machine improvements applied: PRs #100, #101, #102
- Manual code edits: 1 (root description rewrite, STEAM_API_KEY env var)
- Features built from scratch: 22 (10 wrapper + 6 transcendence + 6 insight)

## Journey: 68 → 85 in 3 runs

| Run | Score | Grade | What changed |
|-----|-------|-------|-------------|
| Run 1 | 68 | B | Baseline generation + polish |
| Run 2 | 84 | A | PR #100 scorer fixes (verify naming, dogfood dead-flag, dry-run cache) |
| Run 3 | 85 | A | PR #101 behavioral detection + PR #102 template improvements |

**17 points recovered through machine improvements.** The CLI pattern is the same; the machine got smarter.

## Findings

### 1. Data pipeline 7/10 — generic Search methods (Generator gap — scorer is correct)

- **Scorer correct?** Yes. The scorer gives +3 for domain-specific Search methods like `SearchPlayers()`, `SearchGames()`. The generator emits domain-specific Upsert methods (e.g., `UpsertIsteamUserStats`) but only generic `Search()`. This is a real generator gap — the store template doesn't emit per-entity search methods.
- **Root cause:** `store.go.tmpl` generates Upsert per domain table but Search is only generic.
- **Frequency:** Every API.
- **Recommendation: Fix the generator.** Emit domain-specific Search methods alongside Upsert in the store template.
- **Impact:** 3 points.

### 2. Sync correctness 7/10 — missing path parameters (Partially inherent, partially fixable)

- **Scorer correct?** Yes. The +3 bonus for `/{` path params in sync.go rewards APIs with parameterized list endpoints (like `/users/{team_id}/members`). Steam's list endpoints (`/ISteamApps/GetAppList/v2/`) don't have path params — this is inherent to Steam's API structure.
- **However:** The sync path resolution issue from Run 1 retro (profiler stores resource names, not endpoint paths) is still unfixed. Fixing that wouldn't change the path_params score for Steam, but would fix sync functionality for non-REST APIs generally.
- **Frequency:** API subclass — APIs without path params in list endpoints. The path resolution fix affects non-REST APIs broadly.
- **Recommendation:** Two separate items:
  1. The +3 sync bonus for path params is a scorer design choice — not a bug. Skip for Steam.
  2. The sync path resolution (profiler stores actual endpoint paths) should still be fixed for non-REST APIs. This is WU-5 from the Run 1 retro.
- **Impact:** 3 points (but 0 recoverable for Steam specifically).

### 3. Auth 8/10 — spec lacks securitySchemes (Partially scorer, partially generator)

- **Scorer correct?** Partially. The Zuplo Steam spec has no `securitySchemes` section — auth is only expressed as a `key` query parameter on 47/158 operations. The scorer can't award auth points because there's nothing to compare against. The generator correctly saw no auth scheme.
- **Root cause:** Two issues: (a) spec quality — no securitySchemes declaration, (b) generator doesn't infer query-param auth from parameter names.
- **Recommendation:** Generator enhancement: if >30% of operations have a `key` or `api_key` query param, infer query-param auth. This was finding #5 from Run 1 retro — still unfixed.
- **Impact:** 2 points.

### 4. Type fidelity 3/5 — improved but not perfect (Both generator and scorer)

- **Scorer correct?** Yes for the remaining 2 points. PR #102 fixed ID params (IntVar → StringVar), gaining 1 point. The remaining deductions: (a) flag description quality — some generated commands have short descriptions (<5 words avg), (b) required flag count may be low for some commands.
- **Frequency:** Every API.
- **Recommendation:** Generator template: ensure flag descriptions in the template include the parameter's spec description, not just its name. The `oneline .Description` is already used — the issue is specs with terse descriptions.
- **Impact:** 2 points (hard to improve without better spec content).

### 5. Dead code 4/5 — 1 remaining dead function (Investigate)

- **Scorer correct?** Need to verify. Dogfood reports 1 dead function. This could be a real dead function added by Claude during wrapper command building, or another false positive.
- **Recommendation:** Check which function is reported dead. If real, remove it. If false positive, it's another dogfood detection gap.
- **Impact:** 1 point.

### 6. Verify 75% — 20 commands score 1/3 (Known limitation)

- **Scorer correct?** Partially. The 20 commands pass --help but fail dry-run and exec because they need `STEAM_API_KEY` in the environment during verify. Verify doesn't pass auth env vars during testing.
- **Recommendation:** Enhance verify to read auth env var names from the CLI's config template and pass them during testing. This was finding #11 from Run 2 retro — still unfixed.
- **Impact:** 5% verify improvement (75% → ~95%).

## Prioritized Improvements

### Do Now
| # | Fix | Component | Impact | Complexity |
|---|-----|-----------|--------|------------|
| 5 | Remove 1 dead function | Generated CLI | 1 point | Trivial |

### Do Next (from prior retros, still unfixed)
| # | Fix | Component | Impact | Complexity |
|---|-----|-----------|--------|------------|
| 1 | Emit domain-specific Search methods alongside Upsert | `store.go.tmpl` | 3 points | Medium |
| 3 | Infer query-param auth from parameter names | OpenAPI parser | 2 points | Medium |
| 6 | Pass auth env vars to verify during testing | `verify` runtime | 5% verify | Medium |
| 2 | Sync path resolution for non-REST APIs | Profiler + sync template | 0 for Steam, helps other APIs | Medium |

### Skip
| # | Fix | Why |
|---|-----|-----|
| 2 (partial) | Sync path params bonus | Inherent to Steam's API — no path params in list endpoints |
| 4 | Flag description quality | Depends on spec content quality, not generator |

## What the Machine Got Right

**The retro→plan→implement loop works.** Three iterations of retro → identify scorer bugs vs real gaps → fix the right thing → regenerate produced a 17-point improvement (68→85) with zero changes to the retro/build process itself. The improvement compounds: each fix helps every future CLI, not just Steam.

**Specific wins from machine improvements:**
- **Path validity 0→10** (PR #101): scanning all files instead of sampling 10 eliminated the wrapper-command bias
- **Insight 4→9** (PR #101): behavioral detection found `completionist.go`, `playtime.go`, `rare.go` that filename-prefix detection missed
- **Workflows 8→10** (PR #101): counting total API calls instead of unique HTTP methods correctly identified `profile.go` (5 c.Get calls)
- **Dead code 4→5→4** (PR #100, #101): dead-flag false positives eliminated; 1 real dead function remains from Claude's build phase
- **Type fidelity 2→3** (PR #102): ID params use StringVar; dead import markers removed
- **README 7→9** (PR #102): cookbook section with 5 workflow examples

**The scorer validity lens from the retro skill paid off.** Without it, we would have spent effort adding more insight commands (gaming the filename prefix list) instead of fixing the scorer to detect behavior. The first retro correctly identified path_validity 0/10 as a scorer bug worth 10 points — more than all the generator improvements combined.

## Anti-patterns to Avoid

- **Diminishing returns on scorecard optimization.** We're at 85/100. The remaining 15 points include 3 from sync (inherent to Steam), 2 from auth (spec quality), 2 from type fidelity (spec quality). The practical ceiling for this API+spec combination is ~90. Further machine improvements should be validated against a DIFFERENT API to confirm they generalize, not tuned further on Steam.
