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
	SourceBlueprint string         `json:"sourceBlueprint"`
	TargetBlueprint string         `json:"targetBlueprint"`
	SourceTotal     int            `json:"sourceTotal"` // true total available in old install
	TargetTotal     int            `json:"targetTotal"` // true total available in new install
	SourceCompared  int            `json:"sourceCompared"`
	TargetCompared  int            `json:"targetCompared"`
	Summary         DiffSummary    `json:"summary"`
	Changes         []EntityChange `json:"changes"`
}

// DiffSummary holds summary statistics
type DiffSummary struct {
	Identical   int `json:"identical"`
	NotMigrated int `json:"notMigrated"`
	Changed     int `json:"changed"`
	Orphaned    int `json:"orphaned"`
}

// EntityChange represents a single entity difference
type EntityChange struct {
	Identifier    string                  `json:"identifier"`
	Type          string                  `json:"type"` // "identical", "changed", "notMigrated", "orphaned"
	OldEntity     map[string]interface{}  `json:"oldEntity,omitempty"`
	NewEntity     map[string]interface{}  `json:"newEntity,omitempty"`
	PropertyDiffs map[string]PropertyDiff `json:"propertyDiffs,omitempty"`
}

// PropertyDiff represents a single property difference
type PropertyDiff struct {
	OldValue interface{} `json:"oldValue"`
	NewValue interface{} `json:"newValue"`
}

