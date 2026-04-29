package migrator

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/port-labs/port-github-migrator/internal/models"
	"github.com/port-labs/port-github-migrator/internal/port"
)

// Migrator orchestrates the migration process
type Migrator struct {
	client *port.Client
	config *models.Config
}

// NewMigrator creates a new migrator
func NewMigrator(client *port.Client, config *models.Config) *Migrator {
	return &Migrator{
		client: client,
		config: config,
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
	fmt.Println()

	totalEntities := 0
	blueprintCounts := make(map[string]int)

	// Count entities for each blueprint
	for _, bp := range blueprints {
		entities, err := m.client.SearchOldEntitiesByBlueprint(bp, m.config.OldInstallationID, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to search entities for blueprint %s: %w", bp, err)
		}
		count := len(entities)
		blueprintCounts[bp] = count
		totalEntities += count
	}

	fmt.Printf("📊 Total entities affected: %d\n", totalEntities)

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
		count := blueprintCounts[bp]
		
		// Skip blueprints with no entities
		if count == 0 {
			fmt.Printf("\n🔄 Migrating %d entities from blueprint: %s\n", count, bp)
			fmt.Println("⏭️  No entities to migrate")
			continue
		}
		
		fmt.Printf("\n🔄 Migrating %d entities from blueprint: %s\n", count, bp)

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

// migrateBlueprint migrates a single blueprint
func (m *Migrator) migrateBlueprint(blueprintID, newDatasourceID string) error {
	// Get old entities
	entities, err := m.client.SearchOldEntitiesByBlueprint(blueprintID, m.config.OldInstallationID, nil)
	if err != nil {
		return fmt.Errorf("failed to search entities: %w", err)
	}

	if len(entities) == 0 {
		fmt.Println("⏭️  No entities to migrate")
		return nil
	}

	// Extract identifiers
	identifiers := make([]string, len(entities))
	for i, entity := range entities {
		identifiers[i] = entity.Identifier
	}

	// Patch in batches of 20
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

	return nil
}

