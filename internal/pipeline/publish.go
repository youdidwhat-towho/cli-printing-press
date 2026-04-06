package pipeline

import (
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/mvanhorn/cli-printing-press/catalog"
	catalogpkg "github.com/mvanhorn/cli-printing-press/internal/catalog"
	"github.com/mvanhorn/cli-printing-press/internal/naming"
	"github.com/mvanhorn/cli-printing-press/internal/version"
)

type RunManifest struct {
	Version               int       `json:"version"`
	APIName               string    `json:"api_name"`
	RunID                 string    `json:"run_id"`
	Scope                 string    `json:"scope"`
	GitRoot               string    `json:"git_root"`
	SpecPath              string    `json:"spec_path,omitempty"`
	SpecURL               string    `json:"spec_url,omitempty"`
	WorkingDir            string    `json:"working_dir"`
	PublishedCLIDir       string    `json:"published_cli_dir,omitempty"`
	ArchivedManuscriptDir string    `json:"archived_manuscript_dir,omitempty"`
	CreatedAt             time.Time `json:"created_at"`
	UpdatedAt             time.Time `json:"updated_at"`
}

func BuildRunManifest(state *PipelineState) RunManifest {
	return RunManifest{
		Version:               1,
		APIName:               state.APIName,
		RunID:                 state.RunID,
		Scope:                 state.Scope,
		GitRoot:               repoRoot(),
		SpecPath:              state.SpecPath,
		SpecURL:               state.SpecURL,
		WorkingDir:            state.EffectiveWorkingDir(),
		PublishedCLIDir:       state.PublishedDir,
		ArchivedManuscriptDir: ArchivedManuscriptDir(state.APIName, state.RunID),
		CreatedAt:             state.StartedAt,
		UpdatedAt:             time.Now(),
	}
}

func WriteRunManifest(state *PipelineState) error {
	manifest := BuildRunManifest(state)
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling run manifest: %w", err)
	}
	if err := os.WriteFile(state.ManifestPath(), data, 0o644); err != nil {
		return fmt.Errorf("writing run manifest: %w", err)
	}
	return nil
}

func WriteArchivedManifest(state *PipelineState) error {
	manifest := BuildRunManifest(state)
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling archived manifest: %w", err)
	}
	if err := os.MkdirAll(ArchivedManuscriptDir(state.APIName, state.RunID), 0o755); err != nil {
		return fmt.Errorf("creating archived manuscript dir: %w", err)
	}
	if err := os.WriteFile(ArchivedManifestPath(state.APIName, state.RunID), data, 0o644); err != nil {
		return fmt.Errorf("writing archived manifest: %w", err)
	}
	return nil
}

func PublishWorkingCLI(state *PipelineState, targetDir string) (string, error) {
	workingDir := state.EffectiveWorkingDir()
	if workingDir == "" {
		return "", fmt.Errorf("working dir is empty")
	}

	finalDir := targetDir
	var err error
	if finalDir == "" {
		finalDir, err = ClaimOutputDir(DefaultOutputDir(state.APIName))
		if err != nil {
			return "", err
		}
	} else {
		finalDir, err = filepath.Abs(finalDir)
		if err != nil {
			return "", fmt.Errorf("resolving publish dir: %w", err)
		}
		if _, err := os.Stat(finalDir); err == nil {
			return "", fmt.Errorf("publish dir already exists: %s", finalDir)
		} else if !os.IsNotExist(err) {
			return "", fmt.Errorf("checking publish dir: %w", err)
		}
	}

	if err := CopyDir(workingDir, finalDir); err != nil {
		return "", fmt.Errorf("publishing CLI: %w", err)
	}

	state.PublishedDir = finalDir

	if err := writeCLIManifestForPublish(state, finalDir); err != nil {
		return "", err
	}

	// Generate smithery.yaml for MCP marketplace listing if applicable.
	if err := writeSmitheryYAML(finalDir); err != nil {
		// Non-blocking: log warning but don't fail the publish.
		fmt.Fprintf(os.Stderr, "warning: could not write smithery.yaml: %v\n", err)
	}

	if err := state.Save(); err != nil {
		return "", err
	}
	if err := WriteRunManifest(state); err != nil {
		return "", err
	}
	return finalDir, nil
}

func ArchiveRunArtifacts(state *PipelineState) (string, error) {
	archiveDir := ArchivedManuscriptDir(state.APIName, state.RunID)
	if err := os.MkdirAll(archiveDir, 0o755); err != nil {
		return "", fmt.Errorf("creating archive dir: %w", err)
	}

	type pair struct {
		src string
		dst string
	}

	pairs := []pair{
		{src: state.ResearchDir(), dst: ArchivedResearchDir(state.APIName, state.RunID)},
		{src: state.ProofsDir(), dst: ArchivedProofsDir(state.APIName, state.RunID)},
		{src: state.PipelineDir(), dst: ArchivedPipelineDir(state.APIName, state.RunID)},
		{src: state.DiscoveryDir(), dst: ArchivedDiscoveryDir(state.APIName, state.RunID)},
	}

	for _, item := range pairs {
		info, err := os.Stat(item.src)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return "", fmt.Errorf("stat %s: %w", item.src, err)
		}
		if !info.IsDir() {
			continue
		}
		if err := CopyDir(item.src, item.dst); err != nil {
			return "", fmt.Errorf("archiving %s: %w", item.src, err)
		}
	}

	if err := WriteArchivedManifest(state); err != nil {
		return "", err
	}
	if err := WriteRunManifest(state); err != nil {
		return "", err
	}
	return archiveDir, nil
}

