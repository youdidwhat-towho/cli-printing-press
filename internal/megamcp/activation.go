package megamcp

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// ActivationManager manages the state of which APIs are activated
// and handles dynamic tool registration/deregistration.
type ActivationManager struct {
	mu        sync.RWMutex
	server    *server.MCPServer
	manifests map[string]*APIEntry    // all loaded manifests by slug
	activated map[string]bool         // which slugs have been activated
	clients   map[string]*http.Client // per-API HTTP clients
}

// NewActivationManager creates a new ActivationManager with all loaded API entries.
// Registers stub tools for ALL APIs immediately so agents can discover tool names
// via tools/list. Stubs prompt the agent to call activate_api before execution.
func NewActivationManager(s *server.MCPServer, entries []*APIEntry) *ActivationManager {
	manifests := make(map[string]*APIEntry, len(entries))
	for _, entry := range entries {
		manifests[entry.Slug] = entry
	}
	am := &ActivationManager{
		server:    s,
		manifests: manifests,
		activated: make(map[string]bool),
		clients:   make(map[string]*http.Client),
	}

	// Register stub tools for all APIs so agents can see tool names in tools/list.
	am.registerStubs()

	return am
}

// registerStubs registers lightweight stub handlers for all tools across all APIs.
// When called, stubs return an activation prompt instead of making HTTP requests.
func (am *ActivationManager) registerStubs() {
	var stubs []server.ServerTool
	for _, entry := range am.manifests {
		prefix := entry.NormalizedPrefix
		slug := entry.Slug
		apiName := entry.Manifest.APIName
		toolCount := len(entry.Manifest.Tools)

		for _, tool := range entry.Manifest.Tools {
			toolName := prefix + "__" + tool.Name

			toolOpts := []mcp.ToolOption{
				mcp.WithDescription(SanitizeText(tool.Description, 500)),
			}
			for _, param := range tool.Params {
				paramOpts := []mcp.PropertyOption{
					mcp.Description(SanitizeText(param.Description, 200)),
				}
				if param.Required {
					paramOpts = append(paramOpts, mcp.Required())
				}
				toolOpts = append(toolOpts, mcp.WithString(param.Name, paramOpts...))
			}

			mcpTool := mcp.NewTool(toolName, toolOpts...)
			handler := makeStubHandler(slug, apiName, toolCount)
			stubs = append(stubs, server.ServerTool{Tool: mcpTool, Handler: handler})
		}
	}

	if len(stubs) > 0 {
		am.server.AddTools(stubs...)
	}
}

// makeStubHandler returns a handler that prompts the agent to activate the API first.
func makeStubHandler(slug, apiName string, toolCount int) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return mcp.NewToolResultError(
			fmt.Sprintf("This API is not yet activated. Call activate_api(%q) first to enable %d tools for %s.",
				slug, toolCount, apiName),
		), nil
	}
}

// Activate registers real HTTP handlers for the given API slug, replacing
// the stub handlers. Returns the number of tools registered. Idempotent:
// calling twice for the same slug returns the same count without duplicating tools.
func (am *ActivationManager) Activate(slug string) (int, error) {
	am.mu.Lock()
	defer am.mu.Unlock()

	entry, ok := am.manifests[slug]
	if !ok {
		return 0, fmt.Errorf("API not found: %q", slug)
	}

	if am.activated[slug] {
		// Already activated — return existing tool count.
		return len(entry.Manifest.Tools), nil
	}

	// Create a per-API HTTP client with SSRF-safe dialer.
	client := SafeHTTPClient(30 * time.Second)
	am.clients[slug] = client

	// Delete stubs first, then register real handlers.
	prefix := entry.NormalizedPrefix
	var stubNames []string
	var tools []server.ServerTool
	for _, tool := range entry.Manifest.Tools {
		toolName := prefix + "__" + tool.Name
		stubNames = append(stubNames, toolName)

		// Build MCP tool definition with parameter schema.
		toolOpts := []mcp.ToolOption{
			mcp.WithDescription(SanitizeText(tool.Description, 500)),
		}
		for _, param := range tool.Params {
			paramOpts := []mcp.PropertyOption{
				mcp.Description(SanitizeText(param.Description, 200)),
			}
			if param.Required {
				paramOpts = append(paramOpts, mcp.Required())
			}
			toolOpts = append(toolOpts, mcp.WithString(param.Name, paramOpts...))
		}

		mcpTool := mcp.NewTool(toolName, toolOpts...)
		handler := MakeToolHandler(entry.Manifest, tool, client, slug)
		tools = append(tools, server.ServerTool{Tool: mcpTool, Handler: handler})
	}

	// Remove stubs, then add real handlers.
	if len(stubNames) > 0 {
		am.server.DeleteTools(stubNames...)
	}
	if len(tools) > 0 {
		am.server.AddTools(tools...)
	}

	am.activated[slug] = true
	return len(entry.Manifest.Tools), nil
}

