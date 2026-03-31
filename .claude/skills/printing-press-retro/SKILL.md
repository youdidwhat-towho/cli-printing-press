---
name: printing-press-retro
description: >
  Run a retrospective after generating a CLI with the printing press. Identifies
  systemic improvements to the machine — generator templates, Go binary, skill
  instructions, catalog — not patches to the specific CLI. Covers bugs, but also
  recurring friction (like dead code), features that had to be built manually,
  and optimizations discovered during the session. Use after any /printing-press
  run. Trigger phrases: "retro", "retrospective", "what went wrong", "improve
  the press", "post-mortem", "lessons learned", "what can we improve".
allowed-tools:
  - Bash
  - Read
  - Glob
  - Grep
  - Write
  - Agent
  - AskUserQuestion
---

# /printing-press-retro

Analyze a printing press session to find ways to make the *machine* better. Not
fixes to the CLI that was just printed — improvements to the generator, binary,
skill, and catalog so the *next* CLI comes out stronger with less manual effort.

This goes beyond bugs. The most valuable findings are often the work that *succeeded
but shouldn't have been necessary* — features you built by hand that the generator
should have emitted, friction that recurs on every generation, and optimizations you
discovered that should become defaults.

## When to run

Run in the same conversation where the CLI was generated (post-shipcheck). The retro
needs the full conversation history — every error, retry, manual edit, and discovery.

If running in a fresh conversation, point it at the manuscripts directory, but know
you'll miss the in-conversation debugging context.

## Setup

```bash
PRESS_HOME="$HOME/printing-press"
PRESS_MANUSCRIPTS="$PRESS_HOME/manuscripts"

# Find the most recent run across all APIs
LATEST_RUN=$(find "$PRESS_MANUSCRIPTS" -name "*shipcheck*" -type f -exec stat -f "%m %N" {} \; | sort -rn | head -1 | awk '{print $2}')

if [ -z "$LATEST_RUN" ]; then
  echo "No shipcheck proofs found. Run /printing-press first."
  exit 1
fi

API_NAME=$(echo "$LATEST_RUN" | sed "s|$PRESS_MANUSCRIPTS/||" | cut -d'/' -f1)
RUN_ID=$(echo "$LATEST_RUN" | sed "s|$PRESS_MANUSCRIPTS/||" | cut -d'/' -f2)
RUN_DIR="$PRESS_MANUSCRIPTS/$API_NAME/$RUN_ID"

echo "Retro for: $API_NAME (run $RUN_ID)"
echo "Manuscripts: $RUN_DIR"
```

If the user passed an API name as an argument, use that instead of auto-detecting.

## Phase 1: Gather evidence

Read all artifacts from the run:

1. **Research brief** — `$RUN_DIR/research/*brief*`
2. **Absorb manifest** — `$RUN_DIR/research/*absorb*`
3. **Shipcheck proof** — `$RUN_DIR/proofs/*shipcheck*`
4. **Build log** — `$RUN_DIR/proofs/*build-log*` (if exists)
5. **Live smoke log** — `$RUN_DIR/proofs/*live-smoke*` (if exists)
6. **The generated CLI** — `$PRESS_HOME/library/<api>-pp-cli/`

Also gather the scorecard, verify pass rate, and dogfood report (from the shipcheck
proof or by re-running the tools).

## Phase 2: Mine the session

Scan the full conversation history for five categories of signal. Every finding
becomes a row in Phase 3 — don't filter yet, just collect.

### 2a. Errors and retries

Any time a command failed and was re-run, a build broke, or the generator produced
code that didn't compile. What broke and what fixed it?

### 2b. Manual code edits

Every hand-edit to generated code is a signal. Each one means the generator *should
have* gotten it right but didn't. These are the highest-value findings because they
point directly at template gaps.

Examples from real sessions:
- Rewriting the root command `Short:` description from API-speak to user-speak
- Adding top-level commands to wrap deeply-nested generated commands
- Fixing `serviceForPath` routing for proxy-envelope APIs
- Rewriting the sync command for offset-based pagination
- Adding entity-specific store tables the generator didn't create

### 2c. Features built from scratch

Features in the absorb manifest or transcendence list that had to be written entirely
by hand during Phase 3. The generator produced no scaffolding for them. Ask: is this
a feature class the generator could reasonably emit, or is it genuinely custom?

For example: if every CLI needs a `trending` command that queries local SQLite, maybe
the generator should emit a trending template when it detects time-series metrics in
the spec.

### 2d. Recurring friction

