package diff

import (
	"encoding/json"
	"fmt"
	"io"
	"reflect"

	"github.com/port-labs/port-github-migrator/internal/models"
	"github.com/port-labs/port-github-migrator/internal/port"
)

// Service handles entity comparison
type Service struct {
	client *port.Client
}

// NewService creates a new diff service
func NewService(client *port.Client) *Service {
	return &Service{client: client}
}

// CompareBlueprints compares entities between source and target blueprints
func (s *Service) CompareBlueprints(sourceBP, targetBP, oldInstallID, newInstallID string) (*models.DiffResult, error) {
	// Get source entities (old installation)
	sourceEntities, err := s.client.SearchOldEntitiesByBlueprint(sourceBP, oldInstallID, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get source entities: %w", err)
	}

	// Get target entities (new installation)
	targetEntities, err := s.client.SearchNewEntitiesByBlueprint(targetBP, newInstallID, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get target entities: %w", err)
	}

	// Index entities
	sourceMap := make(map[string]port.Entity)
	targetMap := make(map[string]port.Entity)

	for _, e := range sourceEntities {
		sourceMap[e.Identifier] = e
	}

	for _, e := range targetEntities {
		targetMap[e.Identifier] = e
	}

	// Compare entities
	result := &models.DiffResult{
		SourceBlueprint: sourceBP,
		TargetBlueprint: targetBP,
		Changes:         []models.EntityChange{},
	}

	excludedProps := map[string]bool{
		"blueprint": true,
		"createdAt": true,
		"updatedAt": true,
		"createdBy": true,
		"updatedBy": true,
	}

	// Check common entities
	for id, sourceEntity := range sourceMap {
		if targetEntity, exists := targetMap[id]; exists {
			// Entity exists in both
			if entitiesEqual(sourceEntity, targetEntity, excludedProps) {
				result.Summary.Identical++
			} else {
				result.Summary.Changed++
				change := models.EntityChange{
					Identifier: id,
					Type:       "changed",
					PropertyDiffs: getPropertyDiffs(sourceEntity, targetEntity, excludedProps),
				}
				result.Changes = append(result.Changes, change)
			}
		} else {
			// Entity only in source (not migrated)
			result.Summary.NotMigrated++
			change := models.EntityChange{
				Identifier: id,
				Type:       "notMigrated",
				OldEntity:  entityToMap(sourceEntity),
			}
			result.Changes = append(result.Changes, change)
		}
	}

	// Check for orphaned entities (only in target)
	for id := range targetMap {
		if _, exists := sourceMap[id]; !exists {
			result.Summary.Orphaned++
			change := models.EntityChange{
				Identifier: id,
				Type:       "orphaned",
			}
			result.Changes = append(result.Changes, change)
		}
	}

	return result, nil
}

// PrintSummary writes the diff summary with entity identifiers to w
func (s *Service) PrintSummary(w io.Writer, result *models.DiffResult) {
	fmt.Fprintln(w)
	fmt.Fprintf(w, "📊 %s (old) → %s (new)\n", result.SourceBlueprint, result.TargetBlueprint)
	fmt.Fprintln(w, "   "+repeatString("─", 40))
	fmt.Fprintf(w, "   ✅ %d identical\n", result.Summary.Identical)
	if result.Summary.NotMigrated > 0 {
		fmt.Fprintf(w, "   ⚠️  %d not migrated (only in old)\n", result.Summary.NotMigrated)
		for _, change := range result.Changes {
			if change.Type == "notMigrated" {
				fmt.Fprintf(w, "       • %s\n", change.Identifier)
			}
		}
	}
	fmt.Fprintf(w, "   📝 %d changed\n", result.Summary.Changed)
	if result.Summary.Orphaned > 0 {
		fmt.Fprintf(w, "   ❌ %d orphaned (only in new)\n", result.Summary.Orphaned)
		for _, change := range result.Changes {
			if change.Type == "orphaned" {
				fmt.Fprintf(w, "       • %s\n", change.Identifier)
			}
		}
	}
	fmt.Fprintln(w)
}

