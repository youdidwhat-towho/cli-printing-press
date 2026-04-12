package generator

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRootLongIncludesTopNovelFeatures asserts the generated root.go sets
// a Long description naming the top novel features plus --agent and doctor
// pointers, so agents running `<cli> --help` can pick the right command
// without a second discovery round.
func TestRootLongIncludesTopNovelFeatures(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("helped")
	outputDir := filepath.Join(t.TempDir(), "helped-pp-cli")
	gen := New(apiSpec, outputDir)
	gen.Narrative = &ReadmeNarrative{
		Headline: "Every feature plus a local store",
	}
	gen.NovelFeatures = []NovelFeature{
		{Command: "portfolio perf", Description: "Compute unrealized P&L across synced lots"},
		{Command: "digest --watchlist tech", Description: "Biggest movers across a watchlist"},
		{Command: "auth login-chrome", Description: "Import a Chrome session when rate-limited"},
		{Command: "compare", Description: "Side-by-side quote comparison"}, // should be truncated
	}
	require.NoError(t, gen.Generate())

	rootGo, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "root.go"))
	require.NoError(t, err)
	content := string(rootGo)

	assert.True(t, strings.Contains(content, "Highlights (not in the official API docs):"),
		"root Long should introduce the highlights section")
	assert.True(t, strings.Contains(content, "portfolio perf"),
		"root Long should include the first novel feature")
	assert.True(t, strings.Contains(content, "digest --watchlist tech"),
		"root Long should include the second novel feature")
	assert.True(t, strings.Contains(content, "auth login-chrome"),
		"root Long should include the third novel feature")
	assert.False(t, strings.Contains(content, "Side-by-side quote comparison"),
		"root Long should cap at top 3 novel features; the fourth should not appear")
	assert.True(t, strings.Contains(content, "add --agent to any command"),
		"root Long should point at --agent mode for agent consumers")
	assert.True(t, strings.Contains(content, "helped-pp-cli doctor"),
		"root Long should point at doctor for auth/connectivity checks")
	assert.True(t, strings.Contains(content, "Every feature plus a local store"),
		"root Short and Long should incorporate the narrative headline")
}

// TestRootLongFallsBackWhenNoNarrative asserts a sensible generic Long is
// emitted when no narrative or novel features exist — no hallucinated
// highlights, just pointer to --agent and doctor.
func TestRootLongFallsBackWhenNoNarrative(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("plain")
	outputDir := filepath.Join(t.TempDir(), "plain-pp-cli")
	gen := New(apiSpec, outputDir)
	require.NoError(t, gen.Generate())

	rootGo, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "root.go"))
	require.NoError(t, err)
	content := string(rootGo)

	assert.True(t, strings.Contains(content, "Manage plain resources via the plain API."),
		"fallback Long should restate the API")
	assert.True(t, strings.Contains(content, "Add --agent to any command"),
		"fallback Long should still point at --agent")
	assert.True(t, strings.Contains(content, "plain-pp-cli doctor"),
		"fallback Long should still point at doctor")
	assert.False(t, strings.Contains(content, "Highlights (not in the official API docs):"),
		"fallback Long should not render a Highlights header when no novel features exist")
}
