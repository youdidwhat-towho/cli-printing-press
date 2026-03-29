package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/mvanhorn/cli-printing-press/internal/naming"
	"github.com/mvanhorn/cli-printing-press/internal/pipeline"
	"github.com/spf13/cobra"
)

func newPublishCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "publish",
		Short: "Validate and package CLIs for publishing",
		Example: `  # Validate a CLI before publishing
  printing-press publish validate --dir ~/printing-press/library/notion-pp-cli --json

  # Package a CLI for publishing
  printing-press publish package --dir ~/printing-press/library/notion-pp-cli --category productivity --target /tmp/staging --json`,
	}

	cmd.AddCommand(newPublishValidateCmd())
	cmd.AddCommand(newPublishPackageCmd())

	return cmd
}

// CheckResult represents a single validation check.
type CheckResult struct {
	Name    string `json:"name"`
	Passed  bool   `json:"passed"`
	Error   string `json:"error,omitempty"`
	Warning string `json:"warning,omitempty"`
}

// ValidateResult is the JSON output of publish validate.
type ValidateResult struct {
	Passed     bool          `json:"passed"`
	CLIName    string        `json:"cli_name"`
	APIName    string        `json:"api_name"`
	HelpOutput string        `json:"help_output,omitempty"`
	Checks     []CheckResult `json:"checks"`
}

// PackageResult is the JSON output of publish package.
type PackageResult struct {
	StagedDir            string `json:"staged_dir"`
	CLIName              string `json:"cli_name"`
	APIName              string `json:"api_name"`
	Category             string `json:"category"`
	ManuscriptsIncluded  bool   `json:"manuscripts_included"`
	RunID                string `json:"run_id,omitempty"`
}

func newPublishValidateCmd() *cobra.Command {
	var dir string
	var asJSON bool

	cmd := &cobra.Command{
		Use:   "validate",
		Short: "Validate a CLI is ready for publishing",
		Example: `  printing-press publish validate --dir ~/printing-press/library/notion-pp-cli
  printing-press publish validate --dir ~/printing-press/library/notion-pp-cli --json`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if dir == "" {
				return &ExitError{Code: ExitInputError, Err: fmt.Errorf("--dir is required")}
			}

			result := runValidation(dir)

			if asJSON {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				if encErr := enc.Encode(result); encErr != nil {
					return encErr
				}
				if !result.Passed {
					return &ExitError{Code: ExitPublishError, Err: fmt.Errorf("validation failed")}
				}
				return nil
			}

			// Human-readable output
			for _, c := range result.Checks {
				status := "PASS"
				if !c.Passed {
					status = "FAIL"
				}
				if c.Warning != "" {
					status = "WARN"
				}
				fmt.Fprintf(os.Stderr, "  %-20s %s", c.Name, status)
				if c.Error != "" {
					fmt.Fprintf(os.Stderr, "  %s", c.Error)
				}
				if c.Warning != "" {
					fmt.Fprintf(os.Stderr, "  %s", c.Warning)
				}
				fmt.Fprintln(os.Stderr)
			}

			if !result.Passed {
				return &ExitError{Code: ExitPublishError, Err: fmt.Errorf("validation failed")}
			}
			fmt.Fprintln(os.Stderr, "\nAll checks passed.")
			return nil
		},
	}

	cmd.Flags().StringVar(&dir, "dir", "", "CLI directory to validate (required)")
	cmd.Flags().BoolVar(&asJSON, "json", false, "Output as JSON")

	return cmd
}

