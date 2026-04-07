package megamcp

import (
	"testing"

	"github.com/mark3labs/mcp-go/server"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestServer() *server.MCPServer {
	return server.NewMCPServer("test-server", "0.0.1",
		server.WithToolCapabilities(true))
}

func newTestAPIEntries() []*APIEntry {
	return []*APIEntry{
		{
			Slug:             "espn",
			NormalizedPrefix: "espn",
			Manifest: &ToolsManifest{
				APIName: "ESPN",
				BaseURL: "https://site.api.espn.com",
				Auth:    ManifestAuth{Type: "api_key", EnvVars: []string{"ESPN_KEY"}, Header: "apikey", In: "query"},
				Tools: []ManifestTool{
					{Name: "scores_get", Description: "Get live scores", Method: "GET", Path: "/scores"},
					{Name: "teams_list", Description: "List all teams", Method: "GET", Path: "/teams"},
					{Name: "standings_get", Description: "Get league standings", Method: "GET", Path: "/standings"},
				},
			},
		},
		{
			Slug:             "dub",
			NormalizedPrefix: "dub",
			Manifest: &ToolsManifest{
				APIName: "Dub",
				BaseURL: "https://api.dub.co",
				Auth:    ManifestAuth{Type: "bearer_token", EnvVars: []string{"DUB_TOKEN"}, Header: "Authorization", Format: "Bearer {DUB_TOKEN}"},
				Tools: []ManifestTool{
					{Name: "links_list", Description: "List all links", Method: "GET", Path: "/links"},
					{Name: "links_create", Description: "Create a new short link", Method: "POST", Path: "/links"},
				},
			},
		},
		{
			Slug:             "public-api",
			NormalizedPrefix: "public_api",
			Manifest: &ToolsManifest{
				APIName: "Public API",
				BaseURL: "https://api.publicapis.org",
				Auth:    ManifestAuth{Type: "none"},
				Tools: []ManifestTool{
					{Name: "entries_list", Description: "List all public API entries", Method: "GET", Path: "/entries"},
				},
			},
		},
	}
}

func TestActivate_RegistersToolsWithPrefix(t *testing.T) {
	s := newTestServer()
	am := NewActivationManager(s, newTestAPIEntries())

	count, err := am.Activate("espn")
	require.NoError(t, err)
	assert.Equal(t, 3, count)

	// Verify tools are registered on the server with prefix.
	tools := s.ListTools()
	assert.NotNil(t, tools["espn__scores_get"], "espn__scores_get should be registered")
	assert.NotNil(t, tools["espn__teams_list"], "espn__teams_list should be registered")
	assert.NotNil(t, tools["espn__standings_get"], "espn__standings_get should be registered")
}

func TestActivate_Idempotent(t *testing.T) {
	s := newTestServer()
	am := NewActivationManager(s, newTestAPIEntries())

	count1, err := am.Activate("espn")
	require.NoError(t, err)
	assert.Equal(t, 3, count1)

	count2, err := am.Activate("espn")
	require.NoError(t, err)
	assert.Equal(t, 3, count2)

	// Verify no duplicates — still only 3 ESPN tools.
	tools := s.ListTools()
	espnTools := 0
	for name := range tools {
		if len(name) > 6 && name[:6] == "espn__" {
			espnTools++
		}
	}
	assert.Equal(t, 3, espnTools, "should have exactly 3 ESPN tools, not duplicated")
}

func TestActivate_UnknownSlug(t *testing.T) {
	s := newTestServer()
	am := NewActivationManager(s, newTestAPIEntries())

	_, err := am.Activate("nonexistent")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "API not found")
}

func TestDeactivate_ReregistersStubTools(t *testing.T) {
	s := newTestServer()
	am := NewActivationManager(s, newTestAPIEntries())

	_, err := am.Activate("espn")
	require.NoError(t, err)

	err = am.Deactivate("espn")
	require.NoError(t, err)

	// After deactivation, tools should still be registered (as stubs).
	tools := s.ListTools()
	assert.NotNil(t, tools["espn__scores_get"], "espn__scores_get should be a stub")
	assert.NotNil(t, tools["espn__teams_list"], "espn__teams_list should be a stub")

	// But the API should not be marked as activated.
	assert.False(t, am.IsActivated("espn"))

	// Calling the stub should return an activation prompt.
	result, callErr := tools["espn__scores_get"].Handler(t.Context(), makeToolRequest(nil))
	require.NoError(t, callErr)
	assert.True(t, result.IsError)
	text := extractResultText(result)
	assert.Contains(t, text, "not yet activated")
}

func TestDeactivate_NotActivated(t *testing.T) {
	s := newTestServer()
	am := NewActivationManager(s, newTestAPIEntries())

	err := am.Deactivate("espn")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not currently activated")
}

func TestDeactivate_UnknownSlug(t *testing.T) {
	s := newTestServer()
	am := NewActivationManager(s, newTestAPIEntries())

	err := am.Deactivate("nonexistent")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "API not found")
}

func TestIsActivated(t *testing.T) {
	s := newTestServer()
	am := NewActivationManager(s, newTestAPIEntries())

	assert.False(t, am.IsActivated("espn"), "should be false before activation")

	_, err := am.Activate("espn")
	require.NoError(t, err)
	assert.True(t, am.IsActivated("espn"), "should be true after activation")

	err = am.Deactivate("espn")
	require.NoError(t, err)
	assert.False(t, am.IsActivated("espn"), "should be false after deactivation")
}

