package diff

import (
	"encoding/json"
	"fmt"
	"reflect"
	"sync"

	"github.com/port-labs/port-github-migrator/internal/models"
	"github.com/port-labs/port-github-migrator/internal/port"
	"github.com/port-labs/port-github-migrator/internal/store"
)

// Service handles entity comparison
type Service struct {
	client *port.Client
	store  *store.Store
}

// NewService creates a new diff service
func NewService(client *port.Client) *Service {
	return &Service{
		client: client,
		store:  client.Store(),
	}
}

// excludedDiffProps lists property keys we ignore when computing per-property diffs.
// These are server-managed fields that may legitimately differ between integrations
// without representing a meaningful content change.
var excludedDiffProps = map[string]bool{
	"blueprint": true,
	"createdAt": true,
	"updatedAt": true,
	"createdBy": true,
	"updatedBy": true,
}

// CompareBlueprints compares entities between source and target blueprints.
// The fetch-and-cache happens via the Port client; the actual diff is computed
// against the SQLite store using JOINs on identifier and hash equality.
func (s *Service) CompareBlueprints(sourceBP, targetBP, oldInstallID, newInstallID string) (*models.DiffResult, error) {
	var (
		sourceErr error
		targetErr error
		wg        sync.WaitGroup
	)

	wg.Add(2)
	go func() {
		defer wg.Done()
		_, sourceErr = s.client.SearchOldEntitiesByBlueprint(sourceBP, oldInstallID)
	}()
	go func() {
		defer wg.Done()
		_, targetErr = s.client.SearchNewEntitiesByBlueprint(targetBP, newInstallID)
	}()
	wg.Wait()

	if sourceErr != nil {
		return nil, fmt.Errorf("failed to get source entities: %w", sourceErr)
	}
	if targetErr != nil {
		return nil, fmt.Errorf("failed to get target entities: %w", targetErr)
	}

	sets, err := s.store.DiffBlueprints(sourceBP, oldInstallID, targetBP, newInstallID)
	if err != nil {
		return nil, fmt.Errorf("compute diff sets: %w", err)
	}

	result := &models.DiffResult{
		SourceBlueprint: sourceBP,
		TargetBlueprint: targetBP,
		Changes:         []models.EntityChange{},
	}

	result.Summary.Identical = sets.IdenticalCount

	for _, pair := range sets.Changed {
		var sourceEntity, targetEntity models.Entity
		if err := json.Unmarshal([]byte(pair.SourceBlob), &sourceEntity); err != nil {
			return nil, fmt.Errorf("decode source blob for %s: %w", pair.Identifier, err)
		}
		if err := json.Unmarshal([]byte(pair.TargetBlob), &targetEntity); err != nil {
			return nil, fmt.Errorf("decode target blob for %s: %w", pair.Identifier, err)
		}

		result.Summary.Changed++
		result.Changes = append(result.Changes, models.EntityChange{
			Identifier:    pair.Identifier,
			Type:          models.EntityChangeTypeChanged,
			PropertyDiffs: getPropertyDiffs(sourceEntity, targetEntity, excludedDiffProps),
		})
	}

	for _, entity := range sets.NotMigrated {
		result.Summary.NotMigrated++
		result.Changes = append(result.Changes, models.EntityChange{
			Identifier: entity.Identifier,
			Type:       models.EntityChangeTypeNotMigrated,
			OldEntity:  entityToMap(entity),
		})
	}

	for _, entity := range sets.Orphaned {
		result.Summary.Orphaned++
		result.Changes = append(result.Changes, models.EntityChange{
			Identifier: entity.Identifier,
			Type:       models.EntityChangeTypeOrphaned,
			NewEntity:  entityToMap(entity),
		})
	}

	return result, nil
}

// PrintSummary prints the diff summary with entity identifiers
func (s *Service) PrintSummary(result *models.DiffResult) {
	fmt.Println()
	fmt.Printf("📊 %s (old) → %s (new)\n", result.SourceBlueprint, result.TargetBlueprint)
	fmt.Println("   " + repeatString("─", 40))
	fmt.Printf("   ✅ %d identical\n", result.Summary.Identical)
	if result.Summary.NotMigrated > 0 {
		fmt.Printf("   ⚠️  %d not migrated (only in old)\n", result.Summary.NotMigrated)
		for _, change := range result.Changes {
			if change.Type == models.EntityChangeTypeNotMigrated {
				fmt.Printf("       • %s\n", change.Identifier)
			}
		}
	}
	fmt.Printf("   📝 %d changed\n", result.Summary.Changed)
	if result.Summary.Orphaned > 0 {
		fmt.Printf("   ❌ %d orphaned (only in new)\n", result.Summary.Orphaned)
		for _, change := range result.Changes {
			if change.Type == models.EntityChangeTypeOrphaned {
				fmt.Printf("       • %s\n", change.Identifier)
			}
		}
	}
	fmt.Println()
}

