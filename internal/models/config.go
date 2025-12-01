package models

// Config holds migration configuration
type Config struct {
	PortAPIURL          string
	ClientID            string
	ClientSecret        string
	OldInstallationID   string
	NewInstallationID   string
}

// MigrationStats holds migration statistics
type MigrationStats struct {
	TotalBlueprints   int
	TotalEntities     int
	TotalBatches      int
	SuccessfulBatches int
	FailedBatches     int
	Errors            []string
}

// DiffResult holds the comparison results
type DiffResult struct {
	SourceBlueprint string
	TargetBlueprint string
	Summary         DiffSummary
	Changes         []EntityChange
}

// DiffSummary holds summary statistics
type DiffSummary struct {
	Identical   int
	NotMigrated int
	Changed     int
	Orphaned    int
}

// EntityChange represents a single entity difference
type EntityChange struct {
	Identifier   string
	Type         string // "identical", "changed", "notMigrated", "orphaned"
	OldEntity    map[string]interface{}
	NewEntity    map[string]interface{}
	PropertyDiffs map[string]PropertyDiff
}

// PropertyDiff represents a single property difference
type PropertyDiff struct {
	OldValue interface{}
	NewValue interface{}
}

