package diff

import (
	"testing"

	"github.com/port-labs/port-github-migrator/internal/port"
)

func TestFilterEntity_RemovesProperties(t *testing.T) {
	entity := port.Entity{
		Identifier: "test-id",
		Title:      "Test",
		Properties: map[string]interface{}{
			"pr_age":       "5 days",
			"created_at":   "2026-08-14",
			"pr_age_label": "Old",
		},
	}

	filtered := filterEntity(entity, []string{"pr_age", "pr_age_label"}, []string{})

	// Should only have created_at
	if len(filtered.Properties) != 1 {
		t.Fatalf("expected 1 property, got %d", len(filtered.Properties))
	}
	if _, ok := filtered.Properties["created_at"]; !ok {
		t.Fatal("created_at should exist")
	}
	if _, ok := filtered.Properties["pr_age"]; ok {
		t.Fatal("pr_age should be filtered out")
	}
	if _, ok := filtered.Properties["pr_age_label"]; ok {
		t.Fatal("pr_age_label should be filtered out")
	}
}

func TestFilterEntity_NoIgnoreList(t *testing.T) {
	entity := port.Entity{
		Identifier: "test-id",
		Title:      "Test",
		Properties: map[string]interface{}{
			"pr_age":     "5 days",
			"created_at": "2026-08-14",
		},
	}

	filtered := filterEntity(entity, []string{}, []string{})

	// Should remain unchanged
	if len(filtered.Properties) != 2 {
		t.Fatalf("expected 2 properties, got %d", len(filtered.Properties))
	}
}

func TestFilterEntity_WithRelations(t *testing.T) {
	entity := port.Entity{
		Identifier: "test-id",
		Title:      "Test",
		Properties: map[string]interface{}{
			"pr_age": "5 days",
		},
		Relations: map[string]interface{}{
			"assignee":       "user-1",
			"related_pr_age": "6 days",
		},
	}

	filtered := filterEntity(entity, []string{"pr_age"}, []string{"related_pr_age"})

	// Properties should be filtered
	if _, ok := filtered.Properties["pr_age"]; ok {
		t.Fatal("pr_age should be filtered from properties")
	}

	// Relations should be filtered
	if relMap, ok := filtered.Relations.(map[string]interface{}); ok {
		if _, ok := relMap["related_pr_age"]; ok {
			t.Fatal("related_pr_age should be filtered from relations")
		}
		if _, ok := relMap["assignee"]; !ok {
			t.Fatal("assignee should remain in relations")
		}
	} else {
		t.Fatal("relations should be a map")
	}
}

func TestDiffEntities_WithIgnoreProperties(t *testing.T) {
	source := []port.Entity{
		{
			Identifier: "id-1",
			Title:      "Entity",
			Properties: map[string]interface{}{
				"pr_age": "5 days",
				"status": "open",
			},
		},
	}

	target := []port.Entity{
		{
			Identifier: "id-1",
			Title:      "Entity",
			Properties: map[string]interface{}{
				"pr_age": "6 days", // Different but ignored
				"status": "open",   // Same
			},
		},
	}

	identical, changed, notMigrated := DiffEntities(source, target, []string{"pr_age"}, []string{})

	// Should be identical when pr_age is ignored
	if len(identical) != 1 || identical[0] != "id-1" {
		t.Fatalf("expected 1 identical entity, got %d identical and %d changed", len(identical), len(changed))
	}
	if len(changed) != 0 {
		t.Fatalf("expected 0 changed entities, got %d", len(changed))
	}
	if len(notMigrated) != 0 {
		t.Fatalf("expected 0 not migrated entities, got %d", len(notMigrated))
	}
}

func TestDiffEntities_WithoutIgnoreProperties(t *testing.T) {
	source := []port.Entity{
		{
			Identifier: "id-1",
			Title:      "Entity",
			Properties: map[string]interface{}{
				"pr_age": "5 days",
				"status": "open",
			},
		},
	}

	target := []port.Entity{
		{
			Identifier: "id-1",
			Title:      "Entity",
			Properties: map[string]interface{}{
				"pr_age": "6 days", // Different
				"status": "open",   // Same
			},
		},
	}

	identical, changed, _ := DiffEntities(source, target, []string{}, []string{})

	// Should be changed when pr_age is NOT ignored
	if len(identical) != 0 {
		t.Fatalf("expected 0 identical entities, got %d", len(identical))
	}
	if len(changed) != 1 {
		t.Fatalf("expected 1 changed entity, got %d", len(changed))
	}
	if changed[0].Identifier != "id-1" {
		t.Fatalf("expected changed entity to be id-1, got %s", changed[0].Identifier)
	}
}

func TestDiffEntities_NotMigrated(t *testing.T) {
	source := []port.Entity{
		{
			Identifier: "id-1",
			Title:      "Entity 1",
			Properties: map[string]interface{}{},
		},
		{
			Identifier: "id-2",
			Title:      "Entity 2",
			Properties: map[string]interface{}{},
		},
	}

	target := []port.Entity{
		{
			Identifier: "id-1",
			Title:      "Entity 1",
			Properties: map[string]interface{}{},
		},
		// id-2 missing in target
	}

	identical, _, notMigrated := DiffEntities(source, target, []string{}, []string{})

	if len(identical) != 1 {
		t.Fatalf("expected 1 identical entity, got %d", len(identical))
	}
	if len(notMigrated) != 1 || notMigrated[0] != "id-2" {
		t.Fatalf("expected id-2 in not migrated, got %v", notMigrated)
	}
}

func TestFilterEntity_PreservesIdentifier(t *testing.T) {
	entity := port.Entity{
		Identifier: "preserve-me",
		Title:      "Test",
		Properties: map[string]interface{}{
			"pr_age": "5 days",
		},
	}

	filtered := filterEntity(entity, []string{"pr_age"}, []string{})

	if filtered.Identifier != "preserve-me" {
		t.Fatalf("identifier should be preserved, got %s", filtered.Identifier)
	}
	if filtered.Title != "Test" {
		t.Fatalf("title should be preserved, got %s", filtered.Title)
	}
}
