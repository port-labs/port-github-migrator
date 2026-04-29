package diff

import (
	"encoding/json"
	"fmt"
	"io"
	"reflect"
	"sync"
	"time"

	"github.com/briandowns/spinner"
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

// CompareBlueprints compares entities between source and target blueprints.
// Source and target searches run concurrently. A spinner is rendered to
// spinnerOut while the requests are in flight; pass io.Discard to disable it.
func (s *Service) CompareBlueprints(sourceBP, targetBP, oldInstallID, newInstallID string, spinnerOut io.Writer) (*models.DiffResult, error) {
	sp := spinner.New(spinner.CharSets[14], 100*time.Millisecond)
	sp.Writer = spinnerOut
	sp.HideCursor = true

	var (
		progressMu sync.Mutex
		fetched    int
	)
	setFetchSuffix := func() {
		sp.Lock()
		sp.Suffix = fmt.Sprintf(" Fetching entities for %s and %s (%d/2)", sourceBP, targetBP, fetched)
		sp.Unlock()
	}
	setDiffSuffix := func() {
		sp.Lock()
		sp.Suffix = fmt.Sprintf(" Performing diff between %s and %s", sourceBP, targetBP)
		sp.Unlock()
	}
	setFetchSuffix()
	sp.Start()
	defer sp.Stop()

	var (
		sourceEntities []port.Entity
		targetEntities []port.Entity
		sourceTotal    int
		targetTotal    int
		sourceErr      error
		targetErr      error
		sourceCountErr error
		targetCountErr error
		wg             sync.WaitGroup
	)

	tickFetched := func() {
		progressMu.Lock()
		fetched++
		setFetchSuffix()
		progressMu.Unlock()
	}

	// The target search must filter by the source identifiers, so it depends
	// on the source search completing first. The two count calls are
	// independent and run alongside both searches.
	sourceDone := make(chan struct{})

	wg.Add(4)
	go func() {
		defer wg.Done()
		defer close(sourceDone)
		sourceEntities, sourceErr = s.client.SearchOldEntitiesByBlueprint(sourceBP, oldInstallID, nil)
		tickFetched()
	}()
	go func() {
		defer wg.Done()
		<-sourceDone
		if sourceErr != nil {
			return
		}
		identifiers := make([]string, len(sourceEntities))
		for i, e := range sourceEntities {
			identifiers[i] = e.Identifier
		}
		targetEntities, targetErr = s.client.SearchNewEntitiesByBlueprint(targetBP, newInstallID, identifiers, nil)
		tickFetched()
	}()
	go func() {
		defer wg.Done()
		sourceTotal, sourceCountErr = s.client.CountOldEntitiesByBlueprint(sourceBP, oldInstallID)
	}()
	go func() {
		defer wg.Done()
		targetTotal, targetCountErr = s.client.CountNewEntitiesByBlueprint(targetBP, newInstallID)
	}()
	wg.Wait()

	if sourceErr != nil {
		return nil, fmt.Errorf("failed to get source entities: %w", sourceErr)
	}
	if targetErr != nil {
		return nil, fmt.Errorf("failed to get target entities: %w", targetErr)
	}
	if sourceCountErr != nil {
		return nil, fmt.Errorf("failed to count source entities: %w", sourceCountErr)
	}
	if targetCountErr != nil {
		return nil, fmt.Errorf("failed to count target entities: %w", targetCountErr)
	}

	setDiffSuffix()

	// Index entities
	sourceMap := make(map[string]port.Entity)
	targetMap := make(map[string]port.Entity)
	sourceIdentifiers := make([]string, 0, len(sourceEntities))

	for _, e := range sourceEntities {
		sourceMap[e.Identifier] = e
		sourceIdentifiers = append(sourceIdentifiers, e.Identifier)
	}

	for _, e := range targetEntities {
		targetMap[e.Identifier] = e
	}

	// Compare entities
	result := &models.DiffResult{
		SourceBlueprint:   sourceBP,
		TargetBlueprint:   targetBP,
		SourceTotal:       sourceTotal,
		TargetTotal:       targetTotal,
		SourceIdentifiers: sourceIdentifiers,
		SourceCompared:    len(sourceEntities),
		TargetCompared:    len(targetEntities),
		Changes:           []models.EntityChange{},
	}

	// Check common entities
	for id, sourceEntity := range sourceMap {
		if targetEntity, exists := targetMap[id]; exists {
			// Entity exists in both
			if entitiesEqual(sourceEntity, targetEntity) {
				result.Summary.Identical++
			} else {
				result.Summary.Changed++
				change := models.EntityChange{
					Identifier: id,
					Type:       "changed",
					PropertyDiffs: getPropertyDiffs(sourceEntity, targetEntity),
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

	return result, nil
}

// PrintSummary writes the diff summary with entity identifiers to w
func (s *Service) PrintSummary(w io.Writer, result *models.DiffResult) {
	fmt.Fprintln(w)
	fmt.Fprintf(w, "📊 %s (old) → %s (new)\n", result.SourceBlueprint, result.TargetBlueprint)
	fmt.Fprintln(w, "   "+repeatString("─", 40))
	if result.SourceCompared < result.SourceTotal {
		fmt.Fprintf(w, "   ⚠️  source: compared %d / %d (capped)\n", result.SourceCompared, result.SourceTotal)
	}
	if result.TargetCompared < result.TargetTotal {
		fmt.Fprintf(w, "   ⚠️  target: compared %d / %d (capped)\n", result.TargetCompared, result.TargetTotal)
	}
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

func entitiesEqual(e1, e2 port.Entity) bool {
	// Compare title
	if e1.Title != e2.Title {
		return false
	}

	// Compare properties (excluding specific fields)
	if !reflect.DeepEqual(e1.Properties, e2.Properties) {
		return false
	}

	// Compare relations
	return reflect.DeepEqual(e1.Relations, e2.Relations)
}

func getPropertyDiffs(e1, e2 port.Entity) map[string]models.PropertyDiff {
	diffs := make(map[string]models.PropertyDiff)

	// Check title
	if e1.Title != e2.Title {
		diffs["title"] = models.PropertyDiff{
			OldValue: e1.Title,
			NewValue: e2.Title,
		}
	}

	// Check e1 properties
	for k, v1 := range e1.Properties {
		v2, exists := e2.Properties[k]
		if !exists || !reflect.DeepEqual(v1, v2) {
			diffs["properties."+k] = models.PropertyDiff{
				OldValue: v1,
				NewValue: v2,
			}
		}
	}

	// Check e2 properties for new fields
	for k, v2 := range e2.Properties {
		if _, exists := e1.Properties[k]; !exists {
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

