package pipeline

import (
	"testing"
)

// TestMatchNovelFeature covers the three-pass matcher — exact, prefix, alias —
// exercising the yahoo-finance retro scenarios where the previous literal
// matcher false-negatived features that were actually built.
func TestMatchNovelFeature(t *testing.T) {
	registered := map[string]bool{
		"perf":          true, // under `portfolio perf`
		"gains":         true,
		"digest":        true,
		"login-chrome":  true, // under `auth login-chrome`
		"options":       true,
		"options-chain": true,
		"sparkline":     true,
		"earnings":      true, // registered but planned was "earnings-calendar"
	}

	cases := []struct {
		name    string
		feature NovelFeature
		want    bool
		pass    string
	}{
		{
			name:    "exact match on leaf (portfolio perf → perf)",
			feature: NovelFeature{Command: "portfolio perf"},
			want:    true,
			pass:    "exact",
		},
		{
			name:    "exact match after flag strip (digest --watchlist tech → digest)",
			feature: NovelFeature{Command: "digest --watchlist tech"},
			want:    true,
			pass:    "exact",
		},
		{
			name:    "prefix match: planned auth login --chrome → built login-chrome",
			feature: NovelFeature{Command: "auth login --chrome"},
			want:    true,
			pass:    "prefix",
		},
		{
			name:    "prefix match: planned options --moneyness → built options or options-chain",
			feature: NovelFeature{Command: "options --moneyness otm --max-dte 45"},
			want:    true,
			pass:    "exact (options registered) or prefix (options-chain)",
		},
		{
			name:    "no match when truly absent",
			feature: NovelFeature{Command: "insiders --recent 30d --net-buying"},
			want:    false,
		},
		{
			name:    "alias rescues a rename",
			feature: NovelFeature{Command: "portfolio dividends", Aliases: []string{"portfolio gains"}},
			want:    true,
			pass:    "alias (gains built as renamed feature)",
		},
		{
			name:    "alias prefix match",
			feature: NovelFeature{Command: "portfolio dividends", Aliases: []string{"auth login-chrome"}},
			want:    true,
			pass:    "alias exact (login-chrome registered)",
		},
		{
			name:    "empty command returns false",
			feature: NovelFeature{Command: ""},
			want:    false,
		},
		{
			name:    "flag-only command returns false (nothing to match)",
			feature: NovelFeature{Command: "--json"},
			want:    false,
		},
		{
			name:    "case insensitive",
			feature: NovelFeature{Command: "PORTFOLIO PERF"},
			want:    true,
			pass:    "exact (case folded)",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := matchNovelFeature(tc.feature, registered)
			if got != tc.want {
				t.Errorf("matchNovelFeature(%q, aliases=%v) = %v, want %v (pass: %s)",
					tc.feature.Command, tc.feature.Aliases, got, tc.want, tc.pass)
			}
		})
	}
}

// TestCommandLeaf covers the flag-stripping helper.
func TestCommandLeaf(t *testing.T) {
	cases := map[string]string{
		"portfolio perf":                       "perf",
		"auth login --chrome":                  "login",
		"options --moneyness otm --max-dte 45": "options",
		"digest":                               "digest",
		"digest --watchlist tech":              "digest",
		"earnings-calendar --watchlist":        "earnings-calendar",
		"":                                     "",
		"--json":                               "",
		"   padded   perf  ":                   "perf",
	}
	for in, want := range cases {
		if got := commandLeaf(in); got != want {
			t.Errorf("commandLeaf(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestMatchNovelFeature_PrefixDoesNotFalsePositive guards against over-eager
// prefix matching. "op" should NOT match "options" — only hyphen-bounded
// prefixes count.
func TestMatchNovelFeature_PrefixDoesNotFalsePositive(t *testing.T) {
	registered := map[string]bool{
		"options": true,
	}
	nf := NovelFeature{Command: "op"}
	if matchNovelFeature(nf, registered) {
		t.Error("commandLeaf 'op' should not prefix-match 'options' (no hyphen boundary)")
	}
}
