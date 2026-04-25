# Printed CLI Patch Catalog — Issue #275 + #287 (2026-04-25)

This document describes the generator fixes that landed on 2026-04-25 and how
to assess each printed CLI in `~/printing-press/library/<api>/` against them.
The audience is an agent (or human) deciding whether each affected CLI needs a
**surgical patch** (apply the fix in place, preserve all post-generation
customizations) or a **full reprint** (regenerate from spec, then re-apply
customizations on top).

The default disposition is **surgical**. Reprinting is destructive in
practice: every printed CLI has accumulated post-generation tweaks, hand-built
features, and quirks. Reprint only when surgical patching is clearly more
work than re-applying customizations onto a fresh generation.

---

## 1. Relevance summary

Six PRs landed in this batch. Only three of them changed the *shape of
generated code*; the other three were generator-binary-only changes that have
no effect on already-printed CLIs.

| PR | Bug | Touches generated code? | Per-CLI work? |
|----|------|--------------------------|----------------|
| #281 (F-1) | `--output` flag silently overridden | No (CLI flag handling in the printing-press binary) | None |
| #283 (F-2) | Param flag identifier collisions | **Yes** — templates + new dedup pass | Detect + decide |
| #284 (F-3) | Go-keyword type names | **Yes** — sanitizer prepends `T` | Detect + decide |
| #285 (F-4) | Object-shaped `description` fields | No (parser pre-pass; printed CLI never re-parses) | None |
| #286 (F-5) | Catalog hygiene + non-spec-body rejection | No (binary-only) | None |
| #288 (#287) | Body-field collisions | **Yes** — dedup pass extended to body | Detect + decide |

The rest of this document covers the three with `Yes` only.

---

## 2. Inputs you have per CLI

For each `<api>` in `~/printing-press/library/`:

| Path | Contains |
|------|----------|
| `.printing-press.json` | Provenance: `api_name`, `cli_name`, `spec_url`, `spec_checksum`, `run_id`, `printing-press` version, generation timestamp |
| `spec.yaml` or `spec.json` | The exact spec bytes the CLI was generated from (the archive — your source of truth for "what would the generator have done?") |
| `internal/cli/*.go`, `internal/types/types.go`, etc. | Standard generated layout, possibly with hand edits |
| `~/printing-press/manuscripts/<api>/<run_id>/proofs/` | Original generation artifacts, if a baseline is needed for diffing customizations |

**Important:** `~/printing-press/library/` is **not a git repo**. There is no
git history of customizations. To identify what's hand-edited vs original
generation, regenerate from the archived spec into a temp dir and diff.

---

## 3. Per-PR detection and fix recipes

Each section is self-contained: trigger, detection, surgical fix, reprint
signals. Apply them in order — F-2 first (easiest detection: the build
fails), then F-3 (build also fails), then #287 (build fails or cobra errors
at runtime).

### 3.1 PR #283 (F-2) — Param flag identifier collisions

**Trigger condition.** A spec endpoint has 2+ non-positional query/path
params where any of:

- `toCamel(name1) == toCamel(name2)` — Twilio's `StartTime`, `StartTime>`,
  `StartTime<` all collapse to `StartTime`. `start_time` and `StartTime`
  also collapse.
- A param is literally named `all` AND the endpoint declares pagination.
  Pagination's reserved `--all` flag collides.
- A param is named `wait` / `wait-timeout` / `wait-interval` AND the
  endpoint is async-detected. Async's reserved wait flags collide.

**Detection signatures:**

```bash
cd ~/printing-press/library/<api>
go build ./... 2>&1 | grep "redeclared in this block"
```

If output contains `flag<X> redeclared in this block`, F-2 hit this CLI.
Confirm specifically by inspecting the file:

```bash
# Lists files where any flag<X> identifier appears more than once
for f in internal/cli/*.go; do
  awk '/var flag[A-Z]/ {print FILENAME":"$2}' "$f"
done | sort | uniq -d
```

Spec-side detection (predicts F-2 hits without building):

```bash
# Query the archived spec for endpoints with collision-prone param names.
# Implementation depends on spec format; the camel function lives in
# internal/generator/generator.go (toCamel) — same logic.
```

**Surgical fix recipe.** For each affected file (e.g.,
`internal/cli/<resource>_<endpoint>.go`):

1. Locate the duplicate `var flag<X>` declarations near the top of the
   `new...Cmd` function. Rename second/third/etc. to `flag<X>2`, `flag<X>3`,
   ... — matching the generator's suffix convention.
