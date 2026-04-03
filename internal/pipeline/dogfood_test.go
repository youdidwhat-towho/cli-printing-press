package pipeline

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunDogfood(t *testing.T) {
	dir := t.TempDir()

	require.NoError(t, os.MkdirAll(filepath.Join(dir, "internal", "cli"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "internal", "client"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "internal", "store"), 0o755))

	writeTestFile(t, filepath.Join(dir, "internal", "cli", "root.go"), `package cli
type rootFlags struct {
	jsonOutput bool
	csvOutput  bool
	stdinInput bool
	noCache    bool
	deadOnly   bool
}
func initFlags(flags *rootFlags) {
	_ = &flags.jsonOutput
	_ = &flags.csvOutput
	_ = &flags.stdinInput
	_ = &flags.noCache
	_ = &flags.deadOnly
}
func configure(flags *rootFlags) {
	if flags.noCache {
		disableCache()
	}
}
`)
	writeTestFile(t, filepath.Join(dir, "internal", "cli", "helpers.go"), `package cli
func usedHelper() {}
func deadHelper() {}
`)
	writeTestFile(t, filepath.Join(dir, "internal", "cli", "users_list.go"), `package cli
func usersList() {
	path := "/users/123"
	flags.jsonOutput = true
	usedHelper()
}
`)
	writeTestFile(t, filepath.Join(dir, "internal", "cli", "projects_get.go"), `package cli
func projectsGet() {
	path := "/bogus"
}
`)
	writeTestFile(t, filepath.Join(dir, "internal", "cli", "sync.go"), `package cli
func runSync(s interface{ UpsertUsers() error }) error {
	return s.UpsertUsers()
}
`)
	writeTestFile(t, filepath.Join(dir, "internal", "cli", "search.go"), `package cli
func runSearch(s interface{ SearchUsers() error }) error {
	return s.SearchUsers()
}
`)
	writeTestFile(t, filepath.Join(dir, "internal", "client", "client.go"), `package client
func authHeader(token string) string {
	return "Bearer " + token
}
`)
	writeTestFile(t, filepath.Join(dir, "internal", "store", "store.go"), "package store\n"+
		"func schema() string {\n"+
		"\treturn `\n"+
		"\t\tCREATE TABLE IF NOT EXISTS users (\n"+
		"\t\t\tid TEXT PRIMARY KEY,\n"+
		"\t\t\tname TEXT NOT NULL,\n"+
		"\t\t\temail TEXT,\n"+
		"\t\t\tdata JSON NOT NULL\n"+
		"\t\t);\n"+
		"\t\tCREATE TABLE IF NOT EXISTS sync_state (\n"+
		"\t\t\tentity_type TEXT PRIMARY KEY,\n"+
		"\t\t\tlast_sync_at TEXT NOT NULL,\n"+
		"\t\t\tcursor TEXT\n"+
		"\t\t);\n"+
		"\t`\n"+
		"}\n")

	specPath := filepath.Join(dir, "spec.json")
	writeTestFile(t, specPath, `{
  "paths": {
    "/users/{id}": {},
    "/projects/{id}": {}
  },
  "components": {
    "securitySchemes": {
      "BotToken": {
        "type": "http",
        "scheme": "bearer"
      }
    }
  }
}`)

	report, err := RunDogfood(dir, specPath)
	require.NoError(t, err)

	assert.Equal(t, "FAIL", report.Verdict)
	assert.Equal(t, 2, report.PathCheck.Tested)
	assert.Equal(t, 1, report.PathCheck.Valid)
	assert.Equal(t, 50, report.PathCheck.Pct)
	assert.Equal(t, []string{"/bogus"}, report.PathCheck.Invalid)
	assert.False(t, report.AuthCheck.Match)
	assert.Equal(t, 5, report.DeadFlags.Total)
	assert.Equal(t, 3, report.DeadFlags.Dead)
	assert.Equal(t, []string{"csvOutput", "deadOnly", "stdinInput"}, report.DeadFlags.Items)
	assert.Equal(t, 2, report.DeadFuncs.Total)
	assert.Equal(t, 1, report.DeadFuncs.Dead)
	assert.Equal(t, []string{"deadHelper"}, report.DeadFuncs.Items)
	assert.True(t, report.PipelineCheck.SyncCallsDomain)
	assert.True(t, report.PipelineCheck.SearchCallsDomain)
	assert.Equal(t, 1, report.PipelineCheck.DomainTables)
	assert.Equal(t, 0, report.ExampleCheck.Tested)
	assert.True(t, report.ExampleCheck.Skipped)
	assert.Equal(t, "no CLI command directory found", report.ExampleCheck.Detail)

	loaded, err := LoadDogfoodResults(dir)
	require.NoError(t, err)
	assert.Equal(t, report.Verdict, loaded.Verdict)
}