func newPublishPackageCmd() *cobra.Command {
	var dir string
	var category string
	var target string
	var asJSON bool

	cmd := &cobra.Command{
		Use:   "package",
		Short: "Package a CLI for publishing to the library repo",
		Example: `  printing-press publish package --dir ~/printing-press/library/notion-pp-cli --category productivity --target /tmp/staging --json`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if dir == "" {
				return &ExitError{Code: ExitInputError, Err: fmt.Errorf("--dir is required")}
			}
			if category == "" {
				return &ExitError{Code: ExitInputError, Err: fmt.Errorf("--category is required")}
			}
			if target == "" {
				return &ExitError{Code: ExitInputError, Err: fmt.Errorf("--target is required")}
			}

			// Check target doesn't exist (before expensive validation)
			if _, err := os.Stat(target); err == nil {
				return &ExitError{Code: ExitInputError, Err: fmt.Errorf("target directory already exists: %s", target)}
			}

			// Re-validate before packaging
			vResult := runValidation(dir)
			if !vResult.Passed {
				if asJSON {
					enc := json.NewEncoder(os.Stdout)
					enc.SetIndent("", "  ")
					return enc.Encode(vResult)
				}
				return &ExitError{Code: ExitPublishError, Err: fmt.Errorf("validation failed, cannot package")}
			}

			// Determine CLI name from manifest or directory name
			cliName := vResult.CLIName
			if cliName == "" {
				cliName = filepath.Base(dir)
			}

			// Build staging structure: target/library/<category>/<cli-name>/
			stagingCLIDir := filepath.Join(target, "library", category, cliName)
			if err := os.MkdirAll(filepath.Dir(stagingCLIDir), 0o755); err != nil {
				return &ExitError{Code: ExitPublishError, Err: fmt.Errorf("creating staging dir: %w", err)}
			}

			// Copy CLI source
			if err := pipeline.CopyDir(dir, stagingCLIDir); err != nil {
				return &ExitError{Code: ExitPublishError, Err: fmt.Errorf("copying CLI: %w", err)}
			}

			// Resolve and copy manuscripts
			result := PackageResult{
				StagedDir: stagingCLIDir,
				CLIName:   cliName,
				APIName:   vResult.APIName,
				Category:  category,
			}

			apiName := vResult.APIName
			if apiName == "" {
				apiName = naming.TrimCLISuffix(cliName)
			}

			msRoot := pipeline.PublishedManuscriptsRoot()
			msAPIDir := filepath.Join(msRoot, apiName)
			runID, err := findMostRecentRun(msAPIDir)
			if err == nil && runID != "" {
				result.RunID = runID
				srcMsDir := filepath.Join(msAPIDir, runID)
				dstMsDir := filepath.Join(stagingCLIDir, ".manuscripts", runID)
				if err := pipeline.CopyDir(srcMsDir, dstMsDir); err != nil {
					fmt.Fprintf(os.Stderr, "warning: could not copy manuscripts: %v\n", err)
				} else {
					result.ManuscriptsIncluded = true
				}
			} else {
				fmt.Fprintln(os.Stderr, "warning: no manuscripts found, packaging without them")
			}

			if asJSON {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(result)
			}

			fmt.Fprintf(os.Stderr, "Packaged %s at %s\n", cliName, stagingCLIDir)
			if result.ManuscriptsIncluded {
				fmt.Fprintf(os.Stderr, "  Manuscripts: %s (run %s)\n", ".manuscripts/"+runID, runID)
			} else {
				fmt.Fprintln(os.Stderr, "  Manuscripts: not included (none found)")
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&dir, "dir", "", "CLI directory to package (required)")
	cmd.Flags().StringVar(&category, "category", "", "Category for the CLI (required)")
	cmd.Flags().StringVar(&target, "target", "", "Staging directory to create (required)")
	cmd.Flags().BoolVar(&asJSON, "json", false, "Output as JSON")

	return cmd
}

func runValidation(dir string) ValidateResult {
	result := ValidateResult{}
	allPassed := true

	// 1. Manifest check
	manifestPath := filepath.Join(dir, pipeline.CLIManifestFilename)
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		result.Checks = append(result.Checks, CheckResult{Name: "manifest", Passed: false, Error: "missing .printing-press.json"})
		allPassed = false
	} else {
		var m pipeline.CLIManifest
		if err := json.Unmarshal(data, &m); err != nil {
			result.Checks = append(result.Checks, CheckResult{Name: "manifest", Passed: false, Error: fmt.Sprintf("invalid JSON: %v", err)})
			allPassed = false
		} else {
			if m.APIName == "" || m.CLIName == "" {
				result.Checks = append(result.Checks, CheckResult{Name: "manifest", Passed: false, Error: "missing required fields (api_name, cli_name)"})
				allPassed = false
			} else {
				result.Checks = append(result.Checks, CheckResult{Name: "manifest", Passed: true})
				result.CLIName = m.CLIName
				result.APIName = m.APIName
			}
		}
	}

	// 2. go mod tidy check
	tidyCheck := runGoCheck(dir, "mod", "tidy")
	if tidyCheck.Passed {
		// Check if go.sum or go.mod changed (tidy made modifications)
		diffCmd := exec.Command("git", "diff", "--name-only", "go.mod", "go.sum")
		diffCmd.Dir = dir
		if diffOut, err := diffCmd.Output(); err == nil && len(strings.TrimSpace(string(diffOut))) > 0 {
			tidyCheck.Passed = false
			tidyCheck.Error = "go.mod or go.sum is not tidy"
			allPassed = false
			// Restore original state
			restoreCmd := exec.Command("git", "checkout", "--", "go.mod", "go.sum")
			restoreCmd.Dir = dir
			_ = restoreCmd.Run()
		}
	} else {
		allPassed = false
	}
	result.Checks = append(result.Checks, tidyCheck)

	// 3. go vet check
	vetCheck := runGoCheck(dir, "vet", "./...")
	if !vetCheck.Passed {
		allPassed = false
	}
	result.Checks = append(result.Checks, vetCheck)

	// 4. go build check
	buildCheck := runGoCheck(dir, "build", "./...")
	if !buildCheck.Passed {
		allPassed = false
	}
	result.Checks = append(result.Checks, buildCheck)

	// 5. --help check
	cliName := result.CLIName
	if cliName == "" {
		cliName = filepath.Base(dir)
	}
	binaryPath := findBuiltBinary(dir, cliName)
	if binaryPath == "" {
		result.Checks = append(result.Checks, CheckResult{Name: "--help", Passed: false, Error: "built binary not found"})
		allPassed = false
		result.Checks = append(result.Checks, CheckResult{Name: "--version", Passed: false, Error: "built binary not found"})
		allPassed = false
	} else {
		helpCmd := exec.Command(binaryPath, "--help")
		helpCmd.Dir = dir
		helpOut, helpErr := helpCmd.CombinedOutput()
		if helpErr != nil {
			result.Checks = append(result.Checks, CheckResult{Name: "--help", Passed: false, Error: fmt.Sprintf("exit error: %v", helpErr)})
			allPassed = false
		} else {
			result.Checks = append(result.Checks, CheckResult{Name: "--help", Passed: true})
			result.HelpOutput = string(helpOut)
		}

		// 6. --version check
		versionCmd := exec.Command(binaryPath, "--version")
		versionCmd.Dir = dir
		if _, vErr := versionCmd.CombinedOutput(); vErr != nil {
			result.Checks = append(result.Checks, CheckResult{Name: "--version", Passed: false, Error: fmt.Sprintf("exit error: %v", vErr)})
			allPassed = false
		} else {
			result.Checks = append(result.Checks, CheckResult{Name: "--version", Passed: true})
		}
	}

	// 7. Manuscripts check (warn-only)
	apiName := result.APIName
	if apiName == "" {
		apiName = naming.TrimCLISuffix(cliName)
	}
	msDir := filepath.Join(pipeline.PublishedManuscriptsRoot(), apiName)
	if _, err := os.Stat(msDir); os.IsNotExist(err) {
		result.Checks = append(result.Checks, CheckResult{Name: "manuscripts", Passed: true, Warning: "no manuscripts found"})
	} else {
		runID, err := findMostRecentRun(msDir)
		if err != nil || runID == "" {
			result.Checks = append(result.Checks, CheckResult{Name: "manuscripts", Passed: true, Warning: "manuscripts directory exists but no runs found"})
		} else {
			result.Checks = append(result.Checks, CheckResult{Name: "manuscripts", Passed: true})
		}
	}

	result.Passed = allPassed
	return result
}

