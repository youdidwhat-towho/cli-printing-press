---
title: "refactor: Decouple skills and binary from repo checkout"
type: refactor
status: completed
date: 2026-03-28
origin: docs/brainstorms/2026-03-28-repo-decoupling-requirements.md
---

# Decouple Skills and Binary from Repo Checkout

## Overview

Make the CLI Printing Press distributable as a standalone `go install` binary + Claude Code plugin. All three skills currently assume they run inside the cli-printing-press repo checkout. This refactor removes that assumption so users can print CLIs from any directory without cloning the repo.

## Problem Frame

PR #30 decoupled the output side (artifacts to `~/printing-press/`). The input side still requires the repo: skills `cd` to repo root, invoke `./printing-press`, read `catalog/` from disk, and build from source. This blocks standalone distribution. (see origin: docs/brainstorms/2026-03-28-repo-decoupling-requirements.md)

## Requirements Trace

- R1. Setup contract works from any directory (not just cli-printing-press repo)
- R2. PRESS_SCOPE: git root (any repo) with CWD fallback, canonicalized for symlink stability
- R3. Binary check on $PATH with GOPATH/bin diagnostic and install instructions
- R4. Skills invoke `printing-press` (PATH) not `./printing-press` (repo-relative), including reference files
- R5. Score and catalog skills stop running `go build`
- R5a. Remove all `cd "$REPO_ROOT"` before binary invocations
- R6. Embed catalog/ YAMLs via go:embed
- R7. Catalog skill reads from binary subcommands, not disk
- R8. Catalog list/show/search subcommands with --json
- R9. Catalog always uses embedded data; update fullrun.go and research.go to use embedded FS (no disk fallback — devs rebuild binary for catalog changes)
- R10. Existing plugin structure works; bump version on merge
- R11. Plugin ships skills only, no Go source
- R12. Plugin README documents two-step setup
- R13. In-repo dev unaffected; catalog changes require rebuild (same as template changes)
- R14. Transparent repo-vs-standalone detection
- R15. Update contracts_test.go for new contract
- R16. Skills declare minimum binary version; setup contract checks it

## Scope Boundaries

- Not in scope: `/printing-press-publish` skill, cli-pp-Library changes, Homebrew/release automation
- Not in scope: Removing `catalog/` from repo (stays as source-of-truth embedded at build time)
- fullrun.go / MakeBestCLI() is dev/test-only; not required to work standalone

## Context & Research

### Relevant Code and Patterns

- **Setup contract**: Copy-pasted across 3 skills between `<!-- PRESS_SETUP_CONTRACT_START/END -->` markers. Enforced by `internal/pipeline/contracts_test.go` TestSkillSetupBlocksMatchWorkspaceContract
- **Catalog package**: `internal/catalog/catalog.go` — `ParseDir(dir string)` uses `os.ReadDir`, `ParseEntry(data []byte)` parses single YAML. `Entry` struct has all fields including `KnownAlternatives`
- **Catalog consumers**: `loadCatalogAlternatives()` in `internal/pipeline/research.go:170` defaults to `"catalog"` dir. Called by `RunResearch()` in `fullrun.go:116` with hardcoded `"catalog"`
- **go:embed pattern**: `internal/generator/generator.go:20` embeds `templates/` directory. Accessed via `templateFS.ReadFile()`
- **CLI framework**: Cobra. Commands registered in `internal/cli/root.go` via `newXxxCmd()` pattern
- **Path helpers**: `internal/pipeline/paths.go` — `PressHome()`, `WorkspaceScope()`, `repoRoot()` all support env var overrides (`PRINTING_PRESS_HOME`, `PRINTING_PRESS_SCOPE`, `PRINTING_PRESS_REPO_ROOT`)
- **Version**: Hardcoded `var version = "0.1.0"` in `internal/cli/root.go:28`. `version` subcommand outputs `printing-press 0.1.0`
- **Plugin**: `.claude-plugin/plugin.json` with name, version 0.4.0, description, repository. Skills discovered by convention from `skills/` directory

### Institutional Learnings