Work that happens on *every* generation, not just this one. The key question for each:
**is this inherent to the approach, or can the machine eliminate it?**

Examples:
- **Dead code** — The generator emits generic helpers (CSV, delete-classify, etc.) and
  some are never called. Is the fix to stop emitting them (risk: some CLIs need them)?
  Or to add a post-generation dead-code sweep? Or to make the generator smarter about
  which helpers each API actually needs?
- **Default resource mismatch** — `defaultSyncResources()` always returns a placeholder.
  Could the generator derive the right resources from the spec's entity types?
- **DB path inconsistency** — Different generated commands use different default paths.
  Could the generator emit a single `defaultDBPath()` and reference it everywhere?

For each piece of friction, propose at least two possible fixes at different levels
(generator, binary post-processing, skill instruction) and assess which is most durable.

### 2e. Discovered optimizations

Improvements noticed during the session that weren't fixing a problem — they were
making something better. These might be UX ideas, performance improvements, new
command patterns, or output format improvements that emerged from actually using the
CLI.

Ask: could this optimization be detected automatically and applied by the generator?

## Phase 3: Classify findings

For each finding from Phase 2, answer these questions. Skip findings that only
affect this specific API and wouldn't recur.

### The Six Questions

**1. What happened?**
One sentence. Describe the symptom or the work that was done, not the fix.

**2. What category is this?**

| Category | Description | Example |
|----------|-------------|---------|
| **Bug** | Generated code is wrong | serviceForPath returns wrong service |
| **Template gap** | Generator has no template for a common pattern | No top-level command aliases |
| **Assumption mismatch** | Generator assumes X but API uses Y | Cursor pagination vs offset |
| **Recurring friction** | Happens every generation, might be inherent | Dead code cleanup |
| **Missing scaffolding** | Feature class the generator could emit but doesn't | Entity-specific store tables |
| **Default gap** | Generator emits a wrong or placeholder default | Sync resources list, DB path |
| **Discovered optimization** | Improvement found during use | Compact number formatting |
| **Skill instruction gap** | Skill told Claude wrong thing or missed a step | Phase ordering issue |
| **Tool limitation** | Verify/dogfood/scorecard missed or mis-reported | False positive dead code |

**3. Where in the machine does this originate?**

| Component | Path | Controls |
|-----------|------|----------|
| Generator templates | `internal/generator/` | Go code emitted for commands, store, client |
| Spec parser | `internal/spec/` | Internal YAML spec parsing |
| OpenAPI parser | `internal/openapi/` | OpenAPI 3.0+ parsing |
| Catalog | `catalog/` | API entries and metadata |
| Main skill | `skills/printing-press/SKILL.md` | Orchestration instructions |
| Verify/dogfood/scorecard | CLI commands | Quality checking tools |

**4. Blast radius and fallback cost — should the machine handle this?**

This is the most important question and the easiest to get wrong. The retro runs
right after a session with one API, and pattern-matching from a single example is
unreliable. A finding that felt universal during the Postman Explore session might
be specific to proxy-envelope APIs, or to sniffed specs, or to APIs with entity-type
enum params.

**Step A: Cross-API stress test.** Test across both API shapes and input methods
since the printing press handles all combinations:

API shapes:
- A standard REST API with clean resources (e.g., Stripe, GitHub)
- A non-standard or minimal API (proxy-envelope, RPC-style, etc.)

Input methods:
- OpenAPI spec (well-documented, machine-readable)
- Crowd-sniffed (derived from npm/GitHub SDKs, may have gaps)
- HAR-sniffed (derived from captured traffic)
- No spec (Claude researches and builds from scratch)

For each relevant combination, ask: "Would this exact problem occur? Would the
proposed fix help, be irrelevant, or actively hurt?"

**Step B: Estimate frequency.** Based on the stress test, assign a blast radius:

- **Every API** — occurs regardless of API shape or input method.
- **Most APIs** — affects common patterns. Name the triggering condition.
- **API subclass** — affects a specific pattern. Name the subclass precisely:
  proxy-envelope, GraphQL, sniffed-only, offset-paginated, etc.
- **This API only** — isolated quirk.

**Step C: Assess fallback cost.** This is what happens if the machine does NOT have
the fix. The cost is NOT measured in time — Claude does the implementation. The cost
is about **reliability and quality degradation**:

