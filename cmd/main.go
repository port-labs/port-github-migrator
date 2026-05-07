package main

import (
	"os"

	"github.com/joho/godotenv"
	"github.com/port-labs/port-github-migrator/cmd/commands"
)

const Version = "v0.0.8"

func main() {
	// Load .env file
	_ = godotenv.Load()

	rootCmd := commands.NewRootCommand()
	rootCmd.Version = Version

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