- `docs/solutions/best-practices/checkout-scoped-printing-press-output-layout-2026-03-28.md`: Never hardcode `~/cli-printing-press`. Always resolve paths dynamically. Path logic centralized in `internal/pipeline/paths.go`, naming in `internal/naming/`
- Scope derivation: basename + 8-char SHA256 of full path. Already falls back to CWD when not in a git repo (paths.go:152-173)

## Key Technical Decisions

- **go:embed location**: Create `catalog/catalog.go` as `package catalog` with `//go:embed *.yaml` and `var FS embed.FS`. This follows Go convention (package name matches directory name). Import as `github.com/mvanhorn/cli-printing-press/catalog`. Consumers that also import `internal/catalog` use an import alias (e.g., `catalogfs "github.com/mvanhorn/cli-printing-press/catalog"`) — standard Go practice for name collisions
- **fs.FS interface for catalog**: Add `ParseFS(fsys fs.FS) ([]Entry, error)` to the existing `internal/catalog/catalog.go`. All consumers use the embedded FS
- **Always use embedded catalog**: No disk-first fallback. The binary always reads from the embedded catalog data. In-repo developers who modify catalog YAMLs simply rebuild the binary (`go build`). This eliminates the risk of accidentally reading a stray `catalog/` directory in the user's CWD and avoids the complexity of per-entry vs per-directory fallback logic
- **Version from build info**: Use `runtime/debug.ReadBuildInfo()` to detect the module version set by `go install`. Fall back to hardcoded version for local `go build`. This avoids needing `-ldflags` injection
- **Minimum version protocol**: Skills declare `min-binary-version` in YAML frontmatter. The version check is an **agent instruction** outside the bash contract block (not bash — bash can't read its own file's YAML frontmatter). Claude Code reads the frontmatter, runs `printing-press version --json`, and compares. Warns but does not halt on mismatch. The minimum version is also hardcoded in the contract bash block as a comment for contracts_test.go to verify matches the frontmatter
- **Catalog subcommand output**: Human-readable by default, `--json` flag for machine output (matching the existing convention in scorecard, generate, and version commands). Skills pass `--json` explicitly when invoking catalog subcommands

## Open Questions

### Resolved During Planning

- **Where does go:embed live?** `catalog/catalog.go` as `package catalog`. Go's embed can only reference same-dir or subdirs. `catalog/` is at repo root, unreachable from `internal/` or `cmd/`. Package name matches directory name per Go convention; consumers use import aliases when they also import `internal/catalog`
- **How does version detection work with go install?** `debug.ReadBuildInfo()` returns the module version automatically when built via `go install`. No ldflags needed
- **Working directory after removing cd $REPO_ROOT?** Binary commands already accept `--dir` and `--spec` flags. No specific working directory needed. Skills cd to the output/working directory for the current run
- **Does fullrun.go need to work standalone?** No. It's dev/test-only. Explicitly not in scope
- **Catalog output format?** Human-readable by default, `--json` flag for machine parsing (consistent with all other CLI commands). Skills pass `--json` explicitly

### Deferred to Implementation

- Exact semver comparison logic for R16 version checking (simple string comparison may suffice initially)
- Whether `repoRoot()` in paths.go needs updating or if the existing env var override is sufficient
- Whether the catalog skill should support adding user-provided catalog entries to the embedded set

## Implementation Units

- [ ] **Unit 1: Catalog embed package and fs.FS refactor**

**Goal:** Make catalog data accessible without the repo filesystem by embedding YAMLs and adding fs.FS support to the catalog package.

**Requirements:** R6, R9, R13

**Dependencies:** None

**Files:**
- Create: `catalog/catalog.go`
- Modify: `internal/catalog/catalog.go`
- Modify: `AGENTS.md` (update project structure: `catalog/` now contains a Go package alongside YAML data)
- Test: `internal/catalog/catalog_test.go`

