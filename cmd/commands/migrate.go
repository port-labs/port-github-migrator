package commands

import (
	"fmt"

	"github.com/port-labs/port-github-migrator/internal/blueprints"
	"github.com/port-labs/port-github-migrator/internal/migrator"
	"github.com/port-labs/port-github-migrator/internal/models"
	"github.com/port-labs/port-github-migrator/internal/port"
	"github.com/spf13/cobra"
)

func NewMigrateCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:          "migrate [blueprint]",
		Short:        "Migrate Ownership of entities from a specific blueprint or all blueprints",
		Long:         `Migrate Ownership of entities from the old GitHub App integration to the new GitHub Ocean integration.`,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			portURL, _ := cmd.Flags().GetString("port-url")
			clientID, _ := cmd.Flags().GetString("client-id")
			clientSecret, _ := cmd.Flags().GetString("client-secret")
			oldInstallID, _ := cmd.Flags().GetString("old-installation-id")
			newInstallID, _ := cmd.Flags().GetString("new-installation-id")
			dryRun, _ := cmd.Flags().GetBool("dry-run")
			all, _ := cmd.Flags().GetBool("all")

			if len(args) == 0 && !all {
				return fmt.Errorf("❌ either provide a blueprint name or use --all flag. Usage: migrate <blueprint> or migrate --all")
			}
			if len(args) > 0 && all {
				return fmt.Errorf("❌ cannot use both blueprint argument and --all flag")
			}

			blueprint := ""
			if len(args) > 0 {
				blueprint = args[0]
			}

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

			client := port.NewClient(portURL, clientID, clientSecret)

			// Fetch the new integration. We need its version for the datasource id
			// and its config for the entityDeletionThreshold safety check.
			intg, err := client.GetIntegration(newInstallID)
			if err != nil {
				return fmt.Errorf("failed to get integration: %w", err)
			}
			if intg.Version == "" {
				return fmt.Errorf("integration version not found")
			}

			if err := requireZeroDeletionThreshold(intg); err != nil {
				return err
			}

			newDatasourceID := fmt.Sprintf("port-ocean/github-ocean/%s/%s/exporter", intg.Version, newInstallID)

			config := &models.Config{
				PortAPIURL:        portURL,
				ClientID:          clientID,
				ClientSecret:      clientSecret,
				OldInstallationID: oldInstallID,
				NewInstallationID: newInstallID,
			}

			mig := migrator.NewMigrator(client, config)

			if all {
				fmt.Fprintln(cmd.OutOrStdout(), "📋 Blueprints to migrate:")

				counts, err := blueprints.FetchCounts(client, oldInstallID, cmd.ErrOrStderr())
				if err != nil {
					return err
				}
				blueprints.PrintCounts(cmd.OutOrStdout(), counts, false, true)
				fmt.Fprintln(cmd.OutOrStdout())
			}

			var bp *string
			if !all && blueprint != "" {
				bp = &blueprint
			}

			_, err = mig.Migrate(newDatasourceID, bp, dryRun)
			return err
		},
	}

	cmd.Flags().Bool("dry-run", false, "Show what would be migrated without making changes")
	cmd.Flags().Bool("all", false, "Migrate all blueprints with entities")

	return cmd
}

// requireZeroDeletionThreshold enforces that the GitHub Ocean integration's
// mapping config has `entityDeletionThreshold` explicitly set to 0, so that
// the next resync after migration does not delete the freshly re-owned
// entities.
func requireZeroDeletionThreshold(intg *port.Integration) error {
	const key = "entityDeletionThreshold"
	const remediation = "Set `entityDeletionThreshold: 0` in the GitHub Ocean integration's mapping config and try again."

	raw, ok := intg.Config[key]
	if !ok {
		return fmt.Errorf("❌ %s is not set in the new GitHub Ocean integration's mapping config; without it, the next resync may delete migrated entities. %s", key, remediation)
	}
	n, ok := raw.(float64)
	if !ok {
		return fmt.Errorf("❌ %s in the new GitHub Ocean integration's mapping config is %v (expected 0). %s", key, raw, remediation)
	}
	if n != 0 {
		return fmt.Errorf("❌ %s in the new GitHub Ocean integration's mapping config is %v; it must be 0 to prevent migrated entities from being deleted on the next resync. %s", key, n, remediation)
	}
	return nil
}
