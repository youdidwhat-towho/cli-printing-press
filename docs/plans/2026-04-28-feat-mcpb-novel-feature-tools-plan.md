---
title: "MCPB feature parity: expose novel CLI features as MCP tools"
type: feat
status: locked
date: 2026-04-28
related:
  - cli-printing-press#355 (machine: MCPB manifest emission)
  - public-library spike/mcpb-company-goat (validation)
---

# MCPB feature parity: expose novel CLI features as MCP tools

## Goal

A user who installs an `<api>-pp-mcp.mcpb` bundle in Claude Desktop can call every novel hand-written CLI feature through the MCP, not just the spec-driven endpoints. The MCPB bundle delivers feature parity with the companion CLI; "MCPB install" stops being a hollow shell and becomes the actual product.

Concrete acceptance test (using company-goat as the canonical case): a Claude Desktop user installs `company-goat-pp-mcp.mcpb`, optionally fills in `GITHUB_TOKEN` / `COMPANIES_HOUSE_API_KEY`, and can call `snapshot`, `funding`, `signal`, `search`, `compare`, `funding-trend`, and `funding --who` as MCP tools — not just `filings_list`. No separate `go install` needed.

## Background

PR #355 (machine) shipped MCPB manifest emission, friendly server identity, the substitution-bug fix, the `bundle` subcommand, and the context-tool capability boundary. Validated end-to-end via the spike branch on the public library where `company-goat-pp-cli` was hand-patched and tested in Claude Desktop.

The spike surfaced three problems with company-goat's MCP surface:

1. **Substitution bug** — `filings_list` sent literal `{cik}` to SEC EDGAR. Fixed at machine-level in #355.
2. **Tool description framing** — the `filings_list` description ("seed call when resolving a company's…") implied it was a stepping stone to other commands, biasing Claude toward "use the CLI for the real work."
3. **Tool surface narrowness** — only 1 API endpoint (`filings_list`) exposed; the 7 novel features (`snapshot`, `funding`, `signal`, `search`, `compare`, `funding-trend`, `funding --who`) live in `internal/cli/` but were never wrapped as MCP tools.

Issue #2 is partially addressed by the `cli_only_capabilities` rename + `tool_surface` field in `handleContext`, but that only signals the boundary to agents — it doesn't widen the surface. Issue #3 is the iceberg: every printed CLI with novel features ships an MCP that's missing the actual product capability. Across the public library, ~20 of the 25 MCP-shipping CLIs have novel features; none expose them.

This plan addresses #3 via a shell-out approach: each novel feature becomes an MCP tool whose handler invokes the companion CLI binary. The MCPB bundle ships both binaries (CLI + MCP). Per-CLI hand-coding is zero — the generator emits everything from `.printing-press.json`'s `novel_features[]` array.

## Why shell-out (Path 2) over refactor (Path 1)

A "refactor" approach would split each novel command's body into "pure work" and "CLI presentation" so the MCP can call the pure work directly and format as JSON. That's the architecturally cleaner answer but requires per-CLI hand-coding for ~20 CLIs across hundreds of novel commands. Months of work.

Shell-out instead: the MCP tool handler runs `<bundle>/bin/<api>-pp-cli <command> <args>` and returns the CLI's stdout. The CLI's existing `--json` flag (already universal in printed CLIs) gives structured output the agent can parse. Per-CLI cost is zero. Generator-emittable from existing metadata.

The trade-off: the MCP tool surface is "args: string" rather than typed parameters per feature. Agents handle this fine — constructing CLI args is a core agent skill. Claude Desktop's "view tool" UI shows a single textarea. We can migrate individual high-traffic features to typed schemas later if demand surfaces; the shell-out scaffolding doesn't preclude that.

## Phases

### Phase 1 — Machine (cli-printing-press PR)

Ships the generator-side capability. After this lands, every fresh `printing-press generate` produces a CLI whose MCP tool surface includes all novel features. Existing CLIs in the public library are unaffected until Phase 2.

**Files to change:**

- `internal/spec/spec.go`
  - No struct changes; existing `MCPConfig` and the manifest's `novel_features[]` already carry name/command/description.
