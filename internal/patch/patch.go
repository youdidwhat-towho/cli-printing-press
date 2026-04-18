// Package patch applies PR #218's profile/deliver/feedback features to an
// already-published printing-press CLI without re-rendering its templates.
//
// The patcher works by AST-injecting the cobra wiring into internal/cli/root.go
// (preserving every existing AddCommand, including novel/synthetic commands)
// and dropping in three self-contained companion files rendered from templates.
// It never reads the source spec, never touches per-endpoint command files,
// and never changes the CLI's module path.
//
// See docs/plans/2026-04-18-001-feat-patch-library-clis-v2-plan.md for the
// architecture rationale.
package patch

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// Options controls a single Patch run.
type Options struct {
	// Dir is the CLI root, e.g. ~/Code/printing-press-library/library/productivity/cal-com
	Dir string
	// DryRun prints the file list without writing anything.
	DryRun bool
	// Force overrides a resource-level collision.
	Force bool
	// SkipBuild skips the post-patch `go build` gate.
	SkipBuild bool
}

// Report captures the outcome of a Patch run.
type Report struct {
	Dir           string      `json:"dir"`
	Name          string      `json:"name"`
	FilesCreated  []string    `json:"files_created"`
	FilesModified []string    `json:"files_modified"`
	Collisions    []Collision `json:"collisions,omitempty"`
	Idempotent    bool        `json:"idempotent"`
	BuildOK       bool        `json:"build_ok"`
	BuildOutput   string      `json:"build_output,omitempty"`
	DryRun        bool        `json:"dry_run"`
}

// Collision is a conflict between the patcher's output and an existing
// symbol or file in the target CLI.
type Collision struct {
	Kind    string `json:"kind"`    // "resource", "flag", "command"
	Symbol  string `json:"symbol"`  // e.g. "feedback", "newProfileCmd"
	File    string `json:"file"`    // existing file path that collides, if any
	Message string `json:"message"` // human-readable explanation
}

// cliProvenance is the subset of .printing-press.json the patcher needs.
type cliProvenance struct {
	APIName string `json:"api_name"`
	CLIName string `json:"cli_name"`
}

// Patch applies PR #218's features to the CLI at opts.Dir and returns a Report.
// The CLI's own files are only modified when DryRun is false and there are no
// fatal collisions (unless Force is set).
func Patch(opts Options) (*Report, error) {
	prov, err := readProvenance(opts.Dir)
	if err != nil {
		return nil, err
	}
	report := &Report{
		Dir:    opts.Dir,
		Name:   prov.APIName,
		DryRun: opts.DryRun,
	}

	rootPath := filepath.Join(opts.Dir, "internal", "cli", "root.go")
	rootSrc, err := os.ReadFile(rootPath)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", rootPath, err)
	}

	collisions, err := detectCollisions(opts.Dir, rootSrc)
	if err != nil {
		return nil, fmt.Errorf("collision detection: %w", err)
	}
	fatal := filterFatalCollisions(collisions)
	if len(fatal) > 0 && !opts.Force {
		report.Collisions = fatal
		return report, nil
	}
	report.Collisions = collisions

	patchedRoot, rootChanged, err := injectRootAST(rootSrc)
	if err != nil {
		return nil, fmt.Errorf("AST injection: %w", err)
	}

	dropins := planDropins(opts.Dir, fatal)

	if !rootChanged && len(dropins) == 0 {
		report.Idempotent = true
		report.BuildOK = true
		return report, nil
	}

	if opts.DryRun {
		if rootChanged {
			report.FilesModified = append(report.FilesModified, rootPath)
		}
		for _, d := range dropins {
			report.FilesCreated = append(report.FilesCreated, d.path)
		}
		return report, nil
	}

	if rootChanged {
		if err := os.WriteFile(rootPath, patchedRoot, 0o644); err != nil {
			return nil, fmt.Errorf("writing %s: %w", rootPath, err)
		}
		report.FilesModified = append(report.FilesModified, rootPath)
	}

	owner := extractOwner(opts.Dir)
	for _, d := range dropins {
		if err := renderDropin(d, prov.APIName, owner); err != nil {
			return nil, fmt.Errorf("rendering %s: %w", d.path, err)
		}
		report.FilesCreated = append(report.FilesCreated, d.path)
	}

	if err := goimportsDir(filepath.Join(opts.Dir, "internal", "cli")); err != nil {
		return nil, fmt.Errorf("goimports: %w", err)
	}

	if !opts.SkipBuild {
		out, buildErr := exec.Command("go", "build", "./...").CombinedOutput()
		report.BuildOK = buildErr == nil
		if buildErr != nil {
			report.BuildOutput = string(out)
		}
	} else {
		report.BuildOK = true
	}

	return report, nil
}

func readProvenance(dir string) (*cliProvenance, error) {
	data, err := os.ReadFile(filepath.Join(dir, ".printing-press.json"))
	if err != nil {
		return nil, fmt.Errorf("reading .printing-press.json: %w", err)
	}
	var prov cliProvenance
	if err := json.Unmarshal(data, &prov); err != nil {
		return nil, fmt.Errorf("parsing .printing-press.json: %w", err)
	}
	if prov.APIName == "" {
		return nil, fmt.Errorf(".printing-press.json: api_name is required")
	}
	return &prov, nil
}

func filterFatalCollisions(all []Collision) []Collision {
	var fatal []Collision
	for _, c := range all {
		if c.Kind == "resource" {
			fatal = append(fatal, c)
		}
	}
	return fatal
}

// goimportsDir runs `goimports -w` over the given directory. Missing binary
// falls back to `gofmt -w` since goimports is a superset.
func goimportsDir(dir string) error {
	cmd := exec.Command("goimports", "-w", dir)
	if err := cmd.Run(); err == nil {
		return nil
	}
	// Fallback: gofmt only sorts imports within a group, won't add/remove
	// groups, but it's better than nothing and is always available.
	out, err := exec.Command("gofmt", "-w", dir).CombinedOutput()
	if err != nil {
		return fmt.Errorf("gofmt: %v: %s", err, bytes.TrimSpace(out))
	}
	return nil
}
