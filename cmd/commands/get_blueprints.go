package commands

import (
	"fmt"
	"sort"

	blueprintcounts "github.com/port-labs/port-github-migrator/internal/blueprints"
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
			if len(missing) > 0 {
				return fmt.Errorf("❌ missing required options: %v", missing)
			}

			st, err := openStore()
			if err != nil {
				return err
			}
			defer st.Close()

			client := port.NewClient(portURL, clientID, clientSecret, st)

			// Get blueprints
			blueprints, err := client.GetBlueprintsByDataSource(oldInstallID)
			if err != nil {
				return fmt.Errorf("failed to get blueprints: %w", err)
			}

			// Sort and display with entity counts
			sort.Strings(blueprints)

			fmt.Println("NAME                              ENTITIES")
			fmt.Println("──────────────────────────────────────────")
			counts, _ := blueprintcounts.CountOldEntities(client, blueprints, oldInstallID)
			for _, bp := range blueprints {
				count, ok := counts[bp]
				if !ok {
					// If we can't get count, just show the blueprint name
					fmt.Printf("%-33s ?\n", bp)
					continue
				}

				// Skip empty blueprints unless --include-empty is set
				if count == 0 && !includeEmpty {
					continue
				}

				fmt.Printf("%-33s %d\n", bp, count)
			}

			return nil
		},
	}

	cmd.Flags().Bool("include-empty", false, "Include blueprints with 0 entities")

	return cmd
}
