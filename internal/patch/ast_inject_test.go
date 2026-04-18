package patch

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// baseRootGo is a minimal root.go that mirrors the shape of every published
// printing-press CLI (rootFlags struct + Execute() with PersistentFlags,
// PersistentPreRunE, AddCommand sequence, err := rootCmd.Execute()).
const baseRootGo = `package cli

import (
	"strings"
	"time"

	"github.com/example/test/internal/client"
	"github.com/spf13/cobra"
)

var version = "1.0.0"

type rootFlags struct {
	asJSON   bool
	compact  bool
	timeout  time.Duration
}

func Execute() error {
	var flags rootFlags

	rootCmd := &cobra.Command{Use: "test-pp-cli"}

	rootCmd.PersistentFlags().BoolVar(&flags.asJSON, "json", false, "Output as JSON")
	rootCmd.PersistentFlags().BoolVar(&flags.compact, "compact", false, "Compact output")

	rootCmd.PersistentPreRunE = func(cmd *cobra.Command, args []string) error {
		if flags.asJSON {
			flags.compact = true
		}
		return nil
	}

	rootCmd.AddCommand(newFooCmd(&flags))
	rootCmd.AddCommand(newBarCmd(&flags))
	rootCmd.AddCommand(newNovelSQLCmd(&flags))
	rootCmd.AddCommand(newDoctorCmd(&flags))

	err := rootCmd.Execute()
	if err != nil && strings.Contains(err.Error(), "unknown flag") {
		return err
	}
	return err
}
`

func TestInjectRootAST_MutatesAllSixTargets(t *testing.T) {
	out, changed, err := injectRootAST([]byte(baseRootGo))
	require.NoError(t, err)
	require.True(t, changed, "expected mutation")
	src := string(out)

	assert.Contains(t, src, "profileName", "rootFlags should gain profileName field")
	assert.Contains(t, src, "deliverSpec", "rootFlags should gain deliverSpec field")
	assert.Contains(t, src, "deliverBuf", "rootFlags should gain deliverBuf field")
	assert.Contains(t, src, "deliverSink", "rootFlags should gain deliverSink field")

	assert.Contains(t, src, `"bytes"`, "should import bytes")
	assert.Contains(t, src, `"io"`, "should import io")
	assert.Contains(t, src, `"os"`, "should import os")

	assert.Contains(t, src, `"profile"`, "should register --profile flag")
	assert.Contains(t, src, `"deliver"`, "should register --deliver flag")

	assert.Contains(t, src, "flags.deliverSpec != \"\"", "PersistentPreRunE should have deliver-setup block")
	assert.Contains(t, src, "ParseDeliverSink", "PersistentPreRunE should call ParseDeliverSink")
	assert.Contains(t, src, "flags.profileName != \"\"", "PersistentPreRunE should have profile-lookup block")
	assert.Contains(t, src, "GetProfile", "PersistentPreRunE should call GetProfile")

	assert.Contains(t, src, "newProfileCmd(&flags)", "Execute should register newProfileCmd")
	assert.Contains(t, src, "newFeedbackCmd(&flags)", "Execute should register newFeedbackCmd")

	assert.Contains(t, src, "Deliver(flags.deliverSink", "Execute should flush deliverBuf after rootCmd.Execute")
}

func TestInjectRootAST_PreservesNovelCommands(t *testing.T) {
	out, _, err := injectRootAST([]byte(baseRootGo))
	require.NoError(t, err)
	src := string(out)

	// Every original AddCommand must still be present, in the same order,
	// before the new profile/feedback registrations.
	fooIdx := strings.Index(src, "newFooCmd(&flags)")
	barIdx := strings.Index(src, "newBarCmd(&flags)")
	sqlIdx := strings.Index(src, "newNovelSQLCmd(&flags)")
	doctorIdx := strings.Index(src, "newDoctorCmd(&flags)")
	profileIdx := strings.Index(src, "newProfileCmd(&flags)")
	feedbackIdx := strings.Index(src, "newFeedbackCmd(&flags)")

	require.Greater(t, fooIdx, 0, "newFooCmd must survive")
	require.Greater(t, barIdx, fooIdx, "newBarCmd order preserved")
	require.Greater(t, sqlIdx, barIdx, "newNovelSQLCmd order preserved")
	require.Greater(t, doctorIdx, sqlIdx, "newDoctorCmd order preserved")
	require.Greater(t, profileIdx, doctorIdx, "newProfileCmd inserted after last original")
	require.Greater(t, feedbackIdx, profileIdx, "newFeedbackCmd inserted after profile")
}

func TestInjectRootAST_Idempotent(t *testing.T) {
	first, changed, err := injectRootAST([]byte(baseRootGo))
	require.NoError(t, err)
	require.True(t, changed)

	second, changedAgain, err := injectRootAST(first)
	require.NoError(t, err)
	assert.False(t, changedAgain, "second run should be a no-op")
	assert.Equal(t, string(first), string(second), "second run must not alter bytes")
}

func TestInjectRootAST_PreservesVersionAndImports(t *testing.T) {
	out, _, err := injectRootAST([]byte(baseRootGo))
	require.NoError(t, err)
	src := string(out)

	assert.Contains(t, src, `var version = "1.0.0"`, "version var must survive")
	assert.Contains(t, src, `"github.com/example/test/internal/client"`, "module path import must survive unchanged")
	assert.Contains(t, src, `"github.com/spf13/cobra"`, "cobra import must survive")
}

// TestInjectRootAST_NoPersistentFlagsBlock exercises the "refuse silently"
// path: if the target root.go doesn't have the expected shape, no mutation.
func TestInjectRootAST_NoPersistentFlagsBlock(t *testing.T) {
	src := `package cli

func Execute() error {
	return nil
}
`
	out, changed, err := injectRootAST([]byte(src))
	require.NoError(t, err)
	// rootFlags struct is missing so no field mutation; no PersistentFlags
	// call so no flag mutation; no AddCommand so no command mutation.
	// No mutation should occur — returns unchanged.
	assert.False(t, changed, "no changes when target shape missing")
	assert.Equal(t, src, string(out))
}