// PrintDetailedDiffs writes detailed property diffs for changed entities to w
func (s *Service) PrintDetailedDiffs(w io.Writer, changes []models.EntityChange, limit int) {
	changedCount := 0
	for _, change := range changes {
		if change.Type == "changed" {
			changedCount++
		}
	}

	if changedCount == 0 {
		return
	}

	fmt.Fprintf(w, "📋 Changed Entities (showing first %d):\n\n", limit)

	shown := 0
	for _, change := range changes {
		if change.Type != "changed" {
			continue
		}

		if shown >= limit {
			fmt.Fprintf(w, "⏭️  Showing %d of %d changed entities. Use --limit to show more.\n", limit, changedCount)
			break
		}

		if shown > 0 {
			fmt.Fprintln(w)
		}

		fmt.Fprintf(w, "  • %s\n", change.Identifier)
		flatDiffs := flattenDiffs(change.PropertyDiffs)
		for _, path := range flatDiffs {
			fmt.Fprintf(w, "    - %s: %v\n", path.Path, path.OldValue)
			fmt.Fprintf(w, "    + %s: %v\n", path.Path, path.NewValue)
		}
		shown++
	}

	fmt.Fprintln(w)
}

// Helper functions

func entitiesEqual(e1, e2 port.Entity, excluded map[string]bool) bool {
	// Compare title
	if e1.Title != e2.Title {
		return false
	}

	// Compare properties (excluding specific fields)
	m1 := filterProperties(e1.Properties, excluded)
	m2 := filterProperties(e2.Properties, excluded)

	if !reflect.DeepEqual(m1, m2) {
		return false
	}

	// Compare relations
	return reflect.DeepEqual(e1.Relations, e2.Relations)
}

func filterProperties(props map[string]interface{}, excluded map[string]bool) map[string]interface{} {
	result := make(map[string]interface{})
	for k, v := range props {
		if !excluded[k] {
			result[k] = v
		}
	}
	return result
}

func getPropertyDiffs(e1, e2 port.Entity, excluded map[string]bool) map[string]models.PropertyDiff {
	diffs := make(map[string]models.PropertyDiff)

	// Check title
	if e1.Title != e2.Title {
		diffs["title"] = models.PropertyDiff{
			OldValue: e1.Title,
			NewValue: e2.Title,
		}
	}

	m1 := filterProperties(e1.Properties, excluded)
	m2 := filterProperties(e2.Properties, excluded)

	// Check e1 properties
	for k, v1 := range m1 {
		v2, exists := m2[k]
		if !exists || !reflect.DeepEqual(v1, v2) {
			diffs["properties."+k] = models.PropertyDiff{
				OldValue: v1,
				NewValue: v2,
			}
		}
	}

	// Check e2 properties for new fields
	for k, v2 := range m2 {
		if _, exists := m1[k]; !exists {
			diffs["properties."+k] = models.PropertyDiff{
				OldValue: nil,
				NewValue: v2,
			}
		}
	}

	// Check relations
	if !reflect.DeepEqual(e1.Relations, e2.Relations) {
		diffs["relations"] = models.PropertyDiff{
			OldValue: e1.Relations,
			NewValue: e2.Relations,
		}
	}

	return diffs
}

func entityToMap(e port.Entity) map[string]interface{} {
	data, _ := json.Marshal(e)
	var m map[string]interface{}
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

// FlattenedDiff represents a single flattened property difference
type FlattenedDiff struct {
	Path     string
	OldValue interface{}
	NewValue interface{}
}

// flattenDiffs flattens nested property diffs into dot-notation paths
func flattenDiffs(diffs map[string]models.PropertyDiff) []FlattenedDiff {
	var result []FlattenedDiff

	for prop, diff := range diffs {
		flattenedPaths := flattenValue(prop, diff.OldValue, diff.NewValue)
		result = append(result, flattenedPaths...)
	}

	return result
}

// flattenValue recursively flattens a value into dot-notation paths
func flattenValue(prefix string, oldVal, newVal interface{}) []FlattenedDiff {
	var result []FlattenedDiff

	// If both are maps, recursively flatten
	oldMap, oldIsMap := oldVal.(map[string]interface{})
	newMap, newIsMap := newVal.(map[string]interface{})

	if oldIsMap && newIsMap {
		// Check all keys from both maps
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
		// One is a map, one isn't - show as single diff
		result = append(result, FlattenedDiff{
			Path:     prefix,
			OldValue: oldVal,
			NewValue: newVal,
		})
	} else if !reflect.DeepEqual(oldVal, newVal) {
		// Values are different - add as diff
		result = append(result, FlattenedDiff{
			Path:     prefix,
			OldValue: oldVal,
			NewValue: newVal,
		})
	}

	return result
}