func TestRunDogfoodAcceptsYAMLSpec(t *testing.T) {
	dir := t.TempDir()

	require.NoError(t, os.MkdirAll(filepath.Join(dir, "internal", "cli"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "internal", "client"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "internal", "store"), 0o755))

	writeTestFile(t, filepath.Join(dir, "internal", "cli", "root.go"), `package cli
type rootFlags struct{}
func initFlags(flags *rootFlags) { _ = flags }
`)
	writeTestFile(t, filepath.Join(dir, "internal", "cli", "users_get.go"), `package cli
func usersGet() {
	path := "/users/{id}"
}
`)
	writeTestFile(t, filepath.Join(dir, "internal", "client", "client.go"), `package client
func authHeader(token string) string {
	return "Bearer " + token
}
`)
	writeTestFile(t, filepath.Join(dir, "internal", "store", "store.go"), "package store\n")

	specPath := filepath.Join(dir, "spec.yaml")
	writeTestFile(t, specPath, `openapi: 3.0.0
info:
  title: Users API
  version: "1.0"
servers:
  - url: https://api.example.com
paths:
  /users/{id}:
    get:
      operationId: getUser
      parameters:
        - name: id
          in: path
          required: true
          schema:
            type: string
      responses:
        "200":
          description: ok
components:
  securitySchemes:
    bearerAuth:
      type: http
      scheme: bearer
security:
  - bearerAuth: []
`)

	report, err := RunDogfood(dir, specPath)
	require.NoError(t, err)
	assert.Equal(t, 1, report.PathCheck.Tested)
	assert.Equal(t, 1, report.PathCheck.Valid)
	assert.True(t, report.AuthCheck.Match)
}

func TestCountDomainTables(t *testing.T) {
	storeSource := `
CREATE TABLE IF NOT EXISTS users (
	id TEXT PRIMARY KEY,
	name TEXT NOT NULL,
	email TEXT,
	data JSON NOT NULL
);

CREATE TABLE IF NOT EXISTS sync_state (
	entity_type TEXT PRIMARY KEY,
	last_sync_at TEXT NOT NULL,
	cursor TEXT
);
`
	assert.Equal(t, 1, countDomainTables(storeSource))
}

func TestDeriveDogfoodVerdict(t *testing.T) {
	report := &DogfoodReport{
		PathCheck:     PathCheckResult{Tested: 10, Valid: 10, Pct: 100},
		AuthCheck:     AuthCheckResult{Match: true},
		DeadFlags:     DeadCodeResult{Dead: 1},
		DeadFuncs:     DeadCodeResult{Dead: 0},
		PipelineCheck: PipelineResult{SyncCallsDomain: true},
	}
	assert.Equal(t, "WARN", deriveDogfoodVerdict(report, true))

	report.DeadFlags.Dead = 0
	report.DeadFuncs.Dead = 1
	assert.Equal(t, "WARN", deriveDogfoodVerdict(report, true))

	report.DeadFuncs.Dead = 0
	report.PipelineCheck.SyncCallsDomain = false
	assert.Equal(t, "WARN", deriveDogfoodVerdict(report, true))

	report.PipelineCheck.SyncCallsDomain = true
	assert.Equal(t, "PASS", deriveDogfoodVerdict(report, true))

	report.ExampleCheck = ExampleCheckResult{Tested: 10, WithExamples: 4}
	assert.Equal(t, "FAIL", deriveDogfoodVerdict(report, true))

	report.ExampleCheck = ExampleCheckResult{Tested: 10, WithExamples: 5}
	assert.Equal(t, "PASS", deriveDogfoodVerdict(report, true))

	report.ExampleCheck = ExampleCheckResult{Tested: 10, WithExamples: 10, InvalidFlags: []string{"--bogus"}}
	assert.Equal(t, "WARN", deriveDogfoodVerdict(report, true))

	report.ExampleCheck = ExampleCheckResult{Skipped: true, Detail: "could not build CLI binary"}
	assert.Equal(t, "WARN", deriveDogfoodVerdict(report, true))

	report.ExampleCheck = ExampleCheckResult{Tested: 10, WithExamples: 10, ValidExamples: 10}
	assert.Equal(t, "PASS", deriveDogfoodVerdict(report, true))
}