func writeCLIManifestForPublish(state *PipelineState, dir string) error {
	// Normalize spec_url vs spec_path. The fullrun pipeline sets
	// state.SpecURL to the raw --spec argument (URL or file path)
	// and state.SpecPath = SpecURL for --spec runs. We need to put
	// URLs in spec_url and file paths in spec_path, not both.
	specURL, specPath := state.SpecURL, state.SpecPath
	isURL := strings.HasPrefix(specURL, "http://") || strings.HasPrefix(specURL, "https://")
	if !isURL && specURL != "" {
		// Raw --spec argument was a file path, not a URL.
		specPath = specURL
		specURL = ""
	}
	if isURL {
		// Don't duplicate a URL into spec_path.
		if specPath == specURL {
			specPath = ""
		}
	}

	m := CLIManifest{
		SchemaVersion:        1,
		GeneratedAt:          time.Now().UTC(),
		PrintingPressVersion: version.Version,
		APIName:              state.APIName,
		CLIName:              naming.CLI(state.APIName),
		SpecURL:              specURL,
		SpecPath:             specPath,
		RunID:                state.RunID,
	}

	// Detect spec format and compute checksum from the spec file in the
	// working directory. spec.json only exists when specFlag is --spec;
	// for --docs runs it won't be present and these fields stay empty.
	specFile := filepath.Join(state.EffectiveWorkingDir(), "spec.json")
	if data, err := os.ReadFile(specFile); err == nil {
		m.SpecFormat = detectSpecFormat(data)
		checksum, err := specChecksum(specFile)
		if err == nil {
			m.SpecChecksum = checksum
		}
	}

	// Look up catalog entry by API name; empty string if not found.
	if entry, err := catalogpkg.LookupFS(catalog.FS, state.APIName); err == nil {
		m.CatalogEntry = entry.Name
		m.Category = entry.Category
		m.Description = entry.Description
	}

	return WriteCLIManifest(dir, m)
}

// writeSmitheryYAML generates a smithery.yaml marketplace metadata file
// alongside the CLI manifest. Reads .printing-press.json from dir to get
// MCP metadata. Skips writing if MCPReady is "cli-only" or if no MCP
// metadata is present.
func writeSmitheryYAML(dir string) error {
	data, err := os.ReadFile(filepath.Join(dir, CLIManifestFilename))
	if err != nil {
		return nil // no manifest, nothing to do
	}
	var m CLIManifest
	if err := json.Unmarshal(data, &m); err != nil {
		return fmt.Errorf("parsing manifest for smithery: %w", err)
	}
	if m.MCPBinary == "" || m.MCPReady == "cli-only" {
		return nil // no MCP or cli-only — skip
	}

	var sb strings.Builder
	sb.WriteString("name: " + m.MCPBinary + "\n")
	desc := m.Description
	if desc == "" {
		desc = m.APIName + " API"
	}
	sb.WriteString("description: \"" + desc + "\"\n")
	sb.WriteString("startCommand:\n")
	sb.WriteString("  command: ./" + m.MCPBinary + "\n")

	if len(m.AuthEnvVars) > 0 && m.AuthType != "cookie" && m.AuthType != "composed" {
		sb.WriteString("env:\n")
		for _, envVar := range m.AuthEnvVars {
			sb.WriteString("  " + envVar + ":\n")
			sb.WriteString("    description: \"" + m.APIName + " API credential\"\n")
			sb.WriteString("    required: true\n")
		}
	} else if len(m.AuthEnvVars) > 0 {
		// Cookie/composed: env vars are optional (some tools work without)
		sb.WriteString("env:\n")
		for _, envVar := range m.AuthEnvVars {
			sb.WriteString("  " + envVar + ":\n")
			sb.WriteString("    description: \"Required for authenticated endpoints only — some tools work without credentials\"\n")
			sb.WriteString("    required: false\n")
		}
	}

	return os.WriteFile(filepath.Join(dir, "smithery.yaml"), []byte(sb.String()), 0o644)
}

func CopyDir(src, dst string) error {
	info, err := os.Stat(src)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("%s is not a directory", src)
	}

	if err := os.MkdirAll(dst, info.Mode()); err != nil {
		return err
	}

	srcRoot, err := filepath.Abs(src)
	if err != nil {
		return fmt.Errorf("resolving source dir: %w", err)
	}

	// WalkDir (unlike Walk) does not follow directory symlinks, so the
	// callback sees them as symlink entries and we can validate them
	// without descending into potentially huge or circular targets.
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == src {
			return nil
		}

		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)

		// d.Type() returns mode bits from Lstat, so symlinks (including
		// directory symlinks) are detected before any descent.
		if d.Type()&os.ModeSymlink != 0 {
			link, err := os.Readlink(path)
			if err != nil {
				return err
			}
			ok, err := symlinkTargetWithinRoot(srcRoot, path, link)
			if err != nil {
				return err
			}
			if !ok {
				return fmt.Errorf("symlink %s points outside source tree", path)
			}
			return os.Symlink(link, target)
		}

		if d.IsDir() {
			info, err := d.Info()
			if err != nil {
				return err
			}
			return os.MkdirAll(target, info.Mode())
		}

		info, err := d.Info()
		if err != nil {
			return err
		}
		return copyFile(path, target, info.Mode())
	})
}

func symlinkTargetWithinRoot(root, path, link string) (bool, error) {
	resolved := link
	if !filepath.IsAbs(resolved) {
		resolved = filepath.Join(filepath.Dir(path), resolved)
	}

	absResolved, err := filepath.Abs(resolved)
	if err != nil {
		return false, fmt.Errorf("resolving symlink target for %s: %w", path, err)
	}

	rel, err := filepath.Rel(root, absResolved)
	if err != nil {
		return false, fmt.Errorf("checking symlink target for %s: %w", path, err)
	}

	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator))), nil
}

func copyFile(src, dst string, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}

	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	defer func() { _ = out.Close() }()

	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Close()
}
