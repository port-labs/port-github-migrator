package migrator

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/port-labs/port-github-migrator/internal/models"
	"github.com/port-labs/port-github-migrator/internal/port"
	"github.com/port-labs/port-github-migrator/internal/store"
)

// Migrator orchestrates the migration process
type Migrator struct {
	client *port.Client
	config *models.Config
	store  *store.Store // optional; nil disables manifest-based migration
}

// NewMigrator creates a new migrator. If `st` is non-nil, blueprint manifests
// produced by `get-diff` are honored (the migrator patches exactly the
// captured identifiers and removes the manifest on success).
func NewMigrator(client *port.Client, config *models.Config, st *store.Store) *Migrator {
	return &Migrator{
		client: client,
		config: config,
		store:  st,
	}
}

// Migrate orchestrates the migration process
func (m *Migrator) Migrate(newDatasourceID string, blueprintID *string, dryRun bool) (*models.MigrationStats, error) {
	stats := &models.MigrationStats{}

	// Get blueprints to migrate
	var blueprints []string
	if blueprintID != nil {
		blueprints = []string{*blueprintID}
	} else {
		bps, err := m.client.GetBlueprintsByDataSource(m.config.OldInstallationID)
		if err != nil {
			return nil, fmt.Errorf("failed to get blueprints: %w", err)
		}
		blueprints = bps
	}

	stats.TotalBlueprints = len(blueprints)

	// Show warning and get confirmation
	fmt.Println()
	fmt.Println("⚠️  WARNING: This action cannot be undone!")
	fmt.Println("    Please verify your data with 'get-diff' and 'dry-run' before proceeding.")
	fmt.Printf("    Only the first %d entities per blueprint will be migrated.\n", port.MaxSearchResults)
	fmt.Println()

	totalEntities := 0
	totalAvailable := 0
	blueprintCounts := make(map[string]int)

	// Count entities for each blueprint via the cheap aggregate endpoint.
	for _, bp := range blueprints {
		count, err := m.client.CountOldEntitiesByBlueprint(bp, m.config.OldInstallationID)
		if err != nil {
			return nil, fmt.Errorf("failed to count entities for blueprint %s: %w", bp, err)
		}
		blueprintCounts[bp] = count
		totalAvailable += count
		if count > port.MaxSearchResults {
			totalEntities += port.MaxSearchResults
		} else {
			totalEntities += count
		}
	}

	if totalEntities < totalAvailable {
		fmt.Printf("📊 Total entities affected: %d / %d (capped at %d per blueprint)\n", totalEntities, totalAvailable, port.MaxSearchResults)
	} else {
		fmt.Printf("📊 Total entities affected: %d\n", totalEntities)
	}

	if totalEntities == 0 {
		fmt.Println("⚠️  No entities found to migrate. Exiting.")
		return stats, nil
	}

	if dryRun {
		fmt.Println("🔄 DRY RUN MODE - No changes will be made")
	}

	// Get user confirmation
	fmt.Print("\nType 'yes' to proceed: ")
	reader := bufio.NewReader(os.Stdin)
	input, _ := reader.ReadString('\n')
	input = strings.TrimSpace(input)

	if input != "yes" {
		fmt.Println("❌ Migration cancelled.")
		return stats, nil
	}

	// Migrate each blueprint
	for _, bp := range blueprints {
		total := blueprintCounts[bp]

		if total == 0 {
			fmt.Println("⏭️  No entities to migrate")
			continue
		}

		if total > port.MaxSearchResults {
			fmt.Printf("\n🔄 Migrating %d / %d entities from blueprint: %s\n", port.MaxSearchResults, total, bp)
		} else {
			fmt.Printf("\n🔄 Migrating %d entities from blueprint: %s\n", total, bp)
		}

		if !dryRun {
			if err := m.migrateBlueprint(bp, newDatasourceID); err != nil {
				stats.FailedBatches++
				stats.Errors = append(stats.Errors, fmt.Sprintf("Failed to migrate blueprint %s: %v", bp, err))
				fmt.Printf("❌ Error migrating blueprint %s: %v\n", bp, err)
				continue
			}
		}

		stats.SuccessfulBatches++
	}

	fmt.Println()
	if stats.FailedBatches > 0 {
		fmt.Printf("⚠️  Migration completed with errors. Successfully migrated %d blueprints, %d failed\n", stats.SuccessfulBatches, stats.FailedBatches)
	} else {
		fmt.Printf("✅ Migration complete! Successfully migrated %d blueprints\n", stats.SuccessfulBatches)
	}

	return stats, nil
}

