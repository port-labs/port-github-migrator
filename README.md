# Port GitHub App to GitHub Ocean Migration Tool

A Go CLI that transfers **entity ownership** from Port's legacy GitHub App integration to GitHub Ocean.

Entities stay on the **same blueprints**. The tool only updates `$datasource` so Ocean can manage them. It does not copy or move entities into a new catalog location.

## Features

- Dry-run and diff before you transfer ownership
- Compare legacy vs Ocean entities (`get-diff`)
- Migrate one blueprint at a time, or all (with limits — see below)
- `--auto` for large blueprints (paginated; patches identical entities only)

## Installation

### Quick install (recommended)

```bash
curl -sL https://raw.githubusercontent.com/port-labs/port-github-migrator/main/install.sh | bash
```

The script detects your OS/architecture, downloads the release binary, verifies it, and installs to `/usr/local/bin/port-github-migrator`.

```bash
port-github-migrator --version
```

### Manual install

1. Download the binary for your platform from [GitHub Releases](https://github.com/port-labs/port-github-migrator/releases) (e.g. `port-github-migrator-macos-arm64`).
2. Make it executable and move it onto your `PATH`:

```bash
chmod +x port-github-migrator-macos-arm64
sudo mv port-github-migrator-macos-arm64 /usr/local/bin/port-github-migrator
```

## Full migration guide

This README covers the CLI. For the end-to-end customer checklist (temp blueprints, mapping, actions, uninstall), use the Port docs:

[Migration steps](https://docs.port.io/build-your-software-catalog/sync-data-to-catalog/git/github-ocean/migration/migration-steps)

## Configuration

### Environment variables

Create a `.env` file:

```env
PORT_API_URL=https://api.port.io
PORT_CLIENT_ID=your_client_id
PORT_CLIENT_SECRET=your_client_secret
OLD_INSTALLATION_ID=your_old_github_app_installation_id
NEW_INSTALLATION_ID=your_new_ocean_installation_id
```

Use `https://api.us.port.io` if your org is on the US region.

| Variable / flag | Type | Example |
|---|---|---|
| `OLD_INSTALLATION_ID` / `--old-installation-id` | **Numeric** GitHub App installation ID | `48448110` |
| `NEW_INSTALLATION_ID` / `--new-installation-id` | **String** Port Ocean integration id | `github-ocean-v2` |

These are different kinds of values. Passing a numeric GitHub install id as `--new-installation-id` makes every entity look "not migrated".

### CLI flags (same values)

```bash
port-github-migrator migrate repository \
  --client-id your_id \
  --client-secret your_secret \
  --old-installation-id 48448110 \
  --new-installation-id github-ocean-v2 \
  --port-url https://api.port.io
```

## Commands

```
USAGE:
  port-github-migrator [flags] [command]

GLOBAL FLAGS:
  --port-url string                Port API URL (default: https://api.port.io)
  --client-id string               Port API client ID
  --client-secret string           Port API client secret
  --old-installation-id string     Numeric GitHub App installation ID
  --new-installation-id string     String Port Ocean integration identifier
  --verbose                        Enable verbose logging
  -h, --help                       Show help

COMMANDS:
  migrate         Transfer ownership for one blueprint, all blueprints, or --auto
  get-blueprints  List blueprints with entities under the old installation
  get-diff        Compare entities between source and target blueprints
```

## Source vs target (important)

For `get-diff` and `migrate ... --auto` you pass **two** blueprint identifiers:

- **Source** — your real blueprint (e.g. `repository`). Ownership is transferred **here**.
- **Target** — a **temporary** Ocean blueprint (e.g. `repository-ocean-temp`) used only for comparison / identical checks.

Typical flow per blueprint (details in the [migration guide](https://docs.port.io/build-your-software-catalog/sync-data-to-catalog/git/github-ocean/migration/migration-steps)):

1. Duplicate the blueprint → `*-ocean-temp`.
2. Point Ocean mapping at the temp blueprint, with `entityDeletionThreshold: 0` **and** at least one `resources` block (an empty mapping cannot save the threshold alone).
3. Resync Ocean into the temp blueprint.
4. `get-diff` / `migrate` (or `--auto`).
5. Point Ocean mapping back at the original blueprint; delete the temp.

There is no supported "re-own every blueprint in place with one command" path that also does safe diffs. Work **one blueprint at a time**.

## Usage examples

### List blueprints

```bash
port-github-migrator get-blueprints
```

### Compare entities

```bash
port-github-migrator get-diff repository repository-ocean-temp \
  --show-diffs \
  --limit 10
```

`get-diff` compares up to **5000** source entities per run.

### Migrate (interactive)

Transfers ownership for matching legacy entities on that blueprint (up to **5000 per run**). Re-run to continue if the blueprint is larger. Asks you to type `yes` before patching.

```bash
port-github-migrator migrate repository --dry-run
port-github-migrator migrate repository
```

```bash
# All blueprints — still 5000 entities per blueprint per run; no content diff
port-github-migrator migrate --all
```

Prefer `--auto` for large blueprints instead of relying on many `--all` / single-blueprint re-runs without a temp target.

### Migrate with `--auto` (large blueprints)

Paginates past the 5000 cap, diffs each batch against the **target** (temp) blueprint, patches **identical** entities on the **source**, and writes leftovers (`changed` / `notMigrated`) to a result file under your user cache directory.

```bash
port-github-migrator migrate repository repository-ocean-temp --auto \
  --client-id your_id \
  --client-secret your_secret \
  --old-installation-id 48448110 \
  --new-installation-id github-ocean-v2 \
  --port-url https://api.port.io
```

Optional: ignore noisy dynamic properties during the diff:

```bash
port-github-migrator migrate repository repository-ocean-temp --auto \
  --ignore-property updatedAt \
  --ignore-property syncStatus
```

### Flag combinations that do not work

| Combination | Result |
|---|---|
| `migrate --all --auto` | **Rejected** — `--auto` requires a single `<source> <target>` pair |
| `migrate --auto` with one blueprint | **Rejected** — need both source and target |
| `entityDeletionThreshold: 0` on an empty Ocean mapping | **Save fails** — add threshold together with `resources` pointing at the temp blueprint |

`migrate` also refuses to run unless the Ocean integration mapping has `entityDeletionThreshold: 0` (protects entities while Ocean still maps to temps).

## Safety notes

- Transfer ownership **before** uninstalling the legacy GitHub App.
- Self-service actions using the legacy `GITHUB` backend must be rewritten to `INTEGRATION_ACTION` with your Ocean `installationId` — this tool does not do that.
- Start with a small blueprint, then move to large ones (`pull_request`, `branch`, etc.).

See the [migration steps](https://docs.port.io/build-your-software-catalog/sync-data-to-catalog/git/github-ocean/migration/migration-steps) guide for the full checklist.
