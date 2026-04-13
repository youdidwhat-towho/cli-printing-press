package pipeline

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// LiveCheckResult summarizes a live-behavior sampling of a printed CLI's
// novel features. For each novel feature with an Example command, the check
// runs the command against real targets (the CLI's actual configured API or
// sites) and asserts the output has the shape a working feature would produce:
// non-empty, query-relevant when the command encodes a query.
//
// Produced by RunLiveCheck. Consumed by the scorecard to apply a behavioral
// correctness cap on the Insight dimension — a Grade A scorecard with a
// flagship feature returning wrong data shouldn't be possible.
type LiveCheckResult struct {
	Checked  int                 `json:"checked"`
	Passed   int                 `json:"passed"`
	Failed   int                 `json:"failed"`
	Skipped  int                 `json:"skipped"`
	PassRate float64             `json:"pass_rate"` // passed / checked, 0..1
	Features []LiveFeatureResult `json:"features"`
	Unable   bool                `json:"unable,omitempty"` // true when the check couldn't run (no research.json, no binary)
	Reason   string              `json:"reason,omitempty"` // why Unable is set
	RanAt    time.Time           `json:"ran_at"`
}

// LiveFeatureResult is one feature's outcome.
type LiveFeatureResult struct {
	Name    string `json:"name"`
	Command string `json:"command"`
	Example string `json:"example"`
	Status  string `json:"status"` // "pass", "fail", "skip"
	Reason  string `json:"reason,omitempty"`
}

// RunLiveCheck samples each novel feature's Example command against the real
// CLI. Returns an Unable=true result (not an error) when research.json or the
// binary is missing — the scorecard treats those as "could not run" rather
// than failure, so an absent check doesn't penalize the CLI.
//
// cliDir is the printed CLI's root (containing the built binary). binaryName
// is the executable name (e.g., "recipe-goat-pp-cli"). timeout bounds each
// command; 10s is usually enough for list/search/get calls.
func RunLiveCheck(cliDir, binaryName string, timeout time.Duration) *LiveCheckResult {
	out := &LiveCheckResult{RanAt: time.Now().UTC()}

	research, err := LoadResearch(cliDir)
	if err != nil {
		out.Unable = true
		out.Reason = "no research.json: " + err.Error()
		return out
	}

	features := pickFeatures(research)
	if len(features) == 0 {
		out.Unable = true
		out.Reason = "no novel features with Example commands to sample"
		return out
	}

	binaryPath := filepath.Join(cliDir, binaryName)
	info, statErr := os.Stat(binaryPath)
	if statErr != nil {
		out.Unable = true
		out.Reason = fmt.Sprintf("binary %q not found: %v", binaryPath, statErr)
		return out
	}
	if info.Mode()&0o111 == 0 {
		out.Unable = true
		out.Reason = fmt.Sprintf("binary %q is not executable", binaryPath)
		return out
	}

	if timeout <= 0 {
		timeout = 10 * time.Second
	}

	for _, f := range features {
		result := runOneFeatureCheck(cliDir, binaryPath, f, timeout)
		out.Features = append(out.Features, result)
		out.Checked++
		switch result.Status {
		case "pass":
			out.Passed++
		case "fail":
			out.Failed++
		default:
			out.Skipped++
		}
	}

	if out.Checked > 0 {
		out.PassRate = float64(out.Passed) / float64(out.Checked)
	}
	return out
}

// pickFeatures returns the novel features to sample. Prefers NovelFeaturesBuilt
// (the verified subset) when dogfood has run; falls back to NovelFeatures.
// Only features with a non-empty Example are usable — the rest are skipped
// silently since we have no invocation to run.
func pickFeatures(r *ResearchResult) []NovelFeature {
	source := r.NovelFeatures
	if r.NovelFeaturesBuilt != nil && len(*r.NovelFeaturesBuilt) > 0 {
		source = *r.NovelFeaturesBuilt
	}
	var out []NovelFeature
	for _, f := range source {
		if strings.TrimSpace(f.Example) != "" {
			out = append(out, f)
		}
	}
	return out
}

