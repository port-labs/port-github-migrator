// Package store persists per-blueprint identifier lists under the user's
// cache directory so that `migrate` can act on the exact set of entities that
// `get-diff` compared, even across separate invocations.
package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

const (
	appDir         = "port-github-migrator"
	blueprintsDir  = "blueprints"
	manifestSuffix = ".json"
)

// Store is a small wrapper around the on-disk cache directory.
type Store struct {
	root string
}

// Open returns a Store rooted at <UserCacheDir>/port-github-migrator/. The
// directory is not created until the first write.
func Open() (*Store, error) {
	base, err := os.UserCacheDir()
	if err != nil {
		return nil, fmt.Errorf("failed to resolve user cache dir: %w", err)
	}
	return &Store{root: filepath.Join(base, appDir)}, nil
}

// Root returns the on-disk directory backing this store.
func (s *Store) Root() string { return s.root }

// ManifestPath returns the absolute path that the identifier list for the
// given (oldInstallationID, blueprint) is stored at. Lists are namespaced by
// old installation id so that concurrent migrations of different integrations
// do not collide.
func (s *Store) ManifestPath(oldInstallID, bp string) string {
	return filepath.Join(s.root, blueprintsDir, oldInstallID, bp+manifestSuffix)
}

// SaveIdentifiers writes (or overwrites) the identifier list for
// (oldInstallID, bp) and returns the file path it was written to.
func (s *Store) SaveIdentifiers(oldInstallID, bp string, identifiers []string) (string, error) {
	if oldInstallID == "" || bp == "" {
		return "", errors.New("oldInstallID and blueprint are required")
	}

	path := s.ManifestPath(oldInstallID, bp)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", fmt.Errorf("failed to create cache dir: %w", err)
	}

	data, err := json.MarshalIndent(identifiers, "", "  ")
	if err != nil {
		return "", err
	}

	if err := os.WriteFile(path, data, 0o644); err != nil {
		return "", err
	}
	return path, nil
}

// LoadIdentifiers returns the saved identifier list for (oldInstallID, bp).
// If no list has been saved, it returns (nil, nil) so callers can simply
// branch on the nil result.
func (s *Store) LoadIdentifiers(oldInstallID, bp string) ([]string, error) {
	data, err := os.ReadFile(s.ManifestPath(oldInstallID, bp))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var identifiers []string
	if err := json.Unmarshal(data, &identifiers); err != nil {
		return nil, fmt.Errorf("failed to parse identifier list %s: %w", bp, err)
	}
	return identifiers, nil
}

// DeleteIdentifiers removes a blueprint's identifier file. It's a no-op if
// the file is already gone.
func (s *Store) DeleteIdentifiers(oldInstallID, bp string) error {
	if err := os.Remove(s.ManifestPath(oldInstallID, bp)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}