- `internal/generator/templates/mcp_tools.go.tmpl`
  - New section: `RegisterNovelFeatureTools(s)` function emitted whenever `.NovelFeatures` is non-empty. One `s.AddTool(...)` per entry, schema is `args: string` (required). Handler is `shellOutToCLI(<command>, <argsTransform>)`.
  - Each feature's `Description` becomes the tool description. `Name` becomes the tool name (snake-cased). `Command` (e.g. `funding --who`) becomes the constant string the handler prepends to user-supplied args.
- `internal/generator/templates/mcp_tools.go.tmpl` (continued)
  - New helper at file bottom: `siblingCLIPath()` resolves the companion CLI binary by inspecting `os.Args[0]` and walking to the conventional sibling path (`bin/<api>-pp-cli`). Falls back to `<API>_CLI_PATH` env var, then to PATH lookup of `<api>-pp-cli`.
  - New helper: `shellOutToCLI(command string, baseArgs []string) server.ToolHandlerFunc` — captures the command + base args in a closure, the returned handler runs `exec.CommandContext(ctx, cliPath, baseArgs..., userArgs...)` and returns CombinedOutput.
- `internal/generator/templates/main_mcp.go.tmpl`
  - Conditionally call `mcptools.RegisterNovelFeatureTools(s)` after the existing `mcptools.RegisterTools(s)` when the spec has novel features.
- `internal/pipeline/mcpb_bundle.go`
  - `BuildMCPBBundle` extended to ZIP both binaries (CLI + MCP) when both exist on disk. New optional `BundleParams.CLIBinaryPath` field; if set, ZIPs at `bin/<api>-pp-cli`.
- `internal/cli/bundle.go`
  - `bundle` subcommand and `autoBundleForHost` extended to build the CLI binary alongside the MCP binary, then pass both paths to `BuildMCPBBundle`.
  - `--cli-skip-build` and `--cli-binary` flags mirror the existing `--skip-build` and `--binary` shape.
- `internal/pipeline/mcpb_manifest.go`
  - `MCPBManifest` and `buildMCPBManifest` unchanged. The manifest's `entry_point` still points at the MCP binary; the CLI binary is invisible to the host.
- Tests
  - `internal/spec/spec_test.go` — verify the template renders novel-feature tools when `novel_features[]` is populated.
  - `internal/pipeline/mcpb_bundle_test.go` — bundle includes both binaries when both paths supplied.
  - `internal/cli/bundle_test.go` — `--cli-skip-build` flag handling.
- Goldens
  - Extend `testdata/golden/fixtures/golden-api.yaml` with one or two synthetic novel features so the generated `tools.go` snapshot exercises the new emission.
  - Update `testdata/golden/expected/generate-golden-api/...` to reflect the new tools and the bundle's two-binary contents.

**Decisions (locked 2026-04-28):**

| Decision | Locked answer |
|---|---|
| Args schema | Single `args: string` parameter; agent constructs CLI args. Per-feature typed schemas deferred to Phase 3. |
| CLI binary discovery | 3-tier fallback: sibling (`os.Args[0]` dirname → `bin/<api>-pp-cli`) → `<API>_CLI_PATH` env var → PATH lookup. |
| Args parsing | `kballard/go-shellquote` split (or equivalent) on user-supplied string. |
| Stdout capture | `CombinedOutput()` (stderr merged into result). |
| Failure mode | Non-zero CLI exit → `mcplib.NewToolResultError(combinedOutput)`. |
| Default to enabled | Yes — auto-emit `RegisterNovelFeatureTools` whenever `novel_features[]` is non-empty. No opt-in flag. |
| `cli_binary` field in manifest | Include (`"cli_binary": "<api>-pp-cli"`) for documentation. |

**Out of scope for Phase 1:**
- Per-feature typed argument schemas (Phase 3)
- MCP `progress` notifications for long-running novel features
- Streaming subprocess output (returns full output at end)
- Interactive auth flows that need user input mid-command (composed-auth CLIs)

