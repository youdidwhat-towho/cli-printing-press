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
	bin := buildPrintingPressBinary(t)
	dir := t.TempDir()

	require.NoError(t, os.MkdirAll(filepath.Join(dir, "internal", "cli"), 0o755))

	// Minimal source: search (without --max-time) and tonight (with --max-time)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "internal", "cli", "search.go"), []byte(`package cli
import "github.com/spf13/cobra"
func newSearchCmd() *cobra.Command {
	var limit int
	cmd := &cobra.Command{Use: "search <query>"}
	cmd.Flags().IntVar(&limit, "limit", 10, "Max results")
	return cmd
}
`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "internal", "cli", "tonight.go"), []byte(`package cli
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
`), 0o644))

	// SKILL claims search --max-time (the bug).
	skill := `---
name: pp-fixture
description: "fixture"
---

# Fixture

` + "```bash" + `
fixture-pp-cli search "chicken" --max-time 30m
` + "```" + `
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(skill), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".printing-press.json"), []byte(`{"cli_name":"fixture-pp-cli"}`), 0o644))

	out, err := exec.Command(bin, "verify-skill", "--dir", dir).CombinedOutput()
	require.Error(t, err, "verifier must exit non-zero for a SKILL with an undeclared flag")
	exitErr, ok := err.(*exec.ExitError)
	require.True(t, ok)
	require.Equal(t, 1, exitErr.ExitCode(), "exit 1 signals findings (not usage error)")
	require.Contains(t, string(out), "--max-time is declared elsewhere but not on search",
		"diagnostic must name the exact mismatch so the skill reader knows what to fix")
}

// TestVerifySkill_PassesWhenSkillMatches confirms the verifier doesn't
// false-positive on a well-formed CLI.
func TestVerifySkill_PassesWhenSkillMatches(t *testing.T) {
	bin := buildPrintingPressBinary(t)
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "internal", "cli"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "internal", "cli", "search.go"), []byte(`package cli
import "github.com/spf13/cobra"
func newSearchCmd() *cobra.Command {
	var limit int
	cmd := &cobra.Command{Use: "search <query>"}
	cmd.Flags().IntVar(&limit, "limit", 10, "Max results")
	return cmd
}
`), 0o644))
	skill := "---\nname: pp-fixture\n---\n\n# Fixture\n\n```bash\nfixture-pp-cli search \"chicken\" --limit 5\n```\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(skill), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".printing-press.json"), []byte(`{"cli_name":"fixture-pp-cli"}`), 0o644))

	out, err := exec.Command(bin, "verify-skill", "--dir", dir).CombinedOutput()
	require.NoError(t, err, "clean SKILL should exit 0, got: %s", string(out))
	require.Contains(t, string(out), "All checks passed")
}

// TestVerifySkill_RejectsMissingInputs confirms usage errors (code 2).
func TestVerifySkill_RejectsMissingInputs(t *testing.T) {
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
