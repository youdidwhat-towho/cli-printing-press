# Dogfood Testing Reference

> **When to read:** Phase 4.5 of the printing-press skill, after shipcheck passes.
> This replaces the old Phase 5 (Optional Live Smoke) with structured testing.

## Overview

Dogfood testing runs the CLI against the real API as an actual user would.
Shipcheck verifies that commands start and return exit codes. Dogfood verifies
that they produce correct, useful output for real workflows.

The user chooses the depth before testing begins. Both levels require an API key.

## Depth Levels

### Quick Check (5-10 min)

Verifies core commands produce correct output against the live API. No mutations.

**What it tests:**
1. **Auth**: `doctor` shows valid credentials
2. **List endpoints**: The 3-5 highest-traffic list commands return data (not empty, not error)
3. **Sync round-trip**: `sync` → `sql "SELECT count(*) FROM user_bookings"` → count > 0
4. **Search**: `search "<term from synced data>"` → finds the result
5. **Output modes**: Pick one list command and test `--json`, `--select <2 fields>`, `--compact`, `--csv`
6. **Transcendence**: At least one transcendence command produces output from synced data (e.g., `health`, `today --date <date-with-data>`)

**What it skips:**
- Mutations (create, update, delete, cancel)
- Full booking/entity lifecycle
- Edge cases and error paths

**Pass criteria:** All 6 checks produce non-empty, correctly-formatted output. Any failure → investigate and fix before proceeding.

### Full Dogfood (15-30 min)

Tests a complete entity lifecycle including mutations, sync verification, and
all output modes. Requires explicit user permission since mutations affect their account.

**What it tests:**

Everything in Quick Check, plus:

7. **Create**: Create a test entity via the API (booking, issue, record — whatever
   the primary mutable entity is). Use obviously-test data ("Dogfood Test User",
   "dogfood@test.example.com").

8. **Verify creation**: List command shows the new entity. Get-by-ID returns full details.

9. **Sync + search**: Re-sync. Search for the test entity by name. Verify it appears
   in transcendence commands (`today --date`, `health`, `sql`).

10. **Mutate**: Test 2-3 mutation subcommands on the test entity:
    - Update/modify (reschedule, edit, etc.)
    - Re-sync and verify the change is reflected
    - Cancel/delete
    - Re-sync and verify status change

11. **Output fidelity**: For each tested command, verify:
    - `--json` produces valid JSON (pipe through `python3 -c "import json,sys; json.load(sys.stdin)"`)
    - `--select <fields>` returns only those fields
    - `--csv` produces CSV with header row
    - `--compact` returns fewer fields than full output
    - Table output (default terminal) is readable

12. **Error paths**: Test 2-3 expected error cases:
    - Get a non-existent ID → meaningful error message (not stack trace)
    - Missing required flag → help text shown
    - Invalid filter value → clear error

13. **Incremental sync**: Run `sync` twice. Second run should be faster (fewer items fetched).
    Verify with: `sync` → note count → create entity via API → `sync` → count increased by 1.

**Pass criteria:** All 13 checks pass. Mutations successfully execute against the real API.
Entity lifecycle completes: create → verify → mutate → sync → verify change → delete → verify deletion.

## Running the Dogfood

### Before starting

1. API key must be available (from the API Key Gate in Phase 0.5)
2. Shipcheck must have passed (at least `ship-with-gaps`)
3. CLI binary must be built and working

### Step 1: Ask the user

Present via `AskUserQuestion`:

> "Shipcheck passed. How thoroughly should I test against the live API?"
>
> 1. **Quick check** — Read-only tests: list, sync, search, output modes (~5 min)
> 2. **Full dogfood** — Complete lifecycle with mutations: create, modify, cancel, sync verification (~15-30 min). I'll create test entities on your account.
> 3. **Skip testing** — Proceed to publish

If the user selects "Full dogfood", confirm: "I'll create test data on your account (test bookings, test records, etc.) and clean up by cancelling/deleting them. OK to proceed?"

### Step 2: Execute the test plan

For each test, print the command being run and its result. Use a clear format:

```
[1/6] doctor
  $ cal-com-pp-cli doctor
  PASS: Config ok, API reachable, credentials valid

[2/6] bookings list
  $ cal-com-pp-cli bookings --no-cache --json | head -3
  PASS: 3 bookings returned

[3/6] sync + sql
  $ cal-com-pp-cli sync --full
  $ cal-com-pp-cli sql "SELECT count(*) as n FROM user_bookings"
  PASS: 3 rows synced
```

### Step 3: Fix issues inline

When a test fails:
1. Diagnose the root cause immediately
2. Fix it in the printed CLI
3. Re-run the failing test
4. Note whether the fix is CLI-specific or a machine issue (for the retro)

Do NOT accumulate failures and fix them later. Fix each one as you find it.
This is the value of dogfood — you discover issues in context and fix them
while you understand the API's behavior.

### Step 4: Report results

After all tests complete, print a summary:

```
Dogfood Results: <cli-name>
  Level: Quick Check / Full Dogfood
  Tests: N/N passed
  Fixes applied: M
    - [list each fix with 1-line description]
  Machine issues found: K
    - [list issues for the retro]
```

### Common failure patterns

| Symptom | Likely cause | Fix location |
|---------|-------------|--------------|
| All list commands return empty | Response envelope not unwrapped | Client or output helpers |
| `--select` strips everything | filterFields can't parse envelope | Add extractResponseData call |
| `--csv` shows JSON | CSV check after JSON pipe check | Promoted template output path |
| `search` returns no results | FTS table not wired into search cmd | search.go switch statement |
| `sync` gets 404 on some endpoints | API version header mismatch | Client header per-path |
| Mutation command requires ugly name | operationId not cleaned up | Command Use: field |
| `<cmd> --help` shows wrong example | Example field has placeholder values | Command Example: field |
| `me` shows "0 results" | Provenance counter assumes array | Count single objects as 1 |

## What NOT to test

- Internal implementation details (store schema, migration order)
- Performance benchmarks
- Concurrent access
- Edge cases that require specific account setup (team features, org hierarchy)
- Endpoints the user doesn't have access to (org-level when user is individual)