**Approach:**
- Create `catalog/catalog.go` as `package catalog` with `//go:embed *.yaml` and `var FS embed.FS`
- Add `ParseFS(fsys fs.FS) ([]Entry, error)` to `internal/catalog/catalog.go` that mirrors `ParseDir()` but reads from an `fs.FS` via `fs.ReadDir` and `fs.ReadFile`
- Add `LookupFS(fsys fs.FS, name string) (*Entry, error)` for single-entry lookup by name (used by catalog show and loadCatalogAlternatives)
- Keep `ParseDir()` unchanged for backward compatibility
- Consumers that import both packages use an alias: `catalogfs "github.com/mvanhorn/cli-printing-press/catalog"`

**Patterns to follow:**
- `internal/generator/generator.go:20` — `//go:embed templates` with `embed.FS`
- `internal/catalog/catalog.go:70` — `ParseDir()` structure for the new `ParseFS()`

**Test scenarios:**
- Happy path: ParseFS reads embedded catalog and returns all 17 entries
- Happy path: LookupFS finds "stripe" entry with correct SpecURL
- Edge case: LookupFS returns error for non-existent entry name
- Edge case: ParseFS with empty fs.FS returns empty slice, no error
- Integration: catalog.FS (from `github.com/mvanhorn/cli-printing-press/catalog`) is importable and contains the expected YAML files

**Verification:**
- `go test ./internal/catalog/...` passes
- `go build ./catalog/...` succeeds (new package compiles)

---

- [ ] **Unit 2: Update catalog consumers to use embedded FS**

**Goal:** Wire the embedded catalog into `loadCatalogAlternatives()` and `fullrun.go` so they always use embedded data instead of reading from disk.

**Requirements:** R9

**Dependencies:** Unit 1

**Files:**
- Modify: `internal/pipeline/research.go`
- Modify: `internal/pipeline/fullrun.go`
- Test: `internal/pipeline/research_test.go`

**Approach:**
- Refactor `loadCatalogAlternatives()` to use `LookupFS(catalogFS, apiName)` with the embedded FS instead of `os.ReadFile(filepath.Join(catalogDir, apiName+".yaml"))`
- Remove the `catalogDir string` parameter from `loadCatalogAlternatives()` and `RunResearch()` — replace with the embedded FS
- Update `fullrun.go` callsite to remove the hardcoded `"catalog"` directory argument
- Note: fullrun.go is dev/test-only (see Scope Boundaries) but is updated here for API consistency — removing the `catalogDir` parameter that no longer exists. This is a signature cleanup, not a standalone requirement

**Patterns to follow:**
- Existing `loadCatalogAlternatives()` structure at `research.go:170`
- Unit 1's `LookupFS()` for single-entry access

**Test scenarios:**
- Happy path: loadCatalogAlternatives reads from embedded FS and returns known alternatives for "stripe"
- Edge case: API not in embedded catalog — returns nil (graceful, no error)
- Happy path: RunResearch no longer takes a catalogDir parameter

**Verification:**
- `go test ./internal/pipeline/...` passes
- When run from a directory without `catalog/`, the binary still finds catalog data

---

- [ ] **Unit 3: Catalog CLI subcommands**

**Goal:** Add `printing-press catalog list|show|search` subcommands that expose embedded catalog data for skill consumption.

**Requirements:** R7, R8

**Dependencies:** Unit 1

**Files:**
- Create: `internal/cli/catalog.go`
- Modify: `internal/cli/root.go` (add `rootCmd.AddCommand(newCatalogCmd())`)
- Test: `internal/cli/catalog_test.go`

**Approach:**
- Create `newCatalogCmd()` returning a parent Cobra command with three subcommands
- `catalog list`: Parse all entries via `ParseFS(catalogfs.FS)`, output grouped-by-category display. `--json` flag for JSON array output
- `catalog show <name>`: Lookup single entry via `LookupFS(catalogfs.FS, name)`, output formatted display. `--json` for JSON object with spec_url, category, description, known_alternatives
- `catalog search <query>`: Case-insensitive search across name, display_name, description, category. Output matching entries. `--json` for JSON array
- All subcommands use the embedded catalog FS directly — no disk fallback
- Error handling: unknown name returns exit code + clear message

**Patterns to follow:**
- `internal/cli/scorecard.go` — Cobra command structure with `RunE` and local flags
- `internal/cli/exitcodes.go` — custom exit code pattern

