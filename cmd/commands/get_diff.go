package commands

import (
	"fmt"

	"github.com/port-labs/port-github-migrator/internal/diff"
	"github.com/port-labs/port-github-migrator/internal/port"
	"github.com/spf13/cobra"
)

func NewGetDiffCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get-diff <sourceBlueprint> <targetBlueprint>",
		Short: "Compare entities between source and target blueprints",
		Long:  `Compare entities from the source blueprint (with old datasource) to the target blueprint (with new datasource).`,
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

			sourceBlueprint := args[0]
			targetBlueprint := args[1]

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

			limit := 10
			if limitStr != "" {
				fmt.Sscanf(limitStr, "%d", &limit)
			}

			st, err := openStore()
			if err != nil {
				return err
			}
			defer st.Close()

			client := port.NewClient(portURL, clientID, clientSecret, st)
			diffService := diff.NewService(client)

			result, err := diffService.CompareBlueprints(sourceBlueprint, targetBlueprint, oldInstallID, newInstallID)
			if err != nil {
				return fmt.Errorf("failed to compare blueprints: %w", err)
			}

			diffService.PrintSummary(result)

			if showDiffs && len(result.Changes) > 0 {
				diffService.PrintDetailedDiffs(result.Changes, limit)
			}

			return nil
		},
	}

	cmd.Flags().Bool("show-diffs", true, "Show detailed property differences")
	cmd.Flags().String("limit", "10", "Limit number of shown changes")

	return cmd
}