func runGoCheck(dir string, args ...string) CheckResult {
	name := "go " + args[0]
	cmd := exec.Command("go", args...)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		errMsg := strings.TrimSpace(string(output))
		if errMsg == "" {
			errMsg = err.Error()
		}
		return CheckResult{Name: name, Passed: false, Error: errMsg}
	}
	return CheckResult{Name: name, Passed: true}
}

func findBuiltBinary(dir, cliName string) string {
	// Look for the binary in common locations
	candidates := []string{
		filepath.Join(dir, cliName),
		filepath.Join(dir, "cmd", cliName, cliName),
	}

	// Also try go build output location
	buildCmd := exec.Command("go", "build", "-o", filepath.Join(dir, cliName), "./cmd/"+cliName)
	buildCmd.Dir = dir
	if err := buildCmd.Run(); err == nil {
		return filepath.Join(dir, cliName)
	}

	for _, c := range candidates {
		if info, err := os.Stat(c); err == nil && !info.IsDir() {
			return c
		}
	}
	return ""
}

func findMostRecentRun(msAPIDir string) (string, error) {
	entries, err := os.ReadDir(msAPIDir)
	if err != nil {
		return "", err
	}

	var runs []string
	for _, e := range entries {
		if e.IsDir() && !strings.HasPrefix(e.Name(), ".") {
			runs = append(runs, e.Name())
		}
	}

	if len(runs) == 0 {
		return "", nil
	}

	// Lexicographic sort (run-ids are timestamp-prefixed)
	sort.Strings(runs)
	return runs[len(runs)-1], nil
}