// PrintDetailedDiffs prints detailed property diffs for changed entities
func (s *Service) PrintDetailedDiffs(changes []models.EntityChange, limit int) {
	changedCount := 0
	for _, change := range changes {
		if change.Type == models.EntityChangeTypeChanged {
			changedCount++
		}
	}

	if changedCount == 0 {
		return
	}

	fmt.Println("📋 Changed Entities (showing first " + fmt.Sprintf("%d", limit) + "):")
	fmt.Println()

	shown := 0
	for _, change := range changes {
		if change.Type != models.EntityChangeTypeChanged {
			continue
		}

		if shown >= limit {
			fmt.Printf("⏭️  Showing %d of %d changed entities. Use --limit to show more.\n", limit, changedCount)
			break
		}

		if shown > 0 {
			fmt.Println()
		}

		fmt.Printf("  • %s\n", change.Identifier)
		flatDiffs := flattenDiffs(change.PropertyDiffs)
		for _, path := range flatDiffs {
			fmt.Printf("    - %s: %v\n", path.Path, path.OldValue)
			fmt.Printf("    + %s: %v\n", path.Path, path.NewValue)
		}
		shown++
	}

	fmt.Println()
}

func filterProperties(props map[string]any, excluded map[string]bool) map[string]any {
	result := make(map[string]any)
	for k, v := range props {
		if !excluded[k] {
			result[k] = v
		}
	}
	return result
}

func getPropertyDiffs(e1, e2 models.Entity, excluded map[string]bool) map[string]models.PropertyDiff {
	diffs := make(map[string]models.PropertyDiff)

	if e1.Title != e2.Title {
		diffs["title"] = models.PropertyDiff{
			OldValue: e1.Title,
			NewValue: e2.Title,
		}
	}

	m1 := filterProperties(e1.Properties, excluded)
	m2 := filterProperties(e2.Properties, excluded)

	for k, v1 := range m1 {
		v2, exists := m2[k]
		if !exists || !reflect.DeepEqual(v1, v2) {
			diffs["properties."+k] = models.PropertyDiff{
				OldValue: v1,
				NewValue: v2,
			}
		}
	}

	for k, v2 := range m2 {
		if _, exists := m1[k]; !exists {
			diffs["properties."+k] = models.PropertyDiff{
				OldValue: nil,
				NewValue: v2,
			}
		}
	}

	if !reflect.DeepEqual(e1.Relations, e2.Relations) {
		diffs["relations"] = models.PropertyDiff{
			OldValue: e1.Relations,
			NewValue: e2.Relations,
		}
	}

	return diffs
}

func entityToMap(e models.Entity) map[string]any {
	data, _ := json.Marshal(e)
	var m map[string]any
	json.Unmarshal(data, &m)
	return m
}

func repeatString(s string, count int) string {
	var result string
	for i := 0; i < count; i++ {
		result += s
	}
	return result
}

func flattenDiffs(diffs map[string]models.PropertyDiff) []models.FlattenedDiff {
	var result []models.FlattenedDiff

	for prop, diff := range diffs {
		flattenedPaths := flattenValue(prop, diff.OldValue, diff.NewValue)
		result = append(result, flattenedPaths...)
	}

	return result
}

func flattenValue(prefix string, oldVal, newVal any) []models.FlattenedDiff {
	var result []models.FlattenedDiff

	oldMap, oldIsMap := oldVal.(map[string]any)
	newMap, newIsMap := newVal.(map[string]any)

	if oldIsMap && newIsMap {
		allKeys := make(map[string]bool)
		for k := range oldMap {
			allKeys[k] = true
		}
		for k := range newMap {
			allKeys[k] = true
		}

		for k := range allKeys {
			oldV := oldMap[k]
			newV := newMap[k]
			newPrefix := prefix + "." + k
			result = append(result, flattenValue(newPrefix, oldV, newV)...)
		}
	} else if oldIsMap || newIsMap {
		result = append(result, models.FlattenedDiff{
			Path:     prefix,
			OldValue: oldVal,
			NewValue: newVal,
		})
	} else if !reflect.DeepEqual(oldVal, newVal) {
		result = append(result, models.FlattenedDiff{
			Path:     prefix,
			OldValue: oldVal,
			NewValue: newVal,
		})
	}

	return result
}
