package crowdsniff

// Source tier constants for endpoint provenance.
const (
	TierOfficialSDK  = "official-sdk"
	TierCommunitySDK = "community-sdk"
	TierCodeSearch   = "code-search"
	TierPostman      = "postman"
)

// DiscoveredParam represents a query parameter extracted from SDK source code.
type DiscoveredParam struct {
	Name     string // parameter name (e.g., "steamid", "include_appinfo")
	Type     string // inferred type: "string", "integer", "boolean"
	Required bool   // true if positional arg without default in function signature
	Default  string // default value as string, empty if none
}

// DiscoveredEndpoint represents a single API endpoint found by a source.
type DiscoveredEndpoint struct {
	Method        string            // HTTP method (GET, POST, etc.)
	Path          string            // URL path (e.g., "/v1/users", "/users/:id")
	Params        []DiscoveredParam // query parameters extracted from SDK code
	SourceTier    string            // one of the Tier* constants
	SourceName    string            // e.g., "@notionhq/client", "github-code-search"
	OriginBaseURL string            // nearest base URL observed in the same source file (e.g., "https://api.notion.com"); empty if not detectable
}

// DiscoveredAuth represents an authentication pattern detected in SDK source code.
type DiscoveredAuth struct {
	Type       string // "api_key", "bearer_token", "basic"
	Header     string // header name or query param name (e.g., "key", "X-Api-Key", "Authorization")
	In         string // "header" or "query"
	Format     string // e.g., "Bearer {token}", "{api_key}"
	EnvVarHint string // detected env var name if visible (e.g., "STEAM_API_KEY")
	KeyURLHint string // URL to get an API key, extracted from SDK README
	SourceTier string // tier of the source that found this auth pattern
}

// SourceResult is returned by each discovery source.
type SourceResult struct {
	Endpoints         []DiscoveredEndpoint
	BaseURLCandidates []string // e.g., "https://api.notion.com"
	Auth              []DiscoveredAuth
}

// AggregatedEndpoint is a deduplicated endpoint with cross-source metadata.
type AggregatedEndpoint struct {
	Method         string
	Path           string            // normalized
	Params         []DiscoveredParam // union-merged params across sources, sorted by name
	SourceTier     string            // highest tier across sources
	SourceCount    int               // number of distinct sources
	OriginBaseURLs []string          // distinct base URLs observed in source files that contributed this endpoint; used for host filtering
}
