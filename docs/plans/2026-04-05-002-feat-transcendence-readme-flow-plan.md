---
title: "feat: Flow transcendence features into generated READMEs"
type: feat
status: completed
date: 2026-04-05
---

# feat: Flow transcendence features into generated READMEs

## Overview

Transcendence features (novel capabilities the machine invents during absorb, not found in any existing tool) are the printed CLI's key differentiator. They get identified, scored, and built into the CLI -- but never called out in the README. This change extends the existing `research.json` sidecar to carry novel features through the generator pipeline and renders them in a dedicated README section.

## Problem Frame

During the absorb phase (Phase 1.5), the skill identifies compound use cases only possible with our local data layer. These score >= 5/10 on domain fit, user pain, build feasibility, and research backing. The skill builds them as Priority 2 commands. But the data stays in the absorb manifest (markdown) and never reaches the README because:

1. `research.json` has no field for individual novel features
2. `Generator` struct doesn't carry them
3. `readme.md.tmpl` has no section for them

The Sources & Inspiration section credits tools we learned from. There's no counterpart section saying "here's what we invented."

## Requirements Trace

- R1. Generated READMEs must include a section highlighting transcendence features when they exist
- R2. The section must show the feature name, CLI command, user-facing description, and rationale
- R3. The section must be absent (no empty heading, no placeholder) when no novel features qualify
- R4. The data must flow through the existing `research.json` -> `loadResearchSources()` -> Generator -> template pipeline
- R5. Existing scored README sections (Quick Start, Agent Usage, Health Check, Troubleshooting, Cookbook) must remain intact

## Scope Boundaries

