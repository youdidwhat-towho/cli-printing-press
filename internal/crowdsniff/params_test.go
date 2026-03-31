package crowdsniff

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestEnrichWithParams(t *testing.T) {
	t.Parallel()

	t.Run("single-line params extracts keys", func(t *testing.T) {
		t.Parallel()
		content := `this.get('/path', { key1: val1, key2: val2 })`
		endpoints := []DiscoveredEndpoint{
			{Method: "GET", Path: "/path", SourceTier: TierCommunitySDK, SourceName: "sdk"},
		}

		result := EnrichWithParams(content, endpoints)

		assert.Len(t, result, 1)
		assert.Len(t, result[0].Params, 2)
		names := paramNames(result[0].Params)
		assert.Contains(t, names, "key1")
		assert.Contains(t, names, "key2")
	})

	t.Run("multi-line params spanning 4 lines", func(t *testing.T) {
		t.Parallel()
		content := `this.get('/items', {
  steamid: steamids.join(','),
  include_appinfo: includeAppInfo,
  count: 10,
  format: 'json'
})`
		endpoints := []DiscoveredEndpoint{
			{Method: "GET", Path: "/items", SourceTier: TierCommunitySDK, SourceName: "sdk"},
		}

		result := EnrichWithParams(content, endpoints)

		assert.Len(t, result, 1)
		assert.Len(t, result[0].Params, 4)
		names := paramNames(result[0].Params)
		assert.Contains(t, names, "steamid")
		assert.Contains(t, names, "include_appinfo")
		assert.Contains(t, names, "count")
		assert.Contains(t, names, "format")
	})

	t.Run("shorthand property extracts key", func(t *testing.T) {
		t.Parallel()
		content := `this.get('/path', { steamid })`
		endpoints := []DiscoveredEndpoint{
			{Method: "GET", Path: "/path", SourceTier: TierCommunitySDK, SourceName: "sdk"},
		}

		result := EnrichWithParams(content, endpoints)

		assert.Len(t, result, 1)
		assert.Len(t, result[0].Params, 1)
		assert.Equal(t, "steamid", result[0].Params[0].Name)
	})

	t.Run("join value infers string type", func(t *testing.T) {
		t.Parallel()
		content := `this.get('/path', { steamid: steamids.join(',') })`
		endpoints := []DiscoveredEndpoint{
			{Method: "GET", Path: "/path", SourceTier: TierCommunitySDK, SourceName: "sdk"},
		}

		result := EnrichWithParams(content, endpoints)

		assert.Len(t, result[0].Params, 1)
		assert.Equal(t, "string", result[0].Params[0].Type)
	})

	t.Run("boolean value infers boolean type", func(t *testing.T) {
		t.Parallel()
		content := `this.get('/path', { active: true, disabled: false })`
		endpoints := []DiscoveredEndpoint{
			{Method: "GET", Path: "/path", SourceTier: TierCommunitySDK, SourceName: "sdk"},
		}

		result := EnrichWithParams(content, endpoints)

		assert.Len(t, result[0].Params, 2)
		for _, p := range result[0].Params {
			assert.Equal(t, "boolean", p.Type, "param %s should be boolean", p.Name)
		}
	})

	t.Run("numeric default infers integer type", func(t *testing.T) {
		t.Parallel()
		content := `this.get('/path', { page: 1, size: 25 })`
		endpoints := []DiscoveredEndpoint{
			{Method: "GET", Path: "/path", SourceTier: TierCommunitySDK, SourceName: "sdk"},
		}

		result := EnrichWithParams(content, endpoints)

		assert.Len(t, result[0].Params, 2)
		for _, p := range result[0].Params {
			assert.Equal(t, "integer", p.Type, "param %s should be integer", p.Name)
		}
	})

	t.Run("key name count or limit infers integer type", func(t *testing.T) {
		t.Parallel()
		content := `this.get('/path', { count: count, limit: userLimit, offset: off })`
		endpoints := []DiscoveredEndpoint{
			{Method: "GET", Path: "/path", SourceTier: TierCommunitySDK, SourceName: "sdk"},
		}

		result := EnrichWithParams(content, endpoints)

		assert.Len(t, result[0].Params, 3)
		for _, p := range result[0].Params {
			assert.Equal(t, "integer", p.Type, "param %s should be integer", p.Name)
		}
	})

	t.Run("function signature positional arg sets required", func(t *testing.T) {
		t.Parallel()
		content := `
  getSteamUser(steamid) {
    return this.get('/user', { steamid })
  }
`
		endpoints := []DiscoveredEndpoint{
			{Method: "GET", Path: "/user", SourceTier: TierCommunitySDK, SourceName: "sdk"},
		}

		result := EnrichWithParams(content, endpoints)

		assert.Len(t, result[0].Params, 1)
		assert.Equal(t, "steamid", result[0].Params[0].Name)
		assert.True(t, result[0].Params[0].Required, "steamid should be required")
	})

	t.Run("destructured args with defaults are optional", func(t *testing.T) {
		t.Parallel()
		content := `
  getOwnedGames(id, { opt1 = true } = {}) {
    return this.get('/games', { id, opt1 })
  }
`
		endpoints := []DiscoveredEndpoint{
			{Method: "GET", Path: "/games", SourceTier: TierCommunitySDK, SourceName: "sdk"},
		}

		result := EnrichWithParams(content, endpoints)

		assert.Len(t, result[0].Params, 2)
		paramMap := make(map[string]DiscoveredParam)
		for _, p := range result[0].Params {
			paramMap[p.Name] = p
		}
		assert.True(t, paramMap["id"].Required, "id should be required")
		assert.False(t, paramMap["opt1"].Required, "opt1 should be optional")
	})

	t.Run("no params object after URL returns nil params", func(t *testing.T) {
		t.Parallel()
		content := `this.get('/path')`
		endpoints := []DiscoveredEndpoint{
			{Method: "GET", Path: "/path", SourceTier: TierCommunitySDK, SourceName: "sdk"},
		}

		result := EnrichWithParams(content, endpoints)

		assert.Len(t, result, 1)
		assert.Nil(t, result[0].Params, "params should be nil, not empty slice")
	})

	t.Run("nested object extracts only top-level key", func(t *testing.T) {
		t.Parallel()
		content := `this.get('/path', { filter: { type: "active" }, name: val })`
		endpoints := []DiscoveredEndpoint{
			{Method: "GET", Path: "/path", SourceTier: TierCommunitySDK, SourceName: "sdk"},
		}

		result := EnrichWithParams(content, endpoints)

		names := paramNames(result[0].Params)
		assert.Contains(t, names, "filter")
		assert.Contains(t, names, "name")
		assert.NotContains(t, names, "type", "nested key should not be extracted")
	})

	t.Run("trailing comma after last param", func(t *testing.T) {
		t.Parallel()
		content := `this.get('/path', { key1: val1, key2: val2, })`
		endpoints := []DiscoveredEndpoint{
			{Method: "GET", Path: "/path", SourceTier: TierCommunitySDK, SourceName: "sdk"},
		}

		result := EnrichWithParams(content, endpoints)

		assert.Len(t, result[0].Params, 2)
		names := paramNames(result[0].Params)
		assert.Contains(t, names, "key1")
		assert.Contains(t, names, "key2")
	})

	t.Run("multiple HTTP calls get only own params", func(t *testing.T) {
		t.Parallel()
		content := `
class API {
  getUsers() {
    return this.get('/users', { role: 'admin' })
  }
  getProjects() {
    return this.get('/projects', { status: 'active' })
  }
}
`
		endpoints := []DiscoveredEndpoint{
			{Method: "GET", Path: "/users", SourceTier: TierCommunitySDK, SourceName: "sdk"},
			{Method: "GET", Path: "/projects", SourceTier: TierCommunitySDK, SourceName: "sdk"},
		}

		result := EnrichWithParams(content, endpoints)

		assert.Len(t, result, 2)

		epMap := make(map[string][]DiscoveredParam)
		for _, ep := range result {
			epMap[ep.Path] = ep.Params
		}

		usersNames := paramNames(epMap["/users"])
		assert.Contains(t, usersNames, "role")
		assert.NotContains(t, usersNames, "status", "cross-product: /users should not have status")

		projectsNames := paramNames(epMap["/projects"])
		assert.Contains(t, projectsNames, "status")
		assert.NotContains(t, projectsNames, "role", "cross-product: /projects should not have role")
	})

	t.Run("multi-line whitespace between URL and params", func(t *testing.T) {
		t.Parallel()
		content := "this.get('/path',\n    { key: val })"
		endpoints := []DiscoveredEndpoint{
			{Method: "GET", Path: "/path", SourceTier: TierCommunitySDK, SourceName: "sdk"},
		}

		result := EnrichWithParams(content, endpoints)

		assert.Len(t, result[0].Params, 1)
		assert.Equal(t, "key", result[0].Params[0].Name)
	})

	t.Run("malformed object unbalanced braces does not panic", func(t *testing.T) {
		t.Parallel()
		content := `this.get('/path', { key1: val1, key2: { broken`
		endpoints := []DiscoveredEndpoint{
			{Method: "GET", Path: "/path", SourceTier: TierCommunitySDK, SourceName: "sdk"},
		}

		assert.NotPanics(t, func() {
			result := EnrichWithParams(content, endpoints)
			// Should return endpoint without params rather than crash
			assert.Len(t, result, 1)
			assert.Nil(t, result[0].Params)
		})
	})

	t.Run("integration: GrepEndpoints then EnrichWithParams", func(t *testing.T) {
		t.Parallel()
		content := `
class SteamAPI {
  getOwnedGames(steamid, { includeAppInfo = true } = {}) {
    return this.get('/IPlayerService/GetOwnedGames/v1', {
      steamid: steamid,
      include_appinfo: includeAppInfo,
      count: 10
    })
  }
}
`
		endpoints, _ := GrepEndpoints(content, "steamapi", TierCommunitySDK)
		assert.NotEmpty(t, endpoints, "GrepEndpoints should find at least one endpoint")

		result := EnrichWithParams(content, endpoints)

		// Find the endpoint with our path
		var found *DiscoveredEndpoint
		for i := range result {
			if result[i].Path == "/IPlayerService/GetOwnedGames/v1" {
				found = &result[i]
				break
			}
		}
		assert.NotNil(t, found, "should find /IPlayerService/GetOwnedGames/v1")
		assert.NotNil(t, found.Params, "params should be populated")

		names := paramNames(found.Params)
		assert.Contains(t, names, "steamid")
		assert.Contains(t, names, "include_appinfo")
		assert.Contains(t, names, "count")
	})

	t.Run("negative: GrepEndpoints alone returns nil params", func(t *testing.T) {
		t.Parallel()
		content := `this.get('/v1/users');`

		endpoints, _ := GrepEndpoints(content, "sdk", TierCommunitySDK)

		assert.NotEmpty(t, endpoints)
		for _, ep := range endpoints {
			assert.Nil(t, ep.Params, "GrepEndpoints should not populate params")
		}
	})

	t.Run("steam SDK pattern: steamids.join", func(t *testing.T) {
		t.Parallel()
		content := `
  getPlayerSummaries(steamid) {
    return this.get('/ISteamUser/GetPlayerSummaries/v2', {
      steamids: steamids.join(',')
    })
  }
`
		endpoints := []DiscoveredEndpoint{
			{Method: "GET", Path: "/ISteamUser/GetPlayerSummaries/v2", SourceTier: TierCommunitySDK, SourceName: "sdk"},
		}

		result := EnrichWithParams(content, endpoints)

		assert.Len(t, result[0].Params, 1)
		assert.Equal(t, "steamids", result[0].Params[0].Name)
		assert.Equal(t, "string", result[0].Params[0].Type)
	})

	t.Run("template literal path matches cleaned endpoint", func(t *testing.T) {
		t.Parallel()
		content := "this.get(`/users/${userId}`, { expand: true })"
		// GrepEndpoints would produce this cleaned path
		endpoints := []DiscoveredEndpoint{
			{Method: "GET", Path: "/users/{id}", SourceTier: TierCommunitySDK, SourceName: "sdk"},
		}

		result := EnrichWithParams(content, endpoints)

		assert.Len(t, result, 1)
		assert.Len(t, result[0].Params, 1)
		assert.Equal(t, "expand", result[0].Params[0].Name)
		assert.Equal(t, "boolean", result[0].Params[0].Type)
	})

	t.Run("page and maxlength key names infer integer", func(t *testing.T) {
		t.Parallel()
		content := `this.get('/path', { page: p, maxlength: ml })`
		endpoints := []DiscoveredEndpoint{
			{Method: "GET", Path: "/path", SourceTier: TierCommunitySDK, SourceName: "sdk"},
		}

		result := EnrichWithParams(content, endpoints)

		assert.Len(t, result[0].Params, 2)
		for _, p := range result[0].Params {
			assert.Equal(t, "integer", p.Type, "param %s should be integer", p.Name)
		}
	})

	t.Run("async function signature correlation", func(t *testing.T) {
		t.Parallel()
		content := `
  async function fetchUser(userId) {
    return this.get('/user', { userId })
  }
`
		endpoints := []DiscoveredEndpoint{
			{Method: "GET", Path: "/user", SourceTier: TierCommunitySDK, SourceName: "sdk"},
		}

		result := EnrichWithParams(content, endpoints)

		assert.Len(t, result[0].Params, 1)
		assert.Equal(t, "userId", result[0].Params[0].Name)
		assert.True(t, result[0].Params[0].Required, "userId should be required from function sig")
	})

	t.Run("string value inside quotes infers string type", func(t *testing.T) {
		t.Parallel()
		content := `this.get('/path', { format: 'json' })`
		endpoints := []DiscoveredEndpoint{
			{Method: "GET", Path: "/path", SourceTier: TierCommunitySDK, SourceName: "sdk"},
		}

		result := EnrichWithParams(content, endpoints)

		assert.Len(t, result[0].Params, 1)
		assert.Equal(t, "string", result[0].Params[0].Type)
	})

	t.Run("braces inside line comments are ignored", func(t *testing.T) {
		t.Parallel()
		content := `this.get('/path', {
			key: val, // } closing brace in comment
			more: val2
		})`
		endpoints := []DiscoveredEndpoint{
			{Method: "GET", Path: "/path", SourceTier: TierCommunitySDK, SourceName: "sdk"},
		}

		result := EnrichWithParams(content, endpoints)

		names := paramNames(result[0].Params)
		assert.Contains(t, names, "key")
		assert.Contains(t, names, "more")
	})

	t.Run("braces inside block comments are ignored", func(t *testing.T) {
		t.Parallel()
		content := `this.get('/path', {
			key: val, /* { nested } brace */
			other: val2
		})`
		endpoints := []DiscoveredEndpoint{
			{Method: "GET", Path: "/path", SourceTier: TierCommunitySDK, SourceName: "sdk"},
		}

		result := EnrichWithParams(content, endpoints)

		names := paramNames(result[0].Params)
		assert.Contains(t, names, "key")
		assert.Contains(t, names, "other")
	})

	t.Run("braces inside string literals are not mismatched", func(t *testing.T) {
		t.Parallel()
		content := `this.get('/path', { query: '{complex}', name: val })`
		endpoints := []DiscoveredEndpoint{
			{Method: "GET", Path: "/path", SourceTier: TierCommunitySDK, SourceName: "sdk"},
		}

		result := EnrichWithParams(content, endpoints)

		names := paramNames(result[0].Params)
		assert.Contains(t, names, "query")
		assert.Contains(t, names, "name")
	})
}