**Test scenarios:**
- Happy path: `catalog list` outputs grouped-by-category human-readable display
- Happy path: `catalog list --json` returns all 17 entries as JSON array
- Happy path: `catalog show stripe --json` returns Stripe entry with spec_url field
- Happy path: `catalog search auth` returns entries matching "auth" in any field
- Edge case: `catalog show nonexistent` returns error with clear message
- Edge case: `catalog search` with no matches returns empty array

**Verification:**
- `go test ./internal/cli/...` passes
- `./printing-press catalog list --json | jq length` returns 17
- `./printing-press catalog show stripe --json | jq .spec_url` returns the Stripe spec URL

---

- [ ] **Unit 4: Version detection from build info**

**Goal:** Make `printing-press version` report the correct version when installed via `go install`, and add machine-parseable output for skills to check.

**Requirements:** R16

**Dependencies:** None (parallel with Units 1-3)

**Files:**
- Modify: `internal/cli/root.go`
- Test: `internal/cli/root_test.go` (or inline test in existing test file)

**Approach:**
- In `init()` or package-level var block, use `debug.ReadBuildInfo()` to read `(vcs.revision)` and module version. If module version is set (not empty or "(devel)"), use it as `version`
- Bump hardcoded `version` from `"0.1.0"` to `"0.2.0"` in this PR (the version that adds catalog subcommands). Use `debug.ReadBuildInfo()` to override with the module version when available (go install). Fall back to the hardcoded `"0.2.0"` for local `go build`
- `printing-press version` already outputs `printing-press X.Y.Z\n` — no format change needed
- Add `--json` flag to version command for machine parsing: `{"version": "X.Y.Z", "go": "1.26.1"}`

**Patterns to follow:**
- Existing `newVersionCmd()` in `internal/cli/root.go:439-447`

**Test scenarios:**
- Happy path: Version command outputs `printing-press X.Y.Z` format
- Happy path: `--json` flag outputs parseable JSON with version field
- Edge case: When build info is unavailable, falls back to hardcoded version

**Verification:**
- `go test ./internal/cli/...` passes
- `./printing-press version` outputs version string
- `./printing-press version --json | jq .version` returns version

---

- [ ] **Unit 5: Rewrite setup contract and update contract tests**

**Goal:** Replace the repo-dependent setup contract with one that works from any directory, checks binary on PATH, and verifies minimum version.

**Requirements:** R1, R2, R3, R14, R15, R16

**Dependencies:** Unit 4 (version --json needed for version check)

**Files:**
- Modify: `skills/printing-press/SKILL.md` (setup contract block only)
- Modify: `skills/printing-press-catalog/SKILL.md` (setup contract block only)
- Modify: `skills/printing-press-score/SKILL.md` (setup contract block only)
- Modify: `internal/pipeline/contracts_test.go`

**Approach:**
- New contract block (between existing HTML comment markers):
  1. Check `command -v printing-press`. If missing, check `~/go/bin/printing-press` and suggest PATH fix. Otherwise show `go install` instructions and halt
  2. (Agent instruction outside contract block, not bash): Read `min-binary-version` from skill frontmatter, run `printing-press version --json`, compare versions. Warn if binary is older than minimum. The contract block includes a comment with the minimum version for `contracts_test.go` to verify matches frontmatter
  3. Derive PRESS_SCOPE: try `git rev-parse --show-toplevel 2>/dev/null`. If succeeds, use that path (any repo, not just cli-printing-press). If fails, use CWD. In both cases, canonicalize with `cd "$dir" && pwd -P` for symlink stability (portable — macOS default readlink doesn't support `-f` and `realpath` wasn't universal until macOS 12.3)
  4. Derive PRESS_HOME, PRESS_RUNSTATE, PRESS_LIBRARY, PRESS_MANUSCRIPTS from PRESS_SCOPE (same as today, just different root derivation)
  5. Remove `cd "$REPO_ROOT"` — no longer needed