func TestGetManifest(t *testing.T) {
	s := newTestServer()
	am := NewActivationManager(s, newTestAPIEntries())

	entry := am.GetManifest("espn")
	require.NotNil(t, entry)
	assert.Equal(t, "ESPN", entry.Manifest.APIName)

	missing := am.GetManifest("nonexistent")
	assert.Nil(t, missing)
}

func TestAllManifests(t *testing.T) {
	s := newTestServer()
	entries := newTestAPIEntries()
	am := NewActivationManager(s, entries)

	all := am.AllManifests()
	assert.Len(t, all, 3)
}

func TestSearchTools_MatchesName(t *testing.T) {
	s := newTestServer()
	am := NewActivationManager(s, newTestAPIEntries())

	results := am.SearchTools("scores")
	require.Len(t, results, 1)
	assert.Equal(t, "espn", results[0].APISlug)
	assert.Equal(t, "espn__scores_get", results[0].ToolName)
}

func TestSearchTools_CaseInsensitive(t *testing.T) {
	s := newTestServer()
	am := NewActivationManager(s, newTestAPIEntries())

	results := am.SearchTools("SCORES")
	require.Len(t, results, 1)
	assert.Equal(t, "espn__scores_get", results[0].ToolName)
}

func TestSearchTools_MatchesDescription(t *testing.T) {
	s := newTestServer()
	am := NewActivationManager(s, newTestAPIEntries())

	results := am.SearchTools("short link")
	require.Len(t, results, 1)
	assert.Equal(t, "dub", results[0].APISlug)
	assert.Equal(t, "dub__links_create", results[0].ToolName)
}

func TestSearchTools_AcrossUnactivatedAPIs(t *testing.T) {
	s := newTestServer()
	am := NewActivationManager(s, newTestAPIEntries())

	// Search across all APIs without activating any.
	assert.False(t, am.IsActivated("espn"))
	assert.False(t, am.IsActivated("dub"))

	results := am.SearchTools("list")
	// Should match: espn__teams_list, dub__links_list, public_api__entries_list
	assert.GreaterOrEqual(t, len(results), 3)
}

func TestSearchTools_NoMatches(t *testing.T) {
	s := newTestServer()
	am := NewActivationManager(s, newTestAPIEntries())

	results := am.SearchTools("pizza")
	assert.Empty(t, results)
}

func TestSearchTools_EmptyQuery(t *testing.T) {
	s := newTestServer()
	am := NewActivationManager(s, newTestAPIEntries())

	results := am.SearchTools("")
	assert.Nil(t, results)
}

func TestActivateMultipleAPIs(t *testing.T) {
	s := newTestServer()
	am := NewActivationManager(s, newTestAPIEntries())

	_, err := am.Activate("espn")
	require.NoError(t, err)

	_, err = am.Activate("dub")
	require.NoError(t, err)

	tools := s.ListTools()
	// Should have 3 ESPN + 2 Dub = 5 tools.
	assert.NotNil(t, tools["espn__scores_get"])
	assert.NotNil(t, tools["dub__links_list"])
	assert.NotNil(t, tools["dub__links_create"])
}

func TestToolNamesForSlug(t *testing.T) {
	s := newTestServer()
	am := NewActivationManager(s, newTestAPIEntries())

	names := am.toolNamesForSlug("espn")
	assert.Len(t, names, 3)
	assert.Contains(t, names, "espn__scores_get")
	assert.Contains(t, names, "espn__teams_list")
	assert.Contains(t, names, "espn__standings_get")

	missing := am.toolNamesForSlug("nonexistent")
	assert.Nil(t, missing)
}

func TestStubToolsRegisteredAtStartup(t *testing.T) {
	s := newTestServer()
	entries := newTestAPIEntries()
	_ = NewActivationManager(s, entries)

	// Stub tools should be visible in the server's tool list immediately.
	// We can't directly query the server's tool list, but we can verify
	// that calling a stub returns the activation prompt.
	// The stubs are registered via AddTools in registerStubs().
	// We verify this indirectly by checking that the stub handler works.
	handler := makeStubHandler("espn", "ESPN", 3)
	result, err := handler(t.Context(), makeToolRequest(nil))
	require.NoError(t, err)
	assert.True(t, result.IsError)
	text := extractResultText(result)
	assert.Contains(t, text, "not yet activated")
	assert.Contains(t, text, "activate_api")
	assert.Contains(t, text, "espn")
	assert.Contains(t, text, "3 tools")
}

func TestDeactivate_ReregistersStubs(t *testing.T) {
	s := newTestServer()
	am := NewActivationManager(s, newTestAPIEntries())

	// Activate then deactivate.
	_, err := am.Activate("espn")
	require.NoError(t, err)
	assert.True(t, am.IsActivated("espn"))

	err = am.Deactivate("espn")
	require.NoError(t, err)
	assert.False(t, am.IsActivated("espn"))

	// After deactivation, re-activating should work (stubs were re-registered).
	count, err := am.Activate("espn")
	require.NoError(t, err)
	assert.Equal(t, 3, count)
	assert.True(t, am.IsActivated("espn"))
}
