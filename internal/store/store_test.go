package store

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/port-labs/port-github-migrator/internal/models"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	st, err := Open(dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

func TestSyncTimestampRoundTrip(t *testing.T) {
	st := newTestStore(t)

	if _, ok, err := st.GetSyncTimestamp("bp", "inst"); err != nil || ok {
		t.Fatalf("expected no sync timestamp: ok=%v err=%v", ok, err)
	}

	want := time.Date(2026, 4, 27, 9, 30, 0, 0, time.UTC)
	if err := st.SetSyncTimestamp("bp", "inst", want); err != nil {
		t.Fatalf("set: %v", err)
	}

	got, ok, err := st.GetSyncTimestamp("bp", "inst")
	if err != nil || !ok {
		t.Fatalf("get after set: ok=%v err=%v", ok, err)
	}
	if !got.Equal(want) {
		t.Fatalf("timestamp mismatch: got %v want %v", got, want)
	}

	if err := st.DeleteSyncTimestamp("bp", "inst"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, ok, err := st.GetSyncTimestamp("bp", "inst"); err != nil || ok {
		t.Fatalf("expected timestamp gone: ok=%v err=%v", ok, err)
	}
}

func TestUpsertReplacesExisting(t *testing.T) {
	st := newTestStore(t)

	original := models.Entity{
		Identifier: "repo-1",
		Title:      "Old title",
		Blueprint:  "githubRepository",
		Properties: map[string]any{"stars": float64(1)},
	}
	if err := st.UpsertEntities("githubRepository", "old", []models.Entity{original}); err != nil {
		t.Fatalf("first upsert: %v", err)
	}

	updated := original
	updated.Title = "New title"
	updated.Properties = map[string]any{"stars": float64(42)}
	if err := st.UpsertEntities("githubRepository", "old", []models.Entity{updated}); err != nil {
		t.Fatalf("second upsert: %v", err)
	}

	loaded, err := st.LoadEntities("githubRepository", "old")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(loaded) != 1 {
		t.Fatalf("expected 1 entity, got %d", len(loaded))
	}
	if loaded[0].Title != "New title" {
		t.Fatalf("expected updated title, got %q", loaded[0].Title)
	}
}

func TestDiffBlueprints(t *testing.T) {
	st := newTestStore(t)

	identical := models.Entity{
		Identifier: "shared",
		Title:      "Shared",
		Blueprint:  "githubRepository",
		Properties: map[string]any{"stars": float64(10)},
	}
	changedOld := models.Entity{
		Identifier: "drifted",
		Title:      "Old",
		Blueprint:  "githubRepository",
		Properties: map[string]any{"stars": float64(5)},
	}
	changedNew := models.Entity{
		Identifier: "drifted",
		Title:      "New",
		Blueprint:  "githubRepository",
		Properties: map[string]any{"stars": float64(7)},
	}
	onlyOld := models.Entity{
		Identifier: "not-migrated",
		Title:      "Stuck",
		Blueprint:  "githubRepository",
	}
	onlyNew := models.Entity{
		Identifier: "orphan",
		Title:      "Stranger",
		Blueprint:  "githubRepository",
	}

	if err := st.UpsertEntities("bp", "old", []models.Entity{identical, changedOld, onlyOld}); err != nil {
		t.Fatalf("seed old: %v", err)
	}
	if err := st.UpsertEntities("bp", "new", []models.Entity{identical, changedNew, onlyNew}); err != nil {
		t.Fatalf("seed new: %v", err)
	}

	sets, err := st.DiffBlueprints("bp", "old", "bp", "new")
	if err != nil {
		t.Fatalf("diff: %v", err)
	}

	if sets.IdenticalCount != 1 {
		t.Errorf("identical count: got %d want 1", sets.IdenticalCount)
	}
	if len(sets.Changed) != 1 || sets.Changed[0].Identifier != "drifted" {
		t.Errorf("changed: got %+v", sets.Changed)
	}
	if len(sets.NotMigrated) != 1 || sets.NotMigrated[0].Identifier != "not-migrated" {
		t.Errorf("not migrated: got %+v", sets.NotMigrated)
	}
	if len(sets.Orphaned) != 1 || sets.Orphaned[0].Identifier != "orphan" {
		t.Errorf("orphaned: got %+v", sets.Orphaned)
	}
}

func TestEntityHashStable(t *testing.T) {
	a := models.Entity{
		Identifier: "x",
		Title:      "T",
		Blueprint:  "bp",
		Properties: map[string]any{"a": float64(1), "b": float64(2)},
		Relations:  map[string]any{"r": "v"},
	}
	b := models.Entity{
		Identifier: "x",
		Title:      "T",
		Blueprint:  "bp",
		// Reorder properties to confirm key ordering doesn't affect hash.
		Properties: map[string]any{"b": float64(2), "a": float64(1)},
		Relations:  map[string]any{"r": "v"},
		// Server metadata must not affect hash.
		CreatedAt: "2026-01-01",
		UpdatedAt: "2026-04-27",
	}

	ha, err := EntityHash(a)
	if err != nil {
		t.Fatalf("hash a: %v", err)
	}
	hb, err := EntityHash(b)
	if err != nil {
		t.Fatalf("hash b: %v", err)
	}
	if ha != hb {
		t.Fatalf("expected hashes to match for same-content entities: %s vs %s", ha, hb)
	}

	c := a
	c.Title = "Different"
	hc, err := EntityHash(c)
	if err != nil {
		t.Fatalf("hash c: %v", err)
	}
	if hc == ha {
		t.Fatalf("expected hash to change with title")
	}
}