// migrateBlueprint migrates a single blueprint. The set of identifiers to
// patch comes either from a previously persisted manifest (e.g. produced by
// `get-diff`) or from a live search; in both cases it is persisted to the
// cache before patching so users can audit the exact list, and the manifest
// is removed on full success.
func (m *Migrator) migrateBlueprint(blueprintID, newDatasourceID string) error {
	identifiers, manifestPath, err := m.resolveIdentifiers(blueprintID)
	if err != nil {
		return err
	}

	if len(identifiers) == 0 {
		fmt.Println("⏭️  No entities to migrate")
		return nil
	}

	printIdentifierPreview(identifiers, manifestPath)

	// Patch in batches of 20.
	batchSize := 20
	for i := 0; i < len(identifiers); i += batchSize {
		end := i + batchSize
		if end > len(identifiers) {
			end = len(identifiers)
		}

		batch := identifiers[i:end]
		if err := m.client.PatchEntitiesDatasourceBulk(blueprintID, batch, newDatasourceID); err != nil {
			return fmt.Errorf("failed to patch batch: %w", err)
		}

		fmt.Printf("✅ Successfully patched %d entities\n", len(batch))
	}

	if m.store != nil {
		_ = m.store.DeleteIdentifiers(m.config.OldInstallationID, blueprintID)
	}

	return nil
}

// resolveIdentifiers returns the entity identifiers to migrate for a
// blueprint, plus the path of the file backing them (empty if the store was
// unavailable). It loads an existing identifier cache when present, otherwise
// it runs a live search and persists the result so the snapshot is stable
// across retries.
func (m *Migrator) resolveIdentifiers(blueprintID string) ([]string, string, error) {
	if m.store != nil {
		if cached, err := m.store.LoadIdentifiers(m.config.OldInstallationID, blueprintID); err != nil {
			fmt.Printf("⚠️  Could not read identifiers cache for %s: %v (falling back to live search)\n", blueprintID, err)
		} else if cached != nil {
			return cached, m.store.ManifestPath(m.config.OldInstallationID, blueprintID), nil
		}
	}

	entities, err := m.client.SearchOldEntitiesByBlueprint(blueprintID, m.config.OldInstallationID, nil)
	if err != nil {
		return nil, "", fmt.Errorf("failed to search entities: %w", err)
	}

	identifiers := make([]string, len(entities))
	for i, entity := range entities {
		identifiers[i] = entity.Identifier
	}

	var path string
	if m.store != nil {
		p, err := m.store.SaveIdentifiers(m.config.OldInstallationID, blueprintID, identifiers)
		if err != nil {
			fmt.Printf("⚠️  Could not save identifiers cache for %s: %v\n", blueprintID, err)
		} else {
			path = p
		}
	}

	return identifiers, path, nil
}

// printIdentifierPreview shows the first few identifiers and points the user
// at the manifest file (if any) for the full list, so we never spam stdout
// with thousands of identifiers.
func printIdentifierPreview(identifiers []string, manifestPath string) {
	const previewN = 5
	preview := identifiers
	if len(preview) > previewN {
		preview = preview[:previewN]
	}
	fmt.Printf("📋 First %d of %d identifiers: %s\n", len(preview), len(identifiers), strings.Join(preview, ", "))
	if manifestPath != "" {
		fmt.Printf("   Full list: %s\n", manifestPath)
	} else if len(identifiers) > previewN {
		fmt.Printf("   ... and %d more\n", len(identifiers)-previewN)
	}
}

