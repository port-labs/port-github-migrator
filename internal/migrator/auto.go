package migrator

import (
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/briandowns/spinner"
	"github.com/port-labs/port-github-migrator/internal/diff"
	"github.com/port-labs/port-github-migrator/internal/models"
	"github.com/port-labs/port-github-migrator/internal/port"
	"github.com/port-labs/port-github-migrator/internal/store"
)

// autoBatchSize is how many old-installation entities one auto-mode batch
// processes end-to-end (fetch source -> fetch target -> diff -> patch).
// Matches port.MaxSearchResults so each batch mirrors what get-diff/migrate
// would do today.
const autoBatchSize = port.MaxSearchResults

// autoPatchChunkSize is the number of identifiers per PatchEntitiesDatasourceBulk
// call. Mirrors migrateBlueprint's chunking.
const autoPatchChunkSize = 20

// MigrateAuto runs the unattended auto migration for one source -> target
// blueprint pair: it walks the source blueprint under the old installation in
// autoBatchSize batches, diffs each batch against the target blueprint under
// the new installation, patches identical source-blueprint entities to the
// new datasource, and persists the remaining changed/missing identifiers
// into a single result file under the cache directory. The path of that
// file is returned on success.
//
// Auto mode requires a working store (we always write a result file). The
// spinner is rendered to spinnerOut; pass io.Discard to disable it.
func (m *Migrator) MigrateAuto(sourceBlueprintID, targetBlueprintID, newDatasourceID string, dryRun bool, spinnerOut io.Writer) (string, error) {
	if m.store == nil {
		return "", errors.New("auto mode requires a writable cache directory; could not open one")
	}

	sp := spinner.New(spinner.CharSets[14], 100*time.Millisecond)
	sp.Writer = spinnerOut
	sp.HideCursor = true

	var (
		stateMu      sync.Mutex
		currentBatch int
		phase        string
	)
	setSuffix := func(p string) {
		stateMu.Lock()
		phase = p
		sp.Lock()
		if currentBatch == 0 {
			sp.Suffix = fmt.Sprintf(" %s", phase)
		} else {
			sp.Suffix = fmt.Sprintf(" Batch %d: %s", currentBatch, phase)
		}
		sp.Unlock()
		stateMu.Unlock()
	}
	setBatch := func(i int) {
		stateMu.Lock()
		currentBatch = i
		stateMu.Unlock()
	}

	setSuffix(fmt.Sprintf("counting %s entities (old install)...", sourceBlueprintID))
	sp.Start()
	defer sp.Stop()

	totalAvailable, err := m.client.CountOldEntitiesByBlueprint(sourceBlueprintID, m.config.OldInstallationID)
	if err != nil {
		return "", fmt.Errorf("failed to count source entities: %w", err)
	}

	result := store.AutoResult{
		SourceBlueprint: sourceBlueprintID,
		TargetBlueprint: targetBlueprintID,
		GeneratedAt:     time.Now().UTC(),
		Changed:         []models.EntityChange{},
		NotMigrated:     []string{},
	}
	var totalIdentical, totalProcessed int

	if totalAvailable == 0 {
		setSuffix("no entities to migrate; writing empty result file...")
		return m.store.SaveAutoResult(m.config.OldInstallationID, result)
	}

	processBatch := func(batch []port.Entity, batchIndex int) error {
		setBatch(batchIndex + 1)
		setSuffix(fmt.Sprintf("fetching target entities from %s (%d ids)...", targetBlueprintID, len(batch)))

		ids := make([]string, len(batch))
		for i, e := range batch {
			ids[i] = e.Identifier
		}

		targetEntities, err := m.client.SearchNewEntitiesByBlueprint(
			targetBlueprintID,
			m.config.NewInstallationID,
			ids,
			&port.SearchOptions{
				IncludeTitle:      true,
				IncludeProperties: true,
				IncludeRelations:  true,
				EnforceTotalLimit: false,
			},
		)
		if err != nil {
			return fmt.Errorf("failed to fetch target entities: %w", err)
		}

		setSuffix(fmt.Sprintf("diffing %d source vs %d target entities...", len(batch), len(targetEntities)))

		identical, changed, notMigrated := diff.DiffEntities(batch, targetEntities)
		totalProcessed += len(batch)

		result.Changed = append(result.Changed, changed...)
		result.NotMigrated = append(result.NotMigrated, notMigrated...)
		result.Summary.Changed += len(changed)
		result.Summary.NotMigrated += len(notMigrated)

		if len(identical) == 0 {
			setSuffix(fmt.Sprintf("no identicals to patch (processed %d/%d)", totalProcessed, totalAvailable))
			return nil
		}

		if dryRun {
			totalIdentical += len(identical)
			result.Summary.Identical += len(identical)
			setSuffix(fmt.Sprintf("dry-run: would patch %d identicals (processed %d/%d)", len(identical), totalProcessed, totalAvailable))
			return nil
		}

		patched := 0
		for i := 0; i < len(identical); i += autoPatchChunkSize {
			end := min(i+autoPatchChunkSize, len(identical))
			chunk := identical[i:end]

			setSuffix(fmt.Sprintf("patching identicals %d/%d (processed %d/%d)", patched+len(chunk), len(identical), totalProcessed, totalAvailable))

			if err := m.client.PatchEntitiesDatasourceBulk(sourceBlueprintID, chunk, newDatasourceID); err != nil {
				return fmt.Errorf("failed to patch batch: %w", err)
			}
			patched += len(chunk)
		}

		totalIdentical += patched
		result.Summary.Identical += patched
		return nil
	}

	setSuffix(fmt.Sprintf("fetching source entities from %s (0/%d)...", sourceBlueprintID, totalAvailable))
	if err := m.client.SearchOldEntitiesPaged(
		sourceBlueprintID,
		m.config.OldInstallationID,
		autoBatchSize,
		nil,
		processBatch,
	); err != nil {
		return "", err
	}

	setBatch(0)
	setSuffix("writing result file...")

	path, err := m.store.SaveAutoResult(m.config.OldInstallationID, result)
	if err != nil {
		return "", fmt.Errorf("failed to write result file: %w", err)
	}

	sp.Stop()

	fmt.Println()
	if dryRun {
		fmt.Printf("🔄 DRY RUN: would have patched %d identical entities (%s -> %s)\n", totalIdentical, sourceBlueprintID, targetBlueprintID)
	} else {
		fmt.Printf("✅ Patched %d identical entities (%s -> %s)\n", totalIdentical, sourceBlueprintID, targetBlueprintID)
	}
	fmt.Printf("📊 Summary: %d identical, %d changed, %d not migrated (%d processed / %d available)\n",
		result.Summary.Identical, result.Summary.Changed, result.Summary.NotMigrated, totalProcessed, totalAvailable)

	return path, nil
}
