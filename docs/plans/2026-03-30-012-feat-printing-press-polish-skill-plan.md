---
title: "feat: /printing-press-polish standalone skill"
type: feat
status: completed
date: 2026-03-30
origin: docs/brainstorms/2026-03-30-printing-press-polish-skill-requirements.md
---

# feat: /printing-press-polish standalone skill

## Overview

Create a standalone `/printing-press-polish` skill that automatically fixes a generated CLI to pass verify, removes dead code, cleans up descriptions and README, and offers to publish. Extract and replace the inline "Emboss Mode" from the main printing-press skill. Always suggest after shipcheck.

## Problem Frame

Generated CLIs compile and have working API commands, but consistently fail verify (52% for Redfin) due to mechanical issues the generator can't prevent: cobra Args constraints that block no-arg testing, dead helper functions, placeholder descriptions, and generic READMEs. This manual fix work should be automated into a skill that runs diagnostics, fixes everything, reports the delta, and offers publish. (see origin: `docs/brainstorms/2026-03-30-printing-press-polish-skill-requirements.md`)

## Requirements Trace

- R1. Standalone skill at `skills/printing-press-polish/SKILL.md`
- R2. Invocable as `/printing-press-polish <cli-name-or-path>`
- R3-R4. Name resolution with fuzzy matching and user selection
- R5-R7. Diagnostics: dogfood + verify + scorecard + go vet, parsed into categorized findings
- R8-R11. Fix: autonomous fixes in priority order, rebuild, re-diagnose, report delta
- R12-R13. Publish offer after showing delta
- R14-R16. Integration: suggest after shipcheck, remove emboss from main skill

## Scope Boundaries

