package naming

import (
	"regexp"
	"strings"
	"unicode"

	"golang.org/x/text/cases"
	"golang.org/x/text/language"
	"golang.org/x/text/unicode/norm"
)

const (
	CurrentCLISuffix = "-pp-cli"
	LegacyCLISuffix  = "-cli"
	MCPSuffix        = "-pp-mcp"
)

func CLI(name string) string {
	return name + CurrentCLISuffix
}

func MCP(name string) string {
	return name + MCPSuffix
}

func LegacyCLI(name string) string {
	return name + LegacyCLISuffix
}

func ValidationBinary(name string) string {
	return CLI(name) + "-validation"
}

// HumanName turns a kebab-case slug into a space-separated title-cased
// string ("steam-web" → "Steam Web", "company-goat" → "Company Goat").
// Multi-byte rune safe via cases.Title; previous hand-rolled callers
// using `s[:1]` would slice mid-codepoint on accented inputs.
func HumanName(slug string) string {
	if slug == "" {
		return ""
	}
	return cases.Title(language.English).String(strings.ReplaceAll(slug, "-", " "))
}

// EnvPrefix returns an ASCII-only shell-safe environment variable prefix.
// API display names and OpenAPI titles can contain accents or punctuation
// ("PokéAPI", "Cal.com", "1Password"); generated env vars must not.
func EnvPrefix(name string) string {
	var b strings.Builder
	lastUnderscore := false
	for _, r := range norm.NFD.String(name) {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r - ('a' - 'A'))
			lastUnderscore = false
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r)
			lastUnderscore = false
		case r >= '0' && r <= '9':
			b.WriteRune(r)
			lastUnderscore = false
		case unicode.Is(unicode.Mn, r):
			continue
		default:
			if !lastUnderscore && b.Len() > 0 {
				b.WriteByte('_')
				lastUnderscore = true
			}
		}
	}
	out := strings.Trim(b.String(), "_")
	if out == "" {
		return "API"
	}
	if out[0] >= '0' && out[0] <= '9' {
		return "API_" + out
	}
	return out
}

// Snake converts CamelCase to snake_case for generated tool name segments.
// Hyphens are intentionally preserved to match the historical MCP template
// helper behavior.
func Snake(s string) string {
	var result strings.Builder
	for i, r := range s {
		if unicode.IsUpper(r) && i > 0 {
			result.WriteRune('_')
		}
		result.WriteRune(unicode.ToLower(r))
	}
	return result.String()
}

// EnvVarPlaceholder derives the placeholder name from an environment variable.
// DUB_TOKEN -> token, STYTCH_PROJECT_ID -> project_id.
func EnvVarPlaceholder(envVar string) string {
	parts := strings.Split(envVar, "_")
	if len(parts) <= 1 {
		return strings.ToLower(envVar)
	}
	lower := make([]string, 0, len(parts)-1)
	for _, p := range parts[1:] {
		lower = append(lower, strings.ToLower(p))
	}
	return strings.Join(lower, "_")
}

// OneLine normalizes generated descriptions for compact template and manifest
// output.
func OneLine(s string) string {
	s = strings.ReplaceAll(s, "\r\n", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\r", " ")
	s = strings.ReplaceAll(s, `"`, `'`)
	s = strings.ReplaceAll(s, "\\", "")
	for strings.Contains(s, "  ") {
		s = strings.ReplaceAll(s, "  ", " ")
	}
	s = strings.TrimSpace(s)
	if len(s) > 120 {
		cut := s[:117]
		if idx := strings.LastIndex(cut, " "); idx > 60 {
			s = cut[:idx] + "..."
		} else {
			s = cut + "..."
		}
	}
	return s
}

// MCPDescription builds an MCP tool description with optional minority-side
// auth annotation. It annotates only when an API has a mix of public and
// auth-required tools, and only the minority side gets annotated.
func MCPDescription(desc string, noAuth bool, authType string, publicCount, totalCount int) string {
	authCount := totalCount - publicCount
	mixed := publicCount > 0 && authCount > 0

	if mixed {
		if noAuth && publicCount < authCount {
			desc = desc + " (public)"
		} else if !noAuth && authCount < publicCount {
			switch authType {
			case "api_key":
				desc = desc + " (requires API key)"
			case "cookie", "composed":
				desc = desc + " (requires browser login)"
			case "oauth2", "bearer_token":
				desc = desc + " (requires auth)"
			default:
				desc = desc + " (requires auth)"
			}
		}
	}

	return OneLine(desc)
}

func DogfoodBinary(name string) string {
	return CLI(name) + "-dogfood"
}

func IsCLIDirName(name string) bool {
	trimmed := trimNumericRunSuffix(name)
	return strings.HasSuffix(trimmed, CurrentCLISuffix) || strings.HasSuffix(trimmed, LegacyCLISuffix)
}

func TrimCLISuffix(name string) string {
	name = trimNumericRunSuffix(name)

	switch {
	case strings.HasSuffix(name, CurrentCLISuffix):
		return strings.TrimSuffix(name, CurrentCLISuffix)
	case strings.HasSuffix(name, LegacyCLISuffix):
		return strings.TrimSuffix(name, LegacyCLISuffix)
	default:
		return name
	}
}

// LibraryDirName maps a CLI-style name to the corresponding library directory
// key while preserving rerun suffixes. Examples:
//   - "dub-pp-cli" -> "dub"
//   - "dub-pp-cli-2" -> "dub-2"
//   - "dub-2-pp-cli" -> "dub-2"
//
// Bare slug-keyed names are returned unchanged.
func LibraryDirName(name string) string {
	trimmed := trimNumericRunSuffix(name)

	switch {
	case strings.HasSuffix(trimmed, CurrentCLISuffix):
		return strings.Replace(name, CurrentCLISuffix, "", 1)
	case strings.HasSuffix(trimmed, LegacyCLISuffix):
		return strings.Replace(name, LegacyCLISuffix, "", 1)
	default:
		return name
	}
}

// slugRe matches the slug grammar: lowercase alphanumeric + hyphens, must start
// with an alphanumeric character. Accepts rerun suffixes like "dub-2".
var slugRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

// IsValidLibraryDirName returns true if name is a valid library directory name.
// It accepts both legacy CLI directory names (e.g. "dub-pp-cli", "dub-pp-cli-2")
// and slug-keyed names (e.g. "dub", "cal-com", "dub-2"). It rejects empty strings,
// path separators, ".." components, and dotfiles. This is Layer 1 input validation;
// callers that use the name in filepath.Join must still apply Layer 2 containment.
func IsValidLibraryDirName(name string) bool {
	if name == "" {
		return false
	}
	if strings.HasPrefix(name, ".") {
		return false
	}
	if strings.Contains(name, "/") || strings.Contains(name, string([]byte{0})) {
		return false
	}
	if strings.Contains(name, "..") {
		return false
	}
	// Accept legacy CLI directory names
	if IsCLIDirName(name) {
		return true
	}
	// Accept slug grammar
	return slugRe.MatchString(name)
}

func trimNumericRunSuffix(name string) string {
	idx := strings.LastIndex(name, "-")
	if idx == -1 {
		return name
	}

	suffix := name[idx+1:]
	if suffix == "" {
		return name
	}
	for _, r := range suffix {
		if r < '0' || r > '9' {
			return name
		}
	}
	return name[:idx]
}
