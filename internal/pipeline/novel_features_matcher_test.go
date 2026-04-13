package pipeline

import (
	"os"
	"path/filepath"
	"testing"
)

// TestMatchNovelFeature_LeafFallback covers the leaf-only fallback path
// (empty registeredPaths). This exercises the same scenarios the previous
// flat-map matcher handled: exact leaf, hyphen-prefix, aliases, case
// folding. Preserves backward compatibility for CLI layouts where the
// path-aware command-tree walker produces nothing.
func TestMatchNovelFeature_LeafFallback(t *testing.T) {
	leaves := map[string]bool{
		"perf":          true,
		"gains":         true,
		"digest":        true,
		"login-chrome":  true,
		"options":       true,
		"options-chain": true,
		"sparkline":     true,
		"earnings":      true,
	}
	var emptyPaths map[string]bool

	cases := []struct {
		name    string
		feature NovelFeature
		want    bool
	}{
		{"exact leaf (portfolio perf → perf)", NovelFeature{Command: "portfolio perf"}, true},
		{"exact after flag strip (digest --watchlist tech → digest)", NovelFeature{Command: "digest --watchlist tech"}, true},
		{"hyphen-prefix (auth login --chrome → login-chrome)", NovelFeature{Command: "auth login --chrome"}, true},
		{"exact when bare command is registered (options)", NovelFeature{Command: "options --moneyness otm"}, true},
		{"no match when truly absent", NovelFeature{Command: "insiders --recent 30d"}, false},
		{"alias rescues a rename", NovelFeature{Command: "portfolio dividends", Aliases: []string{"portfolio gains"}}, true},
		{"alias exact (login-chrome registered)", NovelFeature{Command: "portfolio dividends", Aliases: []string{"auth login-chrome"}}, true},
		{"empty command returns false", NovelFeature{Command: ""}, false},
		{"flag-only command returns false", NovelFeature{Command: "--json"}, false},
		{"case insensitive", NovelFeature{Command: "PORTFOLIO PERF"}, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, reason := matchNovelFeature(tc.feature, emptyPaths, leaves)
			if got != tc.want {
				t.Errorf("matchNovelFeature(%q) = %v, want %v (reason: %s)",
					tc.feature.Command, got, tc.want, reason)
			}
		})
	}
}

// TestMatchNovelFeature_PathAware covers the core new behavior:
// path-aware matching against a real command tree, with hyphen-prefix
// restricted to sibling commands (same parent path).
func TestMatchNovelFeature_PathAware(t *testing.T) {
	// Full command tree with parent context. `perf` lives under
	// `portfolio`. `sparkline` and `login-chrome` are at similar depths
	// but in different subtrees.
	paths := map[string]bool{
		"portfolio":         true,
		"portfolio perf":    true,
		"portfolio gains":   true,
		"portfolio add":     true,
		"auth":              true,
		"auth login":        true,
		"auth login-chrome": true,
		"auth logout":       true,
		"options":           true,
		"options-chain":     true, // top-level sibling of `options`
		"digest":            true,
		"watchlist":         true,
		"watchlist create":  true,
		"watchlist add":     true,
	}

	cases := []struct {
		name    string
		feature NovelFeature
		want    bool
	}{
		{
			name:    "exact full path",
			feature: NovelFeature{Command: "portfolio perf"},
			want:    true,
		},
		{
			name:    "exact full path: auth login-chrome",
			feature: NovelFeature{Command: "auth login-chrome"},
			want:    true,
		},
		{
			name:    "hyphen-prefix under same parent: auth login → auth login-chrome",
			feature: NovelFeature{Command: "auth login --chrome"},
			want:    true,
		},
		{
			name:    "hyphen-prefix under root: options → options-chain",
			feature: NovelFeature{Command: "options --moneyness otm"},
			want:    true,
		},
		{
			name:    "exact digest",
			feature: NovelFeature{Command: "digest"},
			want:    true,
		},
		{
			name:    "no match on unbuilt feature",
			feature: NovelFeature{Command: "insiders --recent 30d"},
			want:    false,
		},
		{
			name:    "alias rescues a missing primary",
			feature: NovelFeature{Command: "portfolio dividends", Aliases: []string{"portfolio gains"}},
			want:    true,
		},
		{
			name:    "case insensitive full path",
			feature: NovelFeature{Command: "Portfolio Perf"},
			want:    true,
		},
		{
			name:    "no match when planned path has wrong parent",
			feature: NovelFeature{Command: "nonexistent perf"},
			want:    false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, reason := matchNovelFeature(tc.feature, paths, nil)
			if got != tc.want {
				t.Errorf("matchNovelFeature(%q) = %v, want %v (reason: %s)",
					tc.feature.Command, got, tc.want, reason)
			}
		})
	}
}

// TestMatchNovelFeature_NoFalsePositive_CrossParent is the critical
// accuracy guarantee: a leaf match under one parent must NOT match a
// sibling leaf under a different parent. Without path awareness, the
// old flat-map matcher would match any "perf" anywhere. With path
// awareness, "portfolio perf" must not be satisfied by a hypothetical
// "analytics perf" living under a different root.
func TestMatchNovelFeature_NoFalsePositive_CrossParent(t *testing.T) {
	paths := map[string]bool{
		"analytics":      true,
		"analytics perf": true,
		// deliberately NOT "portfolio perf"
	}
	nf := NovelFeature{Command: "portfolio perf"}
	got, reason := matchNovelFeature(nf, paths, nil)
	if got {
		t.Errorf("planned 'portfolio perf' should not match 'analytics perf' across parents; got matched with reason: %s", reason)
	}
}