// runOneFeatureCheck parses the Example invocation, runs it against the real
// binary, and evaluates the output shape. The Example is expected to start
// with the binary name (e.g., "recipe-goat-pp-cli goat \"brownies\" --limit 5");
// we drop that prefix and replace it with the absolute binary path so the
// check works regardless of the caller's PATH.
//
// Note: runCLIWithOutput in runtime.go uses CombinedOutput (stdout+stderr
// merged) and wraps non-zero exits as a generic error. This check instead
// separates stdout (for the relevance pass) from stderr (for failure
// messaging) and needs structured access to *exec.ExitError +
// DeadlineExceeded, so it runs exec inline.
func runOneFeatureCheck(cliDir, binaryPath string, f NovelFeature, timeout time.Duration) LiveFeatureResult {
	result := LiveFeatureResult{Name: f.Name, Command: f.Command, Example: f.Example}

	args, err := parseExampleArgs(f.Example)
	if err != nil {
		result.Status = "skip"
		result.Reason = "could not parse example: " + err.Error()
		return result
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, binaryPath, args...)
	cmd.Dir = cliDir
	output, runErr := cmd.Output()
	if ctx.Err() == context.DeadlineExceeded {
		result.Status = "fail"
		result.Reason = fmt.Sprintf("timed out after %s", timeout)
		return result
	}

	// Exit 0 and non-empty output is the minimum bar.
	if runErr != nil {
		var exitErr *exec.ExitError
		if errors.As(runErr, &exitErr) {
			result.Status = "fail"
			result.Reason = fmt.Sprintf("exit %d: %s", exitErr.ExitCode(), trimStderr(exitErr.Stderr))
			return result
		}
		result.Status = "fail"
		result.Reason = "run error: " + runErr.Error()
		return result
	}
	if len(strings.TrimSpace(string(output))) == 0 {
		result.Status = "fail"
		result.Reason = "empty output"
		return result
	}

	// Relevance check: when the Example encodes a query (positional string
	// argument that isn't a flag), at least one query token should appear in
	// the output. Filters out cases where a search returns unrelated results.
	if query := extractQueryToken(args); query != "" {
		if !outputMentionsQuery(string(output), query) {
			result.Status = "fail"
			result.Reason = fmt.Sprintf("output does not contain any token from query %q", query)
			return result
		}
	}

	result.Status = "pass"
	return result
}

// parseExampleArgs takes an Example like:
//
//	recipe-goat-pp-cli goat "chicken tikka masala" --limit 5
//
// and returns the subcommand arguments (everything after the binary name).
// Respects double-quoted tokens so queries with spaces stay intact.
func parseExampleArgs(example string) ([]string, error) {
	tokens, err := shellSplit(example)
	if err != nil {
		return nil, err
	}
	if len(tokens) < 2 {
		return nil, fmt.Errorf("example has no subcommand: %q", example)
	}
	// Drop the binary-name prefix; the live check injects the absolute path.
	return tokens[1:], nil
}

// shellSplit handles double-quoted tokens — enough for the Example formats
// the absorb phase produces. No shell metacharacter interpolation is done.
func shellSplit(s string) ([]string, error) {
	var tokens []string
	var current strings.Builder
	inQuote := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '"':
			inQuote = !inQuote
		case c == ' ' && !inQuote:
			if current.Len() > 0 {
				tokens = append(tokens, current.String())
				current.Reset()
			}
		default:
			current.WriteByte(c)
		}
	}
	if inQuote {
		return nil, fmt.Errorf("unclosed quote in %q", s)
	}
	if current.Len() > 0 {
		tokens = append(tokens, current.String())
	}
	return tokens, nil
}

