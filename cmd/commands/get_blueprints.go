package commands

import (
	"fmt"

	"github.com/port-labs/port-github-migrator/internal/blueprints"
	"github.com/port-labs/port-github-migrator/internal/port"
	"github.com/spf13/cobra"
)

func NewGetBlueprintsCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:          "get-blueprints",
		Short:        "Get all blueprints that the old installation ingested entities into",
		Long:         "List all blueprints that the old GitHub App installation ingested entities into.",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			portURL, _ := cmd.Flags().GetString("port-url")
			clientID, _ := cmd.Flags().GetString("client-id")
			clientSecret, _ := cmd.Flags().GetString("client-secret")
			oldInstallID, _ := cmd.Flags().GetString("old-installation-id")
			includeEmpty, _ := cmd.Flags().GetBool("include-empty")

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
			if len(missing) > 0 {
				return fmt.Errorf("❌ missing required options: %v", missing)
			}

			client := port.NewClient(portURL, clientID, clientSecret)

			counts, err := blueprints.FetchCounts(client, oldInstallID, cmd.ErrOrStderr())
			if err != nil {
				return err
			}

			blueprints.PrintCounts(cmd.OutOrStdout(), counts, includeEmpty, false)

			return nil
		},
	}

	cmd.Flags().Bool("include-empty", false, "Include blueprints with 0 entities")

	return cmd
}
