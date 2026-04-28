package cli_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestVerifySkill_DetectsWrongFlagOnCommand is the regression guard for
// PR library#66: the recipe-goat SKILL advertised `search --max-time` but
// --max-time is a `tonight` flag, not a `search` flag. This test writes a
// synthetic CLI fixture with exactly that shape and confirms
// `printing-press verify-skill` catches it at generation time instead of
// letting it ship to the library.
func TestVerifySkill_DetectsWrongFlagOnCommand(t *testing.T) {
	t.Parallel()

	bin := buildPrintingPressBinary(t)
	dir := t.TempDir()

	skill := `---
name: pp-fixture
description: "fixture"
---

# Fixture

` + "```bash" + `
fixture-pp-cli search "chicken" --max-time 30m
` + "```" + `
`
	writeVerifySkillFixture(t, dir, map[string]string{
		"search.go": `package cli
import "github.com/spf13/cobra"
func newSearchCmd() *cobra.Command {
	var limit int
	cmd := &cobra.Command{Use: "search <query>"}
	cmd.Flags().IntVar(&limit, "limit", 10, "Max results")
	return cmd
}
`,
		"tonight.go": `package cli
import (
	"github.com/spf13/cobra"
	"time"
)
func newTonightCmd() *cobra.Command {
	var maxTime time.Duration
	cmd := &cobra.Command{Use: "tonight"}
	cmd.Flags().DurationVar(&maxTime, "max-time", 0, "Max total time")
	return cmd
}
`,
	}, skill)

	out, err := exec.Command(bin, "verify-skill", "--dir", dir).CombinedOutput()
	require.Error(t, err, "verifier must exit non-zero for a SKILL with an undeclared flag")
	exitErr, ok := err.(*exec.ExitError)
	require.True(t, ok)
	require.Equal(t, 1, exitErr.ExitCode(), "exit 1 signals findings (not usage error)")
	require.Contains(t, string(out), "--max-time is declared elsewhere but not on search",
		"diagnostic must name the exact mismatch so the skill reader knows what to fix")
}

// TestVerifySkill_NoFalsePositiveOnSharedLeafName is the regression
// guard for retro #301 finding F1: when two cobra commands share a leaf
// name at different paths (e.g., a top-level `save <url>` plus a
// `profile save <name>` subcommand), the old specificity-based file
// picker silently dropped the lower-specificity file from the
// flag-declaration union check. The result was a false-positive
// `--<flag> is declared elsewhere but not on save` even though the flag
// was correctly declared on the top-level save command.
//
// This test writes a synthetic CLI with that exact shape and asserts
// the verifier does NOT report a false-positive flag-commands finding
// when the SKILL example uses a flag declared on the top-level command.
func TestVerifySkill_NoFalsePositiveOnSharedLeafName(t *testing.T) {
	t.Parallel()

	bin := buildPrintingPressBinary(t)
	dir := t.TempDir()

	skill := "---\nname: pp-fixture\n---\n\n# Fixture\n\n```bash\nfixture-pp-cli save https://example.com --tags foo,bar\n```\n"
	writeVerifySkillFixture(t, dir, map[string]string{
		"root.go": `package cli
import "github.com/spf13/cobra"
func Execute() error {
	rootCmd := &cobra.Command{Use: "fixture-pp-cli"}
	rootCmd.AddCommand(newSaveCmd())
	rootCmd.AddCommand(newProfileCmd())
	return rootCmd.Execute()
}
`,
		"save_cmd.go": `package cli
import "github.com/spf13/cobra"
func newSaveCmd() *cobra.Command {
	var tags string
	cmd := &cobra.Command{Use: "save <url>"}
	cmd.Flags().StringVar(&tags, "tags", "", "Comma-separated tags")
	return cmd
}
`,
		"profile.go": `package cli
import "github.com/spf13/cobra"
func newProfileCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "profile"}
	cmd.AddCommand(newProfileSaveCmd())
	return cmd
}
func newProfileSaveCmd() *cobra.Command {
	var label string
	cmd := &cobra.Command{Use: "save <name> [--<flag> <value> ...]"}
	cmd.Flags().StringVar(&label, "label", "", "Profile label")
	return cmd
}
`,
	}, skill)

	out, err := exec.Command(bin, "verify-skill", "--dir", dir).CombinedOutput()
	require.NoError(t, err, "verifier must NOT raise findings for valid shared-leaf usage; got: %s", string(out))
	require.Contains(t, string(out), "All checks passed",
		"shared-leaf disambiguation should resolve via rootCmd.AddCommand graph, not specificity heuristic")
}