func TestExtractExamplesSection(t *testing.T) {
	tests := []struct {
		name string
		help string
		want string
	}{
		{
			name: "standard cobra help",
			help: "Some command\n\nUsage:\n  cli users list [flags]\n\nExamples:\n  # List all users\n  cli users list --limit 10\n\nFlags:\n  --limit int   max results\n",
			want: "# List all users\n  cli users list --limit 10",
		},
		{
			name: "no examples section",
			help: "Some command\n\nUsage:\n  cli version\n\nFlags:\n  -h, --help   help\n",
			want: "",
		},
		{
			name: "examples before global flags",
			help: "Examples:\n  cli foo --bar baz\n\nGlobal Flags:\n  --config string\n",
			want: "cli foo --bar baz",
		},
		{
			name: "multi-line examples",
			help: "Examples:\n  # First example\n  cli do --a 1\n\n  # Second example\n  cli do --b 2\n\nFlags:\n  --a int\n",
			want: "# First example\n  cli do --a 1\n\n  # Second example\n  cli do --b 2",
		},
		{
			name: "empty help",
			help: "",
			want: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, extractExamplesSection(tt.help))
		})
	}
}

func TestExtractFlagNames(t *testing.T) {
	tests := []struct {
		name string
		text string
		want []string
	}{
		{
			name: "multiple flags",
			text: "cli users list --limit 10 --format json",
			want: []string{"format", "limit"},
		},
		{
			name: "deduplication",
			text: "--flag value --flag other",
			want: []string{"flag"},
		},
		{
			name: "hyphenated flag names",
			text: "--dry-run --output-format table",
			want: []string{"dry-run", "output-format"},
		},
		{
			name: "ignores short flags",
			text: "-h --help -v --verbose",
			want: []string{"help", "verbose"},
		},
		{
			name: "no flags",
			text: "just some text with no flags",
			want: nil,
		},
		{
			name: "ignores uppercase",
			text: "--OK should not match",
			want: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, extractFlagNames(tt.text))
		})
	}
}

func TestCheckCommandTree(t *testing.T) {
	dir := t.TempDir()
	cliDir := filepath.Join(dir, "internal", "cli")
	require.NoError(t, os.MkdirAll(cliDir, 0o755))

	// Define two commands via newXxxCmd() functions
	writeTestFile(t, filepath.Join(cliDir, "foo.go"), `package cli
func newFooCmd() {}
`)
	writeTestFile(t, filepath.Join(cliDir, "bar.go"), `package cli
func newBarCmd() {}
`)

	// Without a buildable binary, checkCommandTree can't verify registration,
	// so it treats all as registered. We test the scanning logic directly.
	result := checkCommandTree(dir)
	assert.Equal(t, 2, result.Defined)
	// No cmd/ directory means no binary, so all treated as registered
	assert.Equal(t, 2, result.Registered)
	assert.Empty(t, result.Unregistered)
}

func TestCheckCommandTree_KebabConversion(t *testing.T) {
	dir := t.TempDir()
	cliDir := filepath.Join(dir, "internal", "cli")
	require.NoError(t, os.MkdirAll(cliDir, 0o755))

	// Define subcommands with CamelCase function names
	writeTestFile(t, filepath.Join(cliDir, "api.go"), `package cli
func newApiGetCategoryCmd() {}
func newApiListTeamsCmd() {}
`)
	writeTestFile(t, filepath.Join(cliDir, "auth.go"), `package cli
func newAuthLoginCmd() {}
`)
	writeTestFile(t, filepath.Join(cliDir, "sync.go"), `package cli
func newSyncCmd() {}
`)

	result := checkCommandTree(dir)
	// Should find 4 defined commands
	assert.Equal(t, 4, result.Defined)
	// Without a buildable binary, all treated as registered
	assert.Equal(t, 4, result.Registered)
	assert.Empty(t, result.Unregistered)
}

func TestCheckConfigConsistency(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "internal", "cli"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "internal", "client"), 0o755))

	// Write site uses "AccessToken"
	writeTestFile(t, filepath.Join(dir, "internal", "cli", "auth.go"), `package cli
func saveAuth() {
	config.Set("AccessToken", token)
}
`)
	// Read site uses "DominosToken" - a mismatch
	writeTestFile(t, filepath.Join(dir, "internal", "client", "client.go"), `package client
func getAuth() string {
	return config.Get("DominosToken")
}
`)

	result := checkConfigConsistency(dir)
	assert.False(t, result.Consistent)
	assert.Contains(t, result.WriteFields, "AccessToken")
	assert.Contains(t, result.ReadFields, "DominosToken")
	assert.NotEmpty(t, result.Mismatched)
}

func TestCheckConfigConsistency_Consistent(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "internal", "cli"), 0o755))

	// Both write and read use the same field
	writeTestFile(t, filepath.Join(dir, "internal", "cli", "auth.go"), `package cli
func saveAuth() {
	config.Set("AccessToken", token)
}
func getAuth() string {
	return config.Get("AccessToken")
}
`)

	result := checkConfigConsistency(dir)
	assert.True(t, result.Consistent)
}

