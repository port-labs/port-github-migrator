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

// DiffResult holds the comparison results. Changed and NotMigrated are kept
// as disjoint slices so callers (renderers, the auto-mode result file, etc.)
// don't have to demux a tagged type at every read site.
type DiffResult struct {
	SourceBlueprint   string         `json:"sourceBlueprint"`
	TargetBlueprint   string         `json:"targetBlueprint"`
	SourceTotal       int            `json:"sourceTotal"` // true total available in old install
	TargetTotal       int            `json:"targetTotal"` // true total available in new install
	SourceCompared    int            `json:"sourceCompared"`
	TargetCompared    int            `json:"targetCompared"`
	SourceIdentifiers []string       `json:"sourceIdentifiers,omitempty"` // identifiers actually compared on the source side
	Summary           DiffSummary    `json:"summary"`
	Changed           []EntityChange `json:"changed"`     // entities present in both with property differences
	NotMigrated       []string       `json:"notMigrated"` // identifiers present only in the source
}

// DiffSummary holds summary statistics
type DiffSummary struct {
	Identical   int `json:"identical"`
	NotMigrated int `json:"notMigrated"`
	Changed     int `json:"changed"`
}

// EntityChange represents an entity that exists on both sides with differing
// properties. Use DiffResult.NotMigrated for entities missing from the target.
type EntityChange struct {
	Identifier    string                  `json:"identifier"`
	PropertyDiffs map[string]PropertyDiff `json:"propertyDiffs,omitempty"`
}

// PropertyDiff represents a single property difference
type PropertyDiff struct {
	OldValue interface{} `json:"oldValue"`
	NewValue interface{} `json:"newValue"`
}