// TestVerifySkill_PassesWhenSkillMatches confirms the verifier doesn't
// false-positive on a well-formed CLI.
func TestVerifySkill_PassesWhenSkillMatches(t *testing.T) {
	t.Parallel()

	bin := buildPrintingPressBinary(t)
	dir := t.TempDir()

	skill := "---\nname: pp-fixture\n---\n\n# Fixture\n\n```bash\nfixture-pp-cli search \"chicken\" --limit 5\n```\n"
	writeVerifySkillFixture(t, dir, map[string]string{
		"search.go": `package cli
import "github.com/spf13/cobra"
func newSearchCmd() *cobra.Command {
	var limit int
	cmd := &cobra.Command{Use: "search <query>"}
	cmd.Flags().IntVar(&limit, "limit", 10, "Max results")
	return cmd
}
`,
	}, skill)

	out, err := exec.Command(bin, "verify-skill", "--dir", dir).CombinedOutput()
	require.NoError(t, err, "clean SKILL should exit 0, got: %s", string(out))
	require.Contains(t, string(out), "All checks passed")
}

// TestVerifySkill_FlagDeclaredViaHelper asserts the verifier accepts a flag
// declared one level deep through a shared helper invoked with cmd as first
// arg, e.g. addTargetFlags(cmd, &t) whose body declares the flag.
func TestVerifySkill_FlagDeclaredViaHelper(t *testing.T) {
	bin := buildPrintingPressBinary(t)
	dir := t.TempDir()

	skill := "---\nname: pp-fixture\n---\n\n# Fixture\n\n```bash\nfixture-pp-cli snapshot --domain example.com\n```\n"
	writeVerifySkillFixture(t, dir, map[string]string{
		"snapshot.go": `package cli
import "github.com/spf13/cobra"
type targetFlags struct{ domain, pick string }
func newSnapshotCmd() *cobra.Command {
	var t targetFlags
	cmd := &cobra.Command{Use: "snapshot [co]"}
	addTargetFlags(cmd, &t)
	return cmd
}
`,
		"helpers.go": `package cli
import "github.com/spf13/cobra"
func addTargetFlags(cmd *cobra.Command, t *targetFlags) {
	cmd.Flags().StringVar(&t.domain, "domain", "", "Domain")
	cmd.Flags().StringVar(&t.pick, "pick", "", "Pick which source")
}
`,
		"root.go": `package cli
import "github.com/spf13/cobra"
func Execute() error {
	rootCmd := &cobra.Command{Use: "fixture-pp-cli"}
	rootCmd.AddCommand(newSnapshotCmd())
	return rootCmd.Execute()
}
`,
	}, skill)

	out, err := exec.Command(bin, "verify-skill", "--dir", dir).CombinedOutput()
	require.NoError(t, err, "verifier must accept flags declared via one level of helper indirection; got: %s", string(out))
	require.Contains(t, string(out), "All checks passed")
}

// TestVerifySkill_FlagHelperDoesNotScanAdjacentFunctions confirms helper
// matching is limited to the called helper's body. A later helper in the same
// file must not make addTargetFlags look like it declares --pick.
func TestVerifySkill_FlagHelperDoesNotScanAdjacentFunctions(t *testing.T) {
	bin := buildPrintingPressBinary(t)
	dir := t.TempDir()

	skill := "---\nname: pp-fixture\n---\n\n# Fixture\n\n```bash\nfixture-pp-cli snapshot --pick sec\n```\n"
	writeVerifySkillFixture(t, dir, map[string]string{
		"snapshot.go": `package cli
import "github.com/spf13/cobra"
func newSnapshotCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "snapshot [co]"}
	addTargetFlags(cmd)
	return cmd
}
`,
		"helpers.go": `package cli
import "github.com/spf13/cobra"
func addTargetFlags(cmd *cobra.Command) {
	var domain string
	cmd.Flags().StringVar(&domain, "domain", "", "Domain")
}

func addPickFlags(cmd *cobra.Command) {
	var pick string
	cmd.Flags().StringVar(&pick, "pick", "", "Pick which source")
}
`,
		"root.go": `package cli
import "github.com/spf13/cobra"
func Execute() error {
	rootCmd := &cobra.Command{Use: "fixture-pp-cli"}
	rootCmd.AddCommand(newSnapshotCmd())
	return rootCmd.Execute()
}
`,
	}, skill)

	out, err := exec.Command(bin, "verify-skill", "--dir", dir).CombinedOutput()
	require.Error(t, err, "verifier must not treat adjacent helper declarations as part of addTargetFlags")
	require.Contains(t, string(out), "--pick")
}

