package pipeline

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
)

// BundleParams describes one MCPB bundle build. CLIDir must contain a
// manifest.json (emitted by WriteMCPBManifest) and a built MCP binary at
// BinaryPath. OutputPath is where the .mcpb file will be written; the
// caller is responsible for choosing a path that includes platform
// information so multi-platform builds don't overwrite each other.
type BundleParams struct {
	CLIDir     string
	BinaryPath string
	OutputPath string
}

// BuildMCPBBundle assembles an MCPB ZIP at OutputPath. The bundle layout is:
//
//	manifest.json
//	bin/<binary>            (the path declared by manifest's server.entry_point)
//
// Returns nil and creates no file when manifest.json is missing — the
// caller's CLI dir is presumably one we don't want to bundle (cli-only
// readiness, no MCP, etc., the same gates WriteMCPBManifest uses).
func BuildMCPBBundle(params BundleParams) error {
	manifestPath := filepath.Join(params.CLIDir, MCPBManifestFilename)
	manifestData, err := os.ReadFile(manifestPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("reading manifest: %w", err)
	}

	var manifest MCPBManifest
	if err := json.Unmarshal(manifestData, &manifest); err != nil {
		return fmt.Errorf("parsing manifest: %w", err)
	}
	if manifest.Server.EntryPoint == "" {
		return errors.New("manifest server.entry_point is empty")
	}

	binStat, err := os.Stat(params.BinaryPath)
	if err != nil {
		return fmt.Errorf("locating MCP binary at %s: %w", params.BinaryPath, err)
	}
	binData, err := os.ReadFile(params.BinaryPath)
	if err != nil {
		return fmt.Errorf("reading MCP binary: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(params.OutputPath), 0o755); err != nil {
		return fmt.Errorf("creating bundle output dir: %w", err)
	}

	out, err := os.Create(params.OutputPath)
	if err != nil {
		return fmt.Errorf("creating bundle file: %w", err)
	}
	defer func() { _ = out.Close() }()

	zw := zip.NewWriter(out)
	if err := writeZipEntry(zw, MCPBManifestFilename, manifestData, 0o644); err != nil {
		_ = zw.Close()
		return fmt.Errorf("writing manifest into bundle: %w", err)
	}
	// Preserve executable bit so hosts that respect zip POSIX mode bits
	// (Claude Desktop on macOS, MCP for Windows) can launch the binary
	// directly without an extra chmod step.
	if err := writeZipEntry(zw, manifest.Server.EntryPoint, binData, binStat.Mode()&0o777); err != nil {
		_ = zw.Close()
		return fmt.Errorf("writing binary into bundle: %w", err)
	}
	if err := zw.Close(); err != nil {
		return fmt.Errorf("finalizing bundle archive: %w", err)
	}
	return nil
}

func writeZipEntry(zw *zip.Writer, name string, data []byte, mode os.FileMode) error {
	header := &zip.FileHeader{Name: name, Method: zip.Deflate}
	header.SetMode(mode)
	w, err := zw.CreateHeader(header)
	if err != nil {
		return err
	}
	_, err = io.Copy(w, bytes.NewReader(data))
	return err
}

// DefaultBundleOutputPath returns the conventional path the generator and
// `printing-press bundle` use when no --output is set. Platform suffix in
// the filename keeps cross-compiled bundles from clobbering each other.
func DefaultBundleOutputPath(cliDir, mcpBinary, goos, goarch string) string {
	if goos == "" {
		goos = runtime.GOOS
	}
	if goarch == "" {
		goarch = runtime.GOARCH
	}
	name := fmt.Sprintf("%s-%s-%s.mcpb", mcpBinary, goos, goarch)
	return filepath.Join(cliDir, "build", name)
}

// StagedMCPBinaryPath returns the conventional path where bundle's
// pre-zip staging copies of the MCP binary live (cliDir/build/stage/bin/).
// Exposed so internal/cli callers don't reach into pipeline internals
// to construct the path themselves.
func StagedMCPBinaryPath(cliDir, mcpBinary string) string {
	return filepath.Join(cliDir, "build", "stage", "bin", mcpBinary)
}
