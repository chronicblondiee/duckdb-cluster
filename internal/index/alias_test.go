package index

import (
	"testing"
)

func TestAliasManagerCRUD(t *testing.T) {
	dir := t.TempDir()
	am := NewAliasManager(dir)

	// Put
	if err := am.Put("logs", []string{"logs-2024", "logs-2025"}); err != nil {
		t.Fatalf("Put: %v", err)
	}

	// Get
	a, err := am.Get("logs")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if a.Name != "logs" {
		t.Errorf("expected name 'logs', got %q", a.Name)
	}
	if len(a.Indices) != 2 {
		t.Errorf("expected 2 indices, got %d", len(a.Indices))
	}

	// List
	all := am.List()
	if len(all) != 1 {
		t.Errorf("expected 1 alias, got %d", len(all))
	}

	// Resolve
	indices := am.Resolve("logs")
	if len(indices) != 2 {
		t.Errorf("Resolve: expected 2 indices, got %d", len(indices))
	}

	// Resolve non-existent
	if am.Resolve("nope") != nil {
		t.Error("expected nil for non-existent alias")
	}

	// Delete
	if err := am.Delete("logs"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := am.Get("logs"); err == nil {
		t.Error("expected error after delete")
	}
}

func TestAliasManagerPersistence(t *testing.T) {
	dir := t.TempDir()

	am1 := NewAliasManager(dir)
	if err := am1.Put("prod", []string{"prod-v1"}); err != nil {
		t.Fatalf("Put: %v", err)
	}

	am2 := NewAliasManager(dir)
	if err := am2.LoadAll(); err != nil {
		t.Fatalf("LoadAll: %v", err)
	}

	a, err := am2.Get("prod")
	if err != nil {
		t.Fatalf("Get after reload: %v", err)
	}
	if a.Indices[0] != "prod-v1" {
		t.Errorf("expected 'prod-v1', got %q", a.Indices[0])
	}
}

func TestAliasManagerValidation(t *testing.T) {
	dir := t.TempDir()
	am := NewAliasManager(dir)

	if err := am.Put("INVALID!", []string{"idx"}); err == nil {
		t.Error("expected error for invalid name")
	}

	if err := am.Put("valid", nil); err == nil {
		t.Error("expected error for empty indices")
	}
}
