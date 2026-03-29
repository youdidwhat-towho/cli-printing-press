---
date: 2026-03-28
topic: repo-decoupling
---

# Decouple Skills and Binary from Repo Checkout

## Problem Frame

The CLI Printing Press binary is already repo-independent (templates embedded, commands accept URL/path args, output goes to `~/printing-press/`). But all three skills assume they're running inside the cli-printing-press repo checkout: they `cd` to repo root, invoke `./printing-press` from there, and read `catalog/` from the filesystem. This prevents distributing the printing press as a standalone tool (binary + Claude Code plugin) for users who want to print CLIs without cloning the repo.

PR #30 decoupled the output side. This work decouples the input side.

## Requirements

**Setup Contract**

- R1. The shared setup contract (`PRESS_SETUP_CONTRACT`) must not require the cli-printing-press repo. It should work from any directory — another project's repo, a non-repo folder, a cloud instance.
- R2. `PRESS_SCOPE` derivation: use git root if inside any git repo (not just cli-printing-press), fall back to CWD-based scope if not in a repo. Same isolation semantics as today, broader compatibility. Note: scoping by directory means a user who generates from project-A and then switches to project-B won't see project-A's runs in `/printing-press-score`. The scope should be derived from a stable, canonicalized path (resolve symlinks) to avoid drift when coding agents change the working directory mid-session.
- R3. The setup contract must check for the `printing-press` binary on `$PATH` (via `command -v printing-press`). If not found, check if `~/go/bin/printing-press` exists (common when GOPATH/bin is not on PATH) and suggest adding it. Otherwise display install instructions (`go install github.com/mvanhorn/cli-printing-press/cmd/printing-press@latest`) and halt until the user installs it.

**Binary Discovery**

- R4. Skills must invoke `printing-press` (PATH lookup) instead of `./printing-press` (repo-relative). All three skills need this change, including reference files under `skills/printing-press/references/` (e.g., `scorecard-patterns.md` contains `./printing-press scorecard`). All repo-specific text outside the setup contract markers must also be updated (prerequisites, error messages that say "cli-printing-press checkout").
- R5. The `/printing-press-score` and `/printing-press-catalog` skills must stop running `go build -o ./printing-press ./cmd/printing-press`. They should use the installed binary like the main skill.
- R5a. All `cd "$REPO_ROOT"` invocations before binary calls (Phase 2 generate, Phase 4 shipcheck, emboss mode) must be removed. The binary is on PATH and should be invocable from any working directory. Skills should `cd` to the output/working directory as needed, not to a repo root.

**Catalog Embedding**

- R6. Embed the `catalog/` YAML files into the binary via `go:embed`. The catalog data should be accessible at runtime without the repo filesystem. Note: Go's `//go:embed` can only embed files in the same directory or below the source file. Since `catalog/` is at repo root, the embed directive must live at or above that level (e.g., in `cmd/printing-press/` or a new top-level package), not in `internal/catalog/`.
- R7. The `/printing-press-catalog` skill must read catalog data from the binary (e.g., `printing-press catalog list`, `printing-press catalog show stripe`) instead of reading `catalog/*.yaml` from disk.
- R8. Add CLI subcommands to expose embedded catalog data: `printing-press catalog list` (all entries, JSON by default for skill consumption), `printing-press catalog show <name>` (single entry details including spec_url), and `printing-press catalog search <query>` (case-insensitive search across name, description, category). All subcommands must support `--json` output so skills can extract fields like `spec_url` programmatically.
- R9. `loadCatalogAlternatives()` in `research.go` and `ParseDir()` in the catalog package should support reading from an embedded FS. The binary always uses embedded catalog data — no disk fallback. In-repo developers who modify catalog YAMLs rebuild the binary (same as template changes). The `fullrun.go` callsite that hardcodes `"catalog"` as the directory path must also be updated.

**Plugin Packaging**