2. Update **every** usage of the renamed identifier within the same function
   scope:
   - The `if cmd.Flags().Changed(...)` validation block (rename the
     `Changed("kebab-name")` arg to `"kebab-name-2"`)
   - Path replacement: `replacePathParam(path, "WireName", flag<X>)` →
     `replacePathParam(path, "WireName", flag<X>2)` (KEEP `"WireName"`)
   - Param/body map entries: `"WireName": fmt.Sprintf("%v", flag<X>2)`
     (KEEP `"WireName"` — it's the API-side query/path key)
   - Cobra registration at function bottom: `cmd.Flags().*Var(&flag<X>2,
     "kebab-name-2", ...)` — both the Go ident and the cobra flag name get
     the suffix.
3. Build: `go build ./...`. Vet: `go vet ./...`. Run `<cli> <command>
   --help` to confirm cobra accepts the new flags.

**The wire-side names (the strings inside `replacePathParam(path, "X",
...)` calls and the keys of `params["X"]` / `body["X"]` maps) MUST NOT
change.** Those are what the API expects. Only the Go identifier and the
user-facing CLI flag name change.

**Reprint signals:**
- 3+ collisions across multiple files (mechanical but error-prone surgically)
- Affected endpoints have `>` / `<` / decorator characters in param names
  (Twilio shape — generator handles cleanly via the dedup pass)
- The CLI's customizations are themselves stale (e.g., predate other
  generator improvements)

---

### 3.2 PR #284 (F-3) — Go-keyword type names

**Trigger condition.** The spec defines a schema (typically under
`components.schemas` in OpenAPI) whose name, after `sanitizeTypeName`'s
non-alphanumeric stripping, equals one of the 25 Go reserved keywords:

```
break, case, chan, const, continue, default, defer, else, fallthrough,
for, func, go, goto, if, import, interface, map, package, range, return,
select, struct, switch, type, var
```

Real-world examples: GitHub's `import` (Source Imports API) and `package`
(GitHub Packages).

**Detection signatures:**

```bash
cd ~/printing-press/library/<api>
go vet ./internal/types/ 2>&1 | grep "expected 'IDENT'"
```

If output contains `expected 'IDENT', found '<keyword>'`, F-3 hit this CLI.
Confirm by inspecting the file:

```bash
grep -nE '^type (break|case|chan|const|continue|default|defer|else|fallthrough|for|func|go|goto|if|import|interface|map|package|range|return|select|struct|switch|type|var) struct' \
  internal/types/types.go
```

**Surgical fix recipe.**

1. In `internal/types/types.go`: rename `type <keyword> struct` to
   `type T<keyword> struct` (e.g., `import` → `Timport`, `package` →
   `Tpackage`). Match the generator's exact convention: prepend `T`, do
   not capitalize the keyword.
2. Find all usages: every reference to the old type name in *any* `.go`
   file outside the `import (...)` block needs renaming. Likely sites:
   `internal/cli/*.go`, `internal/client/client.go`. Use:

   ```bash
   # Replace REPLACEME with each affected keyword
   grep -rnE '\bREPLACEME\b' --include='*.go' .
   ```

   Skip matches inside `import (...)` blocks (those are package imports,
   not type references) — the keyword as a Go syntactic token is what
   shows up there.

3. **Keep JSON struct tags as-is.** The struct is now `type Timport struct
   { ID string \`json:"id"\` }`, not changed otherwise. The JSON wire-side
   key (e.g., the response field name) is in the struct tag, not the type
   name.

**Reprint signals:**
- 5+ keyword collisions (mechanical but error-prone surgically; generator
  handles all 25 cleanly)
- Customizations rely on the original (illegal) type name, e.g., the CLI
  was hand-fixed before our PR landed and customizations reference the
  hand-fix's name — in that case, decide whether the hand-fix's name
  (often something like `ImportType`) or the generator's `Timport` is the
  long-term winner.

---

### 3.3 PR #288 (#287) — Body field collisions

**Trigger condition.** A POST/PUT/PATCH endpoint's request body has 2+
fields where any of:

- `toCamel(name1) == toCamel(name2)` — same camel-collision class as F-2,
  but in the body namespace.
- A body field's `flagName` matches a query/path param's `flagName` on the
  same endpoint. Body and params share the cobra flag-name namespace; cobra
  rejects the second registration.
- A body field is literally named `stdin` AND the endpoint is
  POST/PUT/PATCH. The template emits `cmd.Flags().BoolVar(&stdinBody,
  "stdin", ...)` for mutating methods.

**Detection signatures:**

Build-level:

```bash
cd ~/printing-press/library/<api>
go build ./... 2>&1 | grep "body[A-Z].* redeclared"
```

Cobra runtime (cross-namespace collision):

```bash
# Pick a POST/PUT/PATCH endpoint in the CLI's command tree
<cli> <some-mutating-command> --help 2>&1 | grep "already registered"
```

Spec-side (proactive):

```bash
# Find POST/PUT/PATCH endpoints whose body field names collide with each
# other or with a query/path param on the same endpoint.
```

**Surgical fix recipe.** Same shape as F-2, but for body declarations:

1. Locate duplicate `var body<X>` declarations. Rename second/third/etc. to
   `body<X>2`, `body<X>3`, ...
2. Update every usage in the same function (the JSON unmarshal block, the
   `body["WireName"] = ...` assembly map, the cobra flag binding).
3. **JSON keys (`body["WireName"]`) MUST NOT change** — they're the
   wire-side body key.
4. For cross-namespace collisions (body + param sharing a cobra flag name),
   either side can be renamed. Prefer renaming the body field to match the
   generator's behavior (params win first registration).