| Fallback | Cost | Why it's costly |
|----------|------|-----------------|
| **Claude rewrites from scratch** | High — error-prone, may produce inconsistent code | Claude might forget, get it wrong, or produce code that doesn't match the generator's patterns |
| **Claude makes targeted edits** | Medium — usually succeeds but adds friction | Claude generally catches it, but each manual edit is a chance for the CLI to diverge from machine quality |
| **Claude deletes/tweaks one thing** | Low — mechanical, reliable | Claude catches it consistently, fix is trivial and deterministic |
| **CLI ships broken** | Critical — user hits the bug at runtime | Defect reaches the user. No amount of Claude effort during generation prevents this if the machine emits wrong code |

The key question: **how reliably will Claude catch and fix this every time, across
every future API?** A "simple" edit that Claude forgets 30% of the time is actually
high cost because those 30% ship with the defect.

**Step D: Make the tradeoff.** The default is to **fix it in the machine**. The
retro exists to feed an improvement loop — if findings get punted to Skip by
default, the loop stalls and CLIs don't get better. Every finding left out of the
machine is a bet that Claude will catch it every time, and Claude won't.

The burden of proof is on *not* fixing, not on fixing.

**Skip** when the behavior is unlikely to occur with other APIs or services.

For each finding, ask: **"If I'm building CLIs for 50 different APIs — big
enterprise platforms, small indie services, legacy systems, modern startups — is
this something I'd plausibly encounter again, or is it truly this one vendor's
idiosyncratic choice?"**

Use your knowledge of the API landscape to make this judgment. You know how APIs
are built, what conventions exist across the industry, and what patterns different
vendors converge on independently. Trust that judgment rather than trying to apply
a mechanical rule.

Examples of findings that were correctly skipped:
- **Valve's `input_json` wrapping** — Steam's "Service" interfaces require all params
  encoded as `input_json={"steamid":"xxx"}`. Valve invented this for their internal
  service RPC layer. Across 50 APIs, you wouldn't see this convention again.

Examples of findings that LOOK isolated but should NOT be skipped:
- "This API uses API keys in query params instead of headers" — many older APIs
  do this. You'd see it again.
- "This API returns 200 for errors with an error field in the body" — common
  anti-pattern across many APIs.
- "This API has no pagination and returns everything in one call" — a pagination
  variant (none/single-page), not a quirk.

**Do** when you'd plausibly encounter the behavior again — even if this is the
first API to trigger it.

The printing press handles multiple input paths — OpenAPI specs, crowd-sniffed
specs, HAR-sniffed specs, and no-spec APIs. Findings can surface from any path.
Examples that cut across input methods:
- Offset pagination (vs cursor) — fundamental pagination approach, surfaces whether
  you have an OpenAPI spec or are sniffing traffic
- Response envelope wrapping — most APIs do this regardless of how the spec was
  obtained
- Incomplete parameter discovery — happens with crowd-sniff (SDK gaps), HAR sniff
  (limited traffic capture), and even OpenAPI specs (undocumented params)
- Auth pattern detection — needed whether auth info comes from an OpenAPI
  `securitySchemes`, an SDK constructor, or HAR request headers
- Resource naming mismatches — the generator's command structure vs user-friendly
  names is independent of spec source

With the diversity of APIs and input methods the printing press targets, **true
vendor quirks are rarer than they seem.** When in doubt, lean toward Do with a
guard rather than Skip.

When the finding applies to a subclass, include conditional logic so it doesn't
regress the simple case.

When the finding applies to an API subclass, the recommendation must include:
- **Condition:** When to activate (e.g., "spec has `x-proxy-routes`")
- **Guard:** When to skip (e.g., "standard REST APIs without proxy pattern")
- **Frequency estimate:** How common is this subclass? If it's >20% of APIs the
  printing press targets, the conditional logic is likely worth the complexity.

**5. Is this inherent or fixable?**
This question matters most for recurring friction. Some friction is structural — code
generation will always produce some unused code because templates are generic. But
"inherent" shouldn't be the default answer. Push hard on whether a smarter generator,
a post-processing step, or better spec analysis could eliminate the friction.

