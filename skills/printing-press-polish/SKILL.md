---
name: printing-press-polish
description: >
  Polish a generated CLI to pass verification and become publish-ready. Runs
  diagnostics (dogfood, verify, scorecard, go vet), automatically fixes all
  issues (verify failures, dead code, descriptions, README), reports the
  before/after delta, and offers to publish. Use after any /printing-press run,
  or on any CLI in ~/printing-press/library/. Trigger phrases: "polish",
  "improve the CLI", "fix verify", "make it publish-ready", "clean up the CLI",
  "get this ready to ship".
allowed-tools:
  - Bash
  - Read
  - Glob
  - Grep
  - Write
  - Edit
  - Agent
  - AskUserQuestion
---

# /printing-press-polish

Fix a generated CLI so it passes verification and is ready to publish. This skill
does the work that `/printing-press-retro` *identifies* for the machine -- but
applied to the CLI itself.

The retro improves the generator. Polish improves the generated CLI.

```bash
/printing-press-polish redfin
/printing-press-polish redfin-pp-cli
/printing-press-polish ~/printing-press/library/redfin-pp-cli
```

## When to run

After any `/printing-press` generation, especially when:
- The shipcheck verdict is `ship-with-gaps`
- The verify pass rate is below 80%
- The scorecard is below 85
- You want the CLI publish-ready in one pass

Can also be run standalone on any CLI in `~/printing-press/library/`.

## Setup

```bash
PRESS_HOME="$HOME/printing-press"
PRESS_LIBRARY="$PRESS_HOME/library"
```

### Resolve CLI

The argument can be:
- A short name: `redfin` (searches for `redfin-pp-cli` in `$PRESS_LIBRARY`)
- A full name: `redfin-pp-cli` (looks up `$PRESS_LIBRARY/redfin-pp-cli`)
- A path: `~/printing-press/library/redfin-pp-cli` (used directly)

Resolution order:
1. If the argument is an absolute or `~`-prefixed path and exists, use it
2. Try `$PRESS_LIBRARY/<arg>` (exact match)
3. Try `$PRESS_LIBRARY/<arg>-pp-cli` (append suffix)
4. Fuzzy search: `ls $PRESS_LIBRARY/ | grep -i <arg>` for close matches

If no match or multiple matches, present via `AskUserQuestion`. Show at most 4
matches sorted by modification time (most recent first) with human-friendly
relative timestamps (e.g., "generated 2 hours ago").

```bash
CLI_DIR="<resolved path>"
CLI_NAME="$(basename "$CLI_DIR")"

# Verify it's a valid Go CLI
if [ ! -f "$CLI_DIR/go.mod" ]; then
  echo "Not a valid CLI directory: $CLI_DIR"
  exit 1
fi

echo "Polishing: $CLI_NAME"
echo "Location: $CLI_DIR"
```

## Phase 1: Diagnostics

Run all diagnostic tools to establish a baseline. Capture output for comparison.

```bash
cd "$CLI_DIR"

# Find the spec if one exists in manuscripts
API_SLUG="${CLI_NAME%-pp-cli}"
SPEC_PATH=""
for f in "$PRESS_HOME/manuscripts/$API_SLUG"/*/research/*.yaml "$PRESS_HOME/manuscripts/$API_SLUG"/*/research/*.json; do
  if [ -f "$f" ]; then
    SPEC_PATH="$f"
    break
  fi
done

SPEC_FLAG=""
if [ -n "$SPEC_PATH" ]; then
  SPEC_FLAG="--spec $SPEC_PATH"
fi
```

### 1.1 Run diagnostics

```bash
# Build the binary first
go build -o "$CLI_NAME" ./cmd/"$CLI_NAME" 2>&1

# Run all four diagnostic tools
printing-press dogfood --dir "$CLI_DIR" $SPEC_FLAG 2>&1 | tee /tmp/polish-dogfood.txt
printing-press verify --dir "$CLI_DIR" $SPEC_FLAG --json 2>&1 | tee /tmp/polish-verify.json
printing-press scorecard --dir "$CLI_DIR" $SPEC_FLAG 2>&1 | tee /tmp/polish-scorecard.txt
go vet ./... 2>&1 | tee /tmp/polish-govet.txt
```

### 1.2 Parse findings

Parse the diagnostic output into categorized findings:

| Category | Source | Example |
|----------|--------|---------|
| Verify failures | verify --json | Command "pulse" dry-run FAIL, exec FAIL |
| Dead code | dogfood | 15 dead functions in helpers.go |
| Dead flags | dogfood | 7 flags declared but never read |
| Stale files | dogfood + grep | promoted_stingray.go not registered |
| README gaps | scorecard | README score 5/10 |
| Description issues | dogfood | Root Short is "Reverse-engineered..." |
| Example gaps | dogfood | analyze-zips missing example |
| Data pipeline | verify | Data pipeline FAIL |
| Go vet issues | go vet | Unused variables, unreachable code |

### 1.3 Report baseline

Present the baseline clearly:

```
Baseline for <CLI_NAME>:
  Scorecard:    XX/100 (Grade X)
  Verify:       XX% (N/M passed)
  Dogfood:      PASS/FAIL
  Go vet:       N issues

Findings:
  [N] verify failures
  [N] dead code items
  [N] description/README issues
  [N] other issues
```

## Phase 2: Fix

Fix everything automatically in priority order. Do not ask for approval. The user
reviews the diff after all fixes are applied.

