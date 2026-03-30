package crowdsniff

// Source tier constants for endpoint provenance.
const (
	TierOfficialSDK  = "official-sdk"
	TierCommunitySDK = "community-sdk"
	TierCodeSearch   = "code-search"
	TierPostman      = "postman"
)

// DiscoveredEndpoint represents a single API endpoint found by a source.
type DiscoveredEndpoint struct {
	Method     string // HTTP method (GET, POST, etc.)
	Path       string // URL path (e.g., "/v1/users", "/users/:id")
	SourceTier string // one of the Tier* constants
	SourceName string // e.g., "@notionhq/client", "github-code-search"
}

// SourceResult is returned by each discovery source.
type SourceResult struct {
	Endpoints         []DiscoveredEndpoint
	BaseURLCandidates []string // e.g., "https://api.notion.com"
}

// AggregatedEndpoint is a deduplicated endpoint with cross-source metadata.
type AggregatedEndpoint struct {
	Method      string
	Path        string // normalized
	SourceTier  string // highest tier across sources
	SourceCount int    // number of distinct sources
}
