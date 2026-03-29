package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/mvanhorn/cli-printing-press/internal/pipeline"
	"github.com/spf13/cobra"
)

func newScorecardCmd() *cobra.Command {
	var dir string
	var specPath string
	var asJSON bool

	cmd := &cobra.Command{
		Use:   "scorecard",
		Short: "Score a generated CLI against the Steinberger bar",
		Example: `  # Score a generated CLI directory
  printing-press scorecard --dir ./generated/stripe-pp-cli

  # Output as JSON
  printing-press scorecard --dir ./generated/stripe-pp-cli --json`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if dir == "" {
				return &ExitError{Code: ExitInputError, Err: fmt.Errorf("--dir is required")}
			}

			// Use a temp pipeline dir for the scorecard output
			pipelineDir, err := os.MkdirTemp("", "scorecard-*")
			if err != nil {
				return fmt.Errorf("creating temp dir: %w", err)
			}
			defer func() { _ = os.RemoveAll(pipelineDir) }()

			sc, err := pipeline.RunScorecard(dir, pipelineDir, specPath, nil)
			if err != nil {
				return &ExitError{Code: ExitGenerationError, Err: fmt.Errorf("running scorecard: %w", err)}
			}

			if asJSON {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(sc)
			}

			// Human-readable output
			s := sc.Steinberger
			renderScore := func(name string, score, max int) string {
				if sc.IsDimensionUnscored(name) {
					return "N/A"
				}
				return fmt.Sprintf("%d/%d", score, max)
			}

			fmt.Printf("Quality Scorecard: %s\n\n", sc.APIName)
			fmt.Printf("  Output Modes   %d/10\n", s.OutputModes)
			fmt.Printf("  Auth           %d/10\n", s.Auth)
			fmt.Printf("  Error Handling %d/10\n", s.ErrorHandling)
			fmt.Printf("  Terminal UX    %d/10\n", s.TerminalUX)
			fmt.Printf("  README         %d/10\n", s.README)
			fmt.Printf("  Doctor         %d/10\n", s.Doctor)
			fmt.Printf("  Agent Native   %d/10\n", s.AgentNative)
			fmt.Printf("  Local Cache    %d/10\n", s.LocalCache)
			fmt.Printf("  Breadth        %d/10\n", s.Breadth)
			fmt.Printf("  Vision         %d/10\n", s.Vision)
			fmt.Printf("  Workflows      %d/10\n", s.Workflows)
			fmt.Printf("  Insight        %d/10\n", s.Insight)
			fmt.Printf("\n  Domain Correctness\n")
			fmt.Printf("  Path Validity          %s\n", renderScore("path_validity", s.PathValidity, 10))
			fmt.Printf("  Auth Protocol          %s\n", renderScore("auth_protocol", s.AuthProtocol, 10))
			fmt.Printf("  Data Pipeline Integrity %d/10\n", s.DataPipelineIntegrity)
			fmt.Printf("  Sync Correctness       %d/10\n", s.SyncCorrectness)
			fmt.Printf("  Type Fidelity          %d/5\n", s.TypeFidelity)
			fmt.Printf("  Dead Code              %d/5\n", s.DeadCode)
			fmt.Printf("\n  Total: %d/100 - Grade %s\n", s.Total, sc.OverallGrade)
			if len(sc.UnscoredDimensions) > 0 {
				fmt.Printf("  Note: omitted from denominator: %s\n", strings.Join(sc.UnscoredDimensions, ", "))
			}

			if len(sc.GapReport) > 0 {
				fmt.Printf("\nGaps:\n")
				for _, g := range sc.GapReport {
					fmt.Printf("  - %s\n", g)
				}
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&dir, "dir", "", "Path to generated CLI directory")
	cmd.Flags().StringVar(&specPath, "spec", "", "Path to OpenAPI spec JSON for semantic validation")
	cmd.Flags().BoolVar(&asJSON, "json", false, "Output as JSON")

	return cmd
}