func TestParseSignatureParams(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		required []string // expected required param names
	}{
		{name: "empty string", input: "", required: nil},
		{name: "single positional", input: "steamid", required: []string{"steamid"}},
		{name: "multiple positional", input: "steamid, appid", required: []string{"steamid", "appid"}},
		{name: "positional with default is optional", input: "count = 10", required: nil},
		{name: "mixed required and optional", input: "id, count = 10", required: []string{"id"}},
		{name: "destructured object is optional", input: "{ opt1 = true, opt2 } = {}", required: nil},
		{name: "positional then destructured", input: "steamid, { includeAppInfo = true } = {}", required: []string{"steamid"}},
		{name: "multiple positional then destructured", input: "id, name, { page = 1 } = {}", required: []string{"id", "name"}},
		{name: "only destructured no positional", input: "{ a, b, c } = {}", required: nil},
		{name: "trailing comma", input: "steamid, ", required: []string{"steamid"}},
		{name: "whitespace around names", input: "  id  ,  name  ", required: []string{"id", "name"}},
		{name: "rest parameter skipped", input: "id, ...args", required: []string{"id"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := parseSignatureParams(tt.input)

			if tt.required == nil {
				assert.Empty(t, result)
			} else {
				for _, name := range tt.required {
					assert.True(t, result[name], "expected %q to be required", name)
				}
				assert.Len(t, result, len(tt.required))
			}
		})
	}
}

// paramNames extracts just the names from a slice of DiscoveredParam.
func paramNames(params []DiscoveredParam) []string {
	names := make([]string, len(params))
	for i, p := range params {
		names[i] = p.Name
	}
	return names
}