// extractQueryToken returns a positional argument that looks like a human-
// readable search query — the kind of token a search/filter command would
// mention in its output. Returns "" when no such argument exists, in which
// case no relevance check is performed.
//
// URLs and IDs are intentionally excluded: the CLI's output for a URL-based
// command (recipe get <url>) wouldn't contain the URL itself, so matching
// against it produces false negatives.
//
// Examples:
//
//	["goat", "brownies", "--limit", "5"]  → "brownies"
//	["sub", "buttermilk"]                 → "buttermilk"
//	["recipe", "get", "https://foo/bar"]  → "" (URL, skip relevance check)
//	["cookbook", "list", "--json"]        → "" (no query)
func extractQueryToken(args []string) string {
	// Collect positionals before the first flag.
	var positionals []string
	for _, arg := range args {
		if strings.HasPrefix(arg, "-") {
			break
		}
		positionals = append(positionals, arg)
	}
	// Drop the leading subcommand word(s). The last positional is the most
	// likely candidate for a query: for `goat brownies` it's "brownies"; for
	// `recipe get URL` it's the URL (then filtered out as not-a-query below).
	if len(positionals) < 2 {
		return ""
	}
	candidate := positionals[len(positionals)-1]
	if looksLikeURLOrID(candidate) {
		return ""
	}
	if len(candidate) < 3 {
		return ""
	}
	return candidate
}

// looksLikeURLOrID returns true for tokens that shouldn't be used as search-
// relevance queries: URLs, numeric IDs, UUIDs. The CLI output for a
// get-by-id command won't contain the ID as text.
func looksLikeURLOrID(s string) bool {
	if strings.Contains(s, "://") || strings.HasPrefix(s, "/") {
		return true
	}
	allDigits := true
	for _, c := range s {
		if c < '0' || c > '9' {
			allDigits = false
			break
		}
	}
	return allDigits
}

// outputMentionsQuery is case-insensitive; splits the query on whitespace
// and succeeds if any token (with singular/plural tolerance) appears in the
// output. Mirrors the permissive relevance check used inside generated CLIs.
func outputMentionsQuery(output, query string) bool {
	lowered := strings.ToLower(output)
	for _, tok := range strings.Fields(strings.ToLower(query)) {
		tok = strings.TrimFunc(tok, func(r rune) bool { return r == '"' || r == '\'' })
		if len(tok) < 3 {
			continue
		}
		if strings.Contains(lowered, tok) {
			return true
		}
		// Singular/plural tolerance: "brownies" should match "brownie".
		if strings.HasSuffix(tok, "s") && len(tok) > 3 {
			if strings.Contains(lowered, tok[:len(tok)-1]) {
				return true
			}
		}
	}
	return false
}

func trimStderr(b []byte) string {
	s := strings.TrimSpace(string(b))
	if len(s) > 300 {
		s = s[:300] + "..."
	}
	return s
}

// InsightCapFromLiveCheck returns the maximum Insight score a CLI should
// receive given its live-check pass rate. A CLI whose flagships return
// broken output shouldn't earn a Grade A scorecard.
//
//   - Unable or Checked==0: no cap (nil return)
//   - PassRate >= 0.8: no cap
//   - PassRate >= 0.5: cap at 7
//   - PassRate <  0.5: cap at 4
func InsightCapFromLiveCheck(r *LiveCheckResult) *int {
	if r == nil || r.Unable || r.Checked == 0 {
		return nil
	}
	var cap int
	switch {
	case r.PassRate >= 0.8:
		return nil
	case r.PassRate >= 0.5:
		cap = 7
	default:
		cap = 4
	}
	return &cap
}

// MarshalJSON emits a rounded percentage alongside the raw PassRate so
// consumers can use pass_rate_pct without parsing floating-point noise.
func (r *LiveCheckResult) MarshalJSON() ([]byte, error) {
	type alias LiveCheckResult
	a := (*alias)(r)
	blob, err := json.Marshal(a)
	if err != nil {
		return nil, err
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(blob, &m); err != nil {
		return nil, err
	}
	// Replace pass_rate with a truncated form and add pass_rate_pct.
	delete(m, "pass_rate")
	m["pass_rate_pct"], _ = json.Marshal(int(r.PassRate*100 + 0.5))
	return json.Marshal(m)
}