- Does not change the absorb manifest format (it already has the transcendence table)
- Does not change the absorb scoring framework
- Does not add a new CLI subcommand
- Does not change the `--research-dir` flag behavior
- Does not affect the automated `RunResearch()` code path (it doesn't produce novel features)

## Context & Research

### Relevant Code and Patterns

- `internal/pipeline/research.go:23-32` -- `ResearchResult` struct, the JSON contract between skill and generator
- `internal/pipeline/research.go:173-178` -- `ReadmeSource` type (pipeline side)
- `internal/pipeline/research.go:214-234` -- `SourcesForREADME()` filter and sort
- `internal/generator/generator.go:29-34` -- `ReadmeSource` type (generator side, mirrors pipeline type)
- `internal/generator/generator.go:36-46` -- `Generator` struct with `Sources` and `DiscoveryPages`
- `internal/generator/generator.go:237-256` -- `readmeTemplateData` and `readmeData()` method
- `internal/cli/root.go:676-696` -- `loadResearchSources()` bridges pipeline -> generator
- `internal/generator/templates/readme.md.tmpl:210-227` -- "Sources & Inspiration" conditional section
- `skills/printing-press/SKILL.md:815-843` -- Step 1.5e writes research.json

### Institutional Learnings

- `docs/plans/2026-04-01-008-feat-readme-source-credits-plan.md` -- Established the exact pattern this plan follows: skill writes research.json -> `loadResearchSources()` reads it -> Generator populates template. Canonical reference for sidecar data flow.
- `docs/retros/2026-04-01-steam-run4-retro.md` -- README template must always emit the 5 scored sections. New sections must not displace them.
- `docs/plans/2026-03-30-002-feat-readme-source-credits-plan.md` -- Use template-based rendering (not `AugmentREADME`) for data available at generation time.

## Key Technical Decisions

- **Extend `research.json` rather than a separate sidecar file:** The skill writes research.json at Step 1.5e, which happens after transcendence features are identified (Steps 1.5c-1.5d). Adding `novel_features` to that same write is one field change, not a new file + new loader + new function. `json:",omitempty"` keeps backward compatibility. This follows the recommendation from `docs/plans/2026-04-01-008` which established research.json as the single sidecar contract.
- **Duplicate type across packages (pipeline + generator):** Follows the existing `ReadmeSource` pattern. The `generator` package does not import `pipeline`, so a mirror type with field-by-field copy in `loadResearchSources()` is the established convention. Compile-time safety catches drift.
- **Section heading "What's New Here" over "Novel Features" or "Beyond the Ecosystem":** Direct, non-marketing, answers the reader's question. The reader seeing this section immediately understands: these are things other tools don't have.
- **No scores or evidence in README:** Scores and evidence citations are internal research artifacts. The README reader wants to know what the tool does, not how features were rated. The skill filters to >= 5/10 before writing to research.json.
- **Cap at 5 features in the skill instruction:** A focused selection reads better than an exhaustive list. The skill orders by score descending and caps at 5.

## Open Questions

### Resolved During Planning

- **Where to place the new section?** Between the Troubleshooting `---` divider and "Sources & Inspiration". After all practical sections, before credits. Does not interfere with the 5 scored sections.
- **What if `RunResearch()` (automated path) runs and writes research.json without novel features?** `json:",omitempty"` means the field is simply absent. `loadResearchSources()` sees nil/empty, Generator gets nil, template guard skips the section. No behavioral change on the automated path.

### Deferred to Implementation

- **Exact template whitespace:** Go template whitespace trimming (`{{-` vs `{{`) needs to produce clean markdown with no extra blank lines. Will be tuned during implementation by reading the rendered output.

## Implementation Units

- [x] **Unit 1: Add `NovelFeature` type and extend `ResearchResult`**

**Goal:** Make research.json capable of carrying novel features.

**Requirements:** R4

**Dependencies:** None

**Files:**
- Modify: `internal/pipeline/research.go`
- Test: `internal/pipeline/research_test.go`

**Approach:**
- Add `NovelFeature` struct with `Name`, `Command`, `Description`, `Rationale` (all string, all json-tagged)
- Add `NovelFeatures []NovelFeature` field to `ResearchResult` with `json:"novel_features,omitempty"`
- No separate `NovelFeaturesForREADME()` helper needed -- unlike `SourcesForREADME()` which filters empty URLs and sorts by stars, novel features require no transformation. The caller in `loadResearchSources()` accesses `research.NovelFeatures` directly with a nil guard, matching the `DiscoveryPages` pattern

**Patterns to follow:**
- `ReadmeSource` struct definition at line 173
- `Alternative` struct with json tags at line 53

**Test scenarios:**
- Happy path: `TestWriteAndLoadResearch` extended -- write a `ResearchResult` with 2 `NovelFeatures`, load it back, assert fields round-trip correctly
- Empty: write `ResearchResult` with nil `NovelFeatures`, load back, assert `NovelFeatures` is nil (omitempty works)

**Verification:**
- `go test ./internal/pipeline/...` passes
- research.json with novel_features round-trips through write/load

- [x] **Unit 2: Carry novel features through the Generator to the template**

**Goal:** Make the README template able to access novel features.

**Requirements:** R1, R2, R4

**Dependencies:** Unit 1

**Files:**
- Modify: `internal/generator/generator.go`
- Modify: `internal/cli/root.go`
- Test: `internal/generator/generator_test.go`

**Approach:**
- Add `NovelFeature` type in generator package (mirrors pipeline type: `Name`, `Command`, `Description`, `Rationale`)
- Add `NovelFeatures []NovelFeature` field to `Generator` struct
- Add `NovelFeatures []NovelFeature` field to `readmeTemplateData` struct
- Pass through in `readmeData()` method
- Extend `loadResearchSources()` in root.go: inside the existing `if err == nil` block (where `research` is non-nil), iterate `research.NovelFeatures`, convert each to `generator.NovelFeature`, assign to `gen.NovelFeatures`

**Patterns to follow:**
- `generator.ReadmeSource` type at line 29 (mirrors pipeline type)
- `Generator.Sources` field at line 41
- `readmeTemplateData.Sources` at line 240
- `readmeData()` pass-through at line 253
- `loadResearchSources()` conversion loop at lines 684-692

**Test scenarios:**
- Happy path: set `gen.NovelFeatures` to 2 entries, call `Generate()`, read README.md, assert "What's New Here" heading appears, assert both feature commands appear as `###` headings, assert descriptions and rationales appear
- Absent: leave `gen.NovelFeatures` nil, call `Generate()`, assert README does not contain "What's New Here"
- Edge case: single novel feature still renders the section (unlike Sources which requires 2+)
- Ordering: if both "What's New Here" and "Sources & Inspiration" are present, assert "What's New Here" appears first using `strings.Index` comparison

**Verification:**
- `go test ./internal/generator/...` passes
- No cli-level test needed for `loadResearchSources` -- the conversion is covered indirectly by the generator-level happy/absent tests above

- [x] **Unit 3: Add "What's New Here" section to the README template**

**Goal:** Render transcendence features in the generated README when present.

**Requirements:** R1, R2, R3, R5

**Dependencies:** Unit 2 (template data must be available)

**Files:**
- Modify: `internal/generator/templates/readme.md.tmpl`

**Approach:**
- Insert a conditional block between the `---` divider (line 209) and the Sources & Inspiration conditional (line 210)
- Guard with `{{- if .NovelFeatures}}`
- Each feature rendered as a `###` heading with the full CLI command, description as body text, rationale as a blockquote
- Section is completely absent when `NovelFeatures` is nil or empty

**Patterns to follow:**
- Sources & Inspiration conditional at lines 210-227 (conditional section with `{{- if}}` guard)
- Resource command rendering pattern at lines 107-114 (range over collection, use `$.Name` for CLI name)

**Test scenarios:**
- Test expectation: covered by Unit 2's generator tests (template rendering is tested by calling `gen.Generate()` and reading the output file)

**Verification:**
- Generated README with novel features contains the "What's New Here" heading, command subheadings, descriptions, and rationale blockquotes
- Generated README without novel features has no trace of the section
- All 5 scored sections (Quick Start, Agent Usage, Health Check, Troubleshooting, Cookbook) remain present

- [x] **Unit 4: Update skill instructions to write novel features to research.json**

**Goal:** Have the skill populate the `novel_features` field when writing research.json at Step 1.5e.

**Requirements:** R1, R4

**Dependencies:** Unit 1 (the struct must accept the field)

**Files:**
- Modify: `skills/printing-press/SKILL.md`

**Approach:**
- Extend the Step 1.5e heredoc example to include a `"novel_features"` array
- Add rules below the example: only features scoring >= 5/10, cap at 5 ordered by score descending, user-facing descriptions (not implementation details), skip the field entirely if no features qualify
- The `description` field should be user-benefit language, the `rationale` field should explain why this is only possible with our approach
- The `command` field must match an actual CLI subcommand that will be built in Phase 3

**Patterns to follow:**
- Step 1.5e's existing heredoc and rules for research.json at lines 815-843

**Test scenarios:**
- Test expectation: none -- skill instruction change, not code. Validated by next printing-press run.

**Verification:**
- Skill SKILL.md contains the updated Step 1.5e with novel_features in the JSON example
- The JSON example matches the `ResearchResult` struct contract (field name `novel_features`, array of objects with `name`, `command`, `description`, `rationale`)

## System-Wide Impact

- **Interaction graph:** `loadResearchSources()` is the sole bridge between research data and the generator. No other callers need changes. The skill writes the data; the generator reads it. No callbacks, no middleware.
- **Error propagation:** `LoadResearch()` already returns errors that `loadResearchSources` silently skips (bare `if err == nil` guard, no stderr warning). Novel features ride the same path -- if research.json is malformed, the entire Sources + NovelFeatures loading is silently skipped, producing empty sections in the README.
- **API surface parity:** The automated `RunResearch()` path does not produce novel features. This is correct -- only the skill-driven absorb phase invents features. No change needed to `RunResearch()`.
- **Unchanged invariants:** The 5 scored README sections, the Sources & Inspiration section, the `--research-dir` flag contract, the `AugmentREADME` marker system, and the quality gates are all unaffected.

## Risks & Dependencies

| Risk | Mitigation |
|------|------------|
| Skill writes malformed novel_features JSON | `json:",omitempty"` + graceful nil handling means the README section simply doesn't appear. Same resilience as existing Sources. |
| Command name in novel_features diverges from actual built command | The skill writes novel_features at Step 1.5e and builds the commands in Phase 3. Names may drift. Acceptable: the LLM polish pass or manual review catches this. |
| Template whitespace produces extra blank lines | Tune `{{-` trimming during implementation; verify by reading rendered README. |

## Sources & References

- Predecessor plan: `docs/plans/2026-04-01-008-feat-readme-source-credits-plan.md`
- Absorb & Transcend philosophy: `docs/plans/2026-03-28-feat-skill-landscape-absorb-transcend-plan.md`
- Auto-brainstorm scoring: `docs/plans/2026-03-30-009-feat-auto-brainstorm-before-absorb-gate-plan.md`
- Scored README sections retro: `docs/retros/2026-04-01-steam-run4-retro.md`
- Output layout contract: `docs/solutions/best-practices/checkout-scoped-printing-press-output-layout-2026-03-28.md`