- R10. The repo is already a correctly structured Claude Code plugin. No structural changes needed — once R1-R9 make the skills repo-independent, the plugin automatically works when installed outside the repo. Bump the plugin version in `.claude-plugin/plugin.json` when merging to main so users' Claude Code picks up the update.
- R11. The plugin must not include Go source code, build scripts, or anything that assumes the repo. Skills only — the binary is installed separately.
- R12. The plugin's README should document the two-step setup: (1) install binary via `go install`, (2) install skills plugin.

**Backward Compatibility**

- R13. In-repo development must continue to work. When running from the cli-printing-press repo checkout, the existing dev workflow (build from source, run tests, etc.) is unaffected. Catalog changes require a rebuild of the binary to update embedded data, but R9's disk-first fallback means local edits are visible without rebuilding.
- R14. The setup contract should detect repo-vs-standalone transparently. No flags, no mode switches.
- R15. The existing `contracts_test.go` (which asserts the setup contract block content across all skills) must be updated to match the new contract. This is a known test change, not a regression.
- R16. Skills should declare a minimum binary version they require. The setup contract should check `printing-press version` and warn if the installed binary is older than the skill's minimum. Skills and binary evolve at different rates — version parity cannot be assumed.

## Success Criteria

- A user with Go installed can `go install` the binary and install the skills plugin, then successfully run `/printing-press <API>` from any directory without cloning the repo.
- `/printing-press-catalog` works standalone, browsing and installing from embedded catalog data.
- `/printing-press-score` works standalone using the PATH binary.
- All existing in-repo workflows continue to work unchanged.
- `go test ./...` passes.
- `go install github.com/mvanhorn/cli-printing-press/cmd/printing-press@latest` succeeds from a clean environment (validates the module is publicly installable).

## Scope Boundaries

- **Not in scope**: The `/printing-press-publish` skill. That's a separate future effort. The current `~/printing-press/library/` and `~/printing-press/manuscripts/` layout is sufficient for now.
- **Not in scope**: Changing the cli-pp-Library repo structure. The publish skill will handle assembly (bundling CLI + manuscripts) when we build it.
- **Not in scope**: Homebrew formula or binary release automation. `go install` is the initial distribution path.
- **Not in scope**: Removing `catalog/` from the repo. It stays as a development convenience and the source-of-truth that gets embedded into the binary at build time.

## Key Decisions

- **Scope derivation**: Git root (any repo) with CWD fallback — not tied to cli-printing-press specifically.
- **Binary distribution**: `go install` with skills guiding install if missing — not auto-download or bundled.
- **Catalog**: Embed in binary, expose via new CLI subcommands — not remote fetch or removal.
- **Skill distribution**: Claude Code plugin — not manual file installation.
- **Publish prep**: Deferred. Current artifact layout is sufficient. The publish skill can assemble bundles from library + manuscripts at publish time.

## Dependencies / Assumptions

- The existing Claude Code plugin structure (`.claude-plugin/`) continues to work when installed outside the repo, provided skills don't reference repo-local resources.
- `go install` works for the module path `github.com/mvanhorn/cli-printing-press/cmd/printing-press`. Validated in Success Criteria.
- The binary name `printing-press` doesn't collide with anything common on PATH.

## Outstanding Questions

### Deferred to Planning

- [Affects R6][Needs research] Where should the `//go:embed catalog` directive live given Go's directory constraint? Options: `cmd/printing-press/`, a new top-level `embed.go`, or restructuring `catalog/` under `internal/`.
- [Affects R12] Should the plugin README live in the repo root or in a separate `plugin/` directory?
- [Affects R5a] What working directory should skills use for binary invocations when `$REPO_ROOT` is removed? Likely the output/working directory for the current run.
- [Affects R9][Needs research] Should `MakeBestCLI()`/`fullrun.go` pipeline work in standalone mode, or is it explicitly dev/test-only? If dev-only, document that explicitly.

## Next Steps

-> `/ce:plan` for structured implementation planning