- Add `min-binary-version: 0.2.0` to all three skill YAML frontmatter (0.2.0 = version that has catalog subcommands)
- Update `contracts_test.go`:
  - Remove assertion for `REPO_ROOT`
  - Add assertions for `command -v printing-press`, `printing-press version`, `realpath`/symlink handling
  - Keep assertions for PRESS_HOME, PRESS_SCOPE, PRESS_LIBRARY, PRESS_RUNSTATE, PRESS_MANUSCRIPTS

**Patterns to follow:**
- Existing `PRESS_SETUP_CONTRACT_START/END` HTML comment markers
- `internal/pipeline/contracts_test.go` — extractContractBlock() and assertion pattern

**Test scenarios:**
- Happy path: Contract block extracted from each skill contains `command -v printing-press`
- Happy path: Contract block contains PRESS_SCOPE derivation with git fallback to CWD
- Happy path: Contract block contains `printing-press version` check
- Happy path: Contract block does NOT contain `cd "$REPO_ROOT"`
- Happy path: Contract block does NOT contain `./printing-press` (repo-relative)
- Happy path: Contract block does NOT contain `git rev-parse --show-toplevel` as a hard requirement (only as optional scope hint)
- Edge case: Contract block does NOT contain `go build`

**Verification:**
- `go test ./internal/pipeline/...` passes (contract tests green)
- All three skills have identical contract blocks (within expected variation for PRESS_MANUSCRIPTS)

---

- [ ] **Unit 6: Rewrite skill binary invocations and text**

**Goal:** Replace all `./printing-press` with `printing-press`, remove all `cd "$REPO_ROOT"`, add catalog-aware shortcut to the main skill, and deprecate `/printing-press-catalog`.

**Requirements:** R4, R5, R5a, R7, R8

**Dependencies:** Unit 3 (catalog subcommands), Unit 5 (new setup contract)

**Files:**
- Modify: `skills/printing-press/SKILL.md`
- Deprecate: `skills/printing-press-catalog/SKILL.md` (mark as deprecated, point users to main skill)
- Modify: `skills/printing-press-score/SKILL.md`
- Modify: `skills/printing-press/references/scorecard-patterns.md`

**Approach:**
- Main skill — binary path fixes: Replace all `cd "$REPO_ROOT" && ./printing-press` with just `printing-press`. Binary invocations that need a specific directory should use `--dir` flag, not `cd`
- Main skill — catalog-aware shortcut: Before starting the research phase, check `printing-press catalog show <api> --json 2>/dev/null`. If the API is in the catalog, present a choice: "Stripe is in the built-in catalog (official spec, 500+ endpoints). Use the catalog config, or run full discovery?" If the user chooses catalog config, use the spec_url directly, skip discovery. If not in catalog or user chooses discovery, proceed with the normal workflow
- Deprecate `/printing-press-catalog`: The main skill now handles catalog lookup automatically. Mark the catalog skill as deprecated in its frontmatter (`deprecated: true`) and add a note pointing users to `/printing-press <api>` instead. The `printing-press catalog list` CLI command still exists for browsing
- Score skill: Remove `go build -o ./printing-press ./cmd/printing-press`. Replace `./printing-press scorecard` with `printing-press scorecard`. Remove prerequisite "Running from inside the cli-printing-press repo". Update error message
- Reference files: Update `scorecard-patterns.md` to use `printing-press scorecard`

**Patterns to follow:**
- The new setup contract from Unit 5 already checks binary availability
- Catalog subcommands from Unit 3 provide the `--json` output the skill parses

**Test scenarios:**
- Happy path: No skill file contains `./printing-press` (contract test assertion)
- Happy path: No skill file contains `go build -o ./printing-press`
- Happy path: No skill file contains `cd "$REPO_ROOT"` outside the setup contract markers
- Happy path: No skill file contains "cli-printing-press checkout" or "cli-printing-press repo" in prerequisites
- Happy path: Main skill's Phase 1 checks catalog for the API name before starting research
- Happy path: Catalog skill is marked deprecated with redirect to main skill

**Verification:**
- `grep -r './printing-press' skills/` returns no matches
- `grep -r 'go build.*printing-press' skills/` returns no matches
- `go test ./internal/pipeline/...` passes (contract tests verify no deprecated patterns)