If inherent: propose the cheapest mitigation (e.g., "dogfood auto-deletes dead helpers
as a post-generation step").

If fixable: propose the fix at the right level.

**6. What is the durable fix?**
A concrete change to the machine. Prefer this hierarchy:

1. **Generator template fix** — Code is emitted correctly from the start. Zero manual work.
2. **Binary post-processing** — A printing-press command that auto-fixes after generation
   (like a `printing-press polish` that removes dead code and aligns paths).
3. **Skill instruction** — Tell Claude to do it during generation. Last resort because
   Claude might forget or get it wrong. Every instruction is a tax on every future run.

Describe what test would verify the fix: "Generate a CLI for an API with offset
pagination and verify sync terminates after fetching all pages."

## Phase 4: Prioritize

Group findings into two buckets using judgment, not a formula. No "backlog" —
backlog is where findings go to die. Either it's worth doing or it's not.

- **Do** — worth a machine fix. This is the default. Split into "Do now" (scoped
  cleanly, can implement immediately) and "Do next" (needs design work or careful
  guards — plan before implementing).
- **Skip** — unlikely to encounter again across other APIs. State why.

Do NOT use numerical scoring formulas. The inputs (frequency=3, fallback=4) are
ordinal gut-feels, and multiplying them produces fake precision that obscures
judgment. State the reasoning in words instead.

## Phase 5: Write the retro

```markdown
# Printing Press Retro: <API name>

## Session Stats
- API: <name>
- Spec source: <catalog/sniffed/docs/HAR>
- Scorecard: <before> -> <after> (if applicable)
- Verify pass rate: <X>%
- Fix loops: <N>
- Manual code edits: <N>
- Features built from scratch: <N>

## Findings

### 1. <Title> (<category>)
- **What happened:** ...
- **Root cause:** Component + what's specifically wrong
- **Cross-API check:** Would this recur across other APIs and input methods?
- **Frequency:** every API / most / subclass:<name> / this API only
- **Fallback if machine doesn't fix it:** What Claude has to do dynamically and how
  reliably Claude catches it (always / usually / sometimes / never). If "sometimes"
  or "never", the CLI ships with the defect — that's the real cost.
- **Worth a machine fix?** Default is yes. Only no if you'd be unlikely to encounter
  this across other APIs. State the reasoning in plain language.
- **Inherent or fixable:** ...
- **Durable fix:** Concrete machine change. If subclass-scoped, include:
  - Condition: when to activate
  - Guard: when to skip
  - Frequency estimate: how common is this subclass?
- **Test:** How to verify, including a negative test for APIs that should NOT be affected
- **Evidence:** Session moment that surfaced this

### 2. ...

## Prioritized Improvements

### Do Now
| # | Fix | Component | Frequency | Fallback Reliability | Complexity | Guards |
|---|-----|-----------|-----------|---------------------|------------|--------|

### Do Next (needs design/planning)
| # | Fix | Component | Frequency | Fallback Reliability | Complexity | Guards |
|---|-----|-----------|-----------|---------------------|------------|--------|

### Skip
| # | Fix | Why unlikely to recur |
|---|-----|----------------------|

## Work Units

### WU-1: <Title> (findings #N, #M)
- **Goal:** ...
- **Target files:** actual paths from Glob/Grep
- **Acceptance criteria:**
  - positive test: ...
  - negative test: ...
- **Scope boundary:** ...
- **Complexity:** small / medium / large

### WU-2: ...

## Anti-patterns

Patterns that looked right but led to problems. These should become warnings in
AGENTS.md or the relevant skill:
- ...

## What the Machine Got Right

Patterns to preserve and extend — things that worked well and should not be
accidentally degraded by future changes:
- ...
```

## Phase 5.5: Plannable work units

The retro's findings are analytical. To bridge to implementation planning (e.g.,
via `/compound-engineering:ce-plan`), group related findings into coherent work units that a planner
could pick up directly.

For each "Do now" or "Do next" group, produce a work unit block:

```markdown
## Work Units

### WU-1: <Title> (from findings #N, #M, ...)
- **Goal:** One sentence describing the outcome
- **Target files:** Specific file paths in the printing-press repo to modify
  (use Glob/Grep to resolve component names to actual files)
- **Acceptance criteria:** 2-3 concrete, testable scenarios:
  - "Generate from postman-explore spec → sync terminates without manual fix"
  - "Generate from Stripe spec → sync still uses cursor-based pagination (negative test)"
- **Scope boundary:** What this does NOT include
- **Dependencies:** Other work units that must complete first (if any)
- **Complexity:** small (1-2 files, straightforward) / medium (3-5 files, needs design) / large (new capability, multiple subsystems)
```

To resolve target files, actually look at the printing-press repo:

```bash
# Find generator template files
find <repo>/internal/generator -name "*.go" -o -name "*.tmpl" | head -20

# Find where sync code is generated
grep -rl "syncResource\|defaultSyncResources\|determinePaginationDefaults" <repo>/internal/
```

Group related findings into work units when they touch the same files or when
one fix enables another. For example:
- Response envelope unwrapping + pagination detection + sync resource derivation
  → "WU: Data layer generation pipeline"