**Definition of done:**
- `go test ./...` passing (was 2342, expect ~2350+)
- `golangci-lint run ./...` clean
- `scripts/golden.sh verify` passing with new fixture coverage
- A fresh `printing-press generate --spec testdata/golden/fixtures/golden-api.yaml --output /tmp/golden` produces a CLI whose MCP exposes both spec endpoints AND the synthetic novel features as separate tools
- Bundle produced via `printing-press bundle /tmp/golden` contains `bin/printing-press-golden-pp-cli` and `bin/printing-press-golden-pp-mcp`
- Manual test: install the resulting `.mcpb` in Claude Desktop, observe the novel-feature tools appear in the connector list

### Phase 2 — Library codemod (printing-press-library)

Applies the Phase 1 generator's new emission to the existing 25 MCP-shipping CLIs in the public library. No full regenerate — additive edits only, customizations preserved.

**Per-CLI changes (executed by parallel subagents, one CLI per agent):**

For each CLI under `library/<category>/<slug>/`:

1. **Read** `.printing-press.json` to determine whether the CLI has `novel_features[]`. If empty/missing, skip novel-feature steps but still apply manifest + name + context updates.
2. **NEW `manifest.json`** at the CLI root. Use the new `printing-press` binary's manifest emitter (or a one-shot `printing-press generate-manifest --dir <cli-dir>` subcommand if we add one).
3. **EDIT `cmd/<api>-pp-mcp/main.go`**:
   - Update `server.NewMCPServer(...)` first arg to the friendly display name.
   - Add `mcptools.RegisterNovelFeatureTools(s)` call after the existing `RegisterTools(s)` call when novel features exist.
4. **EDIT `internal/mcp/tools.go`**:
   - **Substitution-fix sweep** (only the 5 affected CLIs: pagliacci-pizza 29 sites, cal-com 13, movie-goat 4, flightgoat 2, company-goat 1): rewrite each `makeAPIHandler(...path-with-{name}..., []string{})` to include the path placeholders.
   - **handleContext refactor** (all 25): replace the function body with the new shape (`tool_surface`, `cli_only_capabilities` rename, `via: "cli"` markers).
   - **Append `RegisterNovelFeatureTools` + `shellOutToCLI` + `siblingCLIPath`** at the end of the file when novel features exist. These are mechanically generated from `.printing-press.json`'s `novel_features[]`.
5. **Verify locally** per CLI: `go build ./...`, `go vet ./...` in the CLI's directory.
6. **Open PR** in the public library: branch `mcpb-feature-parity/<api>`, title `feat(<api>): MCPB feature-parity codemod`.

**Codemod orchestration:**

- One controller agent reads the CLI inventory, decides which subagents handle which CLIs, dispatches one subagent per CLI (or per CLI cluster if grouping by similar shape).
- Each subagent gets:
  - The CLI's path
  - The exact recipe (this section)
  - The Phase 1 helper code (cliutil functions if any)
- Subagents work in parallel, each producing one PR.
- Controller monitors PR creation, surfaces failures.

**No-touch rule:** the codemod does NOT modify:
- `internal/cli/` (hand-written CLI commands stay as-is)
- `internal/client/`, `internal/store/`, `internal/source/`
- `README.md`, `SKILL.md`, `tools-manifest.json` (inputs, not outputs)
- `.manuscripts/`, `dogfood-results.json`, `workflow-verify-report.json`
- `cmd/<api>-pp-cli/main.go` (CLI binary entry point)

**Definition of done per CLI:**
- New `manifest.json` parses as valid JSON and has the expected `user_config` fields.
- `cmd/<api>-pp-mcp/main.go` `server.NewMCPServer(<friendly>, ...)` matches the catalog's display_name.
- `internal/mcp/tools.go` builds clean (`go build ./...` from CLI dir).
- Substitution bug fixed (only 5 affected CLIs).
- Novel-feature tools registered (≈20 of 25 CLIs).
- PR opened against `printing-press-library:main`.

**Definition of done across the codemod:**
- 25 PRs open in the public library, each green CI.
- Each PR title follows `type(scope): description` convention.
- A spot-check of 3 CLIs (one with substitution fix, one with novel features, one without) installs cleanly in Claude Desktop and exposes the expected tools.

### Phase 3 — Optional follow-ups

After Phases 1 and 2 ship, these become tractable:

- **Per-feature typed argument schemas.** For high-traffic features (e.g., company-goat's `funding`, dub's `links lint`), migrate from single-string args to structured fields. Requires per-feature flag-parsing intelligence in the generator.
- **MCP `progress` notifications** for long-running novel features (search across many sources, sync, etc.). Requires the shell-out handler to stream output.
- **Tool description audit** (workstream E from the original codemod scope). With novel features now exposed, the framing-drift problem changes shape — descriptions need to reflect the expanded surface, not the narrow one. Doing E before this phase would mean rewriting twice.
- **Goreleaser MCPB cross-platform packaging.** Generate `.mcpb` artifacts on every release for darwin-arm64/amd64, linux-amd64/arm64, windows-amd64. Requires per-CLI `.goreleaser.yaml` updates across the public library.
- **Path 1 refactor** of selected CLIs (extract pure work from CLI presentation, MCP calls work directly). Reserve for the CLIs where shell-out latency or output size becomes painful.

## Risks & mitigations

| Risk | Mitigation |
|---|---|
| Shell-out latency makes simple novel features feel slow | The CLI binary already starts in <100ms; `exec.Command` overhead is negligible. Profile if it becomes a complaint. |
| Bundle size doubles (CLI + MCP both included) | Both are Go binaries (~10-15 MB each → ~25 MB compressed). Acceptable for an installer. |
| Args-as-string is too loose; agents pass malformed flags | Document the expected shape per tool in the description. Agents already construct shell args reliably. Add an example in each description (auto-generated from the spec). |
| `siblingCLIPath` lookup fails on unconventional bundle layouts | Three-tier fallback: sibling → env var → PATH. Documented in the doctor command. |
| Per-CLI codemod produces 25 PRs reviewers can't keep up with | Subagents auto-merge low-risk PRs after CI green if the user opts in; otherwise queue them with shared review ownership. |
| Customizations in `internal/mcp/tools.go` get overwritten | Codemod only edits well-known generator-emitted regions (handleContext body, append at file end). It greps for the `RegisterTools` boundary and edits within those landmarks. |
| Subagent variance produces inconsistent PRs | Controller agent provides the exact recipe and a verification checklist. Each subagent must run `go build` + `go vet` before opening its PR. |

## Effort estimate

| Phase | Estimated effort | Complexity |
|---|---|---|
| Phase 1 (machine) | 1-2 days focused work | Medium — new template, new pipeline plumbing, goldens |
| Phase 2 (library codemod) | 1 day to scaffold + 1 day for 25 parallel subagents to apply + 1-2 days for review/merge | Low per-PR, high coordination |
| Phase 3 (follow-ups) | Multi-week, demand-driven | Variable per item |

Total Phase 1 + 2: roughly 4-5 days of focused work. Phase 3 follow-ups are not on the critical path.

## Resolved decisions (was: open questions)

All decisions locked 2026-04-28:

1. **PR batching: hybrid 2-PR** — PR 1 ships substitution-bug fix across 5 affected CLIs (urgent track). PR 2 ships MCPB-enable + novel-feature tools across the remaining ~20 MCP-shipping CLIs.
2. **`cli_binary` field in manifest:** include it.
3. **CLIs without MCP** (`instacart`, `agent-capture`, `slack`, `linear`, `archive-is`, `steam-web`, `hubspot`, `trigger-dev`, `postman-explore`): skip entirely. They're CLI-only by design — adding an empty MCP creates dead surface.
4. **`tools-manifest.json`:** input-only. `.printing-press.json`'s `novel_features[]` is the source of truth; `tools-manifest.json` keeps documenting only spec-driven tools.

## Hand-off after Phase 2

When 25 PRs are open and CI green:

> MCPB feature parity codemod complete. 25 PRs open in `printing-press-library`. Each adds manifest.json, friendly server name, capability-boundary context tool, and (where applicable) novel-feature MCP tools. Bug-fix subset (5 CLIs) also corrects substitution-bug tool registrations. Spot-check on company-goat in Claude Desktop confirms snapshot/funding/etc. now callable as MCP tools. Ready for review / merge.

Reviewers can land PRs in any order; each is independent. After all 25 land, the public library is MCPB-feature-complete. Phase 3 follow-ups (typed args, goreleaser, description audit) become discretionary work driven by user feedback.