---

- [ ] **Unit 7: Plugin version bump and final validation**

**Goal:** Bump plugin version, ensure the plugin works when installed standalone, and validate end-to-end.

**Requirements:** R10, R11, R12, R13

**Dependencies:** Units 1-6

**Files:**
- Modify: `.claude-plugin/plugin.json` (version bump)
- Modify: `AGENTS.md` (if any repo-assumption text remains)

**Approach:**
- Bump `.claude-plugin/plugin.json` version (e.g., 0.4.0 -> 0.5.0)
- Verify R11: no Go source, build scripts, or repo-assuming content ships with the plugin (skills/ directory is clean)
- Verify R13: `go test ./...` passes (in-repo dev workflow intact)
- Verify R12: README or AGENTS.md documents two-step setup
- Final grep audit: no remaining `./printing-press`, `cd "$REPO_ROOT"`, or "cli-printing-press checkout" in skills or references

**Test scenarios:**
- Happy path: `go test ./...` — all tests pass
- Happy path: `go build -o ./printing-press ./cmd/printing-press` — binary builds
- Happy path: `./printing-press catalog list --json` — returns catalog data
- Happy path: `./printing-press version --json` — returns version
- Integration: From a directory outside the repo, `printing-press catalog list --json` works (if binary is on PATH)

**Verification:**
- All tests pass
- Binary builds and runs standalone
- No repo-path assumptions remain in skills

## System-Wide Impact

- **Interaction graph:** The setup contract block is shared (copy-pasted) across 3 skills. Changing it affects all skill invocations. The contract test in contracts_test.go is the safety net
- **Error propagation:** Skills that can't find the binary now halt with install instructions (R3) instead of silently failing on `./printing-press`
- **State lifecycle risks:** PRESS_SCOPE changes from "cli-printing-press repo root" to "any git root or CWD". Existing runstate in `~/printing-press/.runstate/` uses scopes derived from the old formula. Users with existing runs will get new scope hashes — old runs remain on disk but won't be discovered by the new scope. This is acceptable since the tool is pre-release
- **API surface parity:** The `catalog list/show/search` subcommands are new CLI surface. The main `/printing-press` skill now checks the catalog automatically, making `/printing-press-catalog` redundant (deprecated). Two skills ship instead of three
- **Unchanged invariants:** All output paths (`~/printing-press/library/`, `~/printing-press/manuscripts/`, `~/printing-press/.runstate/`) remain the same. Generated CLI structure is unaffected. The generate/dogfood/verify/scorecard/emboss commands are unchanged

## Risks & Dependencies

| Risk | Mitigation |
|------|------------|
| go:embed path constraint — `catalog/` at repo root can't be embedded from `internal/` | Resolved: create `catalog/catalog.go` as `package catalog` at the same level |
| Existing runstate scopes invalidated by PRESS_SCOPE change | Pre-release tool; old runs remain on disk. Document in release notes |
| `go install` may not work if module not on public proxy | Success criteria includes validating this. Module is already public on GitHub |
| Skills may reference repo paths in subtle ways not caught by search | Contract tests + grep audit in Unit 7 as safety net |
| Version skew between binary and skills | R16 minimum version check warns users. Non-blocking to avoid hard failures |

## Sources & References

- **Origin document:** [docs/brainstorms/2026-03-28-repo-decoupling-requirements.md](docs/brainstorms/2026-03-28-repo-decoupling-requirements.md)
- Related code: `internal/pipeline/paths.go`, `internal/catalog/catalog.go`, `internal/cli/root.go`, `internal/pipeline/contracts_test.go`
- Related PR: #30 (feat(pipeline): move mutable runs into scoped runstate)
- Prior plans: `docs/plans/2026-03-25-feat-distribution-skill-homebrew-catalog-plan.md`, `docs/plans/2026-03-23-feat-cli-printing-press-phase4-catalog-community-plan.md`
- Institutional learning: `docs/solutions/best-practices/checkout-scoped-printing-press-output-layout-2026-03-28.md`
