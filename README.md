# Port GitHub App to GitHub Ocean Migration Tool

A fast, single-file CLI tool written in Go to safely migrate Port entities from the legacy GitHub App integration to the new GitHub Ocean integration.

## Features

- 🔒 **Safe migration** - Dry-run mode and diffing before migration
- 📊 **Entity comparison** - See differences between old and new datasources
- 🎯 **Blueprint-by-blueprint** - Migrate one blueprint at a time or all at once

## Installation

### Quick Install (Recommended)

One-line installation that automatically downloads the correct binary for your platform:

```bash
curl -sL https://raw.githubusercontent.com/omby8888/port-github-migrator/main/install.sh | bash
```

The script will:
- Detect your OS and architecture (macOS, Linux, Windows)
- Download the appropriate binary from GitHub Releases
- Verify the binary works
- Install to `/usr/local/bin/port-github-migrator`

### Verify Installation

```bash
port-github-migrator --version
```

### Manual Installation

If you prefer manual installation:

1. Go to [GitHub Releases](https://github.com/omby8888/port-github-migrator/releases)
2. Download the binary for your platform (e.g., `port-github-migrator-macos-arm64`)
3. Make it executable and move to your PATH:
   ```bash
   chmod +x port-github-migrator-macos-arm64
   sudo mv port-github-migrator-macos-arm64 /usr/local/bin/port-github-migrator
   ```

## Configuration

### Environment Variables

Create a `.env` file with your Port API credentials:

```env
PORT_API_URL=https://api.getport.io
PORT_CLIENT_ID=your_client_id
PORT_CLIENT_SECRET=your_client_secret
OLD_INSTALLATION_ID=your_old_github_app_installation_id
NEW_INSTALLATION_ID=your_new_ocean_installation_id
```

Alternatively, pass these as CLI flags:

```bash
port-github-migrator migrate githubRepository \
  --client-id your_id \
  --client-secret your_secret \
  --old-installation-id 97280772 \
  --new-installation-id 12345678
```

## Commands Reference

```
USAGE:
  port-github-migrator [flags] [command]

GLOBAL FLAGS:
  --port-url string                Port API URL (default: https://api.getport.io)
  --client-id string              Port API Client ID
  --client-secret string          Port API Client Secret
  --old-installation-id string    Old GitHub App Installation ID
  --new-installation-id string    New GitHub Ocean Installation ID
  --verbose                       Enable verbose logging
  -h, --help                      Show this help message

COMMANDS:
  migrate       Migrate entities from a specific blueprint or all blueprints
  get-blueprints Get all blueprints managed by the old installation
  get-diff      Compare entities between source and target blueprints
```

## Usage

Please refer to the migration guide documentation: https://docs.port.io/build-your-software-catalog/sync-data-to-catalog/git/github-ocean/migration-guide

### Get Blueprints

List all blueprints managed by the old GitHub App installation:

```bash
port-github-migrator get-blueprints
```

### Compare Entities (Diff)

Compare entities between the old and new installations:

```bash
port-github-migrator get-diff githubRepository githubRepository-new \
  --show-diffs \
  --limit 10
```

### Migrate Entities

Migrate entities from old to new installation:

```bash
# Migrate single blueprint
port-github-migrator migrate githubRepository

# Migrate all blueprints
port-github-migrator migrate all

# Dry-run (see what would be migrated)
port-github-migrator migrate githubRepository --dry-run
```