// Deactivate removes real handlers for the given API slug and re-registers stubs.
func (am *ActivationManager) Deactivate(slug string) error {
	am.mu.Lock()
	defer am.mu.Unlock()

	entry, ok := am.manifests[slug]
	if !ok {
		return fmt.Errorf("API not found: %q", slug)
	}

	if !am.activated[slug] {
		return fmt.Errorf("API %q is not currently activated", slug)
	}

	// Collect tool names to delete.
	prefix := entry.NormalizedPrefix
	var names []string
	var stubs []server.ServerTool
	apiName := entry.Manifest.APIName
	toolCount := len(entry.Manifest.Tools)

	for _, tool := range entry.Manifest.Tools {
		toolName := prefix + "__" + tool.Name
		names = append(names, toolName)

		// Re-register as stub.
		toolOpts := []mcp.ToolOption{
			mcp.WithDescription(SanitizeText(tool.Description, 500)),
		}
		for _, param := range tool.Params {
			paramOpts := []mcp.PropertyOption{
				mcp.Description(SanitizeText(param.Description, 200)),
			}
			if param.Required {
				paramOpts = append(paramOpts, mcp.Required())
			}
			toolOpts = append(toolOpts, mcp.WithString(param.Name, paramOpts...))
		}

		mcpTool := mcp.NewTool(toolName, toolOpts...)
		handler := makeStubHandler(slug, apiName, toolCount)
		stubs = append(stubs, server.ServerTool{Tool: mcpTool, Handler: handler})
	}

	// Delete real handlers, add back stubs.
	if len(names) > 0 {
		am.server.DeleteTools(names...)
	}
	if len(stubs) > 0 {
		am.server.AddTools(stubs...)
	}

	delete(am.activated, slug)
	delete(am.clients, slug)
	return nil
}

// IsActivated returns whether the given API slug is currently activated.
func (am *ActivationManager) IsActivated(slug string) bool {
	am.mu.RLock()
	defer am.mu.RUnlock()
	return am.activated[slug]
}

// GetManifest returns the APIEntry for a given slug, or nil if not found.
func (am *ActivationManager) GetManifest(slug string) *APIEntry {
	am.mu.RLock()
	defer am.mu.RUnlock()
	return am.manifests[slug]
}

// AllManifests returns all loaded API entries.
func (am *ActivationManager) AllManifests() []*APIEntry {
	am.mu.RLock()
	defer am.mu.RUnlock()
	result := make([]*APIEntry, 0, len(am.manifests))
	for _, entry := range am.manifests {
		result = append(result, entry)
	}
	return result
}

// SearchResult represents a matching tool from a search query.
type SearchResult struct {
	APISlug     string `json:"api_slug"`
	ToolName    string `json:"tool_name"`
	Description string `json:"description"`
}

// SearchTools searches across ALL manifests (not just activated) for tools
// matching the given query. Case-insensitive substring match on tool name
// and description.
func (am *ActivationManager) SearchTools(query string) []SearchResult {
	am.mu.RLock()
	defer am.mu.RUnlock()

	if query == "" {
		return nil
	}

	lowerQuery := strings.ToLower(query)
	var results []SearchResult

	for _, entry := range am.manifests {
		prefix := entry.NormalizedPrefix
		for _, tool := range entry.Manifest.Tools {
			fullName := prefix + "__" + tool.Name
			if strings.Contains(strings.ToLower(fullName), lowerQuery) ||
				strings.Contains(strings.ToLower(tool.Description), lowerQuery) {
				results = append(results, SearchResult{
					APISlug:     entry.Slug,
					ToolName:    fullName,
					Description: tool.Description,
				})
			}
		}
	}

	return results
}

// toolNamesForSlug returns the full prefixed tool names for a given API slug.
// Used for display purposes.
func (am *ActivationManager) toolNamesForSlug(slug string) []string {
	am.mu.RLock()
	defer am.mu.RUnlock()

	entry, ok := am.manifests[slug]
	if !ok {
		return nil
	}

	names := make([]string, 0, len(entry.Manifest.Tools))
	for _, tool := range entry.Manifest.Tools {
		names = append(names, entry.NormalizedPrefix+"__"+tool.Name)
	}
	return names
}
