package crowdsniff

import (
	"regexp"
	"sort"
	"strings"
)

var (
	// Concrete value patterns (from websniff/classifier.go).
	uuidSegmentPattern = regexp.MustCompile(`(?i)^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
	hashSegmentPattern = regexp.MustCompile(`(?i)^[0-9a-f]{32,}$`)
	numericPattern     = regexp.MustCompile(`^\d+$`)

	// Parameter syntax patterns — matches :id, {id}, {user_id}, <id>, <userId>, $id.
	colonParamPattern   = regexp.MustCompile(`^:[a-zA-Z_][a-zA-Z0-9_]*$`)
	bracketParamPattern = regexp.MustCompile(`^\{[a-zA-Z_][a-zA-Z0-9_]*\}$`)
	angleParamPattern   = regexp.MustCompile(`^<[a-zA-Z_][a-zA-Z0-9_]*>$`)
	dollarParamPattern  = regexp.MustCompile(`^\$[a-zA-Z_][a-zA-Z0-9_]*$`)
)

// tierRank returns a numeric rank for tier ordering (higher = more authoritative).
func tierRank(tier string) int {
	switch tier {
	case TierOfficialSDK:
		return 4
	case TierCommunitySDK:
		return 3
	case TierCodeSearch:
		return 2
	case TierPostman:
		return 1
	default:
		return 0
	}
}

// Aggregate deduplicates endpoints from multiple sources, computing the highest
// source tier and distinct source count for each unique method+path combination.
// It also collects all base URL candidates from the sources.
func Aggregate(results []SourceResult) ([]AggregatedEndpoint, []string) {
	type endpointKey struct {
		method string
		path   string
	}

	type accumulator struct {
		bestTier string
		sources  map[string]struct{} // distinct source names
		params   map[string]paramEntry
		origins  map[string]struct{} // distinct origin base URLs contributed by sources
	}

	index := make(map[endpointKey]*accumulator)
	var order []endpointKey
	var baseURLs []string

	for _, result := range results {
		baseURLs = append(baseURLs, result.BaseURLCandidates...)

		for _, ep := range result.Endpoints {
			method := strings.ToUpper(strings.TrimSpace(ep.Method))
			path := NormalizePath(ep.Path)
			key := endpointKey{method: method, path: path}

			acc, exists := index[key]
			if !exists {
				acc = &accumulator{
					sources: make(map[string]struct{}),
					params:  make(map[string]paramEntry),
					origins: make(map[string]struct{}),
				}
				index[key] = acc
				order = append(order, key)
			}

			if tierRank(ep.SourceTier) > tierRank(acc.bestTier) {
				acc.bestTier = ep.SourceTier
			}
			acc.sources[ep.SourceName] = struct{}{}
			if ep.OriginBaseURL != "" {
				acc.origins[ep.OriginBaseURL] = struct{}{}
			}

			// Union-merge params: prefer metadata from higher-tier source.
			for _, p := range ep.Params {
				existing, seen := acc.params[p.Name]
				if !seen {
					acc.params[p.Name] = paramEntry{param: p, tier: ep.SourceTier}
				} else if tierRank(ep.SourceTier) > tierRank(existing.tier) {
					acc.params[p.Name] = paramEntry{param: p, tier: ep.SourceTier}
				} else if tierRank(ep.SourceTier) == tierRank(existing.tier) && paramFieldCount(p) > paramFieldCount(existing.param) {
					acc.params[p.Name] = paramEntry{param: p, tier: ep.SourceTier}
				}
			}
		}
	}

	aggregated := make([]AggregatedEndpoint, 0, len(order))
	for _, key := range order {
		acc := index[key]
		ep := AggregatedEndpoint{
			Method:      key.method,
			Path:        key.path,
			SourceTier:  acc.bestTier,
			SourceCount: len(acc.sources),
		}
		if len(acc.params) > 0 {
			ep.Params = sortedParams(acc.params)
		}
		if len(acc.origins) > 0 {
			ep.OriginBaseURLs = make([]string, 0, len(acc.origins))
			for o := range acc.origins {
				ep.OriginBaseURLs = append(ep.OriginBaseURLs, o)
			}
			sort.Strings(ep.OriginBaseURLs)
		}
		aggregated = append(aggregated, ep)
	}

	return aggregated, deduplicateStrings(baseURLs)
}

// FilterByHost drops aggregated endpoints whose origin base URL hosts don't
// match the target host. Endpoints with no known origin (nothing detected in
// source file context) are kept — we fall open when signal is absent rather
// than silently losing candidate endpoints.
//
// Returns (kept, dropped) so callers can report how much noise was filtered
// and let users inspect the drops if needed.
func FilterByHost(endpoints []AggregatedEndpoint, targetHost string) (kept []AggregatedEndpoint, dropped []AggregatedEndpoint) {
	targetHost = strings.ToLower(strings.TrimSpace(targetHost))
	if targetHost == "" {
		return endpoints, nil
	}
	for _, ep := range endpoints {
		if len(ep.OriginBaseURLs) == 0 {
			// No origin signal — keep (permissive default).
			kept = append(kept, ep)
			continue
		}
		matched := false
		for _, origin := range ep.OriginBaseURLs {
			if hostFromURL(origin) == targetHost {
				matched = true
				break
			}
		}
		if matched {
			kept = append(kept, ep)
		} else {
			dropped = append(dropped, ep)
		}
	}
	return kept, dropped
}

// hostFromURL extracts the lowercased host from a URL, stripping scheme,
// port, and path. Returns empty string on parse failure.
func hostFromURL(rawURL string) string {
	s := strings.TrimSpace(rawURL)
	if s == "" {
		return ""
	}
	// Strip scheme
	if idx := strings.Index(s, "://"); idx >= 0 {
		s = s[idx+3:]
	}
	// Strip path / query / fragment
	for _, sep := range []byte{'/', '?', '#'} {
		if i := strings.IndexByte(s, sep); i >= 0 {
			s = s[:i]
		}
	}
	// Strip port
	if i := strings.IndexByte(s, ':'); i >= 0 {
		s = s[:i]
	}
	return strings.ToLower(s)
}

// NormalizePath unifies parameter syntax and replaces concrete values with placeholders.
// Step 1: Convert :id, {user_id}, <id>, $id → {id}
// Step 2: Replace UUIDs, numeric IDs, long hashes → {id}/{uuid}/{hash}
func NormalizePath(path string) string {
	// Strip query string if present.
	if idx := strings.Index(path, "?"); idx >= 0 {
		path = path[:idx]
	}

	parts := strings.Split(path, "/")
	segments := make([]string, 0, len(parts))
	for _, segment := range parts {
		if segment == "" {
			continue
		}

		// Step 1: Unify parameter syntax → {id}
		switch {
		case colonParamPattern.MatchString(segment):
			segment = "{id}"
		case bracketParamPattern.MatchString(segment):
			segment = "{id}"
		case angleParamPattern.MatchString(segment):
			segment = "{id}"
		case dollarParamPattern.MatchString(segment):
			segment = "{id}"
		// Step 2: Replace concrete values
		case numericPattern.MatchString(segment):
			segment = "{id}"
		case uuidSegmentPattern.MatchString(segment):
			segment = "{uuid}"
		case hashSegmentPattern.MatchString(segment):
			segment = "{hash}"
		}
		segments = append(segments, segment)
	}

	normalized := "/" + strings.Join(segments, "/")
	if normalized == "" {
		return "/"
	}
	return normalized
}

// paramEntry tracks a discovered param alongside the tier of its source,
// enabling tier-aware merge when the same param is found by multiple sources.
type paramEntry struct {
	param DiscoveredParam
	tier  string
}

// sortedParams converts the accumulator's param map into a slice sorted by name
// for deterministic YAML output.
func sortedParams(m map[string]paramEntry) []DiscoveredParam {
	params := make([]DiscoveredParam, 0, len(m))
	for _, entry := range m {
		params = append(params, entry.param)
	}
	sort.Slice(params, func(i, j int) bool {
		return params[i].Name < params[j].Name
	})
	return params
}

// paramFieldCount returns how many fields are populated on a DiscoveredParam.
// Used as a tiebreaker when two same-tier sources provide the same param name.
func paramFieldCount(p DiscoveredParam) int {
	count := 0
	if p.Type != "" {
		count++
	}
	if p.Required {
		count++
	}
	if p.Default != "" {
		count++
	}
	return count
}

// AggregateAuth merges auth patterns from multiple source results and returns
// the single best auth pattern. Auth from higher-tier sources takes precedence.
// Returns nil if no auth patterns were detected.
func AggregateAuth(results []SourceResult) *DiscoveredAuth {
	var best *DiscoveredAuth
	bestRank := -1

	for _, result := range results {
		for i := range result.Auth {
			auth := &result.Auth[i]
			rank := tierRank(auth.SourceTier)
			if rank > bestRank {
				bestRank = rank
				best = auth
			} else if rank == bestRank && best != nil {
				// Same tier: prefer auth with more metadata (env var hint).
				if auth.EnvVarHint != "" && best.EnvVarHint == "" {
					best = auth
				}
			}
		}
	}

	if best == nil {
		return nil
	}

	// Return a copy so callers don't modify the original.
	cp := *best
	return &cp
}

func deduplicateStrings(items []string) []string {
	seen := make(map[string]struct{}, len(items))
	result := make([]string, 0, len(items))
	for _, item := range items {
		if _, exists := seen[item]; exists {
			continue
		}
		seen[item] = struct{}{}
		result = append(result, item)
	}
	return result
}
