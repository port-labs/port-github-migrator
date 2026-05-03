package commands

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/port-labs/port-github-migrator/internal/diff"
	"github.com/port-labs/port-github-migrator/internal/port"
	"github.com/port-labs/port-github-migrator/internal/store"
	"github.com/spf13/cobra"
)

func NewGetDiffCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:          "get-diff <sourceBlueprint> <targetBlueprint>",
		Short:        "Compare entities between source and target blueprints",
		Long:         `Compare entities from the source blueprint (with old datasource) to the target blueprint (with new datasource).`,
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) < 2 {
				return fmt.Errorf("❌ both sourceBlueprint and targetBlueprint arguments are required. Usage: get-diff <sourceBlueprint> <targetBlueprint>")
			}
			return nil
		},
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			portURL, _ := cmd.Flags().GetString("port-url")
			clientID, _ := cmd.Flags().GetString("client-id")
			clientSecret, _ := cmd.Flags().GetString("client-secret")
			oldInstallID, _ := cmd.Flags().GetString("old-installation-id")
			newInstallID, _ := cmd.Flags().GetString("new-installation-id")
			showDiffs, _ := cmd.Flags().GetBool("show-diffs")
			limitStr, _ := cmd.Flags().GetString("limit")
			outputPath, _ := cmd.Flags().GetString("output")

			sourceBlueprint := args[0]
			targetBlueprint := args[1]

			// Validate required parameters
			var missing []string
			if clientID == "" {
				missing = append(missing, "--client-id")
			}
			if clientSecret == "" {
				missing = append(missing, "--client-secret")
			}
			if oldInstallID == "" {
				missing = append(missing, "--old-installation-id")
			}
			if newInstallID == "" {
				missing = append(missing, "--new-installation-id")
			}
			if len(missing) > 0 {
				return fmt.Errorf("❌ missing required options: %v", missing)
			}

			// Parse limit
			limit := 10
			if limitStr != "" {
				fmt.Sscanf(limitStr, "%d", &limit)
			}

			// Create Port client
			client := port.NewClient(portURL, clientID, clientSecret)

			// Create diff service
			diffService := diff.NewService(client)

			// Run comparison
			result, err := diffService.CompareBlueprints(sourceBlueprint, targetBlueprint, oldInstallID, newInstallID, cmd.ErrOrStderr())
			if err != nil {
				return fmt.Errorf("failed to compare blueprints: %w", err)
			}

			out, closeOut, err := openDiffOutput(outputPath)
			if err != nil {
				return fmt.Errorf("failed to open output file: %w", err)
			}
			defer closeOut()

			diffService.PrintSummary(out, result)

			if showDiffs && (len(result.Changed) > 0 || len(result.NotMigrated) > 0) {
				diffService.PrintDetailedDiffs(out, result, limit)
			}

			if outputPath != "" {
				fmt.Fprintf(cmd.OutOrStderr(), "📝 Wrote diff to %s\n", outputPath)
			}

			if st, err := store.Open(); err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "⚠️  Could not open local cache, identifiers not saved: %v\n", err)
			} else if _, err := st.SaveIdentifiers(oldInstallID, sourceBlueprint, result.SourceIdentifiers); err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "⚠️  Failed to save identifiers: %v\n", err)
			}

			return nil
		},
	}

	cmd.Flags().Bool("show-diffs", true, "Show detailed property differences")
	cmd.Flags().String("limit", "10", "Limit number of shown changes")
	cmd.Flags().StringP("output", "o", "", "Write the diff to this file instead of stdout")

	return cmd
}

func openDiffOutput(path string) (io.Writer, func(), error) {
	if path == "" {
		return os.Stdout, func() {}, nil
	}

	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, nil, err
		}
	}

	f, err := os.Create(path)
	if err != nil {
		return nil, nil, err
	}
	return f, func() { _ = f.Close() }, nil
}