// TestVerifySkill_FlagNotDeclaredAnywhere confirms the helper-indirection
// fallback does not cover for a flag that genuinely isn't declared.
func TestVerifySkill_FlagNotDeclaredAnywhere(t *testing.T) {
	bin := buildPrintingPressBinary(t)
	dir := t.TempDir()

	// SKILL claims --pick which is not declared anywhere.
	skill := "---\nname: pp-fixture\n---\n\n# Fixture\n\n```bash\nfixture-pp-cli snapshot --pick sec\n```\n"
	writeVerifySkillFixture(t, dir, map[string]string{
		"snapshot.go": `package cli
import "github.com/spf13/cobra"
func newSnapshotCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "snapshot [co]"}
	addTargetFlags(cmd)
	return cmd
}
`,
		"helpers.go": `package cli
import "github.com/spf13/cobra"
func addTargetFlags(cmd *cobra.Command) {
	var x string
	cmd.Flags().StringVar(&x, "domain", "", "Domain")
}
`,
		"root.go": `package cli
import "github.com/spf13/cobra"
func Execute() error {
	rootCmd := &cobra.Command{Use: "fixture-pp-cli"}
	rootCmd.AddCommand(newSnapshotCmd())
	return rootCmd.Execute()
}
`,
	}, skill)

	out, err := exec.Command(bin, "verify-skill", "--dir", dir).CombinedOutput()
	require.Error(t, err, "undeclared flag must still produce a finding")
	require.Contains(t, string(out), "--pick")
}

// TestVerifySkill_RejectsMissingInputs confirms usage errors (code 2).
func TestVerifySkill_RejectsMissingInputs(t *testing.T) {
	t.Parallel()

	bin := buildPrintingPressBinary(t)

	// Missing --dir
	_, err := exec.Command(bin, "verify-skill").CombinedOutput()
	require.Error(t, err)

	// --dir without SKILL.md
	emptyDir := t.TempDir()
	out, err := exec.Command(bin, "verify-skill", "--dir", emptyDir).CombinedOutput()
	require.Error(t, err)
	require.True(t, strings.Contains(string(out), "no SKILL.md") || strings.Contains(string(out), "no internal/cli"))
}

func writeVerifySkillFixture(t *testing.T, dir string, files map[string]string, skill string) {
	t.Helper()
	cliDir := filepath.Join(dir, "internal", "cli")
	require.NoError(t, os.MkdirAll(cliDir, 0o755))
	for name, content := range files {
		require.NoError(t, os.WriteFile(filepath.Join(cliDir, name), []byte(content), 0o644))
	}
	require.NoError(t, os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(skill), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".printing-press.json"), []byte(`{"cli_name":"fixture-pp-cli"}`), 0o644))
}

// buildPrintingPressBinary compiles the printing-press binary into a test
// tempdir and returns its path. Built once per test because each test's
// TempDir is fresh; Go's test cache ensures the compile is fast.
func buildPrintingPressBinary(t *testing.T) string {
	t.Helper()
	out := filepath.Join(t.TempDir(), "printing-press")
	cmd := exec.Command("go", "build", "-o", out, "./cmd/printing-press")
	// The test runs from internal/cli; go up to repo root.
	cmd.Dir = "../.."
	if buildOut, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("building printing-press: %v\n%s", err, string(buildOut))
	}
	return out
}