- Entity-specific store tables + FTS index generation + typed columns
  → "WU: Schema-driven store generation"

A good work unit is scoped to a single implementable chunk with a clear definition
of done. If it touches more than ~5 files across multiple subsystems, split it.

## Phase 6: Save and present

### Save locations

Save the retro to two places:

1. **Manuscripts** (ephemeral, tied to the run):
   ```
   $PRESS_MANUSCRIPTS/<api>/<run-id>/proofs/<stamp>-retro-<api>-pp-cli.md
   ```

2. **Repo** (durable, checkable-into-git, readable by future sessions):
   ```bash
   REPO_ROOT="$(git rev-parse --show-toplevel 2>/dev/null || echo "$PWD")"
   RETRO_DIR="$REPO_ROOT/docs/retros"
   mkdir -p "$RETRO_DIR"

   # Filename: YYYY-MM-DD-<api>-retro.md
   RETRO_FILE="$RETRO_DIR/$(date +%Y-%m-%d)-<api>-retro.md"
   ```

   The repo copy is the canonical one. It accumulates over time so future retros
   can reference past findings ("this was also flagged in the notion retro on 2026-03-15").

### Present summary

Show the user: the "Do now" table and the work units.

### Offer to plan

Check whether `/compound-engineering:ce-plan` is available (the compound-engineering plugin is in
`.claude/settings.json` as a dependency, so it should be — but might not be for
standalone installs). Use `AskUserQuestion` to offer next steps:

> "Retro saved to `docs/retros/<date>-<api>-retro.md`. Found <N> findings across
> <M> work units. Want to plan implementation?"
>
> 1. **Plan "Do now" work units** — invoke `/compound-engineering:ce-plan` with the retro's Do now work
>    units as input
> 2. **Plan a specific work unit** — pick one WU to plan
> 3. **Done for now** — retro is saved, plan later

If the user picks option 1 or 2, invoke the `compound-engineering:ce:plan` skill
(if that name doesn't resolve, try `compound-engineering:ce-plan` as a fallback)
with a prompt like:

```
Create a plan to improve the printing-press CLI generation system in this repo.
We just generated a CLI for <API> and encountered systemic problems and
opportunities documented in the retro. The retro includes prioritized work units
with target files, acceptance criteria, and scope boundaries:
docs/retros/<date>-<api>-retro.md
[If option 2: Focus on work unit WU-<N>: <title>.]

Note: the retro is advisory, not declarative. During planning you may discover
valid reasons to adjust scope — combining work units, dropping items that turn out
to be impractical once you read the code, or adding items the retro missed. That's
expected. Use the retro as a starting point, not a spec.
```

If neither skill name resolves, fall back to:
- Tell the user the retro is saved and they can invoke it manually
- Print the prompt they'd use:
  `/compound-engineering:ce-plan Create a plan to improve the printing-press system given the retro at docs/retros/<file>`

## Rules

- The retro is about the machine, not the CLI. Do not propose fixes to the generated
  CLI.
- Do not add more phases, documents, or gates to the main skill. It's already long.
  Propose making existing phases smarter or the generator emit better defaults.
- Prefer automatic fixes (generator, binary) over instructional fixes (skill).
- For recurring friction, always answer "inherent or fixable?" honestly. Don't
  dismiss friction as inherent without considering alternatives.
- Be honest about what went well. Protecting good patterns is as important as
  fixing bad ones.
- **Bias toward fixing.** The retro feeds an improvement loop. If the loop is too
  conservative, CLIs don't get better. When in doubt, fix it — scope narrowly with
  conditional logic if needed. A guarded fix for a rare case is better than no fix
  at all. Future retros can widen the scope when the pattern recurs.
- **Look for broader patterns.** Before skipping a finding as API-specific, consider
  whether it's the first sighting of a behavior you'd encounter again. Offset
  pagination was first seen on one API — but it's clearly a pattern across many.
- When a fix only applies to a subclass of APIs, the recommendation must include the
  condition (when to activate) AND the guard (when to skip). A generator change
  without a guard is a blanket change, and blanket changes break simple cases.
- **No time estimates.** Claude does the implementation. Hours/days/weeks are
  meaningless. Use complexity sizing (small/medium/large) based on number of files
  touched and design work needed.
- Be thorough. The retro document is a reference for future planning — include
  enough detail that someone reading it months later can understand the finding,
  the tradeoff reasoning, and the proposed fix without needing the original
  conversation.
