package commands

import (
	"os"

	"github.com/spf13/cobra"
)

func NewRootCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:          "port-github-migrator",
		Short:        "Migrate Ownership of Port entities from GitHub App to GitHub Ocean",
		Long:         `A tool to safely migrate Ownership of Port entities from the legacy GitHub App integration to the new GitHub Ocean integration.`,
		SilenceUsage: true,
	}

	// Hide the auto-generated completion and help commands
	cmd.CompletionOptions.HiddenDefaultCmd = true

	cmd.PersistentFlags().String("port-url", getEnv("PORT_API_URL", "https://api.getport.io"), "Port API URL")
	cmd.PersistentFlags().String("client-id", getEnv("PORT_CLIENT_ID", ""), "Port API Client ID")
	cmd.PersistentFlags().String("client-secret", getEnv("PORT_CLIENT_SECRET", ""), "Port API Client Secret")
	cmd.PersistentFlags().String("old-installation-id", getEnv("OLD_INSTALLATION_ID", ""), "Old GitHub App Installation ID")
	cmd.PersistentFlags().String("new-installation-id", getEnv("NEW_INSTALLATION_ID", ""), "New GitHub Ocean Installation ID")
	cmd.PersistentFlags().Bool("verbose", false, "Enable verbose logging")

	cmd.AddCommand(
		NewMigrateCommand(),
		NewGetBlueprintsCommand(),
		NewGetDiffCommand(),
	)

	return cmd
}

func getEnv(key, defaultVal string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	return defaultVal
}