- Does not add features to the CLI (that's the absorb manifest)
- Does not improve the generator (that's /printing-press-retro)
- Does not re-run research or generation
- Does not handle broken specs needing re-generation

## Context & Research

### Relevant Code and Patterns

- `~/.claude/skills/printing-press-retro/SKILL.md` - Pattern for standalone skill structure (frontmatter, setup, phases)
- `skills/printing-press/SKILL.md:74-97` - Current emboss mode to extract and replace
- `skills/printing-press/SKILL.md:1520-1570` - Phase 6 publish flow to extend with polish option
- `skills/printing-press/SKILL.md:84-86` - Name resolution pattern to reuse
- `skills/printing-press-publish/` - Publish skill invoked at the end

### Institutional Learnings

- Scorecard accuracy: dead code detection uses `Count >= 2` to avoid matching the function's own definition (see `docs/solutions/logic-errors/scorecard-accuracy-broadened-pattern-matching-2026-03-27.md`)
- Validation must not mutate source directory (see `docs/solutions/best-practices/validation-must-not-mutate-source-directory-2026-03-29.md`) - relevant because polish DOES mutate; it should be explicit about this

## Key Technical Decisions

- **Skill file, not Go code**: This is a SKILL.md (LLM instructions) like the retro skill, not a new Go command in the binary. The LLM reads diagnostic output and applies fixes using standard tools (Edit, Write, Bash).
- **Diagnostics before fixes**: Run all diagnostic tools first to establish baseline. Don't fix piecemeal.
- **Priority-ordered fix categories**: Verify failures first (biggest impact), then dead code, then cosmetic. Each category has a known fix strategy from the Redfin session.
- **Replace emboss, don't coexist**: The main skill's emboss section becomes a one-line reference to `/printing-press-polish`. No dual maintenance.
- **Add `go vet` to diagnostics**: Cheap, catches things dogfood misses (unused variables, unreachable code). Run alongside the three existing tools.

## Open Questions

### Resolved During Planning

- **Fix strategy for verify failures?** Two patterns cover ~95%: (1) Remove cobra `Args:` constraints, add `if len(args) == 0 { return cmd.Help() }` in RunE. (2) Add nil-data guards after API calls in dry-run mode. The skill instructs the LLM to apply these patterns systematically.
- **How to remove emboss from SKILL.md?** Replace the "Emboss Mode" section (lines 74-97) with a reference: "For second-pass improvements, use `/printing-press-polish <cli-name>`." Update Phase 6 to add polish as an option in the AskUserQuestion.

### Deferred to Implementation

- **Edge cases in name resolution**: The current emboss name resolution logic handles fuzzy matching. Implementation should replicate this exactly from the existing code.
- **Fix strategies for rare verify failure types**: The Redfin session established the common patterns. New patterns may emerge with other APIs; the skill should have a fallback "read the error, fix it" instruction for unknown failure types.

## Implementation Units

- [ ] **Unit 1: Create the printing-press-polish skill file**

  **Goal:** Write the complete SKILL.md with frontmatter, setup, diagnostics, fix, and publish phases.

  **Requirements:** R1, R2, R3, R4, R5, R6, R7, R8, R9, R10, R11, R12, R13

  **Dependencies:** None

  **Files:**
  - Create: `skills/printing-press-polish/SKILL.md`

  **Approach:**
  - Follow the retro skill's frontmatter pattern: name, description, trigger phrases, allowed-tools
  - Trigger phrases: "polish", "improve the CLI", "fix verify", "make it publish-ready", "clean up the CLI"
  - Setup phase: resolve CLI name/path using the same name resolution pattern from emboss (search `$PRESS_LIBRARY/`, fuzzy match, AskUserQuestion for disambiguation)
  - Diagnostics phase: run `printing-press dogfood`, `printing-press verify`, `printing-press scorecard`, and `go vet` against the CLI. Parse output into categorized findings. Report baseline scores.
  - Fix phase: apply fixes in priority order. For each category, describe the fix strategy:
    1. Verify: remove cobra Args constraints, add len(args) check + cmd.Help(), add nil-data guards for dry-run
    2. Dead code: grep for each flagged function, remove if truly unused, clean up imports
    3. Descriptions: read root.go Short field, rewrite from API-speak to user-speak using the research brief if available
    4. README: generate from actual `--help` output and command list
    5. Dogfood: address remaining flagged issues
  - After fixes: rebuild binary (`go build -o <cli-name> ./cmd/<cli-name>`), re-run all diagnostics
  - Report: before/after table showing scorecard, verify pass rate, dogfood verdict, specific improvements
  - Publish: same AskUserQuestion flow as Phase 6 in the main skill. If critical issues remain, offer: fix again, publish anyway, or done

  **Patterns to follow:**
  - `~/.claude/skills/printing-press-retro/SKILL.md` for overall structure
  - `skills/printing-press/SKILL.md:84-86` for name resolution
  - `skills/printing-press/SKILL.md:1544-1570` for publish offer flow

  **Test scenarios:**
  - Happy path: Invoke `/printing-press-polish redfin` on an as-generated CLI with 52% verify -> skill fixes Args, dead code, README, reports 96%+ verify, offers publish
  - Happy path: Invoke with full path `~/printing-press/library/redfin-pp-cli` -> resolves correctly
  - Edge case: CLI name doesn't match -> fuzzy search finds close matches, user picks
  - Edge case: CLI already at 85+ scorecard -> diagnostics run, few/no fixes needed, reports "already clean"
  - Edge case: Fixes don't fully resolve verify -> reports remaining issues, offers "fix again" option

  **Verification:** The skill file exists, has valid frontmatter, and contains all phases described in the requirements.

- [ ] **Unit 2: Remove emboss from main printing-press skill**

  **Goal:** Replace inline emboss mode with a reference to `/printing-press-polish`. Drop the "emboss" name from the in-flow improvement loop.

  **Requirements:** R15, R16

  **Dependencies:** Unit 1

  **Files:**
  - Modify: `skills/printing-press/SKILL.md`

  **Approach:**
  - Replace lines 74-97 (Emboss Mode section) with: "For second-pass improvements to an existing CLI, use `/printing-press-polish <cli-name>`. See the printing-press-polish skill."
  - Remove the emboss examples from the top-level usage examples (lines 30-32)
  - Update any references to "emboss mode" or "Emboss Cycle" elsewhere in the file (line 301)
  - The in-flow shipcheck fix loop (Phase 4) keeps its behavior but any "emboss" naming is removed

  **Patterns to follow:**
  - The main skill's existing section structure

  **Test scenarios:**
  - Happy path: grep for "emboss" in SKILL.md returns only the reference to the new skill (not inline instructions)
  - Happy path: The shipcheck fix loop in Phase 4 still works as before (no behavioral change)
  - Edge case: Any cross-references to emboss in other skills or docs are updated

  **Verification:** No inline emboss instructions remain. The reference to `/printing-press-polish` is clear.

- [ ] **Unit 3: Add polish suggestion to Phase 6 publish flow**

  **Goal:** Always suggest `/printing-press-polish` as an option after shipcheck, alongside publish and retro.

  **Requirements:** R14

  **Dependencies:** Unit 1

  **Files:**
  - Modify: `skills/printing-press/SKILL.md` (Phase 6 section)

  **Approach:**
  - In the Phase 6 AskUserQuestion, add a new option: "Polish first" - Run `/printing-press-polish` to improve the CLI before publishing
  - Place it as option 2 (between "Yes - publish now" and "No - I'm done")
  - For `ship-with-gaps` verdicts, recommend the polish option
  - If the user picks polish, invoke `/printing-press-polish <cli-name>`. The polish skill handles everything from there including its own publish offer at the end.

  **Patterns to follow:**
  - Existing AskUserQuestion pattern in Phase 6

  **Test scenarios:**
  - Happy path: After shipcheck with `ship` verdict -> publish offer includes "Polish first" option
  - Happy path: After shipcheck with `ship-with-gaps` -> polish is recommended
  - Happy path: User picks "Polish first" -> skill invokes `/printing-press-polish`
  - Edge case: `hold` verdict -> Phase 6 skipped entirely (no change to existing behavior)

  **Verification:** The AskUserQuestion in Phase 6 shows the polish option. Selecting it invokes the polish skill.

## System-Wide Impact

- **Interaction graph:** The polish skill invokes printing-press CLI tools (dogfood, verify, scorecard) and the publish skill. It reads and modifies generated CLI source files.
- **API surface parity:** The polish skill's publish offer must match Phase 6's publish flow exactly to avoid UX divergence.
- **Unchanged invariants:** The main printing-press skill's generation phases (0-4) are unchanged. The shipcheck fix loop behavior is unchanged (only naming changes).

## Risks & Dependencies

| Risk | Mitigation |
|------|------------|
| Polish skill fixes break working code | The fix strategies are mechanical and well-tested from the Redfin session. Rebuild + re-verify after fixes catches regressions. |
| Removing emboss breaks existing user muscle memory | Low risk - emboss is rarely invoked manually. The main skill's reference to `/printing-press-polish` provides the redirect. |
| Polish + retro confusion | Clear scope separation: polish fixes the CLI, retro improves the generator. Skill descriptions make this explicit. |

## Sources & References

- **Origin document:** [docs/brainstorms/2026-03-30-printing-press-polish-skill-requirements.md](docs/brainstorms/2026-03-30-printing-press-polish-skill-requirements.md)
- Retro skill pattern: `~/.claude/skills/printing-press-retro/SKILL.md`
- Main skill emboss section: `skills/printing-press/SKILL.md:74-97`
- Main skill Phase 6: `skills/printing-press/SKILL.md:1520-1570`
- Redfin retro (evidence for fix strategies): `docs/retros/2026-03-30-redfin-retro.md`