// TestMatchNovelFeature_NoFalsePositive_BareLeaf is the guard against the
// old matcher's worst failure mode: "op" shouldn't match "options"
// because the hyphen-boundary rule rejects substring prefixes.
func TestMatchNovelFeature_NoFalsePositive_BareLeaf(t *testing.T) {
	paths := map[string]bool{"options": true}
	nf := NovelFeature{Command: "op"}
	got, _ := matchNovelFeature(nf, paths, nil)
	if got {
		t.Error("planned 'op' should not prefix-match 'options' (no hyphen boundary)")
	}
}

// TestCommandPath covers the flag-stripping path extractor.
func TestCommandPath(t *testing.T) {
	cases := map[string]string{
		"portfolio perf":                       "portfolio perf",
		"auth login --chrome":                  "auth login",
		"options --moneyness otm --max-dte 45": "options",
		"digest":                               "digest",
		"digest --watchlist tech":              "digest",
		"earnings-calendar --watchlist":        "earnings-calendar",
		"":                                     "",
		"--json":                               "",
		"   padded   perf  ":                   "padded perf",
		"PORTFOLIO PERF":                       "portfolio perf",
	}
	for in, want := range cases {
		if got := commandPath(in); got != want {
			t.Errorf("commandPath(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestCommandLeaf preserves backward compatibility — commandLeaf remains
// a thin wrapper around commandPath + lastPathSegment.
func TestCommandLeaf(t *testing.T) {
	cases := map[string]string{
		"portfolio perf":                       "perf",
		"auth login --chrome":                  "login",
		"options --moneyness otm --max-dte 45": "options",
		"digest":                               "digest",
		"earnings-calendar --watchlist":        "earnings-calendar",
		"":                                     "",
		"--json":                               "",
	}
	for in, want := range cases {
		if got := commandLeaf(in); got != want {
			t.Errorf("commandLeaf(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestCollectRegisteredCommandPaths exercises the tree walker end-to-end:
// it writes a synthetic CLI fixture mirroring the yahoo-finance layout,
// invokes the regex-based command-tree resolver, and asserts the emitted
// paths cover nested subcommands (portfolio perf) and hyphenated
// transcendence commands (auth login-chrome).
func TestCollectRegisteredCommandPaths(t *testing.T) {
	dir := t.TempDir()
	cli := filepath.Join(dir, "internal", "cli")
	if err := os.MkdirAll(cli, 0o755); err != nil {
		t.Fatal(err)
	}

	writeTestFile(t, filepath.Join(cli, "root.go"), `package cli
import "github.com/spf13/cobra"
func Execute() {
	rootCmd := &cobra.Command{Use: "tool"}
	rootCmd.AddCommand(newAuthCmd())
	rootCmd.AddCommand(newPortfolioCmd())
	rootCmd.AddCommand(newDigestCmd())
	rootCmd.AddCommand(newOptionsCmd())
	rootCmd.AddCommand(newOptionsChainCmd())
}
`)
	writeTestFile(t, filepath.Join(cli, "auth.go"), `package cli
import "github.com/spf13/cobra"
func newAuthCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "auth"}
	cmd.AddCommand(newAuthLoginCmd())
	cmd.AddCommand(newAuthLoginChromeCmd())
	cmd.AddCommand(newAuthLogoutCmd())
	return cmd
}
func newAuthLoginCmd() *cobra.Command { return &cobra.Command{Use: "login"} }
func newAuthLoginChromeCmd() *cobra.Command { return &cobra.Command{Use: "login-chrome"} }
func newAuthLogoutCmd() *cobra.Command { return &cobra.Command{Use: "logout"} }
`)
	writeTestFile(t, filepath.Join(cli, "portfolio.go"), `package cli
import "github.com/spf13/cobra"
func newPortfolioCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "portfolio"}
	cmd.AddCommand(newPortfolioPerfCmd())
	cmd.AddCommand(newPortfolioGainsCmd())
	return cmd
}
func newPortfolioPerfCmd() *cobra.Command { return &cobra.Command{Use: "perf"} }
func newPortfolioGainsCmd() *cobra.Command { return &cobra.Command{Use: "gains"} }
`)
	writeTestFile(t, filepath.Join(cli, "misc.go"), `package cli
import "github.com/spf13/cobra"
func newDigestCmd() *cobra.Command { return &cobra.Command{Use: "digest"} }
func newOptionsCmd() *cobra.Command { return &cobra.Command{Use: "options <symbol>"} }
func newOptionsChainCmd() *cobra.Command { return &cobra.Command{Use: "options-chain <symbol>"} }
`)

	paths := collectRegisteredCommandPaths(dir)

	// Every nested + transcendence path the matcher needs.
	want := []string{
		"auth",
		"auth login",
		"auth login-chrome",
		"auth logout",
		"portfolio",
		"portfolio perf",
		"portfolio gains",
		"digest",
		"options",
		"options-chain",
	}
	for _, w := range want {
		if !paths[w] {
			t.Errorf("expected path %q in tree, got paths: %v", w, paths)
		}
	}
}
