package crowdsniff

import (
	"testing"
)

// TestGrepEndpoints_StampsOriginFromBaseURL verifies that endpoints inherit
// the most recent base URL observed in the source file scan order.
func TestGrepEndpoints_StampsOriginFromBaseURL(t *testing.T) {
	src := `
const baseUrl = "https://query1.finance.yahoo.com";
client.get("/v8/finance/chart/AAPL");
`
	eps, _ := GrepEndpoints(src, "yahoo-test", TierCommunitySDK)
	if len(eps) == 0 {
		t.Fatalf("expected at least one endpoint, got none")
	}
	if eps[0].OriginBaseURL != "https://query1.finance.yahoo.com" {
		t.Errorf("expected OriginBaseURL to be stamped, got %q", eps[0].OriginBaseURL)
	}
}

// TestGrepEndpoints_MultipleOriginsInOneFile verifies that when a file
// redeclares its base URL (common in aggregator wrappers), subsequent
// endpoints inherit the new origin — not the old one. Uses only recognized
// call shapes (client.get/axios.get) since the extractors don't match
// arbitrary identifiers like `yahoo.get`.
func TestGrepEndpoints_MultipleOriginsInOneFile(t *testing.T) {
	src := `
const baseUrl = "https://query1.finance.yahoo.com";
client.get("/v8/finance/chart/AAPL");
const apiBase = "https://api.polygon.io";
axios.get("/v3/reference/tickers");
const baseUri = "https://api.stlouisfed.org";
client.get("/fred/series/observations");
`
	eps, _ := GrepEndpoints(src, "aggregator", TierCommunitySDK)
	if len(eps) < 3 {
		t.Fatalf("expected >=3 endpoints extracted, got %d", len(eps))
	}

	byPath := make(map[string]string)
	for _, e := range eps {
		byPath[e.Path] = e.OriginBaseURL
	}

	cases := map[string]string{
		"/v8/finance/chart/AAPL":    "https://query1.finance.yahoo.com",
		"/v3/reference/tickers":     "https://api.polygon.io",
		"/fred/series/observations": "https://api.stlouisfed.org",
	}
	for path, wantOrigin := range cases {
		if got, found := byPath[path]; !found {
			t.Errorf("endpoint %s not extracted", path)
		} else if got != wantOrigin {
			t.Errorf("endpoint %s: expected origin %q, got %q", path, wantOrigin, got)
		}
	}
}

// TestFilterByHost_DropsCrossAPIContamination is the retro F1 acceptance test —
// reproduces the exact yahoo-finance contamination scenario and verifies
// FilterByHost cleans it up.
func TestFilterByHost_DropsCrossAPIContamination(t *testing.T) {
	endpoints := []AggregatedEndpoint{
		{Method: "GET", Path: "/v8/finance/chart/{id}", OriginBaseURLs: []string{"https://query1.finance.yahoo.com"}},
		{Method: "GET", Path: "/v7/finance/quote", OriginBaseURLs: []string{"https://query1.finance.yahoo.com"}},
		{Method: "GET", Path: "/v3/reference/tickers", OriginBaseURLs: []string{"https://api.polygon.io"}},
		{Method: "GET", Path: "/fred/series/observations", OriginBaseURLs: []string{"https://api.stlouisfed.org"}},
		{Method: "GET", Path: "/tiingo/daily/{id}", OriginBaseURLs: []string{"https://api.tiingo.com"}},
	}

	kept, dropped := FilterByHost(endpoints, "query1.finance.yahoo.com")

	if len(kept) != 2 {
		t.Errorf("expected 2 kept (Yahoo endpoints), got %d", len(kept))
	}
	if len(dropped) != 3 {
		t.Errorf("expected 3 dropped (Polygon/FRED/Tiingo), got %d", len(dropped))
	}
}

// TestFilterByHost_FallsOpenForUnknownOrigin verifies endpoints with no origin
// detected are kept (we don't have signal to drop them, so be permissive).
func TestFilterByHost_FallsOpenForUnknownOrigin(t *testing.T) {
	endpoints := []AggregatedEndpoint{
		{Method: "GET", Path: "/v7/finance/quote", OriginBaseURLs: []string{"https://query1.finance.yahoo.com"}},
		{Method: "GET", Path: "/unknown/origin", OriginBaseURLs: nil}, // no origin signal
	}
	kept, dropped := FilterByHost(endpoints, "query1.finance.yahoo.com")
	if len(kept) != 2 {
		t.Errorf("expected both kept (one match + one unknown), got %d kept", len(kept))
	}
	if len(dropped) != 0 {
		t.Errorf("expected no drops, got %d", len(dropped))
	}
}

// TestFilterByHost_NoTargetNoFilter verifies that passing an empty target host
// is a no-op — preserves backwards-compatible behavior for callers that don't
// care about origin filtering.
func TestFilterByHost_NoTargetNoFilter(t *testing.T) {
	endpoints := []AggregatedEndpoint{
		{Method: "GET", Path: "/v7/finance/quote", OriginBaseURLs: []string{"https://query1.finance.yahoo.com"}},
		{Method: "GET", Path: "/v3/reference/tickers", OriginBaseURLs: []string{"https://api.polygon.io"}},
	}
	kept, dropped := FilterByHost(endpoints, "")
	if len(kept) != 2 || len(dropped) != 0 {
		t.Errorf("expected no-op with empty target: got %d kept, %d dropped", len(kept), len(dropped))
	}
}

// TestHostFromURL exercises host extraction for URL shapes we'll see in the wild.
func TestHostFromURL(t *testing.T) {
	cases := map[string]string{
		"https://query1.finance.yahoo.com":                "query1.finance.yahoo.com",
		"https://query1.finance.yahoo.com/":               "query1.finance.yahoo.com",
		"https://api.polygon.io:443/v3/reference/tickers": "api.polygon.io",
		"HTTPS://API.Notion.COM/v1/pages":                 "api.notion.com",
		"no-scheme-at-all":                                "no-scheme-at-all",
		"":                                                "",
	}
	for in, want := range cases {
		if got := hostFromURL(in); got != want {
			t.Errorf("hostFromURL(%q) = %q, want %q", in, got, want)
		}
	}
}
