package models

type MigrationStats struct {
	TotalBlueprints   int
	TotalEntities     int
	TotalBatches      int
	SuccessfulBatches int
	FailedBatches     int
	Errors            []string
}

type DiffResult struct {
	SourceBlueprint string
	TargetBlueprint string
	Summary         DiffSummary
	Changes         []EntityChange
}

type EntityChangeType string

const (
	EntityChangeTypeIdentical   EntityChangeType = "identical"
	EntityChangeTypeChanged     EntityChangeType = "changed"
	EntityChangeTypeNotMigrated EntityChangeType = "notMigrated"
	EntityChangeTypeOrphaned    EntityChangeType = "orphaned"
)

type DiffSummary struct {
	Identical   int
	NotMigrated int
	Changed     int
	Orphaned    int
}

type EntityChange struct {
	Identifier    string
	Type          EntityChangeType
	OldEntity     map[string]any
	NewEntity     map[string]any
	PropertyDiffs map[string]PropertyDiff
}

type PropertyDiff struct {
	OldValue any
	NewValue any
}

type FlattenedDiff struct {
	Path     string
	OldValue any
	NewValue any
}
