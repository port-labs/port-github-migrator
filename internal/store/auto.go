package store

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/port-labs/port-github-migrator/internal/models"
)

const autoDir = "auto"

// AutoResult is the on-disk shape of a single auto-mode run's outcome for one
// (sourceBlueprint -> targetBlueprint) pair: the changed entities (with their
// property diffs) and the identifiers of entities present in the source
// blueprint under the old install but missing from the target blueprint
// under the new install.
type AutoResult struct {
	SourceBlueprint string                `json:"sourceBlueprint"`
	TargetBlueprint string                `json:"targetBlueprint"`
	GeneratedAt     time.Time             `json:"generatedAt"`
	Summary         models.DiffSummary    `json:"summary"`
	Changed         []models.EntityChange `json:"changed"`
	NotMigrated     []string              `json:"notMigrated"`
}

// SaveAutoResult writes the result file for an auto-mode run to
// <root>/auto/<oldInstallID>/migration-result-<uuid>.json and returns the
// absolute path it was written to.
func (s *Store) SaveAutoResult(oldInstallID string, r AutoResult) (string, error) {
	if oldInstallID == "" {
		return "", errors.New("oldInstallID is required")
	}

	id, err := newUUIDv4()
	if err != nil {
		return "", fmt.Errorf("failed to generate result id: %w", err)
	}

	dir := filepath.Join(s.root, autoDir, oldInstallID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("failed to create auto results dir: %w", err)
	}

	path := filepath.Join(dir, fmt.Sprintf("migration-result-%s.json", id))

	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return "", err
	}

	if err := os.WriteFile(path, data, 0o644); err != nil {
		return "", err
	}
	return path, nil
}

// newUUIDv4 generates an RFC 4122 v4 UUID without pulling in a new dependency.
func newUUIDv4() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80

	hexed := hex.EncodeToString(b[:])
	return fmt.Sprintf("%s-%s-%s-%s-%s", hexed[0:8], hexed[8:12], hexed[12:16], hexed[16:20], hexed[20:32]), nil
}
