package diff

import (
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

	identical, changed, notMigrated := DiffEntities(sourceEntities, targetEntities)

	sourceIdentifiers := make([]string, 0, len(sourceEntities))
	for _, e := range sourceEntities {
		sourceIdentifiers = append(sourceIdentifiers, e.Identifier)
	}

	return &models.DiffResult{
		SourceBlueprint:   sourceBP,
		TargetBlueprint:   targetBP,
		SourceTotal:       sourceTotal,
		TargetTotal:       targetTotal,
		SourceIdentifiers: sourceIdentifiers,
		SourceCompared:    len(sourceEntities),
		TargetCompared:    len(targetEntities),
		Changed:           changed,
		NotMigrated:       notMigrated,
		Summary: models.DiffSummary{
			Identical:   len(identical),
			Changed:     len(changed),
			NotMigrated: len(notMigrated),
		},
	}, nil
}

// DiffEntities compares one batch of source entities to the target entities
// fetched for them. It returns three disjoint slices so callers can act on
// them directly without re-discriminating by type:
//
//   - identical:  identifiers that match on both sides
//   - changed:    EntityChange values carrying the per-property diffs
//   - notMigrated: identifiers present on the source but missing from the target
func DiffEntities(source, target []port.Entity) (identical []string, changed []models.EntityChange, notMigrated []string) {
	targetMap := make(map[string]port.Entity, len(target))
	for _, e := range target {
		targetMap[e.Identifier] = e
	}

	identical = make([]string, 0, len(source))
	changed = make([]models.EntityChange, 0)
	notMigrated = make([]string, 0)

	for _, sourceEntity := range source {
		id := sourceEntity.Identifier
		targetEntity, exists := targetMap[id]
		if !exists {
			notMigrated = append(notMigrated, id)
			continue
		}
		if entitiesEqual(sourceEntity, targetEntity) {
			identical = append(identical, id)
			continue
		}
		changed = append(changed, models.EntityChange{
			Identifier:    id,
			PropertyDiffs: getPropertyDiffs(sourceEntity, targetEntity),
		})
	}

	return identical, changed, notMigrated
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
	}
	fmt.Fprintf(w, "   📝 %d changed\n", result.Summary.Changed)
	fmt.Fprintln(w)
}

// PrintDetailedDiffs writes detailed property diffs for changed entities and
// the identifiers of not-migrated entities to w. limit caps each section
// independently; pass <= 0 for unlimited.
func (s *Service) PrintDetailedDiffs(w io.Writer, result *models.DiffResult, limit int) {
	printChangedSection(w, result.Changed, limit)
	printNotMigratedSection(w, result.NotMigrated, limit)
}

func printChangedSection(w io.Writer, changed []models.EntityChange, limit int) {
	if len(changed) == 0 {
		return
	}

	header := "📋 Changed Entities"
	if limit > 0 && len(changed) > limit {
		fmt.Fprintf(w, "%s (showing first %d):\n\n", header, limit)
	} else {
		fmt.Fprintf(w, "%s:\n\n", header)
	}

	shown := 0
	for _, change := range changed {
		if limit > 0 && shown >= limit {
			fmt.Fprintf(w, "⏭️  Showing %d of %d changed entities. Use --limit to show more, or --output to dump the full list to a file.\n", limit, len(changed))
			break
		}
		if shown > 0 {
			fmt.Fprintln(w)
		}
		fmt.Fprintf(w, "  • %s\n", change.Identifier)
		for _, path := range flattenDiffs(change.PropertyDiffs) {
			fmt.Fprintf(w, "    - %s: %v\n", path.Path, path.OldValue)
			fmt.Fprintf(w, "    + %s: %v\n", path.Path, path.NewValue)
		}
		shown++
	}

	fmt.Fprintln(w)
}

func printNotMigratedSection(w io.Writer, notMigrated []string, limit int) {
	if len(notMigrated) == 0 {
		return
	}

	header := "⚠️  Not Migrated (only in old)"
	if limit > 0 && len(notMigrated) > limit {
		fmt.Fprintf(w, "%s (showing first %d):\n\n", header, limit)
	} else {
		fmt.Fprintf(w, "%s:\n\n", header)
	}

	shown := 0
	for _, id := range notMigrated {
		if limit > 0 && shown >= limit {
			fmt.Fprintf(w, "⏭️  Showing %d of %d not-migrated entities. Use --limit to show more, or --output to dump the full list to a file.\n", limit, len(notMigrated))
			break
		}
		fmt.Fprintf(w, "  • %s\n", id)
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

