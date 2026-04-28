package generator

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMCPRegisterNovelFeatureToolsEmitted verifies that the generator emits
// MCP tool registrations for each NovelFeature, plus the shell-out helpers,
// only when novel features exist. The empty-features case is exercised by
// the golden suite (every standard fixture has no novel features today and
// the goldens lock in the empty-body shape).
func TestMCPRegisterNovelFeatureToolsEmitted(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("noveltest")
	outputDir := filepath.Join(t.TempDir(), "noveltest-pp-cli")
	gen := New(apiSpec, outputDir)
	gen.NovelFeatures = []NovelFeature{
		{
			Name:        "Snapshot fanout",
			Command:     "snapshot",
			Description: "Look up a company across multiple sources in one call.",
			Rationale:   "Saves agent round-trips.",
		},
		{
			Name:        "Form D related-person graph",
			Command:     "funding --who",
			Description: "Show every Form D filing that names a given person.",
			Rationale:   "Reveals serial founders.",
		},
		{
			Name:        "Funding cadence",
			Command:     "funding-trend",
			Description: "Time series of Form D filings for a company.",
			Rationale:   "Spots silent-quarter signals.",
		},
	}
	require.NoError(t, gen.Generate())

	tools, err := os.ReadFile(filepath.Join(outputDir, "internal", "mcp", "tools.go"))
	require.NoError(t, err)
	content := string(tools)

	// Function declaration with body
	assert.Contains(t, content, "func RegisterNovelFeatureTools(s *server.MCPServer) {")

	// One s.AddTool call per novel feature, with snake_case tool names derived
	// from each Command. The "funding --who" command must collapse to
	// "funding_who" (no leading dashes, single underscore between tokens).
	assert.Contains(t, content, `mcplib.NewTool("snapshot",`)
	assert.Contains(t, content, `mcplib.NewTool("funding_who",`)
	assert.Contains(t, content, `mcplib.NewTool("funding_trend",`)

	// Each tool gets the args-string parameter
	assert.Contains(t, content, `mcplib.WithString("args"`)

	// Handler dispatch passes the original command spec verbatim — this is
	// what shellOutToCLI splits and prepends to user args. Preserves the
	// "funding --who" form so the CLI sees the right subcommand+flag pair.
	assert.Contains(t, content, `shellOutToCLI("snapshot")`)
	assert.Contains(t, content, `shellOutToCLI("funding --who")`)
	assert.Contains(t, content, `shellOutToCLI("funding-trend")`)

	// Helpers must emit when novel features exist
	assert.Contains(t, content, "func siblingCLIPath()")
	assert.Contains(t, content, "func shellOutToCLI(commandSpec string)")
	assert.Contains(t, content, "func splitShellArgs(s string)")

	// Sibling CLI binary name must match the spec's CLI name
	assert.Contains(t, content, `const cliName = "noveltest-pp-cli"`)

	// Env-var fallback uses the API's prefix (uppercased, hyphens to underscores)
	assert.Contains(t, content, `os.Getenv("NOVELTEST_CLI_PATH")`)

	// os/exec import must be present (only when novel features exist)
	assert.Contains(t, content, `"os/exec"`)

	// main.go always calls RegisterNovelFeatureTools — wiring stays uniform
	main, err := os.ReadFile(filepath.Join(outputDir, "cmd", "noveltest-pp-mcp", "main.go"))
	require.NoError(t, err)
	assert.Contains(t, string(main), "mcptools.RegisterNovelFeatureTools(s)")
}

// TestMCPNovelFeatureToolNameSanitization pins the snake-case tool-name
// derivation across the corner cases the catalog actually uses.
func TestMCPNovelFeatureToolNameSanitization(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"snapshot":         "snapshot",
		"funding-trend":    "funding_trend",
		"funding --who":    "funding_who",
		"compare":          "compare",
		"signal":           "signal",
		"FUNDING --WHO":    "funding_who", // uppercase folds
		"  weird   spaces": "weird_spaces",
		"trailing-":        "trailing", // trailing underscore stripped
		"":                 "",         // empty stays empty
	}

	apiSpec := minimalSpec("sanitize")
	outputDir := filepath.Join(t.TempDir(), "sanitize-pp-cli")
	gen := New(apiSpec, outputDir)
	for command := range cases {
		if command == "" {
			continue
		}
		gen.NovelFeatures = append(gen.NovelFeatures, NovelFeature{
			Name:        "Test " + command,
			Command:     command,
			Description: "test feature",
		})
	}
	require.NoError(t, gen.Generate())

	tools, err := os.ReadFile(filepath.Join(outputDir, "internal", "mcp", "tools.go"))
	require.NoError(t, err)
	content := string(tools)

	for command, want := range cases {
		if command == "" || want == "" {
			continue
		}
		assert.True(t,
			strings.Contains(content, `mcplib.NewTool("`+want+`",`),
			"command %q should produce tool name %q", command, want)
	}
}
