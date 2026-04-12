package pipeline

import (
	"os"
	"path/filepath"
	"testing"
)

// TestRegisteredCommandFiles_OrphanIgnored verifies that scoreWorkflows and
// scoreInsight no longer count files whose constructor is never registered in
// root.go. This prevents dead-code removal from dropping the score and ensures
// orphaned command files don't inflate it either.
func TestRegisteredCommandFiles_OrphanIgnored(t *testing.T) {
	dir := t.TempDir()
	cliDir := filepath.Join(dir, "internal", "cli")
	if err := os.MkdirAll(cliDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// root.go registers only `newStaleCmd`. `newDeadCmd` exists but isn't added.
	writeFile(t, filepath.Join(cliDir, "root.go"), `package cli
import "github.com/spf13/cobra"
func newRootCmd() *cobra.Command {
	rootCmd := &cobra.Command{Use: "x"}
	rootCmd.AddCommand(newStaleCmd(nil))
	return rootCmd
}`)

	writeFile(t, filepath.Join(cliDir, "stale.go"), `package cli
import "github.com/spf13/cobra"
func newStaleCmd(flags any) *cobra.Command {
	return &cobra.Command{Use: "stale"}
}`)

	writeFile(t, filepath.Join(cliDir, "dead.go"), `package cli
import "github.com/spf13/cobra"
func newDeadCmd(flags any) *cobra.Command {
	return &cobra.Command{Use: "dead"}
}`)

	// With only stale.go registered, workflows should count 1 (not 2).
	registered := registeredCommandFiles(cliDir)
	if !registered["stale.go"] {
		t.Errorf("expected stale.go to be registered, got %v", registered)
	}
	if registered["dead.go"] {
		t.Errorf("expected dead.go to NOT be registered (orphan), got %v", registered)
	}
	if registered["root.go"] {
		t.Errorf("root.go itself should not be in registered set (it has no newXxxCmd)")
	}
}

// TestRegisteredCommandFiles_FallsOpenOnMissingRoot verifies graceful handling
// when root.go is missing or unparseable — older CLIs and partial trees must
// still score, not return zero.
func TestRegisteredCommandFiles_FallsOpenOnMissingRoot(t *testing.T) {
	dir := t.TempDir()
	cliDir := filepath.Join(dir, "internal", "cli")
	if err := os.MkdirAll(cliDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// No root.go at all.
	writeFile(t, filepath.Join(cliDir, "stale.go"), `package cli
func newStaleCmd() {}`)

	registered := registeredCommandFiles(cliDir)
	if len(registered) != 0 {
		t.Errorf("expected empty map when root.go is missing, got %v", registered)
	}
}

// TestScoreWorkflows_IgnoresOrphanFile is the integration-level guard — the
// workflows dimension must not count a dead-code file just because its name
// matches a workflow prefix.
func TestScoreWorkflows_IgnoresOrphanFile(t *testing.T) {
	dir := t.TempDir()
	cliDir := filepath.Join(dir, "internal", "cli")
	if err := os.MkdirAll(cliDir, 0o755); err != nil {
		t.Fatal(err)
	}

	writeFile(t, filepath.Join(cliDir, "root.go"), `package cli
import "github.com/spf13/cobra"
func run() {
	rootCmd := &cobra.Command{}
	rootCmd.AddCommand(newStaleCmd(nil))
}`)
	// Registered workflow command
	writeFile(t, filepath.Join(cliDir, "stale.go"), `package cli
import "github.com/spf13/cobra"
func newStaleCmd(flags any) *cobra.Command { return &cobra.Command{} }`)
	// Orphan — filename matches workflow prefix, but not registered
	writeFile(t, filepath.Join(cliDir, "search_query.go"), `package cli
import "github.com/spf13/cobra"
func newSearchQueryCmd(flags any) *cobra.Command { return &cobra.Command{} }`)

	score := scoreWorkflows(dir)
	// Exactly one registered workflow-prefix file → score 2 (per scoreWorkflows
	// rubric: >=1 compound command → 2). The orphan search_query.go must not
	// bump this to 4.
	if score != 2 {
		t.Errorf("expected score=2 (one registered workflow), got %d — orphan likely counted", score)
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
