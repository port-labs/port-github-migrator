package commands

import (
	"fmt"

	"github.com/port-labs/port-github-migrator/internal/blueprints"
	"github.com/port-labs/port-github-migrator/internal/migrator"
	"github.com/port-labs/port-github-migrator/internal/models"
	"github.com/port-labs/port-github-migrator/internal/port"
	"github.com/port-labs/port-github-migrator/internal/store"
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

			// Validate blueprint or --all flag
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

			// Create Port client
			client := port.NewClient(portURL, clientID, clientSecret)

			// Get integration version
			version, err := client.GetIntegrationVersion(newInstallID)
			if err != nil {
				return fmt.Errorf("failed to get integration version: %w", err)
			}

			// Construct new datasource ID
			newDatasourceID := fmt.Sprintf("port-ocean/github-ocean/%s/%s/exporter", version, newInstallID)

			// Create config
			config := &models.Config{
				PortAPIURL:        portURL,
				ClientID:          clientID,
				ClientSecret:      clientSecret,
				OldInstallationID: oldInstallID,
				NewInstallationID: newInstallID,
			}

			// Open the local manifest store; if it can't be opened we still
			// migrate, just without the get-diff manifest contract.
			st, storeErr := store.Open()
			if storeErr != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "⚠️  Could not open local cache, manifests will be ignored: %v\n", storeErr)
			}

			mig := migrator.NewMigrator(client, config, st)

		// If migrating "all", show blueprints with entity counts first
		if all {
			fmt.Fprintln(cmd.OutOrStdout(), "📋 Blueprints to migrate:")

			counts, err := blueprints.FetchCounts(client, oldInstallID, cmd.ErrOrStderr())
			if err != nil {
				return err
			}
			blueprints.PrintCounts(cmd.OutOrStdout(), counts, false, true)
			fmt.Fprintln(cmd.OutOrStdout())
		}

		// Determine if migrating single blueprint or all
		var bp *string
		if !all && blueprint != "" {
			bp = &blueprint
		}

		// Run migration
		_, err = mig.Migrate(newDatasourceID, bp, dryRun)
		return err
		},
	}

	cmd.Flags().Bool("dry-run", false, "Show what would be migrated without making changes")
	cmd.Flags().Bool("all", false, "Migrate all blueprints with entities")

	return cmd
}