**Reprint signals:**
- 3+ body collisions
- Cross-namespace collision with multiple param names (each collision
  needs both a Go ident rename and a cobra flag rename)

---

## 4. Surgical-vs-reprint decision matrix

For each affected CLI, weigh:

| Factor | Surgical favored | Reprint favored |
|--------|------------------|------------------|
| Number of detected issues | 1–2 patterns | 3+ patterns or multiple PR classes hit |
| Customization volume | High (preserve them) | Low (easy to re-apply on fresh) |
| Time since last regeneration | Recent (low generator drift) | Old (high drift unrelated to these PRs) |
| Spec stability since printing | Stable (checksum match) | Spec changed (regen pulls in real upstream changes too) |
| Test coverage in the printed CLI | Strong (catches surgical errors) | Weak (silent surgical errors possible) |
| User-visible flag-name churn | Renames are acceptable | Stable flag names matter (scripts in the wild) |
| Existing hand-fix for the same bug | Drop hand-fix in favor of generator's convention | Keep hand-fix, skip the surgical rename |

**Heuristic**: surgical patch when the affected CLI has ≤2 distinct issues
AND any of these is true: the CLI has 5+ post-generation customization
commits visible by diffing against the manuscripts baseline, the
customizations include hand-built novel features, or the spec has changed
upstream since generation. Reprint when the CLI is mostly vanilla generator
output, has minimal customizations, and the spec is stable.

---

## 5. Reprint workflow (when chosen)

1. **Verify spec stability.** Compare the upstream spec to the archived one:
   ```bash
   curl -sSL "$(jq -r .spec_url .printing-press.json)" \
     | sha256sum
   sha256sum spec.yaml
   ```
   If checksums differ, the regenerate will also pull in unrelated upstream
   changes — that's a separate decision than these PR fixes.

2. **Establish a customization baseline.** From the provenance's
   `run_id`, look in `~/printing-press/manuscripts/<api>/<run_id>/proofs/`
   for the original generation snapshot. Diff:
   ```bash
   diff -ruN ~/printing-press/manuscripts/<api>/<run_id>/proofs/<api>-pp-cli/ \
            ~/printing-press/library/<api>/ \
     > /tmp/<api>-customizations.diff
   ```
   If no manuscripts baseline exists, generate one fresh from the archived
   spec as the baseline (it'll match the current state minus customizations
   minus the new fixes).

3. **Regenerate to a temp dir** using the *archived* spec (not the
   upstream URL — keeps the spec dimension stable):
   ```bash
   printing-press generate \
     --spec ~/printing-press/library/<api>/spec.yaml \
     --output /tmp/<api>-regen \
     --validate=false --force
   ```

4. **Three-way merge.** The new generation contains the PR fixes (renamed
   identifiers, T-prefixed types). The customizations diff captures
   user-added features. Apply customizations to the regenerated tree;
   resolve conflicts where customizations referenced names that the new
   generation renamed.

5. **Replace and verify**:
   ```bash
   rm -rf ~/printing-press/library/<api>
   mv /tmp/<api>-regen ~/printing-press/library/<api>
   cd ~/printing-press/library/<api>
   go build ./... && go vet ./... && ./<cli> --help && ./<cli> version && ./<cli> doctor
   ```

---

## 6. Surgical workflow (when chosen)

1. Apply each affected PR's recipe from §3.
2. After each PR's fixes:
   - `go build ./...`
   - `go vet ./...`
3. Final smoke:
   - `<cli> --help`
   - `<cli> version`
   - `<cli> doctor`
   - For each renamed flag, confirm the new name shows in `<cli> <command>
     --help` and the old name is gone.

---

## 7. What NOT to do

- **Do not bulk-regenerate** every CLI. The whole point is to preserve
  customizations.
- **Do not rename wire-side names.** URL params, JSON struct tags, body map
  keys, path placeholders are part of the API contract. They use the spec's
  original names regardless of any Go-side renames.
- **Do not apply F-4 (#285) fixes to printed CLIs.** F-4 is a
  parser-side change that affects what the *generator* can ingest, not
  what comes out. A CLI that was successfully generated already passed the
  parser; there's nothing in its source tree to patch from F-4.
- **Do not skip detection.** A CLI whose spec doesn't trigger any PR's
  conditions needs no work. Confirm via the detection signatures before
  even considering a fix.

---

## 8. Quick agent runbook

```
For each <api> in ~/printing-press/library/:
  1. cd ~/printing-press/library/<api>
  2. Run F-2 detection: go build ./... — look for "redeclared in this block"
  3. Run F-3 detection: go vet ./internal/types/ — look for "expected 'IDENT'"
  4. Run #287 detection: go build ./... — look for "body<X>.* redeclared"
                         <cli> <mutating-cmd> --help — look for "already registered"
  5. If no signatures fire → skip this CLI
  6. If signatures fire → score against §4
  7. If surgical → §6 workflow with §3 recipes
  8. If reprint → §5 workflow
  9. Verify: build + vet + smoke
```