### Priority 1: Verify failures

The most common verify failure pattern is cobra `Args:` constraints that prevent
the verify tool from testing commands with no positional args. The verify tool
runs `<binary> <cmd> --dry-run` and `<binary> <cmd> --json` with no positional
args.

**Fix strategy:**

For each command that fails verify dry-run or exec:

1. Read the command file (e.g., `internal/cli/pulse.go`)
2. Find `Args: cobra.ExactArgs(N)` or similar constraint in the cobra.Command struct
3. Remove the `Args:` field entirely
4. Add at the top of `RunE`:
   ```go
   if len(args) == 0 {
       return cmd.Help()
   }
   ```
5. For commands needing 2+ args (like `compare-hoods`), use `if len(args) < 2`
6. For commands that use only flags (no positional args), ensure they show help
   and exit 0 when required flags are missing

Also check for dry-run nil-data crashes:
- If a command makes API calls and the dry-run response is nil, add a guard:
  ```go
  if flags.dryRun {
      return nil
  }
  ```

### Priority 2: Dead code

For each dead function flagged by dogfood:

1. Grep all `.go` files to verify the function is truly unused (not just its
   definition matching itself)
2. If truly unused: remove the function
3. If used internally by another helper: leave it (dogfood false positive)
4. After removal, check for unused imports and remove them
5. Delete any stale files (e.g., promoted commands that are no longer registered
   in root.go)

### Priority 3: CLI description and metadata

1. Read the root command's `Short` field in `internal/cli/root.go`
2. If it contains generator boilerplate (e.g., "Reverse-engineered...", raw API
   title), rewrite it to be user-friendly:
   - Pattern: `"<Product> CLI with <key-capability-1>, <key-capability-2>, and <key-capability-3>"`
   - Example: `"Redfin real estate CLI with offline search, market analysis, and portfolio tracking"`
3. Check all commands for missing `Example` fields. Add realistic examples with
   domain-specific values.

### Priority 4: README

1. Read the current README.md
2. If it uses the generator's template with placeholder names or generic examples,
   rewrite it:
   - Title: CLI name
   - One-line description matching the root Short
   - Install section with `go install` command
   - Quick start with 3-5 real usage examples
   - Command list organized by category (from `--help` output)
   - Output format section showing `--json`, `--select`, `--dry-run`
   - Configuration section
3. Use actual command names and realistic example values from the API domain

### Priority 5: Remaining dogfood issues

Address any remaining issues flagged by dogfood:
- Path validity mismatches
- Auth protocol mismatches
- Example drift (examples that reference wrong commands)
- Data pipeline integrity issues

### After all fixes

```bash
# Rebuild
go build -o "$CLI_NAME" ./cmd/"$CLI_NAME"

# Format
gofmt -w .
```

## Phase 3: Re-diagnose

Re-run all diagnostic tools on the fixed CLI.

```bash
printing-press dogfood --dir "$CLI_DIR" $SPEC_FLAG 2>&1 | tee /tmp/polish-dogfood-after.txt
printing-press verify --dir "$CLI_DIR" $SPEC_FLAG --json 2>&1 | tee /tmp/polish-verify-after.json
printing-press scorecard --dir "$CLI_DIR" $SPEC_FLAG 2>&1 | tee /tmp/polish-scorecard-after.txt
go vet ./... 2>&1 | tee /tmp/polish-govet-after.txt
```

## Phase 4: Report delta

Present the before/after comparison:

```
Polish Results for <CLI_NAME>:

                    Before    After     Delta
  Scorecard:        XX/100    XX/100    +N
  Verify:           XX%       XX%       +N%
  Dogfood:          FAIL      PASS
  Go vet issues:    N         N         -N

Fixes applied:
  - Fixed N commands for no-arg handling (verify)
  - Removed N dead functions
  - Rewrote CLI description
  - Updated README
  - [other fixes]

Remaining issues:
  - [any issues that couldn't be fixed automatically]
```

## Phase 5: Publish offer

If the final scorecard is >= 65 and verify is PASS (or >= 80%):

Present via `AskUserQuestion`:

> "<CLI_NAME> polished: scorecard XX/100, verify XX%. Ready to publish?"
>
> 1. **Publish now** — validate, package, and open a PR to printing-press-library
> 2. **Polish again** — run another fix pass on remaining issues
> 3. **Done for now** — CLI is at ~/printing-press/library/<cli-name>

If the verdict was `ship-with-gaps` or there are remaining issues, prepend:
"Note: some issues remain (see above)."

### If "Publish now"

Check for existing PR:
```bash
gh pr list --repo mvanhorn/printing-press-library --head "feat/$CLI_NAME" --state open --author @me --json number,url --jq '.[0]' 2>/dev/null
```

Then invoke `/printing-press publish <cli-name>`.

### If "Polish again"

Re-run Phase 2-4. Maximum 2 additional polish passes.

### If "Done for now"

End normally.

## Rules

- Fix everything. Do not ask for approval before fixing.
- Report results honestly. Show what improved and what didn't.
- Do not add new features. Polish fixes quality issues, not feature gaps.
- Do not re-run research or generation. Polish works with the CLI as-is.
- Do not modify the printing-press generator. That's `/printing-press-retro`.
- Prefer mechanical fixes over creative decisions. When a creative decision is
  needed (like the CLI description), use the research brief from manuscripts if
  available to inform the choice.
- Maximum 3 total polish passes (initial + 2 retries).
