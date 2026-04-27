package store

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/port-labs/port-github-migrator/internal/models"

	_ "modernc.org/sqlite"
)

const (
	schemaVersion    = "1"
	schemaVersionKey = "schema_version"
)

// Store is a SQLite-backed cache for Port entities and per-blueprint sync metadata.
type Store struct {
	db *sql.DB
}

// DefaultPath returns the default database path under the user cache directory.
func DefaultPath() (string, error) {
	base, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "port-github-migrator", "cache.db"), nil
}

// Open opens (and migrates if needed) the SQLite database at path.
func Open(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("create cache dir: %w", err)
	}

	db, err := sql.Open("sqlite", path+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)")
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	db.SetMaxOpenConns(1)

	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

// Close closes the underlying database handle.
func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *Store) migrate() error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS schema_meta (
			key TEXT PRIMARY KEY,
			value TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS entities (
			blueprint       TEXT NOT NULL,
			installation_id TEXT NOT NULL,
			identifier      TEXT NOT NULL,
			hash            TEXT NOT NULL,
			blob            TEXT NOT NULL,
			updated_at      TEXT NOT NULL,
			PRIMARY KEY (blueprint, installation_id, identifier)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_entities_identifier_blueprint
			ON entities (identifier, blueprint)`,
		`CREATE INDEX IF NOT EXISTS idx_entities_installation
			ON entities (installation_id, blueprint)`,
		`CREATE TABLE IF NOT EXISTS sync_metadata (
			blueprint        TEXT NOT NULL,
			installation_id  TEXT NOT NULL,
			last_sync_at     TEXT NOT NULL,
			PRIMARY KEY (blueprint, installation_id)
		)`,
	}

	for _, stmt := range stmts {
		if _, err := s.db.Exec(stmt); err != nil {
			return fmt.Errorf("apply schema: %w", err)
		}
	}

	if _, err := s.db.Exec(
		`INSERT INTO schema_meta (key, value) VALUES (?, ?)
		 ON CONFLICT(key) DO UPDATE SET value=excluded.value`,
		schemaVersionKey, schemaVersion,
	); err != nil {
		return fmt.Errorf("write schema version: %w", err)
	}
	return nil
}

// GetSyncTimestamp returns the last sync timestamp for (blueprint, installationID).
func (s *Store) GetSyncTimestamp(blueprint, installationID string) (time.Time, bool, error) {
	var raw string
	err := s.db.QueryRow(
		`SELECT last_sync_at FROM sync_metadata WHERE blueprint = ? AND installation_id = ?`,
		blueprint, installationID,
	).Scan(&raw)
	if err == sql.ErrNoRows {
		return time.Time{}, false, nil
	}
	if err != nil {
		return time.Time{}, false, fmt.Errorf("get sync timestamp: %w", err)
	}
	ts, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		return time.Time{}, false, fmt.Errorf("parse sync timestamp %q: %w", raw, err)
	}
	return ts, true, nil
}

// SetSyncTimestamp upserts the last sync timestamp for (blueprint, installationID).
func (s *Store) SetSyncTimestamp(blueprint, installationID string, ts time.Time) error {
	_, err := s.db.Exec(
		`INSERT INTO sync_metadata (blueprint, installation_id, last_sync_at)
		 VALUES (?, ?, ?)
		 ON CONFLICT(blueprint, installation_id) DO UPDATE SET last_sync_at = excluded.last_sync_at`,
		blueprint, installationID, ts.UTC().Format(time.RFC3339Nano),
	)
	if err != nil {
		return fmt.Errorf("set sync timestamp: %w", err)
	}
	return nil
}

// DeleteSyncTimestamp removes the sync metadata row for (blueprint, installationID).
func (s *Store) DeleteSyncTimestamp(blueprint, installationID string) error {
	_, err := s.db.Exec(
		`DELETE FROM sync_metadata WHERE blueprint = ? AND installation_id = ?`,
		blueprint, installationID,
	)
	if err != nil {
		return fmt.Errorf("delete sync timestamp: %w", err)
	}
	return nil
}

// UpsertEntities inserts or replaces the given entities under (blueprint, installationID).
func (s *Store) UpsertEntities(blueprint, installationID string, entities []models.Entity) error {
	if len(entities) == 0 {
		return nil
	}

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin upsert: %w", err)
	}

	stmt, err := tx.Prepare(`
		INSERT INTO entities (blueprint, installation_id, identifier, hash, blob, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(blueprint, installation_id, identifier) DO UPDATE SET
			hash       = excluded.hash,
			blob       = excluded.blob,
			updated_at = excluded.updated_at`)
	if err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("prepare upsert: %w", err)
	}
	defer stmt.Close()

	for _, entity := range entities {
		hash, err := EntityHash(entity)
		if err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("hash entity %s: %w", entity.Identifier, err)
		}

		blob, err := json.Marshal(entity)
		if err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("marshal entity %s: %w", entity.Identifier, err)
		}

		updatedAt := entity.UpdatedAt
		if updatedAt == "" {
			updatedAt = time.Now().UTC().Format(time.RFC3339Nano)
		}

		if _, err := stmt.Exec(blueprint, installationID, entity.Identifier, hash, string(blob), updatedAt); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("upsert entity %s: %w", entity.Identifier, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit upsert: %w", err)
	}
	return nil
}

// LoadEntities returns all cached entities for (blueprint, installationID).
func (s *Store) LoadEntities(blueprint, installationID string) ([]models.Entity, error) {
	rows, err := s.db.Query(
		`SELECT blob FROM entities WHERE blueprint = ? AND installation_id = ?`,
		blueprint, installationID,
	)
	if err != nil {
		return nil, fmt.Errorf("query entities: %w", err)
	}
	defer rows.Close()

	var entities []models.Entity
	for rows.Next() {
		var blob string
		if err := rows.Scan(&blob); err != nil {
			return nil, fmt.Errorf("scan entity: %w", err)
		}

		var entity models.Entity
		if err := json.Unmarshal([]byte(blob), &entity); err != nil {
			return nil, fmt.Errorf("decode entity blob: %w", err)
		}
		entities = append(entities, entity)
	}
	return entities, rows.Err()
}

// CountEntities returns how many entities are cached for (blueprint, installationID).
func (s *Store) CountEntities(blueprint, installationID string) (int, error) {
	var count int
	err := s.db.QueryRow(
		`SELECT COUNT(*) FROM entities WHERE blueprint = ? AND installation_id = ?`,
		blueprint, installationID,
	).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count entities: %w", err)
	}
	return count, nil
}

// DeleteEntities removes all cached entities for (blueprint, installationID).
func (s *Store) DeleteEntities(blueprint, installationID string) error {
	_, err := s.db.Exec(
		`DELETE FROM entities WHERE blueprint = ? AND installation_id = ?`,
		blueprint, installationID,
	)
	if err != nil {
		return fmt.Errorf("delete entities: %w", err)
	}
	return nil
}

// DiffPair represents a single (source, target) entity pair whose hashes differ.
type DiffPair struct {
	Identifier string
	SourceBlob string
	TargetBlob string
}

// DiffSets is the result of a SQL-level diff between two (blueprint, installation) sets.
type DiffSets struct {
	IdenticalCount int
	Changed        []DiffPair
	NotMigrated    []models.Entity
	Orphaned       []models.Entity
}

// DiffBlueprints computes the cross-installation diff for two blueprint/installation tuples.
// The heavy lifting (intersection by identifier, equality by hash) happens in SQL.
func (s *Store) DiffBlueprints(sourceBP, sourceInstall, targetBP, targetInstall string) (*DiffSets, error) {
	result := &DiffSets{}

	if err := s.db.QueryRow(`
		SELECT COUNT(*)
		FROM entities src
		JOIN entities tgt
		  ON src.identifier = tgt.identifier
		 AND tgt.blueprint = ?
		 AND tgt.installation_id = ?
		WHERE src.blueprint = ?
		  AND src.installation_id = ?
		  AND src.hash = tgt.hash`,
		targetBP, targetInstall, sourceBP, sourceInstall,
	).Scan(&result.IdenticalCount); err != nil {
		return nil, fmt.Errorf("count identical: %w", err)
	}

	changedRows, err := s.db.Query(`
		SELECT src.identifier, src.blob, tgt.blob
		FROM entities src
		JOIN entities tgt
		  ON src.identifier = tgt.identifier
		 AND tgt.blueprint = ?
		 AND tgt.installation_id = ?
		WHERE src.blueprint = ?
		  AND src.installation_id = ?
		  AND src.hash <> tgt.hash`,
		targetBP, targetInstall, sourceBP, sourceInstall,
	)
	if err != nil {
		return nil, fmt.Errorf("query changed: %w", err)
	}
	for changedRows.Next() {
		var pair DiffPair
		if err := changedRows.Scan(&pair.Identifier, &pair.SourceBlob, &pair.TargetBlob); err != nil {
			changedRows.Close()
			return nil, fmt.Errorf("scan changed: %w", err)
		}
		result.Changed = append(result.Changed, pair)
	}
	changedRows.Close()
	if err := changedRows.Err(); err != nil {
		return nil, err
	}

	notMigrated, err := s.queryAntiJoin(sourceBP, sourceInstall, targetBP, targetInstall)
	if err != nil {
		return nil, fmt.Errorf("query not-migrated: %w", err)
	}
	result.NotMigrated = notMigrated

	orphaned, err := s.queryAntiJoin(targetBP, targetInstall, sourceBP, sourceInstall)
	if err != nil {
		return nil, fmt.Errorf("query orphaned: %w", err)
	}
	result.Orphaned = orphaned

	return result, nil
}

func (s *Store) queryAntiJoin(haveBP, haveInstall, otherBP, otherInstall string) ([]models.Entity, error) {
	rows, err := s.db.Query(`
		SELECT have.blob
		FROM entities have
		LEFT JOIN entities other
		  ON have.identifier = other.identifier
		 AND other.blueprint = ?
		 AND other.installation_id = ?
		WHERE have.blueprint = ?
		  AND have.installation_id = ?
		  AND other.identifier IS NULL`,
		otherBP, otherInstall, haveBP, haveInstall,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entities []models.Entity
	for rows.Next() {
		var blob string
		if err := rows.Scan(&blob); err != nil {
			return nil, err
		}
		var entity models.Entity
		if err := json.Unmarshal([]byte(blob), &entity); err != nil {
			return nil, err
		}
		entities = append(entities, entity)
	}
	return entities, rows.Err()
}

// EntityHash returns a stable hash of the parts of an entity that participate in diffs.
// We intentionally exclude server-managed metadata (createdAt/updatedAt/createdBy/updatedBy/blueprint/identifier)
// so two entities with the same logical content always hash equally across integrations.
func EntityHash(entity models.Entity) (string, error) {
	hashable := struct {
		Title      string         `json:"title"`
		Properties map[string]any `json:"properties"`
		Relations  map[string]any `json:"relations"`
	}{
		Title:      entity.Title,
		Properties: entity.Properties,
		Relations:  entity.Relations,
	}

	bytes, err := json.Marshal(hashable)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(bytes)
	return hex.EncodeToString(sum[:]), nil
}