func TestCheckWorkflowCompleteness_NoManifest(t *testing.T) {
	dir := t.TempDir()
	result := checkWorkflowCompleteness(dir)
	assert.True(t, result.Skipped)
	assert.Contains(t, result.Detail, "no workflow_verify.yaml found")
}

func TestCheckWorkflowCompleteness_HappyPath(t *testing.T) {
	dir := t.TempDir()

	// Create a manifest with commands that "exist"
	// Since there's no cmd/ dir, the check will treat all as mapped (can't build binary)
	writeTestFile(t, filepath.Join(dir, "workflow_verify.yaml"), `workflows:
  - name: order flow
    steps:
      - command: auth login
        name: login
      - command: menu list
        name: browse menu
`)

	result := checkWorkflowCompleteness(dir)
	assert.False(t, result.Skipped)
	assert.Equal(t, 2, result.TotalSteps)
	// No cmd/ dir means no binary, so all steps treated as mapped
	assert.Equal(t, 2, result.MappedSteps)
	assert.Empty(t, result.UnmappedSteps)
}

func TestCheckWorkflowCompleteness_MissingCommand(t *testing.T) {
	// This test verifies parsing works correctly for a manifest with steps.
	// Without a buildable binary, the check can't actually verify commands,
	// so we test that the YAML parsing and step counting work.
	dir := t.TempDir()

	writeTestFile(t, filepath.Join(dir, "workflow_verify.yaml"), `workflows:
  - name: order flow
    steps:
      - command: cart checkout
        name: checkout
      - command: auth login
        name: login
`)

	result := checkWorkflowCompleteness(dir)
	assert.False(t, result.Skipped)
	assert.Equal(t, 2, result.TotalSteps)
}

func TestWiringCheckIntegration(t *testing.T) {
	dir := t.TempDir()

	require.NoError(t, os.MkdirAll(filepath.Join(dir, "internal", "cli"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "internal", "client"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "internal", "store"), 0o755))

	writeTestFile(t, filepath.Join(dir, "internal", "cli", "root.go"), `package cli
type rootFlags struct{}
func initFlags(flags *rootFlags) { _ = flags }
`)
	writeTestFile(t, filepath.Join(dir, "internal", "client", "client.go"), `package client
func authHeader(token string) string {
	return "Bearer " + token
}
`)
	writeTestFile(t, filepath.Join(dir, "internal", "store", "store.go"), "package store\n")

	report, err := RunDogfood(dir, "")
	require.NoError(t, err)

	// WiringCheck should be populated in the report
	assert.True(t, report.WiringCheck.ConfigConsist.Consistent)
	assert.True(t, report.WiringCheck.WorkflowComplete.Skipped)
	assert.Equal(t, 0, report.WiringCheck.CommandTree.Defined)
}

func TestDeriveDogfoodVerdict_WiringChecks(t *testing.T) {
	// Test that unregistered commands cause FAIL
	report := &DogfoodReport{
		PathCheck:     PathCheckResult{Tested: 10, Valid: 10, Pct: 100},
		AuthCheck:     AuthCheckResult{Match: true},
		DeadFlags:     DeadCodeResult{Dead: 0},
		DeadFuncs:     DeadCodeResult{Dead: 0},
		PipelineCheck: PipelineResult{SyncCallsDomain: true},
		WiringCheck: WiringCheckResult{
			CommandTree:      CommandTreeResult{Defined: 2, Registered: 1, Unregistered: []string{"bar"}},
			ConfigConsist:    ConfigConsistResult{Consistent: true},
			WorkflowComplete: WorkflowCompleteResult{Skipped: true},
		},
	}
	assert.Equal(t, "FAIL", deriveDogfoodVerdict(report, true))

	// Test that config inconsistency causes FAIL
	report.WiringCheck.CommandTree.Unregistered = nil
	report.WiringCheck.ConfigConsist.Consistent = false
	report.WiringCheck.ConfigConsist.Mismatched = []string{"AccessToken", "DominosToken"}
	assert.Equal(t, "FAIL", deriveDogfoodVerdict(report, true))

	// Test that unmapped workflow steps cause WARN
	report.WiringCheck.ConfigConsist.Consistent = true
	report.WiringCheck.WorkflowComplete = WorkflowCompleteResult{
		TotalSteps:    2,
		MappedSteps:   1,
		UnmappedSteps: []string{"cart checkout"},
	}
	assert.Equal(t, "WARN", deriveDogfoodVerdict(report, true))

	// Test that clean wiring passes
	report.WiringCheck.WorkflowComplete = WorkflowCompleteResult{Skipped: true}
	assert.Equal(t, "PASS", deriveDogfoodVerdict(report, true))
}

func writeTestFile(t *testing.T, path string, content string) {
	t.Helper()
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
}
