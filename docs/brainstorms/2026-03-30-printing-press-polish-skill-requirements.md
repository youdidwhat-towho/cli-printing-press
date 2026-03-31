---
date: 2026-03-30
topic: printing-press-polish-skill
---

# printing-press-polish: Standalone CLI Improvement Skill

## Problem Frame

After the printing press generates a CLI, there's always a gap between "it compiles" and "it's publish-ready." This gap currently requires manual work: fixing verify failures, removing dead code, cleaning up descriptions, writing the README, and other CLI-specific improvements that the generator can't anticipate because they depend on the unique characteristics of each API.

Today this work happens ad-hoc during the session (or not at all). The Redfin session required 5,504 lines of hand-written code and multiple fix iterations to go from 52% verify to 96%. That manual process should be a repeatable skill.

The existing "polish" mode inside the main printing-press skill is awkwardly named (polishing during printing is confusing) and underspecified (5 bullet points). It should be extracted into a standalone skill with clear phases, autonomy to fix everything, and a publish offer at the end.

## Requirements

**Skill Identity**
- R1. Standalone Claude Code skill at `skills/printing-press-polish/SKILL.md`
- R2. Invocable as `/printing-press-polish <cli-name-or-path>`
- R3. Name resolution: accept CLI name ("redfin"), full name ("redfin-pp-cli"), or path ("~/printing-press/library/redfin-pp-cli")
- R4. If name doesn't resolve, search `$PRESS_LIBRARY/` for close matches and let the user pick (same UX as current polish name resolution)

**Diagnostics Phase**
- R5. Run all three diagnostic tools against the CLI: `printing-press dogfood`, `printing-press verify`, `printing-press scorecard`
- R6. Parse results into a structured finding list with categories: verify failures, dead code, dead flags, path issues, README gaps, description issues, example gaps, data pipeline issues
- R7. Report the baseline: scorecard total, verify pass rate, dogfood verdict, with specific failing items listed

**Fix Phase**
- R8. Fix everything automatically without asking for approval. The user reviews the diff after, not before.
- R9. Fix categories (in priority order):
  1. Verify failures (cobra Args constraints, dry-run handling, missing mock arg support)
  2. Dead code (unused functions, unused imports, stale files like unregistered promoted commands)
  3. CLI description and metadata (root Short/Long descriptions, command examples)
  4. README (update to reflect actual commands, install instructions, quick start)
  5. Dogfood-flagged issues (path validity, auth mismatches, example drift)
- R10. After fixing, rebuild the binary and re-run all three diagnostic tools
- R11. Report the delta: before/after scorecard, verify pass rate, specific improvements made

**Publish Offer**
- R12. After showing the delta, offer to publish (same flow as Phase 6 in the main skill)
- R13. If the CLI still has critical issues after the fix pass, report them and offer: fix again, publish anyway, or done

**Integration with Main Skill**
- R14. The main printing-press skill should always suggest `/printing-press-polish` as an option after shipcheck, alongside publish and retro
- R15. Remove the inline "polish mode" from the main printing-press skill. Replace with a reference to the standalone skill.
- R16. The in-flow improvement loop (shipcheck fix iterations) keeps its current behavior but drops the "polish" name

## Success Criteria

- Running `/printing-press-polish redfin` on the Redfin CLI as-generated (before manual fixes) would automatically achieve the same 52% -> 96% verify improvement we did manually
- The skill completes in one pass for typical CLIs (no iterative user interaction needed)
- The main printing-press skill no longer contains polish mode instructions

## Scope Boundaries

- Does not add new features to the CLI (that's the absorb manifest's job during generation)
- Does not improve the printing-press generator itself (that's /printing-press-retro)
- Does not re-run the research or generation phases
- Does not handle APIs that require re-generation (broken specs, wrong endpoints)

## Key Decisions

- **Standalone skill, not embedded mode**: Polishing mid-print is confusing. A separate invocation after printing makes the mental model clearer.
- **Full autonomy**: Fix everything, report results. No approval gates. The user sees the before/after delta and can review the diff. This matches the "fix everything, report results" pattern.
- **Includes publish offer**: One continuous flow from "CLI needs work" to "CLI is published." No handoff friction.
- **Always suggested after shipcheck**: Not gated on score threshold. Even a 90/100 CLI might benefit from polishing.

## Outstanding Questions

### Deferred to Planning
- [Affects R9][Technical] What's the exact fix strategy for each verify failure type? The Redfin session showed that cobra Args constraints are the main culprit, but other CLIs may have different patterns.
- [Affects R6][Needs research] Should the diagnostics phase also run `go vet` and check for compilation warnings beyond what dogfood/verify/scorecard cover?
- [Affects R15][Technical] How to cleanly remove polish from SKILL.md without breaking the existing polish name resolution and library search logic that other parts of the skill reference.

## Next Steps

`/ce:plan` for structured implementation planning
